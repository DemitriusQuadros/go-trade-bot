import React, { useState } from 'react';
import {
  AlertCircle,
  Database,
  Loader2,
  Pause,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Trash2,
  ChevronDown,
  ChevronUp,
} from 'lucide-react';
import {
  useCandleDatasets,
  useCandleChunks,
  useCreateCandleDataset,
  usePauseCandleDataset,
  useResumeCandleDataset,
  useRetryFailedCandleDataset,
  useReconcileCandleDataset,
  useDeleteCandleDataset,
} from '@/hooks/queries';
import { useAuth } from '@/context/AuthContext';
import type { CandleChunkStatus, CandleDataset, CandleRange } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { HelpTooltip } from '@/components/ui/HelpTooltip';
import { formatDateTime, formatNumber } from '@/lib/format';
import { useT, type MessageKey } from '@/i18n';

// 3d/1w are not supported by the reconciler (Binance aligns them differently).
const TIMEFRAME_OPTIONS = ['1m', '3m', '5m', '15m', '30m', '1h', '2h', '4h', '6h', '8h', '12h', '1d'];

const STATE_STYLES: Record<CandleDataset['state'], string> = {
  live: 'bg-success/15 text-success border-success/40',
  converging: 'bg-primary/15 text-primary border-primary/40',
  idle: 'bg-muted text-muted-foreground border-border',
  paused: 'bg-warning/15 text-warning border-warning/40',
  failed: 'bg-destructive/15 text-destructive border-destructive/40',
};

const CHUNK_FILTERS: Array<CandleChunkStatus | 'all'> = ['all', 'pending', 'running', 'failed', 'dead', 'done'];

function coveredThrough(d: CandleDataset): string | null {
  return d.loaded.length > 0 ? d.loaded[d.loaded.length - 1].to : null;
}

function errMessage(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback;
}

function StatTile({ label, value, tone }: { label: string; value: string; tone?: 'danger' | 'primary' }) {
  const toneClass = tone === 'danger' ? 'text-destructive' : tone === 'primary' ? 'text-primary' : 'text-foreground';
  return (
    <Card className="p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={`text-2xl font-semibold mt-1 font-mono ${toneClass}`}>{value}</div>
    </Card>
  );
}

/** Loaded / exchange-gap / missing ranges laid over the desired span. */
function CoverageBar({ dataset, tall }: { dataset: CandleDataset; tall?: boolean }) {
  const desired = dataset.desired;
  if (!desired) {
    return <div className="h-2 rounded bg-muted" />;
  }
  const start = new Date(desired.from).getTime();
  const span = Math.max(new Date(desired.to).getTime() - start, 1);
  const seg = (r: CandleRange, cls: string, key: string) => {
    const left = ((new Date(r.from).getTime() - start) / span) * 100;
    const width = ((new Date(r.to).getTime() - new Date(r.from).getTime()) / span) * 100;
    return (
      <div
        key={key}
        className={`absolute top-0 bottom-0 ${cls}`}
        style={{ left: `${Math.max(left, 0)}%`, width: `${Math.max(width, 0.3)}%` }}
      />
    );
  };
  return (
    <div className={`relative ${tall ? 'h-4' : 'h-2'} rounded bg-muted overflow-hidden`} role="img" aria-label={`${formatNumber(dataset.progress_pct, { digits: 1 })}%`}>
      {dataset.loaded.map((r, i) => seg(r, 'bg-success/80', `l${i}`))}
      {dataset.known_gaps.map((r, i) => seg(r, 'bg-warning/80', `g${i}`))}
    </div>
  );
}

function StateBadge({ state }: { state: CandleDataset['state'] }) {
  const t = useT();
  return (
    <span className={`inline-flex items-center rounded border px-2 py-0.5 text-[11px] font-semibold ${STATE_STYLES[state]}`}>
      {state === 'converging' && <Loader2 className="w-3 h-3 mr-1 animate-spin" />}
      {t(`candles.state.${state}` as MessageKey)}
    </span>
  );
}

