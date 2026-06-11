package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"ytdlp-gui/internal/binaries"
	"ytdlp-gui/internal/config"
	"ytdlp-gui/internal/db"
	"ytdlp-gui/internal/download"
	"ytdlp-gui/internal/updater"
	"ytdlp-gui/internal/ytdlp"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// errStoreUnavailable is returned by the library-backed bound methods when the
// store failed to open at startup (e.g. the configured DB path is on removable
// media that's gone). The frontend surfaces this so the user can fix the path
// in Settings rather than the app crashing.
var errStoreUnavailable = errors.New("library database is unavailable; check the download directory / database path in Settings")

// errYtdlpMissing is returned by fetch/download methods when yt-dlp can't be
// resolved from config or PATH. yt-dlp is required (not optional like
// ffmpeg/node), so the frontend surfaces this as a hard "open Settings" prompt.
var errYtdlpMissing = errors.New("yt-dlp was not found; set its path in Settings or install it on your PATH")

// App is the thin Wails facade. It holds the runtime context, the loaded
// config, and (in later chunks) the long-lived services such as the library
// store. Bound methods delegate into the internal packages.
type App struct {
	ctx context.Context

	// mu guards cfg, which can be replaced by SaveConfig from the frontend
	// while bound methods read it.
	mu  sync.RWMutex
	cfg config.Config

	// firstRun is true when startup created config.json (no prior config
	// existed). The frontend reads this via IsFirstRun() to trigger the
	// welcome/setup wizard. Set once in startup; never mutated afterwards
	// so reads don't need the mutex.
	firstRun bool

	// store is the library database, opened once in startup and closed in
	// shutdown. It is nil when the open failed (see errStoreUnavailable); the
	// *sql.DB pool inside is itself concurrency-safe so no per-call locking is
	// needed around store access.
	store *db.Store

	// downloads is the active-download manager, built in startup (after the
	// store) and closed in shutdown. It owns concurrency/cancel and emits
	// progress on the `download` event channel via the runtimeEmitter below. It
	// is nil only if startup failed to build it (it does not depend on the store
	// being non-nil — a nil store just means finished rows aren't persisted).
	downloads *download.Manager

	// versionRunner runs `<bin> --version` for the doctor. It's a field so
	// tests (and future fakes) can substitute it.
	versionRunner binaries.VersionRunner
}

// runtimeEmitter is the download.Emitter implementation backed by the Wails
// runtime. It lives here (not in internal/download) so that package stays
// runtime-free and unit-testable. EventsEmit fans the event out to all
// frontend subscribers of the channel.
type runtimeEmitter struct {
	ctx context.Context
}

func (e runtimeEmitter) Emit(event string, data ...any) {
	wruntime.EventsEmit(e.ctx, event, data...)
}

// NewApp creates a new App.
func NewApp() *App {
	return &App{
		versionRunner: binaries.ExecVersionRunner{},
	}
}

