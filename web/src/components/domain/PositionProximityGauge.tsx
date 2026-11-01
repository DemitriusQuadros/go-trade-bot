import React from 'react';
import { Target, ShieldAlert, ArrowRight, ShieldCheck } from 'lucide-react';
import { formatNumber, formatPct } from '@/lib/format';
import { useT } from '@/i18n';

interface PositionProximityGaugeProps {
  entryPrice: number;
  currentPrice: number;
  takeProfitPrice?: number;
  stopLossPrice?: number;
  className?: string;
  compact?: boolean;
}

export const PositionProximityGauge: React.FC<PositionProximityGaugeProps> = ({
  entryPrice,
  currentPrice,
  takeProfitPrice,
  stopLossPrice,
  className = '',
  compact = false,
}) => {
  const t = useT();

  const hasTP = !!takeProfitPrice && takeProfitPrice > 0;
  const hasSL = !!stopLossPrice && stopLossPrice > 0;

  // Stop buffer calculation (from current to SL)
  const stopBufferPct = hasSL && currentPrice > 0
    ? ((currentPrice - stopLossPrice!) / currentPrice) * 100
    : null;

  // Target distance calculation (from current to TP)
  const distToTPPct = hasTP && currentPrice > 0
    ? ((takeProfitPrice! - currentPrice) / currentPrice) * 100
    : null;

  // Calculate normalized slider percentage [0, 100]
  let sliderMin = hasSL ? stopLossPrice! : entryPrice * 0.95;
  let sliderMax = hasTP ? takeProfitPrice! : entryPrice * 1.05;

  if (sliderMax <= sliderMin) {
    sliderMax = sliderMin + 1;
  }

  // Position of entry and current price inside [sliderMin, sliderMax]
  const clamp = (val: number) => Math.min(100, Math.max(0, val));
  const entryPercent = clamp(((entryPrice - sliderMin) / (sliderMax - sliderMin)) * 100);
  const currentPercent = clamp(((currentPrice - sliderMin) / (sliderMax - sliderMin)) * 100);

  const isProfitable = currentPrice >= entryPrice;

  if (compact) {
    return (
      <div className={`flex flex-col gap-1 text-[11px] min-w-[160px] ${className}`}>
        <div className="flex items-center justify-between text-muted-foreground font-mono text-[10px]">
          <span>{hasSL ? `$${formatNumber(stopLossPrice!, { digits: 2 })}` : '—'}</span>
          <span className="font-semibold text-foreground">${formatNumber(currentPrice, { digits: 2 })}</span>
          <span>{hasTP ? `$${formatNumber(takeProfitPrice!, { digits: 2 })}` : '—'}</span>
        </div>
        <div className="relative h-1.5 w-full bg-muted/60 rounded-full overflow-hidden">
          {/* Loss zone */}
          <div
            className="absolute top-0 bottom-0 left-0 bg-rose-500/30"
            style={{ width: `${entryPercent}%` }}
          />
          {/* Profit zone */}
          <div
            className="absolute top-0 bottom-0 right-0 bg-emerald-500/30"
            style={{ width: `${100 - entryPercent}%` }}
          />
          {/* Current price marker */}
          <div
            className={`absolute top-0 bottom-0 w-2 -ml-1 rounded-full ${
              isProfitable ? 'bg-emerald-500 shadow-xs shadow-emerald-500/50' : 'bg-rose-500 shadow-xs shadow-rose-500/50'
            }`}
            style={{ left: `${currentPercent}%` }}
          />
        </div>
        <div className="flex items-center justify-between text-[10px]">
          {hasSL && stopBufferPct !== null ? (
            <span className={stopBufferPct <= 1.0 ? 'text-destructive font-semibold' : 'text-muted-foreground'}>
              {stopBufferPct >= 0 ? `-${formatPct(stopBufferPct)}` : t('dashboard.gaugeBreached')}
            </span>
          ) : (
            <span className="text-muted-foreground/60">{t('dashboard.gaugeNoStop')}</span>
          )}
          {hasTP && distToTPPct !== null ? (
            <span className={distToTPPct <= 0.5 ? 'text-emerald-500 font-bold' : 'text-emerald-600 dark:text-emerald-400'}>
              {distToTPPct <= 0 ? t('dashboard.gaugeTargetHit') : `+${formatPct(distToTPPct)}`}
            </span>
          ) : (
            <span className="text-muted-foreground/60">{t('dashboard.gaugeNoTarget')}</span>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className={`p-2.5 rounded-lg border border-border/60 bg-muted/20 flex flex-col gap-2 ${className}`}>
      {/* Top Labels: SL, Current, TP */}
      <div className="flex items-center justify-between text-xs">
        {/* Stop Loss Side */}
        <div className="flex items-center gap-1.5">
          {hasSL ? (
            <>
              <ShieldAlert className="w-3.5 h-3.5 text-rose-500" />
              <div className="flex flex-col">
                <span className="text-[10px] uppercase font-semibold text-muted-foreground tracking-wider">
                  {t('dashboard.gaugeStopLoss')}
                </span>
                <span className="font-mono font-medium text-rose-500">
                  ${formatNumber(stopLossPrice!, { digits: 2 })}
                </span>
              </div>
            </>
          ) : (
            <div className="flex items-center gap-1 text-muted-foreground/70 text-[11px]">
              <ShieldAlert className="w-3.5 h-3.5 text-amber-500/70" />
              <span>{t('dashboard.gaugeNoStop')}</span>
            </div>
          )}
        </div>

        {/* Current & Entry Center */}
        <div className="flex flex-col items-center">
          <span className="text-[10px] text-muted-foreground font-medium flex items-center gap-1">
            <span>{t('dashboard.gaugeEntry', { price: formatNumber(entryPrice, { digits: 2 }) })}</span>
            <ArrowRight className="w-3 h-3" />
          </span>
          <span className={`font-mono text-xs font-bold ${isProfitable ? 'text-emerald-500' : 'text-destructive'}`}>
            ${formatNumber(currentPrice, { digits: 2 })}
          </span>
        </div>

        {/* Take Profit Side */}
        <div className="flex items-center gap-1.5 justify-end text-right">
          {hasTP ? (
            <>
              <div className="flex flex-col">
                <span className="text-[10px] uppercase font-semibold text-muted-foreground tracking-wider">
                  {t('dashboard.gaugeTakeProfit')}
                </span>
                <span className="font-mono font-medium text-emerald-500">
                  ${formatNumber(takeProfitPrice!, { digits: 2 })}
                </span>
              </div>
              <Target className="w-3.5 h-3.5 text-emerald-500" />
            </>
          ) : (
            <div className="flex items-center gap-1 text-muted-foreground/70 text-[11px]">
              <Target className="w-3.5 h-3.5 text-muted-foreground/50" />
              <span>{t('dashboard.gaugeNoTarget')}</span>
            </div>
          )}
        </div>
      </div>

      {/* Progress Track */}
      <div className="relative h-2 w-full bg-muted rounded-full overflow-hidden">
        {/* Entry boundary line */}
        <div
          className="absolute top-0 bottom-0 w-0.5 bg-foreground/40 z-10"
          style={{ left: `${entryPercent}%` }}
          title={`Entry: $${formatNumber(entryPrice, { digits: 2 })}`}
        />
        {/* Loss range fill */}
        <div
          className="absolute top-0 bottom-0 left-0 bg-rose-500/20"
          style={{ width: `${entryPercent}%` }}
        />
        {/* Profit range fill */}
        <div
          className="absolute top-0 bottom-0 right-0 bg-emerald-500/20"
          style={{ width: `${100 - entryPercent}%` }}
        />
        {/* Animated active cursor */}
        <div
          className={`absolute top-0 bottom-0 w-3 -ml-1.5 rounded-full z-20 transition-all duration-300 ${
            isProfitable
              ? 'bg-emerald-500 shadow-md shadow-emerald-500/60 ring-2 ring-emerald-500/30'
              : 'bg-rose-500 shadow-md shadow-rose-500/60 ring-2 ring-rose-500/30'
          }`}
          style={{ left: `${currentPercent}%` }}
        />
      </div>

      {/* Bottom Sub-stats: Buffer to SL vs Distance to TP */}
      <div className="flex items-center justify-between text-[11px]">
        {hasSL && stopBufferPct !== null ? (
          <span className="flex items-center gap-1 font-medium text-rose-500">
            <ShieldCheck className="w-3 h-3" />
            <span>
              {stopBufferPct >= 0
                ? `${t('dashboard.gaugeBuffer')}: -${formatPct(stopBufferPct)}`
                : t('dashboard.gaugeBreached')}
            </span>
          </span>
        ) : (
          <span className="text-muted-foreground text-[10px]">—</span>
        )}

        {hasTP && distToTPPct !== null ? (
          <span className="flex items-center gap-1 font-medium text-emerald-500">
            <Target className="w-3 h-3" />
            <span>
              {distToTPPct <= 0
                ? t('dashboard.gaugeTargetHit')
                : `${t('dashboard.gaugeToTarget')}: +${formatPct(distToTPPct)}`}
            </span>
          </span>
        ) : (
          <span className="text-muted-foreground text-[10px]">—</span>
        )}
      </div>
    </div>
  );
};
