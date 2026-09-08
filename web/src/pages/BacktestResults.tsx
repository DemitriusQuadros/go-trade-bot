import { useBacktest } from '@/hooks/queries';
import React, { useState, useEffect, useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api } from '@/api/client';
import { BacktestRun, MonteCarloSummary, DrawdownPoint } from '@/api/types';
import { Card, CardHeader, MetricCard } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen } from '@/components/ui/Spinner';
import { EquityCurveChart } from '@/components/charts/EquityCurveChart';
import { DrawdownChart } from '@/components/charts/DrawdownChart';
import { MonteCarloDistribution } from '@/components/charts/MonteCarloDistribution';
import {
  ArrowLeft,
  Download,
  Dices,
  FileText,
  TrendingUp,
  Percent,
  Activity,
  AlertTriangle,
  Layers,
  Calendar,
  ExternalLink,
} from 'lucide-react';

export function BacktestResults() {
  const { id } = useParams<{ id: string }>();
  const runId = Number(id);

  const [run, setRun] = useState<BacktestRun | null>(null);
  const { data: backtest, refetch: refetchBacktest, isLoading: isBacktestLoading } = useBacktest(Number(id));
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Monte Carlo state
  const [monteCarlo, setMonteCarlo] = useState<MonteCarloSummary | null>(null);
  const [mcLoading, setMcLoading] = useState(false);
  const [mcError, setMcError] = useState<string | null>(null);

  // Tab: Charts & Analysis vs HTML Report iframe
  const [activeTab, setActiveTab] = useState<'analytics' | 'report'>('analytics');

  useEffect(() => {
    if (!runId) return;
    setLoading(true);
    setError(null);

    api
      .getBacktest(runId)
      .then((res) => {
        setRun(res);
        // Try fetching cached Monte Carlo
        api.getMonteCarlo(runId).then((mc) => setMonteCarlo(mc)).catch(() => {});
      })
      .catch((err) => setError(err.message || 'Failed to load backtest results'))
      .finally(() => setLoading(false));
  }, [runId]);

  const handleRunMonteCarlo = async () => {
    if (!runId) return;
    setMcLoading(true);
    setMcError(null);
    try {
      const res = await api.runMonteCarlo(runId, 1000);
      setMonteCarlo(res);
    } catch (err: any) {
      setMcError(err.message || 'Failed to compute Monte Carlo simulations');
    } finally {
      setMcLoading(false);
    }
  };

  // Compute drawdown curve from equity points if needed
  const drawdownPoints: DrawdownPoint[] = useMemo(() => {
    if (!run || !run.equity_curve || run.equity_curve.length === 0) return [];
    let peak = run.equity_curve[0].value;
    return run.equity_curve.map((pt) => {
      if (pt.value > peak) peak = pt.value;
      const dd = peak > 0 ? ((pt.value - peak) / peak) * 100 : 0;
      return {
        time: pt.time,
        drawdownPct: dd,
      };
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

  if (loading) {
    return <LoadingScreen message={`Loading Backtest #${runId}...`} />;
  }

  if (error || !run) {
    return (
      <div className="container-custom p-8 text-center space-y-4">
        <div className="p-4 bg-red-950/50 border border-red-800 rounded-lg max-w-md mx-auto text-xs text-red-300">
          {error || 'Backtest not found.'}
        </div>
        <Link to="/backtest" className="btn btn-secondary text-xs inline-flex items-center gap-1.5">
          <ArrowLeft className="w-4 h-4" />
          <span>Back to Launcher</span>
        </Link>
      </div>
    );
  }

  const isReturnPositive = run.total_return_pct >= 0;

  return (
    <div className="container-custom space-y-6">
      {/* Top Header */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link to="/backtest" className="text-green-700 hover:text-green-400">
              <ArrowLeft className="w-4 h-4" />
            </Link>
            <h1 className="text-2xl font-bold text-green-500 font-mono">
              Backtest #{run.id} — {run.symbol}
            </h1>
            <StatusBadge status={run.passed ? 'passed' : 'failed'} />
            {run.is_walk_forward && (
              <span className="badge badge-purple">Walk-Forward</span>
            )}
          </div>
          <p className="text-xs text-green-700">
            Strategy #{run.strategy_id} | Range: {new Date(run.start_date).toLocaleDateString()} — {new Date(run.end_date).toLocaleDateString()}
          </p>
        </div>

        {/* View Switcher */}
        <div className="flex items-center gap-2">
          <div className="bg-green-950/20 p-1 rounded-lg border border-green-900/30 flex items-center gap-1">
            <button
              onClick={() => setActiveTab('analytics')}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold ${
                activeTab === 'analytics'
                  ? 'bg-green-800 text-white shadow'
                  : 'text-green-700 hover:text-green-400'
              }`}
            >
              Analytics & Charts
            </button>
            <button
              onClick={() => setActiveTab('report')}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold flex items-center gap-1.5 ${
                activeTab === 'report'
                  ? 'bg-green-800 text-white shadow'
                  : 'text-green-700 hover:text-green-400'
              }`}
            >
              <FileText className="w-3.5 h-3.5" />
              <span>Full HTML Report</span>
            </button>
          </div>
        </div>
      </div>

      {/* Metrics Row */}
      <div className="grid-4">
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
        /* Embedded HTML Report View */
        <Card className="p-0 overflow-hidden">
          <div className="p-3 bg-green-950/20 border-b border-green-900/30 flex items-center justify-between">
            <span className="text-xs font-semibold text-green-600">
              Interactive HTML Simulation Report
            </span>
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
        /* Analytics Tab */
        <div className="space-y-6">
          {/* Equity & Drawdown Charts */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <Card>
              <CardHeader
                title="Equity Growth Curve"
                subtitle="Portfolio value progression over historical ticks"
              />
              <EquityCurveChart points={run.equity_curve || []} height={260} />
            </Card>

            <Card>
              <CardHeader
                title="Underwater Drawdown (%)"
                subtitle="High-water mark equity drawdown depth"
              />
              <DrawdownChart points={drawdownPoints} height={260} />
            </Card>
          </div>

          {/* Monte Carlo Section */}
          <Card>
            <CardHeader
              title="Monte Carlo Stress Testing"
              subtitle="Randomized permutation analysis for ruin risk estimation (1,000 iterations)"
              action={
                <button
                  onClick={handleRunMonteCarlo}
                  disabled={mcLoading}
                  className="btn btn-secondary text-xs flex items-center gap-1.5"
                >
                  <Dices className="w-3.5 h-3.5 text-green-500" />
                  <span>{mcLoading ? 'Simulating...' : 'Run Monte Carlo'}</span>
                </button>
              }
            />

            {mcError && (
              <div className="mb-4 p-3 bg-red-950/50 border border-red-800 text-red-300 text-xs rounded">
                {mcError}
              </div>
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
                Click "Run Monte Carlo" to generate 1,000 randomized resamplings of this trade log.
              </div>
            )}
          </Card>

          {/* Trade Execution Log */}
          <Card>
            <CardHeader
              title="Executed Trade Log"
              subtitle={`Detailed ledger of ${run.trade_log && Array.isArray(run.trade_log) ? run.trade_log.length : 0} simulated executions`}
              action={
                run.trade_log && Array.isArray(run.trade_log) && run.trade_log.length > 0 ? (
                  <button
                    onClick={handleExportCSV}
                    className="btn btn-secondary text-xs flex items-center gap-1.5"
                  >
                    <Download className="w-3.5 h-3.5" />
                    <span>Export CSV</span>
                  </button>
                ) : undefined
              }
            />

            {!run.trade_log || !Array.isArray(run.trade_log) || run.trade_log.length === 0 ? (
              <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
                No individual trade logs recorded for this run.
              </div>
            ) : (
              <div className="table-container max-h-96 overflow-y-auto">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Timestamp</th>
                      <th>Action</th>
                      <th>Price</th>
                      <th>Quantity</th>
                      <th>PnL ($)</th>
                      <th>Signal Reason</th>
                    </tr>
                  </thead>
                  <tbody>
                    {run.trade_log.map((trade, idx) => {
                      const isProfit = (trade.pnl || 0) >= 0;
                      return (
                        <tr key={idx}>
                          <td className="text-xs text-green-700 font-mono">
                            {trade.timestamp ? new Date(trade.timestamp).toLocaleString() : '—'}
                          </td>
                          <td>
                            <span
                              className={`badge ${
                                trade.action?.toLowerCase() === 'buy'
                                  ? 'badge-green'
                                  : trade.action?.toLowerCase() === 'sell'
                                  ? 'badge-red'
                                  : 'badge-blue'
                              }`}
                            >
                              {trade.action}
                            </span>
                          </td>
                          <td className="font-mono text-xs">${Number(trade.price || 0).toFixed(2)}</td>
                          <td className="font-mono text-xs">{Number(trade.quantity || 0).toFixed(4)}</td>
                          <td className="font-mono text-xs">
                            {trade.pnl !== undefined ? (
                              <span className={`font-semibold ${isProfit ? 'text-emerald-400' : 'text-rose-400'}`}>
                                {isProfit ? '+' : ''}${Number(trade.pnl).toFixed(2)}
                              </span>
                            ) : (
                              '—'
                            )}
                          </td>
                          <td className="text-xs text-green-700">{trade.reason || '—'}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
      )}
    </div>
  );
}
