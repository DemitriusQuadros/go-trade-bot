import React from 'react';
import { Link } from 'react-router-dom';
import { Activity, AlertTriangle, Clock, Hand, Link2, Zap } from 'lucide-react';
import { Agent, AgentRun } from '@/api/types';
import { useMarketSymbols } from '@/hooks/queries';
import { apiErrorMessage } from '@/api/client';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { describeCron } from '@/lib/cron';
import { formatDateTime, formatPrice, formatRelative } from '@/lib/format';
import {
  chainOnLabel,
  describeMarketDetail,
  normalizeTriggers,
  runTriggerInfo,
  triggerDetailLines,
  triggerSummary,
} from '@/lib/triggers';
import { useT } from '@/i18n';

// Read-only trigger displays for the agents platform (C-02 §3-§5): the
// agents table's compact summary, the runs table's trigger badge + detail
// line, and the market-watch status card.

// --- Agents table (C-02 §3) ----------------------------------------------------

// "⏱ every 6h · 3 events · 2 market · ⛓ 1" with lucide icons; the native
// title lists every trigger (a HelpTooltip would steal the row's click).
export function AgentTriggerSummaryCell({ agent, agentName }: { agent: Agent; agentName: (id: number) => string }) {
  const t = useT();
  const sum = triggerSummary(agent);
  const cron = normalizeTriggers(agent.triggers).cron;
  const parts: React.ReactNode[] = [];
  if (sum.cron > 0) {
    parts.push(
      <span key="cron" className="inline-flex items-center gap-1">
        <Clock className="w-3 h-3 text-muted-foreground" aria-hidden />
        {sum.cron === 1 && cron[0] ? describeCron(cron[0]) : t('agents.schedulesCount', { count: sum.cron })}
      </span>,
    );
  }
  if (sum.events > 0) {
    parts.push(
      <span key="events" className="inline-flex items-center gap-1">
        <Zap className="w-3 h-3 text-muted-foreground" aria-hidden />
        {t('agents.eventsCount', { count: sum.events })}
      </span>,
    );
  }
  if (sum.market > 0) {
    parts.push(
      <span key="market" className="inline-flex items-center gap-1">
        <Activity className="w-3 h-3 text-muted-foreground" aria-hidden />
        {t('agents.marketCount', { count: sum.market })}
      </span>,
    );
  }
  if (sum.chain_from > 0) {
    parts.push(
      <span key="chain" className="inline-flex items-center gap-1">
        <Link2 className="w-3 h-3 text-muted-foreground" aria-hidden />
        {sum.chain_from}
        <span className="sr-only">{t('agents.chainedFrom')}</span>
      </span>,
    );
  }
  if (parts.length === 0) return <span className="text-muted-foreground">{t('agents.manualOnly')}</span>;
  const title = triggerDetailLines(agent, agentName).join('\n');
  return (
    <span className="inline-flex flex-wrap items-center gap-x-1.5 gap-y-0.5" title={title || undefined}>
      {parts.map((p, i) => (
        <React.Fragment key={i}>
          {i > 0 && <span className="text-muted-foreground" aria-hidden>·</span>}
          {p}
        </React.Fragment>
      ))}
    </span>
  );
}

// --- Runs table (C-02 §4) -----------------------------------------------------

const BADGE_BASE =
  'inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border whitespace-nowrap';

const TRIGGER_TONE: Record<string, string> = {
  cron: 'bg-secondary text-muted-foreground border-border',
  manual: 'bg-accent text-accent-foreground border-border',
  event: 'bg-warning/15 text-warning border-warning/40',
  market: 'bg-primary/15 text-primary border-primary/40',
  chain: 'bg-success/15 text-success border-success/40',
};

const TRIGGER_ICON: Record<string, React.ComponentType<{ className?: string }>> = {
  cron: Clock,
  manual: Hand,
  event: Zap,
  market: Activity,
  chain: Link2,
};

export function RunTriggerBadge({ trigger }: { trigger: string }) {
  const t = useT();
  const tone = TRIGGER_TONE[trigger] ?? 'bg-secondary text-muted-foreground border-border';
  const Icon = TRIGGER_ICON[trigger];
  return (
    <span className={`${BADGE_BASE} ${tone}`}>
      {Icon && <Icon className="w-3 h-3" />}
      {t.enum('trigger', trigger) || trigger.replace('_', ' ')}
    </span>
  );
}

