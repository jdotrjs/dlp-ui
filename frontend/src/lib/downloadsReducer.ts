// downloadsReducer is the pure reducer behind AppContext's downloads Map. It
// folds the 'download' event stream (plus a synthetic 'enqueue' action emitted
// locally when StartDownload resolves) into a Map<downloadID, DownloadState>.
//
// Kept pure + standalone so it's trivially unit-testable and so chunk 5 can
// extend it (e.g. trigger a ListContent refetch on item-done/completed — see
// the NOTE in AppContext where the dispatch happens). The reducer itself stays
// side-effect-free; cross-cutting reactions live in the context effect.
import { DownloadEvent, DownloadState } from './types';

export type DownloadsMap = Map<string, DownloadState>;

// EnqueueAction is dispatched locally (not a Wails event) the moment
// StartDownload returns an id, so the tray shows the download immediately —
// before the first server event arrives.
export interface EnqueueAction {
  type: 'enqueue';
  id: string;
  title: string;
}

// ClearAction removes a settled (completed/failed/cancelled) download from the
// tray when the user dismisses it.
export interface ClearAction {
  type: 'clear';
  id: string;
}

export type DownloadsAction = EnqueueAction | ClearAction | DownloadEvent;

// newDownload seeds a DownloadState for a freshly-enqueued id.
function newDownload(id: string, title: string): DownloadState {
  return {
    id,
    status: 'active',
    title,
    itemIndex: 0,
    percent: 0,
    downloadedBytes: 0,
    totalBytes: 0,
    speed: 0,
    eta: 0,
    doneCount: 0,
    error: '',
    startedAt: Date.now(),
  };
}

// downloadsReducer returns a NEW map (immutable update) for each action so
// Preact re-renders. Events for an unknown id (e.g. an enqueue race) seed a
// minimal state so nothing is dropped.
export function downloadsReducer(state: DownloadsMap, action: DownloadsAction): DownloadsMap {
  if (action.type === 'enqueue') {
    const next = new Map(state);
    next.set(action.id, newDownload(action.id, action.title));
    return next;
  }
  if (action.type === 'clear') {
    if (!state.has(action.id)) return state;
    const next = new Map(state);
    next.delete(action.id);
    return next;
  }

  // From here, action is a DownloadEvent (has downloadID).
  const id = action.downloadID;
  const prev = state.get(id) ?? newDownload(id, id);
  let cur: DownloadState = { ...prev };

  switch (action.type) {
    case 'queued':
      cur.status = 'queued';
      break;
    case 'item-start':
      cur.status = 'active';
      break;
    case 'progress':
      cur.status = 'active';
      cur.itemIndex = action.itemIndex;
      cur.percent = action.percent;
      cur.downloadedBytes = action.downloadedBytes;
      cur.totalBytes = action.totalBytes;
      cur.speed = action.speed;
      cur.eta = action.eta;
      break;
    case 'item-done':
      cur.doneCount = prev.doneCount + 1;
      cur.itemIndex = action.itemIndex;
      // A finished item resets per-item progress for the next item.
      cur.percent = 0;
      cur.speed = 0;
      cur.eta = 0;
      break;
    case 'completed':
      cur.status = 'completed';
      cur.percent = 100;
      cur.speed = 0;
      cur.eta = 0;
      break;
    case 'failed':
      cur.status = 'failed';
      cur.error = action.error;
      cur.speed = 0;
      cur.eta = 0;
      break;
    case 'cancelled':
      cur.status = 'cancelled';
      cur.speed = 0;
      cur.eta = 0;
      break;
    default:
      return state;
  }

  const next = new Map(state);
  next.set(id, cur);
  return next;
}
