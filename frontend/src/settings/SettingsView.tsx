import './settings.css';
import { h, Fragment } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { GetConfig, SaveConfig, ResolveBinaries, config, binaries } from '../api/client';
import { PlaylistMode } from '../lib/types';
import { BinaryPathField } from './BinaryPathField';
import { PathField } from './PathField';
import { TemplateField } from './TemplateField';
import { RestrictNamesToggle } from './RestrictNamesToggle';
import { PlaylistModeRadio } from './PlaylistModeRadio';
import { MaxConcurrentField } from './MaxConcurrentField';

// SettingsView loads the config, edits a local working copy, persists it via
// SaveConfig, then re-runs ResolveBinaries to refresh the doctor status (per
// the plan, changing a binary path is just a SaveConfig + re-doctor — no
// dedicated method).
export function SettingsView() {
  const [cfg, setCfg] = useState<config.Config | null>(null);
  const [statuses, setStatuses] = useState<binaries.BinaryStatus[]>([]);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');

  // Load config + initial doctor status on mount.
  useEffect(() => {
    let alive = true;
    (async () => {
      const loaded = await GetConfig();
      if (!alive) return;
      setCfg(loaded);
      const st = await ResolveBinaries();
      if (alive) setStatuses(st);
    })();
    return () => {
      alive = false;
    };
  }, []);

  if (!cfg) {
    return <div class="view-pad">Loading settings…</div>;
  }

  // patch updates one field on the working copy.
  function patch(p: Partial<config.Config>) {
    setCfg((prev) => (prev ? Object.assign(config.Config.createFrom(prev), p) : prev));
    setMessage('');
  }

  async function save() {
    if (!cfg) return;
    setSaving(true);
    setMessage('');
    try {
      await SaveConfig(cfg);
      // Re-run the doctor so binary status reflects the just-saved paths.
      const st = await ResolveBinaries();
      setStatuses(st);
      setMessage('Saved.');
    } catch (e) {
      setMessage(`Save failed: ${e}`);
    } finally {
      setSaving(false);
    }
  }

  const statusFor = (name: string) => statuses.find((s) => s.name === name);

  return (
    <div class="settings-view">
      <header class="view-header">
        <h1>Settings</h1>
        <div class="header-actions">
          {message && <span class="save-message">{message}</span>}
          <button class="btn btn-primary" onClick={save} disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </header>

      <section class="settings-section">
        <h2>Binaries</h2>
        <BinaryPathField
          label="yt-dlp"
          hint="Leave blank to use the one on your PATH."
          value={cfg.ytdlpPath}
          filePattern="yt-dlp;yt-dlp.exe;*"
          status={statusFor('yt-dlp')}
          onChange={(v) => patch({ ytdlpPath: v })}
        />
        <BinaryPathField
          label="ffmpeg"
          hint="Passed to yt-dlp via --ffmpeg-location when set."
          value={cfg.ffmpegPath}
          filePattern="ffmpeg;ffmpeg.exe;*"
          status={statusFor('ffmpeg')}
          onChange={(v) => patch({ ffmpegPath: v })}
        />
        <BinaryPathField
          label="deno"
          hint="This specifies the JS runtime.."
          value={cfg.denoPath}
          filePattern="deno;deno.exe;*"
          status={statusFor('deno')}
          onChange={(v) => patch({ denoPath: v })}
        />
      </section>

      <section class="settings-section">
        <h2>Locations</h2>
        <PathField
          label="Download directory"
          value={cfg.downloadDir}
          onChange={(v) => patch({ downloadDir: v })}
        />
        <PathField
          label="Library database (optional)"
          hint="Leave blank to keep library.db inside the download directory."
          value={cfg.dbPath}
          placeholder="(derived from download directory)"
          onChange={(v) => patch({ dbPath: v })}
        />
      </section>

      <section class="settings-section">
        <h2>Downloading</h2>
        <MaxConcurrentField
          value={cfg.maxConcurrent}
          onChange={(n) => patch({ maxConcurrent: n })} />
      </section>

      <section class="settings-section">
        <h2>Output</h2>
        <TemplateField
          label="Output template"
          value={cfg.outputTemplate}
          restrict={cfg.restrictFilenames}
          placeholder="%(title)s-id-%(id)s.%(ext)s"
          onChange={(v) => patch({ outputTemplate: v })}
        />
        <RestrictNamesToggle
          value={cfg.restrictFilenames}
          onChange={(v) => patch({ restrictFilenames: v })}
        />
        <PlaylistModeRadio
          mode={cfg.playlistMode as PlaylistMode}
          template={cfg.playlistTemplate}
          onModeChange={(m) => patch({ playlistMode: m })}
          onTemplateChange={(t) => patch({ playlistTemplate: t })}
        />
      </section>

    </div>
  );
}

/*
 * Pending support on backend.
function PlaylistMaxItemsField(props: {
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <div class="field">
      <label class="field-label">Max Playlist Length</label>
      <input
        class="number-input"
        type="number"
        min={-1}
        value={props.value}
        onInput={(e) => {
          const n = parseInt((e.target as HTMLInputElement).value, 10);
          props.onChange(Number.isFinite(n) && n > -1 ? n : -1);
        }}
      />
    </div>
  )
}

*/