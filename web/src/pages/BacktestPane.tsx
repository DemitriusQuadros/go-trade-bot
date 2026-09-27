import React, { useEffect, useMemo, useState } from 'react';
import { useOutletContext, useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { api } from '@/api/client';
import { useBacktest } from '@/hooks/queries';
import { MonteCarloSummary, DrawdownPoint } from '@/api/types';
import { Card, CardHeader, MetricCard } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen } from '@/components/ui/Spinner';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { EquityCurveChart } from '@/components/charts/EquityCurveChart';
import { DrawdownChart } from '@/components/charts/DrawdownChart';
import { MonteCarloDistribution } from '@/components/charts/MonteCarloDistribution';
import { BacktestLaunchForm } from '@/components/domain/BacktestLaunchForm';
import { WorkbenchContext } from './WorkbenchShell';
import {
  Download,
  Dices,
  FileText,
  TrendingUp,
  Percent,
  Activity,
  AlertTriangle,
  ExternalLink,
} from 'lucide-react';

export function BacktestPane() {
  const ctx = useOutletContext<WorkbenchContext>();
  const { runId } = useParams<{ runId?: string }>();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { setBacktestRun, setActiveTraceSource, strategyId, appendConsoleEntry } = ctx;

  const numericRunId = runId ? Number(runId) : 0;
  const { data: run, isLoading } = useBacktest(numericRunId);

  const [monteCarlo, setMonteCarlo] = useState<MonteCarloSummary | null>(null);
  const [mcLoading, setMcLoading] = useState(false);
  const [mcError, setMcError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<'analytics' | 'report'>('analytics');

  useEffect(() => {
    setActiveTraceSource('backtest');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    setBacktestRun(run || null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run]);

  useEffect(() => {
    if (!numericRunId) return;
    api
      .getMonteCarlo(numericRunId)
      .then((mc) => setMonteCarlo(mc))
      .catch(() => {});
  }, [numericRunId]);

  useEffect(() => {
    if (run?.execution_trace && run.execution_trace.length > 0) {
      // Console log entries for a selected run's own execution trace -
      // last-cycle-only, same volume-shaping rule as Editor/REPL panes.
      const lastRecord = run.execution_trace[run.execution_trace.length - 1];
      lastRecord?.log?.forEach((entry) =>
        appendConsoleEntry({ source: 'backtest', kind: 'debug_log', label: entry.label, message: String(entry.value) })
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run?.id]);

  const handleRunMonteCarlo = async () => {
    if (!numericRunId) return;
    setMcLoading(true);
    setMcError(null);
    try {
      const res = await api.runMonteCarlo(numericRunId, 1000);
      setMonteCarlo(res);
    } catch (err: any) {
      setMcError(err.message || 'Failed to compute Monte Carlo simulations');
    } finally {
      setMcLoading(false);
    }
  };

  const drawdownPoints: DrawdownPoint[] = useMemo(() => {
    if (!run || !run.equity_curve || run.equity_curve.length === 0) return [];
    let peak = run.equity_curve[0].value;
    return run.equity_curve.map((pt) => {
      if (pt.value > peak) peak = pt.value;
      const dd = peak > 0 ? ((pt.value - peak) / peak) * 100 : 0;
      return { time: pt.time, drawdownPct: dd };
    });
  }, [run]);

  const handleExportCSV = () => {
    if (!run || !run.trade_log || !Array.isArray(run.trade_log)) return;
    const headers = ['Entry Time', 'Exit Time', 'Entry Price', 'Exit Price', 'Quantity', 'Profit', 'Exit Reason'];
    const rows = run.trade_log.map((t) => [
      t.entry_time || '',
      t.exit_time || '',
      t.entry_price || '',
      t.exit_price || '',
      t.quantity || '',
      t.profit || '',
      `"${(t.exit_reason || '').replace(/"/g, '""')}"`,
    ]);
    const csvContent = [headers.join(','), ...rows.map((r) => r.join(','))].join('\n');
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', `backtest_${run.id}_tradelog.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  if (!runId) {
    // Launching a backtest for THIS strategy used to mean leaving the
    // Workbench for a standalone /backtest page (frontend-01's original
    // "link out" resolution) - a page-per-object detour both TradingView
    // and MetaTrader avoid (one workspace, tabs for every action on the
    // open object). Embedded here instead; /backtest is now a pure
    // cross-strategy run history browser (BacktestRuns.tsx).
    if (strategyId == null) {
      return (
        <div className="p-8 text-center text-xs text-muted-foreground">
          Save this strategy before running a backtest.
        </div>
      );
    }
    return (
      <Card>
        <CardHeader title="Launch Historical Simulation" subtitle="Configure the test range, symbol & fill assumptions for this strategy" />
        <BacktestLaunchForm
          strategyId={strategyId}
          initialSymbol={searchParams.get('symbol') || undefined}
          onLaunched={(newRunId) => navigate(`/strategies/${strategyId}/edit/backtest/${newRunId}`)}
        />
      </Card>
    );
  }

  if (isLoading) {
    return <LoadingScreen message={`Loading Backtest #${runId}...`} />;
  }

  if (!run) {
    return (
      <div className="p-8 text-center text-xs text-destructive bg-destructive/15 border border-destructive/40 rounded-lg">
        Backtest run #{runId} not found.
      </div>
    );
  }

  const isReturnPositive = run.total_return_pct >= 0;

  return (
    <div className="space-y-6">
      {/* Was md:flex-row - a viewport-width breakpoint that fires regardless
          of this pane's actual (narrow) container width once squeezed into
          WorkbenchShell's side panel. Always-stacked, tab labels shortened
          to fit. */}
      <div className="flex flex-col gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2 mb-1">
            <h2 className="text-lg font-bold text-foreground font-mono">
              Backtest #{run.id} — {run.symbol}
            </h2>
            <StatusBadge status={run.passed ? 'passed' : 'failed'} />
            {run.is_walk_forward && (
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-accent text-accent-foreground border-border">
                Walk-Forward
              </span>
            )}
          </div>
          <p className="text-xs text-muted-foreground">
            Strategy #{run.strategy_id}
            {run.strategy_name ? ` — ${run.strategy_name}` : ''}
          </p>
          <p className="text-xs text-muted-foreground">
            {new Date(run.start_date).toLocaleDateString()} — {new Date(run.end_date).toLocaleDateString()}
          </p>
        </div>

        <div className="bg-card/20 p-1 rounded-lg border border-border/30 flex items-center gap-1 w-fit">
          <button
            onClick={() => setActiveTab('analytics')}
            className={`px-3 py-1.5 rounded-md text-xs font-semibold ${
              activeTab === 'analytics' ? 'bg-secondary text-white shadow' : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            Analytics
          </button>
          <button
            onClick={() => setActiveTab('report')}
            className={`px-3 py-1.5 rounded-md text-xs font-semibold flex items-center gap-1.5 ${
              activeTab === 'report' ? 'bg-secondary text-white shadow' : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            <FileText className="w-3.5 h-3.5" />
            <span>Report</span>
          </button>
        </div>
      </div>

      {/* Was `.grid-4` - a dead class (no CSS rule ever defined it, same
          issue as `.btn`/`.badge`/`.table` elsewhere in this pane), so these
          4 metric cards rendered in normal block flow with zero grid
          layout. 2 columns fits the narrow side-panel width this pane
          renders in now (see WorkbenchShell). */}
      <div className="grid grid-cols-2 gap-3">
        <MetricCard
          title="Total Return"
          value={`${isReturnPositive ? '+' : ''}${run.total_return_pct.toFixed(2)}%`}
          isPositive={isReturnPositive}
          subtitle={`Trades executed: ${run.total_trades}`}
          icon={<TrendingUp className="w-4 h-4 text-success" />}
        />
        <MetricCard
          title="Sharpe Ratio"
          value={run.sharpe.toFixed(2)}
          subtitle={run.sharpe >= 1.5 ? 'Strong risk-adjusted return' : 'Moderate'}
          icon={<Activity className="w-4 h-4 text-foreground" />}
        />
        <MetricCard
          title="Max Drawdown"
          value={`${run.max_drawdown_pct.toFixed(2)}%`}
          isPositive={false}
          subtitle="Peak-to-trough decline"
          icon={<AlertTriangle className="w-4 h-4 text-destructive" />}
        />
        <MetricCard
          title="Win Rate / Profit Factor"
          value={`${run.win_rate_pct.toFixed(1)}%`}
          subtitle={`Profit Factor: ${run.profit_factor}`}
          icon={<Percent className="w-4 h-4 text-warning" />}
        />
      </div>

      {activeTab === 'report' ? (
        <Card className="p-0 overflow-hidden">
          <div className="p-3 bg-card/20 border-b border-border/30 flex items-center justify-between">
            <span className="text-xs font-semibold text-muted-foreground">Interactive HTML Simulation Report</span>
            <a
              href={api.getReportUrl(run.id)}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-foreground hover:text-foreground flex items-center gap-1 font-medium"
            >
              <span>Open in new tab</span>
              <ExternalLink className="w-3 h-3" />
            </a>
          </div>
          <iframe
            src={api.getReportUrl(run.id)}
            title={`Backtest ${run.id} Report`}
            className="w-full h-[800px] border-none bg-background"
          />
        </Card>
      ) : (
        <div className="space-y-6">
          {/* Was lg:grid-cols-2 - a viewport-width breakpoint, but this pane
              only renders inside WorkbenchShell's narrow left panel (see
              App.tsx routes), where that breakpoint fires off the window's
              width regardless of how narrow this container actually is. */}
          <div className="grid grid-cols-1 gap-6">
            <CollapsibleSection id="workbench.backtest.equity" title="Equity Growth Curve" defaultOpen>
              <EquityCurveChart points={run.equity_curve || []} height={260} />
            </CollapsibleSection>
            <CollapsibleSection id="workbench.backtest.drawdown" title="Underwater Drawdown (%)" defaultOpen>
              <DrawdownChart points={drawdownPoints} height={260} />
            </CollapsibleSection>
          </div>

          <CollapsibleSection
            id="workbench.backtest.montecarlo"
            title="Monte Carlo Stress Testing"
            subtitle="(1,000 iterations)"
            defaultOpen
            action={
              <button
                onClick={handleRunMonteCarlo}
                disabled={mcLoading}
                className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs flex items-center gap-1.5 px-2.5 py-1"
              >
                <Dices className="w-3.5 h-3.5 text-foreground" />
                <span>{mcLoading ? 'Simulating...' : 'Run Monte Carlo'}</span>
              </button>
            }
          >
            {mcError && (
              <div className="mb-4 p-3 bg-destructive/15 border border-destructive/40 text-destructive text-xs rounded">{mcError}</div>
            )}
            {monteCarlo ? (
              <MonteCarloDistribution
                totalReturn={monteCarlo.total_return_distribution}
                sharpe={monteCarlo.sharpe_distribution}
                maxDrawdown={monteCarlo.max_drawdown_distribution}
                iterations={monteCarlo.iterations}
              />
            ) : (
              <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
                Click "Run Monte Carlo" for 1,000 randomized resamplings of this trade log.
              </div>
            )}
          </CollapsibleSection>

          <CollapsibleSection
            id="workbench.backtest.tradeLog"
            title="Executed Trade Log"
            subtitle={`(${run.trade_log && Array.isArray(run.trade_log) ? run.trade_log.length : 0})`}
            defaultOpen
            action={
              run.trade_log && Array.isArray(run.trade_log) && run.trade_log.length > 0 ? (
                <button
                  onClick={handleExportCSV}
                  className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs flex items-center gap-1.5 px-2.5 py-1"
                >
                  <Download className="w-3.5 h-3.5" />
                  <span>Export CSV</span>
                </button>
              ) : undefined
            }
          >
            {!run.trade_log || !Array.isArray(run.trade_log) || run.trade_log.length === 0 ? (
              <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
                No individual trade logs recorded for this run.
              </div>
            ) : (
              // `.table`/`.table-container` were dead classes (see the
              // `.btn`/`.badge`/`.grid-4` note above) - this rendered as a
              // bare unstyled HTML table with wrapping cells, which is what
              // made it look broken squeezed into the narrow side panel.
              // Real Tailwind now: whitespace-nowrap cells + horizontal
              // scroll instead of wrapping, compact abbreviated headers, and
              // a shorter timestamp format so the column doesn't dominate.
              <div className="max-h-96 overflow-auto rounded border border-border/60">
                <table className="w-full text-xs border-collapse">
                  <thead className="sticky top-0 bg-card/60">
                    <tr>
                      <th className="px-2 py-1.5 text-left text-foreground uppercase text-[10px] font-semibold whitespace-nowrap">
                        Entry
                      </th>
                      <th className="px-2 py-1.5 text-left text-foreground uppercase text-[10px] font-semibold whitespace-nowrap">
                        Exit
                      </th>
                      <th className="px-2 py-1.5 text-right text-foreground uppercase text-[10px] font-semibold whitespace-nowrap">
                        Entry Price
                      </th>
                      <th className="px-2 py-1.5 text-right text-foreground uppercase text-[10px] font-semibold whitespace-nowrap">
                        Exit Price
                      </th>
                      <th className="px-2 py-1.5 text-right text-foreground uppercase text-[10px] font-semibold whitespace-nowrap">
                        Qty
                      </th>
                      <th className="px-2 py-1.5 text-right text-foreground uppercase text-[10px] font-semibold whitespace-nowrap">
                        Profit
                      </th>
                      <th className="px-2 py-1.5 text-left text-foreground uppercase text-[10px] font-semibold whitespace-nowrap">
                        Reason
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {run.trade_log.map((trade, idx) => {
                      const isProfit = (trade.profit || 0) >= 0;
                      const formatTs = (ts: string) =>
                        ts
                          ? new Date(ts).toLocaleString(undefined, {
                              month: '2-digit',
                              day: '2-digit',
                              hour: '2-digit',
                              minute: '2-digit',
                            })
                          : '—';
                      return (
                        <tr key={idx} className="border-t border-border/40 hover:bg-card/10">
                          <td className="px-2 py-1.5 text-muted-foreground font-mono whitespace-nowrap">
                            {formatTs(trade.entry_time)}
                          </td>
                          <td className="px-2 py-1.5 text-muted-foreground font-mono whitespace-nowrap">
                            {formatTs(trade.exit_time)}
                          </td>
                          <td className="px-2 py-1.5 font-mono text-right whitespace-nowrap">
                            ${Number(trade.entry_price || 0).toFixed(2)}
                          </td>
                          <td className="px-2 py-1.5 font-mono text-right whitespace-nowrap">
                            ${Number(trade.exit_price || 0).toFixed(2)}
                          </td>
                          <td className="px-2 py-1.5 font-mono text-right whitespace-nowrap">
                            {Number(trade.quantity || 0).toFixed(4)}
                          </td>
                          <td className="px-2 py-1.5 font-mono text-right whitespace-nowrap">
                            {trade.profit !== undefined ? (
                              <span className={`font-semibold ${isProfit ? 'text-success' : 'text-destructive'}`}>
                                {isProfit ? '+' : ''}${Number(trade.profit).toFixed(2)}
                              </span>
                            ) : (
                              '—'
                            )}
                          </td>
                          <td className="px-2 py-1.5 text-muted-foreground max-w-[200px] truncate" title={trade.exit_reason || ''}>
                            {trade.exit_reason || '—'}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </CollapsibleSection>
        </div>
      )}
    </div>
  );
}
