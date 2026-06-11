package ytdlp

// download.go is the download + progress half of the ytdlp wrapper. It is
// Wails-agnostic: Download assembles the yt-dlp download invocation (reusing
// buildCommand for --ffmpeg-location / node PATH injection), runs the child
// with a STREAMING stdout reader, parses the custom --progress-template
// PROGRESS lines and the --print after_move DONE lines, and invokes callbacks
// for live progress and per-item completion. The bound *App method (chunk 4's
// internal/download manager) adapts those callbacks to Wails runtime events.
//
// Stdout protocol (one stream, parsed by sentinel prefix; fields delimited by
// the ASCII Unit Separator \x1f so a video title / filepath containing the
// human-friendly '|' can never break parsing):
//
//   PROGRESS<US>playlist_index<US>percent_str<US>downloaded<US>total<US>speed<US>eta
//   DONE<US>playlist_index<US>extractor<US>id<US>uploader<US>duration<US>upload_date<US>thumbnail<US>ext<US>filesize<US>webpage_url<US>title<US>filepath
//
// Lines without a known prefix are ignored. yt-dlp's own animated progress bar
// is replaced by the custom --progress-template; --newline forces one progress
// line per update (no carriage-return overwriting) so the line scanner sees it.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// us is the ASCII Unit Separator used as the field delimiter in the custom
// templates. It won't appear in titles, paths, ids, etc., so a simple split is
// robust.
const us = "\x1f"

const (
	progressPrefix = "PROGRESS"
	donePrefix     = "DONE"
)

// progressTemplate is the value passed to --progress-template. The "download:"
// scope restricts it to download progress (not post-processing). _percent_str
// is e.g. " 42.0%"; total_bytes is "NA" until known (we fall back to
// total_bytes_estimate); speed/eta are "NA"/numeric.
const progressTemplate = "download:" + progressPrefix + us +
	"%(info.playlist_index)s" + us +
	"%(progress._percent_str)s" + us +
	"%(progress.downloaded_bytes)s" + us +
	"%(progress.total_bytes,total_bytes_estimate)s" + us +
	"%(progress.speed)s" + us +
	"%(progress.eta)s"

// doneTemplate is the value passed to --print after_move:. after_move runs once
// per item after the final file is moved into place, so %(filepath)s is the
// moved path. filesize falls back to filesize_approx. title and filepath are
// last; with the \x1f delimiter their contents are still safe, but keeping the
// free-text fields trailing is defensive.
const doneTemplate = "after_move:" + donePrefix + us +
	"%(playlist_index)s" + us +
	"%(extractor)s" + us +
	"%(id)s" + us +
	"%(uploader)s" + us +
	"%(duration)s" + us +
	"%(upload_date)s" + us +
	"%(thumbnail)s" + us +
	"%(ext)s" + us +
	"%(filesize,filesize_approx)s" + us +
	"%(webpage_url)s" + us +
	"%(title)s" + us +
	"%(filepath)s"

// Request is the resolved download request the ytdlp wrapper consumes. It is
// the package-local mirror of the bound DownloadRequest, decoupled from the
// Wails/main layer: the manager translates a DownloadRequest into a Request
// (resolving output-template/format/playlist args from config) — except those
// arg-shaping decisions live here via the fields below so this stays the single
// place that knows the yt-dlp CLI.
type Request struct {
	// URL is the page URL to download.
	URL string
	// AudioOnly requests audio extraction (-x).
	AudioOnly bool
	// FormatID, when non-empty (and not AudioOnly), is passed as -f.
	FormatID string
	// PlaylistItems, when non-empty, is the comma-joined 1-based index list
	// passed as --playlist-items (single video → empty).
	PlaylistItems string
	// OutputArgs is the fully-resolved -o / --restrict-filenames / playlist
	// layout fragment (built by the caller from config + per-request
	// overrides). Download appends it verbatim.
	OutputArgs []string
	// DownloadDir, when non-empty, is passed as --paths home:<dir> so output
	// lands there regardless of the -o template's leading directory.
	DownloadDir string
}

// Progress is one parsed PROGRESS line: a live per-item progress update.
// Unknown/NA numeric fields come through as zero. PlaylistIndex is 0 for a
// single (non-playlist) download (yt-dlp emits "NA").
type Progress struct {
	PlaylistIndex   int
	Percent         float64 // 0..100
	DownloadedBytes int64
	TotalBytes      int64
	Speed           float64 // bytes/sec
	ETA             int     // seconds
}

// Completion is one parsed DONE line: a finished item with its moved file path
// and the metadata needed to persist a library row.
type Completion struct {
	PlaylistIndex int
	Extractor     string // raw extractor key (caller lowercases for `source`)
	ID            string
	Uploader      string
	Duration      int
	UploadDate    string // yt-dlp YYYYMMDD
	Thumbnail     string
	Ext           string
	Filesize      int64
	WebpageURL    string
	Title         string
	Filepath      string // final moved path (after_move)
}

// Callbacks bundles the streaming hooks Download invokes. Any may be nil.
type Callbacks struct {
	// OnProgress fires for each parsed PROGRESS line (already throttled? no —
	// throttling is the manager's job; this fires for every update).
	OnProgress func(Progress)
	// OnItemDone fires for each parsed DONE line (one per finished item).
	OnItemDone func(Completion)
}

