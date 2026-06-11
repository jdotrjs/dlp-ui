package ytdlp

import (
	"strings"
	"testing"
)

// field joins fields with the unit separator the templates use.
func field(parts ...string) string {
	return strings.Join(parts, us)
}

func TestParseProgress(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Progress
		ok   bool
	}{
		{
			name: "full",
			line: field("PROGRESS", "2", " 42.5%", "1234567", "2900000", "512000.0", "12"),
			want: Progress{PlaylistIndex: 2, Percent: 42.5, DownloadedBytes: 1234567, TotalBytes: 2900000, Speed: 512000, ETA: 12},
			ok:   true,
		},
		{
			// Single (non-playlist) download: index NA; total unknown (NA →
			// estimate also NA); speed/eta NA at start.
			name: "na fields",
			line: field("PROGRESS", "NA", " 0.0%", "0", "NA", "NA", "NA"),
			want: Progress{PlaylistIndex: 0, Percent: 0, DownloadedBytes: 0, TotalBytes: 0, Speed: 0, ETA: 0},
			ok:   true,
		},
		{
			// eta sometimes arrives as a float string.
			name: "float eta",
			line: field("PROGRESS", "1", "100.0%", "5000", "5000", "0.0", "0.0"),
			want: Progress{PlaylistIndex: 1, Percent: 100, DownloadedBytes: 5000, TotalBytes: 5000, Speed: 0, ETA: 0},
			ok:   true,
		},
		{
			name: "wrong field count",
			line: field("PROGRESS", "1", "100.0%"),
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseProgress(tc.line)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if got != tc.want {
				t.Errorf("parseProgress = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseCompletion(t *testing.T) {
	line := field(
		"DONE",
		"3",
		"youtube",
		"dQw4w9WgXcQ",
		"Rick Astley",
		"212",
		"20091025",
		"https://i.ytimg.com/vi/dQw4w9WgXcQ/maxres.jpg",
		"mp4",
		"56000000",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"Never Gonna Give You Up | Official Video", // pipe in title must survive
		"/home/u/Downloads/Never Gonna Give You Up.mp4",
	)
	got, ok := parseCompletion(line)
	if !ok {
		t.Fatal("parseCompletion ok = false, want true")
	}
	want := Completion{
		PlaylistIndex: 3,
		Extractor:     "youtube",
		ID:            "dQw4w9WgXcQ",
		Uploader:      "Rick Astley",
		Duration:      212,
		UploadDate:    "20091025",
		Thumbnail:     "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxres.jpg",
		Ext:           "mp4",
		Filesize:      56000000,
		WebpageURL:    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Title:         "Never Gonna Give You Up | Official Video",
		Filepath:      "/home/u/Downloads/Never Gonna Give You Up.mp4",
	}
	if got != want {
		t.Errorf("parseCompletion =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestParseCompletionNA(t *testing.T) {
	// A single video: playlist_index, uploader, upload_date can be NA.
	line := field(
		"DONE", "NA", "soundcloud", "track123", "NA", "180", "NA",
		"NA", "opus", "NA", "https://soundcloud.com/x/track123", "Some Track", "/tmp/Some Track.opus",
	)
	got, ok := parseCompletion(line)
	if !ok {
		t.Fatal("ok = false")
	}
	if got.PlaylistIndex != 0 {
		t.Errorf("PlaylistIndex = %d, want 0 for NA", got.PlaylistIndex)
	}
	if got.Uploader != "" || got.UploadDate != "" || got.Thumbnail != "" {
		t.Errorf("NA string fields should be blank: %+v", got)
	}
	if got.Filesize != 0 {
		t.Errorf("Filesize = %d, want 0 for NA", got.Filesize)
	}
	if got.Filepath != "/tmp/Some Track.opus" {
		t.Errorf("Filepath = %q", got.Filepath)
	}
}

func TestScanStreamDispatch(t *testing.T) {
	var progs []Progress
	var dones []Completion
	stream := strings.Join([]string{
		"[youtube] Extracting URL ...",                                  // ignored noise
		field("PROGRESS", "1", " 10.0%", "100", "1000", "50.0", "18"),   // progress
		"[download] Destination: foo.mp4",                               // ignored
		field("PROGRESS", "1", "100.0%", "1000", "1000", "0.0", "0"),    // progress
		field("DONE", "1", "youtube", "id1", "Up", "60", "20200101", "", "mp4", "1000", "https://x/1", "Title One", "/dl/Title One.mp4"),
		"",
	}, "\n")

	err := scanStream(strings.NewReader(stream), Callbacks{
		OnProgress: func(p Progress) { progs = append(progs, p) },
		OnItemDone: func(c Completion) { dones = append(dones, c) },
	})
	if err != nil {
		t.Fatalf("scanStream: %v", err)
	}
	if len(progs) != 2 {
		t.Fatalf("got %d progress callbacks, want 2", len(progs))
	}
	if progs[1].Percent != 100 {
		t.Errorf("second progress percent = %v, want 100", progs[1].Percent)
	}
	if len(dones) != 1 {
		t.Fatalf("got %d done callbacks, want 1", len(dones))
	}
	if dones[0].Title != "Title One" || dones[0].Filepath != "/dl/Title One.mp4" {
		t.Errorf("done = %+v", dones[0])
	}
}

func TestDownloadArgs(t *testing.T) {
	t.Run("audio only forces -x and ignores format", func(t *testing.T) {
		args := downloadArgs(Request{AudioOnly: true, FormatID: "137", OutputArgs: []string{"-o", "tmpl"}})
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-x") {
			t.Errorf("missing -x: %v", args)
		}
		if strings.Contains(joined, "-f 137") {
			t.Errorf("audio-only must not pass -f: %v", args)
		}
	})
	t.Run("format id", func(t *testing.T) {
		args := downloadArgs(Request{FormatID: "248+251"})
		if !strings.Contains(strings.Join(args, " "), "-f 248+251") {
			t.Errorf("missing -f: %v", args)
		}
	})
	t.Run("best (no -f)", func(t *testing.T) {
		args := downloadArgs(Request{})
		if strings.Contains(strings.Join(args, " "), "-f ") {
			t.Errorf("best should pass no -f: %v", args)
		}
	})
	t.Run("playlist items + paths", func(t *testing.T) {
		args := downloadArgs(Request{PlaylistItems: "1,3,5", DownloadDir: "/dl"})
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--playlist-items 1,3,5") {
			t.Errorf("missing --playlist-items: %v", args)
		}
		if !strings.Contains(joined, "--paths home:/dl") {
			t.Errorf("missing --paths home: %v", args)
		}
	})
	t.Run("always carries progress template and after_move print", func(t *testing.T) {
		args := downloadArgs(Request{})
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--progress-template") || !strings.Contains(joined, "--newline") {
			t.Errorf("missing progress streaming flags: %v", args)
		}
		if !strings.Contains(joined, "--print") || !strings.Contains(joined, "after_move:") {
			t.Errorf("missing after_move print: %v", args)
		}
	})
}
