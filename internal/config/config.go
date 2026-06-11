// Package config owns the application's persisted configuration: the on-disk
// config.json (load/save with forward-compatible defaults), derivation rules
// for paths that are left blank, and the yt-dlp argument helpers that are a
// direct function of config values (e.g. --restrict-filenames / the output
// template).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ytdlp-gui/internal/paths"
)

// PlaylistMode controls how playlist downloads are laid out on disk.
type PlaylistMode string

const (
	// PlaylistModeName prefixes the playlist name onto each item.
	PlaylistModeName PlaylistMode = "name"
	// PlaylistModeDir places playlist items in a per-playlist directory.
	PlaylistModeDir PlaylistMode = "dir"
	// PlaylistModeCustom uses the user-supplied PlaylistTemplate verbatim.
	PlaylistModeCustom PlaylistMode = "custom"
)

// Config is the full persisted application configuration. JSON tags are
// camelCase because the same shape is surfaced to the Preact frontend via the
// Wails bindings.
type Config struct {
	// Binary locations. Empty means "resolve via PATH".
	YtdlpPath  string `json:"ytdlpPath"`
	FfmpegPath string `json:"ffmpegPath"`
	DenoPath   string `json:"denoPath"`

	// DownloadDir is where finished downloads land.
	DownloadDir string `json:"downloadDir"`
	// DBPath is the library database location. Empty means "derive from
	// DownloadDir" (see ResolvedDBPath); we persist it empty until the user
	// explicitly overrides so the DB tracks the download dir.
	DBPath string `json:"dbPath"`

	// OutputTemplate is the yt-dlp -o template for a single download.
	OutputTemplate string `json:"outputTemplate"`
	// RestrictFilenames, when true, passes --restrict-filenames so output
	// names are ASCII-only with spaces collapsed to underscores.
	RestrictFilenames bool `json:"restrictFilenames"`

	// PlaylistMode + PlaylistTemplate control playlist layout.
	PlaylistMode     PlaylistMode `json:"playlistMode"`
	PlaylistTemplate string       `json:"playlistTemplate"`

	// TODO[feat]: PlaylistMaxItmes was intended to limit fetching huge
	// playlists but there doesn't seem to be a flag for that; reconsider
	// how/if we want to support.
	// PlaylistMaxItems *int `json:"playlistMaxItems,omitempty"`

	// MaxConcurrent caps simultaneous downloads.
	MaxConcurrent int `json:"maxConcurrent"`

	// SkipYtdlpVersionCache forces ResolveBinaries to actually run
	// `yt-dlp --version` every time, instead of returning the cached version
	// associated with the binary's file hash. Set this when yt-dlp is invoked
	// through a wrapper (shell script, pyenv shim, etc.) whose file hash does
	// not change even though the underlying tool may have been upgraded.
	SkipYtdlpVersionCache bool `json:"skipYtdlpVersionCache"`
}

// Default returns the configuration used on first run (and as the overlay base
// for an existing config file, so newly-added fields get sane values).
func Default() Config {
	return Config{
		YtdlpPath:             "",
		FfmpegPath:            "",
		DenoPath:              "",
		DownloadDir:           defaultDownloadDir(),
		DBPath:                "",
		OutputTemplate:        "%(title)s-id-%(id)s.%(ext)s",
		RestrictFilenames:     true,
		PlaylistMode:          PlaylistModeDir,
		PlaylistTemplate:      "",
		MaxConcurrent:         2,
		SkipYtdlpVersionCache: false,
	}
}

// defaultDownloadDir is ~/Downloads/ytdlp-ui, falling back to a relative path
// if the home directory cannot be determined (extremely unusual).
func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join("Downloads", "ytdlp-ui")
	}
	return filepath.Join(home, "Downloads", "ytdlp-ui")
}

// Path returns the absolute path to config.json. It is a thin re-export of
// paths.ConfigPath so existing callers keep working.
func Path() (string, error) {
	return paths.ConfigPath()
}

