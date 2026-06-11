// events.ts wraps the Wails event runtime for the single 'download' channel.
//
// The Go side emits one channel ('download') carrying a DownloadEvent payload
// (see internal/download). subscribeDownloads sets up an EventsOn listener that
// forwards each (typed) event to the supplied handler and returns an
// unsubscribe function — the AppContext subscribes once and dispatches into its
// downloads reducer. Keeping the runtime import behind this module mirrors how
// client.ts hides the generated bindings.
import { EventsOn } from '../../wailsjs/runtime/runtime';
import {
  DOWNLOAD_EVENT,
  DownloadEvent,
  SETUP_EVENT,
  SetupProgress,
  UPDATE_STATUS_EVENT,
} from '../lib/types';
import { updater } from './client';

// subscribeDownloads registers a listener on the 'download' channel and returns
// the unsubscribe function (call it on cleanup). Wails delivers the emitted
// payload as the first variadic arg.
export function subscribeDownloads(
  handler: (event: DownloadEvent) => void,
): () => void {
  return EventsOn(DOWNLOAD_EVENT, (payload: DownloadEvent) => {
    if (payload && typeof payload.type === 'string') {
      handler(payload);
    }
  });
}

// subscribeSetup registers a listener on the welcome-wizard install-progress
// channel and returns the unsubscribe function. Each call to InstallDependency
// fires a stream of SetupProgress payloads keyed by the binary name.
export function subscribeSetup(
  handler: (event: SetupProgress) => void,
): () => void {
  return EventsOn(SETUP_EVENT, (payload: SetupProgress) => {
    if (payload && typeof payload.name === 'string') {
      handler(payload);
    }
  });
}

// subscribeUpdateStatus registers a listener on the update-status channel and
// returns the unsubscribe function. The Go side emits the channel after the
// startup auto-check or a manual CheckForUpdate so the sidebar badge can
// refresh without polling.
export function subscribeUpdateStatus(
  handler: (info: updater.Info) => void,
): () => void {
  return EventsOn(UPDATE_STATUS_EVENT, (payload: updater.Info) => {
    if (payload && typeof payload.currentVersion === 'string') {
      handler(payload);
    }
  });
}