// startup is called by Wails when the app starts. It saves the context and
// builds the application's services: load config first, then (in later chunks)
// open the library store and construct the download/fetch services, storing
// them all on *App. The single store opened here would be closed in shutdown.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, _, firstRun, err := config.Load()
	if err != nil {
		// Non-fatal: fall back to in-memory defaults so the UI still loads
		// and the user can fix paths in Settings.
		wruntime.LogErrorf(ctx, "load config: %v", err)
		cfg = config.Default()
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	a.firstRun = firstRun

	// Open the library store exactly once, here, from the resolved DB path; the
	// single *Store is shared by every bound method and closed in shutdown. A
	// failure (parent unwritable, removable media gone) is non-fatal: leave the
	// store nil so the library methods return errStoreUnavailable and the user
	// can still reach Settings to fix the path. Don't silently create the DB
	// somewhere else.
	dbPath := cfg.ResolvedDBPath()
	store, freshlyCreated, err := db.Open(dbPath)
	if err != nil {
		wruntime.LogErrorf(ctx, "open library db %q: %v", dbPath, err)
	} else {
		a.store = store
		if freshlyCreated {
			// Per the DB-orphaning warning: a fresh DB where an existing one was
			// expected may mean the path is wrong (e.g. removable media). We log
			// it; a richer UI surface can read this signal later.
			wruntime.LogInfof(ctx, "created a fresh library database at %q", dbPath)
		} else {
			wruntime.LogInfof(ctx, "opened library database at %q", dbPath)
		}
	}

	wruntime.LogInfof(ctx, "dbPath: %s", dbPath)
	wruntime.LogInfof(ctx, "download dir: %s", cfg.DownloadDir)

	// Wrap the version runner with the meta-backed cache so ResolveBinaries
	// reuses the previous `yt-dlp --version` output when the binary hash hasn't
	// changed. NewApp sets the bare ExecVersionRunner so unit-test callers that
	// skip startup keep working; we replace it here once the store exists and
	// the in-memory config is loaded. The skip closure reads cfg under the
	// mutex so flipping SkipYtdlpVersionCache in Settings takes effect on the
	// next ResolveBinaries without a restart. Skip the wrap entirely when the
	// store is nil — a typed-nil *db.Store wrapped in the metaCache interface
	// is not equal to nil, so we just keep the bare runner instead.
	if a.store != nil {
		a.versionRunner = cachingVersionRunner{
			inner: binaries.ExecVersionRunner{},
			store: a.store,
			skip: func() bool {
				a.mu.RLock()
				defer a.mu.RUnlock()
				return a.cfg.SkipYtdlpVersionCache
			},
		}
	}

	// Build the download manager: it emits progress on the `download` channel
	// via the runtime-backed emitter, persists finished rows to a.store (nil is
	// tolerated — events still fire, rows just aren't saved), and sizes its
	// concurrency from MaxConcurrent. Sized once here: a MaxConcurrent change in
	// Settings takes effect on the next restart (consistent with the existing
	// config-change limitations).
	a.downloads = download.New(a.store, runtimeEmitter{ctx: ctx}, ytdlp.Download, cfg.MaxConcurrent)

	// Kick off an auto update-check off the main goroutine if the cached
	// check is older than updateCheckInterval (or never). A small delay gives
	// the frontend time to subscribe to UpdateStatusEvent before the result
	// fires; if it misses the event the frontend's mount-time GetUpdateStatus
	// still picks up the fresh state.
	go a.maybeAutoUpdateCheck()
}

// maybeAutoUpdateCheck fires a background update check when the persisted
// last-check timestamp is stale (or absent). Errors are logged, never
// surfaced — the user can still trigger a manual check from Settings.
func (a *App) maybeAutoUpdateCheck() {
	// Brief delay so the frontend has a chance to subscribe before we emit.
	time.Sleep(2 * time.Second)

	cached := updater.Load(Version, VersionName, a.metaStore())
	if !cached.IsStale(updateCheckInterval) {
		return
	}
	if _, err := a.CheckForUpdate(); err != nil {
		wruntime.LogWarningf(a.ctx, "auto update check failed: %v", err)
	}
}

// metaStore adapts the (possibly-nil) *db.Store into the small key/value
// surface the updater package needs. Returning nil when the store failed to
// open lets the updater silently skip persistence rather than crashing.
func (a *App) metaStore() updater.Store {
	if a.store == nil {
		return nil
	}
	return metaStoreShim{a.store}
}

// metaStoreShim bridges db.Store.MetaGet/MetaSet to updater.Store. db.ErrNotFound
// collapses to ok=false so the updater doesn't need to know the db package's
// sentinel.
type metaStoreShim struct{ s *db.Store }

func (m metaStoreShim) Get(key string) (string, bool, error) {
	e, err := m.s.MetaGet(key)
	if errors.Is(err, db.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return e.Value, true, nil
}

func (m metaStoreShim) Set(key, value string) error {
	return m.s.MetaSet(key, value)
}

// shutdown is called by Wails when the app is closing. It cancels any in-flight
// downloads, then closes the library store opened in startup.
func (a *App) shutdown(ctx context.Context) {
	wruntime.LogInfo(ctx, "shutting down: canceling downloads and closing library database")
	if a.downloads != nil {
		a.downloads.Close()
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			wruntime.LogErrorf(ctx, "close library db: %v", err)
		}
	}
	defer func() {
		wruntime.LogInfo(ctx, "everything shut down")
	}()
}
