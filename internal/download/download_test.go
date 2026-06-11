package download

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ytdlp-gui/internal/config"
	"ytdlp-gui/internal/db"
	"ytdlp-gui/internal/ytdlp"
)

// fakeEmitter records every emitted event under a mutex (events arrive from the
// run goroutine).
type fakeEmitter struct {
	mu     sync.Mutex
	events []DownloadEvent
}

func (f *fakeEmitter) Emit(event string, data ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event != EventName {
		return
	}
	if len(data) == 1 {
		if ev, ok := data[0].(DownloadEvent); ok {
			f.events = append(f.events, ev)
		}
	}
}

func (f *fakeEmitter) snapshot() []DownloadEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]DownloadEvent, len(f.events))
	copy(out, f.events)
	return out
}

func (f *fakeEmitter) typesSeen() map[string]int {
	out := map[string]int{}
	for _, e := range f.snapshot() {
		out[e.Type]++
	}
	return out
}

// waitFor polls cond until true or the deadline; fails the test on timeout.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

func openTestStore(t *testing.T) *db.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "lib", "library.db")
	s, _, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seqIDGen yields deterministic ids for assertions.
func seqIDGen() func() (string, error) {
	var n int
	var mu sync.Mutex
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return "id" + string(rune('0'+n)), nil
	}
}

func TestStartCompletesAndPersists(t *testing.T) {
	store := openTestStore(t)
	emit := &fakeEmitter{}

	// fake download fn: emit one progress + one DONE, then succeed.
	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		cb.OnProgress(ytdlp.Progress{PlaylistIndex: 0, Percent: 50, DownloadedBytes: 500, TotalBytes: 1000})
		cb.OnItemDone(ytdlp.Completion{
			Extractor: "YouTube",
			ID:        "abc123",
			Title:     "Test Video",
			Uploader:  "Chan",
			Ext:       "mp4",
			Filesize:  1000,
			Filepath:  "/dl/Test Video.mp4",
		})
		return nil
	}

	m := New(store, emit, fn, 2)
	id, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/v"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("Start returned empty id")
	}

	waitFor(t, func() bool { return emit.typesSeen()[TypeCompleted] == 1 }, "completed event")

	seen := emit.typesSeen()
	for _, want := range []string{TypeItemStart, TypeProgress, TypeItemDone, TypeCompleted} {
		if seen[want] == 0 {
			t.Errorf("missing %q event; seen=%v", want, seen)
		}
	}

	// item-done must carry the persisted content id.
	var doneEv *DownloadEvent
	for _, e := range emit.snapshot() {
		if e.Type == TypeItemDone {
			ev := e
			doneEv = &ev
		}
	}
	if doneEv == nil || doneEv.ContentID == 0 {
		t.Fatalf("item-done missing a content id: %+v", doneEv)
	}
	if doneEv.Filepath != "/dl/Test Video.mp4" {
		t.Errorf("item-done filepath = %q", doneEv.Filepath)
	}

	// The content row must exist with the lowercased source + completed status.
	c, err := store.GetContent(doneEv.ContentID)
	if err != nil {
		t.Fatalf("GetContent: %v", err)
	}
	if c.Source != "youtube" {
		t.Errorf("source = %q, want youtube (lowercased extractor)", c.Source)
	}
	if c.Status != db.StatusCompleted {
		t.Errorf("status = %q, want completed", c.Status)
	}
	if c.Title != "Test Video" || c.Filepath != "/dl/Test Video.mp4" {
		t.Errorf("row = %+v", c)
	}
}

func TestQueuedWhenOverCapacity(t *testing.T) {
	emit := &fakeEmitter{}

	// release gates the fake download fn so we can hold both slots busy.
	release := make(chan struct{})
	var running sync.WaitGroup
	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		running.Done()
		<-release
		return nil
	}

	m := New(nil, emit, fn, 1) // capacity 1
	m.idgen = seqIDGen()

	// First download occupies the single slot.
	running.Add(1)
	if _, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/1"}); err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	running.Wait() // slot now held

	// Second download can't get a slot → must emit queued.
	if _, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/2"}); err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	waitFor(t, func() bool { return emit.typesSeen()[TypeQueued] >= 1 }, "queued event for over-capacity download")

	// Release both; everything completes.
	running.Add(1)
	close(release)
	running.Wait()
	waitFor(t, func() bool { return emit.typesSeen()[TypeCompleted] == 2 }, "both completed")
}

