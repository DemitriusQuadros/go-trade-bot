import React, { useState } from 'react';
import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Cpu,
  History,
  TrendingUp,
  Activity,
  Download,
  Settings,
  HelpCircle,
  ExternalLink,
  BarChart3,
  LineChart,
  Terminal,
  ListChecks,
} from 'lucide-react';
import { usePlatformSettings } from '@/hooks/queries';

export function MiniSidebar() {
  const [expanded, setExpanded] = useState(false);
  const { data: settings } = usePlatformSettings();

  return (
    <aside
      className={`fixed left-0 top-0 h-screen bg-card border-r border-border transition-all duration-300 z-50 flex flex-col ${
        expanded ? 'w-48' : 'w-14'
      }`}
      onMouseEnter={() => setExpanded(true)}
      onMouseLeave={() => setExpanded(false)}
    >
      <div className="flex items-center gap-2.5 h-14 px-3 border-b border-border overflow-hidden shrink-0">
        <img src="/gopher-face.png" alt="" className="w-8 h-8 rounded-md object-cover shrink-0" />
        <span
          className={`font-semibold text-sm tracking-tight text-foreground whitespace-nowrap transition-opacity duration-300 ${
            expanded ? 'opacity-100' : 'opacity-0'
          }`}
        >
          GTB <span className="text-muted-foreground font-mono font-normal text-xs">v2.0</span>
        </span>
      </div>

      {/* Grouped by workflow (Overview / Trade / Analyze / Manage) instead
          of one flat list - see the IA review this replaced: ten equally-
          weighted items with no way to tell "used constantly" from
          "touched once a quarter" apart. Script REPL and the standalone
          Backtest launcher are gone from here entirely (folded into the
          per-strategy Workbench's own tabs); Positions/Execution Log/Agent
          History collapsed into one Activity entry. */}
      <nav className="flex-1 py-3 flex flex-col gap-0.5 overflow-y-auto px-2">
        <GroupLabel expanded={expanded}>Overview</GroupLabel>
        <NavItem to="/" icon={<LayoutDashboard className="w-5 h-5" />} label="Dashboard" expanded={expanded} />

        <GroupLabel expanded={expanded}>Trade</GroupLabel>
        <NavItem
          to="/strategies"
          icon={<Cpu className="w-5 h-5" />}
          label="Strategies"
          expanded={expanded}
          dataWalkthrough="nav-strategies"
        />
        <NavItem to="/activity" icon={<Activity className="w-5 h-5" />} label="Activity" expanded={expanded} />

        <GroupLabel expanded={expanded}>Analyze</GroupLabel>
        <NavItem
          to="/backtest"
          icon={<History className="w-5 h-5" />}
          label="Backtest Runs"
          expanded={expanded}
          dataWalkthrough="nav-backtest"
        />
        <NavItem to="/optimization" icon={<TrendingUp className="w-5 h-5" />} label="Optimization" expanded={expanded} />

        <GroupLabel expanded={expanded}>Manage</GroupLabel>
        <NavItem to="/candles" icon={<Download className="w-5 h-5" />} label="Candle Import" expanded={expanded} />
        <NavItem
          to="/settings"
          icon={<Settings className="w-5 h-5" />}
          label="Settings"
          expanded={expanded}
          dataWalkthrough="nav-settings"
        />
        <NavItem
          to="/help"
          icon={<HelpCircle className="w-5 h-5" />}
          label="Help & Docs"
          expanded={expanded}
          dataWalkthrough="nav-help"
        />

        {/* Monitoring External Links */}
        {(settings?.prometheus_url || settings?.grafana_url || settings?.asynqmon_url) && (
          <>
            <GroupLabel expanded={expanded}>Monitoring</GroupLabel>
            <div className="flex flex-col gap-1">
              {settings?.prometheus_url && (
                <ExternalNavItem
                  href={settings.prometheus_url}
                  icon={<BarChart3 className="w-5 h-5" />}
                  label="Prometheus"
                  expanded={expanded}
                />
              )}
              {settings?.grafana_url && (
                <ExternalNavItem
                  href={settings.grafana_url}
                  icon={<LineChart className="w-5 h-5" />}
                  label="Grafana"
                  expanded={expanded}
                />
              )}
              {settings?.asynqmon_url && (
                <ExternalNavItem
                  href={settings.asynqmon_url}
                  icon={<ListChecks className="w-5 h-5" />}
                  label="Asynqmon"
                  expanded={expanded}
                />
              )}
            </div>
          </>
        )}
      </nav>
    </aside>
  );
}

// Collapsed (icon-only rail): no room for a label, so this renders nothing
// and a border does the separating instead - a text label would either
// wrap or get clipped at 56px wide. Expanded: a small uppercase caption,
// the same role the artifact's IA review's section headers played.
function GroupLabel({ expanded, children }: { expanded: boolean; children: React.ReactNode }) {
  if (!expanded) {
    return <div className="my-1.5 mx-1 border-t border-border/60" />;
  }
  return (
    <div className="mt-3 mb-1 px-2 text-[10px] font-mono font-semibold uppercase tracking-wider text-muted-foreground/70 first:mt-0">
      {children}
    </div>
  );
}

function NavItem({
  to,
  icon,
  label,
  expanded,
  dataWalkthrough,
}: {
  to: string;
  icon: React.ReactNode;
  label: string;
  expanded: boolean;
  dataWalkthrough?: string;
}) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      data-walkthrough={dataWalkthrough}
      className={({ isActive }) =>
        `flex items-center gap-3 px-2 py-2 rounded-md transition-colors overflow-hidden ${
          isActive
            ? 'bg-accent text-accent-foreground font-semibold'
            : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'
        }`
      }
      title={expanded ? undefined : label}
    >
      <div className="shrink-0">{icon}</div>
      <span
        className={`text-sm whitespace-nowrap transition-opacity duration-300 ${
          expanded ? 'opacity-100' : 'opacity-0 w-0'
        }`}
      >
        {label}
      </span>
    </NavLink>
  );
}

function ExternalNavItem({
  href,
  icon,
  label,
  expanded,
}: {
  href: string;
  icon: React.ReactNode;
  label: string;
  expanded: boolean;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={`${label} (opens in new tab)`}
      className="flex items-center gap-3 px-2 py-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 overflow-hidden transition-colors"
      title={expanded ? undefined : label}
    >
      <div className="shrink-0">{icon}</div>
      <span
        className={`text-sm whitespace-nowrap transition-opacity duration-300 flex items-center gap-1.5 ${
          expanded ? 'opacity-100' : 'opacity-0 w-0'
        }`}
      >
        {label}
        <ExternalLink className="w-3 h-3 opacity-60" />
      </span>
    </a>
  );
}
