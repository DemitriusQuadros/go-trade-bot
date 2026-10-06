import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import {
  FlaskConical,
  Coins,
  Wallet,
  RefreshCw,
  ArrowUpRight,
  RotateCcw,
  TrendingUp,
  TrendingDown,
  ShieldCheck,
  AlertCircle,
  SlidersHorizontal,
} from 'lucide-react';
import { Account } from '@/api/types';
import { Card } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { useResetDryRunAccount } from '@/hooks/queries';
import { useToast } from '@/context/ToastContext';
import { formatUsd, formatNumber, formatPct } from '@/lib/format';
import { useT } from '@/i18n';
import { DashboardViewMode } from './DashboardEnvironmentSwitcher';

interface DashboardCapitalHeroProps {
  activeView: DashboardViewMode;
  account?: Account;
  serverMode?: string;
  dryRunPositionsCount: number;
  dryRunUnrealizedPnL: number;
  /** Cost basis of open dry-run positions (capital already deducted from the cash balance). */
  dryRunInvestedCapital: number;
  livePositionsCount: number;
  liveUnrealizedPnL: number;
  onSyncExchange: () => Promise<void>;
  isSyncing: boolean;
  onAccountRefetch?: () => void;
}

export function DashboardCapitalHero({
  activeView,
  account,
  serverMode,
  dryRunPositionsCount,
  dryRunUnrealizedPnL,
  dryRunInvestedCapital,
  livePositionsCount,
  liveUnrealizedPnL,
  onSyncExchange,
  isSyncing,
  onAccountRefetch,
}: DashboardCapitalHeroProps) {
  const t = useT();
  const { toast } = useToast();
  const resetDryRunMutation = useResetDryRunAccount();
  const [resetDialogOpen, setResetDialogOpen] = useState(false);

  // Accounts resolution
  const dryRunAcc = account?.accounts?.['dryrun'] ?? (account?.mode === 'dryrun' ? account : null);
  const liveAcc = account?.accounts?.['live'] ?? (account?.mode === 'live' ? account : null);

  // Dry-run calculations
  const dryAmount = dryRunAcc ? (dryRunAcc.amount ?? (dryRunAcc as any).Amount ?? 0) : 0;
  const dryInitial = dryRunAcc?.initial_amount ?? 10000;
  // The dry-run balance is cash only: opening a position deducts its cost, so
  // "balance - initial" shows capital locked in open positions as a loss.
  const dryCashPnl = dryAmount - dryInitial;
  const dryCashRoiPct = dryInitial > 0 ? (dryCashPnl / dryInitial) * 100 : 0;
  // Portfolio value = cash + cost basis of open positions + their unrealized P&L.
  const dryEquity = dryAmount + dryRunInvestedCapital + dryRunUnrealizedPnL;
  const dryNetPnl = dryEquity - dryInitial;
  const dryRoiPct = dryInitial > 0 ? (dryNetPnl / dryInitial) * 100 : 0;
  const dryCurrency = dryRunAcc?.currency || 'USDT';
  const dryMaxOrders = dryRunAcc?.available_orders ?? 5;

  // Live calculations
  const liveFree = liveAcc ? (liveAcc.amount ?? (liveAcc as any).Amount ?? 0) : 0;
  const liveLocked = liveAcc?.locked_amount ?? 0;
  const liveTotal = liveFree + liveLocked;
  const liveCurrency = liveAcc?.currency || 'USDT';
  const liveMaxAlloc = liveAcc?.max_allocation ?? 0;
  const liveMaxOrders = liveAcc?.available_orders ?? 5;
  const lastSynced = liveAcc?.last_synced_at;

  const handleConfirmReset = async () => {
    setResetDialogOpen(false);
    try {
      await resetDryRunMutation.mutateAsync();
      toast(t('capital.dryRunResetSuccess'), 'success');
      onAccountRefetch?.();
    } catch (err: any) {
      toast(err?.message || t('capital.errReset'), 'error');
    }
  };

  // 1. Unified Side-by-Side Dual Card View
  if (activeView === 'unified') {
    return (
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Left: Virtual Sandbox Card */}
        <Card className="relative overflow-hidden border-border/70 hover:border-border transition-colors">
          <div className="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-violet-500 to-indigo-500" />
          
          <div className="p-5 flex flex-col justify-between h-full space-y-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <div className="w-8 h-8 rounded-lg bg-violet-500/10 border border-violet-500/20 flex items-center justify-center text-violet-400">
                  <FlaskConical className="w-4 h-4" />
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-foreground flex items-center gap-1.5">
                    {t('dashboard.virtualSandboxTitle')}
                  </h3>
                  <span className="text-[10px] text-muted-foreground">
                    {t('dashboard.startingBaseline', { amount: formatUsd(dryInitial, { digits: 0 }) })}
                  </span>
                </div>
              </div>

              <span className="px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider bg-violet-500/10 text-violet-400 border border-violet-500/20 border-dashed">
                {t('dashboard.simulatedTag')}
              </span>
            </div>

            {/* Balances */}
            <div className="flex items-baseline justify-between pt-1">
              <div>
                <div className="flex items-baseline gap-1.5">
                  <span className="text-2xl font-extrabold text-foreground tracking-tight">
                    {formatUsd(dryAmount, { digits: 2 })}
                  </span>
                  <span className="text-xs font-mono text-muted-foreground">{dryCurrency}</span>
                </div>
                <div className="flex items-center gap-1.5 mt-1 text-xs">
                  {dryNetPnl >= 0 ? (
                    <span className="text-emerald-500 flex items-center gap-0.5 font-semibold">
                      <TrendingUp className="w-3.5 h-3.5" />
                      +{formatUsd(dryNetPnl, { digits: 2 })} ({formatPct(dryRoiPct)})
                    </span>
                  ) : (
                    <span className="text-destructive flex items-center gap-0.5 font-semibold">
                      <TrendingDown className="w-3.5 h-3.5" />
                      {formatUsd(dryNetPnl, { digits: 2 })} ({formatPct(dryRoiPct)})
                    </span>
                  )}
                  <span className="text-muted-foreground">• {t('capital.simulatedPnl')}</span>
                </div>
                <div
                  className="flex flex-wrap items-center gap-x-2 mt-1 text-[11px] text-muted-foreground"
                  title={t('capital.cashPnlHint')}
                >
                  <span>
                    {t('capital.cashPnl')}:{' '}
                    <strong className={dryCashPnl >= 0 ? 'text-emerald-500' : 'text-destructive'}>
                      {formatUsd(dryCashPnl, { digits: 2, signed: true })} ({formatPct(dryCashRoiPct)})
                    </strong>
                  </span>
                  <span>•</span>
                  <span>
                    {t('capital.inOpenPositions')}:{' '}
                    <strong className="text-foreground">{formatUsd(dryRunInvestedCapital, { digits: 2 })}</strong>
                  </span>
                </div>
              </div>
            </div>

            {/* Bottom Telemetry & Actions */}
            <div className="pt-3 border-t border-border/50 flex items-center justify-between text-xs">
              <div className="flex items-center gap-3 text-muted-foreground">
                <span>
                  {t('dashboard.openPositionsCount', { count: dryRunPositionsCount })}
                </span>
                <span>•</span>
                <span>
                  {t('capital.virtualSlots', { count: dryMaxOrders })}
                </span>
              </div>

              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setResetDialogOpen(true)}
                  title={t('capital.resetToInitial')}
                  className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted transition-colors text-[11px] flex items-center gap-1"
                >
                  <RotateCcw className="w-3 h-3" />
                  <span>{t('dashboard.resetWallet')}</span>
                </button>
                <Link
                  to="/settings#capital-management"
                  className="text-primary hover:underline text-[11px] font-semibold flex items-center gap-1"
                >
                  <span>{t('dashboard.adjustFunds')}</span>
                  <ArrowUpRight className="w-3 h-3" />
                </Link>
              </div>
            </div>
          </div>
        </Card>

        {/* Right: Binance Spot Account Card */}
        <Card className="relative overflow-hidden border-border/70 hover:border-border transition-colors">
          <div className="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-amber-500 to-yellow-400" />
          
          <div className="p-5 flex flex-col justify-between h-full space-y-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <div className="w-8 h-8 rounded-lg bg-amber-500/10 border border-amber-500/20 flex items-center justify-center text-amber-500">
                  <Coins className="w-4 h-4" />
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-foreground flex items-center gap-1.5">
                    {t('dashboard.binanceSpotTitle')}
                  </h3>
                  <span className="text-[10px] text-muted-foreground">
                    {lastSynced
                      ? t('capital.lastSynced', { time: new Date(lastSynced).toLocaleTimeString() })
                      : t('capital.neverSynced')}
                  </span>
                </div>
              </div>

              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={onSyncExchange}
                  disabled={isSyncing}
                  title={t('capital.syncNow')}
                  className="p-1.5 rounded-lg border border-border/60 bg-muted/40 hover:bg-muted text-muted-foreground hover:text-foreground transition-all disabled:opacity-50 flex items-center gap-1 text-[11px]"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${isSyncing ? 'animate-spin text-amber-500' : ''}`} />
                  <span className="hidden sm:inline">{t('dashboard.syncBinance')}</span>
                </button>

                <span className="px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider bg-amber-500/10 text-amber-500 border border-amber-500/30">
                  {t('dashboard.realFundsTag')}
                </span>
              </div>
            </div>

            {/* Balances */}
            <div className="flex items-baseline justify-between pt-1">
              <div>
                <div className="flex items-baseline gap-1.5">
                  <span className="text-2xl font-extrabold text-foreground tracking-tight">
                    {formatUsd(liveTotal, { digits: 2 })}
                  </span>
                  <span className="text-xs font-mono text-muted-foreground">{liveCurrency}</span>
                </div>
                <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                  <span>
                    {t('dashboard.freeFunds')}: <strong className="text-foreground">{formatUsd(liveFree, { digits: 2 })}</strong>
                  </span>
                  <span>•</span>
                  <span>
                    {t('dashboard.lockedFunds')}: <strong className="text-foreground">{formatUsd(liveLocked, { digits: 2 })}</strong>
                  </span>
                </div>
              </div>
            </div>

            {/* Bottom Telemetry & Actions */}
            <div className="pt-3 border-t border-border/50 flex items-center justify-between text-xs">
              <div className="flex items-center gap-2 text-muted-foreground">
                <ShieldCheck className="w-3.5 h-3.5 text-amber-500" />
                <span className="text-[11px]">
                  {liveMaxAlloc > 0
                    ? t('dashboard.allocationCap', { amount: formatUsd(liveMaxAlloc, { digits: 0 }) })
                    : t('dashboard.allocationUnlimited')}
                </span>
              </div>

              <Link
                to="/settings#capital-management"
                className="text-primary hover:underline text-[11px] font-semibold flex items-center gap-1"
              >
                <span>{t('capital.manage')}</span>
                <ArrowUpRight className="w-3 h-3" />
              </Link>
            </div>
          </div>
        </Card>

        {/* Confirm Reset Dialog */}
        <ConfirmDialog
          isOpen={resetDialogOpen}
          title={t('capital.resetTitle')}
          message={t('capital.resetMessage', { amount: formatUsd(dryInitial, { digits: 2 }) })}
          confirmText={t('capital.confirmReset')}
          isDangerous={true}
          onConfirm={handleConfirmReset}
          onCancel={() => setResetDialogOpen(false)}
        />
      </div>
    );
  }

  // 2. Focused Dry-Run Sandbox View
  if (activeView === 'dryrun') {
    return (
      <Card className="relative overflow-hidden border-violet-500/30 bg-card/80">
        <div className="absolute top-0 left-0 right-0 h-1.5 bg-gradient-to-r from-violet-500 via-indigo-500 to-purple-500" />
        
        <div className="p-6 space-y-6">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-xl bg-violet-500/10 border border-violet-500/30 flex items-center justify-center text-violet-400">
                <FlaskConical className="w-5 h-5" />
              </div>
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="text-base font-bold text-foreground">
                    {t('dashboard.virtualSandboxTitle')}
                  </h2>
                  <span className="px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider bg-violet-500/15 text-violet-400 border border-violet-500/30 border-dashed">
                    {t('dashboard.simulatedTag')}
                  </span>
                </div>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {t('capital.setVirtualAmountHelp')}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setResetDialogOpen(true)}
                className="px-3 py-1.5 rounded-lg border border-border/80 bg-background hover:bg-muted text-muted-foreground hover:text-foreground text-xs font-medium transition-colors flex items-center gap-1.5"
              >
                <RotateCcw className="w-3.5 h-3.5" />
                <span>{t('capital.resetToInitial')}</span>
              </button>
              <Link
                to="/settings#capital-management"
                className="px-3 py-1.5 rounded-lg bg-violet-600 hover:bg-violet-700 text-white text-xs font-semibold shadow-sm transition-colors flex items-center gap-1.5"
              >
                <SlidersHorizontal className="w-3.5 h-3.5" />
                <span>{t('dashboard.adjustFunds')}</span>
              </Link>
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4 p-4 rounded-xl bg-muted/20 border border-border/40">
            <div>
              <span className="text-xs text-muted-foreground font-medium">{t('capital.virtualBalance')}</span>
              <div className="flex items-baseline gap-1 mt-1">
                <span className="text-2xl font-black text-foreground">
                  {formatUsd(dryAmount, { digits: 2 })}
                </span>
                <span className="text-xs font-mono text-muted-foreground">{dryCurrency}</span>
              </div>
              <span className="text-[10px] text-muted-foreground">
                {t('capital.portfolioValue')}: {formatUsd(dryEquity, { digits: 2 })}
              </span>
            </div>

            <div>
              <span className="text-xs text-muted-foreground font-medium">{t('capital.simulatedPnl')}</span>
              <div className="flex items-baseline gap-1.5 mt-1">
                {dryNetPnl >= 0 ? (
                  <span className="text-xl font-bold text-emerald-500 flex items-center gap-0.5">
                    +{formatUsd(dryNetPnl, { digits: 2 })}
                  </span>
                ) : (
                  <span className="text-xl font-bold text-destructive flex items-center gap-0.5">
                    {formatUsd(dryNetPnl, { digits: 2 })}
                  </span>
                )}
                <span className="text-xs font-semibold text-muted-foreground">({formatPct(dryRoiPct)})</span>
              </div>
              <span className="text-[10px] text-muted-foreground" title={t('capital.cashPnlHint')}>
                {t('capital.cashPnl')}: {formatUsd(dryCashPnl, { digits: 2, signed: true })} ({formatPct(dryCashRoiPct)}) •{' '}
                {t('capital.inOpenPositions')}: {formatUsd(dryRunInvestedCapital, { digits: 2 })}
              </span>
            </div>

              <div>
                <span className="text-xs text-muted-foreground font-medium">{t('dashboard.openPositionsCard')}</span>
                <div className="text-xl font-bold text-foreground mt-1">
                  {t('dashboard.openPositionsCount', { count: dryRunPositionsCount })}
                </div>
              </div>

            <div>
              <span className="text-xs text-muted-foreground font-medium">{t('capital.maxOrders')}</span>
              <div className="text-xl font-bold text-foreground mt-1">
                {dryMaxOrders} <span className="text-xs font-normal text-muted-foreground">{t('capital.slotsAvailable')}</span>
              </div>
            </div>
          </div>
        </div>

        <ConfirmDialog
          isOpen={resetDialogOpen}
          title={t('capital.resetTitle')}
          message={t('capital.resetMessage', { amount: formatUsd(dryInitial, { digits: 2 }) })}
          confirmText={t('capital.confirmReset')}
          isDangerous={true}
          onConfirm={handleConfirmReset}
          onCancel={() => setResetDialogOpen(false)}
        />
      </Card>
    );
  }

  // 3. Focused Live Binance Account View
  return (
    <Card className="relative overflow-hidden border-amber-500/30 bg-card/80">
      <div className="absolute top-0 left-0 right-0 h-1.5 bg-gradient-to-r from-amber-500 via-yellow-400 to-amber-600" />
      
      <div className="p-6 space-y-6">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-amber-500/10 border border-amber-500/30 flex items-center justify-center text-amber-500">
              <Coins className="w-5 h-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-base font-bold text-foreground">
                  {t('dashboard.binanceSpotTitle')}
                </h2>
                <span className="px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-500 border border-amber-500/30">
                  {t('dashboard.realFundsTag')}
                </span>
              </div>
              <p className="text-xs text-muted-foreground mt-0.5">
                {lastSynced
                  ? t('capital.lastSynced', { time: new Date(lastSynced).toLocaleString() })
                  : t('capital.neverSynced')}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onSyncExchange}
              disabled={isSyncing}
              className="px-3.5 py-1.5 rounded-lg border border-amber-500/40 bg-amber-500/10 hover:bg-amber-500/20 text-amber-500 text-xs font-semibold shadow-sm transition-all disabled:opacity-50 flex items-center gap-1.5"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${isSyncing ? 'animate-spin' : ''}`} />
              <span>{isSyncing ? t('common.loading') : t('capital.syncNow')}</span>
            </button>
            <Link
              to="/settings#capital-management"
              className="px-3 py-1.5 rounded-lg border border-border/80 bg-background hover:bg-muted text-muted-foreground hover:text-foreground text-xs font-medium transition-colors flex items-center gap-1.5"
            >
              <SlidersHorizontal className="w-3.5 h-3.5" />
              <span>{t('capital.manage')}</span>
            </Link>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4 p-4 rounded-xl bg-muted/20 border border-border/40">
          <div>
            <span className="text-xs text-muted-foreground font-medium">{t('dashboard.totalEquity')}</span>
            <div className="flex items-baseline gap-1 mt-1">
              <span className="text-2xl font-black text-foreground">
                {formatUsd(liveTotal, { digits: 2 })}
              </span>
              <span className="text-xs font-mono text-muted-foreground">{liveCurrency}</span>
            </div>
          </div>

          <div>
            <span className="text-xs text-muted-foreground font-medium">{t('capital.freeBalance')}</span>
            <div className="text-xl font-bold text-foreground mt-1">
              {formatUsd(liveFree, { digits: 2 })}
            </div>
            <span className="text-[10px] text-muted-foreground">{t('capital.availableToTrade')}</span>
          </div>

          <div>
            <span className="text-xs text-muted-foreground font-medium">{t('capital.lockedBalance')}</span>
            <div className="text-xl font-bold text-foreground mt-1">
              {formatUsd(liveLocked, { digits: 2 })}
            </div>
            <span className="text-[10px] text-muted-foreground">{t('capital.inActiveOrders')}</span>
          </div>

          <div>
            <span className="text-xs text-muted-foreground font-medium">{t('capital.maxAllocation')}</span>
            <div className="text-xl font-bold text-foreground mt-1">
              {liveMaxAlloc > 0 ? formatUsd(liveMaxAlloc, { digits: 0 }) : '100%'}
            </div>
            <span className="text-[10px] text-muted-foreground">
              {liveMaxAlloc > 0 ? t('capital.allocationCapped', { amount: formatUsd(liveMaxAlloc, { digits: 0 }) }) : t('capital.allocationUnlimited')}
            </span>
          </div>
        </div>
      </div>
    </Card>
  );
}
