import './library.css';
import { h, Fragment } from 'preact';
import { useMemo, useState } from 'preact/hooks';
import {
  DeleteContent,
  FetchMetadata,
  OpenFile,
  OpenInFolder,
  StartDownload,
  download,
  library,
  ytdlp,
} from '../api/client';
import { useApp } from '../lib/AppContext';
import { searchContent } from '../lib/fuse';
import { DownloadRequest, PAGE_SIZE } from '../lib/types';
import { ContentList } from './ContentList';
import { FetchBar } from './FetchBar';
import { FetchPreviewModal } from './FetchPreviewModal';
import { Pager } from './Pager';
import { SearchBar } from './SearchBar';
import { SortBar } from './SortBar';

// LibraryView is the real library list: server-side sort/filter/pagination via
// ListContent (held in AppContext), with client-side fuzzy search (Fuse) over
// the loaded page on top. Row actions (Open / Reveal / Delete) call the bound
// methods directly. The FetchBar + preview modal drive the fetch flow; their
// transient state (URL, in-flight, error, the current Preview) lives here as
// local state rather than in AppContext, whose download seam is reserved for
// chunk 4.
export function LibraryView() {
  const app = useApp();
  const [query, setQuery] = useState('');
  const [actionError, setActionError] = useState('');

  // Fetch-flow state (local to the library view).
  const [url, setUrl] = useState('');
  const [fetching, setFetching] = useState(false);
  const [fetchError, setFetchError] = useState('');
  const [preview, setPreview] = useState<ytdlp.Preview | null>(null);

  async function onFetch() {
    const u = url.trim();
    if (!u || fetching) return;
    setFetching(true);
    setFetchError('');
    try {
      const p = await FetchMetadata(u);
      setPreview(p);
    } catch (e) {
      setFetchError(String(e));
    } finally {
      setFetching(false);
    }
  }

  // onDownload starts the download for the request the preview modal assembled,
  // then enqueues it into the downloads tray (so it shows immediately, before
  // the first server event). Live progress arrives via the 'download' event
  // channel (AppContext subscription). The modal closes optimistically; a start
  // failure (e.g. yt-dlp missing) surfaces in the action banner.
  async function onDownload(req: DownloadRequest) {
    const title = req.isPlaylist
      ? req.playlistTitle || req.url
      : preview?.title || req.url;
    setPreview(null);
    setUrl('');
    setActionError('');
    try {
      // The hand-written DownloadRequest has optional override fields; the bound
      // method wants the generated model (those fields required). createFrom
      // fills the gaps (undefined → the model's field), bridging the two shapes.
      const id = await StartDownload(download.DownloadRequest.createFrom(req));
      app.enqueueDownload(id, title);
    } catch (e) {
      setActionError(`Couldn't start download: ${String(e)}`);
    }
  }

  // Distinct sources present in the loaded page, for the SortBar filter.
  const sources = useMemo(() => {
    const set = new Set<string>();
    for (const it of app.items) if (it.source) set.add(it.source);
    return Array.from(set).sort();
  }, [app.items]);

  // Fuse over the loaded rows only (narrows the visible page).
  const visible = useMemo(() => searchContent(app.items, query), [app.items, query]);

  async function onOpen(id: number) {
    setActionError('');
    try {
      await OpenFile(id);
    } catch (e) {
      setActionError(String(e));
    }
  }

  async function onReveal(id: number) {
    setActionError('');
    try {
      await OpenInFolder(id);
    } catch (e) {
      setActionError(String(e));
    }
  }

  async function onDelete(item: library.Content) {
    const deleteFile = window.confirm(
      `Delete "${item.title || 'this item'}" from the library?\n\n` +
        `Click OK to also delete the file from disk, or Cancel to keep it.`,
    );
    // confirm() is binary; we treat OK as "remove row + file", Cancel as abort.
    // A richer modal (keep-file option) is a later polish.
    if (!deleteFile) return;
    setActionError('');
    try {
      await DeleteContent(item.id, true);
      app.refresh();
    } catch (e) {
      setActionError(String(e));
    }
  }

  return (
    <div class="library-view">
      <header class="view-header library-header">
        <h1>Library</h1>
      </header>

      <FetchBar
        url={url}
        onUrl={setUrl}
        onFetch={onFetch}
        fetching={fetching}
        error={fetchError}
      />

      <div class="library-controls">
        <SearchBar value={query} onChange={setQuery} />
        <SortBar
          sortBy={app.list.sortBy}
          sortDir={app.list.sortDir}
          source={app.list.source}
          sources={sources}
          onSort={app.setSort}
          onSource={app.setSource}
        />
      </div>

      {app.list.playlistId > 0 && (
        <div class="active-filter">
          <span class="active-filter-label">Playlist</span>
          <span class="active-filter-chip">
            {app.list.playlistTitle || `#${app.list.playlistId}`}
            <button
              class="active-filter-clear"
              title="Clear playlist filter"
              onClick={() => app.setPlaylist(0, '')}
            >
              ×
            </button>
          </span>
        </div>
      )}

      {app.error && (
        <div class="library-banner library-banner-error">
          Couldn't load the library: {app.error}
        </div>
      )}
      {actionError && (
        <div class="library-banner library-banner-error">{actionError}</div>
      )}

      {app.loading ? (
        <div class="content-empty muted">Loading…</div>
      ) : (
        <>
          <ContentList
            items={visible}
            filtered={query.trim() !== ''}
            emptyMessage={
              app.list.playlistId > 0
                ? 'No items in this playlist.'
                : app.list.source
                ? `No downloads from "${app.list.source}".`
                : 'No downloads yet — paste a URL above to get started.'
            }
            onOpen={onOpen}
            onReveal={onReveal}
            onDelete={onDelete}
            onPlaylist={app.setPlaylist}
          />
          {/* Pager reflects the server-side total; search filters only the
              current page, so it doesn't change the page count. */}
          <Pager
            page={app.list.page}
            pageSize={PAGE_SIZE}
            total={app.total}
            onPage={app.setPage}
          />
        </>
      )}

      {preview && (
        <FetchPreviewModal
          preview={preview}
          onClose={() => setPreview(null)}
          onDownload={onDownload}
        />
      )}
    </div>
  );
}
