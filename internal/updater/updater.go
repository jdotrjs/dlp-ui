// Package updater queries the GitHub releases API for the latest published
// release tag and compares it against the running app's version. We do not
// download or self-replace the binary — this is purely a "newer version
// exists, point the user at the release page" notifier.
//
// Versions are expected to look like vMAJOR.MINOR.PATCH or
// vMAJOR.MINOR.PATCH-rcN. A leading `v` is optional; the parser strips it.
//
// Caching: Load reads the last-known latest tag and check timestamp from a
// caller-supplied Store; Check fetches GitHub and writes them back. The
// caller (App) is responsible for emitting any UI event after Check returns
// — keeping this package free of Wails-runtime imports so it stays unit
// testable.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ReleasesURL is the GitHub REST endpoint for the latest released tag of
// this project. Exposed as a var so tests can point at a stub server.
var ReleasesURL = "https://api.github.com/repos/jdotrjs/dlp-ui/releases/latest"

// Meta KV keys used to persist update-check state. The key names match the
// scheme requested by the user: "ytdlp-ui.<kebab-name>".
const (
	KeyLatestVersion = "ytdlp-ui.latest-version"
	KeyUpdateCheckTS = "ytdlp-ui.update-check-ts"
)

// Info is the bound shape returned to the frontend. UpdateAvailable is derived
// from a fresh comparison of CurrentVersion vs LatestVersion each time, so
// upgrading the app naturally clears the badge without needing a stored flag.
type Info struct {
	CurrentVersionName string `json:"versionName"`
	CurrentVersion     string `json:"currentVersion"`
	LatestVersion      string `json:"latestVersion"`
	UpdateAvailable    bool   `json:"updateAvailable"`
	LastCheckedAt      int64  `json:"lastCheckedAt"`
	ReleaseURL         string `json:"releaseUrl"`
}

// Store is the persistence seam used by Load/Check. It is intentionally a
// tiny subset of db.Store (mapped via a shim in app.go) so this package
// doesn't pull in the db package and stays unit-testable with a map-backed
// fake.
//
// A nil Store is accepted by Load and Check: reads return "missing" and
// writes are dropped silently. That keeps the update-check flow working
// even when the library DB failed to open (the user can still trigger a
// manual check; it just won't persist this session).
type Store interface {
	Get(key string) (value string, ok bool, err error)
	Set(key, value string) error
}

// Load reads the cached LastChecked timestamp + LatestVersion from store and
// derives UpdateAvailable by comparing against currentVersion. Persistence
// errors are swallowed (treated as "not cached") since the user can always
// trigger a manual Check.
func Load(currentVersion, name string, store Store) Info {
	info := Info{CurrentVersionName: name, CurrentVersion: currentVersion}
	if store == nil {
		return info
	}
	if v, ok, _ := store.Get(KeyLatestVersion); ok {
		info.LatestVersion = v
	}
	if v, ok, _ := store.Get(KeyUpdateCheckTS); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			info.LastCheckedAt = n
		}
	}
	info.UpdateAvailable = isNewer(currentVersion, info.LatestVersion)
	return info
}

// Check fetches the latest release from GitHub, persists the new tag +
// timestamp via store (best-effort: write errors are dropped), and returns
// the resulting Info. Network/parse errors are returned to the caller so the
// UI can surface them; persistence failures are not propagated because they
// would just mean re-checking sooner on next launch.
func Check(ctx context.Context, currentVersion string, store Store) (Info, error) {
	tag, releaseURL, err := Fetch(ctx)
	if err != nil {
		return Info{}, err
	}
	ts := time.Now().Unix()
	if store != nil {
		_ = store.Set(KeyLatestVersion, tag)
		_ = store.Set(KeyUpdateCheckTS, strconv.FormatInt(ts, 10))
	}
	return Info{
		CurrentVersion:  currentVersion,
		LatestVersion:   tag,
		LastCheckedAt:   ts,
		ReleaseURL:      releaseURL,
		UpdateAvailable: isNewer(currentVersion, tag),
	}, nil
}

