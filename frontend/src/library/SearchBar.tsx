import { h } from 'preact';

// SearchBar is the fuzzy-search input. Search runs client-side (Fuse) over the
// rows currently loaded in the page, so it filters what's visible rather than
// re-querying the whole library (see lib/fuse.ts).
interface Props {
  value: string;
  onChange: (q: string) => void;
}

export function SearchBar({ value, onChange }: Props) {
  return (
    <div class="searchbar">
      <span class="searchbar-icon" aria-hidden="true">⌕</span>
      <input
        class="text-input searchbar-input"
        type="search"
        placeholder="Filter loaded results…"
        value={value}
        onInput={(e) => onChange(e.currentTarget.value)}
      />
      {value && (
        <button class="searchbar-clear" title="Clear" onClick={() => onChange('')}>
          ×
        </button>
      )}
    </div>
  );
}
