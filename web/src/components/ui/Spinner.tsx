import React from 'react';
import { Loader2 } from 'lucide-react';

export function Spinner({
  size = 'md',
  className = '',
}: {
  size?: 'sm' | 'md' | 'lg';
  className?: string;
}) {
  const sizeMap = {
    sm: 'w-4 h-4',
    md: 'w-6 h-6',
    lg: 'w-10 h-10',
  };

  return (
    <Loader2
      className={`animate-spin text-blue-500 ${sizeMap[size]} ${className}`}
    />
  );
}

export function LoadingScreen({ message = 'Loading...' }: { message?: string }) {
  return (
    <div className="flex flex-col items-center justify-center p-12 gap-3 text-slate-400">
      <Spinner size="lg" />
      <p className="text-sm font-medium">{message}</p>
    </div>
  );
}
