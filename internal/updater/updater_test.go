package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		cur, lat string
		want     bool
	}{
		// rc -> rc
		{"v0.0.0-rc0", "v0.0.0-rc0", false},
		{"v0.0.0-rc0", "v0.0.0-rc1", true},
		{"v0.0.0-rc1", "v0.0.0-rc0", false},
		// release outranks rc at same x.y.z
		{"v0.0.0-rc1", "v0.0.0", true},
		{"v0.0.0", "v0.0.0-rc5", false},
		// across MAJOR/MINOR/PATCH
		{"v0.1.0", "v1.0.0-rc1", true},
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.3", "v1.2.3", false},
		// leading v is optional
		{"0.0.0-rc0", "v0.0.0-rc1", true},
	}
	for _, c := range cases {
		got, err := Newer(c.cur, c.lat)
		if err != nil {
			t.Errorf("Newer(%q,%q): unexpected err: %v", c.cur, c.lat, err)
			continue
		}
		if got != c.want {
			t.Errorf("Newer(%q,%q) = %v, want %v", c.cur, c.lat, got, c.want)
		}
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	bad := []string{
		"",
		"v",
		"1.2",
		"1.2.3.4",
		"1.2.3-beta1",
		"1.2.3-rc",
		"1.2.3-rcX",
	}
	for _, s := range bad {
		if _, err := parse(s); err == nil {
			t.Errorf("parse(%q) expected error, got nil", s)
		}
	}
}

// mapStore is an in-memory Store used by Load/Check tests.
type mapStore map[string]string

func (m mapStore) Get(key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}

func (m mapStore) Set(key, value string) error {
	m[key] = value
	return nil
}

func TestLoadEmptyStore(t *testing.T) {
	info := Load("v0.0.0-rc0", mapStore{})
	if info.CurrentVersion != "v0.0.0-rc0" {
		t.Errorf("CurrentVersion=%q", info.CurrentVersion)
	}
	if info.LatestVersion != "" || info.LastCheckedAt != 0 || info.UpdateAvailable {
		t.Errorf("expected zero state, got %+v", info)
	}
}

func TestLoadNilStore(t *testing.T) {
	info := Load("v0.0.0-rc0", nil)
	if info.CurrentVersion != "v0.0.0-rc0" {
		t.Errorf("CurrentVersion=%q", info.CurrentVersion)
	}
	if info.UpdateAvailable {
		t.Errorf("expected UpdateAvailable=false with nil store")
	}
}

func TestLoadDerivesUpdateAvailable(t *testing.T) {
	s := mapStore{
		KeyLatestVersion: "v0.0.0-rc1",
		KeyUpdateCheckTS: "1700000000",
	}
	info := Load("v0.0.0-rc0", s)
	if !info.UpdateAvailable {
		t.Errorf("expected UpdateAvailable=true; info=%+v", info)
	}
	if info.LastCheckedAt != 1700000000 {
		t.Errorf("LastCheckedAt=%d", info.LastCheckedAt)
	}
	if info.LatestVersion != "v0.0.0-rc1" {
		t.Errorf("LatestVersion=%q", info.LatestVersion)
	}
}

func TestCheckPersistsAndReturnsInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v0.0.0-rc1",
			"html_url": "https://example.test/release",
		})
	}))
	defer srv.Close()

	orig := ReleasesURL
	ReleasesURL = srv.URL
	defer func() { ReleasesURL = orig }()

	store := mapStore{}
	info, err := Check(context.Background(), "v0.0.0-rc0", store)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if info.LatestVersion != "v0.0.0-rc1" || !info.UpdateAvailable {
		t.Errorf("unexpected info: %+v", info)
	}
	if info.ReleaseURL != "https://example.test/release" {
		t.Errorf("ReleaseURL=%q", info.ReleaseURL)
	}
	if store[KeyLatestVersion] != "v0.0.0-rc1" {
		t.Errorf("store latest=%q", store[KeyLatestVersion])
	}
	if _, err := strconv.ParseInt(store[KeyUpdateCheckTS], 10, 64); err != nil {
		t.Errorf("store ts not numeric: %q (%v)", store[KeyUpdateCheckTS], err)
	}
}

func TestIsStale(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		ts   int64
		dur  time.Duration
		want bool
	}{
		{0, time.Hour, true},                                // never checked
		{now, time.Hour, false},                             // just checked
		{now - int64((2 * time.Hour).Seconds()), time.Hour, true},
	}
	for _, c := range cases {
		got := Info{LastCheckedAt: c.ts}.IsStale(c.dur)
		if got != c.want {
			t.Errorf("IsStale(ts=%d, dur=%v) = %v, want %v", c.ts, c.dur, got, c.want)
		}
	}
}