// One-line detail under the badge. Links stopPropagation so clicking them
// doesn't also toggle the row's expansion.
export function RunTriggerDetail({ run, agentName }: { run: AgentRun; agentName: (id: number) => string }) {
  const t = useT();
  const info = runTriggerInfo(run);
  const stop = (e: React.MouseEvent) => e.stopPropagation();
  switch (info.trigger) {
    case 'cron':
      return info.detail.cron ? (
        <span title={info.detail.cron}>
          {describeCron(info.detail.cron)} <span className="text-muted-foreground">UTC</span>
        </span>
      ) : null;
    case 'manual':
      return info.detail.requested_by ? <span>{t('agents.requestedBy', { name: info.detail.requested_by })}</span> : null;
    case 'event': {
      const d = info.detail;
      if (!d.event) return null;
      return (
        <span title={d.occurred_at ? formatDateTime(d.occurred_at, { seconds: true }) : undefined}>
          {d.strategy_id != null ? (
            t.rich('agents.eventOn', {
              event: <span className="font-mono">{d.event}</span>,
              strategy: (
                <Link to={`/strategies/${d.strategy_id}/edit`} onClick={stop} className="font-mono hover:text-primary hover:underline">
                  #{d.strategy_id}
                </Link>
              ),
            })
          ) : (
            <span className="font-mono">{d.event}</span>
          )}
          {d.symbol && <span className="text-muted-foreground"> ({d.symbol})</span>}
        </span>
      );
    }
    case 'market':
      return info.detail.symbol ? (
        <span className="font-mono" title={info.detail.at ? formatDateTime(info.detail.at, { seconds: true }) : undefined}>
          {describeMarketDetail(info.detail)}
        </span>
      ) : null;
    case 'chain': {
      const d = info.detail;
      const depth = run.chain_depth ?? 0;
      return (
        <span>
          {d.source_agent_id != null ? (
            <>
              {t.rich('agents.chainFrom', {
                agent: (
                  <Link to={`/agents/${d.source_agent_id}/runs`} onClick={stop} className="hover:text-primary hover:underline">
                    {agentName(d.source_agent_id)}
                    {d.source_run_id != null ? t('agents.chainRun', { id: d.source_run_id }) : ''}
                  </Link>
                ),
              })}
              {d.on && <span className="text-muted-foreground"> ({chainOnLabel(d.on)})</span>}
            </>
          ) : (
            <span className="text-muted-foreground">{t('agents.viaTriggerAgent')}</span>
          )}
          {depth > 0 && <span className="ml-1.5 font-mono text-muted-foreground">{t('agents.depth', { depth })}</span>}
        </span>
      );
    }
    default:
      return null;
  }
}

// --- Market watch status (C-02 §5) ---------------------------------------------

const RUNTIME_STALE_MS = 5 * 60 * 1000;

export function MarketWatchCard() {
  const t = useT();
  const { data, isLoading, error } = useMarketSymbols();
  const seenAt = data?.runtime_seen_at ?? null;
  const seenMs = seenAt ? new Date(seenAt).getTime() : NaN;
  const stale = !!data && (!seenAt || Number.isNaN(seenMs) || Date.now() - seenMs > RUNTIME_STALE_MS);
  const symbols = data?.symbols ?? [];

  return (
    <CollapsibleSection
      id="agents.marketWatch"
      title={t('agents.marketWatch')}
      subtitle={
        data ? t('agents.marketWatchSubtitle', { count: symbols.length, seen: formatRelative(seenAt) }) : undefined
      }
    >
      <div className="space-y-3">
        {stale && (
          <div role="status" className="p-2.5 rounded-lg border border-warning/40 bg-warning/15 text-xs text-foreground flex items-center gap-2">
            <AlertTriangle className="w-4 h-4 text-warning shrink-0" />
            <span>{t('agents.runtimeDown')}</span>
          </div>
        )}
        {isLoading ? (
          <p className="text-xs text-muted-foreground">{t('agents.marketLoading')}</p>
        ) : error ? (
          <p className="text-xs text-destructive">{t('agents.marketLoadFailed', { error: apiErrorMessage(error) })}</p>
        ) : symbols.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t('agents.marketEmpty')}
          </p>
        ) : (
          <div className="overflow-x-auto rounded border border-border/60">
            <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-1.5 [&_th]:text-left [&_th]:text-[10px] [&_th]:uppercase [&_th]:font-semibold [&_th]:text-muted-foreground [&_td]:px-3 [&_td]:py-1.5 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40">
              <thead className="bg-secondary/40">
                <tr>
                  <th>{t('agents.colSymbol')}</th>
                  <th className="text-right">{t('agents.colWatchers')}</th>
                  <th className="text-right">{t('agents.colLastPrice')}</th>
                  <th>{t('agents.colLastCandle')}</th>
                </tr>
              </thead>
              <tbody className="font-mono">
                {symbols.map((s) => (
                  <tr key={s.symbol}>
                    <td className="text-foreground">{s.symbol}</td>
                    <td className="text-right text-foreground">{s.watchers}</td>
                    <td className="text-right text-foreground">
                      {s.last_price != null ? formatPrice(s.last_price) : '—'}
                    </td>
                    <td className="text-muted-foreground" title={s.last_candle_at ? formatDateTime(s.last_candle_at, { seconds: true }) : undefined}>
                      {formatRelative(s.last_candle_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </CollapsibleSection>
  );
}
