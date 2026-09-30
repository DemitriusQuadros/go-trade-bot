import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, ArrowUpRight, ChevronDown, ChevronRight } from 'lucide-react';
import { useT } from '@/i18n';

export type ChatDensity = 'compact' | 'comfortable';

// Shared frame for the chat's rich cards (Phase D-02 §6): a bg-card surface
// with a one-line header. In compact density (the dock) the body starts
// collapsed so a turn with several cards stays scannable; comfortable
// (Agent mode) starts expanded.
export function ChatCard({
  icon,
  title,
  meta,
  density,
  actions,
  children,
  testId,
}: {
  icon: React.ReactNode;
  title: React.ReactNode;
  /** Right side of the header: badges / one-line summary. */
  meta?: React.ReactNode;
  density: ChatDensity;
  actions?: React.ReactNode;
  children?: React.ReactNode;
  testId?: string;
}) {
  const [open, setOpen] = useState(density === 'comfortable');
  const hasBody = !!children || !!actions;
  return (
    <div data-testid={testId} className="rounded-lg border border-border bg-card text-card-foreground min-w-0">
      <button
        type="button"
        onClick={() => hasBody && setOpen((o) => !o)}
        aria-expanded={hasBody ? open : undefined}
        className={`w-full flex items-center gap-2 px-3 py-2 text-left min-w-0 ${
          hasBody ? 'hover:bg-accent/40 rounded-lg' : 'cursor-default'
        } ${density === 'compact' ? 'text-xs' : 'text-sm'}`}
      >
        {hasBody &&
          (open ? (
            <ChevronDown className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
          ))}
        <span className="shrink-0 text-muted-foreground">{icon}</span>
        <span className="font-semibold text-foreground truncate min-w-0">{title}</span>
        {meta && <span className="ml-auto flex items-center gap-1.5 shrink-0 min-w-0">{meta}</span>}
      </button>
      {open && hasBody && (
        <div className="px-3 pb-3 space-y-2.5 min-w-0">
          {children}
          {actions && <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 pt-0.5">{actions}</div>}
        </div>
      )}
    </div>
  );
}

export function CardAction({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link to={to} className="inline-flex items-center gap-1 text-[11px] font-medium text-primary hover:underline">
      {children}
      <ArrowUpRight className="w-3 h-3" />
    </Link>
  );
}

export function CardButton({ onClick, icon, children }: { onClick: () => void; icon?: React.ReactNode; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 text-[11px] bg-primary hover:bg-primary/90 text-primary-foreground font-semibold rounded px-2.5 py-1"
    >
      {icon}
      {children}
    </button>
  );
}

export function CardSkeleton({ label }: { label: string }) {
  return (
    <div role="status" aria-label={label} className="rounded-lg border border-border bg-card p-3 space-y-2 animate-pulse">
      <div className="h-3 w-1/3 rounded bg-secondary" />
      <div className="h-3 w-2/3 rounded bg-secondary" />
      <span className="sr-only">{label}</span>
    </div>
  );
}

// One-line fallback when a card's entity can't be loaded (404, error).
export function CardFallback({ text, to, linkLabel }: { text: string; to?: string; linkLabel?: string }) {
  const t = useT();
  return (
    <div className="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2 text-xs text-muted-foreground min-w-0">
      <AlertTriangle className="w-3.5 h-3.5 shrink-0 text-warning" />
      <span className="truncate">{text}</span>
      {to && (
        <Link to={to} className="ml-auto shrink-0 text-primary hover:underline">
          {linkLabel ?? t('cards.open')}
        </Link>
      )}
    </div>
  );
}

// Metric tile, same idea as the Dashboard KPIs but sized for a chat card.
export function Kpi({ label, value, tone }: { label: string; value: string; tone?: 'success' | 'destructive' | 'muted' }) {
  const color = tone === 'success' ? 'text-success' : tone === 'destructive' ? 'text-destructive' : 'text-foreground';
  return (
    <div className="rounded border border-border/60 bg-background/40 px-2 py-1.5 min-w-0">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground truncate">{label}</div>
      <div className={`font-mono tabular-nums text-sm font-semibold ${color}`}>{value}</div>
    </div>
  );
}

export function signTone(v: number | null | undefined): 'success' | 'destructive' | undefined {
  if (v == null || Number.isNaN(v) || v === 0) return undefined;
  return v > 0 ? 'success' : 'destructive';
}
