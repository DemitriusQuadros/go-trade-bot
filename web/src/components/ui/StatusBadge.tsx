import React from 'react';
import { StrategyStatus, SignalStatus, OptimizationStatus } from '@/api/types';

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
  const normalized = (status || '').toLowerCase();

  switch (normalized) {
    case 'productive':
    case 'open':
    case 'completed':
    case 'passed':
      return <span className={`${BADGE_BASE} bg-green-900/40 text-green-300 border-green-700/40 ${className}`}>{status}</span>;
    case 'testing':
    case 'pending':
    case 'running':
      return <span className={`${BADGE_BASE} bg-amber-900/40 text-amber-300 border-amber-700/40 ${className}`}>{status}</span>;
    case 'disabled':
    case 'closed':
      return <span className={`${BADGE_BASE} bg-slate-800/60 text-slate-400 border-slate-600/40 ${className}`}>{status}</span>;
    case 'failed':
      return <span className={`${BADGE_BASE} bg-red-900/40 text-red-300 border-red-700/40 ${className}`}>{status}</span>;
    default:
      return <span className={`${BADGE_BASE} bg-blue-900/40 text-blue-300 border-blue-700/40 ${className}`}>{status}</span>;
  }
}
