package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"
	"ytdlp-gui/internal/binaries"
	"ytdlp-gui/internal/config"
	"ytdlp-gui/internal/db"
	"ytdlp-gui/internal/download"
	"ytdlp-gui/internal/install"
	"ytdlp-gui/internal/paths"
	"ytdlp-gui/internal/updater"
	"ytdlp-gui/internal/ytdlp"

	"github.com/pkg/browser"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// SetupEvent is the Wails event channel used by InstallDependency to stream
// per-binary install progress to the welcome wizard.
const SetupEvent = "setup-progress"

// UpdateStatusEvent is emitted whenever the cached update status changes —
// after the startup auto-check or a manual CheckForUpdate. The frontend
// subscribes to refresh the Settings nav badge without re-polling.
const UpdateStatusEvent = "update-status"

// updateCheckInterval is the staleness threshold for the startup auto-check:
// if the meta-stored last-check timestamp is older than this, we hit GitHub
// again.
const updateCheckInterval = 7 * 24 * time.Hour

// GetConfig returns the current configuration for the Settings view.
func (a *App) GetConfig() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

// IsFirstRun reports whether startup created config.json on disk this session
// (i.e. no config existed before). The frontend uses this to gate the welcome
// wizard. Once the wizard finishes, the in-memory flag stays the same — but
// the next app launch will see firstRun=false because the file now exists.
func (a *App) IsFirstRun() bool {
	return a.firstRun
}

// ConfigPath returns the absolute path to config.json (shown on the welcome
// wizard's final screen so the user knows where to look later).
func (a *App) ConfigPath() (string, error) {
	return paths.ConfigPath()
}

// VendorDir returns the absolute path to the vendor directory used by
// InstallDependency for auto-installed binaries (shown on the welcome wizard
// so the user can see where downloads will land).
func (a *App) VendorDir() (string, error) {
	return paths.VendorDir()
}

// InstallDependency downloads the given binary into the vendor directory,
// streaming progress on the SetupEvent channel, and returns the final path on
// success. Supported names: "yt-dlp", "ffmpeg", "deno". The frontend is
// expected to update Config.{YtdlpPath,FfmpegPath,NodePath} with the returned
// path and SaveConfig at the end of the wizard. (We don't auto-mutate config
// here so the user keeps a single explicit save at the end of setup.)
func (a *App) InstallDependency(name string) (string, error) {
	return install.Install(a.ctx, name, func(p install.Progress) {
		wruntime.EventsEmit(a.ctx, SetupEvent, p)
	})
}

// SaveConfig persists the supplied configuration (including binary paths) and
// updates the in-memory copy. The Settings view re-runs ResolveBinaries after
// this to refresh the doctor status.
func (a *App) SaveConfig(cfg config.Config) error {
	if err := cfg.Save(); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	return nil
}

// ResolveBinaries runs the doctor against the currently configured binary
// paths, returning per-binary status (resolution + --version).
func (a *App) ResolveBinaries() []binaries.BinaryStatus {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()

	return binaries.Doctor(a.versionRunner,
		binaries.Spec{Name: "yt-dlp", Explicit: cfg.YtdlpPath},
		binaries.Spec{Name: "ffmpeg", Explicit: cfg.FfmpegPath, VersionFlag: "-version"},
		binaries.Spec{Name: "deno", Explicit: cfg.DenoPath},
	)
}

// ytdlpOptions resolves the external-binary paths from the current config into
// the ytdlp.Options the wrapper needs. yt-dlp is required: if it can't be
// resolved (explicit path invalid AND not on PATH), it returns errYtdlpMissing
// so the caller can hard-fail. ffmpeg and node are optional (soft) — when they
// resolve, their paths are threaded through (ffmpeg via --ffmpeg-location, node
// via child-PATH injection); when they don't, they're simply omitted and yt-dlp
// falls back to PATH / shells out as it can.
func (a *App) ytdlpOptions() (ytdlp.Options, error) {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()

	ytRes, err := binaries.Resolve("yt-dlp", cfg.YtdlpPath)
	if err != nil {
		return ytdlp.Options{}, errYtdlpMissing
	}

	opts := ytdlp.Options{YtdlpPath: ytRes.Path}
	// Optional binaries: include them only when they resolve. A failure is not
	// fatal (soft-warn) — yt-dlp surfaces its own stderr if it actually needs
	// them and they're absent.
	if res, rerr := binaries.Resolve("ffmpeg", cfg.FfmpegPath); rerr == nil {
		opts.FfmpegPath = res.Path
	}
	if res, rerr := binaries.Resolve("deno", cfg.DenoPath); rerr == nil {
		opts.JSRuntimePath = ytdlp.JSRuntimePath{Deno: res.Path}
	}
	return opts, nil
}

// FetchMetadata runs a blocking, metadata-only yt-dlp fetch for the given URL
// and returns a Preview (single video → formats; playlist/channel → flat
// entries). The frontend shows a spinner while this runs. It hard-fails with a
// clear error when yt-dlp is missing (UI: "open Settings"); on a yt-dlp
// non-zero exit the returned error includes yt-dlp's stderr.
func (a *App) FetchMetadata(url string) (ytdlp.Preview, error) {
	opts, err := a.ytdlpOptions()
	if err != nil {
		return ytdlp.Preview{}, err
	}
	return ytdlp.Metadata(a.ctx, ytdlp.ExecRunner{}, opts, url)
}

