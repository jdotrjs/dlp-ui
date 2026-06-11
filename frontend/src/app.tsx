import './app.css';
import { h } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { IsFirstRun } from './api/client';
import { AppProvider } from './lib/AppContext';
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
      <nav class="sidebar">
        {!collapsed && <div class="sidebar-brand">ytdlp-ui</div>}
        <ul class="nav-list">
          {NAV_ITEMS.map((item) => (
            <li key={item.route}>
              <button
                class={`nav-item ${route === item.route ? 'nav-item-active' : ''}`}
                onClick={() => setRoute(item.route)}
                title={item.label}
              >
                <span class="nav-icon">{item.icon}</span>
                {!collapsed && <span class="nav-label">{item.label}</span>}
              </button>
            </li>
          ))}
        </ul>
        <button
          class="collapse-toggle"
          onClick={() => setCollapsed((c) => !c)}
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed ? '»' : '«'}
        </button>
      </nav>

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
