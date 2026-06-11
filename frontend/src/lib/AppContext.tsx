// AppContext holds app-wide state shared across views: the loaded config and
// the Library's current list state (sort/filter/page + the loaded page rows).
//
// It deliberately leaves a seam for chunk 4's downloads Map (an in-flight
// download id -> progress map) without building it now.
import { h, createContext, ComponentChildren } from 'preact';
import {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'preact/hooks';
import { CancelDownload, GetConfig, ListContent, config, library } from '../api/client';
import { subscribeDownloads } from '../api/events';
import {
  SortBy,
  SortDir,
  DEFAULT_SORT_BY,
  DEFAULT_SORT_DIR,
  PAGE_SIZE,
  DownloadEvent,
  DownloadState,
} from './types';
import { downloadsReducer } from './downloadsReducer';

// ListState is the Library's query + result state. It lives in context so the
// SortBar / SearchBar / Pager can read and mutate it without prop-drilling.
export interface ListState {
  sortBy: SortBy;
  sortDir: SortDir;
  source: string; // '' = no source filter
  // playlistId, when non-zero, filters the list to one playlist grouping.
  playlistId: number;
  // playlistTitle is the human label for the active playlist filter (for the
  // reset chip); '' when no playlist filter is active.
  playlistTitle: string;
  page: number; // zero-based page index
}

export interface AppState {
  // config is null until the initial GetConfig resolves.
  cfg: config.Config | null;

  // Library list state + the currently loaded page.
  list: ListState;
  items: library.Content[];
  total: number;
  loading: boolean;
  error: string;

  // Mutators for the list query. Changing sort/filter resets to page 0.
  setSort: (sortBy: SortBy, sortDir: SortDir) => void;
  setSource: (source: string) => void;
  // setPlaylist filters the list to one playlist grouping (id 0 + '' clears it).
  setPlaylist: (playlistId: number, playlistTitle: string) => void;
  setPage: (page: number) => void;
  // refresh re-runs the current query (e.g. after a delete).
  refresh: () => void;

  // --- Downloads (chunk 4) ---
  //
  // downloads maps an in-flight/recent download id to its accumulated state,
  // folded from the 'download' event stream by downloadsReducer. The
  // subscription is set up once in this provider (cleaned up on unmount).
  downloads: Map<string, DownloadState>;
  // enqueueDownload registers a just-started download (id from StartDownload)
  // so the tray shows it immediately, before the first server event.
  enqueueDownload: (id: string, title: string) => void;
  // cancelDownload cancels an in-flight download (calls the bound method); the
  // resulting 'cancelled' event updates the state.
  cancelDownload: (id: string) => void;
  // clearDownload dismisses a settled download from the tray.
  clearDownload: (id: string) => void;
}

const Ctx = createContext<AppState | null>(null);

const DEFAULT_LIST: ListState = {
  sortBy: DEFAULT_SORT_BY,
  sortDir: DEFAULT_SORT_DIR,
  source: '',
  playlistId: 0,
  playlistTitle: '',
  page: 0,
};

export function AppProvider({ children }: { children: ComponentChildren }) {
  const [cfg, setCfg] = useState<config.Config | null>(null);
  const [list, setList] = useState<ListState>(DEFAULT_LIST);
  const [items, setItems] = useState<library.Content[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  // reloadKey lets refresh() force a re-fetch without changing the query.
  const [reloadKey, setReloadKey] = useState(0);

  // downloads Map, folded from the 'download' event stream by downloadsReducer.
  const [downloads, setDownloads] = useState<Map<string, DownloadState>>(
    () => new Map(),
  );

  // Load config once on mount.
  useEffect(() => {
    let alive = true;
    GetConfig()
      .then((c) => alive && setCfg(c))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  // (Re)load the content page whenever the query or reloadKey changes.
  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError('');
    ListContent({
      sortBy: list.sortBy,
      sortDir: list.sortDir,
      source: list.source,
      playlistId: list.playlistId,
      limit: PAGE_SIZE,
      offset: list.page * PAGE_SIZE,
    })
      .then((res) => {
        if (!alive) return;
        setItems(res.items ?? []);
        setTotal(res.total ?? 0);
      })
      .catch((e) => {
        if (!alive) return;
        setItems([]);
        setTotal(0);
        setError(String(e));
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [list.sortBy, list.sortDir, list.source, list.playlistId, list.page, reloadKey]);

  const setSort = useCallback((sortBy: SortBy, sortDir: SortDir) => {
    setList((l) => ({ ...l, sortBy, sortDir, page: 0 }));
  }, []);
  const setSource = useCallback((source: string) => {
    setList((l) => ({ ...l, source, page: 0 }));
  }, []);
  const setPlaylist = useCallback((playlistId: number, playlistTitle: string) => {
    setList((l) => ({ ...l, playlistId, playlistTitle, page: 0 }));
  }, []);
  const setPage = useCallback((page: number) => {
    setList((l) => ({ ...l, page }));
  }, []);
  const refresh = useCallback(() => setReloadKey((k) => k + 1), []);

  // Debounce timer for live-refresh (chunk 5). A burst of item-done/completed
  // events from a playlist coalesces into a single refetch. Held in a ref so the
  // subscription effect can stay mount-once and we can clear it on cleanup.
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Subscribe to the single 'download' event channel once; fold each event into
  // the downloads Map via the reducer. Unsubscribe on unmount.
  //
  // Live Library refresh (chunk 5): an 'item-done' (a playlist item landed in
  // the DB) or 'completed' (the whole download finished) means new rows exist,
  // so we trigger the existing refresh() — debounced (~400ms) so a burst of
  // playlist completions coalesces into a small number of refetches. We don't
  // refetch on 'progress' (no DB change). The timer is cleared on cleanup so the
  // effect is leak-free.
  useEffect(() => {
    const scheduleRefresh = () => {
      if (refreshTimer.current !== null) clearTimeout(refreshTimer.current);
      refreshTimer.current = setTimeout(() => {
        refreshTimer.current = null;
        refresh();
      }, 400);
    };
    const unsub = subscribeDownloads((event: DownloadEvent) => {
      setDownloads((m) => downloadsReducer(m, event));
      if (event.type === 'item-done' || event.type === 'completed') {
        scheduleRefresh();
      }
    });
    return () => {
      unsub();
      if (refreshTimer.current !== null) {
        clearTimeout(refreshTimer.current);
        refreshTimer.current = null;
      }
    };
  }, [refresh]);

  const enqueueDownload = useCallback((id: string, title: string) => {
    setDownloads((m) => downloadsReducer(m, { type: 'enqueue', id, title }));
  }, []);

  const clearDownload = useCallback((id: string) => {
    setDownloads((m) => downloadsReducer(m, { type: 'clear', id }));
  }, []);

  const cancelDownload = useCallback((id: string) => {
    // Fire-and-forget: the resulting 'cancelled' event updates the state. A
    // failed cancel (already finished) is benign — ignore it.
    CancelDownload(id).catch(() => {});
  }, []);

  const value = useMemo<AppState>(
    () => ({
      cfg,
      list,
      items,
      total,
      loading,
      error,
      setSort,
      setSource,
      setPlaylist,
      setPage,
      refresh,
      downloads,
      enqueueDownload,
      cancelDownload,
      clearDownload,
    }),
    [
      cfg,
      list,
      items,
      total,
      loading,
      error,
      setSort,
      setSource,
      setPlaylist,
      setPage,
      refresh,
      downloads,
      enqueueDownload,
      cancelDownload,
      clearDownload,
    ],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

// useApp returns the AppState; throws if used outside AppProvider.
export function useApp(): AppState {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error('useApp must be used within AppProvider');
  return ctx;
}
