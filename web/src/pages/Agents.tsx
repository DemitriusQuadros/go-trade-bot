import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Pencil, Play, Pause, History, Trash2, RefreshCw, X, Bot, PowerOff } from 'lucide-react';
import { Agent } from '@/api/types';
import { apiErrorMessage } from '@/api/client';
import { useAgents, useDeleteAgent, usePauseAgent, usePlatformSettings } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { DropdownMenu, DropdownMenuItem } from '@/components/ui/DropdownMenu';
import { LoadingScreen } from '@/components/ui/Spinner';
import {
  AgentRuntimeStatus,
  AgentStatusBadge,
  DefaultAgentBadge,
  PermissionBadgeList,
  RunStatusChip,
  BudgetCell,
} from '@/components/domain/AgentBadges';
import { RunAgentDialog } from '@/components/domain/RunAgentDialog';
import { useToast } from '@/context/ToastContext';
import { describeCron } from '@/lib/cron';
import { formatRelative } from '@/lib/time';

const TH = 'px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground whitespace-nowrap';

function runtimeStatus(agent: Agent, globallyPaused: boolean): AgentRuntimeStatus {
  if (globallyPaused) return 'halted';
  if (agent.paused) return 'paused';
  return 'active';
}

function scheduleLabel(agent: Agent): string {
  const specs = agent.triggers?.cron ?? [];
  if (specs.length === 0) return 'Manual only';
  return specs.map(describeCron).join(', ');
}

