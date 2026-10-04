import React, { useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Pencil, Play, RefreshCw } from 'lucide-react';
import { apiErrorMessage } from '@/api/client';
import { useAgent, useAgentRunsForAgent, useAgentsPausedState } from '@/hooks/queries';
import { useAuth } from '@/context/AuthContext';
import { Card, CardHeader } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';
import { AgentRunsTable } from '@/components/domain/AgentRunsTable';
import { BudgetCell } from '@/components/domain/AgentBadges';
import { RunAgentDialog } from '@/components/domain/RunAgentDialog';
import { formatUsd } from '@/lib/format';
import { useT } from '@/i18n';

// One agent's run history (A-03 §5) - /agents/:id/runs. Reuses the Activity
// page's expandable AgentRunsTable (variant "agent": duration/tokens/cost
// columns) rather than a second copy of it.
export function AgentRuns() {
  const t = useT();
  const { id } = useParams<{ id: string }>();
  const agentId = Number(id);
  const { data: agent } = useAgent(agentId);
  // Run-now is admin only (auth-02 §4).
  const isAdmin = useAuth().can('admin');
  const globallyPaused = useAgentsPausedState(isAdmin).paused ?? false;
  const { data, isLoading, isFetching, error, refetch, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useAgentRunsForAgent(agentId);
  const [runOpen, setRunOpen] = useState(false);

  const runs = useMemo(() => data?.pages.flat() ?? [], [data]);
  const shownCost = runs.reduce((sum, r) => sum + (r.cost_usd ?? 0), 0);
  const canRun = !!agent && !agent.paused && !globallyPaused;

  if (isLoading) return <LoadingScreen message={t('agentRuns.loading')} />;

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <Link to="/agents" className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1 mb-1">
            <ArrowLeft className="w-3.5 h-3.5" /> {t('agentEditor.back')}
          </Link>
          <h1 className="text-2xl font-bold text-foreground">{t('agentRuns.title', { name: agent ? agent.name : t('agents.agentNumber', { id: agentId }) })}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('agentRuns.subtitle')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {agent && (
            <span className="text-xs text-muted-foreground mr-2">
              {t.rich('agentRuns.today', { budget: <BudgetCell spent={agent.today_cost_usd} budget={agent.daily_budget_usd} /> })}
            </span>
          )}
          <Link
            to={`/agents/${agentId}`}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs flex items-center gap-1.5 px-3 py-1.5"
          >
            <Pencil className="w-3.5 h-3.5" /> {isAdmin ? t('common.edit') : t('common.view')}
          </Link>
          {isAdmin && (
          <button
            onClick={() => setRunOpen(true)}
            disabled={!canRun}
            title={canRun ? t('agentRuns.runTitle') : t('agentRuns.runDisabledTitle')}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold disabled:opacity-50"
          >
            <Play className="w-3.5 h-3.5" /> {t('agents.runNow')}
          </button>
          )}
        </div>
      </div>

      <Card>
        <div className="flex items-center justify-between gap-2 mb-4">
          <CardHeader
            className="mb-0"
            title={t('agentRuns.runsTitle')}
            subtitle={t('agentRuns.runsSubtitle', { count: runs.length, total: formatUsd(shownCost) })}
          />
          <button
            onClick={() => refetch()}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
            title={t('common.refresh')}
            aria-label={t('agentRuns.refreshRuns')}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>

        {error ? (
          <div className="p-8 text-center text-xs text-destructive bg-destructive/10 rounded-lg">
            Couldn't load runs: {apiErrorMessage(error)}
          </div>
        ) : runs.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg">
            {t('agentRuns.empty')}
          </div>
        ) : (
          <AgentRunsTable runs={runs} variant="agent" />
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

      <RunAgentDialog agent={runOpen && agent ? agent : null} onClose={() => setRunOpen(false)} />
    </div>
  );
}
