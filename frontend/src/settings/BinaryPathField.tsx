import { h } from 'preact';
import { PickFile, binaries } from '../api/client';

// BinaryPathField is a text input + Browse (native file picker via PickFile)
// plus a doctor-status badge for one external binary (yt-dlp / ffmpeg / node).
// The status comes from ResolveBinaries, run by the parent SettingsView.
interface BinaryPathFieldProps {
  label: string;
  hint?: string;
  value: string;
  // filePattern is a semicolon-separated glob passed to PickFile, e.g.
  // "yt-dlp;yt-dlp.exe". Empty means any file.
  filePattern?: string;
  status?: binaries.BinaryStatus;
  onChange: (value: string) => void;
}

export function BinaryPathField(props: BinaryPathFieldProps) {
  async function browse() {
    const picked = await PickFile(props.label, props.filePattern ?? '');
    if (picked) props.onChange(picked);
  }

  return (
    <div class="field">
      <label class="field-label">{props.label}</label>
      {props.hint && <div class="field-hint">{props.hint}</div>}
      <div class="field-row">
        <input
          class="text-input"
          type="text"
          value={props.value}
          placeholder="(use PATH)"
          onInput={(e) => props.onChange((e.target as HTMLInputElement).value)}
        />
        <button class="btn" onClick={browse}>
          Browse…
        </button>
      </div>
      <StatusBadge status={props.status} />
    </div>
  );
}

function StatusBadge({ status }: { status?: binaries.BinaryStatus }) {
  if (!status) {
    return <div class="status status-unknown">not checked yet</div>;
  }
  if (status.ok) {
    return (
      <div class="status status-ok" title={status.resolved}>
        ✓ {status.version || 'found'}
        <span class="status-source"> ({status.source})</span>
      </div>
    );
  }
  return (
    <div class="status status-bad" title={status.error}>
      ✗ {status.error || 'not found'}
    </div>
  );
}
