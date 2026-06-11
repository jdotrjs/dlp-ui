import './welcome.css';
import { h, Fragment } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import {
  ConfigPath,
  GetConfig,
  InstallDependency,
  ResolveBinaries,
  SaveConfig,
  VendorDir,
  binaries,
  config,
} from '../api/client';
import { subscribeSetup } from '../api/events';
import { SetupProgress } from '../lib/types';

// WelcomeView is the first-run setup wizard. Three screens:
//   1. Alpha-software disclaimer.
//   2. Dependency check (ResolveBinaries) + optional auto-install panel.
//   3. "All done" with the path of the just-saved config.
//
// On mount we load the freshly-written default Config (startup already saved
// it) and the initial doctor status. The vendor + config paths are pulled from
// the backend so a future layout change in internal/paths is reflected here
// without a second source of truth.
//
// Auto-install: the user clicks "Download missing dependencies", we call
// InstallDependency for each missing optional binary in series, subscribe to
// the SETUP_EVENT channel for per-step progress, and write the resulting paths
// back into the working-copy config. SaveConfig fires once at the end so the
// user gets a single explicit commit + a re-run doctor.
//
// onDone is invoked when the user dismisses the final screen; the parent
// (App in app.tsx) clears its local "show welcome" flag and the normal shell
// takes over.

// DEPENDENCY_LABEL maps a Go binaries.Spec.Name to the UI label + (for the
// "auto-install" step) the Config field to patch when an install succeeds.
type ConfigBinaryField = 'ytdlpPath' | 'ffmpegPath' | 'denoPath';
interface Dep {
  // doctorName matches the Spec.Name passed by ResolveBinaries.
  doctorName: string;
  // installName is the binary name install.Install knows; for the JS runtime
  // this is "deno" while the doctor still calls the binary "deno".
  installName: string;
  // configField is which Config field to set with the installed path.
  configField: ConfigBinaryField;
  label: string;
  hint: string;
  required: boolean;
}

const DEPS: Dep[] = [
  {
    doctorName: 'yt-dlp',
    installName: 'yt-dlp',
    configField: 'ytdlpPath',
    label: 'yt-dlp',
    hint: 'The downloader itself. Required.',
    required: true,
  },
  {
    doctorName: 'ffmpeg',
    installName: 'ffmpeg',
    configField: 'ffmpegPath',
    label: 'ffmpeg',
    hint: 'Needed for muxing and format conversion.',
    required: false,
  },
  {
    doctorName: 'deno',
    installName: 'deno',
    configField: 'denoPath',
    label: 'JavaScript runtime',
    hint: 'yt-dlp shells out to a JS runtime for some extractors.',
    required: false,
  },
];

interface WelcomeProps {
  onDone: () => void;
}