function DatasetDetail({ dataset }: { dataset: CandleDataset }) {
  const t = useT();
  const [filter, setFilter] = useState<CandleChunkStatus | 'all'>('all');
  const { data: chunks = [], isLoading } = useCandleChunks(dataset.id, filter === 'all' ? undefined : filter);

  return (
    <Card className="space-y-4">
      <CardHeader
        title={t('candles.detailTitle', { symbol: dataset.symbol, timeframe: dataset.timeframe })}
        subtitle={
          dataset.desired
            ? t('candles.desiredRange', { from: formatDateTime(dataset.desired.from, { utc: true }), to: formatDateTime(dataset.desired.to, { utc: true }) })
            : undefined
        }
      />
      {dataset.listing_error && (
        <div className="p-3 rounded-lg bg-destructive/10 border border-destructive/40 text-xs text-destructive">
          {t('candles.listingError', { error: dataset.listing_error })}
        </div>
      )}
      {dataset.last_error && (
        <div className="p-3 rounded-lg bg-destructive/10 border border-destructive/40 text-xs text-destructive font-mono break-words">
          <span className="font-semibold">{t('candles.lastError')}: </span>
          {dataset.last_error}
        </div>
      )}

      <div>
        <h3 className="text-xs font-semibold text-foreground mb-2">{t('candles.timeline')}</h3>
        <CoverageBar dataset={dataset} tall />
        <div className="flex flex-wrap gap-4 mt-2 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded-sm bg-success/80" />{t('candles.legendLoaded')}</span>
          <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded-sm bg-warning/80" />{t('candles.legendGap')}</span>
          <span className="flex items-center gap-1.5"><span className="w-3 h-3 rounded-sm bg-muted border border-border" />{t('candles.legendMissing')}</span>
        </div>
        {dataset.ranges_truncated && <p className="text-[11px] text-muted-foreground mt-1">{t('candles.rangesTruncated')}</p>}
      </div>

      <div>
        <div className="flex items-center justify-between mb-2 gap-3 flex-wrap">
          <h3 className="text-xs font-semibold text-foreground">{t('candles.chunksTitle')}</h3>
          <div className="flex gap-1">
            {CHUNK_FILTERS.map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => setFilter(f)}
                className={`px-2 py-1 rounded border text-[11px] font-semibold transition-colors ${
                  filter === f ? 'bg-card border-primary text-foreground' : 'border-border text-muted-foreground hover:text-foreground'
                }`}
              >
                {f === 'all' ? t('candles.chunksAll') : t(`candles.chunk.${f}` as MessageKey)}
              </button>
            ))}
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-muted-foreground border-b border-border">
                <th className="py-2 pr-3 font-semibold">{t('candles.colRange')}</th>
                <th className="py-2 pr-3 font-semibold">{t('candles.colPurpose')}</th>
                <th className="py-2 pr-3 font-semibold">{t('candles.colStatus')}</th>
                <th className="py-2 pr-3 font-semibold text-right">{t('candles.colAttempts')}</th>
                <th className="py-2 pr-3 font-semibold">{t('candles.colSource')}</th>
                <th className="py-2 pr-3 font-semibold text-right">{t('candles.colRows')}</th>
                <th className="py-2 font-semibold">{t('candles.colError')}</th>
              </tr>
            </thead>
            <tbody>
              {isLoading && (
                <tr><td colSpan={7} className="py-4 text-center"><Loader2 className="w-4 h-4 animate-spin inline" /></td></tr>
              )}
              {!isLoading && chunks.length === 0 && (
                <tr><td colSpan={7} className="py-4 text-center text-muted-foreground">{t('candles.noChunks')}</td></tr>
              )}
              {chunks.map((c) => (
                <tr key={c.id} className="border-b border-border/50 align-top">
                  <td className="py-2 pr-3 font-mono whitespace-nowrap">{formatDateTime(c.from, { utc: true })} – {formatDateTime(c.to, { utc: true })}</td>
                  <td className="py-2 pr-3">{t(`candles.purpose.${c.purpose}` as MessageKey)}</td>
                  <td className="py-2 pr-3">{t(`candles.chunk.${c.status}` as MessageKey)}</td>
                  <td className="py-2 pr-3 text-right font-mono">{c.attempts}</td>
                  <td className="py-2 pr-3 font-mono">{c.source_used || '—'}</td>
                  <td className="py-2 pr-3 text-right font-mono">{formatNumber(c.row_count)}</td>
                  <td className="py-2 text-destructive break-words max-w-xs">{c.last_error}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </Card>
  );
}

export function CandleImport() {
  const t = useT();
  const { can } = useAuth();
  const canEdit = can('admin');

  const { data: datasets = [], isLoading } = useCandleDatasets();
  const createMut = useCreateCandleDataset();
  const pauseMut = usePauseCandleDataset();
  const resumeMut = useResumeCandleDataset();
  const retryMut = useRetryFailedCandleDataset();
  const syncMut = useReconcileCandleDataset();
  const deleteMut = useDeleteCandleDataset();

  const [symbol, setSymbol] = useState('');
  const [timeframe, setTimeframe] = useState('15m');
  const [start, setStart] = useState('');
  const [keepLive, setKeepLive] = useState(true);
  const [formError, setFormError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [openId, setOpenId] = useState<number | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<CandleDataset | null>(null);

  const loadingNow = datasets.filter((d) => d.state === 'converging').length;
  const needAttention = datasets.filter((d) => d.state === 'failed').length;
  const deadDatasets = datasets.filter((d) => d.chunks.dead > 0).length;
  const missingTotal = datasets.reduce((n, d) => n + d.missing_candles, 0);
  const openDataset = datasets.find((d) => d.id === openId) ?? null;

  const handleAdd = (e: React.FormEvent) => {
    e.preventDefault();
    const sym = symbol.trim().toUpperCase();
    if (!/^[A-Z0-9]{4,20}$/.test(sym)) {
      setFormError(t('candles.errSymbol'));
      return;
    }
    setFormError(null);
    createMut.mutate(
      { symbol: sym, timeframe, start: start || undefined, keep_live: keepLive },
      {
        onSuccess: (d) => {
          setSymbol('');
          setStart('');
          setOpenId(d.id);
        },
        onError: (err) => setFormError(errMessage(err, t('candles.errCreate'))),
      },
    );
  };

  const run = (mut: { mutate: (id: number, o: { onError: (e: unknown) => void; onSuccess: () => void }) => void }, id: number) => {
    setActionError(null);
    mut.mutate(id, { onError: (e) => setActionError(errMessage(e, t('candles.errAction'))), onSuccess: () => setActionError(null) });
  };

  const iconBtn = 'p-1.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary transition-colors disabled:opacity-50';

  return (
    <div className="space-y-6 max-w-7xl mx-auto">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-2">
          <Database className="w-6 h-6 text-primary" />
          {t('candles.title')}
        </h1>
        <p className="text-sm text-muted-foreground mt-1">{t('candles.subtitle')}</p>
      </div>

      {deadDatasets > 0 && (
        <div className="p-4 rounded-xl bg-destructive/10 border border-destructive/40 flex items-center gap-2.5 text-xs text-destructive" role="alert">
          <AlertCircle className="w-4 h-4 shrink-0" />
          <span>{t('candles.alertDead', { n: deadDatasets })}</span>
        </div>
      )}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatTile label={t('candles.statDatasets')} value={formatNumber(datasets.length)} />
        <StatTile label={t('candles.statActive')} value={formatNumber(loadingNow)} tone="primary" />
        <StatTile label={t('candles.statFailed')} value={formatNumber(needAttention)} tone={needAttention > 0 ? 'danger' : undefined} />
        <StatTile label={t('candles.statMissing')} value={formatNumber(missingTotal)} />
      </div>

      <Card>
        <CardHeader title={t('candles.addTitle')} subtitle={t('candles.addSubtitle')} />
        <form onSubmit={handleAdd} className="grid grid-cols-1 md:grid-cols-5 gap-4 items-end">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="ds-symbol" className="text-xs font-semibold text-foreground">{t('candles.symbol')}</label>
            <input
              id="ds-symbol"
              value={symbol}
              onChange={(e) => setSymbol(e.target.value)}
              placeholder={t('candles.symbolPlaceholder')}
              disabled={!canEdit}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono uppercase"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="ds-timeframe" className="text-xs font-semibold text-foreground">{t('candles.timeframe')}</label>
            <select
              id="ds-timeframe"
              value={timeframe}
              onChange={(e) => setTimeframe(e.target.value)}
              disabled={!canEdit}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
            >
              {TIMEFRAME_OPTIONS.map((tf) => (
                <option key={tf} value={tf}>{tf}</option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="ds-start" className="text-xs font-semibold text-foreground flex items-center gap-1">
              {t('candles.start')}
              <HelpTooltip>{t('candles.startHelp')}</HelpTooltip>
            </label>
            <input
              id="ds-start"
              type="date"
              value={start}
              onChange={(e) => setStart(e.target.value)}
              disabled={!canEdit}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
            />
          </div>
          <label className="flex items-center gap-2 text-xs text-foreground pb-2.5">
            <input type="checkbox" checked={keepLive} onChange={(e) => setKeepLive(e.target.checked)} disabled={!canEdit} />
            <span className="flex items-center gap-1">
              {t('candles.keepLive')}
              <HelpTooltip>{t('candles.keepLiveHelp')}</HelpTooltip>
            </span>
          </label>
          <div className="flex justify-end">
            {canEdit ? (
              <button
                type="submit"
                disabled={createMut.isPending}
                className="px-5 py-2.5 rounded-lg bg-primary text-xs font-semibold text-primary-foreground shadow flex items-center gap-2 transition-colors disabled:opacity-50"
              >
                {createMut.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
                <span>{t('candles.add')}</span>
              </button>
            ) : (
              <p className="text-xs text-muted-foreground" data-testid="candle-datasets-no-permission">{t('candles.noPermission')}</p>
            )}
          </div>
        </form>
        {formError && <p className="text-xs text-destructive mt-3" role="alert">{formError}</p>}
      </Card>

      {actionError && (
        <div className="p-3 rounded-lg bg-destructive/10 border border-destructive/40 text-xs text-destructive" role="alert">{actionError}</div>
      )}

      <Card className="p-0 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted-foreground border-b border-border">
                <th className="py-3 px-4 font-semibold">{t('candles.colDataset')}</th>
                <th className="py-3 px-4 font-semibold min-w-[220px]">{t('candles.colCoverage')}</th>
                <th className="py-3 px-4 font-semibold">{t('candles.colThrough')}</th>
                <th className="py-3 px-4 font-semibold">{t('candles.colChunks')}</th>
                <th className="py-3 px-4 font-semibold text-right">{t('candles.colActions')}</th>
              </tr>
            </thead>
            <tbody>
              {isLoading && (
                <tr><td colSpan={5} className="py-8 text-center"><Loader2 className="w-5 h-5 animate-spin inline text-muted-foreground" /></td></tr>
              )}
              {!isLoading && datasets.length === 0 && (
                <tr><td colSpan={5} className="py-8 text-center text-xs text-muted-foreground">{t('candles.empty')}</td></tr>
              )}
              {datasets.map((d) => {
                const open = openId === d.id;
                const hasFailed = d.chunks.failed + d.chunks.dead > 0;
                return (
                  <tr key={d.id} className={`border-b border-border/60 align-top ${open ? 'bg-card/60' : ''}`}>
                    <td className="py-3 px-4">
                      <div className="font-mono font-semibold text-foreground">{d.symbol} <span className="text-muted-foreground">{d.timeframe}</span></div>
                      <div className="mt-1 flex items-center gap-2">
                        <StateBadge state={d.state} />
                        <span className="text-[11px] text-muted-foreground">{d.keep_live ? t('candles.modeLive') : t('candles.modeOnce')}</span>
                      </div>
                    </td>
                    <td className="py-3 px-4">
                      <CoverageBar dataset={d} />
                      <div className="mt-1.5 text-[11px] text-muted-foreground font-mono">
                        {t('candles.progress', { pct: formatNumber(d.progress_pct, { digits: 1 }) })}
                        {d.missing_candles > 0 && <> · {t('candles.missingN', { n: formatNumber(d.missing_candles) })}</>}
                      </div>
                    </td>
                    <td className="py-3 px-4 font-mono text-xs whitespace-nowrap">{formatDateTime(coveredThrough(d), { utc: true })}</td>
                    <td className="py-3 px-4 text-[11px] font-mono text-muted-foreground space-y-0.5">
                      <div>{t('candles.chunk.pending')}: {d.chunks.pending} · {t('candles.chunk.running')}: {d.chunks.running}</div>
                      <div className={hasFailed ? 'text-destructive' : ''}>{t('candles.chunk.failed')}: {d.chunks.failed} · {t('candles.chunk.dead')}: {d.chunks.dead}</div>
                    </td>
                    <td className="py-3 px-4">
                      <div className="flex justify-end gap-1.5">
                        {canEdit && (d.paused ? (
                          <button type="button" className={iconBtn} title={t('candles.resume')} aria-label={t('candles.resume')} onClick={() => run(resumeMut, d.id)} disabled={resumeMut.isPending}>
                            <Play className="w-4 h-4" />
                          </button>
                        ) : (
                          <button type="button" className={iconBtn} title={t('candles.pause')} aria-label={t('candles.pause')} onClick={() => run(pauseMut, d.id)} disabled={pauseMut.isPending}>
                            <Pause className="w-4 h-4" />
                          </button>
                        ))}
                        {canEdit && hasFailed && (
                          <button type="button" className={`${iconBtn} text-destructive`} title={t('candles.retryFailed')} aria-label={t('candles.retryFailed')} onClick={() => run(retryMut, d.id)} disabled={retryMut.isPending}>
                            <RotateCw className="w-4 h-4" />
                          </button>
                        )}
                        {canEdit && (
                          <button type="button" className={iconBtn} title={t('candles.syncNow')} aria-label={t('candles.syncNow')} onClick={() => run(syncMut, d.id)} disabled={syncMut.isPending || d.paused}>
                            <RefreshCw className="w-4 h-4" />
                          </button>
                        )}
                        <button type="button" className={iconBtn} title={open ? t('candles.hideDetails') : t('candles.details')} aria-label={open ? t('candles.hideDetails') : t('candles.details')} aria-expanded={open} onClick={() => setOpenId(open ? null : d.id)}>
                          {open ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
                        </button>
                        {canEdit && (
                          <button type="button" className={`${iconBtn} hover:text-destructive`} title={t('candles.delete')} aria-label={t('candles.delete')} onClick={() => setDeleteTarget(d)}>
                            <Trash2 className="w-4 h-4" />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Card>

      {openDataset && <DatasetDetail dataset={openDataset} />}

      <ConfirmDialog
        isOpen={deleteTarget !== null}
        title={t('candles.deleteTitle')}
        message={deleteTarget ? t('candles.deleteMessage', { symbol: deleteTarget.symbol, timeframe: deleteTarget.timeframe }) : ''}
        confirmText={t('candles.deleteConfirm')}
        isDangerous
        onConfirm={() => {
          if (deleteTarget) {
            if (openId === deleteTarget.id) setOpenId(null);
            run(deleteMut, deleteTarget.id);
          }
          setDeleteTarget(null);
        }}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}
