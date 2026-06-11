package db

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// openTestStore opens a Store in a fresh temp dir and registers cleanup.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "lib", "data.db")
	s, fresh, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !fresh {
		t.Errorf("Open of new path: freshlyCreated = false, want true")
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenMigratesFromEmpty(t *testing.T) {
	s := openTestStore(t)

	v, err := s.userVersion()
	if err != nil {
		t.Fatalf("userVersion: %v", err)
	}
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}

	// The WAL files should appear next to the DB after activity.
	if _, err := s.InsertContent(Content{Source: "youtube", SourceID: "x", Title: "t"}); err != nil {
		t.Fatalf("InsertContent: %v", err)
	}
	walPath := s.Path() + "-wal"
	if _, err := os.Stat(walPath); err != nil {
		t.Errorf("expected WAL file at %q: %v", walPath, err)
	}
}

func TestOpenExistingNotFresh(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	s1, fresh1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if !fresh1 {
		t.Error("first Open: want freshlyCreated true")
	}
	s1.Close()

	s2, fresh2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()
	if fresh2 {
		t.Error("second Open: want freshlyCreated false")
	}
	// Re-migrating an existing DB must be a no-op (idempotent).
	if v, _ := s2.userVersion(); v != len(migrations) {
		t.Errorf("user_version after reopen = %d, want %d", v, len(migrations))
	}
}

func TestMigrationRecordsSchemaVersion(t *testing.T) {
	s := openTestStore(t)
	got, err := s.MetaGet(SchemaVersionKey)
	if err != nil {
		t.Fatalf("MetaGet schema version: %v", err)
	}
	if want := strconv.Itoa(len(migrations)); got.Value != want {
		t.Errorf("schema-version meta = %q, want %q", got.Value, want)
	}
	if got.Version < 1 {
		t.Errorf("schema-version meta row version = %d, want >= 1", got.Version)
	}

	// Reopening must not bump the optimistic counter (value unchanged).
	v1 := got.Version
	s.Close()
	s2, _, err := Open(s.Path())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got2, _ := s2.MetaGet(SchemaVersionKey)
	if got2.Version != v1 {
		t.Errorf("schema-version version after reopen = %d, want %d (no bump)", got2.Version, v1)
	}
}

func TestMetaSetAndGet(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.MetaGet("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("MetaGet missing: err = %v, want ErrNotFound", err)
	}

	if err := s.MetaSet("k", "v1"); err != nil {
		t.Fatalf("MetaSet first: %v", err)
	}
	got, _ := s.MetaGet("k")
	if got.Value != "v1" || got.Version != 1 {
		t.Errorf("after first set: %+v, want value=v1 version=1", got)
	}

	if err := s.MetaSet("k", "v2"); err != nil {
		t.Fatalf("MetaSet second: %v", err)
	}
	got, _ = s.MetaGet("k")
	if got.Value != "v2" || got.Version != 2 {
		t.Errorf("after second set: %+v, want value=v2 version=2", got)
	}
}

func TestThumbnailRoundTrip(t *testing.T) {
	s := openTestStore(t)
	cid, err := s.InsertContent(Content{Source: "youtube", SourceID: "thumb"})
	if err != nil {
		t.Fatalf("InsertContent: %v", err)
	}

	// Missing => ErrNotFound.
	if _, err := s.GetThumbnail(cid); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetThumbnail missing: err = %v, want ErrNotFound", err)
	}
	if has, _ := s.HasThumbnail(cid); has {
		t.Errorf("HasThumbnail missing = true, want false")
	}

	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10} // JPEG-ish header
	in := Thumbnail{
		ContentID: cid,
		Mime:      "image/jpeg",
		Data:      data,
		SourceURL: "http://example/image.jpg",
		FetchedAt: 1700000000,
	}
	if err := s.SaveThumbnail(in); err != nil {
		t.Fatalf("SaveThumbnail: %v", err)
	}
	if has, _ := s.HasThumbnail(cid); !has {
		t.Errorf("HasThumbnail after save = false, want true")
	}
	out, err := s.GetThumbnail(cid)
	if err != nil {
		t.Fatalf("GetThumbnail: %v", err)
	}
	if out.Mime != in.Mime || out.SourceURL != in.SourceURL || out.FetchedAt != in.FetchedAt {
		t.Errorf("round-trip header mismatch:\n got %+v\nwant %+v", out, in)
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Errorf("round-trip data mismatch: got %x, want %x", out.Data, in.Data)
	}

	// Upsert replaces in place.
	in2 := in
	in2.Data = []byte{0x00, 0x01, 0x02}
	in2.Mime = "image/png"
	in2.FetchedAt = 1700000100
	if err := s.SaveThumbnail(in2); err != nil {
		t.Fatalf("SaveThumbnail replace: %v", err)
	}
	out, _ = s.GetThumbnail(cid)
	if !bytes.Equal(out.Data, in2.Data) || out.Mime != "image/png" {
		t.Errorf("after replace: %+v, want mime=image/png data=%x", out, in2.Data)
	}
}