// Agents list (A-03 §3): every configured persona, its runtime state and
// today's spend against its budget.
export function Agents() {
  const navigate = useNavigate();
  const { data: agents = [], isLoading, isFetching, refetch, error } = useAgents();
  const { data: settings } = usePlatformSettings();
  const globallyPaused = settings?.agents_paused ?? false;
  const pauseAgent = usePauseAgent();
  const deleteAgent = useDeleteAgent();
  const { toast } = useToast();

  const [runTarget, setRunTarget] = useState<Agent | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Agent | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const togglePause = (agent: Agent) => {
    const next = !agent.paused;
    pauseAgent.mutate(
      { id: agent.id, paused: next },
      {
        onSuccess: () => toast(`${agent.name} ${next ? 'paused' : 'resumed'}`, next ? 'info' : 'success'),
        onError: (err) => setActionError(apiErrorMessage(err, 'Failed to update the agent')),
      },
    );
  };

  const confirmDelete = () => {
    const target = deleteTarget;
    setDeleteTarget(null);
    if (!target) return;
    deleteAgent.mutate(target.id, {
      onSuccess: () => toast(`${target.name} deleted`),
      onError: (err) => setActionError(apiErrorMessage(err, 'Failed to delete the agent')),
    });
  };

  if (isLoading) return <LoadingScreen message="Loading agents..." />;

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">Agents</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            AI personas that watch your strategies on a schedule - each with its own goal, permissions, strategies
            and daily budget. Agents never place orders and never modify live strategies.
          </p>
        </div>
        <button
          onClick={() => navigate('/agents/new')}
          className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
        >
          <Plus className="w-4 h-4" />
          <span>New Agent</span>
        </button>
      </div>

      {globallyPaused && (
        <div className="p-3 rounded-lg border border-warning/40 bg-warning/15 text-xs text-foreground flex items-center gap-2">
          <PowerOff className="w-4 h-4 text-warning shrink-0" />
          <span>
            The global kill switch is on - no agent runs start until it's turned back on from the sidebar footer.
          </span>
        </div>
      )}

      {actionError && (
        <div
          role="alert"
          className="p-3 rounded-lg border text-xs flex items-center justify-between bg-destructive/15 border-destructive/40 text-foreground"
        >
          <span>{actionError}</span>
          <button onClick={() => setActionError(null)} aria-label="Dismiss" className="p-0.5 text-muted-foreground hover:text-foreground">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      <Card>
        <div className="flex items-center justify-between gap-2 mb-4">
          <CardHeader
            className="mb-0"
            title="Configured Agents"
            subtitle={`${agents.length} agent${agents.length === 1 ? '' : 's'} - schedules run in UTC`}
          />
          <button
            onClick={() => refetch()}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
            title="Refresh"
            aria-label="Refresh agents"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>

        {error ? (
          <div className="p-8 text-center text-xs text-destructive bg-destructive/10 rounded-lg">
            Couldn't load agents: {apiErrorMessage(error)}
          </div>
        ) : agents.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg space-y-3">
            <Bot className="w-8 h-8 mx-auto opacity-40" />
            <p>No agents configured yet.</p>
            <button
              onClick={() => navigate('/agents/new')}
              className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs px-3 py-1.5 font-bold"
            >
              + New Agent
            </button>
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full border-collapse">
              <thead>
                <tr className="border-b border-border bg-secondary/40">
                  <th className={TH}>Agent</th>
                  <th className={TH}>Status</th>
                  <th className={TH}>Permissions</th>
                  <th className={TH}>Strategies</th>
                  <th className={TH}>Schedule</th>
                  <th className={TH}>Next run</th>
                  <th className={TH}>Last run</th>
                  <th className={`${TH} text-right`}>Today</th>
                  <th className="w-10 px-2 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {agents.map((agent) => {
                  const status = runtimeStatus(agent, globallyPaused);
                  const items: DropdownMenuItem[] = [
                    { label: 'Edit', icon: <Pencil />, onClick: () => navigate(`/agents/${agent.id}`) },
                    {
                      label: 'Run now',
                      icon: <Play />,
                      onClick: () => setRunTarget(agent),
                      disabled: globallyPaused || agent.paused,
                    },
                    {
                      label: agent.paused ? 'Resume' : 'Pause',
                      icon: agent.paused ? <Play /> : <Pause />,
                      onClick: () => togglePause(agent),
                    },
                    { label: 'View runs', icon: <History />, onClick: () => navigate(`/agents/${agent.id}/runs`) },
                  ];
                  if (!agent.is_default) {
                    items.push({
                      label: 'Delete',
                      icon: <Trash2 />,
                      onClick: () => setDeleteTarget(agent),
                      destructive: true,
                      separatorBefore: true,
                    });
                  }
                  return (
                    <tr
                      key={agent.id}
                      onClick={() => navigate(`/agents/${agent.id}`)}
                      className="border-b border-border last:border-b-0 hover:bg-accent/40 cursor-pointer transition-colors"
                    >
                      <td className="px-4 py-3 align-top max-w-xs">
                        <div className="flex items-center gap-1.5">
                          <span className="font-semibold text-foreground truncate">{agent.name}</span>
                          {agent.is_default && <DefaultAgentBadge />}
                        </div>
                        <div className="text-[11px] text-muted-foreground mt-0.5 font-mono">
                          #{agent.id}
                          {agent.provider ? ` · ${agent.provider}${agent.model ? `/${agent.model}` : ''}` : ' · default model'}
                        </div>
                      </td>
                      <td className="px-4 py-3 align-top">
                        <AgentStatusBadge status={status} />
                      </td>
                      <td className="px-4 py-3 align-top max-w-[14rem]">
                        <PermissionBadgeList permissions={agent.permissions} />
                      </td>
                      <td className="px-4 py-3 align-top text-xs font-mono text-foreground">
                        {agent.strategy_ids?.length ?? 0}
                      </td>
                      <td className="px-4 py-3 align-top text-xs text-foreground">
                        <span title={(agent.triggers?.cron ?? []).join('\n') || undefined}>{scheduleLabel(agent)}</span>
                      </td>
                      <td className="px-4 py-3 align-top text-xs text-muted-foreground whitespace-nowrap">
                        {agent.next_run_at && status === 'active' ? (
                          <span title={new Date(agent.next_run_at).toUTCString()}>{formatRelative(agent.next_run_at)}</span>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="px-4 py-3 align-top text-xs whitespace-nowrap">
                        {agent.last_run ? (
                          <div className="flex items-center gap-1.5">
                            <RunStatusChip status={agent.last_run.status} />
                            <span className="text-muted-foreground" title={new Date(agent.last_run.started_at).toLocaleString()}>
                              {formatRelative(agent.last_run.started_at)}
                            </span>
                          </div>
                        ) : (
                          <span className="text-muted-foreground">Never</span>
                        )}
                      </td>
                      <td className="px-4 py-3 align-top text-right">
                        <BudgetCell spent={agent.today_cost_usd} budget={agent.daily_budget_usd} />
                      </td>
                      <td className="px-2 py-3 align-top text-right" onClick={(e) => e.stopPropagation()}>
                        <DropdownMenu label={`Actions for ${agent.name}`} items={items} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <RunAgentDialog agent={runTarget} onClose={() => setRunTarget(null)} />

      <ConfirmDialog
        isOpen={deleteTarget != null}
        title="Delete agent"
        message={`Delete "${deleteTarget?.name ?? ''}"? Its schedule stops immediately. Past runs, reports and the notes it wrote to strategy memory are kept.`}
        confirmText="Delete agent"
        isDangerous
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}