// Load reads config.json. On first run (file missing) it writes the defaults
// and returns them with firstRun=true. Otherwise it overlays the file's fields
// onto Default() so that fields added in newer versions are populated. Returns
// the loaded config, the path it was read from, and whether this was a
// first-run write.
func Load() (Config, string, bool, error) {
	path, err := Path()
	if err != nil {
		return Config{}, "", false, err
	}
	cfg, firstRun, err := loadFrom(path)
	return cfg, path, firstRun, err
}

// loadFrom is the path-injectable core of Load, used by tests. The second
// return value reports whether the on-disk config was created during this
// call (i.e. first run).
func loadFrom(path string) (Config, bool, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// First run: persist defaults so subsequent edits have a file to
		// overlay onto.
		if werr := saveTo(path, cfg); werr != nil {
			return cfg, true, werr
		}
		return cfg, true, nil
	}
	if err != nil {
		return cfg, false, fmt.Errorf("read config %q: %w", path, err)
	}

	// Overlay the file onto the defaults: only keys present in the file
	// override defaults, so new fields keep their default value.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, false, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, false, nil
}

// Save atomically persists the config to config.json (temp write + rename).
func (c Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return saveTo(path, c)
}

// saveTo is the path-injectable core of Save. It writes to a temp file in the
// destination directory and renames into place so a crash mid-write can never
// leave a truncated config.json.
func saveTo(path string, c Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %q: %w", dir, err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp config into place: %w", err)
	}
	return nil
}

// ResolvedDBPath returns DBPath if explicitly set, otherwise the library DB
// derived from the download directory (DownloadDir/library.db).
func (c Config) ResolvedDBPath() string {
	if c.DBPath != "" {
		return c.DBPath
	}
	return filepath.Join(c.DownloadDir, "library.db")
}

// OutputArgs returns the yt-dlp argument fragment that is purely a function of
// the output-naming config: the -o template and, when RestrictFilenames is
// set, --restrict-filenames. Config owns this because it owns the fields that
// drive it. Callers append these to the rest of the yt-dlp command.
func (c Config) OutputArgs() []string {
	args := []string{}
	if c.OutputTemplate != "" {
		args = append(args, "-o", c.OutputTemplate)
	}
	if c.RestrictFilenames {
		args = append(args, "--restrict-filenames")
	}
	return args
}

// DownloadArgs returns the resolved -o / --restrict-filenames fragment for one
// download, applying the playlist output layout when isPlaylist is true. Config
// owns this because it owns the OutputTemplate / PlaylistMode / PlaylistTemplate
// fields. Per-request overrides (from the bound DownloadRequest) take
// precedence over config:
//
//   - templateOverride, when non-empty, replaces the base output template.
//   - modeOverride, when non-empty, replaces PlaylistMode for this download.
//
// Playlist layout (applied only when isPlaylist):
//
//   - "dir":    prefix "%(playlist_title)s/" onto the template (a subfolder per
//     playlist).
//   - "name":   prefix "%(playlist_title)s - " onto the filename.
//   - "custom": use PlaylistTemplate verbatim (ignoring the base template) when
//     it is set; otherwise fall back to the base template unchanged.
//
// A single (non-playlist) download ignores the playlist layout entirely.
func (c Config) DownloadArgs(isPlaylist bool, templateOverride string, modeOverride PlaylistMode) []string {
	template := c.OutputTemplate
	if templateOverride != "" {
		template = templateOverride
	}

	if isPlaylist {
		mode := c.PlaylistMode
		if modeOverride != "" {
			mode = modeOverride
		}
		switch mode {
		case PlaylistModeDir:
			template = "%(playlist_title)s/" + template
		case PlaylistModeName:
			template = "%(playlist_title)s - " + template
		case PlaylistModeCustom:
			if c.PlaylistTemplate != "" {
				template = c.PlaylistTemplate
			}
		}
	}

	args := []string{}
	if template != "" {
		args = append(args, "-o", template)
	}
	if c.RestrictFilenames {
		args = append(args, "--restrict-filenames")
	}
	return args
}
