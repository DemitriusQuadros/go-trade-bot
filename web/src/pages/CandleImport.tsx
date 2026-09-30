import React, { useState } from 'react';
import {
  Download,
  Clock,
  Calendar,
  Layers,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Plus,
  Trash2,
  Info,
  RotateCw,
  Loader2,
  Play,
} from 'lucide-react';
import {
  useStartCandleImport,
  useCandleImportJob,
  useImportSchedules,
  useCreateImportSchedule,
  usePatchImportSchedule,
  useDeleteImportSchedule,
} from '@/hooks/queries';
import { useAuth } from '@/context/AuthContext';
import { CandleImportRequest, CandleImportSource, ImportSchedule } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { HelpTooltip } from '@/components/ui/HelpTooltip';
import { formatDateTime, formatNumber } from '@/lib/format';
import { tr, type MessageKey } from '@/i18n';
import { useT } from '@/i18n';

const TIMEFRAME_OPTIONS = [
  '1m', '3m', '5m', '15m', '30m',
  '1h', '2h', '4h', '6h', '8h', '12h',
  '1d', '3d', '1w',
];

function toISODate(d: Date): string {
  return d.toISOString().split('T')[0];
}

function getDefaultDates() {
  const to = new Date();
  const from = new Date();
  from.setMonth(from.getMonth() - 2);
  return { from: toISODate(from), to: toISODate(to) };
}

function describeCronSpec(cron?: string | null): string {
  if (!cron || typeof cron !== 'string' || !cron.trim()) {
    return '—';
  }
  const parts = cron.trim().split(/\s+/);
  if (parts.length === 5) {
    const [min, hour, dom, mon, dow] = parts;
    if (min === '0' && dom === '*' && mon === '*') {
      const hh = hour.padStart(2, '0');
      if (dow === '*') {
        return tr('candles.dailyAt', { hh });
      }
      const dayNum = parseInt(dow, 10);
      if (!isNaN(dayNum) && dayNum >= 0 && dayNum <= 6) {
        return tr('candles.weeklyOn', { day: tr(`cron.dow.d${dayNum}` as MessageKey), hh });
      }
    }
  }
  return cron;
}

