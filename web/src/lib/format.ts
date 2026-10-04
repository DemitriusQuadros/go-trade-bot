// Locale-aware display formatting (i18n-01 §4). Every helper reads the
// active UI locale (i18n getLocale()), so separators follow the language:
// 1,234.56 / 9/29/2026 in EN, 1.234,56 / 29/09/2026 in ES and PT-BR.
//
// Display only: form inputs keep parsing with lib/decimal.ts, and numbers
// inside code, the Lua editor or raw JSON views are never localized.

import { getLocale, tr, type Locale } from '@/i18n';

type Num = number | null | undefined;

const nfCache = new Map<string, Intl.NumberFormat>();
function nf(locale: Locale, opts: Intl.NumberFormatOptions): Intl.NumberFormat {
  const id = `${locale}|${JSON.stringify(opts)}`;
  let f = nfCache.get(id);
  if (!f) {
    f = new Intl.NumberFormat(locale, opts);
    nfCache.set(id, f);
  }
  return f;
}

function dfFor(locale: Locale, opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat(locale, opts);
}

function isNum(v: Num): v is number {
  return v != null && Number.isFinite(v);
}

export interface NumberOpts {
  /** Exact number of fraction digits (like toFixed). */
  digits?: number;
  minDigits?: number;
  maxDigits?: number;
  /** Prefix a "+" on positive values. */
  signed?: boolean;
  /** Group thousands (default true). */
  grouping?: boolean;
  /** Placeholder for null / NaN (default "—"). */
  empty?: string;
}

/** Locale-aware number; `digits` behaves like toFixed(digits). */
export function formatNumber(v: Num, opts: NumberOpts = {}): string {
  if (!isNum(v)) return opts.empty ?? '—';
  const min = opts.digits ?? opts.minDigits ?? 0;
  const max = opts.digits ?? opts.maxDigits ?? Math.max(min, 3);
  const s = nf(getLocale(), {
    minimumFractionDigits: min,
    maximumFractionDigits: max,
    useGrouping: opts.grouping ?? true,
  }).format(v);
  return opts.signed && v > 0 ? `+${s}` : s;
}

/** A value that is already a percentage (12.3 -> "12.30%"). */
export function formatPct(v: Num, digits = 2, opts: Omit<NumberOpts, 'digits'> = {}): string {
  if (!isNum(v)) return opts.empty ?? '—';
  return `${formatNumber(v, { ...opts, digits })}%`;
}

/** A ratio (0.123 -> "12.30%"). */
export function formatRatioPct(v: Num, digits = 2, opts: Omit<NumberOpts, 'digits'> = {}): string {
  if (!isNum(v)) return opts.empty ?? '—';
  return formatPct(v * 100, digits, opts);
}

/** US dollars with the "$" prefix; tiny positive amounts keep 4 decimals. Null -> "$0.00". */
export function formatUsd(v: Num, opts: { signed?: boolean; digits?: number } = {}): string {
  if (!isNum(v)) return `$${formatNumber(0, { digits: 2 })}`;
  const abs = Math.abs(v);
  const digits = opts.digits ?? (abs > 0 && abs < 0.01 ? 4 : 2);
  const body = `$${formatNumber(abs, { digits })}`;
  if (v < 0) return `-${body}`;
  return opts.signed && v > 0 ? `+${body}` : body;
}

/**
 * Crypto prices / quantities: keep their precision (up to `maxDigits`,
 * default 8) - only the separators follow the locale.
 */
export function formatPrice(v: Num, maxDigits = 8, minDigits = 0): string {
  return formatNumber(v, { minDigits, maxDigits });
}

/** Token counts: 1.2k / 3.4M. */
export function formatTokens(v: Num): string {
  if (!v) return '0';
  if (v >= 1_000_000) return `${formatNumber(v / 1_000_000, { digits: 1 })}M`;
  if (v >= 1_000) return `${formatNumber(v / 1_000, { digits: 1 })}k`;
  return formatNumber(v, { digits: 0 });
}

function toDate(v: string | number | Date | null | undefined): Date | null {
  if (v == null || v === '') return null;
  const d = v instanceof Date ? v : new Date(v);
  return Number.isNaN(d.getTime()) ? null : d;
}

function dateParts(locale: Locale): Intl.DateTimeFormatOptions {
  // EN keeps 9/29/2026; ES and PT-BR use 29/09/2026.
  const pad = locale === 'en' ? 'numeric' : '2-digit';
  return { year: 'numeric', month: pad, day: pad };
}

