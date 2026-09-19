import React, { useEffect, useMemo, useState } from 'react';
import { useOutletContext, useParams, Link } from 'react-router-dom';
import { api } from '@/api/client';
import { useBacktest } from '@/hooks/queries';
import { MonteCarloSummary, DrawdownPoint } from '@/api/types';
import { Card, MetricCard } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen } from '@/components/ui/Spinner';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { EquityCurveChart } from '@/components/charts/EquityCurveChart';
import { DrawdownChart } from '@/components/charts/DrawdownChart';
import { MonteCarloDistribution } from '@/components/charts/MonteCarloDistribution';
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
    const headers = ['Timestamp', 'Action', 'Price', 'Quantity', 'PnL', 'Reason'];
    const rows = run.trade_log.map((t) => [
      t.timestamp || '',
      t.action || '',
      t.price || '',
      t.quantity || '',
      t.pnl || '',
      `"${(t.reason || '').replace(/"/g, '""')}"`,
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
    // "No run selected yet" state, scoped to this strategy - links out to
    // the existing standalone launcher rather than embedding a mini-
    // launcher (frontend-01's resolved open question).
    return (
      <div className="p-8 text-center space-y-3">
        <p className="text-sm text-green-400">No backtest run selected for this strategy yet.</p>
        <Link
          to={`/backtest?strategy_id=${strategyId ?? ''}`}
          className="bg-green-700 hover:bg-green-600 text-white rounded border border-green-600 text-xs inline-flex items-center gap-1.5 px-4 py-2"
        >
          Launch a new backtest
        </Link>
      </div>
    );
  }

  if (isLoading) {
    return <LoadingScreen message={`Loading Backtest #${runId}...`} />;
  }

  if (!run) {
    return (
      <div className="p-8 text-center text-xs text-red-300 bg-red-950/40 border border-red-800 rounded-lg">
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
            <h2 className="text-lg font-bold text-green-500 font-mono">
              Backtest #{run.id} — {run.symbol}
            </h2>
            <StatusBadge status={run.passed ? 'passed' : 'failed'} />
            {run.is_walk_forward && (
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-purple-900/40 text-purple-300 border-purple-700/40">
                Walk-Forward
              </span>
            )}
          </div>
          <p className="text-xs text-green-700">
            {new Date(run.start_date).toLocaleDateString()} — {new Date(run.end_date).toLocaleDateString()}
          </p>
        </div>

        <div className="bg-green-950/20 p-1 rounded-lg border border-green-900/30 flex items-center gap-1 w-fit">
          <button
            onClick={() => setActiveTab('analytics')}
            className={`px-3 py-1.5 rounded-md text-xs font-semibold ${
              activeTab === 'analytics' ? 'bg-green-800 text-white shadow' : 'text-green-700 hover:text-green-400'
            }`}
          >
            Analytics
          </button>
          <button
            onClick={() => setActiveTab('report')}
            className={`px-3 py-1.5 rounded-md text-xs font-semibold flex items-center gap-1.5 ${
              activeTab === 'report' ? 'bg-green-800 text-white shadow' : 'text-green-700 hover:text-green-400'
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
          icon={<TrendingUp className="w-4 h-4 text-emerald-400" />}
        />
        <MetricCard
          title="Sharpe Ratio"
          value={run.sharpe.toFixed(2)}
          subtitle={run.sharpe >= 1.5 ? 'Strong risk-adjusted return' : 'Moderate'}
          icon={<Activity className="w-4 h-4 text-green-500" />}
        />
        <MetricCard
          title="Max Drawdown"
          value={`${run.max_drawdown_pct.toFixed(2)}%`}
          isPositive={false}
          subtitle="Peak-to-trough decline"
          icon={<AlertTriangle className="w-4 h-4 text-rose-400" />}
        />
        <MetricCard
          title="Win Rate / Profit Factor"
          value={`${run.win_rate_pct.toFixed(1)}%`}
          subtitle={`Profit Factor: ${run.profit_factor}`}
          icon={<Percent className="w-4 h-4 text-amber-400" />}
        />
      </div>

      {activeTab === 'report' ? (
        <Card className="p-0 overflow-hidden">
          <div className="p-3 bg-green-950/20 border-b border-green-900/30 flex items-center justify-between">
            <span className="text-xs font-semibold text-green-600">Interactive HTML Simulation Report</span>
            <a
              href={api.getReportUrl(run.id)}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-green-500 hover:text-blue-300 flex items-center gap-1 font-medium"
            >
              <span>Open in new tab</span>
              <ExternalLink className="w-3 h-3" />
            </a>
          </div>
          <iframe
            src={api.getReportUrl(run.id)}
            title={`Backtest ${run.id} Report`}
            className="w-full h-[800px] border-none bg-black"
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
                className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs flex items-center gap-1.5 px-2.5 py-1"
              >
                <Dices className="w-3.5 h-3.5 text-green-500" />
                <span>{mcLoading ? 'Simulating...' : 'Run Monte Carlo'}</span>
              </button>
            }
          >
            {mcError && (
              <div className="mb-4 p-3 bg-red-950/50 border border-red-800 text-red-300 text-xs rounded">{mcError}</div>
            )}
            {monteCarlo ? (
              <MonteCarloDistribution
                distribution={monteCarlo.distribution}
                mean={monteCarlo.mean_return}
                var95={monteCarlo.var_95}
                ruinProb={monteCarlo.ruin_probability}
                height={220}
              />
            ) : (
              <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
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
                  className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs flex items-center gap-1.5 px-2.5 py-1"
                >
                  <Download className="w-3.5 h-3.5" />
                  <span>Export CSV</span>
                </button>
              ) : undefined
            }
          >
            {!run.trade_log || !Array.isArray(run.trade_log) || run.trade_log.length === 0 ? (
              <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
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
              <div className="max-h-96 overflow-auto rounded border border-green-950/60">
                <table className="w-full text-xs border-collapse">
                  <thead className="sticky top-0 bg-green-950/60">
                    <tr>
                      <th className="px-2 py-1.5 text-left text-green-500 uppercase text-[10px] font-semibold whitespace-nowrap">
                        Time
                      </th>
                      <th className="px-2 py-1.5 text-left text-green-500 uppercase text-[10px] font-semibold whitespace-nowrap">
                        Action
                      </th>
                      <th className="px-2 py-1.5 text-right text-green-500 uppercase text-[10px] font-semibold whitespace-nowrap">
                        Price
                      </th>
                      <th className="px-2 py-1.5 text-right text-green-500 uppercase text-[10px] font-semibold whitespace-nowrap">
                        Qty
                      </th>
                      <th className="px-2 py-1.5 text-right text-green-500 uppercase text-[10px] font-semibold whitespace-nowrap">
                        PnL
                      </th>
                      <th className="px-2 py-1.5 text-left text-green-500 uppercase text-[10px] font-semibold whitespace-nowrap">
                        Reason
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {run.trade_log.map((trade, idx) => {
                      const isProfit = (trade.pnl || 0) >= 0;
                      const isBuy = trade.action?.toLowerCase() === 'buy';
                      const isSell = trade.action?.toLowerCase() === 'sell';
                      return (
                        <tr key={idx} className="border-t border-green-950/40 hover:bg-green-950/10">
                          <td className="px-2 py-1.5 text-green-700 font-mono whitespace-nowrap">
                            {trade.timestamp
                              ? new Date(trade.timestamp).toLocaleString(undefined, {
                                  month: '2-digit',
                                  day: '2-digit',
                                  hour: '2-digit',
                                  minute: '2-digit',
                                })
                              : '—'}
                          </td>
                          <td className="px-2 py-1.5 whitespace-nowrap">
                            <span
                              className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border ${
                                isBuy
                                  ? 'bg-green-900/40 text-green-300 border-green-700/40'
                                  : isSell
                                  ? 'bg-red-900/40 text-red-300 border-red-700/40'
                                  : 'bg-blue-900/40 text-blue-300 border-blue-700/40'
                              }`}
                            >
                              {trade.action}
                            </span>
                          </td>
                          <td className="px-2 py-1.5 font-mono text-right whitespace-nowrap">
                            ${Number(trade.price || 0).toFixed(2)}
                          </td>
                          <td className="px-2 py-1.5 font-mono text-right whitespace-nowrap">
                            {Number(trade.quantity || 0).toFixed(4)}
                          </td>
                          <td className="px-2 py-1.5 font-mono text-right whitespace-nowrap">
                            {trade.pnl !== undefined ? (
                              <span className={`font-semibold ${isProfit ? 'text-emerald-400' : 'text-rose-400'}`}>
                                {isProfit ? '+' : ''}${Number(trade.pnl).toFixed(2)}
                              </span>
                            ) : (
                              '—'
                            )}
                          </td>
                          <td className="px-2 py-1.5 text-green-700 max-w-[200px] truncate" title={trade.reason || ''}>
                            {trade.reason || '—'}
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
