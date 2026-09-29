// Small, dependency-free time formatting helpers shared by the agents
// platform pages (relative "3h ago" labels, run durations).

export function formatRelative(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '—';
  const diffSec = Math.round((t - now) / 1000);
  const abs = Math.abs(diffSec);
  const future = diffSec > 0;
  let value: number;
  let unit: string;
  if (abs < 45) return future ? 'in a few seconds' : 'just now';
  if (abs < 3600) {
    value = Math.round(abs / 60);
    unit = 'm';
  } else if (abs < 86400) {
    value = Math.round(abs / 3600);
    unit = 'h';
  } else if (abs < 86400 * 30) {
    value = Math.round(abs / 86400);
    unit = 'd';
  } else {
    return new Date(iso).toLocaleDateString();
  }
  return future ? `in ${value}${unit}` : `${value}${unit} ago`;
}

export function formatDuration(startIso: string, endIso?: string | null): string {
  if (!endIso) return '—';
  const ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (Number.isNaN(ms) || ms < 0) return '—';
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${Math.round(s % 60)}s`;
}

export function formatUsd(v: number | null | undefined): string {
  if (v == null || Number.isNaN(v)) return '$0.00';
  if (v > 0 && v < 0.01) return `$${v.toFixed(4)}`;
  return `$${v.toFixed(2)}`;
}

export function formatTokens(v: number | null | undefined): string {
  if (!v) return '0';
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
  if (v >= 1_000) return `${(v / 1_000).toFixed(1)}k`;
  return String(v);
}

// Backtest ranges are UTC-midnight bounds - render them in UTC, or a
// negative-offset browser shows the day before (2026-06-01 -> 31/05).
export function formatUtcDay(iso: string | null | undefined): string {
  if (!iso) return '?';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '?' : d.toLocaleDateString(undefined, { timeZone: 'UTC' });
}
