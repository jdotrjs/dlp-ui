// Package install fetches third-party binaries (yt-dlp, ffmpeg, deno) into
// the app's vendor directory on first run. Each binary has a per-OS/per-arch
// asset descriptor (URL + archive format + entry inside the archive); Install
// downloads it, extracts the chosen entry to the final vendor path, marks it
// executable, and returns that path. Progress is reported via a callback so
// the bound method can adapt it to a Wails event.
//
// This package is best-effort: a failed install is surfaced to the caller as
// an error and the user can still set the binary path manually in Settings.
package install

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ytdlp-gui/internal/paths"
)

// ProgressPhase is the coarse stage label sent on each progress callback.
type ProgressPhase string

const (
	PhaseStart      ProgressPhase = "start"
	PhaseDownload   ProgressPhase = "download"
	PhaseExtract    ProgressPhase = "extract"
	PhaseInstall    ProgressPhase = "install"
	PhaseDone       ProgressPhase = "done"
	PhaseError      ProgressPhase = "error"
	PhaseUnsupported ProgressPhase = "unsupported"
)

// Progress is one callback payload during an install. percent is 0..100 and
// only meaningful during PhaseDownload; everything else carries it as a hint
// (often 0 or 100). message is a short human-readable label suitable for UI.
type Progress struct {
	Name    string        `json:"name"`
	Phase   ProgressPhase `json:"phase"`
	Percent int           `json:"percent"`
	Message string        `json:"message"`
	Path    string        `json:"path"`
	Error   string        `json:"error"`
}

// ProgressFunc receives Progress events. It is called from the goroutine that
// runs Install; implementations should not block.
type ProgressFunc func(Progress)

// archiveKind is the supported extraction format for an asset.
type archiveKind string

const (
	archiveNone archiveKind = ""    // raw single-file download
	archiveZip  archiveKind = "zip"
)

// asset describes the remote artifact for one (name, OS, arch) combination.
type asset struct {
	URL string
	// Kind is the archive format. If archiveNone, the URL points at the raw
	// binary; we just save it to the final destination.
	Kind archiveKind
	// Entry is the filename to extract from the archive (matched against the
	// basename of each entry). Required for archived formats.
	Entry string
}

// ErrUnsupportedPlatform is returned when no asset is known for the running
// OS/arch combination. The caller should report this so the user can install
// the binary manually.
var ErrUnsupportedPlatform = errors.New("no auto-install asset for this OS/arch")

// httpClient is overridable for tests; production uses a long-timeout client.
var httpClient = &http.Client{Timeout: 10 * time.Minute}

// SupportedNames is the set of binaries this package can install.
var SupportedNames = []string{"yt-dlp", "ffmpeg", "deno"}

