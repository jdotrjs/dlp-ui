package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// writeFile writes data to path, creating parent dirs, for test setup.
func writeFile(t *testing.T, path string, data []byte) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func TestDefault(t *testing.T) {
	d := Default()
	if d.OutputTemplate != "%(title)s-id-%(id)s.%(ext)s" {
		t.Errorf("OutputTemplate = %q", d.OutputTemplate)
	}
	if !d.RestrictFilenames {
		t.Error("RestrictFilenames should default to true")
	}
	if d.PlaylistMode != PlaylistModeDir {
		t.Errorf("PlaylistMode = %q, want %q", d.PlaylistMode, PlaylistModeDir)
	}
	if d.MaxConcurrent != 2 {
		t.Errorf("MaxConcurrent = %d, want 2", d.MaxConcurrent)
	}
	if !filepath.IsAbs(d.DownloadDir) {
		// Only meaningful when a home dir is resolvable; on CI it is.
		t.Logf("DownloadDir is not absolute: %q (home dir may be unset)", d.DownloadDir)
	}
}

func TestLoadFirstRunWritesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ytdlp-ui", "config.json")

	cfg, firstRun, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom first run: %v", err)
	}
	if !firstRun {
		t.Error("expected firstRun=true on missing config")
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("first-run config = %+v, want defaults %+v", cfg, Default())
	}

	// The file must now exist and reload to the same value, with firstRun=false.
	reloaded, firstRun2, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom second run: %v", err)
	}
	if firstRun2 {
		t.Error("expected firstRun=false on existing config")
	}
	if !reflect.DeepEqual(reloaded, cfg) {
		t.Errorf("reloaded = %+v, want %+v", reloaded, cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	want := Default()
	want.YtdlpPath = "/usr/local/bin/yt-dlp"
	want.DownloadDir = "/data/videos"
	want.MaxConcurrent = 5
	want.RestrictFilenames = false
	want.PlaylistMode = PlaylistModeCustom
	want.PlaylistTemplate = "%(playlist)s/%(title)s.%(ext)s"

	if err := saveTo(path, want); err != nil {
		t.Fatalf("saveTo: %v", err)
	}
	got, _, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestLoadOverlaysDefaultsForMissingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	// A partial config file: only downloadDir is set; everything else absent.
	partial := []byte(`{"downloadDir":"/custom/dir"}`)
	if err := writeFile(t, path, partial); err != nil {
		t.Fatal(err)
	}

	got, _, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got.DownloadDir != "/custom/dir" {
		t.Errorf("DownloadDir = %q, want /custom/dir", got.DownloadDir)
	}
	// Fields absent from the file must take their default values.
	if got.OutputTemplate != Default().OutputTemplate {
		t.Errorf("OutputTemplate = %q, want default", got.OutputTemplate)
	}
	if !got.RestrictFilenames {
		t.Error("RestrictFilenames should overlay to default true")
	}
	if got.MaxConcurrent != 2 {
		t.Errorf("MaxConcurrent = %d, want default 2", got.MaxConcurrent)
	}
}

func TestResolvedDBPath(t *testing.T) {
	// Empty DBPath derives from DownloadDir.
	c := Config{DownloadDir: "/data/videos"}
	want := filepath.Join("/data/videos", "library.db")
	if got := c.ResolvedDBPath(); got != want {
		t.Errorf("ResolvedDBPath() = %q, want %q", got, want)
	}

	// Explicit DBPath is used verbatim.
	c.DBPath = "/elsewhere/lib.db"
	if got := c.ResolvedDBPath(); got != "/elsewhere/lib.db" {
		t.Errorf("ResolvedDBPath() = %q, want /elsewhere/lib.db", got)
	}
}

func TestOutputArgsRestrictFilenames(t *testing.T) {
	c := Default()
	c.OutputTemplate = "%(title)s.%(ext)s"

	c.RestrictFilenames = true
	args := c.OutputArgs()
	if !slices.Contains(args, "--restrict-filenames") {
		t.Errorf("OutputArgs() = %v, want it to contain --restrict-filenames", args)
	}
	// -o and the template must be present and adjacent.
	oi := slices.Index(args, "-o")
	if oi < 0 || oi+1 >= len(args) || args[oi+1] != "%(title)s.%(ext)s" {
		t.Errorf("OutputArgs() = %v, want -o %q", args, "%(title)s.%(ext)s")
	}

	c.RestrictFilenames = false
	args = c.OutputArgs()
	if slices.Contains(args, "--restrict-filenames") {
		t.Errorf("OutputArgs() = %v, should NOT contain --restrict-filenames when disabled", args)
	}
}

// templateOf returns the value following -o in args, or "" if absent.
func templateOf(args []string) string {
	i := slices.Index(args, "-o")
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestDownloadArgs(t *testing.T) {
	base := "%(title)s.%(ext)s"

	t.Run("single video ignores playlist layout", func(t *testing.T) {
		c := Default()
		c.OutputTemplate = base
		c.PlaylistMode = PlaylistModeDir
		got := templateOf(c.DownloadArgs(false, "", ""))
		if got != base {
			t.Errorf("template = %q, want unchanged base %q", got, base)
		}
	})

	t.Run("playlist dir mode prefixes a subfolder", func(t *testing.T) {
		c := Default()
		c.OutputTemplate = base
		c.PlaylistMode = PlaylistModeDir
		got := templateOf(c.DownloadArgs(true, "", ""))
		if got != "%(playlist_title)s/"+base {
			t.Errorf("template = %q", got)
		}
	})

	t.Run("playlist name mode prefixes the filename", func(t *testing.T) {
		c := Default()
		c.OutputTemplate = base
		c.PlaylistMode = PlaylistModeName
		got := templateOf(c.DownloadArgs(true, "", ""))
		if got != "%(playlist_title)s - "+base {
			t.Errorf("template = %q", got)
		}
	})

	t.Run("playlist custom mode uses PlaylistTemplate verbatim", func(t *testing.T) {
		c := Default()
		c.OutputTemplate = base
		c.PlaylistMode = PlaylistModeCustom
		c.PlaylistTemplate = "%(playlist)s/%(playlist_index)s-%(title)s.%(ext)s"
		got := templateOf(c.DownloadArgs(true, "", ""))
		if got != c.PlaylistTemplate {
			t.Errorf("template = %q, want custom template", got)
		}
	})

	t.Run("template override wins over config", func(t *testing.T) {
		c := Default()
		c.OutputTemplate = base
		got := templateOf(c.DownloadArgs(false, "%(id)s.%(ext)s", ""))
		if got != "%(id)s.%(ext)s" {
			t.Errorf("template = %q, want override", got)
		}
	})

	t.Run("mode override wins over config", func(t *testing.T) {
		c := Default()
		c.OutputTemplate = base
		c.PlaylistMode = PlaylistModeDir
		got := templateOf(c.DownloadArgs(true, "", PlaylistModeName))
		if got != "%(playlist_title)s - "+base {
			t.Errorf("template = %q, want name-mode override", got)
		}
	})

	t.Run("restrict-filenames carried through", func(t *testing.T) {
		c := Default()
		c.OutputTemplate = base
		c.RestrictFilenames = true
		if !slices.Contains(c.DownloadArgs(false, "", ""), "--restrict-filenames") {
			t.Error("expected --restrict-filenames")
		}
		c.RestrictFilenames = false
		if slices.Contains(c.DownloadArgs(false, "", ""), "--restrict-filenames") {
			t.Error("did not expect --restrict-filenames when disabled")
		}
	})
}
