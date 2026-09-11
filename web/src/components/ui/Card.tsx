import React, { ReactNode } from 'react';
import { cn } from '../../lib/utils';

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {}

export function Card({ children, className, ...props }: CardProps) {
  return (
    <div className={cn("rounded-lg border bg-card text-card-foreground shadow-sm p-4", className)} {...props}>
      {children}
    </div>
  );
}

export function CardHeader({
  title,
  subtitle,
  action,
  className
}: {
  title: string;
  subtitle?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex items-center justify-between mb-4", className)}>
      <div>
        <h2 className="text-lg font-semibold tracking-tight text-primary/90">{title}</h2>
        {subtitle && <p className="text-sm text-muted-foreground mt-1">{subtitle}</p>}
      </div>
      {action && <div>{action}</div>}
    </div>
  );
}

export function MetricCard({
  title,
  value,
  change,
  isPositive,
  icon,
  subtitle,
  className
}: {
  title: string;
  value: string | number;
  change?: string;
  isPositive?: boolean;
  icon?: ReactNode;
  subtitle?: string;
  className?: string;
}) {
  return (
    <Card className={cn("flex flex-col justify-between", className)}>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm font-medium text-muted-foreground">
          {title}
        </span>
        {icon && <div className="text-muted-foreground opacity-70">{icon}</div>}
      </div>
      <div className="flex items-baseline justify-between">
        <span className="text-2xl font-bold text-foreground">{value}</span>
        {change && (
          <span
            className={cn("text-xs font-semibold", 
              isPositive ? 'text-green-500' : 'text-destructive'
            )}
          >
            {change}
          </span>
        )}
      </div>
      {subtitle && <span className="text-xs text-muted-foreground mt-2">{subtitle}</span>}
    </Card>
  );
}
