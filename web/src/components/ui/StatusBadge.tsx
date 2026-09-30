import React from 'react';
import { StrategyStatus, SignalStatus, OptimizationStatus } from '@/api/types';
import { useT } from '@/i18n';

interface StatusBadgeProps {
  status: StrategyStatus | SignalStatus | OptimizationStatus | string;
  className?: string;
}

// `badge`/`badge-*` were dead classes (no CSS rule ever defined them - see
// the equivalent `.btn`/`.table`/`.grid-4` dead classes found and fixed
// across the workbench panes) so every StatusBadge rendered as bare
// unstyled text throughout the app. Real Tailwind utility classes here,
// same base shape all variants share.
const BADGE_BASE = 'inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border';

export function StatusBadge({ status, className = '' }: StatusBadgeProps) {
  const t = useT();
  const normalized = (status || '').toLowerCase();
  // Display label only - the enum value itself is never translated.
  const label = t.enum('status', normalized) || status;

  switch (normalized) {
    case 'productive':
    case 'open':
    case 'completed':
    case 'passed':
      return <span className={`${BADGE_BASE} bg-success/15 text-success border-success/40 ${className}`}>{label}</span>;
    case 'testing':
    case 'pending':
    case 'running':
      return <span className={`${BADGE_BASE} bg-warning/15 text-warning border-warning/40 ${className}`}>{label}</span>;
    case 'disabled':
    case 'closed':
      return <span className={`${BADGE_BASE} bg-secondary text-muted-foreground border-border ${className}`}>{label}</span>;
    case 'failed':
      return <span className={`${BADGE_BASE} bg-destructive/15 text-destructive border-destructive/40 ${className}`}>{label}</span>;
    default:
      return <span className={`${BADGE_BASE} bg-accent text-accent-foreground border-border ${className}`}>{label}</span>;
  }
}
