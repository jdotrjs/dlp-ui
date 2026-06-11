import { h } from 'preact';
import { formatBytes, formatETA, formatSpeed } from '../lib/format';
import { DownloadState } from '../lib/types';

// DownloadProgressBar renders one tracked download: a title, a status-aware
// progress bar, a percent/speed/eta line, and a Cancel button while it's still
// running (a Dismiss button once it has settled). It's purely presentational;
// the parent (DownloadsTray) supplies onCancel/onClear.
interface Props {
  d: DownloadState;
  onCancel: (id: string) => void;
  onClear: (id: string) => void;
}

// statusLabel is the short human label for the download status.
function statusLabel(d: DownloadState): string {
  switch (d.status) {
    case 'queued':
      return 'Queued';
    case 'active':
      return d.doneCount > 0 ? `Downloading (item ${d.itemIndex})` : 'Downloading';
    case 'completed':
      return d.doneCount > 1 ? `Completed (${d.doneCount} items)` : 'Completed';
    case 'failed':
      return 'Failed';
    case 'cancelled':
      return 'Cancelled';
    default:
      return '';
  }
}

// progressLine is the percent · speed · eta · sizes detail under the bar.
function progressLine(d: DownloadState): string {
  if (d.status === 'failed') return d.error;
  if (d.status === 'queued') return 'Waiting for a free slot…';
  const parts: string[] = [];
  if (d.status === 'active') {
    if (d.percent > 0) parts.push(`${d.percent.toFixed(1)}%`);
    const speed = formatSpeed(d.speed);
    if (speed) parts.push(speed);
    const eta = formatETA(d.eta);
    if (eta) parts.push(eta);
    if (d.totalBytes > 0) {
      parts.push(`${formatBytes(d.downloadedBytes)} / ${formatBytes(d.totalBytes)}`);
    } else if (d.downloadedBytes > 0) {
      parts.push(formatBytes(d.downloadedBytes));
    }
  }
  return parts.join(' · ');
}

const SETTLED = new Set(['completed', 'failed', 'cancelled']);

export function DownloadProgressBar({ d, onCancel, onClear }: Props) {
  const settled = SETTLED.has(d.status);
  // The visual fill: full for completed, the live percent while active, and a
  // thin sliver while queued/indeterminate.
  const fill =
    d.status === 'completed' ? 100 : d.status === 'active' ? d.percent : 0;
  const detail = progressLine(d);

  return (
    <div class={`dl-item dl-item-${d.status}`}>
      <div class="dl-item-head">
        <span class="dl-item-title" title={d.title}>
          {d.title}
        </span>
        <span class="dl-item-status muted">{statusLabel(d)}</span>
      </div>

      <div class="dl-bar">
        <div
          class={`dl-bar-fill dl-bar-fill-${d.status}`}
          style={{ width: `${Math.max(0, Math.min(100, fill))}%` }}
        />
      </div>

      <div class="dl-item-foot">
        <span class={`dl-item-detail muted ${d.status === 'failed' ? 'dl-item-error' : ''}`}>
          {detail}
        </span>
        {settled ? (
          <button class="btn-link dl-item-action" onClick={() => onClear(d.id)}>
            dismiss
          </button>
        ) : (
          <button class="btn-link dl-item-action" onClick={() => onCancel(d.id)}>
            cancel
          </button>
        )}
      </div>
    </div>
  );
}
