import React, { useState, useEffect, useMemo } from 'react';
import { api } from '@/api/client';
import { useStrategies } from '@/hooks/queries';
import { useQuery } from '@tanstack/react-query';
import {
  OptimizationStatusResponse,
  OptimizationResults,
  ParamRange,
} from '@/api/types';
import { Card, CardHeader, MetricCard } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen, Spinner } from '@/components/ui/Spinner';
import { ParamHeatmap } from '@/components/charts/ParamHeatmap';
import {
  Zap,
  Play,
  Calendar,
  Grid,
  TrendingUp,
  Award,
  AlertCircle,
  CheckCircle,
} from 'lucide-react';

export function Optimization() {
  
  

  // Launcher state
  const { data: strategies = [] } = useStrategies();
  const [strategyId, setStrategyId] = useState<number>(strategies[0]?.id || 1);
  const [symbol, setSymbol] = useState('BTCUSDT');
  const [timeframe, setTimeframe] = useState('1h');
  const [startDate, setStartDate] = useState(() => {
    const d = new Date();
    d.setMonth(d.getMonth() - 2);
    return d.toISOString().split('T')[0];
  });
  const [endDate, setEndDate] = useState(() => new Date().toISOString().split('T')[0]);

  // Param grid definitions
  const [param1Name, setParam1Name] = useState('grid_levels');
  const [param1Min, setParam1Min] = useState(5);
  const [param1Max, setParam1Max] = useState(25);
  const [param1Step, setParam1Step] = useState(5);

  const [param2Name, setParam2Name] = useState('grid_spacing_pct');
  const [param2Min, setParam2Min] = useState(0.2);
  const [param2Max, setParam2Max] = useState(1.0);
  const [param2Step, setParam2Step] = useState(0.2);

  // Active run state
  const [activeRunId, setActiveRunId] = useState<number | null>(null);
  const { data: statusData, refetch: refetchStatus } = useQuery({ queryKey: ['optStatus', activeRunId], queryFn: () => api.getOptimizationStatus(activeRunId as number, {}), enabled: !!activeRunId, refetchInterval: 2000 });
  const [launching, setLaunching] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Status polling for active run
  
  const isCompleted = (statusData as any)?.status === "completed";


  // Results fetch
  const [results, setResults] = useState<OptimizationResults | null>(null);
  const [resultsLoading, setResultsLoading] = useState(false);

  useEffect(() => {
    if (activeRunId && isCompleted) {
      setResultsLoading(true);
      api
        .getOptimizationResults(activeRunId)
        .then((res) => setResults(res))
        .catch((err) => setError(err.message))
        .finally(() => setResultsLoading(false));
    }
  }, [activeRunId, isCompleted]);

  const handleLaunch = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLaunching(true);
    setResults(null);

    const param_grid: Record<string, ParamRange> = {
      [param1Name]: { min: Number(param1Min), max: Number(param1Max), step: Number(param1Step) },
      [param2Name]: { min: Number(param2Min), max: Number(param2Max), step: Number(param2Step) },
    };

    try {
      const res = await api.startOptimization({
        strategy_id: Number(strategyId),
        symbol,
        timeframe,
        start_date: new Date(startDate).toISOString(),
        end_date: new Date(endDate).toISOString(),
        param_grid,
      });
      setActiveRunId(res.id);
    } catch (err: any) {
      setError(err.message || 'Failed to start optimization run');
    } finally {
      setLaunching(false);
    }
  };

  // Build heatmap matrix from results.grid
  const heatmapData = useMemo(() => {
    if (!results || !results.grid || results.grid.length === 0) return null;

    const xVals = Array.from(new Set(results.grid.map((g) => g.params[param1Name]))).sort((a, b) => a - b);
    const yVals = Array.from(new Set(results.grid.map((g) => g.params[param2Name]))).sort((a, b) => a - b);

    const cellMatrix: (number | null)[][] = [];
    let bestX = 0;
    let bestY = 0;
    let bestSharpe = -Infinity;

    for (let yi = 0; yi < yVals.length; yi++) {
      const row: (number | null)[] = [];
      for (let xi = 0; xi < xVals.length; xi++) {
        const point = results.grid.find(
          (g) => g.params[param1Name] === xVals[xi] && g.params[param2Name] === yVals[yi]
        );
        const sharpe = point?.metrics?.sharpe ?? null;
        row.push(sharpe);
        if (sharpe !== null && sharpe > bestSharpe) {
          bestSharpe = sharpe;
          bestX = xi;
          bestY = yi;
        }
      }
      cellMatrix.push(row);
    }

    return {
      xVals,
      yVals,
      cellMatrix,
      bestX,
      bestY,
    };
  }, [results, param1Name, param2Name]);

  return (
    <div className="container-custom space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-green-500 flex items-center gap-2">
          Grid Parameter Optimization
        </h1>
        <p className="text-xs text-green-700 mt-0.5">
          Exhaustive multi-dimensional hyperparameter sweep & Sharpe response heatmap
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left Col: Launcher */}
        <div>
          <Card>
            <CardHeader
              title="Launch Parameter Grid"
              subtitle="Define search space ranges & testing period"
            />

            {error && (
              <div className="mb-4 p-3 bg-red-950/60 border border-red-800 text-red-300 rounded text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 flex-shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <form onSubmit={handleLaunch} className="space-y-4 text-xs">
              <div className="form-group mb-0">
                <label className="form-label">Strategy</label>
                <select
                  value={strategyId}
                  onChange={(e) => setStrategyId(Number(e.target.value))}
                  className="form-select text-xs"
                >
                  {strategies.map((s: any) => (
                    <option key={s.id} value={s.id}>
                      #{s.id} — {s.name} ({s.strategy_name})
                    </option>
                  ))}
                </select>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div className="form-group mb-0">
                  <label className="form-label">Symbol</label>
                  <input
                    type="text"
                    required
                    value={symbol}
                    onChange={(e) => setSymbol(e.target.value.toUpperCase())}
                    className="form-input text-xs font-mono"
                  />
                </div>
                <div className="form-group mb-0">
                  <label className="form-label">Timeframe</label>
                  <select
                    value={timeframe}
                    onChange={(e) => setTimeframe(e.target.value)}
                    className="form-select text-xs"
                  >
                    <option value="5m">5m</option>
                    <option value="15m">15m</option>
                    <option value="1h">1h</option>
                    <option value="4h">4h</option>
                  </select>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div className="form-group mb-0">
                  <label className="form-label">Start Date</label>
                  <input
                    type="date"
                    required
                    value={startDate}
                    onChange={(e) => setStartDate(e.target.value)}
                    className="form-input text-xs"
                  />
                </div>
                <div className="form-group mb-0">
                  <label className="form-label">End Date</label>
                  <input
                    type="date"
                    required
                    value={endDate}
                    onChange={(e) => setEndDate(e.target.value)}
                    className="form-input text-xs"
                  />
                </div>
              </div>

              {/* Parameter 1 */}
              <div className="p-3 bg-green-950/20/60 rounded-lg border border-green-900/30 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-green-500">Parameter 1 (X-Axis)</span>
                </div>
                <input
                  type="text"
                  required
                  placeholder="Parameter name (e.g. grid_levels)"
                  value={param1Name}
                  onChange={(e) => setParam1Name(e.target.value)}
                  className="form-input text-xs font-mono mb-2"
                />
                <div className="grid grid-cols-3 gap-2">
                  <div>
                    <label className="form-label text-[10px]">Min</label>
                    <input
                      type="number"
                      step="any"
                      required
                      value={param1Min}
                      onChange={(e) => setParam1Min(Number(e.target.value))}
                      className="form-input text-xs font-mono"
                    />
                  </div>
                  <div>
                    <label className="form-label text-[10px]">Max</label>
                    <input
                      type="number"
                      step="any"
                      required
                      value={param1Max}
                      onChange={(e) => setParam1Max(Number(e.target.value))}
                      className="form-input text-xs font-mono"
                    />
                  </div>
                  <div>
                    <label className="form-label text-[10px]">Step</label>
                    <input
                      type="number"
                      step="any"
                      required
                      value={param1Step}
                      onChange={(e) => setParam1Step(Number(e.target.value))}
                      className="form-input text-xs font-mono"
                    />
                  </div>
                </div>
              </div>

              {/* Parameter 2 */}
              <div className="p-3 bg-green-950/20/60 rounded-lg border border-green-900/30 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-purple-400">Parameter 2 (Y-Axis)</span>
                </div>
                <input
                  type="text"
                  required
                  placeholder="Parameter name (e.g. grid_spacing_pct)"
                  value={param2Name}
                  onChange={(e) => setParam2Name(e.target.value)}
                  className="form-input text-xs font-mono mb-2"
                />
                <div className="grid grid-cols-3 gap-2">
                  <div>
                    <label className="form-label text-[10px]">Min</label>
                    <input
                      type="number"
                      step="any"
                      required
                      value={param2Min}
                      onChange={(e) => setParam2Min(Number(e.target.value))}
                      className="form-input text-xs font-mono"
                    />
                  </div>
                  <div>
                    <label className="form-label text-[10px]">Max</label>
                    <input
                      type="number"
                      step="any"
                      required
                      value={param2Max}
                      onChange={(e) => setParam2Max(Number(e.target.value))}
                      className="form-input text-xs font-mono"
                    />
                  </div>
                  <div>
                    <label className="form-label text-[10px]">Step</label>
                    <input
                      type="number"
                      step="any"
                      required
                      value={param2Step}
                      onChange={(e) => setParam2Step(Number(e.target.value))}
                      className="form-input text-xs font-mono"
                    />
                  </div>
                </div>
              </div>

              <button
                type="submit"
                disabled={launching || (!!activeRunId && !isCompleted)}
                className="w-full btn btn-primary py-2.5 flex items-center justify-center gap-2 font-semibold text-xs"
              >
                <Play className="w-4 h-4" />
                <span>
                  {launching || (!!activeRunId && !isCompleted)
                    ? 'Optimization in progress...'
                    : 'Start Parameter Sweep'}
                </span>
              </button>
            </form>
          </Card>
        </div>

        {/* Right 2 Cols: Progress & Results */}
        <div className="lg:col-span-2 space-y-6">
          {/* Progress / Status Monitor */}
          {activeRunId && (
            <Card>
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <span className="font-mono font-bold text-sm text-green-500">
                    Optimization Run #{activeRunId}
                  </span>
                  <StatusBadge status={statusData?.status || 'running'} />
                </div>
                <span className="text-xs font-mono text-green-600">
                  {statusData ? `${Math.round(statusData.progress * 100)}%` : '0%'}
                </span>
              </div>

              {/* Progress Bar */}
              <div className="w-full bg-green-950/20 rounded-full h-2 overflow-hidden border border-green-900/30">
                <div
                  className="bg-green-800 h-full transition-all duration-300 rounded-full"
                  style={{ width: `${(statusData?.progress || 0) * 100}%` }}
                />
              </div>

              <div className="flex items-center justify-between text-[11px] text-green-700 mt-2">
                <span>Total Combinations: {statusData?.total_combinations || '—'}</span>
                {!isCompleted && (
                  <span className="flex items-center gap-1 text-green-500">
                    <Spinner size="sm" />
                    <span>Evaluating backtest candidates in parallel...</span>
                  </span>
                )}
                {isCompleted && (
                  <span className="flex items-center gap-1 text-emerald-400 font-semibold">
                    <CheckCircle className="w-3.5 h-3.5" />
                    <span>Sweep Completed</span>
                  </span>
                )}
              </div>
            </Card>
          )}

          {/* Results Heatmap & Top Candidates */}
          {results && heatmapData ? (
            <div className="space-y-6">
              {/* Best Configuration Callout */}
              <Card className="border-emerald-500/30 bg-emerald-950/20">
                <div className="flex items-start justify-between">
                  <div>
                    <span className="badge badge-green mb-1">Optimal Parameter Set</span>
                    <h3 className="text-base font-bold text-green-500 mt-1">
                      Best Discovered Configuration
                    </h3>
                    <div className="flex items-center gap-4 mt-2 text-xs font-mono">
                      {Object.entries(results.best_config || {}).map(([k, v]) => (
                        <span key={k} className="p-1.5 bg-green-950/20/80 rounded border border-green-900/30">
                          <strong className="text-emerald-400">{k}:</strong> {v}
                        </span>
                      ))}
                    </div>
                  </div>
                  <div className="text-right">
                    <span className="text-[11px] text-green-700 block uppercase">Sharpe Ratio</span>
                    <span className="text-xl font-bold font-mono text-emerald-400">
                      {Number((results.best_metrics as any)?.sharpe || 0).toFixed(2)}
                    </span>
                  </div>
                </div>
              </Card>

              {/* 2D Heatmap */}
              <Card>
                <CardHeader
                  title="Sharpe Ratio Response Surface"
                  subtitle={`2D parameter heatmap across ${param1Name} vs ${param2Name}`}
                />
                <ParamHeatmap
                  xAxisLabel={param1Name}
                  xAxisValues={heatmapData.xVals}
                  yAxisLabel={param2Name}
                  yAxisValues={heatmapData.yVals}
                  cellValues={heatmapData.cellMatrix}
                  bestX={heatmapData.bestX}
                  bestY={heatmapData.bestY}
                  higherIsBetter={true}
                />
              </Card>

              {/* Candidates Table */}
              <Card>
                <CardHeader
                  title="Top Evaluated Combinations"
                  subtitle="Sorted by risk-adjusted return (Sharpe Ratio)"
                />
                <div className="table-container max-h-80 overflow-y-auto">
                  <table className="table">
                    <thead>
                      <tr>
                        <th>Rank</th>
                        <th>Parameters</th>
                        <th>Sharpe</th>
                        <th>Max Drawdown</th>
                        <th>Win Rate</th>
                        <th>Total Return</th>
                      </tr>
                    </thead>
                    <tbody>
                      {results.grid
                        .filter((g) => g.metrics && g.metrics.sharpe !== undefined)
                        .sort((a, b) => (b.metrics?.sharpe || 0) - (a.metrics?.sharpe || 0))
                        .slice(0, 15)
                        .map((cand, idx) => (
                          <tr key={idx}>
                            <td className="font-mono text-green-800">#{idx + 1}</td>
                            <td className="font-mono text-xs text-green-400">
                              {Object.entries(cand.params)
                                .map(([k, v]) => `${k}=${v}`)
                                .join(', ')}
                            </td>
                            <td className="font-mono font-bold text-emerald-400">
                              {cand.metrics?.sharpe?.toFixed(2) || '—'}
                            </td>
                            <td className="font-mono text-xs text-green-600">
                              {cand.metrics?.max_drawdown_pct?.toFixed(1)}%
                            </td>
                            <td className="font-mono text-xs text-green-600">
                              {cand.metrics?.win_rate_pct?.toFixed(1)}%
                            </td>
                            <td className="font-mono text-xs font-semibold text-green-500">
                              {cand.metrics?.total_return_pct?.toFixed(2)}%
                            </td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                </div>
              </Card>
            </div>
          ) : (
            !activeRunId && (
              <Card className="p-12 text-center text-green-700">
                <Grid className="w-10 h-10 text-slate-600 mx-auto mb-3" />
                <h3 className="text-sm font-semibold text-green-400 mb-1">
                  No Optimization Results Selected
                </h3>
                <p className="text-xs text-green-800 max-w-sm mx-auto">
                  Configure search parameters and launch a sweep to visualize the 2D performance heatmap.
                </p>
              </Card>
            )
          )}
        </div>
      </div>
    </div>
  );
}
