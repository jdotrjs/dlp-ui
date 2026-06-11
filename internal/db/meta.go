package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// MetaEntry is a row of the meta key/value table.
//
// Version is an optimistic-lock revision counter, incremented on every write to
// the key (see MetaSet). A caller that wants concurrency-safe updates can read
// (value, version), compute the next value, and refuse to commit if the version
// has moved since the read. Today nothing does that, but the column lets us
// grow into it without another migration.
type MetaEntry struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

// MetaGet returns the entry for key, or ErrNotFound.
func (s *Store) MetaGet(key string) (MetaEntry, error) {
	var e MetaEntry
	err := s.db.QueryRow(
		"SELECT key, value, version FROM meta WHERE key = ?", key,
	).Scan(&e.Key, &e.Value, &e.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return MetaEntry{}, ErrNotFound
	}
	if err != nil {
		return MetaEntry{}, fmt.Errorf("get meta %q: %w", key, err)
	}
	return e, nil
}

// MetaSet upserts (key, value). On insert the row's version starts at 1; on
// update it is bumped by 1. Callers reading MetaEntry.Version see a monotonic
// revision counter they can use for optimistic locking later.
func (s *Store) MetaSet(key, value string) error {
	if _, err := s.db.Exec(`
		INSERT INTO meta (key, value, version)
		VALUES (?, ?, 1)
		ON CONFLICT(key) DO UPDATE SET
			value   = excluded.value,
			version = meta.version + 1`,
		key, value); err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
}
