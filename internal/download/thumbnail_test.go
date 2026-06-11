package download

import (
	"context"
	"errors"
	"sync"
	"testing"

	"ytdlp-gui/internal/config"
	"ytdlp-gui/internal/db"
	"ytdlp-gui/internal/ytdlp"
)

func TestThumbnailFetchedAndSaved(t *testing.T) {
	store := openTestStore(t)
	emit := &fakeEmitter{}

	var (
		gotURL string
		gotMu  sync.Mutex
	)
	fakeFetch := func(ctx context.Context, url string) (string, []byte, error) {
		gotMu.Lock()
		gotURL = url
		gotMu.Unlock()
		return "image/jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, nil
	}

	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		cb.OnItemDone(ytdlp.Completion{
			Extractor: "youtube",
			ID:        "vid1",
			Title:     "T",
			Thumbnail: "http://example.com/t.jpg",
			Ext:       "mp4",
			Filepath:  "/dl/T.mp4",
		})
		return nil
	}

	m := New(store, emit, fn, 1)
	m.fetchThumbnail = fakeFetch

	if _, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/v"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return emit.typesSeen()[TypeCompleted] == 1 }, "completed")

	// item-done is emitted before the thumbnail goroutine has finished, so
	// poll until SaveThumbnail completes.
	var doneEv *DownloadEvent
	for _, e := range emit.snapshot() {
		if e.Type == TypeItemDone {
			ev := e
			doneEv = &ev
		}
	}
	if doneEv == nil {
		t.Fatal("missing item-done event")
	}
	cid := doneEv.ContentID
	waitFor(t, func() bool {
		has, _ := store.HasThumbnail(cid)
		return has
	}, "thumbnail saved")

	got, err := store.GetThumbnail(cid)
	if err != nil {
		t.Fatalf("GetThumbnail: %v", err)
	}
	if got.Mime != "image/jpeg" || len(got.Data) != 4 {
		t.Errorf("thumbnail row = %+v", got)
	}
	gotMu.Lock()
	defer gotMu.Unlock()
	if gotURL != "http://example.com/t.jpg" {
		t.Errorf("fetcher called with %q", gotURL)
	}
}

func TestThumbnailFetchFailureIsSilent(t *testing.T) {
	store := openTestStore(t)
	emit := &fakeEmitter{}

	fakeFetch := func(ctx context.Context, url string) (string, []byte, error) {
		return "", nil, errors.New("network down")
	}

	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		cb.OnItemDone(ytdlp.Completion{
			Extractor: "youtube",
			ID:        "vid-fail",
			Title:     "T",
			Thumbnail: "http://example.com/dead.jpg",
			Ext:       "mp4",
			Filepath:  "/dl/T.mp4",
		})
		return nil
	}

	m := New(store, emit, fn, 1)
	m.fetchThumbnail = fakeFetch

	if _, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/v"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return emit.typesSeen()[TypeCompleted] == 1 }, "completed")

	// The content row exists but there is no thumbnail row.
	res, _ := store.ListContent(db.ListOptions{})
	if len(res.Items) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Items))
	}
	if has, _ := store.HasThumbnail(res.Items[0].ID); has {
		t.Error("thumbnail should not be saved on fetch error")
	}
}

func TestThumbnailSkippedWhenNoURL(t *testing.T) {
	store := openTestStore(t)
	emit := &fakeEmitter{}

	var called bool
	fakeFetch := func(ctx context.Context, url string) (string, []byte, error) {
		called = true
		return "", nil, nil
	}

	fn := func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error {
		cb.OnItemDone(ytdlp.Completion{
			Extractor: "youtube", ID: "vid", Title: "T", Filepath: "/dl/T.mp4",
		})
		return nil
	}

	m := New(store, emit, fn, 1)
	m.fetchThumbnail = fakeFetch

	if _, err := m.Start(ytdlp.Options{}, config.Default(), DownloadRequest{URL: "https://x/v"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return emit.typesSeen()[TypeCompleted] == 1 }, "completed")

	if called {
		t.Error("fetcher should not be invoked when Completion.Thumbnail is empty")
	}
}
