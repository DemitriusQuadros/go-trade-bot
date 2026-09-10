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
import { CandleImportRequest, ImportSchedule } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { HelpTooltip } from '@/components/ui/HelpTooltip';

const TIMEFRAME_OPTIONS = ['1m', '5m', '15m', '1h', '4h', '1d'];

function toISODate(d: Date): string {
  return d.toISOString().split('T')[0];
}

function getDefaultDates() {
  const to = new Date();
  const from = new Date();
  from.setMonth(from.getMonth() - 2);
  return { from: toISODate(from), to: toISODate(to) };
}

function describeCronSpec(cron: string): string {
  const parts = cron.trim().split(/\s+/);
  if (parts.length === 5) {
    const [min, hour, dom, mon, dow] = parts;
    if (min === '0' && dom === '*' && mon === '*') {
      const hh = hour.padStart(2, '0');
      if (dow === '*') {
        return `Daily at ${hh}:00 UTC`;
      }
      const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
      const dayNum = parseInt(dow, 10);
      if (!isNaN(dayNum) && dayNum >= 0 && dayNum <= 6) {
        return `Weekly on ${days[dayNum]} at ${hh}:00 UTC`;
      }
    }
  }
  return cron;
}

export function CandleImport() {
  const defaultDates = getDefaultDates();

  // One-off Import Form State
  const [symbols, setSymbols] = useState<string[]>(['BTCUSDT', 'ETHUSDT']);
  const [symbolInput, setSymbolInput] = useState('');
  const [selectedTimeframes, setSelectedTimeframes] = useState<string[]>(['1h', '1d']);
  const [fromDate, setFromDate] = useState(defaultDates.from);
  const [toDate, setToDate] = useState(defaultDates.to);
  const [activeJobId, setActiveJobId] = useState<string | null>(null);
  const [importError, setImportError] = useState<string | null>(null);

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
    setImportError(null);

    if (symbols.length === 0) {
      setImportError('At least one symbol is required.');
      return;
    }
    if (selectedTimeframes.length === 0) {
      setImportError('At least one timeframe is required.');
      return;
    }
    if (new Date(fromDate) > new Date(toDate)) {
      setImportError('Start date must be earlier than end date.');
      return;
    }

    const req: CandleImportRequest = {
      symbols,
      timeframes: selectedTimeframes,
      from: new Date(`${fromDate}T00:00:00Z`).toISOString(),
      to: new Date(`${toDate}T23:59:59Z`).toISOString(),
    };

    try {
      const res = await startImportMutation.mutateAsync(req);
      setActiveJobId(res.job_id);
    } catch (err: any) {
      setImportError(err.message || 'Failed to start candle import job.');
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
      alert(err.message || 'Failed to create schedule');
    }
  };

  const handleToggleSchedule = async (sched: ImportSchedule) => {
    try {
      await patchScheduleMutation.mutateAsync({ enabled: !sched.enabled });
      setRecentlyChangedIds((prev) => new Set(prev).add(sched.id));
    } catch (err: any) {
      alert(err.message || 'Failed to update schedule status');
    }
  };

  const handleConfirmDeleteSchedule = async () => {
    if (!deleteTarget) return;
    try {
      await deleteScheduleMutation.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch (err: any) {
      alert(err.message || 'Failed to delete schedule');
    }
  };

  return (
    <div className="max-w-5xl mx-auto space-y-6 pb-20">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-white flex items-center gap-2">
          Historical Candle Data
        </h1>
        <p className="text-xs text-slate-400 mt-1">
          Import historical OHLCV market candles for backtesting and configure automated recurring sync schedules.
        </p>
      </div>

      {/* 1. New One-Off Import Card */}
      <Card>
        <CardHeader
          title="Manual Historical Data Import"
          subtitle="Fetch batch historical candlestick data from exchange archives directly into DB storage"
        />
        <form onSubmit={handleStartImport} className="p-6 pt-0 space-y-5">
          {importError && (
            <div className="p-3 rounded-lg bg-rose-950/60 border border-rose-800 text-xs text-rose-300 flex items-center gap-2">
              <AlertCircle className="w-4 h-4 shrink-0" />
              <span>{importError}</span>
            </div>
          )}

          {/* Symbols tag input */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-slate-300 flex items-center gap-1.5">
              Trading Symbols <span className="text-rose-400">*</span>
              <HelpTooltip>Enter trading pairs to import (e.g. BTCUSDT, ETHUSDT, SOLUSDT)</HelpTooltip>
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
                placeholder="Type symbol and click Add..."
                className="flex-1 px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500"
              />
              <button
                type="button"
                onClick={handleAddSymbol}
                className="px-3.5 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs font-semibold text-slate-200"
              >
                Add
              </button>
            </div>
            <div className="flex flex-wrap gap-1.5 pt-1">
              {symbols.map((sym) => (
                <span
                  key={sym}
                  className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-blue-950/60 border border-blue-800 text-xs font-medium text-blue-300"
                >
                  {sym}
                  <button
                    type="button"
                    onClick={() => handleRemoveSymbol(sym)}
                    className="hover:text-rose-300 p-0.5"
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          </div>

          {/* Timeframes multi-select */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-slate-300 flex items-center gap-1.5">
              Candle Timeframes <span className="text-rose-400">*</span>
              <HelpTooltip>Select all resolution intervals to download simultaneously</HelpTooltip>
            </label>
            <div className="flex flex-wrap gap-3">
              {TIMEFRAME_OPTIONS.map((tf) => {
                const checked = selectedTimeframes.includes(tf);
                return (
                  <label
                    key={tf}
                    className={`flex items-center gap-2 px-3 py-1.5 rounded-lg border text-xs font-medium cursor-pointer transition-colors ${
                      checked
                        ? 'bg-blue-950/70 border-blue-700 text-blue-300'
                        : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => handleToggleTimeframe(tf)}
                      className="rounded border-slate-700 text-blue-600 focus:ring-blue-500"
                    />
                    <span>{tf}</span>
                  </label>
                );
              })}
            </div>
          </div>

          {/* Date range inputs */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-semibold text-slate-300">From Date</label>
              <input
                type="date"
                required
                value={fromDate}
                onChange={(e) => setFromDate(e.target.value)}
                className="px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500 font-mono"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-semibold text-slate-300">To Date</label>
              <input
                type="date"
                required
                value={toDate}
                onChange={(e) => setToDate(e.target.value)}
                className="px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500 font-mono"
              />
            </div>
          </div>

          <div className="flex justify-end pt-2">
            <button
              type="submit"
              disabled={startImportMutation.isPending}
              className="px-5 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 text-xs font-semibold text-white shadow-lg flex items-center gap-2 transition-colors disabled:opacity-50"
            >
              {startImportMutation.isPending ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Download className="w-4 h-4" />
              )}
              <span>Start Import</span>
            </button>
          </div>
        </form>
      </Card>

      {/* 2. Import Progress Card */}
      {activeJobId && (
        <Card>
          <CardHeader
            title="Import Job Progress"
            subtitle={`Job ID: ${activeJobId}`}
          />
          <div className="p-6 pt-0 space-y-4">
            {(!jobData || jobData.status === 'pending' || jobData.status === 'running') && (
              <div className="p-6 rounded-xl bg-slate-950/70 border border-slate-800 flex flex-col items-center justify-center gap-3 text-center" aria-busy="true">
                <Loader2 className="w-8 h-8 text-blue-400 animate-spin" />
                <div>
                  <h4 className="text-sm font-semibold text-white">Importing Candlesticks...</h4>
                  <p className="text-xs text-slate-400 mt-0.5">
                    Fetching exchange partitions, checking for gaps, and writing klines to database.
                  </p>
                </div>
              </div>
            )}

            {jobData?.status === 'completed' && jobData.result && (
              <div className="space-y-4">
                <div className="p-4 rounded-xl bg-emerald-950/40 border border-emerald-800/60 flex items-center gap-2.5 text-xs text-emerald-300">
                  <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
                  <span>
                    Import complete ({jobData.result.per_pair.filter((p) => !p.error).length} of{' '}
                    {jobData.result.per_pair.length} pairs succeeded).
                  </span>
                </div>

                <div className="table-container">
                  <table className="table">
                    <thead>
                      <tr>
                        <th>Symbol</th>
                        <th>Timeframe</th>
                        <th>Candles Imported</th>
                        <th>Gaps Detected</th>
                        <th>Status / Error</th>
                      </tr>
                    </thead>
                    <tbody>
                      {jobData.result.per_pair.map((pair, idx) => (
                        <tr key={idx}>
                          <td className="font-semibold text-white">{pair.symbol}</td>
                          <td className="font-mono text-xs text-slate-300">{pair.timeframe}</td>
                          <td className="font-mono text-xs text-emerald-400">
                            {pair.candles_imported.toLocaleString()}
                          </td>
                          <td className="font-mono text-xs text-amber-400">{pair.gaps_detected}</td>
                          <td>
                            {pair.error ? (
                              <span className="text-xs text-rose-400 flex items-center gap-1">
                                <XCircle className="w-3.5 h-3.5 shrink-0" />
                                {pair.error}
                              </span>
                            ) : (
                              <span className="text-xs text-emerald-400 flex items-center gap-1">
                                <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
                                Success
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
              <div className="p-4 rounded-xl bg-rose-950/60 border border-rose-800 space-y-3 text-xs text-rose-300">
                <div className="flex items-center gap-2 font-semibold text-rose-200">
                  <AlertCircle className="w-4 h-4" /> Import Job Failed
                </div>
                <p>{jobData.error || 'An unexpected error occurred during candle import.'}</p>
                <button
                  type="button"
                  onClick={handleStartImport}
                  className="px-3 py-1.5 rounded bg-rose-900/60 hover:bg-rose-800 text-xs font-semibold text-white flex items-center gap-1.5"
                >
                  <RotateCw className="w-3.5 h-3.5" /> Retry Import
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
            <h3 className="text-lg font-bold text-white">Scheduled Recurring Imports</h3>
            <p className="text-xs text-slate-400">
              Automated cron jobs syncing historical candles periodically in the background worker
            </p>
          </div>
          <button
            type="button"
            onClick={() => setShowScheduleForm(!showScheduleForm)}
            className="px-3 py-1.5 rounded-lg bg-blue-600 hover:bg-blue-500 text-xs font-semibold text-white shadow flex items-center gap-1.5 shrink-0"
          >
            <Plus className="w-3.5 h-3.5" />
            <span>New Schedule</span>
          </button>
        </div>

        <div className="p-6 pt-0 space-y-4">
          {showCreatedNotice && (
            <div className="p-3 rounded-lg bg-amber-950/60 border border-amber-800 text-xs text-amber-300 flex items-center justify-between">
              <ScheduleRestartNotice />
              <button
                type="button"
                onClick={() => setShowCreatedNotice(false)}
                className="hover:text-white p-0.5"
              >
                ×
              </button>
            </div>
          )}

          {/* New Schedule Inline Form */}
          {showScheduleForm && (
            <form onSubmit={handleCreateSchedule} className="p-4 rounded-xl bg-slate-950/70 border border-slate-800 space-y-4">
              <h4 className="text-xs font-bold text-white uppercase tracking-wider">
                Create Recurring Sync Schedule
              </h4>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-slate-400 uppercase font-semibold">Symbol</label>
                  <input
                    type="text"
                    required
                    value={schedSymbol}
                    onChange={(e) => setSchedSymbol(e.target.value)}
                    placeholder="BTCUSDT"
                    className="px-2.5 py-1.5 rounded bg-slate-900 border border-slate-700 text-xs text-slate-200"
                  />
                </div>

                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-slate-400 uppercase font-semibold">Timeframe</label>
                  <select
                    value={schedTimeframe}
                    onChange={(e) => setSchedTimeframe(e.target.value)}
                    className="px-2.5 py-1.5 rounded bg-slate-900 border border-slate-700 text-xs text-slate-200"
                  >
                    {TIMEFRAME_OPTIONS.map((tf) => (
                      <option key={tf} value={tf}>
                        {tf}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-slate-400 uppercase font-semibold">Frequency</label>
                  <select
                    value={schedFreq}
                    onChange={(e) => setSchedFreq(e.target.value as 'daily' | 'weekly')}
                    className="px-2.5 py-1.5 rounded bg-slate-900 border border-slate-700 text-xs text-slate-200"
                  >
                    <option value="daily">Daily</option>
                    <option value="weekly">Weekly</option>
                  </select>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div className="flex flex-col gap-1">
                  <label className="text-[10px] text-slate-400 uppercase font-semibold">
                    Hour (UTC)
                  </label>
                  <select
                    value={schedHour}
                    onChange={(e) => setSchedHour(parseInt(e.target.value, 10))}
                    className="px-2.5 py-1.5 rounded bg-slate-900 border border-slate-700 text-xs text-slate-200"
                  >
                    {Array.from({ length: 24 }).map((_, h) => (
                      <option key={h} value={h}>
                        {h.toString().padStart(2, '0')}:00 UTC
                      </option>
                    ))}
                  </select>
                </div>

                {schedFreq === 'weekly' && (
                  <div className="flex flex-col gap-1">
                    <label className="text-[10px] text-slate-400 uppercase font-semibold">
                      Day of Week
                    </label>
                    <select
                      value={schedDow}
                      onChange={(e) => setSchedDow(parseInt(e.target.value, 10))}
                      className="px-2.5 py-1.5 rounded bg-slate-900 border border-slate-700 text-xs text-slate-200"
                    >
                      <option value={1}>Monday</option>
                      <option value={2}>Tuesday</option>
                      <option value={3}>Wednesday</option>
                      <option value={4}>Thursday</option>
                      <option value={5}>Friday</option>
                      <option value={6}>Saturday</option>
                      <option value={0}>Sunday</option>
                    </select>
                  </div>
                )}
              </div>

              <div className="flex justify-end gap-2 pt-1">
                <button
                  type="button"
                  onClick={() => setShowScheduleForm(false)}
                  className="px-3 py-1.5 rounded text-xs text-slate-400 hover:text-slate-200"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={createScheduleMutation.isPending}
                  className="px-4 py-1.5 rounded bg-blue-600 hover:bg-blue-500 text-xs font-semibold text-white"
                >
                  Create Schedule
                </button>
              </div>
            </form>
          )}

          {/* Schedules Table */}
          {schedules.length === 0 ? (
            <div className="p-8 text-center text-xs text-slate-500 bg-slate-950/40 rounded-lg">
              No scheduled imports configured.
            </div>
          ) : (
            <div className="table-container">
              <table className="table">
                <thead>
                  <tr>
                    <th>Symbol</th>
                    <th>Timeframe</th>
                    <th>Schedule</th>
                    <th>Enabled</th>
                    <th>Last Run</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {schedules.map((sched) => (
                    <React.Fragment key={sched.id}>
                      <tr>
                        <td className="font-semibold text-white">{sched.symbol}</td>
                        <td className="font-mono text-xs text-slate-300">{sched.timeframe}</td>
                        <td className="text-xs text-slate-300">
                          {describeCronSpec(sched.cron_spec)}
                        </td>
                        <td>
                          <label className="relative inline-flex items-center cursor-pointer">
                            <input
                              type="checkbox"
                              checked={sched.enabled}
                              onChange={() => handleToggleSchedule(sched)}
                              className="sr-only peer"
                            />
                            <div className="w-9 h-5 bg-slate-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-slate-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-blue-600"></div>
                          </label>
                        </td>
                        <td className="text-xs font-mono text-slate-400">
                          {sched.last_run_at
                            ? new Date(sched.last_run_at).toLocaleString()
                            : 'Never'}
                        </td>
                        <td>
                          <button
                            type="button"
                            onClick={() => setDeleteTarget(sched)}
                            className="p-1 text-slate-500 hover:text-rose-400 rounded hover:bg-rose-950/30"
                            title="Delete schedule"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </td>
                      </tr>
                      {recentlyChangedIds.has(sched.id) && (
                        <tr>
                          <td colSpan={6} className="bg-amber-950/30 py-1.5 px-3">
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
        title="Delete Scheduled Import"
        message="Delete this scheduled import? Already-imported candles are unaffected, and this only stops future runs after the worker's next restart."
        isDangerous={true}
        confirmText="Delete Schedule"
        onConfirm={handleConfirmDeleteSchedule}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}

function ScheduleRestartNotice() {
  return (
    <p className="text-xs text-amber-400 flex items-center gap-1.5">
      <Info className="w-3.5 h-3.5 shrink-0" />
      Takes effect after the worker's next restart — from the Settings screen, or on its own next
      redeploy.
    </p>
  );
}
