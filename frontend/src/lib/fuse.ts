// Client-side fuzzy search over the library rows currently paged in.
//
// Per the plan, search is NOT a database query (the SQLite driver has no FTS5):
// ListContent pages rows in with server-side sort + pagination, and Fuse then
// filters whatever is loaded. So a search narrows the visible page, it does not
// re-query the whole library.
import Fuse from 'fuse.js';
import type { IFuseOptions } from 'fuse.js';
import type { library } from '../api/client';

// FUSE_KEYS are the fields searched, per the plan: title, uploader, source,
// playlistTitle. Title is weighted highest so a title hit ranks above an
// incidental channel/source match.
const FUSE_KEYS: IFuseOptions<library.Content>['keys'] = [
  { name: 'title', weight: 2 },
  { name: 'uploader', weight: 1 },
  { name: 'source', weight: 0.5 },
  { name: 'playlistTitle', weight: 0.5 },
];

const FUSE_OPTIONS: IFuseOptions<library.Content> = {
  keys: FUSE_KEYS,
  threshold: 0.35, // ~0.35 per the plan: fairly strict fuzzy matching.
  ignoreLocation: true, // match anywhere in the field, not just the start.
};

// searchContent returns the rows matching query, in Fuse relevance order. An
// empty/whitespace query returns the input unchanged (no filtering), preserving
// the server-side sort order.
export function searchContent(
  rows: library.Content[],
  query: string,
): library.Content[] {
  const q = query.trim();
  if (!q) return rows;
  const fuse = new Fuse(rows, FUSE_OPTIONS);
  return fuse.search(q).map((r) => r.item);
}
