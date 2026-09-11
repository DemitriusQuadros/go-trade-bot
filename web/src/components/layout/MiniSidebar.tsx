import React, { useState } from 'react';
import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Cpu,
  Layers,
  History,
  TrendingUp,
  FileSpreadsheet,
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
      className={`fixed left-0 top-0 h-screen bg-slate-950 border-r border-slate-800 transition-all duration-300 z-50 flex flex-col ${
        expanded ? 'w-48' : 'w-14'
      }`}
      onMouseEnter={() => setExpanded(true)}
      onMouseLeave={() => setExpanded(false)}
    >
      <div className="flex items-center gap-2.5 h-14 px-3 border-b border-slate-800 overflow-hidden shrink-0">
        <div className="p-1.5 bg-green-600 rounded-lg text-black shrink-0">
          <Activity className="w-4 h-4" />
        </div>
        <span
          className={`font-bold text-sm tracking-tight text-slate-100 font-mono whitespace-nowrap transition-opacity duration-300 ${
            expanded ? 'opacity-100' : 'opacity-0'
          }`}
        >
          GTB <span className="text-green-500 font-sans font-normal text-xs">v2.0</span>
        </span>
      </div>

      <nav className="flex-1 py-4 flex flex-col gap-1.5 overflow-y-auto px-2">
        <NavItem to="/" icon={<LayoutDashboard className="w-5 h-5" />} label="Overview" expanded={expanded} />
        <NavItem
          to="/strategies"
          icon={<Cpu className="w-5 h-5" />}
          label="Strategies"
          expanded={expanded}
          dataWalkthrough="nav-strategies"
        />
        <NavItem
          to="/scripts/repl"
          icon={<Terminal className="w-5 h-5" />}
          label="Script REPL"
          expanded={expanded}
        />
        <NavItem to="/positions" icon={<Layers className="w-5 h-5" />} label="Positions" expanded={expanded} />
        <NavItem
          to="/backtest"
          icon={<History className="w-5 h-5" />}
          label="Backtest"
          expanded={expanded}
          dataWalkthrough="nav-backtest"
        />
        <NavItem to="/optimization" icon={<TrendingUp className="w-5 h-5" />} label="Optimization" expanded={expanded} />
        <NavItem to="/execution" icon={<FileSpreadsheet className="w-5 h-5" />} label="Execution Log" expanded={expanded} />
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
          <div className="mt-2 pt-2 border-t border-slate-800 flex flex-col gap-1">
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
        )}
      </nav>
    </aside>
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
            ? 'bg-slate-800 text-green-400 font-semibold'
            : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/50'
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
      className="flex items-center gap-3 px-2 py-2 rounded-md text-slate-400 hover:text-slate-200 hover:bg-slate-800/50 overflow-hidden transition-colors"
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