// Install downloads + extracts the named binary into the vendor directory,
// reporting progress via fn (if non-nil) and returning the final binary path.
// A successful install leaves the file executable; a failure leaves the
// vendor dir possibly containing the partial download (cleaned up best-effort).
func Install(ctx context.Context, name string, fn ProgressFunc) (string, error) {
	if fn == nil {
		fn = func(Progress) {}
	}
	emit := func(p Progress) { p.Name = name; fn(p) }

	a, err := assetFor(name, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		emit(Progress{Phase: PhaseUnsupported, Message: err.Error(), Error: err.Error()})
		return "", err
	}

	dest, err := paths.VendorBinPath(name)
	if err != nil {
		emit(Progress{Phase: PhaseError, Error: err.Error()})
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		emit(Progress{Phase: PhaseError, Error: err.Error()})
		return "", fmt.Errorf("create vendor dir: %w", err)
	}

	emit(Progress{Phase: PhaseStart, Message: "Fetching " + a.URL})

	// Download into a sibling tempfile so a crash mid-write never leaves a
	// half-written binary at dest.
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".download-*")
	if err != nil {
		emit(Progress{Phase: PhaseError, Error: err.Error()})
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup of the tempfile on any failure path.
	cleanupTmp := true
	defer func() {
		tmp.Close()
		if cleanupTmp {
			os.Remove(tmpPath)
		}
	}()

	if err := download(ctx, a.URL, tmp, func(pct int) {
		emit(Progress{Phase: PhaseDownload, Percent: pct, Message: "Downloading…"})
	}); err != nil {
		emit(Progress{Phase: PhaseError, Error: err.Error()})
		return "", err
	}
	if err := tmp.Close(); err != nil {
		emit(Progress{Phase: PhaseError, Error: err.Error()})
		return "", fmt.Errorf("close download: %w", err)
	}

	emit(Progress{Phase: PhaseExtract, Percent: 100, Message: "Extracting…"})

	if a.Kind == archiveNone {
		// Raw binary: just rename into place.
		if err := os.Rename(tmpPath, dest); err != nil {
			emit(Progress{Phase: PhaseError, Error: err.Error()})
			return "", fmt.Errorf("install %s: %w", name, err)
		}
		cleanupTmp = false
	} else {
		if err := extractEntry(tmpPath, a.Kind, a.Entry, dest); err != nil {
			emit(Progress{Phase: PhaseError, Error: err.Error()})
			return "", err
		}
		// Tempfile is no longer needed after extraction.
	}

	emit(Progress{Phase: PhaseInstall, Percent: 100, Message: "Finalising…"})

	if runtime.GOOS != "windows" {
		if err := os.Chmod(dest, 0o755); err != nil {
			emit(Progress{Phase: PhaseError, Error: err.Error()})
			return "", fmt.Errorf("chmod %s: %w", dest, err)
		}
	}

	emit(Progress{Phase: PhaseDone, Percent: 100, Message: "Installed", Path: dest})
	return dest, nil
}

// download streams URL into w, calling onPercent at most every ~200ms with the
// current integer percent (or 0 when total size is unknown). Cancellation via
// ctx is honoured by net/http and the io.Copy loop.
func download(ctx context.Context, url string, w io.Writer, onPercent func(int)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "ytdlp-ui-installer")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	total := resp.ContentLength
	pr := &progressReader{
		r:         resp.Body,
		total:     total,
		onPercent: onPercent,
		lastEmit:  time.Now(),
	}
	if _, err := io.Copy(w, pr); err != nil {
		return fmt.Errorf("copy body: %w", err)
	}
	if onPercent != nil {
		onPercent(100)
	}
	return nil
}

// progressReader wraps an io.Reader and periodically reports a percent based
// on bytes-read vs Content-Length. Emission is throttled to ~200ms to avoid
// flooding the event channel during fast downloads.
type progressReader struct {
	r         io.Reader
	total     int64
	read      int64
	onPercent func(int)
	lastEmit  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.onPercent != nil && time.Since(p.lastEmit) > 200*time.Millisecond {
		p.lastEmit = time.Now()
		pct := 0
		if p.total > 0 {
			pct = int(p.read * 100 / p.total)
			if pct > 99 {
				pct = 99
			}
		}
		p.onPercent(pct)
	}
	return n, err
}

// extractEntry pulls a single named entry out of archivePath (matching by
// basename) and writes it to dest.
func extractEntry(archivePath string, kind archiveKind, entry, dest string) error {
	switch kind {
	case archiveZip:
		return extractZipEntry(archivePath, entry, dest)
	default:
		return fmt.Errorf("unsupported archive kind %q", kind)
	}
}

func extractZipEntry(archivePath, entry, dest string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if path.Base(f.Name) != entry {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q: %w", f.Name, err)
		}
		defer rc.Close()

		out, err := os.Create(dest)
		if err != nil {
			return fmt.Errorf("create %s: %w", dest, err)
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return fmt.Errorf("write %s: %w", dest, err)
		}
		return out.Close()
	}
	return fmt.Errorf("entry %q not found in zip", entry)
}

