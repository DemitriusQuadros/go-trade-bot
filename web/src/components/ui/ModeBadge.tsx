import React from 'react';
import { StrategyMode } from '@/api/types';

interface ModeBadgeProps {
  mode: StrategyMode | string;
  className?: string;
}

// `badge`/`badge-*` were dead classes (no CSS rule ever defined them - see
// StatusBadge.tsx for the full story) so this rendered as bare unstyled
// text throughout the app. Real Tailwind now, same base shape as StatusBadge.
const BADGE_BASE = 'inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border';

export function ModeBadge({ mode, className = '' }: ModeBadgeProps) {
  const normalized = (mode || 'dryrun').toLowerCase();

  switch (normalized) {
    case 'live':
      return <span className={`${BADGE_BASE} bg-destructive/15 text-destructive border-destructive/40 ${className}`}>LIVE</span>;
    case 'paper':
      return <span className={`${BADGE_BASE} bg-warning/15 text-warning border-warning/40 ${className}`}>PAPER</span>;
    case 'dryrun':
      return <span className={`${BADGE_BASE} bg-secondary text-muted-foreground border-border ${className}`}>DRYRUN</span>;
    case 'backtest':
      return <span className={`${BADGE_BASE} bg-accent text-accent-foreground border-border ${className}`}>BACKTEST</span>;
    default:
      return <span className={`${BADGE_BASE} bg-secondary text-muted-foreground border-border ${className}`}>{mode}</span>;
  }
}
