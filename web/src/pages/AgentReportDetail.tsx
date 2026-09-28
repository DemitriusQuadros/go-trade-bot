import React, { useMemo } from 'react';
import { Link, useParams } from 'react-router-dom';
import { AlertCircle, ArrowLeft, ExternalLink } from 'lucide-react';
import { api, apiErrorMessage } from '@/api/client';
import { useAgentReport, useStrategies } from '@/hooks/queries';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';
import { Card } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';
import { SeverityBadge } from '@/components/domain/AgentBadges';
import { formatRelative } from '@/lib/time';

// One agent report (A-03 §6) - /agents/reports/:id. Webhook notifications
// deep-link here, so this must work on a cold full-page load (the SPA
// fallback in cmd/api serves index.html; everything below is fetched).
//
// The body is the server-rendered Console Pro HTML in an iframe with
// sandbox="" - no scripts, no forms, no same-origin access - on top of the
// endpoint's own CSP. `theme` makes the report match the app's current mode.
export function AgentReportDetail() {
  const { id } = useParams<{ id: string }>();
  const reportId = Number(id);
  const isDark = useIsDarkMode();
  const { data: report, isLoading, error } = useAgentReport(reportId);
  const { data: strategies = [] } = useStrategies();

  const strategyName = useMemo(() => {
    const m = new Map<number, string>();
    strategies.forEach((s) => m.set(s.id, s.name));
    return m;
  }, [strategies]);

  const htmlUrl = api.getAgentReportHtmlUrl(reportId, isDark ? 'dark' : 'light');

  if (isLoading) return <LoadingScreen message="Loading report..." />;

  if (error || !report) {
    return (
      <div className="container-custom space-y-4">
        <Link to="/agents/reports" className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1">
          <ArrowLeft className="w-3.5 h-3.5" /> Reports
        </Link>
        <div className="flex items-center gap-2 p-6 rounded-lg border border-destructive/40 bg-destructive/10 text-destructive text-sm">
          <AlertCircle className="w-5 h-5 shrink-0" />
          <span>Couldn't load report #{reportId}: {error ? apiErrorMessage(error) : 'not found'}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="container-custom space-y-4 flex flex-col">
      <div>
        <Link to="/agents/reports" className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1 mb-1">
          <ArrowLeft className="w-3.5 h-3.5" /> Reports
        </Link>
        <div className="flex flex-col md:flex-row md:items-start justify-between gap-3">
          <div className="min-w-0">
            <h1 className="text-2xl font-bold text-foreground flex items-center gap-2 flex-wrap">
              <SeverityBadge severity={report.severity} className="text-[11px]" />
              <span className="break-words">{report.title}</span>
            </h1>
            <div className="flex flex-wrap items-center gap-1.5 mt-1.5 text-xs text-muted-foreground">
              <span>
                by{' '}
                <Link to={`/agents/${report.agent_id}`} className="text-foreground hover:text-primary hover:underline">
                  {report.agent_name}
                </Link>
              </span>
              <span className="text-muted-foreground/50">·</span>
              <span title={new Date(report.created_at).toLocaleString()}>{formatRelative(report.created_at)}</span>
              {report.agent_run_id != null && (
                <>
                  <span className="text-muted-foreground/50">·</span>
                  {/* No per-run route exists - link to the agent's run list. */}
                  <Link to={`/agents/${report.agent_id}/runs`} className="text-foreground hover:text-primary hover:underline font-mono">
                    run #{report.agent_run_id}
                  </Link>
                </>
              )}
              {report.strategy_ids?.map((sid) => (
                <Link
                  key={sid}
                  to={`/strategies/${sid}/edit`}
                  className="px-1.5 py-0.5 rounded bg-secondary border border-border font-mono text-[11px] text-foreground hover:border-primary"
                >
                  #{sid} {strategyName.get(sid) ?? ''}
                </Link>
              ))}
            </div>
          </div>
          <a
            href={htmlUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="shrink-0 bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs flex items-center gap-1.5 px-3 py-1.5"
          >
            Open in new tab <ExternalLink className="w-3 h-3" />
          </a>
        </div>
      </div>

      <Card className="p-0 overflow-hidden">
        <iframe
          key={htmlUrl}
          src={htmlUrl}
          sandbox=""
          title={`Agent report: ${report.title}`}
          className="w-full h-[calc(100vh-13rem)] min-h-[600px] border-none bg-background block"
        />
      </Card>
    </div>
  );
}
