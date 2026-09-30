import React, { useMemo } from 'react';
import { 
  useAccount, 
  useStrategies, 
  useSignals, 
  useTickerPrices, 
  usePerformanceSnapshots 
} from '@/hooks/queries';
import { useSSE } from '@/hooks/useSSE';
import { Card, CardHeader, MetricCard } from '@/components/ui/Card';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen } from '@/components/ui/Spinner';
import { PnlHistoryChart } from '@/components/charts/PnlHistoryChart';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/Table';
import { Button } from '@/components/ui/Button';
import {
  Wallet,
  TrendingUp,
  Cpu,
  Layers,
  Radio,
  RefreshCw,
  ExternalLink,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import { formatNumber, formatPct, formatPrice, formatUsd } from '@/lib/format';
import { useT } from '@/i18n';

export function Dashboard() {
  const t = useT();
  const { data: account, isLoading: isAccountLoading, refetch: refetchAccount } = useAccount();
  const { data: strategies = [], isLoading: isStrategiesLoading, refetch: refetchStrategies } = useStrategies();
  const { data: openSignals = [], isLoading: isSignalsLoading, refetch: refetchSignals } = useSignals('open');
  const baseTickers: {Symbol: string, Price: number}[] = []; const refetchTickers = () => {};
  const { data: performance = [], isLoading: isPerformanceLoading, refetch: refetchPerformance } = usePerformanceSnapshots();

  const { prices: ssePrices, positions: ssePositions, connected: sseConnected } = useSSE(true);

  // Merge REST tickers with SSE ticker updates
  const tickers = useMemo(() => {
    const map = new Map<string, number>();
    baseTickers.forEach((t) => {
      map.set(t.Symbol, t.Price);
    });
    Object.values(ssePrices).forEach((p) => {
      map.set(p.symbol, p.price);
    });
    return Array.from(map.entries()).map(([Symbol, Price]) => ({ Symbol, Price }));
  }, [baseTickers, ssePrices]);

  // Derive P&L and positions combining REST data with real-time SSE updates
  const positionsWithPnL = useMemo(() => {
    return openSignals.map((signal) => {
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
      } else {
        const entryOrder = signal.orders?.[0];
        if (entryOrder) {
          entryPrice = entryOrder.entry_price;
          quantity = entryOrder.quantity;
          currentPrice = tickerPrice || entryPrice;

          const valueDiff = (currentPrice - entryPrice) * quantity;
          unrealizedPnL = valueDiff;
          unrealizedPnLPct = entryPrice > 0 ? (unrealizedPnL / (entryPrice * quantity)) * 100 : 0;
        }
      }

      return {
        id: signal.id,
        symbol: signal.symbol,
        strategy_id: signal.strategy_id,
        entryPrice,
        currentPrice,
        quantity,
        unrealizedPnL,
        unrealizedPnLPct,
      };
    }).filter(pos => pos.quantity > 0);
  }, [openSignals, ssePositions, tickers]);

  const totalUnrealizedPnL = positionsWithPnL.reduce((sum, pos) => sum + pos.unrealizedPnL, 0);
  const activeStrategiesCount = strategies.filter((s) => s.status === 'productive').length;

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
    refetchTickers();
    refetchPerformance();
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t('dashboard.title')}</h1>
          <p className="text-sm text-muted-foreground mt-1">{t('dashboard.subtitle')}</p>
        </div>
        <div className="flex items-center gap-3">
          <div className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium border ${
            sseConnected 
              ? 'bg-success/10 text-success border-success/20'
              : 'bg-destructive/10 text-destructive border-destructive/20'
          }`}>
            <Radio className={`w-3.5 h-3.5 ${sseConnected ? 'animate-pulse' : ''}`} />
            {sseConnected ? t('dashboard.live') : t('dashboard.disconnected')}
          </div>
          <Button variant="outline" size="sm" onClick={handleRefresh}>
            <RefreshCw className="w-3.5 h-3.5 mr-2" />
            {t('common.refresh')}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          title={t('dashboard.accountBalance')}
          value={account ? formatUsd(account.amount ?? (account as any).Amount ?? 0, { digits: 2 }) : '—'}
          subtitle={account?.currency ? t('dashboard.currency', { currency: account.currency ?? (account as any).Currency }) : t('dashboard.spotAccount')}
          icon={<Wallet className="w-4 h-4 text-primary" />}
        />
        <MetricCard
          title={t('dashboard.openPnl')}
          value={formatUsd(totalUnrealizedPnL, { digits: 2 })}
          isPositive={totalUnrealizedPnL >= 0}
          change={formatNumber(totalUnrealizedPnL, { digits: 2, signed: true })}
          subtitle={t('dashboard.openPositionsCount', { count: positionsWithPnL.length })}
          icon={<TrendingUp className="w-4 h-4 text-primary" />}
        />
        <MetricCard
          title={t('dashboard.activeStrategies')}
          value={`${activeStrategiesCount} / ${strategies.length}`}
          subtitle={t('dashboard.inLive', { count: strategies.filter((s) => s.mode === 'live').length })}
          icon={<Cpu className="w-4 h-4 text-primary" />}
        />
        <MetricCard
          title={t('dashboard.liveTickers')}
          value={tickers.length}
          subtitle={t('dashboard.monitoredPairs')}
          icon={<Layers className="w-4 h-4 text-primary" />}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 space-y-6">
          <Card>
            <CardHeader
              title={t('dashboard.openPositions')}
              subtitle={t('dashboard.openPositionsSubtitle')}
              action={
                <Link to="/activity" className="text-sm text-primary hover:underline flex items-center gap-1 font-medium">
                  <span>{t('dashboard.viewAll')}</span>
                  <ExternalLink className="w-4 h-4" />
                </Link>
              }
            />

            {positionsWithPnL.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground border-t border-border">
                {t('dashboard.noPositions')}
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('dashboard.colSymbol')}</TableHead>
                    <TableHead>{t('dashboard.colStrategyId')}</TableHead>
                    <TableHead>{t('dashboard.colEntryPrice')}</TableHead>
                    <TableHead>{t('dashboard.colCurrentPrice')}</TableHead>
                    <TableHead>{t('dashboard.colQuantity')}</TableHead>
                    <TableHead>{t('dashboard.colUnrealized')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {positionsWithPnL.map((pos) => (
                    <TableRow key={pos.id}>
                      <TableCell className="font-semibold text-foreground">{pos.symbol}</TableCell>
                      <TableCell className="text-muted-foreground">#{pos.strategy_id}</TableCell>
                      <TableCell>${formatNumber(pos.entryPrice, { digits: 2 })}</TableCell>
                      <TableCell>${formatNumber(pos.currentPrice, { digits: 2 })}</TableCell>
                      <TableCell>{formatNumber(pos.quantity, { digits: 4 })}</TableCell>
                      <TableCell>
                        <span
                          className={`font-semibold ${
                            pos.unrealizedPnL >= 0 ? 'text-foreground' : 'text-destructive'
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

          <Card>
            <CardHeader
              title={t('dashboard.strategyOverview')}
              subtitle={t('dashboard.strategyOverviewSubtitle')}
              action={
                <Link to="/strategies" className="text-sm text-primary hover:underline flex items-center gap-1 font-medium">
                  <span>{t('dashboard.manageStrategies')}</span>
                  <ExternalLink className="w-4 h-4" />
                </Link>
              }
            />

            {strategies.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground border-t border-border">
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
                  {strategies.slice(0, 5).map((strat) => (
                    <TableRow key={strat.id}>
                      <TableCell className="text-muted-foreground">#{strat.id}</TableCell>
                      <TableCell className="font-semibold text-foreground">{strat.name}</TableCell>
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
