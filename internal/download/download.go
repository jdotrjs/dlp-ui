// Package download is the active-download manager: it owns concurrency
// (a maxConcurrent semaphore), per-download cancellation, the single Wails
// `download` event channel, and persistence of finished items to the library
// store. It wires the Wails-agnostic ytdlp.Download progress/completion
// callbacks to runtime events via an injected Emitter, so the package itself
// imports neither wails/v2/pkg/runtime nor internal/binaries and is fully
// unit-testable with a fake Emitter + a fake download function + a temp store.
package download

import (
	"context"
	"fmt"
	"sync"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"ytdlp-gui/internal/config"
	"ytdlp-gui/internal/db"
	"ytdlp-gui/internal/ytdlp"
)

// EventName is the single Wails event channel every download update is emitted
// on. The frontend subscribes once and switches on the payload's Type.
const EventName = "download"

// Event type discriminants. These exact strings cross the Wails binding to the
// frontend reducer (see frontend/src/lib/types.ts DownloadEvent union).
const (
	TypeQueued    = "queued"
	TypeItemStart = "item-start"
	TypeProgress  = "progress"
	TypeItemDone  = "item-done"
	TypeCompleted = "completed"
	TypeFailed    = "failed"
	TypeCancelled = "cancelled"
)

// progressThrottle caps how often a `progress` event is emitted per item.
const progressThrottle = 200 * time.Millisecond

// Emitter is the seam onto the Wails runtime. The concrete implementation
// (app.go's runtimeEmitter) wraps runtime.EventsEmit(ctx, ...); tests pass a
// fake. Mirrors the runtime.EventsEmit signature.
type Emitter interface {
	Emit(event string, data ...any)
}

// DownloadFunc runs one yt-dlp download, streaming progress/completion via cb,
// blocking until the child exits. It is the injection seam for ytdlp.Download
// so the manager is testable without a real binary. ctx cancels the child.
type DownloadFunc func(ctx context.Context, opts ytdlp.Options, req ytdlp.Request, cb ytdlp.Callbacks) error

// DownloadRequest is the bound request shape StartDownload consumes. JSON tags
// match the frontend DownloadRequest (frontend/src/lib/types.ts) so the modal's
// assembled object binds directly. It lives here (not in main) so the manager
// can consume it without an import cycle; app.go's bound method references it,
// which surfaces it in the generated models as the `download` namespace.
type DownloadRequest struct {
	// URL is the original page URL the user fetched.
	URL string `json:"url"`
	// IsPlaylist mirrors Preview.isPlaylist.
	IsPlaylist bool `json:"isPlaylist"`
	// SelectedIndices are the 1-based playlist item indices to download; empty
	// for a single video.
	SelectedIndices []int `json:"selectedIndices"`
	// FormatID is a concrete yt-dlp format id, or "" meaning best (synthetic).
	FormatID string `json:"formatId"`
	// AudioOnly requests audio extraction (yt-dlp -x), overriding FormatID.
	AudioOnly bool `json:"audioOnly"`
	// Optional per-download overrides; empty means "use config".
	OutputTemplate       string `json:"outputTemplate"`
	PlaylistModeOverride string `json:"playlistModeOverride"`
	DownloadDir          string `json:"downloadDir"`
	// PlaylistTitle / PlaylistSourceId describe the grouping for the library.
	PlaylistTitle    string `json:"playlistTitle"`
	PlaylistSourceID string `json:"playlistSourceId"`
}

// DownloadEvent is the single tagged-union payload emitted on the `download`
// channel. Type is always set; the remaining fields are populated per event
// type (omitempty so absent fields don't clutter the JSON). The frontend
// narrows on Type (see the DownloadEvent discriminated union in types.ts).
type DownloadEvent struct {
	Type       string `json:"type"`
	DownloadID string `json:"downloadID"`

	// progress fields.
	ItemIndex       int     `json:"itemIndex,omitempty"`
	Percent         float64 `json:"percent,omitempty"`
	DownloadedBytes int64   `json:"downloadedBytes,omitempty"`
	TotalBytes      int64   `json:"totalBytes,omitempty"`
	Speed           float64 `json:"speed,omitempty"`
	ETA             int     `json:"eta,omitempty"`

	// item-done fields.
	ContentID int64  `json:"contentID,omitempty"`
	Filepath  string `json:"filepath,omitempty"`

	// failed fields.
	Error string `json:"error,omitempty"`
}

