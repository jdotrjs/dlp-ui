package binaries

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// writeExecutable creates a runnable file (mode 0755) for resolve tests.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveExplicitPath(t *testing.T) {
	dir := t.TempDir()
	bin := writeExecutable(t, dir, "yt-dlp")

	res, err := Resolve("yt-dlp", bin)
	if err != nil {
		t.Fatalf("Resolve explicit: %v", err)
	}
	if res.Path != bin {
		t.Errorf("Path = %q, want %q", res.Path, bin)
	}
	if res.Source != SourceConfig {
		t.Errorf("Source = %q, want %q", res.Source, SourceConfig)
	}
}

func TestResolveExplicitMissing(t *testing.T) {
	_, err := Resolve("yt-dlp", filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound", err)
	}
}

func TestResolveExplicitNotExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bit not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "plainfile")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve("yt-dlp", path)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound (non-executable)", err)
	}
}

func TestResolveExplicitDirectory(t *testing.T) {
	_, err := Resolve("yt-dlp", t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound (directory)", err)
	}
}

func TestResolveFallsBackToPATH(t *testing.T) {
	// Explicit empty => LookPath on a binary we place on a temp PATH.
	dir := t.TempDir()
	name := "ytdlp_fake_tool"
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	bin := writeExecutable(t, dir, name)
	_ = bin

	t.Setenv("PATH", dir)

	lookName := "ytdlp_fake_tool"
	if runtime.GOOS == "windows" {
		lookName = name
	}
	res, err := Resolve(lookName, "")
	if err != nil {
		t.Fatalf("Resolve via PATH: %v", err)
	}
	if res.Source != SourcePATH {
		t.Errorf("Source = %q, want %q", res.Source, SourcePATH)
	}
}

func TestResolvePATHNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Resolve("definitely-not-a-real-binary-xyz", "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound", err)
	}
}

func TestResolveExplicitTakesPriorityOverPATH(t *testing.T) {
	pathDir := t.TempDir()
	writeExecutable(t, pathDir, "yt-dlp")
	t.Setenv("PATH", pathDir)

	explicitDir := t.TempDir()
	explicit := writeExecutable(t, explicitDir, "yt-dlp")

	res, err := Resolve("yt-dlp", explicit)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != SourceConfig || res.Path != explicit {
		t.Errorf("got %+v, want explicit config path %q", res, explicit)
	}
}

// fakeRunner is an injectable VersionRunner keyed by path.
type fakeRunner struct {
	versions map[string]string
	errs     map[string]error
}

func (f fakeRunner) Version(path string) (string, error) {
	if err := f.errs[path]; err != nil {
		return "", err
	}
	return f.versions[path], nil
}

func TestDoctor(t *testing.T) {
	dir := t.TempDir()
	ytdlp := writeExecutable(t, dir, "yt-dlp")
	ffmpeg := writeExecutable(t, dir, "ffmpeg")

	runner := fakeRunner{
		versions: map[string]string{
			ytdlp:  "2024.01.01",
			ffmpeg: "ffmpeg version 6.0",
		},
	}

	statuses := Doctor(runner,
		Spec{Name: "yt-dlp", Explicit: ytdlp},
		Spec{Name: "ffmpeg", Explicit: ffmpeg},
		Spec{Name: "node", Explicit: filepath.Join(dir, "missing-node")},
	)
	if len(statuses) != 3 {
		t.Fatalf("got %d statuses, want 3", len(statuses))
	}

	if !statuses[0].OK || statuses[0].Version != "2024.01.01" || statuses[0].Source != SourceConfig {
		t.Errorf("yt-dlp status = %+v", statuses[0])
	}
	if !statuses[1].OK || statuses[1].Version != "ffmpeg version 6.0" {
		t.Errorf("ffmpeg status = %+v", statuses[1])
	}
	// node is missing: resolution fails before the runner is consulted.
	if statuses[2].OK || statuses[2].Error == "" || statuses[2].Source != SourceUnresolved {
		t.Errorf("node status = %+v, want unresolved failure", statuses[2])
	}
}

func TestDoctorVersionFailure(t *testing.T) {
	dir := t.TempDir()
	bin := writeExecutable(t, dir, "yt-dlp")
	runner := fakeRunner{errs: map[string]error{bin: errors.New("boom")}}

	statuses := Doctor(runner, Spec{Name: "yt-dlp", Explicit: bin})
	st := statuses[0]
	// Resolution succeeded but version run failed: resolved path is known,
	// OK is false, error captured.
	if st.OK || st.Resolved != bin || !strings.Contains(st.Error, "boom") {
		t.Errorf("status = %+v, want resolved-but-version-failed", st)
	}
}

func TestFfmpegLocationArgs(t *testing.T) {
	if got := FfmpegLocationArgs(""); got != nil {
		t.Errorf("empty ffmpegPath = %v, want nil", got)
	}
	got := FfmpegLocationArgs("/opt/ffmpeg/bin/ffmpeg")
	want := []string{"--ffmpeg-location", "/opt/ffmpeg/bin/ffmpeg"}
	if !slices.Equal(got, want) {
		t.Errorf("FfmpegLocationArgs = %v, want %v", got, want)
	}
}

func TestNodePathEnv(t *testing.T) {
	sep := string(os.PathListSeparator)

	// Empty nodePath: env unchanged.
	base := []string{"PATH=/usr/bin", "HOME=/home/x"}
	if got := NodePathEnv(base, ""); !slices.Equal(got, base) {
		t.Errorf("empty nodePath changed env: %v", got)
	}

	// node dir prepended to existing PATH.
	node := filepath.Join("/opt", "node", "bin", "node")
	nodeDir := filepath.Dir(node) // /opt/node/bin
	got := NodePathEnv([]string{"PATH=/usr/bin" + sep + "/bin", "HOME=/home/x"}, node)

	var pathVal string
	var sawHome bool
	for _, kv := range got {
		if strings.HasPrefix(kv, "PATH=") {
			pathVal = strings.TrimPrefix(kv, "PATH=")
		}
		if kv == "HOME=/home/x" {
			sawHome = true
		}
	}
	if !sawHome {
		t.Error("HOME entry lost")
	}
	wantPath := nodeDir + sep + "/usr/bin" + sep + "/bin"
	if pathVal != wantPath {
		t.Errorf("PATH = %q, want %q", pathVal, wantPath)
	}

	// No PATH present in base: one is created.
	got = NodePathEnv([]string{"HOME=/home/x"}, node)
	if !slices.Contains(got, "PATH="+nodeDir) {
		t.Errorf("env = %v, want a PATH=%s entry", got, nodeDir)
	}
}