export function WelcomeView({ onDone }: WelcomeProps) {
  const [step, setStep] = useState<1 | 2 | 3>(1);

  const [cfg, setCfg] = useState<config.Config | null>(null);
  const [statuses, setStatuses] = useState<binaries.BinaryStatus[]>([]);
  const [configPath, setConfigPath] = useState('');
  const [vendorDir, setVendorDir] = useState('');

  const [installing, setInstalling] = useState(false);
  const [installError, setInstallError] = useState('');
  const [progress, setProgress] = useState<Map<string, SetupProgress>>(
    () => new Map(),
  );

  useEffect(() => {
    let alive = true;
    (async () => {
      const [c, st, cp, vd] = await Promise.all([
        GetConfig(),
        ResolveBinaries(),
        ConfigPath(),
        VendorDir(),
      ]);
      if (!alive) return;
      setCfg(c);
      setStatuses(st);
      setConfigPath(cp);
      setVendorDir(vd);
    })().catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  // Subscribe to install progress events for the lifetime of the wizard. Each
  // event is keyed by binary name in a Map so concurrent multi-binary installs
  // (we run sequentially today, but the channel doesn't care) render side by
  // side.
  useEffect(() => {
    const unsub = subscribeSetup((p) => {
      setProgress((m) => {
        const next = new Map(m);
        next.set(p.name, p);
        return next;
      });
    });
    return unsub;
  }, []);

  const statusFor = (doctorName: string) =>
    statuses.find((s) => s.name === doctorName);

  const missing = useMemo(
    () => DEPS.filter((d) => !(statusFor(d.doctorName)?.ok ?? false)),
    [statuses],
  );

  async function runInstalls() {
    if (!cfg) return;
    setInstalling(true);
    setInstallError('');
    // Start from the current working copy so successful installs from a prior
    // attempt (if the user retries) aren't dropped.
    let working = config.Config.createFrom(cfg);
    for (const dep of missing) {
      try {
        const path = await InstallDependency(dep.installName);
        working = Object.assign(config.Config.createFrom(working), {
          [dep.configField]: path,
        });
        setCfg(working);
      } catch (e) {
        // The Progress event has already been emitted with phase=error or
        // unsupported, so the panel already shows the failure. Capture a
        // top-level summary too in case the event arrived out of order.
        setInstallError(`${dep.label}: ${e}`);
      }
    }
    // Single explicit save at the end so the user has a clean commit point.
    try {
      await SaveConfig(working);
      const st = await ResolveBinaries();
      setStatuses(st);
    } catch (e) {
      setInstallError(`Save config: ${e}`);
    }
    setInstalling(false);
  }

  return (
    <div class="welcome">
      <div class="welcome-card">
        <div class="welcome-steps" aria-hidden>
          {[1, 2, 3].map((n) => (
            <div key={n} class={`welcome-step ${n <= step ? 'active' : ''}`} />
          ))}
        </div>

        {step === 1 && (
          <Screen1 onNext={() => setStep(2)} />
        )}
        {step === 2 && (
          <Screen2
            statuses={statuses}
            missing={missing}
            vendorDir={vendorDir}
            progress={progress}
            installing={installing}
            installError={installError}
            onInstall={runInstalls}
            onBack={() => setStep(1)}
            onNext={() => setStep(3)}
          />
        )}
        {step === 3 && (
          <Screen3 configPath={configPath} onDone={onDone} onBack={() => setStep(2)} />
        )}
      </div>
    </div>
  );
}

function Screen1({ onNext }: { onNext: () => void }) {
  return (
    <Fragment>
      <h1>Hi — welcome to ytdlp-ui</h1>
      <p>
        Heads up: this is <strong>aggressively WIP / alpha</strong> software.
        Hopefully you find it useful, but expect bugs, rough edges, and the
        occasional surprise. Please be forgiving.
      </p>
      <p>
        Thanks for trying it out. <span aria-hidden>🙇 ♥</span>
      </p>
      <div class="welcome-actions">
        <div class="spacer" />
        <button class="btn btn-primary" onClick={onNext}>
          Let&rsquo;s set up
        </button>
      </div>
    </Fragment>
  );
}

interface Screen2Props {
  statuses: binaries.BinaryStatus[];
  missing: Dep[];
  vendorDir: string;
  progress: Map<string, SetupProgress>;
  installing: boolean;
  installError: string;
  onInstall: () => void;
  onBack: () => void;
  onNext: () => void;
}

function Screen2(p: Screen2Props) {
  const statusFor = (name: string) =>
    p.statuses.find((s) => s.name === name);
  const showPanel = p.installing || p.progress.size > 0;

  return (
    <Fragment>
      <h1>Dependencies</h1>
      <p>
        ytdlp-ui uses a few external binaries. Here&rsquo;s what we found on
        your machine:
      </p>

      <ul class="deps-list">
        {DEPS.map((d) => {
          const st = statusFor(d.doctorName);
          const ok = st?.ok ?? false;
          return (
            <li class="deps-row" key={d.doctorName}>
              <div>
                <div class="deps-name">{d.label}</div>
                <div class="deps-detail">
                  {d.hint}
                  {st?.resolved ? ` · ${st.resolved}` : ''}
                </div>
              </div>
              <div
                class={`deps-status ${ok ? 'ok' : d.required ? 'bad' : 'pending'}`}
              >
                {ok ? '✓ found' : d.required ? '✗ missing' : '— not found'}
              </div>
            </li>
          );
        })}
      </ul>

      {p.missing.length > 0 && (
        <Fragment>
          <p style={{ marginTop: 16 }}>
            We can try to download the missing ones into{' '}
            <span class="welcome-path">{p.vendorDir || '…'}</span>.
          </p>
          <div class="welcome-actions" style={{ marginTop: 12 }}>
            <button
              class="btn btn-primary"
              onClick={p.onInstall}
              disabled={p.installing}
            >
              {p.installing ? 'Downloading…' : 'Download missing dependencies'}
            </button>
          </div>
        </Fragment>
      )}

      {showPanel && (
        <div class="install-panel">
          {DEPS.map((d) => {
            const pr = p.progress.get(d.installName);
            if (!pr) return null;
            const isErr = pr.phase === 'error' || pr.phase === 'unsupported';
            const isDone = pr.phase === 'done';
            const pct = isDone ? 100 : pr.percent || 0;
            return (
              <div class="install-row" key={d.installName}>
                <div class="install-row-head">
                  <strong>{d.label}</strong>
                  <span class="install-row-phase">{pr.phase}</span>
                </div>
                <div class="install-bar">
                  <div
                    class={`install-bar-fill ${
                      isErr ? 'bad' : isDone ? 'ok' : ''
                    }`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
                <div class="install-message">
                  {pr.error || pr.message || (isDone ? `Installed: ${pr.path}` : '')}
                </div>
              </div>
            );
          })}
        </div>
      )}

      {p.installError && (
        <p style={{ marginTop: 12, color: 'var(--bad)' }}>{p.installError}</p>
      )}

      <div class="welcome-actions">
        <button class="btn" onClick={p.onBack} disabled={p.installing}>
          Back
        </button>
        <div class="spacer" />
        <button class="btn btn-primary" onClick={p.onNext} disabled={p.installing}>
          {p.missing.length === 0 ? 'Continue' : 'Skip & continue'}
        </button>
      </div>
    </Fragment>
  );
}

function Screen3({
  configPath,
  onDone,
  onBack,
}: {
  configPath: string;
  onDone: () => void;
  onBack: () => void;
}) {
  return (
    <Fragment>
      <h1>All done</h1>
      <p>
        Everything has been stored at:
      </p>
      <p>
        <span class="welcome-path">{configPath || '…'}</span>
      </p>
      <p>
        You can edit it by hand or tweak everything from the Settings page.
        Happy downloading.
      </p>
      <div class="welcome-actions">
        <button class="btn" onClick={onBack}>
          Back
        </button>
        <div class="spacer" />
        <button class="btn btn-primary" onClick={onDone}>
          Get started
        </button>
      </div>
    </Fragment>
  );
}
