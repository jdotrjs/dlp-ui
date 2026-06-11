import './app.css';
import { h } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { IsFirstRun } from './api/client';
import { AppProvider, useApp } from './lib/AppContext';
import { DownloadsTray } from './downloads/DownloadsTray';
import { LibraryView } from './library/LibraryView';
import { SettingsView } from './settings/SettingsView';
import { WelcomeView } from './welcome/WelcomeView';

// Route is the set of top-level views reachable from the sidebar. No router
// library — a single useState drives which view renders (per the plan).
type Route = 'library' | 'settings';

interface NavItem {
  route: Route;
  label: string;
  icon: string;
}

const NAV_ITEMS: NavItem[] = [
  { route: 'library', label: 'Library', icon: '▣' },
  { route: 'settings', label: 'Settings', icon: '⚙' },
];

// App is the AppShell: a collapsible left sidebar (Library / Settings nav,
// toggled to icon-only via the «/» button) plus the routed main content area.
//
// On startup we ask the backend whether this is a first run (no config.json
// existed pre-startup). If so we render the welcome wizard instead of the
// shell until the user dismisses it. `showWelcome` is null until IsFirstRun
// resolves so we don't flash the shell before the check.
export function App() {
  const [route, setRoute] = useState<Route>('library');
  const [collapsed, setCollapsed] = useState(false);
  const [showWelcome, setShowWelcome] = useState<boolean | null>(null);

  useEffect(() => {
    let alive = true;
    IsFirstRun()
      .then((v) => alive && setShowWelcome(v))
      .catch(() => alive && setShowWelcome(false));
    return () => {
      alive = false;
    };
  }, []);

  if (showWelcome === null) {
    return null;
  }
  if (showWelcome) {
    return <WelcomeView onDone={() => setShowWelcome(false)} />;
  }

  return (
    <AppProvider>
    <div class={`shell ${collapsed ? 'shell-collapsed' : ''}`}>
      <Sidebar
        route={route}
        setRoute={setRoute}
        collapsed={collapsed}
        setCollapsed={setCollapsed}
      />

      <main class="content">
        {route === 'library' ? <LibraryView /> : <SettingsView />}
      </main>

      {/* Docked downloads tray: active + recent downloads with progress bars
          and Cancel/Dismiss. Hidden entirely when there are no downloads. */}
      <DownloadsTray />
    </div>
    </AppProvider>
  );
}

// Sidebar lives inside AppProvider so it can read updateStatus from context
// and decorate the Settings nav item with a subtle update-available dot. The
// dot is route-agnostic (visible whether or not Settings is the active view)
// so an unacknowledged update keeps nudging the user even after they navigate
// away.
function Sidebar(props: {
  route: Route;
  setRoute: (r: Route) => void;
  collapsed: boolean;
  setCollapsed: (fn: (c: boolean) => boolean) => void;
}) {
  const { updateStatus } = useApp();
  const updateAvailable = updateStatus?.updateAvailable ?? false;

  return (
    <nav class="sidebar">
      {!props.collapsed && <div class="sidebar-brand">ytdlp-ui</div>}
      <ul class="nav-list">
        {NAV_ITEMS.map((item) => {
          const showBadge = item.route === 'settings' && updateAvailable;
          return (
            <li key={item.route}>
              <button
                class={`nav-item ${props.route === item.route ? 'nav-item-active' : ''}`}
                onClick={() => props.setRoute(item.route)}
                title={showBadge ? `${item.label} (update available)` : item.label}
              >
                <span class="nav-icon">{item.icon}</span>
                {!props.collapsed && <span class="nav-label">{item.label}</span>}
                {showBadge && <span class="nav-update-dot" aria-label="update available" />}
              </button>
            </li>
          );
        })}
      </ul>
      <button
        class="collapse-toggle"
        onClick={() => props.setCollapsed((c) => !c)}
        title={props.collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
      >
        {props.collapsed ? '»' : '«'}
      </button>
    </nav>
  );
}
