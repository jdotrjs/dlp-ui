// Package db owns the SQLite-backed catalogue: every table the app uses, the
// migration runner that brings the schema up to date, and the typed read/write
// API for those tables. Today that's the library (playlists, content), a meta
// KV store, and content thumbnails; future features that need persistence are
// expected to live in this package too.
//
// It wraps a single *sql.DB pool (modernc.org/sqlite, a pure-Go driver — no
// cgo, no FTS5) opened in WAL mode so reads and writes can run concurrently.
// The schema is versioned via PRAGMA user_version with an append-only list of
// migrations, each applied in its own transaction. Fuzzy search is NOT done in
// the database (no FTS5); it runs client-side over the rows currently paged in.
//
// Concurrency: open the Store exactly once (in app.startup), share the single
// *Store across every service, and Close it in shutdown. *sql.DB is itself a
// concurrency-safe pool, so no per-call open/close.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	// Pure-Go SQLite driver registered under the name "sqlite".
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row lookup by primary key matches nothing.
// Every Get* method uses this sentinel so callers can do a single
// errors.Is(err, db.ErrNotFound) check across tables.
var ErrNotFound = errors.New("db: not found")

// Store is the handle onto the database. The zero value is not usable;
// construct it with Open.
type Store struct {
	db *sql.DB
	// path is the resolved DB file path, kept for diagnostics/logging.
	path string
}

// dsn builds the connection string. WAL lets readers and a writer proceed
// concurrently; synchronous(NORMAL) is the recommended pairing with WAL;
// busy_timeout avoids spurious "database is locked" under contention;
// foreign_keys(1) enforces the FKs (off by default in SQLite); _txlock=immediate
// takes the write lock at BEGIN so a read txn that upgrades to a write can't
// deadlock against another writer.
func dsn(dbPath string) string {
	return "file:" + dbPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_txlock=immediate"
}

// Open opens (creating if absent) the database at dbPath. It MkdirAll's the
// parent directory first, so a fresh download dir works on first run.
//
// freshlyCreated reports whether no database existed at dbPath before this call
// — the app uses it to warn when an expected DB wasn't found (e.g. the
// configured path points at removable media that's gone). If MkdirAll or Open
// fails (parent unwritable, media missing), the error is returned and the
// caller should keep the Store unavailable rather than crash.
func Open(dbPath string) (store *Store, freshlyCreated bool, err error) {
	dir := filepath.Dir(dbPath)
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return nil, false, fmt.Errorf("create db dir %q: %w", dir, mkErr)
	}

	// Detect fresh-creation before opening: modernc creates the file on first
	// connection, so we must check existence up front.
	if _, statErr := os.Stat(dbPath); errors.Is(statErr, os.ErrNotExist) {
		freshlyCreated = true
	}

	conn, err := sql.Open("sqlite", dsn(dbPath))
	if err != nil {
		return nil, false, fmt.Errorf("open db %q: %w", dbPath, err)
	}
	// sql.Open is lazy; force a real connection so a bad path/permission fails
	// here (at startup) rather than on the first query.
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, false, fmt.Errorf("connect db %q: %w", dbPath, err)
	}

	s := &Store{db: conn, path: dbPath}
	if err := s.migrate(); err != nil {
		conn.Close()
		return nil, false, fmt.Errorf("migrate db %q: %w", dbPath, err)
	}
	return s, freshlyCreated, nil
}

// Path returns the resolved database file path.
func (s *Store) Path() string { return s.path }

// Close closes the underlying connection pool. Safe to call once at shutdown.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
