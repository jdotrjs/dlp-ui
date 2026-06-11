package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// Status is the lifecycle state of a content row. It's a string newtype, so it
// erases to plain `string` across the Wails binding — the frontend re-narrows
// it (see frontend/src/lib/types.ts).
type Status string

const (
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusDownloading Status = "downloading"
)

// Content mirrors a row of the content table plus the joined playlist title.
// JSON tags are camelCase because the same shape crosses the Wails binding to
// the Preact frontend. Timestamps (downloadedAt) are unix seconds.
type Content struct {
	ID            int64  `json:"id"`
	Source        string `json:"source"`
	SourceID      string `json:"sourceId"`
	Title         string `json:"title"`
	Uploader      string `json:"uploader"`
	Duration      int    `json:"duration"`
	UploadDate    string `json:"uploadDate"`
	ThumbnailURL  string `json:"thumbnailUrl"`
	Ext           string `json:"ext"`
	Filesize      int64  `json:"filesize"`
	SourceURL     string `json:"sourceUrl"`
	Filepath      string `json:"filepath"`
	Status        Status `json:"status"`
	Error         string `json:"error"`
	PlaylistID    int64  `json:"playlistId"`
	PlaylistIndex int    `json:"playlistIndex"`
	DownloadedAt  int64  `json:"downloadedAt"`

	// PlaylistTitle is joined from playlists; empty when the row has no
	// playlist (playlist_id NULL) or the playlist was deleted.
	PlaylistTitle string `json:"playlistTitle"`
}

// contentColumns is the SELECT list (with the playlist join) used by every
// read. playlist_id is read via COALESCE so a NULL FK comes back as 0.
const contentColumns = `
	c.id, c.source, COALESCE(c.source_id, ''), c.title, c.uploader, c.duration,
	c.upload_date, c.thumbnail_url, c.ext, c.filesize, c.source_url, c.filepath,
	c.status, c.error, COALESCE(c.playlist_id, 0), c.playlist_index,
	c.downloaded_at, COALESCE(p.title, '')`

const contentFromJoin = `
	FROM content c
	LEFT JOIN playlists p ON p.id = c.playlist_id`

// scanContent reads one Content row in contentColumns order.
func scanContent(s interface{ Scan(...any) error }) (Content, error) {
	var c Content
	var status string
	err := s.Scan(
		&c.ID, &c.Source, &c.SourceID, &c.Title, &c.Uploader, &c.Duration,
		&c.UploadDate, &c.ThumbnailURL, &c.Ext, &c.Filesize, &c.SourceURL,
		&c.Filepath, &status, &c.Error, &c.PlaylistID, &c.PlaylistIndex,
		&c.DownloadedAt, &c.PlaylistTitle,
	)
	c.Status = Status(status)
	return c, err
}

// GetContent returns the content row with the given id, or ErrNotFound.
func (s *Store) GetContent(id int64) (Content, error) {
	row := s.db.QueryRow(
		"SELECT "+contentColumns+contentFromJoin+" WHERE c.id = ?", id)
	c, err := scanContent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Content{}, ErrNotFound
	}
	if err != nil {
		return Content{}, fmt.Errorf("get content %d: %w", id, err)
	}
	return c, nil
}

// InsertContent inserts a new content row and returns its assigned id. Most
// callers will prefer UpsertContent, which is idempotent on the (source,
// source_id) natural key; InsertContent is the plain primitive.
func (s *Store) InsertContent(c Content) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO content
			(source, source_id, title, uploader, duration, upload_date,
			 thumbnail_url, ext, filesize, source_url, filepath, status, error,
			 playlist_id, playlist_index, downloaded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.Source, nullIfEmpty(c.SourceID), c.Title, c.Uploader, c.Duration,
		c.UploadDate, c.ThumbnailURL, c.Ext, c.Filesize, c.SourceURL,
		c.Filepath, statusOrDefault(c.Status), c.Error,
		nullIfZero(c.PlaylistID), c.PlaylistIndex, c.DownloadedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert content: %w", err)
	}
	return res.LastInsertId()
}

// UpsertContent inserts a content row, or — when one already exists with the
// same (source, source_id) — updates the mutable columns in place and returns
// the existing id. This is the idempotent entry point the download manager
// uses so a re-download (or a download that completes after a placeholder row
// was written) doesn't create duplicates.
//
// Rows with an empty source_id (loose/ad-hoc downloads) have a NULL natural
// key, which UNIQUE treats as distinct, so each such call inserts a new row.
// Those are inserted via InsertContent semantics (no conflict target matches).
func (s *Store) UpsertContent(c Content) (int64, error) {
	if c.SourceID == "" {
		return s.InsertContent(c)
	}
	res, err := s.db.Exec(`
		INSERT INTO content
			(source, source_id, title, uploader, duration, upload_date,
			 thumbnail_url, ext, filesize, source_url, filepath, status, error,
			 playlist_id, playlist_index, downloaded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(source, source_id) DO UPDATE SET
			title          = excluded.title,
			uploader       = excluded.uploader,
			duration       = excluded.duration,
			upload_date    = excluded.upload_date,
			thumbnail_url  = excluded.thumbnail_url,
			ext            = excluded.ext,
			filesize       = excluded.filesize,
			source_url     = excluded.source_url,
			filepath       = excluded.filepath,
			status         = excluded.status,
			error          = excluded.error,
			playlist_id    = excluded.playlist_id,
			playlist_index = excluded.playlist_index,
			downloaded_at  = excluded.downloaded_at`,
		c.Source, c.SourceID, c.Title, c.Uploader, c.Duration,
		c.UploadDate, c.ThumbnailURL, c.Ext, c.Filesize, c.SourceURL,
		c.Filepath, statusOrDefault(c.Status), c.Error,
		nullIfZero(c.PlaylistID), c.PlaylistIndex, c.DownloadedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert content: %w", err)
	}
	if id, lerr := res.LastInsertId(); lerr == nil && id != 0 {
		// LastInsertId is meaningful on the INSERT branch. On the UPDATE
		// branch SQLite still reports the conflicting rowid, but to be safe we
		// look it up by natural key below when rows weren't affected as insert.
		if n, _ := res.RowsAffected(); n == 1 {
			return id, nil
		}
	}
	// Resolve the id via the natural key (covers the UPDATE branch).
	var id int64
	if err := s.db.QueryRow(
		"SELECT id FROM content WHERE source = ? AND source_id = ?",
		c.Source, c.SourceID).Scan(&id); err != nil {
		return 0, fmt.Errorf("upsert content resolve id: %w", err)
	}
	return id, nil
}

// DeleteContent removes the content row. When deleteFile is true it also makes
// a best-effort attempt to delete the file on disk; a failure to remove the
// file is returned to the caller but the row is already gone (the catalogue is
// the source of truth for what's listed). content_thumbnail rows cascade.
func (s *Store) DeleteContent(id int64, deleteFile bool) error {
	var path string
	err := s.db.QueryRow("SELECT filepath FROM content WHERE id = ?", id).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup content %d: %w", id, err)
	}

	if _, err := s.db.Exec("DELETE FROM content WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete content %d: %w", id, err)
	}

	if deleteFile && path != "" {
		if rmErr := removeFile(path); rmErr != nil {
			return fmt.Errorf("delete file %q: %w", path, rmErr)
		}
	}
	return nil
}

// helpers ------------------------------------------------------------------

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZero(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func statusOrDefault(s Status) string {
	if s == "" {
		return string(StatusCompleted)
	}
	return string(s)
}

// removeFile deletes a file, treating an already-absent file as success so a
// row whose download was manually removed can still be cleaned up.
func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
