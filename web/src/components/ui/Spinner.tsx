import React from 'react';
import { Loader2 } from 'lucide-react';
import { useT } from '@/i18n';

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
      className={`animate-spin text-primary ${sizeMap[size]} ${className}`}
    />
  );
}

export function LoadingScreen({ message }: { message?: string }) {
  const t = useT();
  return (
    <div className="flex flex-col items-center justify-center p-12 gap-3 text-muted-foreground">
      <Spinner size="lg" />
      <p className="text-sm font-medium">{message ?? t('common.loading')}</p>
    </div>
  );
}