// downloadArgs assembles the yt-dlp argument list for a download (everything
// except the trailing URL and the binary-location plumbing buildCommand adds).
// Kept separate so it is unit-testable without running a process.
func downloadArgs(req Request) []string {
	args := []string{
		"--no-playlist-reverse",
		"--progress-template", progressTemplate,
		"--newline",
		"--print", doneTemplate,
	}
	if req.AudioOnly {
		args = append(args, "-x")
	} else if req.FormatID != "" {
		args = append(args, "-f", req.FormatID)
	}
	if req.PlaylistItems != "" {
		args = append(args, "--playlist-items", req.PlaylistItems)
	}
	if req.DownloadDir != "" {
		args = append(args, "--paths", "home:"+req.DownloadDir)
	}
	args = append(args, req.OutputArgs...)
	return args
}

// Download runs a single yt-dlp download invocation, streaming stdout and
// invoking cb for each progress/completion line. It blocks until yt-dlp exits.
// The supplied ctx cancels the child (exec.CommandContext) — the manager wires
// this to CancelDownload. On a non-zero exit the returned error includes
// yt-dlp's stderr so the manager can surface it as a `failed` event.
func Download(ctx context.Context, opts Options, req Request, cb Callbacks) error {
	args := append(downloadArgs(req), req.URL)
	cmd := buildCommand(ctx, opts, args...)
	cmd.Dir = req.DownloadDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("yt-dlp stdout pipe: %w", err)
	}
	// Capture stderr so a failure can surface yt-dlp's message.
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start yt-dlp: %w", err)
	}

	// Stream stdout line-by-line on this goroutine; the child writes to the
	// pipe concurrently. Scan into a generous buffer (long titles/paths).
	scanErr := scanStream(stdout, cb)

	waitErr := cmd.Wait()
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("yt-dlp download: %w: %s", waitErr, msg)
		}
		return fmt.Errorf("yt-dlp download: %w", waitErr)
	}
	// A successful exit means stdout reached EOF cleanly; surface a scan error
	// only if the process itself succeeded (otherwise the exit error wins).
	if scanErr != nil {
		return fmt.Errorf("read yt-dlp output: %w", scanErr)
	}
	return nil
}

// scanStream reads r line by line, dispatching recognised sentinel lines to the
// callbacks. Unknown lines are ignored.
func scanStream(r io.Reader, cb Callbacks) error {
	sc := bufio.NewScanner(r)
	// yt-dlp lines are short, but titles/paths can be long; grow the buffer.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, progressPrefix+us):
			if cb.OnProgress != nil {
				if p, ok := parseProgress(line); ok {
					cb.OnProgress(p)
				}
			}
		case strings.HasPrefix(line, donePrefix+us):
			if cb.OnItemDone != nil {
				if c, ok := parseCompletion(line); ok {
					cb.OnItemDone(c)
				}
			}
		}
	}
	return sc.Err()
}

// parseProgress parses a PROGRESS line into a Progress. ok is false only if the
// line doesn't have the expected field count. Individual NA/empty fields parse
// to zero rather than failing the whole line.
func parseProgress(line string) (Progress, bool) {
	parts := strings.Split(line, us)
	// prefix + 6 fields
	if len(parts) != 7 || parts[0] != progressPrefix {
		return Progress{}, false
	}
	return Progress{
		PlaylistIndex:   atoiNA(parts[1]),
		Percent:         parsePercent(parts[2]),
		DownloadedBytes: atoi64NA(parts[3]),
		TotalBytes:      atoi64NA(parts[4]),
		Speed:           atofNA(parts[5]),
		ETA:             atoiNA(parts[6]),
	}, true
}

// parseCompletion parses a DONE line into a Completion. ok is false only if the
// field count is wrong.
func parseCompletion(line string) (Completion, bool) {
	parts := strings.Split(line, us)
	// prefix + 12 fields
	if len(parts) != 13 || parts[0] != donePrefix {
		return Completion{}, false
	}
	return Completion{
		PlaylistIndex: atoiNA(parts[1]),
		Extractor:     naBlank(parts[2]),
		ID:            naBlank(parts[3]),
		Uploader:      naBlank(parts[4]),
		Duration:      atoiNA(parts[5]),
		UploadDate:    naBlank(parts[6]),
		Thumbnail:     naBlank(parts[7]),
		Ext:           naBlank(parts[8]),
		Filesize:      atoi64NA(parts[9]),
		WebpageURL:    naBlank(parts[10]),
		Title:         naBlank(parts[11]),
		Filepath:      parts[12],
	}, true
}

// parsePercent parses yt-dlp's _percent_str (e.g. " 42.0%") into a float. A
// leading/trailing space and the trailing '%' are stripped; NA → 0.
func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	return atofNA(s)
}

// atoiNA parses an int, treating "NA"/empty/non-numeric as 0. yt-dlp may emit a
// float (e.g. eta "12.0") for nominally-int fields, so we tolerate that.
func atoiNA(s string) int {
	return int(atofNA(s))
}

func atoi64NA(s string) int64 {
	return int64(atofNA(s))
}

// atofNA parses a float, treating "NA"/empty/non-numeric as 0.
func atofNA(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "NA" || s == "none" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// naBlank returns "" for yt-dlp's "NA" sentinel, else the trimmed-of-nothing
// value (preserving internal spacing in titles).
func naBlank(s string) string {
	if s == "NA" {
		return ""
	}
	return s
}
