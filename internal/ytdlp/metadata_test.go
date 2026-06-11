package ytdlp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner returns canned stdout/stderr/err and records the cmd it was given
// (so the command/env building can be asserted).
type fakeRunner struct {
	stdout []byte
	stderr []byte
	err    error
	got    *exec.Cmd
}

func (f *fakeRunner) Run(cmd *exec.Cmd) ([]byte, []byte, error) {
	f.got = cmd
	return f.stdout, f.stderr, f.err
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestMetadataSingleVideo(t *testing.T) {
	url := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	runner := &fakeRunner{stdout: readFixture(t, "single.json")}

	p, err := Metadata(context.Background(), runner, Options{YtdlpPath: "yt-dlp"}, url)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	if p.IsPlaylist {
		t.Errorf("IsPlaylist = true, want false for a single video")
	}
	if p.URL != url {
		t.Errorf("URL = %q, want %q", p.URL, url)
	}
	if p.Source != "youtube" {
		t.Errorf("Source = %q, want %q (lowercased extractor_key)", p.Source, "youtube")
	}
	if p.Title != "Example Single Video" {
		t.Errorf("Title = %q", p.Title)
	}
	if p.Uploader != "Example Channel" {
		t.Errorf("Uploader = %q", p.Uploader)
	}
	if p.Duration != 241 {
		t.Errorf("Duration = %d, want 241", p.Duration)
	}
	if p.Thumbnail != "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxres.jpg" {
		t.Errorf("Thumbnail = %q", p.Thumbnail)
	}
	if len(p.Entries) != 0 {
		t.Errorf("Entries = %d, want 0 for single video", len(p.Entries))
	}
	if len(p.Formats) != 3 {
		t.Fatalf("Formats = %d, want 3", len(p.Formats))
	}

	// Spot-check format mapping, including the width/height -> resolution and
	// filesize_approx -> filesize fallbacks.
	video := p.Formats[1]
	if video.FormatID != "137" || video.Ext != "mp4" {
		t.Errorf("format[1] id/ext = %q/%q", video.FormatID, video.Ext)
	}
	if video.Resolution != "1920x1080" {
		t.Errorf("format[1] resolution = %q, want 1920x1080 (from width/height)", video.Resolution)
	}
	if video.FPS != 30 {
		t.Errorf("format[1] fps = %d, want 30", video.FPS)
	}
	if video.Filesize != 56000000 {
		t.Errorf("format[1] filesize = %d, want 56000000 (from filesize_approx)", video.Filesize)
	}

	audio := p.Formats[0]
	if audio.Resolution != "audio only" || audio.VCodec != "none" {
		t.Errorf("format[0] = %+v, want audio-only", audio)
	}
}

func TestMetadataPlaylist(t *testing.T) {
	url := "https://www.youtube.com/playlist?list=PLexampleplaylistid"
	runner := &fakeRunner{stdout: readFixture(t, "playlist.json")}

	p, err := Metadata(context.Background(), runner, Options{YtdlpPath: "yt-dlp"}, url)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	if !p.IsPlaylist {
		t.Errorf("IsPlaylist = false, want true")
	}
	if p.Source != "youtube" {
		t.Errorf("Source = %q, want %q (youtube:tab -> youtube)", p.Source, "youtube")
	}
	if p.PlaylistID != "PLexampleplaylistid" {
		t.Errorf("PlaylistID = %q", p.PlaylistID)
	}
	if p.Title != "Example Playlist" {
		t.Errorf("Title = %q", p.Title)
	}
	if p.Count != 3 || len(p.Entries) != 3 {
		t.Fatalf("Count/Entries = %d/%d, want 3/3", p.Count, len(p.Entries))
	}
	if len(p.Formats) != 0 {
		t.Errorf("Formats = %d, want 0 for a flat playlist", len(p.Formats))
	}

	e1 := p.Entries[0]
	if e1.SourceID != "vid000000001" || e1.Title != "Item one" {
		t.Errorf("entry[0] = %+v", e1)
	}
	if e1.Index != 1 {
		t.Errorf("entry[0].Index = %d, want 1 (1-based)", e1.Index)
	}
	if e1.Duration != 241 {
		t.Errorf("entry[0].Duration = %d, want 241", e1.Duration)
	}
	if e1.Source != "youtube" {
		t.Errorf("entry[0].Source = %q, want youtube (from ie_key)", e1.Source)
	}
	if e1.URL != "https://www.youtube.com/watch?v=vid000000001" {
		t.Errorf("entry[0].URL = %q", e1.URL)
	}
	if p.Entries[2].Index != 3 {
		t.Errorf("entry[2].Index = %d, want 3", p.Entries[2].Index)
	}
}

func TestMetadataYtdlpError(t *testing.T) {
	runner := &fakeRunner{
		err:    errors.New("exit status 1"),
		stderr: []byte("ERROR: Unsupported URL: https://example.com/nope"),
	}
	_, err := Metadata(context.Background(), runner, Options{YtdlpPath: "yt-dlp"}, "https://example.com/nope")
	if err == nil {
		t.Fatal("expected an error on non-zero yt-dlp exit")
	}
	// The stderr must be surfaced so the UI can show it.
	if !strings.Contains(err.Error(), "Unsupported URL") {
		t.Errorf("error %q does not include yt-dlp stderr", err)
	}
}

// TestMetadataBuildsCommand asserts the invocation: the resolved yt-dlp path,
// the -J --flat-playlist <url> args, the --ffmpeg-location flag, and node's dir
// prepended to the child PATH.
func TestMetadataBuildsCommand(t *testing.T) {
	url := "https://example.com/v"
	node := filepath.Join("/opt", "node", "bin", "node")
	nodeDir := filepath.Dir(node)
	runner := &fakeRunner{stdout: readFixture(t, "single.json")}

	opts := Options{
		YtdlpPath:     "/usr/local/bin/yt-dlp",
		FfmpegPath:    "/opt/ffmpeg/ffmpeg",
		JSRuntimePath: node,
	}
	if _, err := Metadata(context.Background(), runner, opts, url); err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	cmd := runner.got
	if cmd == nil {
		t.Fatal("runner did not receive a command")
	}
	if cmd.Path != opts.YtdlpPath && filepath.Base(cmd.Path) != "yt-dlp" {
		t.Errorf("cmd.Path = %q, want the resolved yt-dlp path", cmd.Path)
	}

	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"-J", "--flat-playlist", url, "--ffmpeg-location", opts.FfmpegPath} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}

	// node dir must lead the child PATH.
	var pathVal string
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "PATH=") {
			pathVal = strings.TrimPrefix(kv, "PATH=")
		}
	}
	if !strings.HasPrefix(pathVal, nodeDir) {
		t.Errorf("child PATH = %q, want it to start with node dir %q", pathVal, nodeDir)
	}
}
