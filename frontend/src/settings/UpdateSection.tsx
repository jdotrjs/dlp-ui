import { h, Fragment } from 'preact';
import { useState } from 'preact/hooks';
import { CheckForUpdate } from '../api/client';
import { useApp } from '../lib/AppContext';

// UpdateSection renders the "About / Updates" block at the bottom of the
// Settings view: the running app version, the last known latest release tag
// from GitHub, when we last checked, and a manual check button. Result is
// pushed into AppContext so the sidebar badge updates immediately without
// waiting on the Wails event round-trip.
export function UpdateSection() {
  const { updateStatus, setUpdateStatus } = useApp();
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState('');

  async function check() {
    setChecking(true);
    setError('');
    try {
      const info = await CheckForUpdate();
      setUpdateStatus(info);
    } catch (e) {
      setError(`Check failed: ${e}`);
    } finally {
      setChecking(false);
    }
  }

  return (
    <section class="settings-section">
      <h2>About</h2>
      <div class="field">
        <label class="field-label">App version</label>
        <div class="update-row">
          <span class="update-current">{updateStatus?.currentVersion ?? '…'}</span>
          <button class="btn" onClick={check} disabled={checking}>
            {checking ? 'Checking…' : 'Check for updates'}
          </button>
        </div>
        <UpdateDetail info={updateStatus} error={error} />
      </div>
    </section>
  );
}

function UpdateDetail({
  info,
  error,
}: {
  info: ReturnType<typeof useApp>['updateStatus'];
  error: string;
}) {
  if (error) {
    return <div class="status status-bad">{error}</div>;
  }
  if (!info) {
    return <div class="field-hint">Loading update status…</div>;
  }
  if (!info.latestVersion) {
    return <div class="field-hint">No update check has run yet.</div>;
  }
  const checkedAt = info.lastCheckedAt
    ? new Date(info.lastCheckedAt * 1000).toLocaleString()
    : 'never';
  if (info.updateAvailable) {
    return (
      <div class="status status-bad" style="margin-top:6px">
        Update available: <strong>{info.latestVersion}</strong>
        {info.releaseUrl && (
          <>
            {' '}
            (<a href={info.releaseUrl} target="_blank" rel="noopener noreferrer">release notes</a>)
          </>
        )}
        <div class="field-hint" style="margin-top:4px">Last checked: {checkedAt}</div>
      </div>
    );
  }
  return (
    <div class="field-hint" style="margin-top:6px">
      Up to date. Latest seen: {info.latestVersion}. Last checked: {checkedAt}.
    </div>
  );
}
