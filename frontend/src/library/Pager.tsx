import { h } from 'preact';

// Pager drives server-side pagination using the total from ListResult. It's
// disabled/hidden when everything fits on one page. Page is zero-based.
interface Props {
  page: number;
  pageSize: number;
  total: number;
  onPage: (page: number) => void;
}

export function Pager({ page, pageSize, total, onPage }: Props) {
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  if (pageCount <= 1) return null;

  const first = total === 0 ? 0 : page * pageSize + 1;
  const last = Math.min(total, (page + 1) * pageSize);

  return (
    <div class="pager">
      <button
        class="btn"
        disabled={page <= 0}
        onClick={() => onPage(page - 1)}
        title="Previous page"
      >
        ‹ Prev
      </button>
      <span class="pager-status">
        {first}–{last} of {total}
      </span>
      <button
        class="btn"
        disabled={page >= pageCount - 1}
        onClick={() => onPage(page + 1)}
        title="Next page"
      >
        Next ›
      </button>
    </div>
  );
}