func TestThumbnailCascadesOnContentDelete(t *testing.T) {
	s := openTestStore(t)
	cid, _ := s.InsertContent(Content{Source: "youtube", SourceID: "cascade"})
	if err := s.SaveThumbnail(Thumbnail{ContentID: cid, Data: []byte{1, 2, 3}}); err != nil {
		t.Fatalf("SaveThumbnail: %v", err)
	}

	if err := s.DeleteContent(cid, false); err != nil {
		t.Fatalf("DeleteContent: %v", err)
	}
	if has, _ := s.HasThumbnail(cid); has {
		t.Errorf("thumbnail not cascaded after content delete")
	}
}

func TestSaveThumbnailRejectsEmpty(t *testing.T) {
	s := openTestStore(t)
	cid, _ := s.InsertContent(Content{Source: "youtube", SourceID: "empty"})

	if err := s.SaveThumbnail(Thumbnail{ContentID: 0, Data: []byte{1}}); err == nil {
		t.Error("SaveThumbnail with zero ContentID: want error")
	}
	if err := s.SaveThumbnail(Thumbnail{ContentID: cid, Data: nil}); err == nil {
		t.Error("SaveThumbnail with empty data: want error")
	}
}

func TestContentRoundTrip(t *testing.T) {
	s := openTestStore(t)

	in := Content{
		Source: "youtube", SourceID: "abc123", Title: "Hello",
		Uploader: "Chan", Duration: 125, UploadDate: "20240101",
		ThumbnailURL: "http://t/x.jpg", Ext: "mp4", Filesize: 1024,
		SourceURL: "http://yt/abc123", Filepath: "/tmp/hello.mp4",
		Status: StatusCompleted, PlaylistIndex: 3, DownloadedAt: 1700000000,
	}
	id, err := s.InsertContent(in)
	if err != nil {
		t.Fatalf("InsertContent: %v", err)
	}

	got, err := s.GetContent(id)
	if err != nil {
		t.Fatalf("GetContent: %v", err)
	}
	in.ID = id
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}

	if _, err := s.GetContent(99999); err != ErrNotFound {
		t.Errorf("GetContent(missing) err = %v, want ErrNotFound", err)
	}
}

func TestStatusDefaultsToCompleted(t *testing.T) {
	s := openTestStore(t)
	id, err := s.InsertContent(Content{Source: "youtube", SourceID: "nodef"})
	if err != nil {
		t.Fatalf("InsertContent: %v", err)
	}
	got, _ := s.GetContent(id)
	if got.Status != StatusCompleted {
		t.Errorf("default status = %q, want %q", got.Status, StatusCompleted)
	}
}

func TestUpsertContentIdempotent(t *testing.T) {
	s := openTestStore(t)

	id1, err := s.UpsertContent(Content{Source: "youtube", SourceID: "dup", Title: "v1", Status: StatusDownloading})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	id2, err := s.UpsertContent(Content{Source: "youtube", SourceID: "dup", Title: "v2", Status: StatusCompleted})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id1 != id2 {
		t.Errorf("upsert ids differ: %d vs %d (should update in place)", id1, id2)
	}
	got, _ := s.GetContent(id1)
	if got.Title != "v2" || got.Status != StatusCompleted {
		t.Errorf("upsert did not update: %+v", got)
	}

	// Same source_id under a DIFFERENT source must be a distinct row.
	id3, err := s.UpsertContent(Content{Source: "vimeo", SourceID: "dup", Title: "other"})
	if err != nil {
		t.Fatalf("cross-source upsert: %v", err)
	}
	if id3 == id1 {
		t.Errorf("cross-source upsert collided: both id %d", id1)
	}

	// Empty source_id rows are always distinct inserts.
	a, _ := s.UpsertContent(Content{Source: "youtube", Title: "loose1"})
	b, _ := s.UpsertContent(Content{Source: "youtube", Title: "loose2"})
	if a == b {
		t.Errorf("empty-source_id upserts collided: both id %d", a)
	}
}

