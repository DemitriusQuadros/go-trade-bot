import React, { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useStrategies, useBacktests, useDeleteBacktest } from '@/hooks/queries';
import { useAuth } from '@/context/AuthContext';
import { Card, CardHeader } from '@/components/ui/Card';
import { History, Trash2, ArrowRight } from 'lucide-react';
import { formatNumber, formatPct } from '@/lib/format';
import { useT } from '@/i18n';

// Was BacktestLauncher.tsx: a full launch form (strategy picker + symbol/
// timeframe/dates/slippage/fees/walk-forward) living on a standalone page,
// duplicating the same form now embedded directly in each strategy's own
// Workbench Backtest tab (BacktestLaunchForm, rendered by BacktestPane.tsx
// when no run is selected yet) - the exact "page per object" pattern
// TradingView/MetaTrader avoid by keeping actions on an object as tabs in
// one workspace. This page is now what /backtest should actually be: a
// cross-strategy run history browser. To launch a NEW backtest, open the
// strategy's own Workbench and use its Backtest tab.
export function BacktestRuns() {
  const t = useT();
  const { data: strategies = [] } = useStrategies();
  const [strategyFilter, setStrategyFilter] = useState<string>('all');
  const { data: runs = [], isLoading } = useBacktests(strategyFilter === 'all' ? undefined : Number(strategyFilter));
  const deleteBacktest = useDeleteBacktest();
  // DELETE /backtest/{id} needs edit_drafts (auth-01 §3).
  const canDelete = useAuth().can('edit_drafts');

  const strategyMap = useMemo(() => {
    const map = new Map<number, string>();
    strategies.forEach((s) => map.set(s.id, s.name));
    return map;
  }, [strategies]);

  const handleDeleteRun = (e: React.MouseEvent, runId: number) => {
    e.preventDefault();
    e.stopPropagation();
    if (window.confirm(t('backtest.deleteRunConfirm', { id: runId }))) {
      deleteBacktest.mutate(runId);
    }
  };

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <History className="w-6 h-6" />
            {t('backtest.runsTitle')}
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('backtest.runsSubtitle')}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground font-medium">{t('backtest.strategyFilter')}</span>
          <select
            value={strategyFilter}
            onChange={(e) => setStrategyFilter(e.target.value)}
            className="form-select text-xs py-1"
          >
            <option value="all">{t('backtest.allStrategies')}</option>
            {strategies.map((s) => (
              <option key={s.id} value={s.id}>
                #{s.id} — {s.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      <Card>
        <CardHeader title={t('backtest.runs')} subtitle={t('backtest.runsCount', { count: runs.length })} />

        {isLoading ? (
          <div className="p-8 text-center text-xs text-muted-foreground">{t('backtest.loadingRuns')}</div>
        ) : runs.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg space-y-3">
            <p>{t('backtest.noRuns')}</p>
            <Link
              to="/strategies"
              className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs px-3 py-1.5 font-bold inline-flex items-center gap-1.5"
            >
              {t('backtest.openStrategyToRun')} <ArrowRight className="w-3.5 h-3.5" />
            </Link>
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
            {runs.map((run: any) => {
              const isPositive = run.total_return_pct >= 0;
              return (
                <Link
                  key={run.id}
                  to={`/strategies/${run.strategy_id}/edit/backtest/${run.id}`}
                  className="block p-3 bg-background/50 hover:bg-card/20 border border-border/30 rounded-lg transition-colors group"
                >
                  <div className="flex items-center justify-between mb-1">
                    <span className="font-mono font-bold text-xs text-foreground">
                      #{run.id} {run.symbol}
                    </span>
                    <div className="flex items-center gap-2">
                      <span className={`font-mono text-xs font-bold ${isPositive ? 'text-success' : 'text-destructive'}`}>
                        {formatPct(run.total_return_pct, 2, { signed: true })}
                      </span>
                      {canDelete && (
                      <button
                        type="button"
                        onClick={(e) => handleDeleteRun(e, run.id)}
                        title={t('backtest.deleteRunTitle')}
                        className="text-muted-foreground hover:text-destructive opacity-0 group-hover:opacity-100 transition-opacity"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                      )}
                    </div>
                  </div>
                  <div className="text-[11px] text-muted-foreground mb-1 truncate">
                    {t('backtest.runStrategy', { id: run.strategy_id, name: strategyMap.get(run.strategy_id) || run.strategy_name || t('backtest.unknownStrategy') })}
                  </div>
                  <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                    <span>{t('backtest.sharpeShort', { value: formatNumber(run.sharpe, { digits: 2 }) })}</span>
                    <span>{t('backtest.maxDdShort', { value: formatPct(run.max_drawdown_pct, 1) })}</span>
                    <span>{t('backtest.tradesShort', { count: run.total_trades })}</span>
                  </div>
                </Link>
              );
            })}
          </div>
        )}
      </Card>
    </div>
  );
}