export function CandleImport() {
  const t = useT();
  const defaultDates = getDefaultDates();

  // One-off Import Form State
  const [symbols, setSymbols] = useState<string[]>(['BTCUSDT', 'ETHUSDT']);
  const [symbolInput, setSymbolInput] = useState('');
  const [selectedTimeframes, setSelectedTimeframes] = useState<string[]>(['1h', '1d']);
  const [fromDate, setFromDate] = useState(defaultDates.from);
  const [toDate, setToDate] = useState(defaultDates.to);
  const [source, setSource] = useState<CandleImportSource>('rest');
  const [activeJobId, setActiveJobId] = useState<string | null>(null);
  const [importError, setImportError] = useState<string | null>(null);
  // auth-02 §4: one-off imports need `backtest`; schedules are admin only.
  const { can } = useAuth();
  const canImport = can('backtest');
  const canSchedule = can('admin');

  // Mutations
  const startImportMutation = useStartCandleImport();
  const { data: jobData, isFetching: isJobFetching } = useCandleImportJob(activeJobId);

  // Scheduled Imports State
  const { data: schedules = [], isLoading: isSchedulesLoading } = useImportSchedules();
  const createScheduleMutation = useCreateImportSchedule();
  const patchScheduleMutation = usePatchImportSchedule(0);
  const deleteScheduleMutation = useDeleteImportSchedule();

  const [showScheduleForm, setShowScheduleForm] = useState(false);
  const [schedSymbol, setSchedSymbol] = useState('BTCUSDT');
  const [schedTimeframe, setSchedTimeframe] = useState('1h');
  const [schedFreq, setSchedFreq] = useState<'daily' | 'weekly'>('daily');
  const [schedHour, setSchedHour] = useState(1);
  const [schedDow, setSchedDow] = useState(1); // Monday
  const [recentlyChangedIds, setRecentlyChangedIds] = useState<Set<number>>(new Set());
  const [showCreatedNotice, setShowCreatedNotice] = useState(false);

  // Delete confirm dialog
  const [deleteTarget, setDeleteTarget] = useState<ImportSchedule | null>(null);

  // One-off Import Handlers
  const handleAddSymbol = () => {
    const s = symbolInput.trim().toUpperCase();
    if (s && !symbols.includes(s)) {
      setSymbols([...symbols, s]);
      setSymbolInput('');
    }
  };

  const handleRemoveSymbol = (sym: string) => {
    setSymbols(symbols.filter((s) => s !== sym));
  };

  const handleToggleTimeframe = (tf: string) => {
    if (selectedTimeframes.includes(tf)) {
      setSelectedTimeframes(selectedTimeframes.filter((t) => t !== tf));
    } else {
      setSelectedTimeframes([...selectedTimeframes, tf]);
    }
  };

  const handleStartImport = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canImport) return;
    setImportError(null);

    if (symbols.length === 0) {
      setImportError(t('candles.errSymbol'));
      return;
    }
    if (selectedTimeframes.length === 0) {
      setImportError(t('candles.errTimeframe'));
      return;
    }
    if (new Date(fromDate) > new Date(toDate)) {
      setImportError(t('candles.errDates'));
      return;
    }

    const req: CandleImportRequest = {
      symbols,
      timeframes: selectedTimeframes,
      from: new Date(`${fromDate}T00:00:00Z`).toISOString(),
      to: new Date(`${toDate}T23:59:59Z`).toISOString(),
      source,
    };

    try {
      const res = await startImportMutation.mutateAsync(req);
      setActiveJobId(res.job_id);
    } catch (err: any) {
      setImportError(err.message || t('candles.errStart'));
    }
  };

  // Schedule Handlers
  const handleCreateSchedule = async (e: React.FormEvent) => {
    e.preventDefault();
    const cron_spec =
      schedFreq === 'daily'
        ? `0 ${schedHour} * * *`
        : `0 ${schedHour} * * ${schedDow}`;

    try {
      await createScheduleMutation.mutateAsync({
        symbol: schedSymbol.trim().toUpperCase(),
        timeframe: schedTimeframe,
        cron_spec,
      });
      setShowScheduleForm(false);
      setShowCreatedNotice(true);
    } catch (err: any) {
      alert(err.message || t('candles.errCreateSchedule'));
    }
  };

  const handleToggleSchedule = async (sched: ImportSchedule) => {
    try {
      await patchScheduleMutation.mutateAsync({ enabled: !sched.enabled });
      setRecentlyChangedIds((prev) => new Set(prev).add(sched.id));
    } catch (err: any) {
      alert(err.message || t('candles.errUpdateSchedule'));
    }
  };

  const handleConfirmDeleteSchedule = async () => {
    if (!deleteTarget) return;
    try {
      await deleteScheduleMutation.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch (err: any) {
      alert(err.message || t('candles.errDeleteSchedule'));
    }
  };

  return (
    <div className="max-w-5xl mx-auto space-y-6 pb-20">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
          {t('candles.title')}
        </h1>
        <p className="text-xs text-muted-foreground mt-1">
          {t('candles.subtitle')}
        </p>
      </div>

      {/* 1. New One-Off Import Card */}
      <Card>
        <CardHeader
          title={t('candles.manualTitle')}
          subtitle={t('candles.manualSubtitle')}
        />
        <form onSubmit={handleStartImport} className="p-6 pt-0 space-y-5">
          {importError && (
            <div className="p-3 rounded-lg bg-destructive/15 border border-destructive/40 text-xs text-destructive flex items-center gap-2">
              <AlertCircle className="w-4 h-4 shrink-0" />
              <span>{importError}</span>
            </div>
          )}

          {/* Symbols tag input */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
              {t('candles.symbols')} <span className="text-destructive">*</span>
              <HelpTooltip>{t('candles.symbolsHelp')}</HelpTooltip>
            </label>
            <div className="flex items-center gap-2">
              <input
                type="text"
                value={symbolInput}
                onChange={(e) => setSymbolInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddSymbol();
                  }
                }}
                placeholder={t('candles.symbolPlaceholder')}
                className="flex-1 px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
              />
              <button
                type="button"
                onClick={handleAddSymbol}
                className="px-3.5 py-2 rounded-lg bg-secondary hover:bg-secondary text-xs font-semibold text-foreground"
              >
                {t('common.add')}
              </button>
            </div>
            <div className="flex flex-wrap gap-1.5 pt-1">
              {symbols.map((sym) => (
                <span
                  key={sym}
                  className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-card/60 border border-border text-xs font-medium text-foreground"
                >
                  {sym}
                  <button
                    type="button"
                    onClick={() => handleRemoveSymbol(sym)}
                    aria-label={t('candles.removeSymbol', { symbol: sym })}
                    className="hover:text-destructive p-0.5"
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          </div>

          {/* Timeframes multi-select */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
              {t('candles.timeframes')} <span className="text-destructive">*</span>
              <HelpTooltip>{t('candles.timeframesHelp')}</HelpTooltip>
            </label>
            <div className="flex flex-wrap gap-3">
              {TIMEFRAME_OPTIONS.map((tf) => {
                const checked = selectedTimeframes.includes(tf);
                return (
                  <label
                    key={tf}
                    className={`flex items-center gap-2 px-3 py-1.5 rounded-lg border text-xs font-medium cursor-pointer transition-colors ${
                      checked
                        ? 'bg-card/70 border-border text-foreground'
                        : 'bg-background border-border text-muted-foreground hover:border-border'
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => handleToggleTimeframe(tf)}
                      className="rounded border-border text-muted-foreground focus:ring-ring"
                    />
                    <span>{tf}</span>
                  </label>
                );
              })}
            </div>
          </div>

          {/* Source: REST (live API, correct for incremental/recent sync)
              vs Archive (Binance's public data.binance.vision monthly
              dumps - no rate limits, the right choice for a deep one-time
              historical backfill). See entities.ImportSource's doc comment
              on the backend for the full rationale. */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
              {t('candles.source')}
              <HelpTooltip>{t('candles.sourceHelp')}</HelpTooltip>
            </label>
            <div className="flex gap-2">
              {(['rest', 'archive'] as CandleImportSource[]).map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => setSource(s)}
                  className={`px-3 py-1.5 rounded-lg border text-xs font-medium transition-colors ${
                    source === s
                      ? 'bg-card/70 border-border text-foreground'
                      : 'bg-background border-border text-muted-foreground hover:border-border'
                  }`}
                >
                  {s === 'rest' ? t('candles.sourceRest') : t('candles.sourceArchive')}
                </button>
              ))}
            </div>
          </div>

          {/* Date range inputs */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-semibold text-foreground">{t('candles.fromDate')}</label>
              <input
                type="date"
                required
                value={fromDate}
                onChange={(e) => setFromDate(e.target.value)}
                className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-semibold text-foreground">{t('candles.toDate')}</label>
              <input
                type="date"
                required
                value={toDate}
                onChange={(e) => setToDate(e.target.value)}
                className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
              />
            </div>
          </div>
          {source === 'archive' && (
            <p className="text-[11px] text-muted-foreground -mt-2">
              {t('candles.archiveNote')}
            </p>
          )}

          <div className="flex justify-end pt-2">
            {!canImport ? (
              <p className="text-xs text-muted-foreground" data-testid="candle-import-no-permission">
                {t('candles.noPermission')}
              </p>
            ) : (
            <button
              type="submit"
              disabled={startImportMutation.isPending}
              className="px-5 py-2.5 rounded-lg bg-primary hover:bg-primary text-xs font-semibold text-white shadow-lg flex items-center gap-2 transition-colors disabled:opacity-50"
            >
              {startImportMutation.isPending ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Download className="w-4 h-4" />
              )}
              <span>{t('candles.start')}</span>
            </button>
            )}
          </div>
        </form>
      </Card>

      {/* 2. Import Progress Card */}
      {activeJobId && (
        <Card>
          <CardHeader
            title={t('candles.progressTitle')}
            subtitle={t('candles.jobId', { id: activeJobId })}
          />
          <div className="p-6 pt-0 space-y-4">
            {(!jobData || jobData.status === 'pending' || jobData.status === 'running') && (
              <div className="p-6 rounded-xl bg-background/70 border border-border flex flex-col items-center justify-center gap-3 text-center" aria-busy="true">
                <Loader2 className="w-8 h-8 text-foreground animate-spin" />
                <div>
                  <h4 className="text-sm font-semibold text-foreground">{t('candles.importing')}</h4>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {t('candles.importingHelp')}
                  </p>
                </div>
              </div>
            )}

            {jobData?.status === 'completed' && jobData.result && (
              <div className="space-y-4">
                <div className="p-4 rounded-xl bg-success/15 border border-success/40 flex items-center gap-2.5 text-xs text-success">
                  <CheckCircle2 className="w-4 h-4 text-success shrink-0" />
                  <span>
                    {t('candles.complete', {
                      ok: jobData.result.per_pair.filter((p) => !p.error).length,
                      total: jobData.result.per_pair.length,
                    })}
                  </span>
                </div>

                {/* `.table-container`/`.table` were dead classes (see
                    BacktestPane's trade log fix for the full story) - real
                    Tailwind now via [&_th]/[&_td] arbitrary variants. */}
                <div className="overflow-x-auto rounded border border-border">
                  <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-muted-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_td]:whitespace-nowrap [&_tbody_tr]:border-t [&_tbody_tr]:border-border [&_thead]:bg-card/60">
                    <thead>
                      <tr>
                        <th>{t('candles.colSymbol')}</th>
                        <th>{t('candles.colTimeframe')}</th>
                        <th>{t('candles.colImported')}</th>
                        <th>{t('candles.colGaps')}</th>
                        <th>{t('candles.colStatus')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {jobData.result.per_pair.map((pair, idx) => (
                        <tr key={idx}>
                          <td className="font-semibold text-foreground">{pair.symbol}</td>
                          <td className="font-mono text-xs text-foreground">{pair.timeframe}</td>
                          <td className="font-mono text-xs text-success">
                            {formatNumber(pair.candles_imported, { digits: 0 })}
                          </td>
                          <td className="font-mono text-xs text-warning">{pair.gaps_detected}</td>
                          <td>
                            {pair.error ? (
                              <span className="text-xs text-destructive flex items-center gap-1">
                                <XCircle className="w-3.5 h-3.5 shrink-0" />
                                {pair.error}
                              </span>
                            ) : (
                              <span className="text-xs text-success flex items-center gap-1">
                                <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
                                {t('candles.success')}
                              </span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {jobData?.status === 'failed' && (
              <div className="p-4 rounded-xl bg-destructive/15 border border-destructive/40 space-y-3 text-xs text-destructive">
                <div className="flex items-center gap-2 font-semibold text-destructive">
                  <AlertCircle className="w-4 h-4" /> {t('candles.failed')}
                </div>
                <p>{jobData.error || t('candles.failedDefault')}</p>
                <button
                  type="button"
                  onClick={handleStartImport}
                  className="px-3 py-1.5 rounded bg-destructive/15 hover:bg-destructive/15 text-xs font-semibold text-white flex items-center gap-1.5"
                >
                  <RotateCw className="w-3.5 h-3.5" /> {t('candles.retry')}
                </button>
              </div>
            )}
          </div>
        </Card>
      )}

      {/* 3. Scheduled Recurring Imports Card */}
      <Card>
        <div className="p-6 pb-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-bold text-foreground">{t('candles.schedulesTitle')}</h3>
            <p className="text-xs text-muted-foreground">
              {t('candles.schedulesSubtitle')}
            </p>
          </div>
          {canSchedule && (
          <button
            type="button"
            onClick={() => setShowScheduleForm(!showScheduleForm)}
            className="px-3 py-1.5 rounded-lg bg-primary hover:bg-primary text-xs font-semibold text-white shadow flex items-center gap-1.5 shrink-0"
          >
            <Plus className="w-3.5 h-3.5" />
            <span>{t('candles.newSchedule')}</span>
          </button>
          )}
        </div>

        <div className="p-6 pt-0 space-y-4">
          {showCreatedNotice && (
            <div className="p-3 rounded-lg bg-warning/15 border border-warning/40 text-xs text-warning flex items-center justify-between">
              <ScheduleRestartNotice />
              <button
                type="button"
                onClick={() => setShowCreatedNotice(false)}
                aria-label={t('candles.dismiss')}
                className="hover:text-white p-0.5"
              >
                ×
              </button>
            </div>
          )}

          {/* New Schedule Inline Form */}
          {showScheduleForm && canSchedule && (
            <form onSubmit={handleCreateSchedule} className="p-4 rounded-xl bg-background/70 border border-border space-y-4">
              <h4 className="text-xs font-bold text-foreground uppercase tracking-wider">
                {t('candles.createTitle')}
              </h4>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-muted-foreground uppercase font-semibold">{t('candles.colSymbol')}</label>
                  <input
                    type="text"
                    required
                    value={schedSymbol}
                    onChange={(e) => setSchedSymbol(e.target.value)}
                    placeholder="BTCUSDT"
                    className="px-2.5 py-1.5 rounded bg-card border border-border text-xs text-foreground"
                  />
                </div>

                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-muted-foreground uppercase font-semibold">{t('candles.colTimeframe')}</label>
                  <select
                    value={schedTimeframe}
                    onChange={(e) => setSchedTimeframe(e.target.value)}
                    className="px-2.5 py-1.5 rounded bg-card border border-border text-xs text-foreground"
                  >
                    {TIMEFRAME_OPTIONS.map((tf) => (
                      <option key={tf} value={tf}>
                        {tf}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-muted-foreground uppercase font-semibold">{t('candles.frequency')}</label>
                  <select
                    value={schedFreq}
                    onChange={(e) => setSchedFreq(e.target.value as 'daily' | 'weekly')}
                    className="px-2.5 py-1.5 rounded bg-card border border-border text-xs text-foreground"
                  >
                    <option value="daily">{t('candles.daily')}</option>
                    <option value="weekly">{t('candles.weekly')}</option>
                  </select>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-muted-foreground uppercase font-semibold">
                    {t('candles.hourUtc')}
                  </label>
                  <select
                    value={schedHour}
                    onChange={(e) => setSchedHour(parseInt(e.target.value, 10))}
                    className="px-2.5 py-1.5 rounded bg-card border border-border text-xs text-foreground"
                  >
                    {Array.from({ length: 24 }).map((_, h) => (
                      <option key={h} value={h}>
                        {t('candles.hourOption', { hh: h.toString().padStart(2, '0') })}
                      </option>
                    ))}
                  </select>
                </div>

                {schedFreq === 'weekly' && (
                  <div className="flex flex-col gap-1">
                    <label className="text-[10px] text-muted-foreground uppercase font-semibold">
                      {t('candles.dayOfWeek')}
                    </label>
                    <select
                      value={schedDow}
                      onChange={(e) => setSchedDow(parseInt(e.target.value, 10))}
                      className="px-2.5 py-1.5 rounded bg-card border border-border text-xs text-foreground"
                    >
                      <option value={1}>{t('candles.day.d1')}</option>
                      <option value={2}>{t('candles.day.d2')}</option>
                      <option value={3}>{t('candles.day.d3')}</option>
                      <option value={4}>{t('candles.day.d4')}</option>
                      <option value={5}>{t('candles.day.d5')}</option>
                      <option value={6}>{t('candles.day.d6')}</option>
                      <option value={0}>{t('candles.day.d0')}</option>
                    </select>
                  </div>
                )}
              </div>

              <div className="flex justify-end gap-2 pt-1">
                <button
                  type="button"
                  onClick={() => setShowScheduleForm(false)}
                  className="px-3 py-1.5 rounded text-xs text-muted-foreground hover:text-foreground"
                >
                  {t('common.cancel')}
                </button>
                <button
                  type="submit"
                  disabled={createScheduleMutation.isPending}
                  className="px-4 py-1.5 rounded bg-primary hover:bg-primary text-xs font-semibold text-white"
                >
                  {t('candles.createSchedule')}
                </button>
              </div>
            </form>
          )}

          {/* Schedules Table */}
          {schedules.length === 0 ? (
            <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
              {t('candles.noSchedules')}
            </div>
          ) : (
            // `.table-container`/`.table` were dead classes (see
            // BacktestPane's trade log fix for the full story) - real
            // Tailwind now via [&_th]/[&_td] arbitrary variants.
            <div className="overflow-x-auto rounded border border-border">
              <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-muted-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_td]:whitespace-nowrap [&_tbody_tr]:border-t [&_tbody_tr]:border-border [&_thead]:bg-card/60">
                <thead>
                  <tr>
                    <th>{t('candles.colSymbol')}</th>
                    <th>{t('candles.colTimeframe')}</th>
                    <th>{t('candles.colSchedule')}</th>
                    <th>{t('candles.colEnabled')}</th>
                    <th>{t('candles.colLastRun')}</th>
                    <th>{t('candles.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {schedules.map((sched) => (
                    <React.Fragment key={sched.id}>
                      <tr>
                        <td className="font-semibold text-foreground">{sched.symbol}</td>
                        <td className="font-mono text-xs text-foreground">{sched.timeframe}</td>
                        <td className="text-xs text-foreground">
                          {describeCronSpec(sched.cron_spec)}
                        </td>
                        <td>
                          <label
                            className={`relative inline-flex items-center ${canSchedule ? 'cursor-pointer' : 'cursor-not-allowed opacity-60'}`}
                            title={canSchedule ? undefined : t('users.adminOnly')}
                          >
                            <input
                              type="checkbox"
                              checked={sched.enabled}
                              onChange={() => handleToggleSchedule(sched)}
                              disabled={!canSchedule}
                              aria-label={t('candles.enabledAria', { symbol: sched.symbol, timeframe: sched.timeframe })}
                              className="sr-only peer"
                            />
                            <div className="w-9 h-5 bg-secondary peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-border after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-primary"></div>
                          </label>
                        </td>
                        <td className="text-xs font-mono text-muted-foreground">
                          {sched.last_run_at
                            ? formatDateTime(sched.last_run_at, { seconds: true })
                            : t('common.never')}
                        </td>
                        <td>
                          {canSchedule && (
                          <button
                            type="button"
                            onClick={() => setDeleteTarget(sched)}
                            className="p-1 text-muted-foreground hover:text-destructive rounded hover:bg-destructive/15"
                            title={t('candles.deleteTitle')}
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                          )}
                        </td>
                      </tr>
                      {recentlyChangedIds.has(sched.id) && (
                        <tr>
                          <td colSpan={6} className="bg-warning/15 py-1.5 px-3">
                            <ScheduleRestartNotice />
                          </td>
                        </tr>
                      )}
                    </React.Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </Card>

      {/* Delete Schedule Confirmation */}
      <ConfirmDialog
        isOpen={deleteTarget !== null}
        title={t('candles.deleteDialogTitle')}
        message={t('candles.deleteMessage')}
        isDangerous={true}
        confirmText={t('candles.deleteConfirm')}
        onConfirm={handleConfirmDeleteSchedule}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}

function ScheduleRestartNotice() {
  const t = useT();
  return (
    <p className="text-xs text-warning flex items-center gap-1.5">
      <Info className="w-3.5 h-3.5 shrink-0" />
      {t('candles.restartNotice')}
    </p>
  );
}
