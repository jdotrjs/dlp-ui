package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Preview is the result of a metadata-only fetch (yt-dlp -J), surfaced to the
// frontend's fetch preview modal. JSON tags are camelCase for the Wails
// binding. For a single video Formats is populated and Entries is empty; for a
// playlist/channel Entries holds the (flat, cheap) items and Formats is empty.
type Preview struct {
	URL        string `json:"url"`
	IsPlaylist bool   `json:"isPlaylist"`
	// Source is the extractor key, lowercased, matching the library's `source`
	// convention (e.g. "youtube").
	Source    string `json:"source"`
	Title     string `json:"title"`
	Uploader  string `json:"uploader"`
	Thumbnail string `json:"thumbnail"`
	// Duration is in seconds (0 when unknown, e.g. flat playlist entries).
	Duration int `json:"duration"`
	// Formats lists the concrete formats yt-dlp reported for a single video.
	// Always nil/empty for a playlist preview (flat entries carry no formats).
	Formats []Format `json:"formats"`
	// Entries are the flat playlist items (empty for a single video).
	Entries []PreviewEntry `json:"entries"`
	// PlaylistID is the upstream playlist id (yt-dlp `id` for a playlist),
	// empty for a single video.
	PlaylistID string `json:"playlistId"`
	// Count is the number of entries for a playlist (len(Entries)).
	Count int `json:"count"`
}

// PreviewEntry is one item of a playlist preview. From a flat playlist these
// carry id/title/url and sometimes a duration; thumbnails/formats are absent
// (that's expected — full per-item info is fetched lazily later).
type PreviewEntry struct {
	// Source is the extractor key lowercased, falling back to the playlist's
	// source when the flat entry doesn't report one.
	Source string `json:"source"`
	// SourceID is the upstream video id.
	SourceID  string `json:"sourceId"`
	Title     string `json:"title"`
	Uploader  string `json:"uploader"`
	Thumbnail string `json:"thumbnail"`
	Duration  int    `json:"duration"`
	// Index is the 1-based position within the playlist.
	Index int `json:"index"`
	// URL is the per-item URL yt-dlp can later resolve (webpage_url/url/id).
	URL string `json:"url"`
}

// Format is a pragmatic subset of a yt-dlp format object, enough to populate a
// format dropdown for a single video. Absent fields come through zero/empty.
// The UI always offers a synthetic "best" plus an audio-only toggle on top of
// these.
type Format struct {
	FormatID   string `json:"formatId"`
	Ext        string `json:"ext"`
	Resolution string `json:"resolution"`
	FPS        int    `json:"fps"`
	Filesize   int64  `json:"filesize"`
	VCodec     string `json:"vcodec"`
	ACodec     string `json:"acodec"`
	Note       string `json:"note"`
}

// rawInfo mirrors the subset of yt-dlp's -J JSON we read. yt-dlp emits a single
// info object: for a single video it has formats and no _type; for a
// playlist/channel it has `_type: "playlist"` and an `entries` array (flat when
// --flat-playlist is used). We decode leniently — missing fields stay zero.
type rawInfo struct {
	Type         string      `json:"_type"`
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Uploader     string      `json:"uploader"`
	Channel      string      `json:"channel"`
	ExtractorKey string      `json:"extractor_key"`
	Extractor    string      `json:"extractor"`
	WebpageURL   string      `json:"webpage_url"`
	URL          string      `json:"url"`
	Duration     float64     `json:"duration"`
	Thumbnail    string      `json:"thumbnail"`
	Thumbnails   []rawThumb  `json:"thumbnails"`
	Formats      []rawFormat `json:"formats"`
	Entries      []rawEntry  `json:"entries"`
}

type rawThumb struct {
	URL string `json:"url"`
}

