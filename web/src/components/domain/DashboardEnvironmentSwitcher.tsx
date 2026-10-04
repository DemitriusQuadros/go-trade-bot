import React from 'react';
import { Layers, FlaskConical, Coins, Radio, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { useT } from '@/i18n';

export type DashboardViewMode = 'unified' | 'dryrun' | 'live';

interface DashboardEnvironmentSwitcherProps {
  activeView: DashboardViewMode;
  onViewChange: (view: DashboardViewMode) => void;
  serverMode?: string;
  sseConnected: boolean;
  onRefresh: () => void;
  isRefreshing?: boolean;
}

export function DashboardEnvironmentSwitcher({
  activeView,
  onViewChange,
  serverMode,
  sseConnected,
  onRefresh,
  isRefreshing = false,
}: DashboardEnvironmentSwitcherProps) {
  const t = useT();

  const isServerLive = serverMode === 'live' || serverMode === 'paper';

  return (
    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-card/60 backdrop-blur-sm border border-border/70 p-2.5 rounded-xl shadow-sm">
      {/* Environment Lens Segmented Control */}
      <div className="flex items-center gap-1.5 p-1 bg-muted/40 rounded-lg border border-border/40">
        <button
          type="button"
          onClick={() => onViewChange('unified')}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
            activeView === 'unified'
              ? 'bg-primary text-white shadow-sm'
              : 'text-muted-foreground hover:text-foreground hover:bg-muted/60'
          }`}
        >
          <Layers className="w-3.5 h-3.5" />
          <span>{t('dashboard.viewUnified')}</span>
        </button>

        <button
          type="button"
          onClick={() => onViewChange('dryrun')}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
            activeView === 'dryrun'
              ? 'bg-violet-600 text-white shadow-sm'
              : 'text-muted-foreground hover:text-foreground hover:bg-muted/60'
          }`}
        >
          <FlaskConical className="w-3.5 h-3.5" />
          <span>{t('dashboard.viewDryRun')}</span>
        </button>

        <button
          type="button"
          onClick={() => onViewChange('live')}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
            activeView === 'live'
              ? 'bg-amber-600 text-white shadow-sm'
              : 'text-muted-foreground hover:text-foreground hover:bg-muted/60'
          }`}
        >
          <Coins className="w-3.5 h-3.5" />
          <span>{t('dashboard.viewLive')}</span>
        </button>
      </div>

      {/* Telemetry Status Indicators */}
      <div className="flex items-center gap-2 self-end sm:self-auto">
        {/* Engine execution mode badge */}
        <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs border bg-background/80">
          <span className="text-[11px] text-muted-foreground font-medium">
            {t('dashboard.serverModeLabel')}:
          </span>
          <span
            className={`text-[10px] font-bold uppercase tracking-wider px-1.5 py-0.5 rounded ${
              isServerLive
                ? 'bg-amber-500/15 text-amber-500 border border-amber-500/30'
                : 'bg-violet-500/15 text-violet-400 border border-violet-500/30'
            }`}
          >
            {serverMode || 'dryrun'}
          </span>
        </div>

        {/* Real-time SSE status */}
        <div
          className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border ${
            sseConnected
              ? 'bg-emerald-500/10 text-emerald-500 border-emerald-500/20'
              : 'bg-destructive/10 text-destructive border-destructive/20'
          }`}
        >
          <Radio className={`w-3 h-3 ${sseConnected ? 'animate-pulse' : ''}`} />
          <span>{sseConnected ? t('dashboard.live') : t('dashboard.disconnected')}</span>
        </div>

        {/* Quick Refresh Button */}
        <Button
          variant="outline"
          size="sm"
          onClick={onRefresh}
          disabled={isRefreshing}
          className="h-8 px-2.5 text-xs"
        >
          <RefreshCw className={`w-3.5 h-3.5 mr-1.5 ${isRefreshing ? 'animate-spin' : ''}`} />
          {t('common.refresh')}
        </Button>
      </div>
    </div>
  );
}
