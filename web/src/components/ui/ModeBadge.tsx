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
      return <span className={`${BADGE_BASE} bg-red-900/40 text-red-300 border-red-700/40 ${className}`}>LIVE</span>;
    case 'paper':
      return <span className={`${BADGE_BASE} bg-amber-900/40 text-amber-300 border-amber-700/40 ${className}`}>PAPER</span>;
    case 'dryrun':
      return <span className={`${BADGE_BASE} bg-blue-900/40 text-blue-300 border-blue-700/40 ${className}`}>DRYRUN</span>;
    case 'backtest':
      return <span className={`${BADGE_BASE} bg-purple-900/40 text-purple-300 border-purple-700/40 ${className}`}>BACKTEST</span>;
    default:
      return <span className={`${BADGE_BASE} bg-slate-800/60 text-slate-400 border-slate-600/40 ${className}`}>{mode}</span>;
  }
}
