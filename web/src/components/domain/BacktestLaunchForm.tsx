import React, { useState } from 'react';
import { api, apiErrorMessage } from '@/api/client';
import { HelpTooltip } from '@/components/ui/HelpTooltip';
import { Play, Calendar, AlertCircle } from 'lucide-react';
import { useT } from '@/i18n';

interface BacktestLaunchFormProps {
  strategyId: number;
  initialSymbol?: string;
  onLaunched: (runId: number) => void;
}

// Launches a historical simulation for ONE already-known strategy - the
// strategy picker that used to live here (back when this was the only way
// to launch a backtest, from the standalone /backtest page) is gone on
// purpose: this form only ever renders inside that strategy's own
// Workbench Backtest tab now, so the strategy is already fixed by the
// route you're on. The standalone /backtest page (BacktestRuns.tsx) is a
// pure cross-strategy run history/browser - it links here rather than
// re-implementing this form.
export function BacktestLaunchForm({ strategyId, initialSymbol, onLaunched }: BacktestLaunchFormProps) {
  const t = useT();
  const [symbol, setSymbol] = useState(initialSymbol || 'BTCUSDT');
  const [timeframe, setTimeframe] = useState('1h');
  const [initialCapital, setInitialCapital] = useState(10000);

  const [startDate, setStartDate] = useState(() => {
    const d = new Date();
    d.setMonth(d.getMonth() - 3);
    return d.toISOString().split('T')[0];
  });
  const [endDate, setEndDate] = useState(() => new Date().toISOString().split('T')[0]);

  const [slippagePct, setSlippagePct] = useState(0.05);
  const [feePct, setFeePct] = useState(0.075);

  const [isWalkForward, setIsWalkForward] = useState(false);
  const [trainMonths, setTrainMonths] = useState(3);
  const [testMonths, setTestMonths] = useState(1);
  const [stepMonths, setStepMonths] = useState(1);

  const [launching, setLaunching] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
      const fill_policy = { slippage_pct: Number(slippagePct), fee_pct: Number(feePct) };

      const res = isWalkForward
        ? await api.runWalkForward({
            strategy_id: strategyId,
            symbol,
            timeframe,
            start_date: startISO,
            end_date: endISO,
            initial_capital: Number(initialCapital),
            train_months: Number(trainMonths),
            test_months: Number(testMonths),
            step_months: Number(stepMonths),
            fill_policy,
          })
        : await api.runBacktest({
            strategy_id: strategyId,
            symbol,
            timeframe,
            start_date: startISO,
            end_date: endISO,
            initial_capital: Number(initialCapital),
            fill_policy,
          });
      onLaunched(res.id);
    } catch (err: any) {
      setError(apiErrorMessage(err, t('backtest.launchFailed')));
    } finally {
      setLaunching(false);
    }
  };

  return (
    <form onSubmit={handleLaunch} className="space-y-4 text-xs">
      {error && (
        <div className="p-3 bg-destructive/15 border border-destructive/40 text-destructive rounded text-xs flex items-center gap-2">
          <AlertCircle className="w-4 h-4 flex-shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="form-group mb-0">
          <label className="form-label">{t('backtest.assetPair')}</label>
          <input
            type="text"
            required
            value={symbol}
            onChange={(e) => setSymbol(e.target.value.toUpperCase())}
            placeholder="BTCUSDT"
            className="form-input text-xs font-mono"
          />
        </div>
        <div className="form-group mb-0">
          <label className="form-label flex items-center gap-1.5">
            {t('backtest.timeframe')}
            <HelpTooltip>{t('backtest.timeframeHelp')}</HelpTooltip>
          </label>
          <select value={timeframe} onChange={(e) => setTimeframe(e.target.value)} className="form-select text-xs">
            <option value="1m">{t('backtest.tf1m')}</option>
            <option value="5m">{t('backtest.tf5m')}</option>
            <option value="15m">{t('backtest.tf15m')}</option>
            <option value="1h">{t('backtest.tf1h')}</option>
            <option value="4h">{t('backtest.tf4h')}</option>
            <option value="1d">{t('backtest.tf1d')}</option>
          </select>
        </div>
      </div>

      <div className="form-group mb-0">
        <label className="form-label flex items-center gap-1.5">
          {t('backtest.initialCapital')}
          <HelpTooltip>{t('backtest.initialCapitalHelp')}</HelpTooltip>
        </label>
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

      <div className="p-3 bg-background/50 rounded-lg border border-border/30 space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-xs font-semibold text-muted-foreground flex items-center gap-1.5">
            <Calendar className="w-3.5 h-3.5 text-foreground" />
            <span>{t('backtest.timeRange')}</span>
          </span>
          <div className="flex items-center gap-1.5">
            {[1, 3, 6, 12].map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => handlePreset(m)}
                className="px-2 py-0.5 bg-secondary hover:bg-accent rounded text-[10px] text-muted-foreground font-medium"
              >
                {m === 12 ? t('backtest.presetYear') : t('backtest.presetMonths', { count: m })}
              </button>
            ))}
          </div>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="form-label text-[11px]">{t('backtest.startDate')}</label>
            <input type="date" required value={startDate} onChange={(e) => setStartDate(e.target.value)} className="form-input text-xs" />
          </div>
          <div>
            <label className="form-label text-[11px]">{t('backtest.endDate')}</label>
            <input type="date" required value={endDate} onChange={(e) => setEndDate(e.target.value)} className="form-input text-xs" />
          </div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="form-group mb-0">
          <label className="form-label flex items-center gap-1.5">
            {t('backtest.slippage')}
            <HelpTooltip>{t('backtest.slippageHelp')}</HelpTooltip>
          </label>
          <input type="number" step="0.01" min="0" value={slippagePct} onChange={(e) => setSlippagePct(Number(e.target.value))} className="form-input text-xs font-mono" />
        </div>
        <div className="form-group mb-0">
          <label className="form-label flex items-center gap-1.5">
            {t('backtest.feeRate')}
            <HelpTooltip>{t('backtest.feeRateHelp')}</HelpTooltip>
          </label>
          <input type="number" step="0.005" min="0" value={feePct} onChange={(e) => setFeePct(Number(e.target.value))} className="form-input text-xs font-mono" />
        </div>
      </div>

      <div className="p-3 bg-background/50 rounded-lg border border-border/30 space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <span className="text-xs font-semibold text-foreground flex items-center gap-1.5">
              {t('backtest.walkForwardTitle')}
              <HelpTooltip>{t('backtest.walkForwardHelp')}</HelpTooltip>
            </span>
            <p className="text-[11px] text-muted-foreground">{t('backtest.walkForwardSubtitle')}</p>
          </div>
          <input
            type="checkbox"
            checked={isWalkForward}
            onChange={(e) => setIsWalkForward(e.target.checked)}
            className="w-4 h-4 rounded text-muted-foreground focus:ring-ring bg-background border-border"
          />
        </div>
        {isWalkForward && (
          <div className="grid grid-cols-3 gap-3 pt-2 border-t border-border/30">
            <div>
              <label className="form-label text-[10px]">{t('backtest.trainMonths')}</label>
              <input type="number" min="1" value={trainMonths} onChange={(e) => setTrainMonths(Number(e.target.value))} className="form-input text-xs font-mono" />
            </div>
            <div>
              <label className="form-label text-[10px]">{t('backtest.testMonths')}</label>
              <input type="number" min="1" value={testMonths} onChange={(e) => setTestMonths(Number(e.target.value))} className="form-input text-xs font-mono" />
            </div>
            <div>
              <label className="form-label text-[10px]">{t('backtest.stepMonths')}</label>
              <input type="number" min="1" value={stepMonths} onChange={(e) => setStepMonths(Number(e.target.value))} className="form-input text-xs font-mono" />
            </div>
          </div>
        )}
      </div>

      <button
        type="submit"
        disabled={launching}
        className="w-full bg-primary hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed text-primary-foreground rounded border border-primary py-2.5 flex items-center justify-center gap-2 font-semibold text-xs"
      >
        <Play className="w-4 h-4" />
        <span>{launching ? t('backtest.launching') : t('backtest.execute')}</span>
      </button>
    </form>
  );
}
