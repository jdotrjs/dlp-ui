package download

import (
	"strconv"
	"strings"
)

// normalizeExtractor lowercases yt-dlp's extractor key and trims any ":" suffix
// (e.g. "youtube:tab" → "youtube"), matching the library `source` convention
// and the metadata parser's normalizeSource.
func normalizeExtractor(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// joinIndices renders a 1-based index slice as yt-dlp's --playlist-items value
// (comma-joined). Empty slice → "" (single video / whole URL).
func joinIndices(idx []int) string {
	if len(idx) == 0 {
		return ""
	}
	parts := make([]string, len(idx))
	for i, n := range idx {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