// StartDownload begins a download for the request the fetch preview modal
// assembled and returns a generated download id immediately; all live progress
// arrives as events on the `download` channel (correlated by that id). It
// resolves the binary paths first and hard-fails with a clear error when yt-dlp
// is missing (UI: "open Settings"). The current config is snapshotted now so
// the in-flight download uses the settings in effect at start time.
func (a *App) StartDownload(req download.DownloadRequest) (string, error) {
	if a.downloads == nil {
		return "", errors.New("download manager is unavailable")
	}
	opts, err := a.ytdlpOptions()
	if err != nil {
		return "", err
	}
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	return a.downloads.Start(opts, cfg, req)
}

// CancelDownload cancels an in-flight download by id (killing the yt-dlp
// child). It errors if no such download is active. A `cancelled` event follows
// on the `download` channel.
func (a *App) CancelDownload(downloadID string) error {
	if a.downloads == nil {
		return errors.New("download manager is unavailable")
	}
	return a.downloads.Cancel(downloadID)
}

// PickDirectory opens a native directory picker and returns the chosen path
// (empty string if the user cancels).
func (a *App) PickDirectory(title string) (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:                title,
		CanCreateDirectories: true,
	})
}

// PickFile opens a native file picker. patterns is a semicolon-separated glob
// list (e.g. "*"; or "yt-dlp;yt-dlp.exe"); empty means any file. Returns the
// chosen path (empty string if the user cancels).
func (a *App) PickFile(title string, patterns string) (string, error) {
	homeDir, _ := os.UserHomeDir()
	opts := wruntime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: homeDir,
		Filters:          []wruntime.FileFilter{},
	}

	if wruntime.Environment(context.TODO()).Platform != "darwin" {
		if patterns != "" {
			opts.Filters = []wruntime.FileFilter{{
				DisplayName: title,
				Pattern:     patterns,
			}}
		}
	}
	return wruntime.OpenFileDialog(a.ctx, opts)
}

// ---- Library ----

// ListContent returns a sorted, filtered, paginated page of library content
// plus the total matching count (for the pager). Sorting and paging are done
// server-side in the store; fuzzy search runs client-side over the returned
// page.
func (a *App) ListContent(opts db.ListOptions) (db.ListResult, error) {
	if a.store == nil {
		return db.ListResult{}, errStoreUnavailable
	}
	return a.store.ListContent(opts)
}

// GetContent returns a single content row by id.
func (a *App) GetContent(id int64) (db.Content, error) {
	if a.store == nil {
		return db.Content{}, errStoreUnavailable
	}
	return a.store.GetContent(id)
}

// DeleteContent removes a content row, and when deleteFile is true also makes a
// best-effort attempt to delete the underlying file from disk.
func (a *App) DeleteContent(id int64, deleteFile bool) error {
	if a.store == nil {
		return errStoreUnavailable
	}
	return a.store.DeleteContent(id, deleteFile)
}

// OpenFile opens the content's downloaded file with the OS default handler.
func (a *App) OpenFile(id int64) error {
	if a.store == nil {
		return errStoreUnavailable
	}
	c, err := a.store.GetContent(id)
	if err != nil {
		return err
	}
	if c.Filepath == "" {
		return fmt.Errorf("content %d has no file on disk", id)
	}
	return browser.OpenFile(c.Filepath)
}

// OpenInFolder reveals the content's file in the OS file manager (selecting it
// where the platform supports it), falling back to opening the containing
// directory.
func (a *App) OpenInFolder(id int64) error {
	if a.store == nil {
		return errStoreUnavailable
	}
	c, err := a.store.GetContent(id)
	if err != nil {
		return err
	}
	if c.Filepath == "" {
		return fmt.Errorf("content %d has no file on disk", id)
	}
	return revealInFolder(c.Filepath)
}

// ---- Update check ----

// GetAppVersion returns the running app's version string (the package-level
// Version var, overridable at build time via -ldflags).
func (a *App) GetAppVersion() string {
	return fmt.Sprintf("%s (%s)", VersionName, Version)
}

// GetUpdateStatus returns the cached update status without hitting the
// network. The updater package handles the meta-store reads and derives
// UpdateAvailable fresh against the running Version.
func (a *App) GetUpdateStatus() updater.Info {
	return updater.Load(Version, a.metaStore())
}

// CheckForUpdate hits GitHub now, persists the result via the meta store,
// emits an update-status event, and returns the fresh Info. Network/parse
// errors are returned as-is so the Settings UI can show them.
func (a *App) CheckForUpdate() (updater.Info, error) {
	info, err := updater.Check(a.ctx, Version, a.metaStore())
	if err != nil {
		return updater.Info{}, err
	}
	wruntime.EventsEmit(a.ctx, UpdateStatusEvent, info)
	return info, nil
}

// GetThumbnailDataURL returns a `data:<mime>;base64,<bytes>` URL for the
// content's saved thumbnail, or "" when no thumbnail has been fetched yet.
// Packaging it as a data URL means the frontend can drop the string straight
// into <img src> without an extra fetch or per-byte plumbing — and a missing
// thumbnail is a normal (not error) result the row UI can branch on.
func (a *App) GetThumbnailDataURL(id int64) (string, error) {
	if a.store == nil {
		return "", errStoreUnavailable
	}
	t, err := a.store.GetThumbnail(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	mime := t.Mime
	if mime == "" {
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(t.Data), nil
}