// Manager owns the active downloads. Construct it with New.
type Manager struct {
	store    *db.Store
	emit     Emitter
	download DownloadFunc

	// sem is a counting semaphore sized by maxConcurrent: acquiring a slot is a
	// send, releasing is a receive. Sized at construction — changing
	// MaxConcurrent in config takes effect on the next app restart.
	sem chan struct{}

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	// closed is set in Close; new Starts are rejected after it.
	closed bool

	// idgen produces download ids; overridable in tests for determinism.
	idgen func() (string, error)

	// fetchThumbnail downloads a thumbnail URL into bytes; overridable in
	// tests so they don't reach out to the network. Production uses
	// fetchThumbnailHTTP.
	fetchThumbnail thumbnailFetcher
}

// New builds a Manager. maxConcurrent < 1 is clamped to 1. store may be nil
// (then finished items are not persisted, but events still fire) — though in
// production app.go only builds the manager when the store opened.
func New(store *db.Store, emit Emitter, fn DownloadFunc, maxConcurrent int) *Manager {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Manager{
		store:          store,
		emit:           emit,
		download:       fn,
		sem:            make(chan struct{}, maxConcurrent),
		cancels:        make(map[string]context.CancelFunc),
		idgen:          func() (string, error) { return gonanoid.New() },
		fetchThumbnail: fetchThumbnailHTTP,
	}
}

// Start validates and launches a download, returning its generated id
// immediately. All further updates arrive as events on the `download` channel.
// opts are the resolved binary paths; cfg supplies output/format defaults. The
// actual yt-dlp run happens on a background goroutine; if all semaphore slots
// are busy, a `queued` event fires and the goroutine blocks until one frees.
func (m *Manager) Start(opts ytdlp.Options, cfg config.Config, req DownloadRequest) (string, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", fmt.Errorf("download manager is shutting down")
	}
	m.mu.Unlock()

	if req.URL == "" {
		return "", fmt.Errorf("download: empty URL")
	}

	id, err := m.idgen()
	if err != nil {
		return "", fmt.Errorf("generate download id: %w", err)
	}

	go m.run(id, opts, cfg, req)
	return id, nil
}

// Cancel cancels an in-flight download by id. It returns an error if no such
// download is active (already finished or never existed). The registered
// cancelFunc kills the yt-dlp child; the run goroutine emits `cancelled`.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	cancel, ok := m.cancels[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("download %q is not active", id)
	}
	cancel()
	return nil
}

// Close cancels every active download and prevents new ones. Called from
// app.shutdown. It does not block on goroutines (the process is exiting).
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, c := range m.cancels {
		cancels = append(cancels, c)
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// run is the background lifecycle of one download: queue → acquire slot →
// (playlist grouping) → yt-dlp run with progress/completion callbacks →
// settle (completed / cancelled / failed). It always releases the slot and
// deregisters the cancelFunc on exit.
func (m *Manager) run(id string, opts ytdlp.Options, cfg config.Config, req DownloadRequest) {
	// Acquire a slot; if not immediately available, announce queued first.
	select {
	case m.sem <- struct{}{}:
	default:
		m.emit.Emit(EventName, DownloadEvent{Type: TypeQueued, DownloadID: id})
		m.sem <- struct{}{}
	}
	defer func() { <-m.sem }()

	// A child-killable context registered for Cancel.
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[id] = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, id)
		m.mu.Unlock()
	}()

	// Resolve a playlist grouping up front so each finished item can link to it.
	// The authoritative per-item source is the extractor on each DONE line; the
	// playlist grouping has no extractor available at request time, so we seed
	// its source from the first finished item's extractor below (a fresh ad-hoc
	// grouping with empty source is the loose-URL case). We upsert the grouping
	// lazily on the first DONE so its source matches the items.
	var (
		playlistID   int64
		playlistOnce sync.Once
		playlistErr  error
	)
	ensurePlaylist := func(itemSource string) int64 {
		if !req.IsPlaylist {
			return 0
		}
		playlistOnce.Do(func() {
			playlistID, playlistErr = m.upsertPlaylist(req, itemSource)
		})
		_ = playlistErr // non-fatal: items still persist, just unlinked
		return playlistID
	}

	m.emit.Emit(EventName, DownloadEvent{Type: TypeItemStart, DownloadID: id})

	// Per-item throttle bookkeeping for progress events.
	lastEmit := make(map[int]time.Time)

	yreq := buildYtdlpRequest(cfg, req)

	cb := ytdlp.Callbacks{
		OnProgress: func(p ytdlp.Progress) {
			now := time.Now()
			if last, ok := lastEmit[p.PlaylistIndex]; ok && now.Sub(last) < progressThrottle {
				return
			}
			lastEmit[p.PlaylistIndex] = now
			m.emit.Emit(EventName, DownloadEvent{
				Type:            TypeProgress,
				DownloadID:      id,
				ItemIndex:       p.PlaylistIndex,
				Percent:         p.Percent,
				DownloadedBytes: p.DownloadedBytes,
				TotalBytes:      p.TotalBytes,
				Speed:           p.Speed,
				ETA:             p.ETA,
			})
		},
		OnItemDone: func(c ytdlp.Completion) {
			itemSource := normalizeExtractor(c.Extractor)
			pid := ensurePlaylist(itemSource)
			contentID := m.persistItem(req, itemSource, pid, c)
			// Kick the thumbnail HTTP fetch off the OnItemDone path so it
			// doesn't delay the item-done event. Best-effort: a failure is
			// silently dropped (the UI just shows no thumbnail).
			if contentID != 0 && c.Thumbnail != "" {
				go m.fetchAndSaveThumbnail(contentID, c.Thumbnail)
			}
			m.emit.Emit(EventName, DownloadEvent{
				Type:       TypeItemDone,
				DownloadID: id,
				ItemIndex:  c.PlaylistIndex,
				ContentID:  contentID,
				Filepath:   c.Filepath,
			})
		},
	}

	err := m.download(ctx, opts, yreq, cb)

	switch {
	case err == nil:
		m.emit.Emit(EventName, DownloadEvent{Type: TypeCompleted, DownloadID: id})
	case ctx.Err() == context.Canceled:
		// Cancelled by the user (or shutdown). No completed row is written for
		// the in-flight item; yt-dlp's .part is left for resume.
		m.emit.Emit(EventName, DownloadEvent{Type: TypeCancelled, DownloadID: id})
	default:
		m.emit.Emit(EventName, DownloadEvent{
			Type:       TypeFailed,
			DownloadID: id,
			Error:      err.Error(),
		})
	}
}