// IsStale reports whether the cached LastCheckedAt is older than d, or if no
// check has ever been recorded. Used by startup to decide whether to fire an
// auto-check.
func (i Info) IsStale(d time.Duration) bool {
	if i.LastCheckedAt == 0 {
		return true
	}
	return time.Since(time.Unix(i.LastCheckedAt, 0)) > d
}

// isNewer is the swallow-error wrapper used inside this package: a parse
// failure (e.g. a malformed upstream tag) collapses to "no update available"
// rather than blowing up the caller. Callers that need the error use Newer.
// A current build of "dev" is always the newest.
func isNewer(current, latest string) bool {
	if current == "old" {
		return true
	}
	if current == "dev" || latest == "" {
		return false
	}
	newer, err := Newer(current, latest)
	if err != nil {
		return false
	}
	return newer
}

// Fetch hits the GitHub releases endpoint and returns the tag_name and the
// human-facing release URL. The 10s timeout keeps a slow/down GitHub from
// blocking startup; callers run this off the main goroutine anyway.
func Fetch(ctx context.Context) (tag, url string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github returned status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("decode github response: %w", err)
	}
	if body.TagName == "" {
		return "", "", errors.New("github response had no tag_name")
	}
	return body.TagName, body.HTMLURL, nil
}

// version is the parsed representation of a tag. hasRC distinguishes a release
// (1.2.3) from a release candidate (1.2.3-rc4); a release sorts above any
// prerelease of the same MAJOR.MINOR.PATCH per semver.
type version struct {
	major, minor, patch int
	rc                  int
	hasRC               bool
}

// parse accepts "vX.Y.Z" or "vX.Y.Z-rcN" (with or without the leading v) and
// rejects anything else. We deliberately don't support arbitrary prerelease
// identifiers — the project commits to rc-only and rejecting unknown forms
// surfaces typos in release tags instead of silently mis-ordering them.
func parse(s string) (version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return version{}, errors.New("empty version")
	}

	core, pre := s, ""
	if i := strings.Index(s, "-"); i >= 0 {
		core, pre = s[:i], s[i+1:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("version %q: want MAJOR.MINOR.PATCH", s)
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return version{}, fmt.Errorf("version %q: major: %w", s, err)
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return version{}, fmt.Errorf("version %q: minor: %w", s, err)
	}
	pat, err := strconv.Atoi(parts[2])
	if err != nil {
		return version{}, fmt.Errorf("version %q: patch: %w", s, err)
	}
	v := version{major: maj, minor: min, patch: pat}

	if pre != "" {
		if !strings.HasPrefix(pre, "rc") {
			return version{}, fmt.Errorf("version %q: unsupported prerelease %q (expected rcN)", s, pre)
		}
		n, err := strconv.Atoi(pre[2:])
		if err != nil || n < 0 {
			return version{}, fmt.Errorf("version %q: invalid rc number %q", s, pre)
		}
		v.rc = n
		v.hasRC = true
	}
	return v, nil
}

// compare returns -1, 0, +1 for a<b, a==b, a>b. Release > prerelease at the
// same X.Y.Z; among prereleases, larger rc number is newer.
func compare(a, b version) int {
	if a.major != b.major {
		return sign(a.major - b.major)
	}
	if a.minor != b.minor {
		return sign(a.minor - b.minor)
	}
	if a.patch != b.patch {
		return sign(a.patch - b.patch)
	}
	switch {
	case !a.hasRC && !b.hasRC:
		return 0
	case !a.hasRC && b.hasRC:
		return 1
	case a.hasRC && !b.hasRC:
		return -1
	}
	return sign(a.rc - b.rc)
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}

// Newer reports whether latest is strictly newer than current. A parse error
// in either version is returned to the caller rather than silently treated as
// "no update" so callers (tests, debugging) can see the cause.
func Newer(current, latest string) (bool, error) {
	c, err := parse(current)
	if err != nil {
		return false, fmt.Errorf("current: %w", err)
	}
	l, err := parse(latest)
	if err != nil {
		return false, fmt.Errorf("latest: %w", err)
	}
	return compare(l, c) > 0, nil
}