func TestUpsertPlaylistIdempotent(t *testing.T) {
	s := openTestStore(t)
	id1, err := s.UpsertPlaylist(Playlist{Source: "youtube", SourceID: "PL1", Title: "Mix", ItemCount: 2})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	id2, err := s.UpsertPlaylist(Playlist{Source: "youtube", SourceID: "PL1", Title: "Mix renamed", ItemCount: 5})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if id1 != id2 {
		t.Errorf("playlist upsert ids differ: %d vs %d", id1, id2)
	}
	p, _ := s.GetPlaylist(id1)
	if p.Title != "Mix renamed" || p.ItemCount != 5 {
		t.Errorf("playlist not updated: %+v", p)
	}
}

func TestForeignKeySetNullOnPlaylistDelete(t *testing.T) {
	s := openTestStore(t)

	pid, err := s.UpsertPlaylist(Playlist{Source: "youtube", SourceID: "PLx", Title: "P"})
	if err != nil {
		t.Fatalf("UpsertPlaylist: %v", err)
	}
	cid, err := s.InsertContent(Content{Source: "youtube", SourceID: "vid", PlaylistID: pid, Title: "in-pl"})
	if err != nil {
		t.Fatalf("InsertContent: %v", err)
	}

	// Joined title is populated while the playlist exists.
	got, _ := s.GetContent(cid)
	if got.PlaylistID != pid || got.PlaylistTitle != "P" {
		t.Fatalf("before delete: PlaylistID=%d PlaylistTitle=%q", got.PlaylistID, got.PlaylistTitle)
	}

	if _, err := s.db.Exec("DELETE FROM playlists WHERE id = ?", pid); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}

	// ON DELETE SET NULL: the content row survives with a null FK (read as 0).
	got, err = s.GetContent(cid)
	if err != nil {
		t.Fatalf("GetContent after playlist delete: %v", err)
	}
	if got.PlaylistID != 0 {
		t.Errorf("PlaylistID after delete = %d, want 0 (SET NULL)", got.PlaylistID)
	}
	if got.PlaylistTitle != "" {
		t.Errorf("PlaylistTitle after delete = %q, want empty", got.PlaylistTitle)
	}
}

func TestDeleteContent(t *testing.T) {
	s := openTestStore(t)

	// Create a real file so deleteFile=true can remove it.
	dir := t.TempDir()
	fp := filepath.Join(dir, "v.mp4")
	if err := os.WriteFile(fp, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := s.InsertContent(Content{Source: "youtube", SourceID: "del", Filepath: fp})
	if err != nil {
		t.Fatalf("InsertContent: %v", err)
	}

	if err := s.DeleteContent(id, true); err != nil {
		t.Fatalf("DeleteContent: %v", err)
	}
	if _, err := s.GetContent(id); err != ErrNotFound {
		t.Errorf("row still present after delete: %v", err)
	}
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Errorf("file not deleted: stat err = %v", err)
	}

	if err := s.DeleteContent(id, false); err != ErrNotFound {
		t.Errorf("delete of missing row err = %v, want ErrNotFound", err)
	}
}

