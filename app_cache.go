package main

import (
	"encoding/json"

	"ytdlp-gui/internal/binaries"
	"ytdlp-gui/internal/db"
)

// ytdlpVersionCacheKey is the meta KV key under which the cached
// (file_hash, version) pair for yt-dlp lives.
const ytdlpVersionCacheKey = "binaries.yt-dlp.current"

// ytdlpVersionCacheValue is the JSON payload stored at ytdlpVersionCacheKey.
// A separate type (not an inline anonymous struct) keeps the on-disk shape
// reviewable and easy to extend.
type ytdlpVersionCacheValue struct {
	FileHash string `json:"file_hash"`
	Version  string `json:"version"`
}

// metaCache is the minimal db.Store slice the caching version runner needs.
// Defined as an interface so tests can supply an in-memory fake instead of
// opening a real database.
type metaCache interface {
	MetaGet(key string) (db.MetaEntry, error)
	MetaSet(key, value string) error
}

// cachingVersionRunner wraps a binaries.VersionRunner so that yt-dlp's
// `--version` output is cached in the meta KV table keyed by the binary's
// file hash. Running yt-dlp bootstraps a Python interpreter and takes several
// seconds; doing it once per upgrade rather than once per Settings open is a
// real UX win. Other binaries (ffmpeg/node) are fast enough that we always
// run them live.
//
// The cache is bypassed (live run, no write) when:
//   - store is nil (the DB failed to open at startup),
//   - skip() returns true (user-set escape hatch for wrapper-script setups
//     where the file hash does not track the underlying tool version),
//   - the spec is not yt-dlp,
//   - the file hash can't be computed (transient I/O blip mustn't poison the
//     cache, so we neither read nor write it in this case).
type cachingVersionRunner struct {
	inner binaries.VersionRunner
	store metaCache
	skip  func() bool
}

func (c cachingVersionRunner) Version(path string, spec binaries.Spec) (string, error) {
	if c.store == nil || spec.Name != "yt-dlp" || (c.skip != nil && c.skip()) {
		return c.inner.Version(path, spec)
	}

	hash, herr := binaries.FileSHA256(path)
	if herr != nil {
		return c.inner.Version(path, spec)
	}

	if entry, err := c.store.MetaGet(ytdlpVersionCacheKey); err == nil {
		var cached ytdlpVersionCacheValue
		if json.Unmarshal([]byte(entry.Value), &cached) == nil &&
			cached.FileHash == hash && cached.Version != "" {
			return cached.Version, nil
		}
	}

	version, runErr := c.inner.Version(path, spec)
	if runErr != nil {
		return "", runErr
	}
	if payload, err := json.Marshal(ytdlpVersionCacheValue{FileHash: hash, Version: version}); err == nil {
		_ = c.store.MetaSet(ytdlpVersionCacheKey, string(payload))
	}
	return version, nil
}
