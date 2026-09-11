import React from 'react';
import { StrategyStatus, SignalStatus, OptimizationStatus } from '@/api/types';

interface StatusBadgeProps {
  status: StrategyStatus | SignalStatus | OptimizationStatus | string;
  className?: string;
}

export function StatusBadge({ status, className = '' }: StatusBadgeProps) {
  const normalized = (status || '').toLowerCase();

  switch (normalized) {
    case 'productive':
    case 'open':
    case 'completed':
    case 'passed':
      return <span className={`badge badge-green ${className}`}>{status}</span>;
    case 'testing':
    case 'pending':
    case 'running':
      return <span className={`badge badge-amber ${className}`}>{status}</span>;
    case 'disabled':
    case 'closed':
      return <span className={`badge badge-gray ${className}`}>{status}</span>;
    case 'failed':
      return <span className={`badge badge-red ${className}`}>{status}</span>;
    default:
      return <span className={`badge badge-blue ${className}`}>{status}</span>;
  }
}
