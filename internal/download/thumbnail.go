package download

import (
	"context"
	"io"
	"net/http"
	"time"

	"ytdlp-gui/internal/db"
)

// Tunables for the post-download thumbnail fetch. Hosts usually serve a JPEG or
// WebP under 200 KiB; the 5 MiB cap is just a guard against pathological
// responses (or a wrong URL pointing at a video file).
const (
	thumbnailFetchTimeout = 20 * time.Second
	thumbnailMaxBytes     = 5 << 20
)

// thumbnailFetcher fetches `url` and returns its bytes + content-type. It exists
// as a seam so tests can substitute a deterministic implementation; production
// uses fetchThumbnailHTTP.
type thumbnailFetcher func(ctx context.Context, url string) (mime string, data []byte, err error)

// fetchThumbnailHTTP is the production thumbnailFetcher. It does a GET with a
// hard timeout and a response-size cap.
func fetchThumbnailHTTP(ctx context.Context, url string) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, &httpStatusError{code: resp.StatusCode}
	}

	// LimitReader+1 lets us detect "too big" by reading one extra byte.
	data, err := io.ReadAll(io.LimitReader(resp.Body, thumbnailMaxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 {
		return "", nil, errEmptyBody
	}
	if len(data) > thumbnailMaxBytes {
		return "", nil, errTooLarge
	}

	mime := resp.Header.Get("Content-Type")
	if mime == "" {
		// Sniff from the bytes if the server didn't say. Good enough for the
		// common JPEG/PNG/WebP cases.
		mime = http.DetectContentType(data)
	}
	return mime, data, nil
}

// fetchAndSaveThumbnail runs the fetcher and persists the bytes to
// content_thumbnail. It is best-effort: any failure short-circuits without
// touching the row (no thumbnail is better than a half-written one) and the
// outer download is unaffected.
func (m *Manager) fetchAndSaveThumbnail(contentID int64, url string) {
	if m.store == nil || contentID == 0 || url == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailFetchTimeout)
	defer cancel()

	mime, data, err := m.fetchThumbnail(ctx, url)
	if err != nil {
		return
	}
	_ = m.store.SaveThumbnail(db.Thumbnail{
		ContentID: contentID,
		Mime:      mime,
		Data:      data,
		SourceURL: url,
		FetchedAt: time.Now().Unix(),
	})
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return http.StatusText(e.code) }

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

const (
	errEmptyBody = sentinelErr("empty body")
	errTooLarge  = sentinelErr("response too large")
)
