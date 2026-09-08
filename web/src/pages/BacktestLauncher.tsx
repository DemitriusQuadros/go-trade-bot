import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { api } from '@/api/client';
import { useStrategies, useBacktests } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen } from '@/components/ui/Spinner';
import {
  Play,
  Calendar,
  DollarSign,
  Clock,
  Layers,
  Sliders,
  History,
  AlertCircle,
  ExternalLink,
} from 'lucide-react';

export function BacktestLauncher() {
  const navigate = useNavigate();

  
  

  
  

  const { data: strategies = [] } = useStrategies();
  const { data: recentRuns = [], refetch: refetchRecentRuns } = useBacktests();
  // Form State
  const [selectedStrategyId, setSelectedStrategyId] = useState<number>(strategies[0]?.id || 1);
  const [symbol, setSymbol] = useState('BTCUSDT');
  const [timeframe, setTimeframe] = useState('1h');
  const [initialCapital, setInitialCapital] = useState(10000);

  // Dates (defaults: last 3 months)
  const [startDate, setStartDate] = useState(() => {
    const d = new Date();
    d.setMonth(d.getMonth() - 3);
    return d.toISOString().split('T')[0];
  });
  const [endDate, setEndDate] = useState(() => new Date().toISOString().split('T')[0]);

  // Fill Policy
  const [slippagePct, setSlippagePct] = useState(0.05);
  const [feePct, setFeePct] = useState(0.075);

  // Walk-forward settings
  const [isWalkForward, setIsWalkForward] = useState(false);
  const [trainMonths, setTrainMonths] = useState(3);
  const [testMonths, setTestMonths] = useState(1);
  const [stepMonths, setStepMonths] = useState(1);

  const [launching, setLaunching] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleStrategyChange = (stratId: number) => {
    setSelectedStrategyId(stratId);
    const strat = strategies.find((s: any) => s.id === stratId);
    if (strat && strat.monitored_symbols?.length > 0) {
      setSymbol(strat.monitored_symbols[0]);
    }
  };

  const handlePreset = (months: number) => {
    const end = new Date();
    const start = new Date();
    start.setMonth(start.getMonth() - months);
    setStartDate(start.toISOString().split('T')[0]);
    setEndDate(end.toISOString().split('T')[0]);
  };

  const handleLaunch = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLaunching(true);

    try {
      const startISO = new Date(startDate).toISOString();
      const endISO = new Date(endDate).toISOString();

      if (isWalkForward) {
        const res = await api.runWalkForward({
          strategy_id: Number(selectedStrategyId),
          symbol,
          timeframe,
          start_date: startISO,
          end_date: endISO,
          initial_capital: Number(initialCapital),
          train_months: Number(trainMonths),
          test_months: Number(testMonths),
          step_months: Number(stepMonths),
          fill_policy: {
            slippage_pct: Number(slippagePct),
            fee_pct: Number(feePct),
          },
        });
        navigate(`/backtest/${res.id}`);
      } else {
        const res = await api.runBacktest({
          strategy_id: Number(selectedStrategyId),
          symbol,
          timeframe,
          start_date: startISO,
          end_date: endISO,
          initial_capital: Number(initialCapital),
          fill_policy: {
            slippage_pct: Number(slippagePct),
            fee_pct: Number(feePct),
          },
        });
        navigate(`/backtest/${res.id}`);
      }
    } catch (err: any) {
      setError(err.message || 'Failed to execute backtest simulation');
    } finally {
      setLaunching(false);
    }
  };

  return (
    <div className="container-custom space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-green-500 flex items-center gap-2">
          Backtest & Walk-Forward Engine
        </h1>
        <p className="text-xs text-green-700 mt-0.5">
          Execute high-precision historical simulations, evaluate drawdown resilience & walk-forward stability
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Launcher Form (2 cols) */}
        <div className="lg:col-span-2">
          <Card>
            <CardHeader
              title="Launch Historical Simulation"
              subtitle="Configure strategy parameters, exchange execution slippage & test range"
            />

            {error && (
              <div className="mb-4 p-3 bg-red-950/60 border border-red-800 text-red-300 rounded text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 flex-shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <form onSubmit={handleLaunch} className="space-y-4 text-xs">
              {/* Strategy & Symbol */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="form-group mb-0">
                  <label className="form-label">Select Strategy</label>
                  <select
                    value={selectedStrategyId}
                    onChange={(e) => handleStrategyChange(Number(e.target.value))}
                    className="form-select text-xs"
                  >
                    {strategies.map((s: any) => (
                      <option key={s.id} value={s.id}>
                        #{s.id} — {s.name} ({s.strategy_name})
                      </option>
                    ))}
                  </select>
                </div>

                <div className="form-group mb-0">
                  <label className="form-label">Asset Pair (Symbol)</label>
                  <input
                    type="text"
                    required
                    value={symbol}
                    onChange={(e) => setSymbol(e.target.value.toUpperCase())}
                    placeholder="BTCUSDT"
                    className="form-input text-xs font-mono"
                  />
                </div>
              </div>

              {/* Timeframe & Capital */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="form-group mb-0">
                  <label className="form-label">Candle Timeframe</label>
                  <select
                    value={timeframe}
                    onChange={(e) => setTimeframe(e.target.value)}
                    className="form-select text-xs"
                  >
                    <option value="1m">1 Minute (1m)</option>
                    <option value="5m">5 Minutes (5m)</option>
                    <option value="15m">15 Minutes (15m)</option>
                    <option value="1h">1 Hour (1h)</option>
                    <option value="4h">4 Hours (4h)</option>
                    <option value="1d">1 Day (1d)</option>
                  </select>
                </div>

                <div className="form-group mb-0">
                  <label className="form-label">Initial Capital (USD)</label>
                  <input
                    type="number"
                    min="100"
                    step="100"
                    required
                    value={initialCapital}
                    onChange={(e) => setInitialCapital(Number(e.target.value))}
                    className="form-input text-xs font-mono"
                  />
                </div>
              </div>

              {/* Date Presets & Inputs */}
              <div className="p-3 bg-black/50 rounded-lg border border-green-900/30 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-green-600 flex items-center gap-1.5">
                    <Calendar className="w-3.5 h-3.5 text-green-500" />
                    <span>Historical Time Range</span>
                  </span>

                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => handlePreset(1)}
                      className="px-2 py-0.5 bg-slate-800 hover:bg-slate-700 rounded text-[10px] text-green-600 font-medium"
                    >
                      1M
                    </button>
                    <button
                      type="button"
                      onClick={() => handlePreset(3)}
                      className="px-2 py-0.5 bg-slate-800 hover:bg-slate-700 rounded text-[10px] text-green-600 font-medium"
                    >
                      3M
                    </button>
                    <button
                      type="button"
                      onClick={() => handlePreset(6)}
                      className="px-2 py-0.5 bg-slate-800 hover:bg-slate-700 rounded text-[10px] text-green-600 font-medium"
                    >
                      6M
                    </button>
                    <button
                      type="button"
                      onClick={() => handlePreset(12)}
                      className="px-2 py-0.5 bg-slate-800 hover:bg-slate-700 rounded text-[10px] text-green-600 font-medium"
                    >
                      1Y
                    </button>
                  </div>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <label className="form-label text-[11px]">Start Date</label>
                    <input
                      type="date"
                      required
                      value={startDate}
                      onChange={(e) => setStartDate(e.target.value)}
                      className="form-input text-xs"
                    />
                  </div>
                  <div>
                    <label className="form-label text-[11px]">End Date</label>
                    <input
                      type="date"
                      required
                      value={endDate}
                      onChange={(e) => setEndDate(e.target.value)}
                      className="form-input text-xs"
                    />
                  </div>
                </div>
              </div>

              {/* Slippage & Fees */}
              <div className="grid grid-cols-2 gap-3">
                <div className="form-group mb-0">
                  <label className="form-label">Slippage (%)</label>
                  <input
                    type="number"
                    step="0.01"
                    min="0"
                    value={slippagePct}
                    onChange={(e) => setSlippagePct(Number(e.target.value))}
                    className="form-input text-xs font-mono"
                  />
                </div>
                <div className="form-group mb-0">
                  <label className="form-label">Fee Rate (%)</label>
                  <input
                    type="number"
                    step="0.005"
                    min="0"
                    value={feePct}
                    onChange={(e) => setFeePct(Number(e.target.value))}
                    className="form-input text-xs font-mono"
                  />
                </div>
              </div>

              {/* Walk-forward toggle */}
              <div className="p-3 bg-black/50 rounded-lg border border-green-900/30 space-y-3">
                <div className="flex items-center justify-between">
                  <div>
                    <span className="text-xs font-semibold text-green-400">
                      Walk-Forward Out-of-Sample Validation
                    </span>
                    <p className="text-[11px] text-green-700">
                      Sequentially roll train/test windows to detect overfitting
                    </p>
                  </div>
                  <input
                    type="checkbox"
                    checked={isWalkForward}
                    onChange={(e) => setIsWalkForward(e.target.checked)}
                    className="w-4 h-4 rounded text-blue-600 focus:ring-blue-500 bg-black border-slate-700"
                  />
                </div>

                {isWalkForward && (
                  <div className="grid grid-cols-3 gap-3 pt-2 border-t border-green-900/30">
                    <div>
                      <label className="form-label text-[10px]">Train Months</label>
                      <input
                        type="number"
                        min="1"
                        value={trainMonths}
                        onChange={(e) => setTrainMonths(Number(e.target.value))}
                        className="form-input text-xs font-mono"
                      />
                    </div>
                    <div>
                      <label className="form-label text-[10px]">Test Months</label>
                      <input
                        type="number"
                        min="1"
                        value={testMonths}
                        onChange={(e) => setTestMonths(Number(e.target.value))}
                        className="form-input text-xs font-mono"
                      />
                    </div>
                    <div>
                      <label className="form-label text-[10px]">Step Months</label>
                      <input
                        type="number"
                        min="1"
                        value={stepMonths}
                        onChange={(e) => setStepMonths(Number(e.target.value))}
                        className="form-input text-xs font-mono"
                      />
                    </div>
                  </div>
                )}
              </div>

              {/* Submit button */}
              <button
                type="submit"
                disabled={launching}
                className="w-full btn btn-primary py-2.5 flex items-center justify-center gap-2 font-semibold text-xs"
              >
                <Play className="w-4 h-4" />
                <span>{launching ? 'Simulating Historical Ticks...' : 'Execute Backtest'}</span>
              </button>
            </form>
          </Card>
        </div>

        {/* Recent Runs History (1 col) */}
        <div>
          <Card>
            <CardHeader
              title="Recent Runs"
              subtitle="Recently completed simulations"
            />

            {(false) && !recentRuns ? (
              <LoadingScreen message="Loading history..." />
            ) : recentRuns.length === 0 ? (
              <div className="p-6 text-center text-xs text-green-800 bg-black/40 rounded-lg">
                No previous backtests found.
              </div>
            ) : (
              <div className="space-y-2.5">
                {recentRuns.slice(0, 8).map((run: any) => {
                  const runDate = new Date(run.created_at);
                  const isPositive = run.total_return_pct >= 0;

                  return (
                    <Link
                      key={run.id}
                      to={`/backtest/${run.id}`}
                      className="block p-3 bg-black/50 hover:bg-green-950/20 border border-green-900/30 rounded-lg transition-colors group"
                    >
                      <div className="flex items-center justify-between mb-1">
                        <span className="font-mono font-bold text-xs text-green-400 group-hover:text-green-500">
                          #{run.id} {run.symbol}
                        </span>
                        <span
                          className={`font-mono text-xs font-bold ${
                            isPositive ? 'text-emerald-400' : 'text-rose-400'
                          }`}
                        >
                          {isPositive ? '+' : ''}
                          {run.total_return_pct.toFixed(2)}%
                        </span>
                      </div>

                      <div className="flex items-center justify-between text-[11px] text-green-700">
                        <span>Sharpe: {run.sharpe.toFixed(2)}</span>
                        <span>MaxDD: {run.max_drawdown_pct.toFixed(1)}%</span>
                        <span>Trades: {run.total_trades}</span>
                      </div>
                    </Link>
                  );
                })}
              </div>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}
