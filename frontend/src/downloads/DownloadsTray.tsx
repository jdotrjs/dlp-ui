import './downloads.css';
import { h, Fragment } from 'preact';
import { useMemo, useState } from 'preact/hooks';
import { useApp } from '../lib/AppContext';
import { DownloadState } from '../lib/types';
import { DownloadProgressBar } from './DownloadProgressBar';

// DownloadsTray is the docked panel listing active + recent downloads. It reads
// the downloads Map from AppContext, sorts newest-first, and renders a
// DownloadProgressBar per download with Cancel/Dismiss wired to the context.
// A header shows the active count and collapses the panel; it hides entirely
// when there are no downloads at all.
const ACTIVE = new Set(['queued', 'active']);

export function DownloadsTray() {
  const app = useApp();
  const [collapsed, setCollapsed] = useState(false);

  // Newest first, by start time.
  const list = useMemo<DownloadState[]>(() => {
    return Array.from(app.downloads.values()).sort((a, b) => b.startedAt - a.startedAt);
  }, [app.downloads]);

  const activeCount = useMemo(
    () => list.filter((d) => ACTIVE.has(d.status)).length,
    [list],
  );

  if (list.length === 0) return null;

  // Whether any settled downloads exist (enables "clear finished").
  const hasFinished = list.some((d) => !ACTIVE.has(d.status));

  function clearFinished() {
    for (const d of list) {
      if (!ACTIVE.has(d.status)) app.clearDownload(d.id);
    }
  }

  return (
    <div class={`downloads-tray ${collapsed ? 'downloads-tray-collapsed' : ''}`}>
      <div class="dl-tray-head">
        <button
          class="dl-tray-toggle"
          onClick={() => setCollapsed((c) => !c)}
          title={collapsed ? 'Expand downloads' : 'Collapse downloads'}
        >
          {collapsed ? '▴' : '▾'}
        </button>
        <span class="dl-tray-title">
          Downloads
          {activeCount > 0 && <span class="dl-tray-count"> · {activeCount} active</span>}
        </span>
        {hasFinished && (
          <button class="btn-link dl-tray-clear" onClick={clearFinished}>
            clear finished
          </button>
        )}
      </div>

      {!collapsed && (
        <div class="dl-tray-list">
          {list.map((d) => (
            <DownloadProgressBar
              key={d.id}
              d={d}
              onCancel={app.cancelDownload}
              onClear={app.clearDownload}
            />
          ))}
        </div>
      )}
    </div>
  );
}