func TestCancel(t *testing.T) {
	emit := &fakeEmitter{}

	started := make(chan struct{})
	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		close(started)
		<-ctx.Done() // block until cancelled
		return ctx.Err()
	}

	m := New(nil, emit, fn, 1)
	m.idgen = seqIDGen()

	id, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/1"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started

	if err := m.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	waitFor(t, func() bool { return emit.typesSeen()[TypeCancelled] == 1 }, "cancelled event")
	if emit.typesSeen()[TypeCompleted] != 0 {
		t.Error("a cancelled download must not emit completed")
	}

	// The cancelFunc must be deregistered after settling: a second Cancel errors.
	waitFor(t, func() bool { return m.Cancel(id) != nil }, "cancelFunc deregistered after settle")
}

func TestCancelUnknownErrors(t *testing.T) {
	m := New(nil, &fakeEmitter{}, func(context.Context, ytdlp.Options, ytdlp.Request, ytdlp.Callbacks) error { return nil }, 1)
	if err := m.Cancel("nope"); err == nil {
		t.Error("Cancel of unknown id should error")
	}
}

func TestFailedEmitsError(t *testing.T) {
	emit := &fakeEmitter{}
	wantErr := errors.New("yt-dlp download: exit status 1: ERROR: boom")
	fn := func(context.Context, ytdlp.Options, ytdlp.Request, ytdlp.Callbacks) error { return wantErr }

	m := New(nil, emit, fn, 1)
	if _, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/1"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return emit.typesSeen()[TypeFailed] == 1 }, "failed event")

	var failed *DownloadEvent
	for _, e := range emit.snapshot() {
		if e.Type == TypeFailed {
			ev := e
			failed = &ev
		}
	}
	if failed == nil || failed.Error == "" {
		t.Fatalf("failed event missing error: %+v", failed)
	}
}

func TestPlaylistLinkage(t *testing.T) {
	store := openTestStore(t)
	emit := &fakeEmitter{}

	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		// Two items of a playlist.
		cb.OnItemDone(ytdlp.Completion{
			PlaylistIndex: 1, Extractor: "youtube", ID: "v1", Title: "One",
			Ext: "mp4", Filepath: "/dl/PL/One.mp4",
		})
		cb.OnItemDone(ytdlp.Completion{
			PlaylistIndex: 2, Extractor: "youtube", ID: "v2", Title: "Two",
			Ext: "mp4", Filepath: "/dl/PL/Two.mp4",
		})
		return nil
	}

	m := New(store, emit, fn, 1)
	_, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{
		URL:              "https://x/playlist",
		IsPlaylist:       true,
		SelectedIndices:  []int{1, 2},
		PlaylistTitle:    "My Playlist",
		PlaylistSourceID: "PL123",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return emit.typesSeen()[TypeCompleted] == 1 }, "completed")

	// Both content rows should share the same non-zero playlist id and carry
	// their 1-based index + the joined playlist title.
	res, err := store.ListContent(db.ListOptions{})
	if err != nil {
		t.Fatalf("ListContent: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("got %d rows, want 2", len(res.Items))
	}
	pid := res.Items[0].PlaylistID
	if pid == 0 {
		t.Fatal("first row has no playlist id")
	}
	for _, c := range res.Items {
		if c.PlaylistID != pid {
			t.Errorf("row %q playlist id = %d, want %d (shared)", c.Title, c.PlaylistID, pid)
		}
		if c.PlaylistTitle != "My Playlist" {
			t.Errorf("row %q playlist title = %q", c.Title, c.PlaylistTitle)
		}
		if c.PlaylistIndex == 0 {
			t.Errorf("row %q has no playlist index", c.Title)
		}
	}
}

func TestProgressThrottle(t *testing.T) {
	emit := &fakeEmitter{}

	// Fire many rapid progress updates for the same item; the throttle should
	// collapse them. The terminal item-done/completed always fire.
	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		for i := 0; i < 50; i++ {
			cb.OnProgress(ytdlp.Progress{PlaylistIndex: 1, Percent: float64(i)})
		}
		return nil
	}

	m := New(nil, emit, fn, 1)
	if _, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/1"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return emit.typesSeen()[TypeCompleted] == 1 }, "completed")

	if got := emit.typesSeen()[TypeProgress]; got > 5 {
		t.Errorf("progress events = %d, want throttled to a small number (<=5) for a rapid burst", got)
	}
	// At least the first progress must have come through.
	if emit.typesSeen()[TypeProgress] < 1 {
		t.Error("expected at least one progress event")
	}
}
