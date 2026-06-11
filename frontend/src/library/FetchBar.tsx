import { h } from 'preact';

// FetchBar is the paste-URL input + Fetch button in the Library header. It owns
// no fetch state itself — the URL value, the in-flight/disabled state, and any
// error are passed in by LibraryView (which runs FetchMetadata and opens the
// preview modal). Pressing Enter in the input triggers a fetch.
interface Props {
  url: string;
  onUrl: (url: string) => void;
  onFetch: () => void;
  fetching: boolean;
  error: string;
}

export function FetchBar({ url, onUrl, onFetch, fetching, error }: Props) {
  const canFetch = url.trim() !== '' && !fetching;

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter' && canFetch) onFetch();
  }

  return (
    <div class="fetchbar">
      <div class="fetchbar-row">
        <input
          class="text-input fetchbar-input"
          type="text"
          placeholder="Paste a video or playlist URL…"
          value={url}
          disabled={fetching}
          onInput={(e) => onUrl(e.currentTarget.value)}
          onKeyDown={onKeyDown}
        />
        <button class="btn btn-primary fetchbar-btn" disabled={!canFetch} onClick={onFetch}>
          {fetching ? (
            <span class="fetchbar-spinner-row">
              <span class="spinner" aria-hidden="true" /> Fetching…
            </span>
          ) : (
            'Fetch'
          )}
        </button>
      </div>
      {error && <div class="library-banner library-banner-error fetchbar-error">{error}</div>}
    </div>
  );
}
