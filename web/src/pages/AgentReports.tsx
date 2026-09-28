import React, { useMemo } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { FileText, RefreshCw } from 'lucide-react';
import { ReportSeverity } from '@/api/types';
import { apiErrorMessage } from '@/api/client';
import { useAgentReports, useAgents, useStrategies } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';
import { SeverityBadge } from '@/components/domain/AgentBadges';
import { formatRelative } from '@/lib/time';

const SEVERITIES: ReportSeverity[] = ['critical', 'warning', 'info'];

// Agent reports inbox (A-03 §6) - /agents/reports. Filters live in the URL
// (?agent_id=&strategy_id=&severity=) so a filtered view can be linked to.
export function AgentReports() {
  const [params, setParams] = useSearchParams();
  const agentId = params.get('agent_id') ? Number(params.get('agent_id')) : undefined;
  const strategyId = params.get('strategy_id') ? Number(params.get('strategy_id')) : undefined;
  const severityParam = params.get('severity');
  const severity = SEVERITIES.includes(severityParam as ReportSeverity) ? (severityParam as ReportSeverity) : undefined;

  const { data: agents = [] } = useAgents();
  const { data: strategies = [] } = useStrategies();
  const { data, isLoading, isFetching, error, refetch, fetchNextPage, hasNextPage, isFetchingNextPage } = useAgentReports({
    agent_id: agentId,
    strategy_id: strategyId,
    severity,
  });

  const reports = useMemo(() => data?.pages.flat() ?? [], [data]);
  const strategyName = useMemo(() => {
    const m = new Map<number, string>();
    strategies.forEach((s) => m.set(s.id, s.name));
    return m;
  }, [strategies]);

  const setFilter = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value === 'all') next.delete(key);
    else next.set(key, value);
    setParams(next, { replace: true });
  };

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <FileText className="w-6 h-6" />
            Agent Reports
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Findings your agents wrote up. Figures in a report are pulled from the database when it was written - never
            typed by the model.
          </p>
        </div>
      </div>

      <Card>
        <div className="flex flex-wrap items-center gap-3">
          <FilterSelect label="Agent" value={agentId != null ? String(agentId) : 'all'} onChange={(v) => setFilter('agent_id', v)}>
            <option value="all">All agents</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect
            label="Strategy"
            value={strategyId != null ? String(strategyId) : 'all'}
            onChange={(v) => setFilter('strategy_id', v)}
          >
            <option value="all">All strategies</option>
            {strategies.map((s) => (
              <option key={s.id} value={s.id}>
                #{s.id} — {s.name}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect label="Severity" value={severity ?? 'all'} onChange={(v) => setFilter('severity', v)}>
            <option value="all">All severities</option>
            {SEVERITIES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </FilterSelect>
          <button
            onClick={() => refetch()}
            className="ml-auto bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
            title="Refresh"
            aria-label="Refresh reports"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </Card>

      <Card>
        <CardHeader title="Reports" subtitle={`${reports.length} report${reports.length === 1 ? '' : 's'} shown`} />

        {isLoading ? (
          <LoadingScreen message="Loading reports..." />
        ) : error ? (
          <div className="p-8 text-center text-xs text-destructive bg-destructive/10 rounded-lg">
            Couldn't load reports: {apiErrorMessage(error)}
          </div>
        ) : reports.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg space-y-2">
            <p>No reports{agentId || strategyId || severity ? ' match these filters' : ' yet'}.</p>
            {!(agentId || strategyId || severity) && (
              <p>
                Agents write a report when a scheduled run finds something noteworthy.{' '}
                <Link to="/agents" className="text-primary hover:underline">
                  Configure an agent
                </Link>
                .
              </p>
            )}
          </div>
        ) : (
          <ul className="space-y-2">
            {reports.map((r) => (
              <li key={r.id}>
                <Link
                  to={`/agents/reports/${r.id}`}
                  className="block p-3 rounded-lg border border-border hover:bg-accent/40 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <SeverityBadge severity={r.severity} />
                    <span className="font-semibold text-sm text-foreground truncate">{r.title}</span>
                    <span
                      className="ml-auto text-[11px] text-muted-foreground whitespace-nowrap"
                      title={new Date(r.created_at).toLocaleString()}
                    >
                      {formatRelative(r.created_at)}
                    </span>
                  </div>
                  {r.summary && <p className="text-xs text-muted-foreground mt-1 line-clamp-2">{r.summary}</p>}
                  <div className="flex flex-wrap items-center gap-1.5 mt-1.5 text-[11px] text-muted-foreground">
                    <span className="text-foreground">{r.agent_name}</span>
                    {r.strategy_ids?.length > 0 && <span className="text-muted-foreground/50">·</span>}
                    {r.strategy_ids?.map((sid) => (
                      <span key={sid} className="px-1.5 py-0.5 rounded bg-secondary border border-border font-mono">
                        #{sid} {strategyName.get(sid) ?? ''}
                      </span>
                    ))}
                  </div>
                </Link>
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

function FilterSelect({
  label,
  value,
  onChange,
  children,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  children: React.ReactNode;
}) {
  const id = `report-filter-${label.toLowerCase()}`;
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
