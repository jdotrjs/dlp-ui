package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// Playlist mirrors a row of the playlists table. A playlist groups content
// rows; source_id is the upstream id (YouTube PL…, etc.) and is empty for
// ad-hoc groupings (a channel page, a multi-video page, a loose selection).
// JSON tags are camelCase for the Wails binding.
type Playlist struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	SourceID  string `json:"sourceId"`
	Title     string `json:"title"`
	SourceURL string `json:"sourceUrl"`
	ItemCount int    `json:"itemCount"`
	CreatedAt int64  `json:"createdAt"`
}

// GetPlaylist returns the playlist row with the given id, or ErrNotFound.
func (s *Store) GetPlaylist(id int64) (Playlist, error) {
	var p Playlist
	var sourceID sql.NullString
	err := s.db.QueryRow(`
		SELECT id, source, source_id, title, source_url, item_count, created_at
		FROM playlists WHERE id = ?`, id).
		Scan(&p.ID, &p.Source, &sourceID, &p.Title, &p.SourceURL,
			&p.ItemCount, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Playlist{}, ErrNotFound
	}
	if err != nil {
		return Playlist{}, fmt.Errorf("get playlist %d: %w", id, err)
	}
	p.SourceID = sourceID.String
	return p, nil
}

// UpsertPlaylist inserts a playlist grouping, or updates the mutable columns of
// an existing one with the same (source, source_id), returning the id. The
// download manager uses this to ensure a grouping exists before attaching
// content to it.
//
// A playlist with an empty source_id (ad-hoc grouping) has a NULL natural key,
// which UNIQUE treats as distinct, so each such call inserts a fresh grouping.
func (s *Store) UpsertPlaylist(p Playlist) (int64, error) {
	if p.SourceID == "" {
		res, err := s.db.Exec(`
			INSERT INTO playlists (source, source_id, title, source_url, item_count, created_at)
			VALUES (?,?,?,?,?,?)`,
			p.Source, nil, p.Title, p.SourceURL, p.ItemCount, p.CreatedAt)
		if err != nil {
			return 0, fmt.Errorf("insert playlist: %w", err)
		}
		return res.LastInsertId()
	}

	_, err := s.db.Exec(`
		INSERT INTO playlists (source, source_id, title, source_url, item_count, created_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(source, source_id) DO UPDATE SET
			title      = excluded.title,
			source_url = excluded.source_url,
			item_count = excluded.item_count`,
		p.Source, p.SourceID, p.Title, p.SourceURL, p.ItemCount, p.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("upsert playlist: %w", err)
	}

	var id int64
	if err := s.db.QueryRow(
		"SELECT id FROM playlists WHERE source = ? AND source_id = ?",
		p.Source, p.SourceID).Scan(&id); err != nil {
		return 0, fmt.Errorf("upsert playlist resolve id: %w", err)
	}
	return id, nil
}
