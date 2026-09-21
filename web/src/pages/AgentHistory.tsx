import React, { useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { History, ChevronDown, ChevronRight, X, RefreshCw } from 'lucide-react';
import { useAgentRuns } from '@/hooks/queries';
import { AgentToolCallCard } from '@/components/domain/AgentToolCallCard';
import { MarkdownMessage } from '@/components/domain/MarkdownMessage';
import { Card, CardHeader } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';

// Frontend Spec 02: a read-only, reverse-chronological audit view over
// AgentRun rows, following the same list/detail-expand pattern
// ExecutionLog.tsx already established for StrategyExecution rows.
export function AgentHistory() {
  const [searchParams, setSearchParams] = useSearchParams();
  const strategyIdParam = searchParams.get('strategy_id');
  const strategyId = strategyIdParam ? parseInt(strategyIdParam, 10) : undefined;

  const [limit, setLimit] = useState(20);
  const [expandedId, setExpandedId] = useState<number | null>(null);

  const { data: runs = [], isLoading, isFetching, refetch } = useAgentRuns(strategyId, limit);

  const clearFilter = () => {
    const next = new URLSearchParams(searchParams);
    next.delete('strategy_id');
    setSearchParams(next);
  };

  const sorted = useMemo(
    () => [...runs].sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime()),
    [runs]
  );

  if (isLoading) {
    return <LoadingScreen message="Loading agent run history..." />;
  }

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-green-500 flex items-center gap-2">
            <History className="w-6 h-6" />
            Agent Run History
          </h1>
          <p className="text-xs text-green-700 mt-0.5">
            Read-only audit log of every AI agent invocation - what was asked, which tools ran, and
            what was actually persisted (post safety-clamp).
          </p>
        </div>

        <div className="flex items-center gap-2">
          {strategyId != null && (
            <button
              onClick={clearFilter}
              className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs py-1.5 px-2.5 flex items-center gap-1.5"
              title="Clear strategy filter"
            >
              <span>Strategy #{strategyId}</span>
              <X className="w-3 h-3" />
            </button>
          )}
          <button
            onClick={() => refetch()}
            className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs p-1.5"
            title="Refresh"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      <Card>
        <CardHeader
          title="Runs"
          subtitle={`${sorted.length} run${sorted.length === 1 ? '' : 's'}${strategyId != null ? ` for strategy #${strategyId}` : ''}`}
        />

        {sorted.length === 0 ? (
          <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
            No agent runs recorded yet.
          </div>
        ) : (
          <div className="overflow-x-auto rounded border border-green-950/60">
            <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-green-500 [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_tbody_tr]:border-t [&_tbody_tr]:border-green-950/40 [&_thead]:bg-green-950/40">
              <thead>
                <tr>
                  <th></th>
                  <th>Started</th>
                  <th>Trigger</th>
                  <th>Provider / Model</th>
                  <th>Status</th>
                  <th>Strategy</th>
                  <th>Input</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((run) => {
                  const isExpanded = expandedId === run.id;
                  const isError = run.status === 'error';
                  return (
                    <React.Fragment key={run.id}>
                      <tr
                        onClick={() => setExpandedId(isExpanded ? null : run.id)}
                        className={`cursor-pointer ${isError ? 'bg-rose-950/10 hover:bg-rose-950/20' : 'hover:bg-green-950/20'}`}
                      >
                        <td className="text-green-700">
                          {isExpanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
                        </td>
                        <td className="font-mono text-green-700 whitespace-nowrap">
                          {new Date(run.started_at).toLocaleString()}
                        </td>
                        <td className="font-mono text-green-600">{run.trigger}</td>
                        <td className="text-green-600">
                          {run.provider || '—'}
                          {run.model ? <span className="text-green-800">/{run.model}</span> : null}
                        </td>
                        <td>
                          <span
                            className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase ${
                              isError
                                ? 'bg-rose-900/40 text-rose-300 border border-rose-700/40'
                                : 'bg-emerald-900/30 text-emerald-300 border border-emerald-700/30'
                            }`}
                          >
                            {run.status}
                          </span>
                        </td>
                        <td>
                          {run.strategy_id ? (
                            <Link
                              to={`/strategies/${run.strategy_id}/edit`}
                              onClick={(e) => e.stopPropagation()}
                              className="text-emerald-400 hover:text-emerald-300 underline underline-offset-2 font-mono"
                            >
                              #{run.strategy_id}
                            </Link>
                          ) : (
                            <span className="text-green-900">—</span>
                          )}
                        </td>
                        <td className="max-w-xs truncate text-green-500">{run.input_summary}</td>
                      </tr>
                      {isExpanded && (
                        <tr>
                          <td colSpan={7} className="bg-black/40 p-3">
                            {isError && run.error_message && (
                              <div className="mb-2 text-xs text-rose-300 bg-rose-950/20 border border-rose-900/50 rounded px-2 py-1.5">
                                {run.error_message}
                              </div>
                            )}
                            {run.tool_calls.length === 0 ? (
                              <div className="text-xs text-green-800 italic">No tool calls in this run.</div>
                            ) : (
                              <div className="space-y-2">
                                {run.tool_calls.map((call, i) => (
                                  <AgentToolCallCard key={i} call={call} />
                                ))}
                              </div>
                            )}
                            {run.response_text && (
                              <div className="mt-2 rounded border border-green-950/60 bg-black/40 text-green-100 px-3 py-2">
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
        )}

        {sorted.length >= limit && (
          <div className="mt-3 flex justify-center">
            <button
              onClick={() => setLimit((l) => l + 20)}
              className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs py-1.5 px-4"
            >
              Load more
            </button>
          </div>
        )}
      </Card>
    </div>
  );
}
