package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// SchemaVersionKey is the meta-table key that mirrors PRAGMA user_version. The
// PRAGMA is still the source of truth the migrate runner consults; this row
// just exposes the same number to features that already speak meta (e.g. a
// future "compat" check that wants every persisted version in one place).
const SchemaVersionKey = "ytdlp-ui.schema.version"

// migrations is the append-only list of schema steps. The index+1 of each entry
// is its target user_version (so migrations[0] takes the DB from version 0 to
// 1). NEVER edit or reorder an existing entry once shipped — only append. Each
// step runs inside its own transaction in migrate().
var migrations = []string{
	// v1: initial schema — playlists + content.
	`
	CREATE TABLE playlists (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		source      TEXT NOT NULL,
		source_id   TEXT,
		title       TEXT NOT NULL DEFAULT '',
		source_url  TEXT NOT NULL DEFAULT '',
		item_count  INTEGER NOT NULL DEFAULT 0,
		created_at  INTEGER NOT NULL DEFAULT 0
	);

	-- A grouping is unique by (source, upstream playlist id). source_id is
	-- nullable (a channel page / ad-hoc selection has no real playlist id);
	-- SQLite treats NULLs as distinct in a UNIQUE index, which is what we want
	-- (each such ad-hoc grouping gets its own row).
	CREATE UNIQUE INDEX idx_playlists_source_sourceid
		ON playlists(source, source_id);

	CREATE TABLE content (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		source         TEXT NOT NULL,
		source_id      TEXT,
		title          TEXT NOT NULL DEFAULT '',
		uploader       TEXT NOT NULL DEFAULT '',
		duration       INTEGER NOT NULL DEFAULT 0,
		upload_date    TEXT NOT NULL DEFAULT '',
		thumbnail_url  TEXT NOT NULL DEFAULT '',
		ext            TEXT NOT NULL DEFAULT '',
		filesize       INTEGER NOT NULL DEFAULT 0,
		source_url     TEXT NOT NULL DEFAULT '',
		filepath       TEXT NOT NULL DEFAULT '',
		status         TEXT NOT NULL DEFAULT 'completed',
		error          TEXT NOT NULL DEFAULT '',
		playlist_id    INTEGER REFERENCES playlists(id) ON DELETE SET NULL,
		playlist_index INTEGER NOT NULL DEFAULT 0,
		downloaded_at  INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX idx_content_playlist_id   ON content(playlist_id);
	CREATE INDEX idx_content_status        ON content(status);
	CREATE INDEX idx_content_downloaded_at ON content(downloaded_at DESC);
	CREATE INDEX idx_content_source        ON content(source);

	-- A video id is only unique within a source, so the natural key is the
	-- pair. NULL source_id rows (loose/ad-hoc downloads) are again distinct.
	CREATE UNIQUE INDEX idx_content_source_sourceid
		ON content(source, source_id);
	`,

	// v2: meta KV store. version is an optimistic-lock revision counter,
	// bumped on every write to a key (see MetaSet).
	`
	CREATE TABLE meta (
		key     TEXT NOT NULL PRIMARY KEY,
		value   TEXT NOT NULL DEFAULT '',
		version INTEGER NOT NULL DEFAULT 0
	);
	`,

	// v3: thumbnail bytes, stored inline as a BLOB rather than as a file path
	// so the catalogue is self-contained (rename/move of the download dir
	// can't orphan them). One-to-one with content via the FK; deleting the
	// content row cascades the thumbnail row.
	`
	CREATE TABLE content_thumbnail (
		content_id INTEGER NOT NULL PRIMARY KEY
			REFERENCES content(id) ON DELETE CASCADE,
		mime       TEXT NOT NULL DEFAULT '',
		data       BLOB NOT NULL,
		source_url TEXT NOT NULL DEFAULT '',
		fetched_at INTEGER NOT NULL DEFAULT 0
	);
	`,
}

// migrate brings the schema up to len(migrations) by applying any pending steps,
// each in its own transaction, advancing PRAGMA user_version after each. After
// all pending steps succeed, it writes SchemaVersionKey to the meta table so
// the version is observable through the meta API too. Idempotent: a no-op on a
// DB that is already current (the meta write still runs, but the row's value
// won't change so its version counter won't bump).
func (s *Store) migrate() error {
	current, err := s.userVersion()
	if err != nil {
		return err
	}

	for v := current; v < len(migrations); v++ {
		if err := s.applyMigration(v); err != nil {
			return fmt.Errorf("migration to v%d: %w", v+1, err)
		}
	}

	// Mirror user_version into meta, once the meta table exists (it's created
	// in v2). A meta-less DB (only v1 applied) is tolerated as a no-op.
	if err := s.recordSchemaVersion(len(migrations)); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

// userVersion reads PRAGMA user_version.
func (s *Store) userVersion() (int, error) {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

// applyMigration runs migrations[idx] and bumps user_version to idx+1, all in a
// single transaction so a failure rolls the step back cleanly.
//
// Note: PRAGMA user_version can't be parameterised, but idx is a controlled
// integer index into our own slice, never user input — so the Sprintf is safe.
func (s *Store) applyMigration(idx int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed.

	if _, err := tx.Exec(migrations[idx]); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", idx+1)); err != nil {
		return err
	}
	return tx.Commit()
}

// recordSchemaVersion upserts SchemaVersionKey to the integer v in the meta
// table. Silently no-ops when the meta table doesn't exist yet (a v1-only DB,
// which is currently only the in-memory state between v1 and v2 applying).
func (s *Store) recordSchemaVersion(v int) error {
	if v <= 0 {
		return nil
	}
	hasMeta, err := s.tableExists("meta")
	if err != nil {
		return err
	}
	if !hasMeta {
		return nil
	}
	// Avoid bumping the version counter if the value hasn't changed: a re-open
	// of an already-current DB should be a clean no-op.
	got, err := s.MetaGet(SchemaVersionKey)
	if err == nil && got.Value == strconv.Itoa(v) {
		return nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.MetaSet(SchemaVersionKey, strconv.Itoa(v))
}

// tableExists reports whether a table with the given name exists in the schema.
func (s *Store) tableExists(name string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
	).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check table %q: %w", name, err)
	}
	return n == 1, nil
}
