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
import { formatDateTime, formatRelative } from '@/lib/format';
import { HISTORY_STATUSES, ProposalTab } from '@/lib/proposals';
import type { MessageKey } from '@/i18n';
import { useT } from '@/i18n';

const TABS: { key: ProposalTab; label: MessageKey; hint: MessageKey }[] = [
  { key: 'pending', label: 'proposals.tabPending', hint: 'proposals.tabPendingHint' },
  {
    key: 'approved',
    label: 'proposals.tabApproved',
    hint: 'proposals.tabApprovedHint',
  },
  { key: 'history', label: 'proposals.tabHistory', hint: 'proposals.tabHistoryHint' },
];

const TAB_KEYS = TABS.map((tb) => tb.key);

// Proposals inbox (B-02 §2) - /agents/proposals. Tab and filters live in
// the URL (?tab=&strategy_id=&agent_id=) so the workbench's challenger
// banner can link to a filtered view. strategy_id matches a proposal's
// target OR its challenger.
export function Proposals() {
  const t = useT();
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
  const activeTab = TABS.find((tb) => tb.key === tab)!;

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <GitPullRequest className="w-6 h-6" />
            {t('proposals.title')}
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('proposals.subtitle')}
          </p>
        </div>

        <div role="tablist" aria-label={t('proposals.statusTabs')} className="flex items-center gap-2 bg-card/20 p-1 rounded-lg border border-border/30">
          {TABS.map((tb) => (
            <button
              key={tb.key}
              role="tab"
              aria-selected={tab === tb.key}
              onClick={() => setParam('tab', tb.key === 'pending' ? null : tb.key)}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
                tab === tb.key ? 'bg-secondary text-foreground shadow' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {t(tb.label)}
            </button>
          ))}
        </div>
      </div>

      <Card>
        <div className="flex flex-wrap items-center gap-3">
          <FilterSelect
            id="proposal-filter-strategy"
            label={t('proposals.filterTarget')}
            value={strategyId != null ? String(strategyId) : 'all'}
            onChange={(v) => setParam('strategy_id', v)}
          >
            <option value="all">{t('proposals.allStrategies')}</option>
            {strategies.map((s) => (
              <option key={s.id} value={s.id}>
                #{s.id} — {s.name}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect
            id="proposal-filter-agent"
            label={t('proposals.filterAgent')}
            value={agentId != null ? String(agentId) : 'all'}
            onChange={(v) => setParam('agent_id', v)}
          >
            <option value="all">{t('proposals.allAgents')}</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </FilterSelect>
          <button
            onClick={() => refetch()}
            className="ml-auto bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
            title={t('common.refresh')}
            aria-label={t('proposals.refresh')}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </Card>

      <Card>
        <CardHeader title={t(activeTab.label)} subtitle={t(activeTab.hint)} />

        {isLoading ? (
          <LoadingScreen message={t('proposals.loading')} />
        ) : error ? (
          <div className="p-8 text-center text-xs text-destructive bg-destructive/10 rounded-lg">
            {t('proposals.loadFailed', { error: apiErrorMessage(error) })}
          </div>
        ) : proposals.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg space-y-2">
            <p>
              {tab === 'pending'
                ? t(filtered ? 'proposals.emptyPendingFiltered' : 'proposals.emptyPending')
                : tab === 'approved'
                  ? t('proposals.emptyApproved')
                  : t(filtered ? 'proposals.emptyHistoryFiltered' : 'proposals.emptyHistory')}
            </p>
            {tab === 'pending' && !filtered && (
              <p>
                {t.rich('proposals.emptyHelp', {
                  proposeLive: <span className="font-mono">propose_live</span>,
                  editTesting: <span className="font-mono">edit_testing</span>,
                  link: (
                    <Link to="/agents" className="text-primary hover:underline">
                      {t('proposals.configureAgent')}
                    </Link>
                  ),
                })}
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
              {isFetchingNextPage ? t('common.loading') : t('agents.loadMore')}
            </button>
          </div>
        )}
      </Card>
    </div>
  );
}

function ProposalRow({ proposal: p, target, showStatus }: { proposal: Proposal; target?: Strategy; showStatus: boolean }) {
  const t = useT();
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
          {p.target_strategy_name || target?.name || t('proposals.strategyNumber', { id: p.target_strategy_id })}
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
          title={formatDateTime(p.created_at, { seconds: true })}
        >
          {formatRelative(p.created_at)}
          <ArrowRight className="w-3 h-3" />
        </span>
      </div>
      {p.rationale && <p className="text-xs text-muted-foreground mt-1 line-clamp-2">{p.rationale}</p>}
      <div className="flex flex-wrap items-center gap-1.5 mt-1.5 text-[11px] text-muted-foreground">
        <span>
          {t.rich('proposals.by', { name: <span className="text-foreground">{p.agent_name || t('proposals.agentNumber', { id: p.agent_id })}</span> })}
        </span>
        {p.challenger_strategy_id != null && (
          <>
            <span className="text-muted-foreground/50">·</span>
            <span className="font-mono">{t('proposals.fromChallenger', { id: p.challenger_strategy_id })}</span>
          </>
        )}
        {p.decided_by && (
          <>
            <span className="text-muted-foreground/50">·</span>
            <span>
              {t.rich('proposals.decidedBy', { name: <span className="text-foreground">{p.decided_by}</span> })}
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
