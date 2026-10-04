import React, { useState, useMemo } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { 
  useAccount, 
  useStrategies, 
  useSignals, 
  usePerformanceSnapshots,
  usePlatformSettings,
  useSyncAccount,
} from '@/hooks/queries';
import { useSSE } from '@/hooks/useSSE';
import { useToast } from '@/context/ToastContext';
import { Card, CardHeader, MetricCard } from '@/components/ui/Card';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen } from '@/components/ui/Spinner';
import { PnlHistoryChart } from '@/components/charts/PnlHistoryChart';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/Table';
import { DashboardEnvironmentSwitcher, DashboardViewMode } from '@/components/domain/DashboardEnvironmentSwitcher';
import { DashboardCapitalHero } from '@/components/domain/DashboardCapitalHero';
import {
  TrendingUp,
  Cpu,
  Layers,
  FlaskConical,
  Coins,
  ExternalLink,
} from 'lucide-react';
import { formatNumber, formatPct, formatPrice, formatUsd } from '@/lib/format';
import { useT } from '@/i18n';

export function Dashboard() {
  const t = useT();
  const { toast } = useToast();
  const [searchParams, setSearchParams] = useSearchParams();

  // Environment lens: 'unified' | 'dryrun' | 'live'
  const initialEnv = (searchParams.get('env') as DashboardViewMode) || 'unified';
  const [activeView, setActiveView] = useState<DashboardViewMode>(
    ['unified', 'dryrun', 'live'].includes(initialEnv) ? initialEnv : 'unified'
  );

  // Sub-filters for tables
  const [positionsTab, setPositionsTab] = useState<'all' | 'dryrun' | 'live'>('all');
  const [strategiesTab, setStrategiesTab] = useState<'all' | 'dryrun' | 'live'>('all');

  const { data: account, isLoading: isAccountLoading, refetch: refetchAccount } = useAccount();
  const { data: settings } = usePlatformSettings();
  const syncAccountMutation = useSyncAccount();
  const { data: strategies = [], isLoading: isStrategiesLoading, refetch: refetchStrategies } = useStrategies();
  const { data: openSignals = [], isLoading: isSignalsLoading, refetch: refetchSignals } = useSignals('open');
  const { data: performance = [], isLoading: isPerformanceLoading, refetch: refetchPerformance } = usePerformanceSnapshots();

  const { prices: ssePrices, positions: ssePositions, connected: sseConnected } = useSSE(true);

  // Merge REST tickers with SSE ticker updates
  const tickers = useMemo(() => {
    const map = new Map<string, number>();
    Object.values(ssePrices).forEach((p) => {
      map.set(p.symbol, p.price);
    });
    return Array.from(map.entries()).map(([Symbol, Price]) => ({ Symbol, Price }));
  }, [ssePrices]);

  // Derive P&L and positions combining REST data with real-time SSE updates & strategy linkage
  const positionsWithPnL = useMemo(() => {
    return openSignals.map((signal) => {
      const strat = strategies.find((s) => s.id === signal.strategy_id);
      const effectiveMode: 'dryrun' | 'live' | 'paper' =
        ((signal.mode as any) || (strat?.mode as any) || 'dryrun').toLowerCase();

      const entryOrder = signal.orders?.[0];
      const brokerOrderId =
        entryOrder?.broker_order_id || (effectiveMode === 'dryrun' ? `SIM-${signal.id}` : undefined);

      let currentPrice = 0;
      let unrealizedPnL = 0;
      let unrealizedPnLPct = 0;
      let quantity = 0;
      let entryPrice = 0;

      const ssePos = ssePositions[signal.id];
      const tickerPrice = tickers.find((t) => t.Symbol === signal.symbol)?.Price;

      if (ssePos) {
        currentPrice = ssePos.current_price;
        unrealizedPnL = ssePos.unrealized_pnl;
        unrealizedPnLPct = ssePos.unrealized_pnl_pct;
        quantity = ssePos.quantity;
        entryPrice = ssePos.entry_price;
      } else if (entryOrder) {
        entryPrice = entryOrder.entry_price;
        quantity = entryOrder.quantity;
        currentPrice = tickerPrice || entryPrice;

        const valueDiff = (currentPrice - entryPrice) * quantity;
        unrealizedPnL = valueDiff;
        unrealizedPnLPct = entryPrice > 0 ? (unrealizedPnL / (entryPrice * quantity)) * 100 : 0;
      }

      return {
        id: signal.id,
        symbol: signal.symbol,
        strategy_id: signal.strategy_id,
        strategy_name: strat?.name || strat?.strategy_name || `#${signal.strategy_id}`,
        mode: effectiveMode,
        brokerOrderId,
        entryPrice,
        currentPrice,
        quantity,
        unrealizedPnL,
        unrealizedPnLPct,
      };
    }).filter((pos) => pos.quantity > 0);
  }, [openSignals, ssePositions, tickers, strategies]);

  // Breakdowns by environment
  const dryRunPositions = useMemo(
    () => positionsWithPnL.filter((p) => p.mode === 'dryrun'),
    [positionsWithPnL]
  );
  const livePositions = useMemo(
    () => positionsWithPnL.filter((p) => p.mode === 'live' || p.mode === 'paper'),
    [positionsWithPnL]
  );

  const dryRunUnrealizedPnL = useMemo(
    () => dryRunPositions.reduce((sum, pos) => sum + pos.unrealizedPnL, 0),
    [dryRunPositions]
  );
  const liveUnrealizedPnL = useMemo(
    () => livePositions.reduce((sum, pos) => sum + pos.unrealizedPnL, 0),
    [livePositions]
  );
  const totalUnrealizedPnL = useMemo(
    () => positionsWithPnL.reduce((sum, pos) => sum + pos.unrealizedPnL, 0),
    [positionsWithPnL]
  );

  // Filtered positions table view
  const displayPositions = useMemo(() => {
    if (positionsTab === 'dryrun') return dryRunPositions;
    if (positionsTab === 'live') return livePositions;
    if (activeView === 'dryrun') return dryRunPositions;
    if (activeView === 'live') return livePositions;
    return positionsWithPnL;
  }, [positionsTab, activeView, dryRunPositions, livePositions, positionsWithPnL]);

  // Filtered strategies
  const dryRunStrategies = useMemo(
    () => strategies.filter((s) => s.mode === 'dryrun'),
    [strategies]
  );
  const liveStrategies = useMemo(
    () => strategies.filter((s) => s.mode === 'live' || s.mode === 'paper'),
    [strategies]
  );

  const displayStrategies = useMemo(() => {
    if (strategiesTab === 'dryrun') return dryRunStrategies;
    if (strategiesTab === 'live') return liveStrategies;
    if (activeView === 'dryrun') return dryRunStrategies;
    if (activeView === 'live') return liveStrategies;
    return strategies;
  }, [strategiesTab, activeView, dryRunStrategies, liveStrategies, strategies]);

  const pnlChartData = useMemo(() => {
    return performance
      .map((p) => ({
        periodStart: p.snapshot_date,
        profit: p.realized_pnl,
      }))
      .sort((a, b) => new Date(a.periodStart).getTime() - new Date(b.periodStart).getTime());
  }, [performance]);

  if (isAccountLoading && isStrategiesLoading) {
    return <LoadingScreen message={t('dashboard.loading')} />;
  }

  const handleRefresh = () => {
    refetchAccount();
    refetchStrategies();
    refetchSignals();
    refetchPerformance();
  };

  const handleViewChange = (view: DashboardViewMode) => {
    setActiveView(view);
    setSearchParams({ env: view });
    if (view === 'unified') {
      setPositionsTab('all');
      setStrategiesTab('all');
    } else if (view === 'dryrun') {
      setPositionsTab('dryrun');
      setStrategiesTab('dryrun');
    } else if (view === 'live') {
      setPositionsTab('live');
      setStrategiesTab('live');
    }
  };

  const handleSyncExchange = async () => {
    try {
      await syncAccountMutation.mutateAsync({ mode: 'live' });
      toast(t('capital.syncSuccess'), 'success');
      refetchAccount();
    } catch (err: any) {
      toast(err?.message || t('capital.errSync'), 'error');
    }
  };

  // Dynamic Open P&L metric values based on active environment lens
  const currentOpenPnl =
    activeView === 'dryrun'
      ? dryRunUnrealizedPnL
      : activeView === 'live'
      ? liveUnrealizedPnL
      : totalUnrealizedPnL;

  const currentOpenPositionsCount =
    activeView === 'dryrun'
      ? dryRunPositions.length
      : activeView === 'live'
      ? livePositions.length
      : positionsWithPnL.length;

  const activeStrategiesCount =
    activeView === 'dryrun'
      ? dryRunStrategies.filter((s) => s.status === 'productive').length
      : activeView === 'live'
      ? liveStrategies.filter((s) => s.status === 'productive').length
      : strategies.filter((s) => s.status === 'productive').length;

  const totalStrategiesInLens =
    activeView === 'dryrun'
      ? dryRunStrategies.length
      : activeView === 'live'
      ? liveStrategies.length
      : strategies.length;

  return (
    <div className="space-y-6">
      {/* Page Title & Environment Switcher Header */}
      <div className="space-y-4">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">{t('dashboard.title')}</h1>
            <p className="text-sm text-muted-foreground mt-1">{t('dashboard.subtitle')}</p>
          </div>
        </div>

        {/* Global Environment Lens Switcher */}
        <DashboardEnvironmentSwitcher
          activeView={activeView}
          onViewChange={handleViewChange}
          serverMode={settings?.mode}
          sseConnected={sseConnected}
          onRefresh={handleRefresh}
          isRefreshing={isAccountLoading || isStrategiesLoading || isSignalsLoading}
        />
      </div>

      {/* Capital & Portfolio Hero Section (Dual card in Unified, focused in Dry-run/Live) */}
      <DashboardCapitalHero
        activeView={activeView}
        account={account}
        serverMode={settings?.mode}
        dryRunPositionsCount={dryRunPositions.length}
        dryRunUnrealizedPnL={dryRunUnrealizedPnL}
        livePositionsCount={livePositions.length}
        liveUnrealizedPnL={liveUnrealizedPnL}
        onSyncExchange={handleSyncExchange}
        isSyncing={syncAccountMutation.isPending}
        onAccountRefetch={refetchAccount}
      />

      {/* 3 Mode-Aware Telemetry Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <MetricCard
          title={t('dashboard.openPnl')}
          value={formatUsd(currentOpenPnl, { digits: 2 })}
          isPositive={currentOpenPnl >= 0}
          change={formatNumber(currentOpenPnl, { digits: 2, signed: true })}
          subtitle={
            activeView === 'unified'
              ? `${dryRunPositions.length} dry-run • ${livePositions.length} live`
              : t('dashboard.openPositionsCount', { count: currentOpenPositionsCount })
          }
          icon={<TrendingUp className="w-4 h-4 text-primary" />}
        />

        <MetricCard
          title={t('dashboard.activeStrategies')}
          value={`${activeStrategiesCount} / ${totalStrategiesInLens}`}
          subtitle={
            activeView === 'dryrun'
              ? t('dashboard.activeDryRunBots', { count: activeStrategiesCount })
              : activeView === 'live'
              ? t('dashboard.activeLiveBots', { count: activeStrategiesCount })
              : t('dashboard.inLive', { count: strategies.filter((s) => s.mode === 'live').length })
          }
          icon={<Cpu className="w-4 h-4 text-primary" />}
        />

        <MetricCard
          title={t('dashboard.liveTickers')}
          value={tickers.length}
          subtitle={t('dashboard.monitoredPairs')}
          icon={<Layers className="w-4 h-4 text-primary" />}
        />
      </div>

      {/* Two Column Layout: Left = Positions & Strategies, Right = Market Rates & PnL History */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 space-y-6">
          {/* Open Positions Card with Mode Filter Tabs */}
          <Card>
            <div className="p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-border">
              <div>
                <h2 className="text-base font-bold text-foreground">
                  {t('dashboard.openPositions')}
                </h2>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {t('dashboard.openPositionsSubtitle')}
                </p>
              </div>

              <div className="flex items-center gap-2">
                {/* Segmented Filter Pills */}
                <div className="flex items-center gap-1 p-0.5 bg-muted/40 rounded-lg border border-border/50 text-xs">
                  <button
                    type="button"
                    onClick={() => setPositionsTab('all')}
                    className={`px-2.5 py-1 rounded-md transition-all font-medium ${
                      positionsTab === 'all'
                        ? 'bg-primary text-white shadow-xs'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    {t('dashboard.filterAll', { count: positionsWithPnL.length })}
                  </button>
                  <button
                    type="button"
                    onClick={() => setPositionsTab('dryrun')}
                    className={`px-2.5 py-1 rounded-md transition-all font-medium flex items-center gap-1 ${
                      positionsTab === 'dryrun'
                        ? 'bg-violet-600 text-white shadow-xs'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    <FlaskConical className="w-3 h-3" />
                    <span>{t('dashboard.filterDryRun', { count: dryRunPositions.length })}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setPositionsTab('live')}
                    className={`px-2.5 py-1 rounded-md transition-all font-medium flex items-center gap-1 ${
                      positionsTab === 'live'
                        ? 'bg-amber-600 text-white shadow-xs'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    <Coins className="w-3 h-3" />
                    <span>{t('dashboard.filterLive', { count: livePositions.length })}</span>
                  </button>
                </div>

                <Link
                  to="/activity"
                  className="text-xs text-primary hover:underline flex items-center gap-1 font-medium ml-1"
                >
                  <span>{t('dashboard.viewAll')}</span>
                  <ExternalLink className="w-3.5 h-3.5" />
                </Link>
              </div>
            </div>

            {displayPositions.length === 0 ? (
              <div className="p-8 text-center text-sm text-muted-foreground">
                {positionsWithPnL.length === 0
                  ? t('dashboard.noPositions')
                  : t('dashboard.noPositionsMatchingFilter')}
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('dashboard.colExecution')}</TableHead>
                    <TableHead>{t('dashboard.colSymbol')}</TableHead>
                    <TableHead>{t('dashboard.colStrategyId')}</TableHead>
                    <TableHead>{t('dashboard.colOrderId')}</TableHead>
                    <TableHead>{t('dashboard.colEntryPrice')}</TableHead>
                    <TableHead>{t('dashboard.colCurrentPrice')}</TableHead>
                    <TableHead>{t('dashboard.colQuantity')}</TableHead>
                    <TableHead>{t('dashboard.colUnrealized')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {displayPositions.map((pos) => (
                    <TableRow key={pos.id}>
                      <TableCell>
                        {pos.mode === 'live' || pos.mode === 'paper' ? (
                          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-500 border border-amber-500/30">
                            <Coins className="w-3 h-3" />
                            <span>{t.enum('mode', 'live')}</span>
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider bg-violet-500/15 text-violet-400 border border-violet-500/30 border-dashed">
                            <FlaskConical className="w-3 h-3" />
                            <span>{t('dashboard.simulatedTag')}</span>
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="font-semibold text-foreground">{pos.symbol}</TableCell>
                      <TableCell className="text-muted-foreground text-xs">
                        <Link to={`/strategies/${pos.strategy_id}`} className="hover:text-primary hover:underline">
                          {pos.strategy_name}
                        </Link>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {pos.brokerOrderId || '—'}
                      </TableCell>
                      <TableCell>${formatNumber(pos.entryPrice, { digits: 2 })}</TableCell>
                      <TableCell>${formatNumber(pos.currentPrice, { digits: 2 })}</TableCell>
                      <TableCell>{formatNumber(pos.quantity, { digits: 4 })}</TableCell>
                      <TableCell>
                        <span
                          className={`font-semibold ${
                            pos.unrealizedPnL >= 0 ? 'text-emerald-500' : 'text-destructive'
                          }`}
                        >
                          {formatUsd(pos.unrealizedPnL, { signed: true, digits: 2 })} ({formatPct(pos.unrealizedPnLPct)})
                        </span>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </Card>

          {/* Strategy Overview Card */}
          <Card>
            <div className="p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-border">
              <div>
                <h2 className="text-base font-bold text-foreground">
                  {t('dashboard.strategyOverview')}
                </h2>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {t('dashboard.strategyOverviewSubtitle')}
                </p>
              </div>

              <div className="flex items-center gap-2">
                <div className="flex items-center gap-1 p-0.5 bg-muted/40 rounded-lg border border-border/50 text-xs">
                  <button
                    type="button"
                    onClick={() => setStrategiesTab('all')}
                    className={`px-2.5 py-1 rounded-md transition-all font-medium ${
                      strategiesTab === 'all'
                        ? 'bg-primary text-white shadow-xs'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    {t('dashboard.filterAll', { count: strategies.length })}
                  </button>
                  <button
                    type="button"
                    onClick={() => setStrategiesTab('dryrun')}
                    className={`px-2.5 py-1 rounded-md transition-all font-medium flex items-center gap-1 ${
                      strategiesTab === 'dryrun'
                        ? 'bg-violet-600 text-white shadow-xs'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    <FlaskConical className="w-3 h-3" />
                    <span>{t('dashboard.filterDryRun', { count: dryRunStrategies.length })}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setStrategiesTab('live')}
                    className={`px-2.5 py-1 rounded-md transition-all font-medium flex items-center gap-1 ${
                      strategiesTab === 'live'
                        ? 'bg-amber-600 text-white shadow-xs'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    <Coins className="w-3 h-3" />
                    <span>{t('dashboard.filterLive', { count: liveStrategies.length })}</span>
                  </button>
                </div>

                <Link
                  to="/strategies"
                  className="text-xs text-primary hover:underline flex items-center gap-1 font-medium ml-1"
                >
                  <span>{t('dashboard.manageStrategies')}</span>
                  <ExternalLink className="w-3.5 h-3.5" />
                </Link>
              </div>
            </div>

            {displayStrategies.length === 0 ? (
              <div className="p-8 text-center text-sm text-muted-foreground">
                {t('dashboard.noStrategies')}
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('dashboard.colId')}</TableHead>
                    <TableHead>{t('dashboard.colName')}</TableHead>
                    <TableHead>{t('dashboard.colType')}</TableHead>
                    <TableHead>{t('dashboard.colSymbols')}</TableHead>
                    <TableHead>{t('dashboard.colCycle')}</TableHead>
                    <TableHead>{t('dashboard.colMode')}</TableHead>
                    <TableHead>{t('dashboard.colStatus')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {displayStrategies.slice(0, 5).map((strat) => (
                    <TableRow key={strat.id}>
                      <TableCell className="text-muted-foreground">#{strat.id}</TableCell>
                      <TableCell className="font-semibold text-foreground">
                        <Link to={`/strategies/${strat.id}`} className="hover:text-primary hover:underline">
                          {strat.name}
                        </Link>
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">{strat.strategy_name}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {strat.monitored_symbols?.join(', ') || '—'}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">{strat.cycle}m</TableCell>
                      <TableCell>
                        <ModeBadge mode={strat.mode} />
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={strat.status} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </Card>
        </div>

        {/* Right Sidebar: Market Rates & Cumulative PnL Chart */}
        <div className="space-y-6">
          <Card>
            <CardHeader title={t('dashboard.marketRates')} subtitle={t('dashboard.marketRatesSubtitle')} />
            {tickers.length === 0 ? (
              <div className="p-4 text-center text-sm text-muted-foreground border-t border-border">
                {t('dashboard.noTickers')}
              </div>
            ) : (
              <div className="px-4 pb-4 space-y-2">
                {tickers.map((ticker) => (
                  <div
                    key={ticker.Symbol}
                    className="flex items-center justify-between p-3 bg-muted/30 rounded-lg border hover:bg-muted/50 transition-colors"
                  >
                    <span className="font-semibold text-foreground">
                      {ticker.Symbol}
                    </span>
                    <span className="font-bold text-foreground">
                      ${formatPrice(ticker.Price ?? 0, 8, 2)}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <Card>
            <CardHeader title={t('dashboard.cumulativePnl')} subtitle={t('dashboard.cumulativePnlSubtitle')} />
            <div className="p-4 pt-0">
              <PnlHistoryChart points={pnlChartData} height={200} />
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}
