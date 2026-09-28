import React from 'react';
import { Link } from 'react-router-dom';
import { Activity, AlertTriangle, Clock, Hand, Link2, Zap } from 'lucide-react';
import { Agent, AgentRun } from '@/api/types';
import { useMarketSymbols } from '@/hooks/queries';
import { apiErrorMessage } from '@/api/client';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { describeCron } from '@/lib/cron';
import { formatRelative } from '@/lib/time';
import {
  chainOnLabel,
  describeMarketDetail,
  normalizeTriggers,
  runTriggerInfo,
  triggerDetailLines,
  triggerSummary,
} from '@/lib/triggers';

// Read-only trigger displays for the agents platform (C-02 §3-§5): the
// agents table's compact summary, the runs table's trigger badge + detail
// line, and the market-watch status card.

// --- Agents table (C-02 §3) ----------------------------------------------------

// "⏱ every 6h · 3 events · 2 market · ⛓ 1" with lucide icons; the native
// title lists every trigger (a HelpTooltip would steal the row's click).
export function AgentTriggerSummaryCell({ agent, agentName }: { agent: Agent; agentName: (id: number) => string }) {
  const sum = triggerSummary(agent);
  const cron = normalizeTriggers(agent.triggers).cron;
  const parts: React.ReactNode[] = [];
  if (sum.cron > 0) {
    parts.push(
      <span key="cron" className="inline-flex items-center gap-1">
        <Clock className="w-3 h-3 text-muted-foreground" aria-hidden />
        {sum.cron === 1 && cron[0] ? describeCron(cron[0]) : `${sum.cron} schedules`}
      </span>,
    );
  }
  if (sum.events > 0) {
    parts.push(
      <span key="events" className="inline-flex items-center gap-1">
        <Zap className="w-3 h-3 text-muted-foreground" aria-hidden />
        {sum.events} event{sum.events === 1 ? '' : 's'}
      </span>,
    );
  }
  if (sum.market > 0) {
    parts.push(
      <span key="market" className="inline-flex items-center gap-1">
        <Activity className="w-3 h-3 text-muted-foreground" aria-hidden />
        {sum.market} market
      </span>,
    );
  }
  if (sum.chain_from > 0) {
    parts.push(
      <span key="chain" className="inline-flex items-center gap-1">
        <Link2 className="w-3 h-3 text-muted-foreground" aria-hidden />
        {sum.chain_from}
        <span className="sr-only"> chained from</span>
      </span>,
    );
  }
  if (parts.length === 0) return <span className="text-muted-foreground">Manual only</span>;
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
  const tone = TRIGGER_TONE[trigger] ?? 'bg-secondary text-muted-foreground border-border';
  const Icon = TRIGGER_ICON[trigger];
  return (
    <span className={`${BADGE_BASE} ${tone}`}>
      {Icon && <Icon className="w-3 h-3" />}
      {trigger.replace('_', ' ')}
    </span>
  );
}

// One-line detail under the badge. Links stopPropagation so clicking them
// doesn't also toggle the row's expansion.
export function RunTriggerDetail({ run, agentName }: { run: AgentRun; agentName: (id: number) => string }) {
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
      return info.detail.requested_by ? <span>by {info.detail.requested_by}</span> : null;
    case 'event': {
      const d = info.detail;
      if (!d.event) return null;
      return (
        <span title={d.occurred_at ? new Date(d.occurred_at).toLocaleString() : undefined}>
          <span className="font-mono">{d.event}</span>
          {d.strategy_id != null && (
            <>
              {' on '}
              <Link to={`/strategies/${d.strategy_id}/edit`} onClick={stop} className="font-mono hover:text-primary hover:underline">
                #{d.strategy_id}
              </Link>
            </>
          )}
          {d.symbol && <span className="text-muted-foreground"> ({d.symbol})</span>}
        </span>
      );
    }
    case 'market':
      return info.detail.symbol ? (
        <span className="font-mono" title={info.detail.at ? new Date(info.detail.at).toLocaleString() : undefined}>
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
              from{' '}
              <Link to={`/agents/${d.source_agent_id}/runs`} onClick={stop} className="hover:text-primary hover:underline">
                {agentName(d.source_agent_id)}
                {d.source_run_id != null ? ` run #${d.source_run_id}` : ''}
              </Link>
              {d.on && <span className="text-muted-foreground"> ({chainOnLabel(d.on)})</span>}
            </>
          ) : (
            <span className="text-muted-foreground">via trigger_agent</span>
          )}
          {depth > 0 && <span className="ml-1.5 font-mono text-muted-foreground">depth {depth}</span>}
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
  const { data, isLoading, error } = useMarketSymbols();
  const seenAt = data?.runtime_seen_at ?? null;
  const seenMs = seenAt ? new Date(seenAt).getTime() : NaN;
  const stale = !!data && (!seenAt || Number.isNaN(seenMs) || Date.now() - seenMs > RUNTIME_STALE_MS);
  const symbols = data?.symbols ?? [];

  return (
    <CollapsibleSection
      id="agents.marketWatch"
      title="Market watch"
      subtitle={
        data ? `${symbols.length} symbol${symbols.length === 1 ? '' : 's'} watched · runtime seen ${formatRelative(seenAt)}` : undefined
      }
    >
      <div className="space-y-3">
        {stale && (
          <div role="status" className="p-2.5 rounded-lg border border-warning/40 bg-warning/15 text-xs text-foreground flex items-center gap-2">
            <AlertTriangle className="w-4 h-4 text-warning shrink-0" />
            <span>Agent runtime not running: market and event triggers won't fire.</span>
          </div>
        )}
        {isLoading ? (
          <p className="text-xs text-muted-foreground">Loading market watch status...</p>
        ) : error ? (
          <p className="text-xs text-destructive">Couldn't load market watch status: {apiErrorMessage(error)}</p>
        ) : symbols.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            No symbols are being watched. Add a market watch to an agent's triggers to start one.
          </p>
        ) : (
          <div className="overflow-x-auto rounded border border-border/60">
            <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-1.5 [&_th]:text-left [&_th]:text-[10px] [&_th]:uppercase [&_th]:font-semibold [&_th]:text-muted-foreground [&_td]:px-3 [&_td]:py-1.5 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40">
              <thead className="bg-secondary/40">
                <tr>
                  <th>Symbol</th>
                  <th className="text-right">Watchers</th>
                  <th className="text-right">Last price</th>
                  <th>Last candle</th>
                </tr>
              </thead>
              <tbody className="font-mono">
                {symbols.map((s) => (
                  <tr key={s.symbol}>
                    <td className="text-foreground">{s.symbol}</td>
                    <td className="text-right text-foreground">{s.watchers}</td>
                    <td className="text-right text-foreground">
                      {s.last_price != null ? s.last_price.toLocaleString(undefined, { maximumFractionDigits: 8 }) : '—'}
                    </td>
                    <td className="text-muted-foreground" title={s.last_candle_at ? new Date(s.last_candle_at).toLocaleString() : undefined}>
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