// assetFor returns the download descriptor for (name, goos, goarch), or
// ErrUnsupportedPlatform when no descriptor is known.
func assetFor(name, goos, goarch string) (asset, error) {
	switch strings.ToLower(name) {
	case "yt-dlp":
		return ytdlpAsset(goos, goarch)
	case "ffmpeg":
		return ffmpegAsset(goos, goarch)
	case "deno":
		return denoAsset(goos, goarch)
	default:
		return asset{}, fmt.Errorf("unknown binary %q", name)
	}
}

// yt-dlp ships single-file builds per platform from its GitHub releases. We
// pull the "latest" alias so the user always gets a current build.
func ytdlpAsset(goos, goarch string) (asset, error) {
	base := "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
	switch goos {
	case "darwin":
		return asset{URL: base + "yt-dlp_macos", Kind: archiveNone}, nil
	case "linux":
		// Linux x86_64 + aarch64 both have prebuilt static binaries.
		switch goarch {
		case "amd64":
			return asset{URL: base + "yt-dlp_linux", Kind: archiveNone}, nil
		case "arm64":
			return asset{URL: base + "yt-dlp_linux_aarch64", Kind: archiveNone}, nil
		}
	case "windows":
		return asset{URL: base + "yt-dlp.exe", Kind: archiveNone}, nil
	}
	return asset{}, fmt.Errorf("%w: yt-dlp on %s/%s", ErrUnsupportedPlatform, goos, goarch)
}

// ffmpeg static builds come from per-platform community hosts: evermeet on
// macOS, johnvansickle on Linux, BtbN's FFmpeg-Builds GitHub releases on
// Windows. None are owned by FFmpeg upstream — they're stable in practice but
// the user can always set the path manually if a URL breaks.
func ffmpegAsset(goos, goarch string) (asset, error) {
	switch goos {
	case "darwin":
		// evermeet ships an x86_64 static ffmpeg as a flat .zip ("ffmpeg" at
		// the archive root). Apple Silicon runs it under Rosetta 2.
		return asset{
			URL:   "https://evermeet.cx/ffmpeg/getrelease/zip",
			Kind:  archiveZip,
			Entry: "ffmpeg",
		}, nil
	case "linux":
		// johnvansickle's static builds are tar.xz only; we don't ship an xz
		// reader, so direct users to install via their package manager.
		return asset{}, fmt.Errorf("%w: ffmpeg on linux (install via package manager: apt/dnf/pacman)", ErrUnsupportedPlatform)
	case "windows":
		if goarch != "amd64" {
			break
		}
		// BtbN ships ffmpeg.exe inside ffmpeg-master-latest-win64-gpl/bin/.
		return asset{
			URL:   "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip",
			Kind:  archiveZip,
			Entry: "ffmpeg.exe",
		}, nil
	}
	return asset{}, fmt.Errorf("%w: ffmpeg on %s/%s", ErrUnsupportedPlatform, goos, goarch)
}

// deno publishes per-target zips on GitHub releases; the "deno" binary is at
// the root of every zip.
func denoAsset(goos, goarch string) (asset, error) {
	base := "https://github.com/denoland/deno/releases/latest/download/"
	entry := "deno"
	if goos == "windows" {
		entry = "deno.exe"
	}
	var target string
	switch goos {
	case "darwin":
		switch goarch {
		case "amd64":
			target = "x86_64-apple-darwin"
		case "arm64":
			target = "aarch64-apple-darwin"
		}
	case "linux":
		switch goarch {
		case "amd64":
			target = "x86_64-unknown-linux-gnu"
		case "arm64":
			target = "aarch64-unknown-linux-gnu"
		}
	case "windows":
		if goarch == "amd64" {
			target = "x86_64-pc-windows-msvc"
		}
	}
	if target == "" {
		return asset{}, fmt.Errorf("%w: deno on %s/%s", ErrUnsupportedPlatform, goos, goarch)
	}
	return asset{
		URL:   base + "deno-" + target + ".zip",
		Kind:  archiveZip,
		Entry: entry,
	}, nil
}
