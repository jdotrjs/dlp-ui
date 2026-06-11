package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// Thumbnail is one row of content_thumbnail: the image bytes for a content row.
// Data holds the raw image bytes; Mime is what the upstream server reported (or
// what we sniffed). The row is keyed 1:1 by ContentID and cascades on content
// delete, so callers do not need to clean up thumbnails by hand.
type Thumbnail struct {
	ContentID int64  `json:"contentId"`
	Mime      string `json:"mime"`
	Data      []byte `json:"data"`
	SourceURL string `json:"sourceUrl"`
	FetchedAt int64  `json:"fetchedAt"`
}

// SaveThumbnail upserts a thumbnail row for ContentID. An existing row is
// replaced in place; this is what the post-download fetcher uses to refresh on
// a re-download.
func (s *Store) SaveThumbnail(t Thumbnail) error {
	if t.ContentID == 0 {
		return fmt.Errorf("save thumbnail: zero content id")
	}
	if len(t.Data) == 0 {
		return fmt.Errorf("save thumbnail %d: empty data", t.ContentID)
	}
	if _, err := s.db.Exec(`
		INSERT INTO content_thumbnail
			(content_id, mime, data, source_url, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(content_id) DO UPDATE SET
			mime       = excluded.mime,
			data       = excluded.data,
			source_url = excluded.source_url,
			fetched_at = excluded.fetched_at`,
		t.ContentID, t.Mime, t.Data, t.SourceURL, t.FetchedAt); err != nil {
		return fmt.Errorf("save thumbnail %d: %w", t.ContentID, err)
	}
	return nil
}

// GetThumbnail returns the thumbnail row for contentID, or ErrNotFound when no
// thumbnail has been fetched yet for that content.
func (s *Store) GetThumbnail(contentID int64) (Thumbnail, error) {
	var t Thumbnail
	err := s.db.QueryRow(`
		SELECT content_id, mime, data, source_url, fetched_at
		FROM content_thumbnail WHERE content_id = ?`, contentID,
	).Scan(&t.ContentID, &t.Mime, &t.Data, &t.SourceURL, &t.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Thumbnail{}, ErrNotFound
	}
	if err != nil {
		return Thumbnail{}, fmt.Errorf("get thumbnail %d: %w", contentID, err)
	}
	return t, nil
}

// HasThumbnail reports whether content_thumbnail has a row for contentID. It's
// cheaper than GetThumbnail when the caller only needs the boolean (e.g. the
// frontend pre-flighting whether to render the <img>).
func (s *Store) HasThumbnail(contentID int64) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM content_thumbnail WHERE content_id = ?`, contentID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has thumbnail %d: %w", contentID, err)
	}
	return true, nil
}
