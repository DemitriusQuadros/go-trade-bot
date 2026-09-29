import React, { useCallback, useEffect } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { MiniSidebar } from './MiniSidebar';
import {
  Activity,
  ArrowLeft,
  BarChart3,
  Bot,
  ChevronDown,
  Code2,
  ExternalLink,
  GitPullRequest,
  LineChart,
  ListChecks,
  LogOut,
  Moon,
  Settings,
  Sun,
} from 'lucide-react';
import { clearToken } from '@/api/client';
import { PlatformSettings } from '@/api/types';
import { DropdownMenu, DropdownMenuItem } from '@/components/ui/DropdownMenu';
import { ErrorBoundary } from '@/components/ui/ErrorBoundary';
import { AgentDock, AGENT_DOCK_OPEN_KEY } from '@/components/domain/AgentDock';
import { AgentKillSwitch } from '@/components/domain/AgentKillSwitch';
import { AGENT_MODE_PATH, agentModeHref, useChatSession } from '@/context/ChatSessionContext';
import { useEditorBridge } from '@/context/EditorBridgeContext';
import { usePendingProposalCount, usePlatformSettings } from '@/hooks/queries';
import { usePersistedOpen } from '@/hooks/usePersistedOpen';
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

// Code mode remembers where it was (Phase D-02 §2) so "Code" in the mode
// toggle returns there. sessionStorage: per tab, best-effort.
const LAST_CODE_LOCATION_KEY = 'gtb_last_code_location';

function readLastCodeLocation(): string {
  try {
    return sessionStorage.getItem(LAST_CODE_LOCATION_KEY) || '/';
  } catch {
    return '/';
  }
}

const SEGMENT = 'flex items-center gap-1.5 px-2.5 py-1.5 text-xs transition-colors';

// Agent | Code segmented toggle - same look as the Workbench's
// Script/Split/Chart control.
function ModeToggle({ agentMode, onAgent, onCode }: { agentMode: boolean; onAgent: () => void; onCode: () => void }) {
  return (
    <div
      role="group"
      aria-label="App mode"
      data-walkthrough="mode-toggle"
      className="flex items-center bg-background border border-border rounded overflow-hidden shrink-0"
    >
      <button
        type="button"
        onClick={onAgent}
        aria-pressed={agentMode}
        title="Agent mode - full-screen conversation (Ctrl+Shift+.)"
        data-testid="mode-toggle-agent"
        className={`${SEGMENT} border-r border-border ${
          agentMode ? 'bg-secondary text-foreground font-semibold' : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'
        }`}
      >
        <Bot className="w-4 h-4" />
        <span>Agent</span>
      </button>
      <button
        type="button"
        onClick={onCode}
        aria-pressed={!agentMode}
        title="Code mode - the app, with the agent as a side dock (Ctrl+Shift+.)"
        data-testid="mode-toggle-code"
        className={`${SEGMENT} ${
          !agentMode ? 'bg-secondary text-foreground font-semibold' : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'
        }`}
      >
        <Code2 className="w-4 h-4" />
        <span>Code</span>
      </button>
    </div>
  );
}

