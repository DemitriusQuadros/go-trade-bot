import React, { useMemo } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { ArrowRight, GitPullRequest, RefreshCw } from 'lucide-react';
import { Proposal, Strategy } from '@/api/types';
import { apiErrorMessage } from '@/api/client';
import { useAgents, useProposals, useStrategies } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { StatusBadge } from '@/components/ui/StatusBadge';
import {
  EarlyBadge,
  GateBadge,
  LiveTargetBadge,
  ProposalKindBadge,
  ProposalStatusBadge,
} from '@/components/domain/AgentBadges';
import { formatRelative } from '@/lib/time';
import { HISTORY_STATUSES, ProposalTab } from '@/lib/proposals';

const TABS: { key: ProposalTab; label: string; hint: string }[] = [
  { key: 'pending', label: 'Pending', hint: 'Waiting for your decision.' },
  {
    key: 'approved',
    label: 'Approved',
    hint: 'Approved and waiting for the target strategy to have no open position - they apply automatically.',
  },
  { key: 'history', label: 'History', hint: 'Applied, rejected, superseded and failed proposals.' },
];

const TAB_KEYS = TABS.map((t) => t.key);

// Proposals inbox (B-02 §2) - /agents/proposals. Tab and filters live in
// the URL (?tab=&strategy_id=&agent_id=) so the workbench's challenger
// banner can link to a filtered view. strategy_id matches a proposal's
// target OR its challenger.
export function Proposals() {
  const [params, setParams] = useSearchParams();
  const tabParam = params.get('tab') as ProposalTab | null;
  const tab: ProposalTab = tabParam && TAB_KEYS.includes(tabParam) ? tabParam : 'pending';
  const strategyId = params.get('strategy_id') ? Number(params.get('strategy_id')) : undefined;
  const agentId = params.get('agent_id') ? Number(params.get('agent_id')) : undefined;

  const { data: strategies = [] } = useStrategies();
  const { data: agents = [] } = useAgents();

  // All tabs filter server-side; History sends its four terminal statuses
  // as a comma-separated list.
  const { data, isLoading, isFetching, error, refetch, fetchNextPage, hasNextPage, isFetchingNextPage } = useProposals({
    status: tab === 'history' ? HISTORY_STATUSES.join(',') : tab,
    strategy_id: strategyId,
    agent_id: agentId,
  });

  const proposals = useMemo(() => data?.pages.flat().filter(Boolean) ?? [], [data]);

  const strategyById = useMemo(() => {
    const m = new Map<number, Strategy>();
    strategies.forEach((s) => m.set(s.id, s));
    return m;
  }, [strategies]);

  const setParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(params);
    if (value == null || value === 'all') next.delete(key);
    else next.set(key, value);
    setParams(next, { replace: true });
  };

  const filtered = !!(strategyId || agentId);
  const activeTab = TABS.find((t) => t.key === tab)!;

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <GitPullRequest className="w-6 h-6" />
            Proposals
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Code changes your agents want to make but can't apply on their own - live promotions and changes that
            failed the deploy gate. Nothing here applies until you approve it.
          </p>
        </div>

        <div role="tablist" aria-label="Proposal status" className="flex items-center gap-2 bg-card/20 p-1 rounded-lg border border-border/30">
          {TABS.map((t) => (
            <button
              key={t.key}
              role="tab"
              aria-selected={tab === t.key}
              onClick={() => setParam('tab', t.key === 'pending' ? null : t.key)}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
                tab === t.key ? 'bg-secondary text-foreground shadow' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      <Card>
        <div className="flex flex-wrap items-center gap-3">
          <FilterSelect
            id="proposal-filter-strategy"
            label="Target"
            value={strategyId != null ? String(strategyId) : 'all'}
            onChange={(v) => setParam('strategy_id', v)}
          >
            <option value="all">All strategies</option>
            {strategies.map((s) => (
              <option key={s.id} value={s.id}>
                #{s.id} — {s.name}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect
            id="proposal-filter-agent"
            label="Agent"
            value={agentId != null ? String(agentId) : 'all'}
            onChange={(v) => setParam('agent_id', v)}
          >
            <option value="all">All agents</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </FilterSelect>
          <button
            onClick={() => refetch()}
            className="ml-auto bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
            title="Refresh"
            aria-label="Refresh proposals"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </Card>

      <Card>
        <CardHeader title={activeTab.label} subtitle={activeTab.hint} />

        {isLoading ? (
          <LoadingScreen message="Loading proposals..." />
        ) : error ? (
          <div className="p-8 text-center text-xs text-destructive bg-destructive/10 rounded-lg">
            Couldn't load proposals: {apiErrorMessage(error)}
          </div>
        ) : proposals.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg space-y-2">
            <p>
              {tab === 'pending'
                ? `Nothing waiting for you${filtered ? ' with these filters' : ''}.`
                : tab === 'approved'
                  ? 'No approved proposals are waiting to apply.'
                  : `No past proposals${filtered ? ' match these filters' : ' yet'}.`}
            </p>
            {tab === 'pending' && !filtered && (
              <p>
                Agents with the <span className="font-mono">propose_live</span> or{' '}
                <span className="font-mono">edit_testing</span> permission file proposals here.{' '}
                <Link to="/agents" className="text-primary hover:underline">
                  Configure an agent
                </Link>
                .
              </p>
            )}
          </div>
        ) : (
          <ul className="space-y-2">
            {proposals.map((p) => (
              <li key={p.id}>
                <ProposalRow proposal={p} target={strategyById.get(p.target_strategy_id)} showStatus={tab === 'history'} />
              </li>
            ))}
          </ul>
        )}

        {hasNextPage && (
          <div className="mt-3 flex justify-center">
            <button
              onClick={() => fetchNextPage()}
              disabled={isFetchingNextPage}
              className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs py-1.5 px-4 disabled:opacity-50"
            >
              {isFetchingNextPage ? 'Loading...' : 'Load more'}
            </button>
          </div>
        )}
      </Card>
    </div>
  );
}

function ProposalRow({ proposal: p, target, showStatus }: { proposal: Proposal; target?: Strategy; showStatus: boolean }) {
  const isLive = target?.mode === 'live';
  return (
    <Link
      to={`/agents/proposals/${p.id}`}
      className={`block p-3 rounded-lg border hover:bg-accent/40 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
        isLive && p.status === 'pending' ? 'border-destructive/40' : 'border-border'
      }`}
    >
      <div className="flex flex-wrap items-center gap-2 min-w-0">
        <span className="font-mono text-[11px] text-muted-foreground">#{p.id}</span>
        <ProposalKindBadge kind={p.kind} />
        {isLive && <LiveTargetBadge />}
        <span className="font-semibold text-sm text-foreground truncate">
          {p.target_strategy_name || target?.name || `Strategy #${p.target_strategy_id}`}
        </span>
        {target && (
          <>
            <ModeBadge mode={target.mode} />
            <StatusBadge status={target.status} />
          </>
        )}
        <GateBadge passed={p.gate_passed} />
        {p.early && <EarlyBadge />}
        {showStatus && <ProposalStatusBadge status={p.status} />}
        <span
          className="ml-auto text-[11px] text-muted-foreground whitespace-nowrap flex items-center gap-1"
          title={new Date(p.created_at).toLocaleString()}
        >
          {formatRelative(p.created_at)}
          <ArrowRight className="w-3 h-3" />
        </span>
      </div>
      {p.rationale && <p className="text-xs text-muted-foreground mt-1 line-clamp-2">{p.rationale}</p>}
      <div className="flex flex-wrap items-center gap-1.5 mt-1.5 text-[11px] text-muted-foreground">
        <span>
          by <span className="text-foreground">{p.agent_name || `agent #${p.agent_id}`}</span>
        </span>
        {p.challenger_strategy_id != null && (
          <>
            <span className="text-muted-foreground/50">·</span>
            <span className="font-mono">from challenger #{p.challenger_strategy_id}</span>
          </>
        )}
        {p.decided_by && (
          <>
            <span className="text-muted-foreground/50">·</span>
            <span>
              decided by <span className="text-foreground">{p.decided_by}</span>
            </span>
          </>
        )}
        {p.status === 'failed' && p.failure_reason && (
          <>
            <span className="text-muted-foreground/50">·</span>
            <span className="text-destructive">{p.failure_reason}</span>
          </>
        )}
      </div>
    </Link>
  );
}

function FilterSelect({
  id,
  label,
  value,
  onChange,
  children,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-2">
      <label htmlFor={id} className="text-xs text-muted-foreground font-medium">
        {label}:
      </label>
      <select id={id} value={value} onChange={(e) => onChange(e.target.value)} className="form-select text-xs py-1">
        {children}
      </select>
    </div>
  );
}