// buildYtdlpRequest translates a DownloadRequest + config into the ytdlp.Request
// the wrapper consumes: format/audio flags, the comma-joined playlist-items
// selection, the resolved output-template args (config owns the layout), and
// the per-request download dir.
func buildYtdlpRequest(cfg config.Config, req DownloadRequest) ytdlp.Request {
	downloadDir := req.DownloadDir
	if downloadDir == "" {
		downloadDir = cfg.DownloadDir
	}
	return ytdlp.Request{
		URL:           req.URL,
		AudioOnly:     req.AudioOnly,
		FormatID:      req.FormatID,
		PlaylistItems: joinIndices(req.SelectedIndices),
		OutputArgs:    cfg.DownloadArgs(req.IsPlaylist, req.OutputTemplate, config.PlaylistMode(req.PlaylistModeOverride)),
		DownloadDir:   downloadDir,
	}
}

// upsertPlaylist ensures a playlist grouping row exists for the download and
// returns its id. Source is the lowercased extractor; an empty PlaylistSourceID
// always inserts a fresh grouping (loose-URL case, by design).
func (m *Manager) upsertPlaylist(req DownloadRequest, source string) (int64, error) {
	if m.store == nil {
		return 0, nil
	}
	return m.store.UpsertPlaylist(db.Playlist{
		Source:    source,
		SourceID:  req.PlaylistSourceID,
		Title:     req.PlaylistTitle,
		SourceURL: req.URL,
		ItemCount: len(req.SelectedIndices),
		CreatedAt: time.Now().Unix(),
	})
}

// persistItem upserts a completed content row from a parsed DONE line and
// returns its id (0 when no store / on error — the event still fires so the UI
// reflects completion even if persistence failed). source is the per-item
// extractor already normalized by the caller.
func (m *Manager) persistItem(req DownloadRequest, source string, playlistID int64, c ytdlp.Completion) int64 {
	if m.store == nil {
		return 0
	}
	content := db.Content{
		Source:        source,
		SourceID:      c.ID,
		Title:         c.Title,
		Uploader:      c.Uploader,
		Duration:      c.Duration,
		UploadDate:    c.UploadDate,
		ThumbnailURL:  c.Thumbnail,
		Ext:           c.Ext,
		Filesize:      c.Filesize,
		SourceURL:     firstNonEmpty(c.WebpageURL, req.URL),
		Filepath:      c.Filepath,
		Status:        db.StatusCompleted,
		PlaylistID:    playlistID,
		PlaylistIndex: c.PlaylistIndex,
		DownloadedAt:  time.Now().Unix(),
	}
	id, err := m.store.UpsertContent(content)
	if err != nil {
		return 0
	}
	return id
}
