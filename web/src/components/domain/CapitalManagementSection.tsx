import React, { useState, useEffect } from 'react';
import {
  Wallet,
  RefreshCw,
  RotateCcw,
  Save,
  CheckCircle2,
  AlertCircle,
  ShieldCheck,
  Coins,
  ArrowUpRight,
  Loader2,
  Lock,
  Unlock,
} from 'lucide-react';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { HelpTooltip } from '@/components/ui/HelpTooltip';
import { useToast } from '@/context/ToastContext';
import {
  useAccount,
  useUpdateDryRunAccount,
  useResetDryRunAccount,
  useUpdateLiveAccount,
  useSyncAccount,
  usePlatformSettings,
} from '@/hooks/queries';
import { useT } from '@/i18n';
import { formatUsd } from '@/lib/format';

export function CapitalManagementSection() {
  const t = useT();
  const { toast } = useToast();
  const { data: accountData, isLoading, isError, refetch } = useAccount();
  const { data: settings } = usePlatformSettings();

  const updateDryRunMutation = useUpdateDryRunAccount();
  const resetDryRunMutation = useResetDryRunAccount();
  const updateLiveMutation = useUpdateLiveAccount();
  const syncAccountMutation = useSyncAccount();

  // Mode tab state: 'dryrun' | 'live'
  const isPlatformLive = settings?.mode === 'live' || settings?.mode === 'paper';
  const [activeTab, setActiveTab] = useState<'dryrun' | 'live'>(isPlatformLive ? 'live' : 'dryrun');

  useEffect(() => {
    if (settings?.mode === 'live') {
      setActiveTab('live');
    }
  }, [settings?.mode]);

  // Accounts breakdown
  const dryRunAccount = accountData?.accounts?.['dryrun'] ?? (accountData?.mode === 'dryrun' ? accountData : null);
  const liveAccount = accountData?.accounts?.['live'] ?? (accountData?.mode === 'live' ? accountData : null);

  // Form states
  const [dryRunAmount, setDryRunAmount] = useState<string>('10000');
  const [dryRunOrders, setDryRunOrders] = useState<string>('5');
  const [dryRunCurrency, setDryRunCurrency] = useState<string>('USDT');

  const [liveMaxAlloc, setLiveMaxAlloc] = useState<string>('0');
  const [liveOrders, setLiveOrders] = useState<string>('5');

  const [resetConfirmOpen, setResetConfirmOpen] = useState(false);

  useEffect(() => {
    if (dryRunAccount) {
      setDryRunAmount(String(dryRunAccount.amount ?? 10000));
      setDryRunOrders(String(dryRunAccount.available_orders ?? 5));
      setDryRunCurrency(dryRunAccount.currency ?? 'USDT');
    }
  }, [dryRunAccount?.amount, dryRunAccount?.available_orders, dryRunAccount?.currency]);

  useEffect(() => {
    if (liveAccount) {
      setLiveMaxAlloc(String(liveAccount.max_allocation ?? 0));
      setLiveOrders(String(liveAccount.available_orders ?? 5));
    }
  }, [liveAccount?.max_allocation, liveAccount?.available_orders]);

  const handleSaveDryRun = async (e: React.FormEvent) => {
    e.preventDefault();
    const amt = parseFloat(dryRunAmount);
    const orders = parseInt(dryRunOrders, 10);
    if (isNaN(amt) || amt <= 0) {
      toast(t('capital.errInvalidAmount'), 'error');
      return;
    }
    try {
      await updateDryRunMutation.mutateAsync({
        amount: amt,
        available_orders: isNaN(orders) || orders < 1 ? 5 : orders,
        currency: dryRunCurrency.trim() || 'USDT',
      });
      toast(t('capital.dryRunSaved'), 'success');
      refetch();
    } catch (err: any) {
      toast(err?.message || t('capital.errSave'), 'error');
    }
  };

  const handleConfirmReset = async () => {
    setResetConfirmOpen(false);
    try {
      await resetDryRunMutation.mutateAsync();
      toast(t('capital.dryRunResetSuccess'), 'success');
      refetch();
    } catch (err: any) {
      toast(err?.message || t('capital.errReset'), 'error');
    }
  };

  const handleSaveLiveLimits = async (e: React.FormEvent) => {
    e.preventDefault();
    const maxAlloc = parseFloat(liveMaxAlloc);
    const orders = parseInt(liveOrders, 10);
    try {
      await updateLiveMutation.mutateAsync({
        max_allocation: isNaN(maxAlloc) || maxAlloc < 0 ? 0 : maxAlloc,
        available_orders: isNaN(orders) || orders < 1 ? 5 : orders,
      });
      toast(t('capital.liveSaved'), 'success');
      refetch();
    } catch (err: any) {
      toast(err?.message || t('capital.errSave'), 'error');
    }
  };

  const handleSyncExchange = async () => {
    try {
      await syncAccountMutation.mutateAsync({ mode: settings?.mode === 'paper' ? 'paper' : 'live' });
      toast(t('capital.syncSuccess'), 'success');
      refetch();
    } catch (err: any) {
      toast(err?.message || t('capital.errSync'), 'error');
    }
  };

  const initialAmt = dryRunAccount?.initial_amount ?? 10000;
  const currentAmt = dryRunAccount?.amount ?? 10000;
  const simulatedPnl = currentAmt - initialAmt;
  const simulatedPnlPct = initialAmt > 0 ? (simulatedPnl / initialAmt) * 100 : 0;

  return (
    <Card id="capital-management">
      <CardHeader
        title={t('capital.title')}
        subtitle={t('capital.subtitle')}
      />

      <div className="p-6 pt-0 space-y-6">
        {/* Navigation Tabs */}
        <div className="flex items-center gap-2 border-b border-border pb-3">
          <button
            type="button"
            onClick={() => setActiveTab('dryrun')}
            className={`px-3.5 py-1.5 rounded-lg text-xs font-semibold transition-colors flex items-center gap-2 ${
              activeTab === 'dryrun'
                ? 'bg-primary text-white shadow-sm'
                : 'bg-card/40 text-muted-foreground hover:text-foreground hover:bg-card/80 border border-border/50'
            }`}
          >
            <Coins className="w-3.5 h-3.5" />
            <span>{t('capital.tabDryRun')}</span>
            {settings?.mode === 'dryrun' && (
              <span className="text-[10px] bg-white/20 text-white px-1.5 py-0.2 rounded font-mono">
                {t('capital.active')}
              </span>
            )}
          </button>

          <button
            type="button"
            onClick={() => setActiveTab('live')}
            className={`px-3.5 py-1.5 rounded-lg text-xs font-semibold transition-colors flex items-center gap-2 ${
              activeTab === 'live'
                ? 'bg-primary text-white shadow-sm'
                : 'bg-card/40 text-muted-foreground hover:text-foreground hover:bg-card/80 border border-border/50'
            }`}
          >
            <Wallet className="w-3.5 h-3.5" />
            <span>{t('capital.tabLive')}</span>
            {(settings?.mode === 'live' || settings?.mode === 'paper') && (
              <span className="text-[10px] bg-white/20 text-white px-1.5 py-0.2 rounded font-mono">
                {t('capital.active')}
              </span>
            )}
          </button>
        </div>

        {/* Tab 1: Dry-Run Virtual Capital */}
        {activeTab === 'dryrun' && (
          <div className="space-y-6 animate-in fade-in duration-200">
            {/* Stat Row */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="p-3.5 rounded-xl bg-card/60 border border-border">
                <span className="text-xs text-muted-foreground">{t('capital.virtualBalance')}</span>
                <div className="text-xl font-bold font-mono text-foreground mt-0.5">
                  {formatUsd(currentAmt, { digits: 2 })}
                </div>
                <span className="text-[11px] text-muted-foreground font-mono">
                  {dryRunAccount?.currency ?? 'USDT'}
                </span>
              </div>

              <div className="p-3.5 rounded-xl bg-card/60 border border-border">
                <span className="text-xs text-muted-foreground">{t('capital.startingBalance')}</span>
                <div className="text-xl font-bold font-mono text-foreground mt-0.5">
                  {formatUsd(initialAmt, { digits: 2 })}
                </div>
                <span className="text-[11px] text-muted-foreground">
                  {t('capital.baselineCapital')}
                </span>
              </div>

              <div className="p-3.5 rounded-xl bg-card/60 border border-border">
                <span className="text-xs text-muted-foreground">{t('capital.simulatedPnl')}</span>
                <div
                  className={`text-xl font-bold font-mono mt-0.5 flex items-center gap-1 ${
                    simulatedPnl >= 0 ? 'text-success' : 'text-destructive'
                  }`}
                >
                  {simulatedPnl >= 0 ? '+' : ''}
                  {formatUsd(simulatedPnl, { digits: 2 })}
                  <span className="text-xs font-normal">
                    ({simulatedPnlPct >= 0 ? '+' : ''}{simulatedPnlPct.toFixed(2)}%)
                  </span>
                </div>
                <span className="text-[11px] text-muted-foreground">
                  {dryRunAccount?.available_orders ?? 5} {t('capital.slotsAvailable')}
                </span>
              </div>
            </div>

            {/* Dry-Run Form */}
            <form onSubmit={handleSaveDryRun} className="space-y-4 pt-2">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                    {t('capital.setVirtualAmount')}
                    <HelpTooltip>{t('capital.setVirtualAmountHelp')}</HelpTooltip>
                  </label>
                  <input
                    type="number"
                    step="100"
                    min="1"
                    value={dryRunAmount}
                    onChange={(e) => setDryRunAmount(e.target.value)}
                    className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
                    placeholder="10000"
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                    {t('capital.maxOrders')}
                    <HelpTooltip>{t('capital.maxOrdersHelp')}</HelpTooltip>
                  </label>
                  <input
                    type="number"
                    step="1"
                    min="1"
                    max="50"
                    value={dryRunOrders}
                    onChange={(e) => setDryRunOrders(e.target.value)}
                    className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
                    placeholder="5"
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-foreground">
                    {t('capital.currency')}
                  </label>
                  <input
                    type="text"
                    value={dryRunCurrency}
                    onChange={(e) => setDryRunCurrency(e.target.value.toUpperCase())}
                    className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
                    placeholder="USDT"
                  />
                </div>
              </div>

              <div className="flex items-center justify-between pt-2">
                <button
                  type="button"
                  onClick={() => setResetConfirmOpen(true)}
                  disabled={resetDryRunMutation.isPending}
                  className="px-3.5 py-2 rounded-lg text-xs font-medium text-destructive hover:bg-destructive/10 border border-destructive/30 flex items-center gap-1.5 transition-colors disabled:opacity-50"
                >
                  <RotateCcw className="w-3.5 h-3.5" />
                  <span>{t('capital.resetToInitial')}</span>
                </button>

                <button
                  type="submit"
                  disabled={updateDryRunMutation.isPending}
                  className="px-4 py-2 rounded-lg bg-primary hover:bg-primary text-xs font-semibold text-white shadow flex items-center gap-2 transition-colors disabled:opacity-50"
                >
                  {updateDryRunMutation.isPending ? (
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  ) : (
                    <Save className="w-3.5 h-3.5" />
                  )}
                  <span>{t('capital.saveVirtual')}</span>
                </button>
              </div>
            </form>
          </div>
        )}

        {/* Tab 2: Binance Live Exchange Funds */}
        {activeTab === 'live' && (
          <div className="space-y-6 animate-in fade-in duration-200">
            {/* Connection Banner */}
            <div className="p-3.5 rounded-xl bg-card/60 border border-border flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
              <div className="flex items-center gap-2.5">
                <div className="w-8 h-8 rounded-lg bg-emerald-500/15 border border-emerald-500/30 flex items-center justify-center shrink-0">
                  <ShieldCheck className="w-4 h-4 text-emerald-400" />
                </div>
                <div>
                  <div className="text-xs font-bold text-foreground">
                    {settings?.testnet ? t('capital.binanceTestnet') : t('capital.binanceLive')}
                  </div>
                  <div className="text-[11px] text-muted-foreground">
                    {liveAccount?.last_synced_at
                      ? t('capital.lastSynced', { time: new Date(liveAccount.last_synced_at).toLocaleString() })
                      : t('capital.neverSynced')}
                  </div>
                </div>
              </div>

              <button
                type="button"
                onClick={handleSyncExchange}
                disabled={syncAccountMutation.isPending}
                className="px-3.5 py-1.5 rounded-lg bg-secondary hover:bg-secondary/80 text-foreground text-xs font-semibold border border-border flex items-center gap-2 transition-colors disabled:opacity-50"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${syncAccountMutation.isPending ? 'animate-spin' : ''}`} />
                <span>{t('capital.syncNow')}</span>
              </button>
            </div>

            {/* Live Balances Grid */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="p-3.5 rounded-xl bg-card/60 border border-border">
                <span className="text-xs text-muted-foreground flex items-center gap-1.5">
                  <Unlock className="w-3.5 h-3.5 text-emerald-400" />
                  {t('capital.freeBalance')}
                </span>
                <div className="text-xl font-bold font-mono text-emerald-400 mt-1">
                  {formatUsd(liveAccount?.amount ?? 0, { digits: 2 })}
                </div>
                <span className="text-[11px] text-muted-foreground">
                  {t('capital.availableToTrade')}
                </span>
              </div>

              <div className="p-3.5 rounded-xl bg-card/60 border border-border">
                <span className="text-xs text-muted-foreground flex items-center gap-1.5">
                  <Lock className="w-3.5 h-3.5 text-amber-400" />
                  {t('capital.lockedBalance')}
                </span>
                <div className="text-xl font-bold font-mono text-amber-400 mt-1">
                  {formatUsd(liveAccount?.locked_amount ?? 0, { digits: 2 })}
                </div>
                <span className="text-[11px] text-muted-foreground">
                  {t('capital.inActiveOrders')}
                </span>
              </div>

              <div className="p-3.5 rounded-xl bg-card/60 border border-border">
                <span className="text-xs text-muted-foreground flex items-center gap-1.5">
                  <Wallet className="w-3.5 h-3.5 text-primary" />
                  {t('capital.totalWallet')}
                </span>
                <div className="text-xl font-bold font-mono text-foreground mt-1">
                  {formatUsd((liveAccount?.amount ?? 0) + (liveAccount?.locked_amount ?? 0), { digits: 2 })}
                </div>
                <span className="text-[11px] text-muted-foreground">
                  {liveAccount?.currency ?? 'USDT'}
                </span>
              </div>
            </div>

            {/* Risk & Allocation Form */}
            <form onSubmit={handleSaveLiveLimits} className="space-y-4 pt-2">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                    {t('capital.maxAllocation')}
                    <HelpTooltip>{t('capital.maxAllocationHelp')}</HelpTooltip>
                  </label>
                  <input
                    type="number"
                    step="100"
                    min="0"
                    value={liveMaxAlloc}
                    onChange={(e) => setLiveMaxAlloc(e.target.value)}
                    className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
                    placeholder="0"
                  />
                  <p className="text-[11px] text-muted-foreground">
                    {parseFloat(liveMaxAlloc) > 0
                      ? t('capital.allocationCapped', { amount: formatUsd(parseFloat(liveMaxAlloc) || 0) })
                      : t('capital.allocationUnlimited')}
                  </p>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                    {t('capital.maxOrders')}
                    <HelpTooltip>{t('capital.maxOrdersHelp')}</HelpTooltip>
                  </label>
                  <input
                    type="number"
                    step="1"
                    min="1"
                    max="50"
                    value={liveOrders}
                    onChange={(e) => setLiveOrders(e.target.value)}
                    className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary font-mono"
                    placeholder="5"
                  />
                  <p className="text-[11px] text-muted-foreground">
                    {t('capital.maxOrdersNote')}
                  </p>
                </div>
              </div>

              <div className="flex items-center justify-end pt-2">
                <button
                  type="submit"
                  disabled={updateLiveMutation.isPending}
                  className="px-4 py-2 rounded-lg bg-primary hover:bg-primary text-xs font-semibold text-white shadow flex items-center gap-2 transition-colors disabled:opacity-50"
                >
                  {updateLiveMutation.isPending ? (
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  ) : (
                    <Save className="w-3.5 h-3.5" />
                  )}
                  <span>{t('capital.saveLiveLimits')}</span>
                </button>
              </div>
            </form>
          </div>
        )}
      </div>

      {/* Confirmation Dialog for Resetting Dry Run Funds */}
      <ConfirmDialog
        isOpen={resetConfirmOpen}
        title={t('capital.resetTitle')}
        message={t('capital.resetMessage', { amount: formatUsd(initialAmt) })}
        isDangerous={true}
        confirmText={t('capital.confirmReset')}
        onConfirm={handleConfirmReset}
        onCancel={() => setResetConfirmOpen(false)}
      />
    </Card>
  );
}
