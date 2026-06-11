import { h } from 'preact';
import { useMemo } from 'preact/hooks';
import { SortBy, SortDir, SORT_OPTIONS } from '../lib/types';
import { Select, SelectOption } from '../lib/Select';

// SortBar lets the user pick the sort key + direction and an optional source
// filter. Changing any of these resets to page 0 (handled by AppContext).
// `sources` is the distinct list of sources present (derived from loaded rows);
// it's a convenience filter, not exhaustive over the whole DB.
interface Props {
  sortBy: SortBy;
  sortDir: SortDir;
  source: string;
  sources: string[];
  onSort: (sortBy: SortBy, sortDir: SortDir) => void;
  onSource: (source: string) => void;
}

export function SortBar({ sortBy, sortDir, source, sources, onSort, onSource }: Props) {
  const sourceOptions = useMemo<SelectOption[]>(
    () => [{ value: '', label: 'All' }, ...sources.map((s) => ({ value: s, label: s }))],
    [sources],
  );

  return (
    <div class="sortbar">
      <label class="sortbar-field">
        <span class="sortbar-label">Sort</span>
        <Select<SortBy>
          value={sortBy}
          options={SORT_OPTIONS}
          onChange={(v) => onSort(v, sortDir)}
        />
      </label>

      <button
        class="btn sortbar-dir"
        title={sortDir === 'asc' ? 'Ascending' : 'Descending'}
        onClick={() => onSort(sortBy, sortDir === 'asc' ? 'desc' : 'asc')}
      >
        {sortDir === 'asc' ? '▲' : '▼'}
      </button>

      <label class="sortbar-field">
        <span class="sortbar-label">Source</span>
        <Select
          value={source}
          options={sourceOptions}
          onChange={onSource}
        />
      </label>
    </div>
  );
}
