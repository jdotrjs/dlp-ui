package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ytdlp-gui/internal/binaries"
	"ytdlp-gui/internal/db"
)

// fakeMetaCache is an in-memory metaCache for runner tests. It also counts
// reads/writes so tests can prove the runner is hitting the cache (or not).
type fakeMetaCache struct {
	entry  db.MetaEntry
	exists bool
	gets   int
	sets   int
	getErr error
	setErr error
}

func (f *fakeMetaCache) MetaGet(_ string) (db.MetaEntry, error) {
	f.gets++
	if f.getErr != nil {
		return db.MetaEntry{}, f.getErr
	}
	if !f.exists {
		return db.MetaEntry{}, db.ErrNotFound
	}
	return f.entry, nil
}

func (f *fakeMetaCache) MetaSet(_, value string) error {
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	f.entry = db.MetaEntry{Key: ytdlpVersionCacheKey, Value: value, Version: f.entry.Version + 1}
	f.exists = true
	return nil
}

// countingRunner is a binaries.VersionRunner that records every call and
// returns a canned value (or error).
type countingRunner struct {
	calls   int
	version string
	err     error
}

func (c *countingRunner) Version(_ string, _ binaries.Spec) (string, error) {
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	return c.version, nil
}

// writeBin creates a fake yt-dlp executable so the runner can hash it.
func writeBin(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}
	return path
}

func ytdlpSpec() binaries.Spec { return binaries.Spec{Name: "yt-dlp"} }

func TestCachingRunnerMissThenHit(t *testing.T) {
	bin := writeBin(t, "fake yt-dlp v1\n")
	cache := &fakeMetaCache{}
	inner := &countingRunner{version: "2024.01.01"}
	r := cachingVersionRunner{inner: inner, store: cache}

	// First call: cache empty → runs the inner, writes the cache.
	v, err := r.Version(bin, ytdlpSpec())
	if err != nil || v != "2024.01.01" {
		t.Fatalf("miss: v=%q err=%v", v, err)
	}
	if inner.calls != 1 {
		t.Errorf("miss: inner.calls = %d, want 1", inner.calls)
	}
	if cache.sets != 1 {
		t.Errorf("miss: cache.sets = %d, want 1", cache.sets)
	}

	// Second call with the same binary: cache hit → inner is NOT consulted.
	v, err = r.Version(bin, ytdlpSpec())
	if err != nil || v != "2024.01.01" {
		t.Fatalf("hit: v=%q err=%v", v, err)
	}
	if inner.calls != 1 {
		t.Errorf("hit: inner.calls = %d, want 1 (must not re-run)", inner.calls)
	}
	if cache.sets != 1 {
		t.Errorf("hit: cache.sets = %d, want 1 (must not rewrite)", cache.sets)
	}

	// Sanity: stored payload is the expected shape.
	var payload ytdlpVersionCacheValue
	if err := json.Unmarshal([]byte(cache.entry.Value), &payload); err != nil {
		t.Fatalf("cached payload not JSON: %v (%q)", err, cache.entry.Value)
	}
	if payload.Version != "2024.01.01" || payload.FileHash == "" {
		t.Errorf("payload = %+v, want version + non-empty file_hash", payload)
	}
}

func TestCachingRunnerHashMismatchReruns(t *testing.T) {
	bin := writeBin(t, "fake yt-dlp v1\n")
	cache := &fakeMetaCache{}
	inner := &countingRunner{version: "2024.01.01"}
	r := cachingVersionRunner{inner: inner, store: cache}

	if _, err := r.Version(bin, ytdlpSpec()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate a yt-dlp upgrade: rewrite the binary, change the runner's
	// version output. The cache must NOT serve the stale entry.
	if err := os.WriteFile(bin, []byte("fake yt-dlp v2\n"), 0o755); err != nil {
		t.Fatalf("upgrade bin: %v", err)
	}
	inner.version = "2025.06.10"

	v, err := r.Version(bin, ytdlpSpec())
	if err != nil || v != "2025.06.10" {
		t.Fatalf("post-upgrade: v=%q err=%v", v, err)
	}
	if inner.calls != 2 {
		t.Errorf("inner.calls = %d, want 2 (hash mismatch must rerun)", inner.calls)
	}
	if cache.sets != 2 {
		t.Errorf("cache.sets = %d, want 2 (must rewrite with new hash)", cache.sets)
	}
}

func TestCachingRunnerSkipFlagBypassesCache(t *testing.T) {
	bin := writeBin(t, "fake yt-dlp v1\n")
	cache := &fakeMetaCache{
		exists: true,
		entry: db.MetaEntry{
			// Doesn't matter what's in here — skip=true means we never look.
			Value: `{"file_hash":"deadbeef","version":"cached-version"}`,
		},
	}
	inner := &countingRunner{version: "live-version"}
	r := cachingVersionRunner{inner: inner, store: cache, skip: func() bool { return true }}

	v, err := r.Version(bin, ytdlpSpec())
	if err != nil || v != "live-version" {
		t.Fatalf("skip: v=%q err=%v", v, err)
	}
	if inner.calls != 1 {
		t.Errorf("inner.calls = %d, want 1", inner.calls)
	}
	if cache.gets != 0 || cache.sets != 0 {
		t.Errorf("cache touched while skip=true: gets=%d sets=%d", cache.gets, cache.sets)
	}
}

func TestCachingRunnerNonYtdlpPassthrough(t *testing.T) {
	bin := writeBin(t, "fake ffmpeg\n")
	cache := &fakeMetaCache{}
	inner := &countingRunner{version: "ffmpeg version 6.0"}
	r := cachingVersionRunner{inner: inner, store: cache}

	v, err := r.Version(bin, binaries.Spec{Name: "ffmpeg"})
	if err != nil || v != "ffmpeg version 6.0" {
		t.Fatalf("passthrough: v=%q err=%v", v, err)
	}
	if cache.gets != 0 || cache.sets != 0 {
		t.Errorf("ffmpeg touched the cache: gets=%d sets=%d", cache.gets, cache.sets)
	}
}

func TestCachingRunnerInnerErrorNotCached(t *testing.T) {
	bin := writeBin(t, "fake yt-dlp v1\n")
	cache := &fakeMetaCache{}
	want := errors.New("boom")
	inner := &countingRunner{err: want}
	r := cachingVersionRunner{inner: inner, store: cache}

	_, err := r.Version(bin, ytdlpSpec())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if cache.sets != 0 {
		t.Errorf("cache.sets = %d, want 0 (must not cache failures)", cache.sets)
	}
}

func TestCachingRunnerWithRealStore(t *testing.T) {
	// Round-trips through the real db.Store so the meta SQL works end-to-end.
	dbPath := filepath.Join(t.TempDir(), "lib", "library.db")
	store, _, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	bin := writeBin(t, "fake yt-dlp v1\n")
	inner := &countingRunner{version: "2024.01.01"}
	r := cachingVersionRunner{inner: inner, store: store}

	if _, err := r.Version(bin, ytdlpSpec()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := r.Version(bin, ytdlpSpec()); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1 (real store should serve the hit)", inner.calls)
	}

	entry, err := store.MetaGet(ytdlpVersionCacheKey)
	if err != nil {
		t.Fatalf("MetaGet: %v", err)
	}
	if entry.Version < 1 {
		t.Errorf("entry.Version = %d, want >= 1", entry.Version)
	}
}
