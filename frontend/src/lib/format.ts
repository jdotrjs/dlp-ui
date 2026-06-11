// Shared formatting helpers. Chunk 1 only needs a tiny live-example renderer
// for the output-template field; duration/byte formatters are stubbed here so
// the Library list (chunk 2) and downloads tray (chunk 4) have a home for them.

// formatDuration renders seconds as H:MM:SS or M:SS.
export function formatDuration(totalSeconds: number): string {
  if (!isFinite(totalSeconds) || totalSeconds < 0) return '0:00';
  const s = Math.floor(totalSeconds % 60);
  const m = Math.floor((totalSeconds / 60) % 60);
  const h = Math.floor(totalSeconds / 3600);
  const pad = (n: number) => n.toString().padStart(2, '0');
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

// formatBytes renders a byte count as a human-readable size.
export function formatBytes(bytes: number): string {
  if (!isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// formatSpeed renders a bytes/sec rate as a human-readable transfer speed
// (e.g. "1.4 MB/s"). Zero/unknown → ''.
export function formatSpeed(bytesPerSec: number): string {
  if (!isFinite(bytesPerSec) || bytesPerSec <= 0) return '';
  return `${formatBytes(bytesPerSec)}/s`;
}

// formatETA renders a seconds-remaining count as a compact "Hh Mm Ss" / "M:SS"
// label. Zero/unknown → ''.
export function formatETA(seconds: number): string {
  if (!isFinite(seconds) || seconds <= 0) return '';
  return `${formatDuration(seconds)} left`;
}

// formatUploadDate renders yt-dlp's YYYYMMDD upload_date string as a readable
// "Mon D, YYYY" (e.g. "Mar 15, 2024"). Non-8-digit / empty values pass through
// unchanged so unexpected formats aren't mangled.
export function formatUploadDate(d: string): string {
  if (!/^\d{8}$/.test(d)) return d;
  const year = Number(d.slice(0, 4));
  const month = Number(d.slice(4, 6));
  const day = Number(d.slice(6, 8));
  // Construct as UTC and format in UTC so the displayed day matches the source
  // YYYYMMDD regardless of the local timezone.
  const dt = new Date(Date.UTC(year, month - 1, day));
  if (isNaN(dt.getTime())) return `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6, 8)}`;
  return dt.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  });
}

// formatDownloadDate renders a unix-seconds timestamp as a locale date+time
// (e.g. "Mar 15, 2024, 2:03 PM"). Zero/unknown → ''.
export function formatDownloadDate(secs: number): string {
  if (!isFinite(secs) || secs <= 0) return '';
  return new Date(secs * 1000).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
}

// exampleFromTemplate produces an illustrative output filename from a yt-dlp -o
// template by substituting a sample title/id/ext, then (when restrict is set)
// applying yt-dlp's --restrict-filenames transform (ASCII, spaces -> _, drop
// special chars). This is a best-effort preview, not an exact yt-dlp replica.
export function exampleFromTemplate(template: string, restrict: boolean): string {
  if (!template) return '';
  const sample: Record<string, string> = {
    title: 'My Video & Friends',
    id: 'dQw4w9WgXcQ',
    ext: 'mp4',
    uploader: 'Some Channel',
    playlist: 'My Playlist',
  };
  // Replace %(field)s (with optional format spec like %(id)05d -> ignore spec).
  let out = template.replace(/%\(([a-zA-Z_]+)\)[^a-zA-Z]*[a-zA-Z]/g, (m, field: string) => {
    return field in sample ? sample[field] : m;
  });
  if (restrict) out = restrictFilename(out);
  return out;
}

// restrictFilename approximates yt-dlp's --restrict-filenames: keep ASCII
// alphanumerics, dot, dash, underscore and the path separator; collapse other
// runs to a single underscore.
function restrictFilename(name: string): string {
  return name
    .replace(/&/g, '')
    .replace(/[^A-Za-z0-9._/\-]+/g, '_')
    .replace(/_+/g, '_')
    .replace(/^_|_$/g, '');
}