/** A calendar date in the local time zone. */
export function formatDate(v: string | number | Date | null | undefined, empty = '—'): string {
  const d = toDate(v);
  if (!d) return empty;
  const locale = getLocale();
  return dfFor(locale, dateParts(locale)).format(d);
}

/** Date + time (minutes, or seconds with `seconds: true`) in the local time zone. */
export function formatDateTime(
  v: string | number | Date | null | undefined,
  opts: { seconds?: boolean; empty?: string; utc?: boolean } = {},
): string {
  const d = toDate(v);
  if (!d) return opts.empty ?? '—';
  const locale = getLocale();
  return dfFor(locale, {
    ...dateParts(locale),
    hour: '2-digit',
    minute: '2-digit',
    second: opts.seconds ? '2-digit' : undefined,
    timeZone: opts.utc ? 'UTC' : undefined,
  }).format(d);
}

/** Time of day only. */
export function formatTime(v: string | number | Date | null | undefined, opts: { seconds?: boolean; empty?: string } = {}): string {
  const d = toDate(v);
  if (!d) return opts.empty ?? '—';
  return dfFor(getLocale(), { hour: '2-digit', minute: '2-digit', second: opts.seconds === false ? undefined : '2-digit' }).format(d);
}

/** Short month + day (chart ticks): "Sep 29" / "29 sept" / "29 de set.". */
export function formatShortDate(v: string | number | Date | null | undefined, opts: { utc?: boolean } = {}): string {
  const d = toDate(v);
  if (!d) return '';
  return dfFor(getLocale(), { month: 'short', day: 'numeric', timeZone: opts.utc ? 'UTC' : undefined }).format(d);
}

// Backtest ranges are UTC-midnight bounds - render them in UTC, or a
// negative-offset browser shows the day before (2026-06-01 -> 31/05).
export function formatUtcDay(iso: string | null | undefined): string {
  const d = toDate(iso);
  if (!d) return '?';
  const locale = getLocale();
  return dfFor(locale, { ...dateParts(locale), timeZone: 'UTC' }).format(d);
}

/** "3 hours ago" / "hace 3 horas" / "há 3 horas", via Intl.RelativeTimeFormat. */
export function formatRelative(iso: string | null | undefined, now: number = Date.now()): string {
  const d = toDate(iso);
  if (!d) return '—';
  const diffSec = Math.round((d.getTime() - now) / 1000);
  const abs = Math.abs(diffSec);
  const rtf = new Intl.RelativeTimeFormat(getLocale(), { numeric: 'auto', style: 'short' });
  if (abs < 45) return rtf.format(0, 'second');
  if (abs < 3600) return rtf.format(Math.round(diffSec / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(diffSec / 3600), 'hour');
  if (abs < 86400 * 30) return rtf.format(Math.round(diffSec / 86400), 'day');
  return formatDate(d);
}

/** Run duration: 850ms / 12.3s / 2m 5s. */
export function formatDuration(startIso: string, endIso?: string | null): string {
  if (!endIso) return '—';
  const ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (Number.isNaN(ms) || ms < 0) return '—';
  return formatMs(ms);
}

export function formatMs(ms: number): string {
  if (ms < 1000) return `${formatNumber(ms, { digits: 0 })}ms`;
  const s = ms / 1000;
  if (s < 60) return `${formatNumber(s, { digits: 1 })}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${Math.round(s % 60)}s`;
}

/** Placeholder used everywhere for "no value". */
export const EMPTY = '—';

/** Localized yes/no. */
export function formatBool(v: boolean): string {
  return v ? tr('common.yes') : tr('common.no');
}

// --- lightweight-charts --------------------------------------------------------

/**
 * `localization` for lightweight-charts: the axis + crosshair labels follow
 * the UI locale. Time values are UTC seconds (the charts' convention).
 */
export function chartLocalization(opts: { priceDigits?: number; utc?: boolean } = {}) {
  const locale = getLocale();
  const digits = opts.priceDigits;
  return {
    locale,
    priceFormatter: (p: number) =>
      digits == null ? formatNumber(p, { maxDigits: Math.abs(p) >= 100 ? 2 : 6 }) : formatNumber(p, { digits }),
    timeFormatter: (time: unknown) => {
      if (typeof time === 'number') return formatDateTime(time * 1000, { utc: opts.utc ?? true });
      if (typeof time === 'string') return formatUtcDay(time);
      if (time && typeof time === 'object' && 'year' in (time as Record<string, unknown>)) {
        const b = time as { year: number; month: number; day: number };
        return formatUtcDay(new Date(Date.UTC(b.year, b.month - 1, b.day)).toISOString());
      }
      return '';
    },
  };
}
