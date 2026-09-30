import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, ChevronDown, ChevronRight } from 'lucide-react';
import { AgentRun } from '@/api/types';
import { AgentToolCallCard } from '@/components/domain/AgentToolCallCard';
import { RunTriggerBadge, RunTriggerDetail } from '@/components/domain/AgentTriggerDisplay';
import { useAgents } from '@/hooks/queries';
import { MarkdownMessage } from '@/components/domain/MarkdownMessage';
import { formatDateTime, formatDuration, formatRelative, formatTokens, formatUsd } from '@/lib/format';
import { useT } from '@/i18n';

// AgentRunsTable is the one expandable run-history table, shared by the
// Activity page's "Agent Log" tab (every run, optionally one strategy's) and
// the per-agent runs page (/agents/:id/runs). Extracted from Activity.tsx
// rather than duplicated. An expanded row shows the error (if any), every
// tool call (AgentToolCallCard) and the model's answer (MarkdownMessage).
//
// variant="log"   - Activity's original columns (provider/model, strategy, input).
// variant="agent" - per-agent view: trigger, status, started, duration,
//                   tokens and cost (A-03 §5).
interface AgentRunsTableProps {
  runs: AgentRun[];
  variant?: 'log' | 'agent';
}

export function AgentRunsTable({ runs, variant = 'log' }: AgentRunsTableProps) {
  const t = useT();
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const colCount = 8;
  // Resolves chain-source agent names (C-02 §4); shares the Agents page's cache.
  const { data: agents = [] } = useAgents();
  const agentName = (id: number) => agents.find((a) => a.id === id)?.name ?? t('agents.agentNumber', { id });

  return (
    <div className="overflow-x-auto rounded border border-border/60">
      <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40 [&_thead]:bg-card/40">
        <thead>
          {variant === 'agent' ? (
            <tr>
              <th></th>
              <th>{t('agents.colStarted')}</th>
              <th>{t('agents.colTrigger')}</th>
              <th>{t('agents.colStatus')}</th>
              <th>{t('agents.colDuration')}</th>
              <th className="text-right">{t('agents.colTokens')}</th>
              <th className="text-right">{t('agents.colCost')}</th>
              <th>{t('agents.colInput')}</th>
            </tr>
          ) : (
            <tr>
              <th></th>
              <th>{t('agents.colStarted')}</th>
              <th>{t('agents.colTrigger')}</th>
              <th>{t('agents.colAgent')}</th>
              <th>{t('agents.colProviderModel')}</th>
              <th>{t('agents.colStatus')}</th>
              <th>{t('agents.colStrategy')}</th>
              <th>{t('agents.colInput')}</th>
            </tr>
          )}
        </thead>
        <tbody>
          {runs.map((run) => {
            const isExpanded = expandedId === run.id;
            const isError = run.status === 'error';
            return (
              <React.Fragment key={run.id}>
                <tr
                  onClick={() => setExpandedId(isExpanded ? null : run.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      setExpandedId(isExpanded ? null : run.id);
                    }
                  }}
                  tabIndex={0}
                  aria-expanded={isExpanded}
                  className={`cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                    isError ? 'bg-destructive/15 hover:bg-destructive/15' : 'hover:bg-card/20'
                  }`}
                >
                  <td className="text-muted-foreground">
                    {isExpanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
                  </td>
                  <td className="font-mono text-muted-foreground whitespace-nowrap" title={formatDateTime(run.started_at, { seconds: true })}>
                    {variant === 'agent' ? formatRelative(run.started_at) : formatDateTime(run.started_at, { seconds: true })}
                  </td>
                  <td className="max-w-[16rem]">
                    <div className="flex flex-col items-start gap-0.5">
                      <RunTriggerBadge trigger={run.trigger} />
                      <div className="text-[11px] text-muted-foreground truncate max-w-full">
                        <RunTriggerDetail run={run} agentName={agentName} />
                      </div>
                    </div>
                  </td>
                  {variant === 'log' && (
                    <td className="text-foreground whitespace-nowrap">
                      {run.agent_id != null ? (
                        <Link
                          to={`/agents/${run.agent_id}/runs`}
                          onClick={(e) => e.stopPropagation()}
                          className="hover:text-primary underline-offset-2 hover:underline"
                        >
                          {run.agent_name || `#${run.agent_id}`}
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">{run.agent_name || '—'}</span>
                      )}
                    </td>
                  )}
                  {variant === 'log' && (
                    <td className="text-muted-foreground">
                      {run.provider || '—'}
                      {run.model ? <span className="text-muted-foreground">/{run.model}</span> : null}
                    </td>
                  )}
                  <td>
                    <span
                      className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase ${
                        isError
                          ? 'bg-destructive/15 text-destructive border border-destructive/40'
                          : run.status === 'running'
                            ? 'bg-primary/15 text-primary border border-primary/40 animate-pulse'
                            : 'bg-success/15 text-success border border-success/40'
                      }`}
                    >
                      {t.enum('runStatus', run.status)}
                    </span>
                    {run.hit_iteration_cap && (
                      <span
                        className="ml-1 inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold whitespace-nowrap bg-warning/15 text-warning border border-warning/40"
                        title={t('agents.iterationCapTitle')}
                      >
                        <AlertTriangle className="w-3 h-3" aria-hidden="true" />
                        {t('agents.iterationCap')}
                      </span>
                    )}
                  </td>
                  {variant === 'agent' && (
                    <>
                      <td className="font-mono text-muted-foreground whitespace-nowrap">
                        {formatDuration(run.started_at, run.finished_at)}
                      </td>
                      <td className="font-mono text-muted-foreground text-right whitespace-nowrap">
                        {formatTokens(run.input_tokens)} / {formatTokens(run.output_tokens)}
                      </td>
                      <td className="font-mono text-foreground text-right whitespace-nowrap">{formatUsd(run.cost_usd)}</td>
                    </>
                  )}
                  {variant === 'log' && (
                    <td>
                      {run.strategy_id ? (
                        <Link
                          to={`/strategies/${run.strategy_id}/edit`}
                          onClick={(e) => e.stopPropagation()}
                          className="text-success hover:text-success underline underline-offset-2 font-mono"
                        >
                          #{run.strategy_id}
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                  )}
                  <td className="max-w-xs truncate text-foreground">{run.input_summary}</td>
                </tr>
                {isExpanded && (
                  <tr>
                    <td colSpan={colCount} className="bg-background/40 p-3">
                      {isError && run.error_message && (
                        <div className="mb-2 text-xs text-destructive bg-destructive/15 border border-destructive/40 rounded px-2 py-1.5">
                          {run.error_message}
                        </div>
                      )}
                      {run.tool_calls.length === 0 ? (
                        <div className="text-xs text-muted-foreground italic">{t('agents.noToolCalls')}</div>
                      ) : (
                        <div className="space-y-2">
                          {run.tool_calls.map((call, i) => (
                            <AgentToolCallCard key={i} call={call} />
                          ))}
                        </div>
                      )}
                      {run.response_text && (
                        <div className="mt-2 rounded border border-border/60 bg-background/40 text-foreground px-3 py-2">
                          <MarkdownMessage content={run.response_text} />
                        </div>
                      )}
                    </td>
                  </tr>
                )}
              </React.Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
