import React from 'react';
import { History } from 'lucide-react';
import { AgentToolCall, BacktestRun } from '@/api/types';
import { useBacktest } from '@/hooks/queries';
import { EquityCurveChart } from '@/components/charts/EquityCurveChart';
import { formatMetric } from '@/lib/proposals';
import { toolArgString } from '@/lib/toolRefs';
import { formatUtcDay } from '@/lib/time';
import { CardAction, CardFallback, CardSkeleton, ChatCard, ChatDensity, Kpi, signTone } from './ChatCard';

export function profitFactorValue(pf: BacktestRun['profit_factor'] | undefined): number | null {
  if (pf == null) return null;
  return pf === 'Infinity' ? Infinity : pf;
}

function pct(v: number | null | undefined, digits = 2): string {
  if (v == null || Number.isNaN(v)) return '—';
  return `${v > 0 ? '+' : ''}${v.toFixed(digits)}%`;
}

export const formatDay = formatUtcDay;

// A run_backtest / get_backtest result (Phase D-02 §6): range, KPIs and a
// small equity curve, fetched via useBacktest.
export function BacktestCard({
  id,
  strategyId,
  call,
  density,
}: {
  id: number;
  strategyId: number | null;
  call: AgentToolCall;
  density: ChatDensity;
}) {
  const { data: run, isLoading, error } = useBacktest(id);
  if (isLoading) return <CardSkeleton label={`Loading backtest #${id}`} />;
  if (error || !run) {
    return (
      <CardFallback
        text={`Backtest #${id} couldn't be loaded.`}
        to={strategyId ? `/strategies/${strategyId}/edit/backtest/${id}` : '/backtest'}
      />
    );
  }

  const sid = run.strategy_id || strategyId;
  const timeframe = run.timeframe || toolArgString(call, 'timeframe');
  const ret = run.total_return_pct;
  const range = `${formatDay(run.start_date)} – ${formatDay(run.end_date)}`;

  return (
    <ChatCard
      testId="chat-backtest-card"
      density={density}
      icon={<History className="w-3.5 h-3.5" />}
      title={
        <>
          Backtest #{run.id}
          <span className="font-normal text-muted-foreground"> · {run.symbol}{timeframe ? ` ${timeframe}` : ''}</span>
        </>
      }
      meta={
        <span className={`font-mono tabular-nums text-[11px] ${ret > 0 ? 'text-success' : ret < 0 ? 'text-destructive' : 'text-muted-foreground'}`}>
          {pct(ret)} · {run.total_trades} trades
        </span>
      }
      actions={
        <>
          {sid ? <CardAction to={`/strategies/${sid}/edit/backtest/${run.id}`}>Open in Code mode</CardAction> : null}
          <CardAction to="/backtest">All runs</CardAction>
        </>
      }
    >
      <div className="text-[11px] text-muted-foreground font-mono">
        {run.strategy_name ? `${run.strategy_name} · ` : ''}
        {range}
        {run.is_walk_forward ? ' · walk-forward' : ''}
      </div>
      <div className="grid grid-cols-3 gap-1.5">
        <Kpi label="Return" value={pct(ret)} tone={signTone(ret)} />
        <Kpi label="Sharpe" value={formatMetric(run.sharpe)} tone={signTone(run.sharpe)} />
        <Kpi
          label="Max DD"
          value={run.max_drawdown_pct == null ? '—' : `${Math.abs(run.max_drawdown_pct).toFixed(2)}%`}
          tone={run.max_drawdown_pct ? 'destructive' : undefined}
        />
        <Kpi label="Win rate" value={run.win_rate_pct == null ? '—' : `${run.win_rate_pct.toFixed(1)}%`} />
        <Kpi label="Profit factor" value={formatMetric(profitFactorValue(run.profit_factor))} />
        <Kpi label="Trades" value={String(run.total_trades ?? '—')} />
      </div>
      <EquityCurveChart
        points={run.equity_curve ?? []}
        height={140}
        summary={`Equity curve for backtest ${run.id}, total return ${pct(ret)}.`}
      />
    </ChatCard>
  );
}
