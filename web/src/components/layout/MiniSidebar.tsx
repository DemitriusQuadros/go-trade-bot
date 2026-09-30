import React, { useState } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import {
  LayoutDashboard,
  Cpu,
  History,
  TrendingUp,
  Activity,
  Download,
  Settings,
  HelpCircle,
  Terminal,
  Bot,
  FileText,
  GitPullRequest,
  Users,
} from 'lucide-react';
import { useAuth } from '@/context/AuthContext';
import { usePendingProposalCount } from '@/hooks/queries';
import { AgentKillSwitch } from '@/components/domain/AgentKillSwitch';

export function MiniSidebar() {
  const [expanded, setExpanded] = useState(false);
  // Polled every 60s; approve/reject invalidate it immediately (B-02 §2).
  const { data: pendingProposals = 0 } = usePendingProposalCount();
  // Settings and Users are admin only (auth-02 §4).
  const isAdmin = useAuth().can('admin');

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
        {/* /agents/reports is nested under /agents, so /agents is
            a prefix of it - activeWhen keeps Agents highlighted on its
            editor/runs pages but not on a report page. */}
        <NavItem
          to="/agents"
          icon={<Bot className="w-5 h-5" />}
          label="Agents"
          expanded={expanded}
          activeWhen={(path) =>
            path.startsWith('/agents') && !path.startsWith('/agents/reports') && !path.startsWith('/agents/proposals')
          }
        />
        <NavItem to="/agents/reports" icon={<FileText className="w-5 h-5" />} label="Reports" expanded={expanded} />
        <NavItem
          to="/agents/proposals"
          icon={<GitPullRequest className="w-5 h-5" />}
          label="Proposals"
          expanded={expanded}
          badge={pendingProposals}
          badgeLabel={`${pendingProposals} pending proposal${pendingProposals === 1 ? '' : 's'}`}
        />

        <GroupLabel expanded={expanded}>Manage</GroupLabel>
        <NavItem to="/candles" icon={<Download className="w-5 h-5" />} label="Candle Import" expanded={expanded} />
        {isAdmin && (
          <NavItem
            to="/settings"
            icon={<Settings className="w-5 h-5" />}
            label="Settings"
            expanded={expanded}
            dataWalkthrough="nav-settings"
          />
        )}
        {isAdmin && <NavItem to="/users" icon={<Users className="w-5 h-5" />} label="Users" expanded={expanded} />}
        <NavItem
          to="/help"
          icon={<HelpCircle className="w-5 h-5" />}
          label="Help & Docs"
          expanded={expanded}
          dataWalkthrough="nav-help"
        />

        {/* External monitoring links (Prometheus, Grafana, Asynqmon) live in
            the top bar's Monitoring menu (AppLayout) - not here. */}
      </nav>

      {/* Global agents kill switch - pinned to the footer so it's reachable
          from every page without scrolling the nav. */}
      <div className="shrink-0 border-t border-border p-2">
        <AgentKillSwitch expanded={expanded} />
      </div>
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
  activeWhen,
  badge,
  badgeLabel,
  badgeMax = 99,
}: {
  to: string;
  icon: React.ReactNode;
  label: string;
  expanded: boolean;
  dataWalkthrough?: string;
  // Count chip (e.g. pending proposals). Hidden at 0. Collapsed rail: a
  // small dot-count over the icon; expanded: a pill after the label.
  badge?: number;
  badgeLabel?: string;
  badgeMax?: number;
  // Overrides NavLink's own prefix matching when two entries share a
  // prefix (/agents vs /agents/reports).
  activeWhen?: (pathname: string) => boolean;
}) {
  const location = useLocation();
  return (
    <NavLink
      to={to}
      end={to === '/'}
      data-walkthrough={dataWalkthrough}
      className={({ isActive }) =>
        `flex items-center gap-3 px-2 py-2 rounded-md transition-colors overflow-hidden ${
          (activeWhen ? activeWhen(location.pathname) : isActive)
            ? 'bg-accent text-accent-foreground font-semibold'
            : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'
        }`
      }
      title={expanded ? undefined : badge ? `${label} (${badgeLabel ?? badge})` : label}
      aria-label={badge ? `${label}, ${badgeLabel ?? badge}` : undefined}
    >
      <div className="shrink-0 relative">
        {icon}
        {!!badge && !expanded && (
          <span
            aria-hidden="true"
            className="absolute -top-1.5 -right-2 min-w-[1rem] h-4 px-1 rounded-full bg-warning text-warning-foreground text-[9px] font-bold leading-4 text-center"
          >
            {badge > badgeMax ? `${badgeMax}+` : badge}
          </span>
        )}
      </div>
      <span
        className={`text-sm whitespace-nowrap transition-opacity duration-300 ${
          expanded ? 'opacity-100' : 'opacity-0 w-0'
        }`}
      >
        {label}
      </span>
      {!!badge && expanded && (
        <span
          aria-hidden="true"
          className="ml-auto min-w-[1.25rem] h-5 px-1.5 rounded-full bg-warning text-warning-foreground text-[10px] font-bold leading-5 text-center"
        >
          {badge > badgeMax ? `${badgeMax}+` : badge}
        </span>
      )}
    </NavLink>
  );
}