function PendingProposalsIndicator() {
  const { data: pending = 0 } = usePendingProposalCount();
  return (
    <Link
      to="/agents/proposals"
      className={HEADER_BUTTON}
      title={`${pending} pending proposal${pending === 1 ? '' : 's'}`}
      aria-label={`Proposals, ${pending} pending`}
    >
      <GitPullRequest className="w-4 h-4" />
      <span className="hidden sm:inline">Proposals</span>
      {pending > 0 && (
        <span className="min-w-[1.25rem] h-5 px-1.5 rounded-full bg-warning text-warning-foreground text-[10px] font-bold leading-5 text-center">
          {pending > 99 ? '99+' : pending}
        </span>
      )}
    </Link>
  );
}

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { theme, toggleTheme } = useTheme();
  const chat = useChatSession();
  const bridge = useEditorBridge();
  const agentMode = location.pathname === AGENT_MODE_PATH;
  const [dockOpen, setDockOpen] = usePersistedOpen(AGENT_DOCK_OPEN_KEY, false);
  const handleLogout = () => {
    clearToken();
    window.location.reload();
  };

  useEffect(() => {
    if (agentMode) return;
    try {
      sessionStorage.setItem(LAST_CODE_LOCATION_KEY, location.pathname + location.search);
    } catch {
      /* best-effort */
    }
  }, [agentMode, location.pathname, location.search]);

  const goAgent = useCallback(() => {
    if (agentMode) return;
    // Inside the Workbench for strategy N -> that strategy's conversation.
    navigate(agentModeHref(bridge?.strategyId ?? chat.strategyId));
  }, [agentMode, bridge, chat.strategyId, navigate]);
  const goCode = useCallback(() => {
    if (!agentMode) return;
    navigate(readLastCodeLocation());
  }, [agentMode, navigate]);

  // Ctrl+Shift+. toggles the mode; Ctrl+. toggles the dock (Code mode).
  // Ctrl (not Cmd) on every platform: Chrome on macOS reserves Cmd+Shift+A
  // (tab search) and Cmd+J (downloads), so those never reach the page.
  // Matched on e.code because Shift turns e.key '.' into '>'.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey || e.metaKey || e.altKey || e.code !== 'Period') return;
      if (e.shiftKey) {
        e.preventDefault();
        if (agentMode) goCode();
        else goAgent();
      } else if (!agentMode) {
        e.preventDefault();
        setDockOpen((o) => !o);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [agentMode, goAgent, goCode, setDockOpen]);

  if (agentMode) {
    // Agent mode: header only (no sidebar nav), full-width content.
    return (
      <div className="min-h-screen bg-background text-foreground flex flex-col">
        <header className="sticky top-0 z-40 bg-background/90 backdrop-blur-md border-b border-border">
          <div className="h-14 flex items-center gap-2 px-3 sm:px-4 min-w-0">
            <Link to="/" className="flex items-center gap-2 shrink-0" title="Dashboard">
              <img src="/gopher-face.png" alt="" className="w-8 h-8 rounded-md object-cover" />
              <span className="hidden md:inline font-semibold text-sm tracking-tight text-foreground">GTB</span>
            </Link>
            <ModeToggle agentMode onAgent={goAgent} onCode={goCode} />
            <button type="button" onClick={goCode} className={`${HEADER_BUTTON} hidden md:flex`} title="Back to where you were in Code mode">
              <ArrowLeft className="w-4 h-4" />
              <span>Back to Code mode</span>
            </button>
            <div className="ml-auto flex items-center gap-1 min-w-0">
              <PendingProposalsIndicator />
              <div className="w-10 md:w-44 shrink-0">
                <AgentKillSwitch expanded />
              </div>
              <button
                onClick={toggleTheme}
                className="p-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors"
                title={theme === 'dark' ? 'Switch to light (paper) mode' : 'Switch to dark mode'}
                aria-label="Toggle color theme"
              >
                {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
              </button>
            </div>
          </div>
        </header>
        <main className="flex-1 min-w-0">
          <ErrorBoundary resetKey={location.pathname}>{children}</ErrorBoundary>
        </main>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-background text-foreground flex">
      <MiniSidebar />
      <div className="flex-1 flex flex-col ml-14 transition-all duration-300 min-w-0">
        <header className="sticky top-0 z-40 bg-background/90 backdrop-blur-md border-b border-border">
          <div className="h-14 flex items-center justify-end gap-1 px-4">
            <div className="mr-auto">
              <ModeToggle agentMode={false} onAgent={goAgent} onCode={goCode} />
            </div>
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
        <div className="flex-1 flex min-w-0">
          <main className="flex-1 p-6 overflow-auto min-w-0">
            <ErrorBoundary resetKey={location.pathname}>{children}</ErrorBoundary>
          </main>
          {/* The agent chat dock (Phase D-02 §5) - a flex sibling of main, so
              opening it narrows the page instead of covering it. */}
          <AgentDock open={dockOpen} onOpenChange={setDockOpen} />
        </div>
      </div>
    </div>
  );
}
