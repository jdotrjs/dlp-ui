import { h } from 'preact';
import { library } from '../api/client';
import { ContentRow } from './ContentRow';

// ContentList renders the (already Fuse-filtered) page of rows. Empty-state
// messaging differs depending on whether the library itself is empty or just
// the current search/filter has no matches.
interface Props {
  items: library.Content[];
  filtered: boolean; // true when a search query is narrowing the page
  emptyMessage?: string; // override for the unfiltered empty-state text
  onOpen: (id: number) => void;
  onReveal: (id: number) => void;
  onDelete: (item: library.Content) => void;
  onPlaylist: (playlistId: number, playlistTitle: string) => void;
}

export function ContentList({
  items,
  filtered,
  emptyMessage,
  onOpen,
  onReveal,
  onDelete,
  onPlaylist,
}: Props) {
  if (items.length === 0) {
    return (
      <div class="content-empty muted">
        {filtered
          ? 'No results match your search.'
          : emptyMessage ?? 'No downloads yet — paste a URL above to get started.'}
      </div>
    );
  }
  return (
    <div class="content-list">
      {items.map((item) => (
        <ContentRow
          key={item.id}
          item={item}
          onOpen={onOpen}
          onReveal={onReveal}
          onDelete={onDelete}
          onPlaylist={onPlaylist}
        />
      ))}
    </div>
  );
}
