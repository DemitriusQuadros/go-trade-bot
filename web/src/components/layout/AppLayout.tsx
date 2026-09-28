import React from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { MiniSidebar } from './MiniSidebar';
import { Activity, BarChart3, Bot, ChevronDown, ExternalLink, LineChart, ListChecks, LogOut, Moon, Settings, Sun } from 'lucide-react';
import { clearToken } from '@/api/client';
import { PlatformSettings } from '@/api/types';
import { DropdownMenu, DropdownMenuItem } from '@/components/ui/DropdownMenu';
import { ErrorBoundary } from '@/components/ui/ErrorBoundary';
import { usePlatformSettings } from '@/hooks/queries';
import { useTheme } from '@/hooks/useTheme';

// Header buttons share one look: muted icon, accent hover.
const HEADER_BUTTON =
  'flex items-center gap-1.5 px-2 py-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 text-xs transition-colors';

// External monitoring dashboards (Settings > Monitoring & Observability
// Endpoints). Unconfigured URLs are hidden; with none configured the menu
// offers a single shortcut to Settings instead of an empty list.
function MonitoringMenu() {
  const navigate = useNavigate();
  const { data: settings } = usePlatformSettings();

  const links: { label: string; key: keyof PlatformSettings; icon: React.ReactNode }[] = [
    { label: 'Prometheus', key: 'prometheus_url', icon: <BarChart3 /> },
    { label: 'Grafana', key: 'grafana_url', icon: <LineChart /> },
    { label: 'Asynqmon – Worker', key: 'asynqmon_url', icon: <ListChecks /> },
    { label: 'Asynqmon – Agents', key: 'agents_asynqmon_url', icon: <Bot /> },
  ];

  const items: DropdownMenuItem[] = links.flatMap(({ label, key, icon }) => {
    const url = settings?.[key];
    return typeof url === 'string' && url.trim()
      ? [{ label, icon, href: url.trim(), external: true, trailingIcon: <ExternalLink /> }]
      : [];
  });

  if (items.length === 0) {
    items.push({ label: 'Configure monitoring links…', icon: <Settings />, onClick: () => navigate('/settings') });
  }

  return (
    <DropdownMenu
      items={items}
      align="right"
      label="Monitoring dashboards"
      triggerClassName={HEADER_BUTTON}
      trigger={
        <>
          <Activity className="w-4 h-4" />
          <span className="hidden sm:inline">Monitoring</span>
          <ChevronDown className="w-3 h-3 opacity-60" />
        </>
      }
    />
  );
}

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const { theme, toggleTheme } = useTheme();
  const handleLogout = () => {
    clearToken();
    window.location.reload();
  };

  return (
    <div className="min-h-screen bg-background text-foreground flex">
      <MiniSidebar />
      <div className="flex-1 flex flex-col ml-14 transition-all duration-300 min-w-0">
        <header className="sticky top-0 z-40 bg-background/90 backdrop-blur-md border-b border-border">
          <div className="h-14 flex items-center justify-end gap-1 px-4">
            <MonitoringMenu />
            <button
              onClick={toggleTheme}
              className="p-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors"
              title={theme === 'dark' ? 'Switch to light (paper) mode' : 'Switch to dark mode'}
              aria-label="Toggle color theme"
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </button>
            <button
              onClick={handleLogout}
              className={HEADER_BUTTON}
              title="Disconnect and clear token"
            >
              <LogOut className="w-4 h-4" />
              <span>Logout</span>
            </button>
          </div>
        </header>
        {/* min-w-0: a flex item's default min-width is auto (its content's
            min-content size), which lets a page with a wide flex-wrap row
            (e.g. WorkbenchShell's header) silently force this whole column
            wider than the viewport instead of actually wrapping - main
            grows past its own flex parent and gets its own horizontal
            scrollbar rather than letting content inside it wrap or shrink. */}
        <main className="flex-1 p-6 overflow-auto min-w-0">
          <ErrorBoundary resetKey={location.pathname}>{children}</ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
