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

export function Dashboard() {
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
    return <LoadingScreen message="Initializing Dashboard..." />;
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
          <h1 className="text-2xl font-bold tracking-tight">Overview</h1>
          <p className="text-sm text-muted-foreground mt-1">Real-time trading performance</p>
        </div>
        <div className="flex items-center gap-3">
          <div className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium border ${
            sseConnected 
              ? 'bg-green-500/10 text-green-500 border-green-500/20' 
              : 'bg-destructive/10 text-destructive border-destructive/20'
          }`}>
            <Radio className={`w-3.5 h-3.5 ${sseConnected ? 'animate-pulse' : ''}`} />
            {sseConnected ? 'Live' : 'Disconnected'}
          </div>
          <Button variant="outline" size="sm" onClick={handleRefresh}>
            <RefreshCw className="w-3.5 h-3.5 mr-2" />
            Refresh
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          title="Account Balance"
          value={account ? `$${(account.amount ?? (account as any).Amount ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}` : '—'}
          subtitle={account?.currency ? `Currency: ${(account.currency ?? (account as any).Currency)}` : 'Spot Account'}
          icon={<Wallet className="w-4 h-4 text-primary" />}
        />
        <MetricCard
          title="Open Positions P&L"
          value={`$${totalUnrealizedPnL.toFixed(2)}`}
          isPositive={totalUnrealizedPnL >= 0}
          change={totalUnrealizedPnL >= 0 ? `+${totalUnrealizedPnL.toFixed(2)}` : `${totalUnrealizedPnL.toFixed(2)}`}
          subtitle={`${positionsWithPnL.length} open position${positionsWithPnL.length === 1 ? '' : 's'}`}
          icon={<TrendingUp className="w-4 h-4 text-primary" />}
        />
        <MetricCard
          title="Active Strategies"
          value={`${activeStrategiesCount} / ${strategies.length}`}
          subtitle={`${strategies.filter((s) => s.mode === 'live').length} in live execution`}
          icon={<Cpu className="w-4 h-4 text-primary" />}
        />
        <MetricCard
          title="Live Tickers"
          value={tickers.length}
          subtitle="Monitored crypto pairs"
          icon={<Layers className="w-4 h-4 text-primary" />}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 space-y-6">
          <Card>
            <CardHeader
              title="Open Positions"
              subtitle="Real-time marked-to-market positions"
              action={
                <Link to="/positions" className="text-sm text-primary hover:underline flex items-center gap-1 font-medium">
                  <span>View all</span>
                  <ExternalLink className="w-4 h-4" />
                </Link>
              }
            />

            {positionsWithPnL.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground border-t border-border">
                No active open positions.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Symbol</TableHead>
                    <TableHead>Strategy ID</TableHead>
                    <TableHead>Entry Price</TableHead>
                    <TableHead>Current Price</TableHead>
                    <TableHead>Quantity</TableHead>
                    <TableHead>Unrealized P&L</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {positionsWithPnL.map((pos) => (
                    <TableRow key={pos.id}>
                      <TableCell className="font-semibold text-foreground">{pos.symbol}</TableCell>
                      <TableCell className="text-muted-foreground">#{pos.strategy_id}</TableCell>
                      <TableCell>${pos.entryPrice.toFixed(2)}</TableCell>
                      <TableCell>${pos.currentPrice.toFixed(2)}</TableCell>
                      <TableCell>{pos.quantity.toFixed(4)}</TableCell>
                      <TableCell>
                        <span
                          className={`font-semibold ${
                            pos.unrealizedPnL >= 0 ? 'text-green-500' : 'text-destructive'
                          }`}
                        >
                          {pos.unrealizedPnL >= 0 ? '+' : ''}
                          ${pos.unrealizedPnL.toFixed(2)} ({pos.unrealizedPnLPct.toFixed(2)}%)
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
              title="Strategy Overview"
              subtitle="Registered bot instances & current run modes"
              action={
                <Link to="/strategies" className="text-sm text-primary hover:underline flex items-center gap-1 font-medium">
                  <span>Manage strategies</span>
                  <ExternalLink className="w-4 h-4" />
                </Link>
              }
            />

            {strategies.length === 0 ? (
              <div className="p-6 text-center text-sm text-muted-foreground border-t border-border">
                No strategies configured yet.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>ID</TableHead>
                    <TableHead>Name</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Symbols</TableHead>
                    <TableHead>Cycle</TableHead>
                    <TableHead>Mode</TableHead>
                    <TableHead>Status</TableHead>
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
            <CardHeader title="Market Rates" subtitle="Latest ticker prices" />
            {tickers.length === 0 ? (
              <div className="p-4 text-center text-sm text-muted-foreground border-t border-border">
                No tickers available.
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
                      ${(ticker.Price ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <Card>
            <CardHeader title="Cumulative P&L" subtitle="Realized historical performance" />
            <div className="p-4 pt-0">
              <PnlHistoryChart points={pnlChartData} height={200} />
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}
