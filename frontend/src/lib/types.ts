// Frontend-side types that the Go bindings can't express precisely.
//
// Go's config.PlaylistMode is a string newtype, so the generated models.ts
// types playlistMode as plain `string`. We narrow it here to the three valid
// values for use in the Settings UI (radio options, custom-template reveal).
export type PlaylistMode = 'name' | 'dir' | 'custom';

export const PLAYLIST_MODES: ReadonlyArray<{ value: PlaylistMode; label: string }> = [
  { value: 'name', label: 'Prefix playlist name' },
  { value: 'dir', label: 'Per-playlist folder' },
  { value: 'custom', label: 'Custom template' },
];

// --- Library list types ---
//
// Go's library.Status and the ListOptions sort enums are string newtypes, so
// the generated models.ts types them as plain `string`. We narrow them here.

// ContentStatus mirrors library.Status (the content lifecycle state).
export type ContentStatus = 'completed' | 'failed' | 'downloading';

// SortBy mirrors the whitelisted ListOptions sort keys (UI names, not columns).
export type SortBy = 'source' | 'title' | 'channel' | 'publishDate' | 'downloadDate';

// SortDir is the sort direction.
export type SortDir = 'asc' | 'desc';

export const SORT_OPTIONS: ReadonlyArray<{ value: SortBy; label: string }> = [
  { value: 'downloadDate', label: 'Downloaded' },
  { value: 'publishDate', label: 'Published' },
  { value: 'title', label: 'Title' },
  { value: 'channel', label: 'Channel' },
  { value: 'source', label: 'Source' },
];

export const DEFAULT_SORT_BY: SortBy = 'downloadDate';
export const DEFAULT_SORT_DIR: SortDir = 'desc';
// PAGE_SIZE is how many rows ListContent returns per page (drives the Pager).
export const PAGE_SIZE = 50;

// --- Fetch / download types (chunk 3 fetch preview, consumed by chunk 4) ---
//
// DownloadRequest is the shape the fetch preview modal assembles from a Preview
// + the user's chosen options. Chunk 4 binds a Go StartDownload that consumes
// the same field names/types, so the modal can be wired to it by swapping the
// placeholder handler. Defined frontend-side now because the Go struct (and
// thus the generated model) doesn't exist until chunk 4.
export interface DownloadRequest {
  // url is the original URL the user fetched.
  url: string;
  // isPlaylist mirrors Preview.isPlaylist.
  isPlaylist: boolean;
  // selectedIndices are the 1-based playlist item indices to download; empty
  // for a single video (the whole URL is fetched).
  selectedIndices: number[];
  // formatId is a concrete yt-dlp format id, or '' meaning "best" (synthetic).
  formatId: string;
  // audioOnly requests audio extraction (yt-dlp -x), overriding formatId video.
  audioOnly: boolean;
  // Optional per-download overrides; empty/undefined means "use config".
  outputTemplate?: string;
  playlistModeOverride?: PlaylistMode;
  downloadDir?: string;
  // playlistTitle / playlistSourceId describe the grouping for the library.
  playlistTitle: string;
  playlistSourceId: string;
}

// BEST_FORMAT_ID is the sentinel for the synthetic "best" choice in the format
// dropdown: it maps to an empty formatId in the DownloadRequest (yt-dlp picks).
export const BEST_FORMAT_ID = '';

// --- Download events (chunk 4) ---
//
// The Go side emits ONE Wails event channel ('download') carrying a single
// DownloadEvent struct (see internal/download.DownloadEvent). It isn't bound to
// a method, so it has no generated model; we hand-write the discriminated union
// here so the AppContext reducer can switch on `type` exhaustively. Field names
// + the `type` strings mirror the Go struct's JSON tags exactly.

// DOWNLOAD_EVENT is the single Wails event channel name (matches Go EventName).
export const DOWNLOAD_EVENT = 'download';

export type DownloadEventType =
  | 'queued'
  | 'item-start'
  | 'progress'
  | 'item-done'
  | 'completed'
  | 'failed'
  | 'cancelled';

interface DownloadEventBase {
  downloadID: string;
}

export type DownloadEvent =
  | (DownloadEventBase & { type: 'queued' })
  | (DownloadEventBase & { type: 'item-start' })
  | (DownloadEventBase & {
      type: 'progress';
      itemIndex: number;
      percent: number;
      downloadedBytes: number;
      totalBytes: number;
      speed: number; // bytes/sec
      eta: number; // seconds
    })
  | (DownloadEventBase & {
      type: 'item-done';
      itemIndex: number;
      contentID: number;
      filepath: string;
    })
  | (DownloadEventBase & { type: 'completed' })
  | (DownloadEventBase & { type: 'failed'; itemIndex?: number; error: string })
  | (DownloadEventBase & { type: 'cancelled' });

// DownloadStatus is the UI lifecycle of a tracked download, derived from the
// event stream (NOT a 1:1 of the event types). 'active' covers item-start +
// progress; terminal states are completed/failed/cancelled.
export type DownloadStatus = 'queued' | 'active' | 'completed' | 'failed' | 'cancelled';

// --- Welcome / setup wizard types ---
//
// Backend emits per-binary install progress on the SETUP_EVENT channel as a
// SetupProgress payload. The wizard maintains a Map<binaryName, SetupProgress>
// keyed by the install.Progress.name field for its in-page download panel.

export const SETUP_EVENT = 'setup-progress';

export type SetupPhase =
  | 'start'
  | 'download'
  | 'extract'
  | 'install'
  | 'done'
  | 'error'
  | 'unsupported';

export interface SetupProgress {
  name: string;
  phase: SetupPhase;
  percent: number;
  message: string;
  path: string;
  error: string;
}

// DownloadState is one tracked download in the downloads Map. It accumulates the
// latest progress for the in-flight item plus terminal info. One StartDownload
// (one yt-dlp process, possibly many playlist items) maps to one DownloadState;
// itemIndex distinguishes items within it for the progress display.
export interface DownloadState {
  id: string;
  status: DownloadStatus;
  // Title shown in the tray. Seeded from the request at enqueue time (URL or
  // playlist title) since events don't carry it until item-done.
  title: string;
  // Latest progress snapshot for the currently-downloading item.
  itemIndex: number;
  percent: number; // 0..100
  downloadedBytes: number;
  totalBytes: number;
  speed: number; // bytes/sec
  eta: number; // seconds
  // doneCount counts finished items (for a multi-item playlist).
  doneCount: number;
  // error is set on a failed download.
  error: string;
  // startedAt is used to sort the tray (most recent first).
  startedAt: number;
}
