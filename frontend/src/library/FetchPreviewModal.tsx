import { h, Fragment } from 'preact';
import { useMemo, useState } from 'preact/hooks';
import { ytdlp } from '../api/client';
import { formatBytes, formatDuration } from '../lib/format';
import { Select, SelectOption } from '../lib/Select';
import { BEST_FORMAT_ID, DownloadRequest } from '../lib/types';
import { PlaylistItemRow } from './PlaylistItemRow';

// FetchPreviewModal renders the fetch preview card for a Preview, in two modes:
//
//   - single video: thumb, title, uploader, duration, a format dropdown
//     (synthetic "best" + the concrete formats), an audio-only toggle, Download.
//   - playlist: header with the item count, the same format/audio controls, a
//     scrollable list of selectable PlaylistItemRow with a select-all, Download.
//
// On Download it assembles a DownloadRequest from the Preview + chosen options
// and hands it to onDownload. For chunk 3 onDownload is a marked placeholder
// (LibraryView logs it and closes); chunk 4 swaps in StartDownload(req).
interface Props {
  preview: ytdlp.Preview;
  onClose: () => void;
  onDownload: (req: DownloadRequest) => void;
}

// formatLabel renders a concrete format for the dropdown, e.g.
// "137 · mp4 · 1920x1080 · 30fps · 53.4 MB".
function formatLabel(f: ytdlp.Format): string {
  const parts = [f.formatId, f.ext, f.resolution].filter(Boolean);
  if (f.fps > 0) parts.push(`${f.fps}fps`);
  if (f.note) parts.push(f.note);
  if (f.filesize > 0) parts.push(formatBytes(f.filesize));
  return parts.join(' · ');
}

export function FetchPreviewModal({ preview, onClose, onDownload }: Props) {
  const isPlaylist = preview.isPlaylist;
  const entries = preview.entries ?? [];
  const formats = preview.formats ?? [];

  const [formatId, setFormatId] = useState<string>(BEST_FORMAT_ID);
  const [audioOnly, setAudioOnly] = useState(false);

  const formatOptions = useMemo<SelectOption[]>(
    () => [
      { value: BEST_FORMAT_ID, label: 'best' },
      ...formats.map((f) => ({ value: f.formatId, label: formatLabel(f) })),
    ],
    [formats],
  );

  // For playlists: the set of checked 1-based indices. Default: all selected.
  const [selected, setSelected] = useState<Set<number>>(
    () => new Set(entries.map((e) => e.index)),
  );

  const allSelected = isPlaylist && entries.length > 0 && selected.size === entries.length;

  function toggle(index: number) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  }

  function toggleAll() {
    setSelected((prev) =>
      prev.size === entries.length ? new Set() : new Set(entries.map((e) => e.index)),
    );
  }

  // selectedIndices in ascending order for a stable request.
  const selectedIndices = useMemo(
    () => Array.from(selected).sort((a, b) => a - b),
    [selected],
  );

  const canDownload = !isPlaylist || selectedIndices.length > 0;

  function onDownloadClick() {
    const req: DownloadRequest = {
      url: preview.url,
      isPlaylist,
      selectedIndices: isPlaylist ? selectedIndices : [],
      formatId: audioOnly ? BEST_FORMAT_ID : formatId,
      audioOnly,
      // Optional overrides left empty: chunk 4 uses config defaults.
      playlistTitle: isPlaylist ? preview.title : '',
      playlistSourceId: isPlaylist ? preview.playlistId : '',
    };
    onDownload(req);
  }

  return (
    <div class="modal-backdrop" onClick={onClose}>
      <div class="modal fetch-modal" onClick={(e) => e.stopPropagation()}>
        <header class="modal-header">
          <h2>Fetch preview</h2>
          <button class="modal-close" title="Close" onClick={onClose}>
            ×
          </button>
        </header>

        <div class="fetch-head">
          {preview.thumbnail && (
            <img class="fetch-thumb" src={preview.thumbnail} alt="" />
          )}
          <div class="fetch-head-text">
            <div class="fetch-subtitle">
              {preview.uploader && <div class="fetch-uploader">{preview.uploader}</div>}
              {!isPlaylist && preview.duration > 0 && (
                <>
                  <div class="fetch-meta muted">/</div>
                  <div class="fetch-meta muted">{formatDuration(preview.duration)}</div>
                </>
              )}
              {preview.source && (
                <>
                  <div class="fetch-meta muted fetch-source">/</div>
                  <div class="fetch-meta muted fetch-source">{preview.source}</div>
                </>
              )}
            </div>
            <div class="fetch-title" title={preview.title}>
              {preview.title || preview.url}
              {isPlaylist && <span class="fetch-count"> ({preview.count})</span>}
            </div>
          </div>
        </div>

        <div class="fetch-controls">
          <div style="width: 100%">
            <label class="fetch-field fetch-field-format">
              <span class="fetch-field-label">Format</span>
              <Select
                class = "fetch-format-select"
                value={formatId}
                options={formatOptions}
                onChange={setFormatId}
                isDisabled={audioOnly}
              />
            </label>
          </div>
          <div>
            <label class="checkbox-row fetch-audio">
              <input
                type="checkbox"
                checked={audioOnly}
                onChange={(e) => setAudioOnly(e.currentTarget.checked)}
              />
              audio only
            </label>
          </div>
        </div>

        {isPlaylist && (
          <>
            <div class="pl-list">
              {entries.map((entry) => (
                <PlaylistItemRow
                  key={entry.index}
                  entry={entry}
                  checked={selected.has(entry.index)}
                  onToggle={toggle}
                />
              ))}
            </div>
            <div class="pl-selectall">
              <button class="btn-link" onClick={toggleAll}>
                {allSelected ? 'select none' : 'select all'}
              </button>
              <span class="muted pl-count">
                {selectedIndices.length} / {entries.length} selected
              </span>
            </div>
          </>
        )}

        <footer class="modal-footer">
          <button class="btn" onClick={onClose}>
            Cancel
          </button>
          <button
            class="btn btn-primary"
            disabled={!canDownload}
            onClick={onDownloadClick}
          >
            Download
          </button>
        </footer>
      </div>
    </div>
  );
}