func TestDeleteContentKeepsFile(t *testing.T) {
	s := openTestStore(t)
	dir := t.TempDir()
	fp := filepath.Join(dir, "keep.mp4")
	if err := os.WriteFile(fp, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _ := s.InsertContent(Content{Source: "youtube", SourceID: "keep", Filepath: fp})
	if err := s.DeleteContent(id, false); err != nil {
		t.Fatalf("DeleteContent: %v", err)
	}
	if _, err := os.Stat(fp); err != nil {
		t.Errorf("file should remain when deleteFile=false: %v", err)
	}
}

// seedSortable inserts rows with deterministic, distinct sort keys.
func seedSortable(t *testing.T, s *Store) {
	t.Helper()
	rows := []Content{
		{Source: "vimeo", SourceID: "1", Title: "Banana", Uploader: "Zed", UploadDate: "20230101", DownloadedAt: 100},
		{Source: "youtube", SourceID: "2", Title: "Apple", Uploader: "Yan", UploadDate: "20240101", DownloadedAt: 300},
		{Source: "twitch", SourceID: "3", Title: "Cherry", Uploader: "Xio", UploadDate: "20220101", DownloadedAt: 200},
	}
	for _, r := range rows {
		if _, err := s.InsertContent(r); err != nil {
			t.Fatalf("seed insert: %v", err)
		}
	}
}

func titles(items []Content) []string {
	out := make([]string, len(items))
	for i, c := range items {
		out[i] = c.Title
	}
	return out
}

func TestListSort(t *testing.T) {
	s := openTestStore(t)
	seedSortable(t, s)

	cases := []struct {
		sortBy, sortDir string
		wantTitles      []string
	}{
		{"title", "asc", []string{"Apple", "Banana", "Cherry"}},
		{"title", "desc", []string{"Cherry", "Banana", "Apple"}},
		{"channel", "asc", []string{"Cherry", "Apple", "Banana"}}, // Xio<Yan<Zed
		{"channel", "desc", []string{"Banana", "Apple", "Cherry"}},
		{"source", "asc", []string{"Cherry", "Banana", "Apple"}}, // twitch<vimeo<youtube
		{"source", "desc", []string{"Apple", "Banana", "Cherry"}},
		{"publishDate", "asc", []string{"Cherry", "Banana", "Apple"}}, // 2022<2023<2024
		{"publishDate", "desc", []string{"Apple", "Banana", "Cherry"}},
		{"downloadDate", "asc", []string{"Banana", "Cherry", "Apple"}}, // 100<200<300
		{"downloadDate", "desc", []string{"Apple", "Cherry", "Banana"}},
		// Unknown key falls back to downloadDate; unknown dir falls back desc.
		{"bogus", "weird", []string{"Apple", "Cherry", "Banana"}},
	}
	for _, c := range cases {
		res, err := s.ListContent(ListOptions{SortBy: c.sortBy, SortDir: c.sortDir})
		if err != nil {
			t.Fatalf("ListContent(%s,%s): %v", c.sortBy, c.sortDir, err)
		}
		got := titles(res.Items)
		if len(got) != len(c.wantTitles) {
			t.Fatalf("%s/%s: len %d, want %d", c.sortBy, c.sortDir, len(got), len(c.wantTitles))
		}
		for i := range got {
			if got[i] != c.wantTitles[i] {
				t.Errorf("%s/%s: got %v, want %v", c.sortBy, c.sortDir, got, c.wantTitles)
				break
			}
		}
		if res.Total != 3 {
			t.Errorf("%s/%s: Total = %d, want 3", c.sortBy, c.sortDir, res.Total)
		}
	}
}

func TestListPagination(t *testing.T) {
	s := openTestStore(t)
	// 10 rows, downloadedAt 0..9; default sort downloadDate desc => 9..0.
	for i := 0; i < 10; i++ {
		if _, err := s.InsertContent(Content{
			Source: "youtube", SourceID: itoa(i),
			Title: "T" + itoa(i), DownloadedAt: int64(i),
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	p1, err := s.ListContent(ListOptions{Limit: 3, Offset: 0})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if p1.Total != 10 {
		t.Errorf("Total = %d, want 10", p1.Total)
	}
	if len(p1.Items) != 3 {
		t.Fatalf("page1 len = %d, want 3", len(p1.Items))
	}
	if p1.Items[0].Title != "T9" || p1.Items[2].Title != "T7" {
		t.Errorf("page1 titles = %v", titles(p1.Items))
	}

	p2, _ := s.ListContent(ListOptions{Limit: 3, Offset: 3})
	if p2.Items[0].Title != "T6" || p2.Items[2].Title != "T4" {
		t.Errorf("page2 titles = %v", titles(p2.Items))
	}

	// Last partial page.
	p4, _ := s.ListContent(ListOptions{Limit: 3, Offset: 9})
	if len(p4.Items) != 1 || p4.Items[0].Title != "T0" {
		t.Errorf("last page = %v", titles(p4.Items))
	}

	// No limit => all rows.
	all, _ := s.ListContent(ListOptions{})
	if len(all.Items) != 10 {
		t.Errorf("no-limit len = %d, want 10", len(all.Items))
	}
}

func TestListFilters(t *testing.T) {
	s := openTestStore(t)
	pid, _ := s.UpsertPlaylist(Playlist{Source: "youtube", SourceID: "PL", Title: "P"})
	s.InsertContent(Content{Source: "youtube", SourceID: "a", Title: "A", PlaylistID: pid})
	s.InsertContent(Content{Source: "youtube", SourceID: "b", Title: "B"})
	s.InsertContent(Content{Source: "vimeo", SourceID: "c", Title: "C"})

	bySource, _ := s.ListContent(ListOptions{Source: "youtube"})
	if bySource.Total != 2 || len(bySource.Items) != 2 {
		t.Errorf("source filter: total=%d items=%d, want 2/2", bySource.Total, len(bySource.Items))
	}

	byPL, _ := s.ListContent(ListOptions{PlaylistID: pid})
	if byPL.Total != 1 || byPL.Items[0].Title != "A" {
		t.Errorf("playlist filter: total=%d, want 1 (A)", byPL.Total)
	}

	// Filter + pagination total is post-filter, pre-page.
	page, _ := s.ListContent(ListOptions{Source: "youtube", Limit: 1})
	if page.Total != 2 || len(page.Items) != 1 {
		t.Errorf("filtered page: total=%d items=%d, want 2/1", page.Total, len(page.Items))
	}
}

// itoa avoids importing strconv just for tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