type rawFormat struct {
	FormatID   string  `json:"format_id"`
	Ext        string  `json:"ext"`
	Resolution string  `json:"resolution"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        float64 `json:"fps"`
	Filesize   int64   `json:"filesize"`
	FilesizeAp int64   `json:"filesize_approx"`
	VCodec     string  `json:"vcodec"`
	ACodec     string  `json:"acodec"`
	FormatNote string  `json:"format_note"`
}

type rawEntry struct {
	Type         string     `json:"_type"`
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Uploader     string     `json:"uploader"`
	Channel      string     `json:"channel"`
	ExtractorKey string     `json:"ie_key"`
	Duration     float64    `json:"duration"`
	Thumbnail    string     `json:"thumbnail"`
	Thumbnails   []rawThumb `json:"thumbnails"`
	URL          string     `json:"url"`
	WebpageURL   string     `json:"webpage_url"`
}

// Metadata runs `yt-dlp -J --flat-playlist <url>` and maps the result into a
// Preview. A single video URL yields full info (formats included) since there's
// no playlist to flatten; a playlist/channel URL yields a cheap flat entry
// list. Playlist vs single is detected via `_type == "playlist"` (or the
// presence of entries). The runner is injected so this is unit-testable against
// captured fixtures.
//
// On a non-zero yt-dlp exit the error includes yt-dlp's stderr so the UI can
// surface it (e.g. an unsupported URL or a soft-warn about a missing optional
// binary).
func Metadata(ctx context.Context, runner Runner, opts Options, url string) (Preview, error) {
	// TODO:[feat]: `--flat-playlist` seems like the simpler but more error
	// prone way to pull playlist data. experiment with no-flat-playlist.
	cmd := buildCommand(ctx, opts, "-J", "--flat-playlist", url)

	stdout, stderr, err := runner.Run(cmd)
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			return Preview{}, fmt.Errorf("yt-dlp metadata: %w", err)
		}
		return Preview{}, fmt.Errorf("yt-dlp metadata: %w: %s", err, msg)
	}

	return parsePreview(stdout, url)
}

// parsePreview decodes yt-dlp -J JSON into a Preview. Exported indirectly via
// Metadata; kept separate so tests can exercise the mapping against fixtures.
func parsePreview(data []byte, url string) (Preview, error) {
	var info rawInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return Preview{}, fmt.Errorf("parse yt-dlp metadata: %w", err)
	}

	// Prefer `extractor` (the colon form, e.g. "youtube:tab") over
	// `extractor_key` (the camelCase form, e.g. "YoutubeTab") so normalizeSource
	// can split on ':' to reach the base source ("youtube") that matches the
	// library's `source` convention.
	source := normalizeSource(firstNonEmpty(info.Extractor, info.ExtractorKey))

	p := Preview{
		URL:       url,
		Source:    source,
		Title:     info.Title,
		Uploader:  firstNonEmpty(info.Uploader, info.Channel),
		Thumbnail: pickThumbnail(info.Thumbnail, info.Thumbnails),
		Duration:  int(info.Duration),
	}

	isPlaylist := info.Type == "playlist" || len(info.Entries) > 0
	p.IsPlaylist = isPlaylist

	if isPlaylist {
		p.PlaylistID = info.ID
		p.Entries = make([]PreviewEntry, 0, len(info.Entries))
		for i, e := range info.Entries {
			entrySource := normalizeSource(e.ExtractorKey)
			if entrySource == "" {
				entrySource = source
			}
			p.Entries = append(p.Entries, PreviewEntry{
				Source:    entrySource,
				SourceID:  e.ID,
				Title:     e.Title,
				Uploader:  firstNonEmpty(e.Uploader, e.Channel),
				Thumbnail: pickThumbnail(e.Thumbnail, e.Thumbnails),
				Duration:  int(e.Duration),
				Index:     i + 1,
				URL:       firstNonEmpty(e.WebpageURL, e.URL, e.ID),
			})
		}
		p.Count = len(p.Entries)
		return p, nil
	}

	// Single video: map the concrete formats for the dropdown.
	p.Formats = make([]Format, 0, len(info.Formats))
	for _, f := range info.Formats {
		p.Formats = append(p.Formats, Format{
			FormatID:   f.FormatID,
			Ext:        f.Ext,
			Resolution: resolutionOf(f),
			FPS:        int(f.FPS),
			Filesize:   firstNonZero(f.Filesize, f.FilesizeAp),
			VCodec:     f.VCodec,
			ACodec:     f.ACodec,
			Note:       f.FormatNote,
		})
	}
	return p, nil
}

// normalizeSource lowercases the extractor key to match the library `source`
// convention. yt-dlp's extractor_key is e.g. "Youtube"; extractor is e.g.
// "youtube:tab" — we take the leading token before any ':' and lowercase it.
func normalizeSource(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// resolutionOf prefers yt-dlp's `resolution` string, falling back to WxH.
func resolutionOf(f rawFormat) string {
	if f.Resolution != "" {
		return f.Resolution
	}
	if f.Width > 0 && f.Height > 0 {
		return fmt.Sprintf("%dx%d", f.Width, f.Height)
	}
	return ""
}

// pickThumbnail returns the explicit thumbnail if set, else the last entry of
// the thumbnails list (yt-dlp orders them worst→best, so last is highest-res).
func pickThumbnail(thumb string, thumbs []rawThumb) string {
	if thumb != "" {
		return thumb
	}
	if len(thumbs) > 0 {
		return thumbs[len(thumbs)-1].URL
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(vals ...int64) int64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
