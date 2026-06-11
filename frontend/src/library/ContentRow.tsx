import { h, Fragment } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { GetThumbnailDataURL, library } from '../api/client';
import { ContentStatus } from '../lib/types';
import {
  formatBytes,
  formatDownloadDate,
  formatDuration,
  formatUploadDate,
} from '../lib/format';

// ContentRow renders one library item: title, channel, duration, source, and a
// status pill, plus a ⋯ actions menu (Open / Reveal / Delete). Clicking the row
// toggles an inline detail panel (a minimal ContentDetail) — the plan allows a
// row-expand instead of a separate component.
interface Props {
  item: library.Content;
  onOpen: (id: number) => void;
  onReveal: (id: number) => void;
  onDelete: (item: library.Content) => void;
  // onPlaylist filters the Library to a playlist grouping (chunk 5).
  onPlaylist: (playlistId: number, playlistTitle: string) => void;
}

function statusClass(status: string): string {
  switch (status as ContentStatus) {
    case 'completed':
      return 'pill-ok';
    case 'failed':
      return 'pill-bad';
    case 'downloading':
      return 'pill-busy';
    default:
      return '';
  }
}

// playlistLabel is the short "‹title› (index)" affordance shown on rows that
// belong to a playlist; falls back to "Playlist" when the joined title is empty.
function playlistLabel(item: library.Content): string {
  const title = item.playlistTitle || 'Playlist';
  return item.playlistIndex > 0 ? `${title} (${item.playlistIndex})` : title;
}

export function ContentRow({ item, onOpen, onReveal, onDelete, onPlaylist }: Props) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);
  // Thumbnails are fetched lazily on first expand so the list view doesn't pay
  // for them. '' is "not loaded yet"; null is "loaded, none available" — both
  // suppress the <img>. A loaded data URL string renders the image.
  const [thumb, setThumb] = useState<string | null>('');
  // The menu is rendered position:fixed so it escapes the .content-list
  // `overflow: hidden` clip (which the rounded corners rely on). We anchor it to
  // the '...' button's viewport rect, captured when the menu opens.
  const btnRef = useRef<HTMLButtonElement>(null);
  const [menuPos, setMenuPos] = useState<{ top: number; right: number } | null>(null);

  const openMenu = () => {
    const btn = btnRef.current;
    if (btn) {
      const rect = btn.getBoundingClientRect();
      setMenuPos({ top: rect.bottom + 4, right: window.innerWidth - rect.right });
    }
    setMenuOpen(true);
  };
  const closeMenu = () => setMenuOpen(false);

  // Close open menus on scroll so they don't float; see btnRef for fixed reasoning
  useEffect(() => {
    if (!menuOpen) return;
    window.addEventListener('scroll', closeMenu, true);
    return () => window.removeEventListener('scroll', closeMenu, true);
  }, [menuOpen]);

  // Load the thumbnail on first expand. cancelled guards against state writes
  // after the row collapses (and the component is still mounted, just hidden).
  useEffect(() => {
    if (!expanded || thumb !== '') return;
    let cancelled = false;
    GetThumbnailDataURL(item.id)
      .then((url) => {
        if (!cancelled) setThumb(url || null);
      })
      .catch(() => {
        if (!cancelled) setThumb(null);
      });
    return () => {
      cancelled = true;
    };
  }, [expanded, item.id, thumb]);

  const inPlaylist = item.playlistId > 0;

  return (
    <>
      <div class={`content-row ${expanded ? 'content-row-expanded' : ''}`}>
        <button
          class="content-main"
          onClick={() => setExpanded((x) => !x)}
          title={item.title}
        >
          <span class="content-title">{item.title || '(untitled)'}</span>
          <span class="content-meta">
            {item.uploader && <span class="content-uploader">{item.uploader}</span>}
            {item.duration > 0 && (
              <span class="content-duration">{formatDuration(item.duration)}</span>
            )}
            <span class="content-source">{item.source}</span>
            {inPlaylist && (
              // Clickable badge: filter the Library to this playlist. Rendered as
              // a span (not a nested <button>, which is invalid inside the row's
              // content-main button) with a stopPropagation click so it doesn't
              // also toggle the row's expand.
              <span
                class="content-playlist-badge"
                title={`Show only "${item.playlistTitle || 'this playlist'}"`}
                role="button"
                tabIndex={0}
                onClick={(e) => {
                  e.stopPropagation();
                  onPlaylist(item.playlistId, item.playlistTitle);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.stopPropagation();
                    e.preventDefault();
                    onPlaylist(item.playlistId, item.playlistTitle);
                  }
                }}
              >
                ▸ {playlistLabel(item)}
              </span>
            )}
            {item.status !== 'completed' && (
              <span class={`content-pill ${statusClass(item.status)}`}>{item.status}</span>
            )}
          </span>
        </button>

        <div class="content-actions">
          <button
            ref={btnRef}
            class="content-menu-btn"
            title="Actions"
            onClick={() => (menuOpen ? closeMenu() : openMenu())}
          >
            ⋯
          </button>
          {menuOpen && menuPos && (
            <>
              {/* click-catcher closes the menu when clicking elsewhere */}
              <div class="menu-backdrop" onClick={closeMenu} />
              <div
                class="content-menu"
                style={{ top: `${menuPos.top - 5}px`, right: `${menuPos.right}px` }}
              >
                <button
                  onClick={() => {
                    closeMenu();
                    onOpen(item.id);
                  }}
                >
                  Open file
                </button>
                <button
                  onClick={() => {
                    closeMenu();
                    onReveal(item.id);
                  }}
                >
                  Reveal in folder
                </button>
                <button
                  class="menu-danger"
                  onClick={() => {
                    closeMenu();
                    onDelete(item);
                  }}
                >
                  Delete…
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      {expanded && (
        <div class="content-detail">
          {thumb && (
            <img class="content-thumb" src={thumb} alt={item.title || 'thumbnail'} />
          )}
          <dl class="detail-grid">
            {inPlaylist && (
              <>
                <dt>Playlist</dt>
                <dd>
                  <button
                    class="btn-link detail-playlist-link"
                    title={`Show only "${item.playlistTitle || 'this playlist'}"`}
                    onClick={() => onPlaylist(item.playlistId, item.playlistTitle)}
                  >
                    {playlistLabel(item)}
                  </button>
                </dd>
              </>
            )}
            <dt>Source</dt>
            <dd>
              {item.source}
              {item.sourceId ? ` · ${item.sourceId}` : ''}
            </dd>
            {item.uploadDate && (
              <>
                <dt>Published</dt>
                <dd>{formatUploadDate(item.uploadDate)}</dd>
              </>
            )}
            {item.downloadedAt > 0 && (
              <>
                <dt>Downloaded</dt>
                <dd>{formatDownloadDate(item.downloadedAt)}</dd>
              </>
            )}
            {item.filesize > 0 && (
              <>
                <dt>Size</dt>
                <dd>
                  {formatBytes(item.filesize)}
                  {item.ext ? ` · ${item.ext}` : ''}
                </dd>
              </>
            )}
            {item.filepath && (
              <>
                <dt>File</dt>
                <dd class="detail-path">{item.filepath}</dd>
              </>
            )}
            {item.sourceUrl && (
              <>
                <dt>URL</dt>
                <dd class="detail-path">{item.sourceUrl}</dd>
              </>
            )}
            {item.error && (
              <>
                <dt>Error</dt>
                <dd class="detail-error">{item.error}</dd>
              </>
            )}
          </dl>
        </div>
      )}
    </>
  );
}
