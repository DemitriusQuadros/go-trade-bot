import React, { useState } from 'react';
import { FileText } from 'lucide-react';
import { api } from '@/api/client';
import { useAgentReport } from '@/hooks/queries';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';
import { SeverityBadge } from '@/components/domain/AgentBadges';
import { CardAction, CardFallback, CardSkeleton, ChatCard, ChatDensity } from './ChatCard';
import { useT } from '@/i18n';

// A write_report result (Phase D-02 §6). "View inline" embeds the same
// server-rendered HTML AgentReportDetail shows, with the same URL builder
// (api.getAgentReportHtmlUrl: session cookie + theme) and the same empty sandbox.
export function ReportCard({ id, density }: { id: number; density: ChatDensity }) {
  const t = useT();
  const { data: report, isLoading, error } = useAgentReport(id);
  const isDark = useIsDarkMode();
  const [inline, setInline] = useState(false);

  if (isLoading) return <CardSkeleton label={t('cards.loadingReport', { id })} />;
  if (error || !report) return <CardFallback text={t('cards.reportFailed', { id })} to={`/agents/reports/${id}`} />;

  const htmlUrl = api.getAgentReportHtmlUrl(id, isDark ? 'dark' : 'light');

  return (
    <ChatCard
      testId="chat-report-card"
      density={density}
      icon={<FileText className="w-3.5 h-3.5" />}
      title={report.title || t('cards.reportTitle', { id })}
      meta={<SeverityBadge severity={report.severity} />}
      actions={
        <>
          <button
            type="button"
            onClick={() => setInline((v) => !v)}
            aria-expanded={inline}
            className="text-[11px] font-medium text-primary hover:underline"
          >
            {inline ? t('cards.hideInline') : t('cards.viewInline')}
          </button>
          <CardAction to={`/agents/reports/${id}`}>{t('cards.openReport')}</CardAction>
        </>
      }
    >
      {report.summary && <p className="text-xs text-foreground whitespace-pre-wrap break-words">{report.summary}</p>}
      {inline && (
        <iframe
          key={htmlUrl}
          src={htmlUrl}
          sandbox=""
          title={t('cards.reportFrame', { title: report.title })}
          className="w-full h-[28rem] rounded border border-border bg-background block"
        />
      )}
    </ChatCard>
  );
}
