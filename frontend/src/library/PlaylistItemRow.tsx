import { h } from 'preact';
import { ytdlp } from '../api/client';
import { formatDuration } from '../lib/format';

// PlaylistItemRow is one selectable row of a playlist preview: a checkbox, the
// 1-based index, the title, and (when known) the duration. Flat playlist
// entries usually lack thumbnails/durations — those just render empty.
interface Props {
  entry: ytdlp.PreviewEntry;
  checked: boolean;
  onToggle: (index: number) => void;
}

export function PlaylistItemRow({ entry, checked, onToggle }: Props) {
  return (
    <label class="pl-item">
      <input
        type="checkbox"
        checked={checked}
        onChange={() => onToggle(entry.index)}
      />
      <span class="pl-item-index">{String(entry.index).padStart(3, '0')}</span>
      <span class="pl-item-title" title={entry.title}>
        {entry.title || '(untitled)'}
      </span>
      {entry.duration > 0 && (
        <span class="pl-item-duration">{formatDuration(entry.duration)}</span>
      )}
    </label>
  );
}
