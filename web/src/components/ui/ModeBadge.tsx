import React from 'react';
import { StrategyMode } from '@/api/types';

interface ModeBadgeProps {
  mode: StrategyMode | string;
  className?: string;
}

export function ModeBadge({ mode, className = '' }: ModeBadgeProps) {
  const normalized = (mode || 'dryrun').toLowerCase();

  switch (normalized) {
    case 'live':
      return <span className={`badge badge-red ${className}`}>LIVE</span>;
    case 'paper':
      return <span className={`badge badge-amber ${className}`}>PAPER</span>;
    case 'dryrun':
      return <span className={`badge badge-blue ${className}`}>DRYRUN</span>;
    case 'backtest':
      return <span className={`badge badge-purple ${className}`}>BACKTEST</span>;
    default:
      return <span className={`badge badge-gray ${className}`}>{mode}</span>;
  }
}
