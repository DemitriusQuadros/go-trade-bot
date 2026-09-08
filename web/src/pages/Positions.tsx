import { useSignals, useStrategies, useTickerPrices } from '@/hooks/queries';
import React, { useState, useMemo } from 'react';
import { api } from '@/api/client';
import { useSSE } from '@/hooks/useSSE';
import { Signal, Strategy } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { LoadingScreen } from '@/components/ui/Spinner';
import {
  Layers,
  ArrowUpRight,
  ArrowDownRight,
  Clock,
  Shield,
  RefreshCw,
  Search,
  ExternalLink,
} from 'lucide-react';

export function Positions() {
  const [statusTab, setStatusTab] = useState<'open' | 'closed'>('open');
  const { data: signals = [], refetch: refetchSignals, isLoading: isSignalsLoading } = useSignals(statusTab as any);
  const { data: strategies = [] } = useStrategies();
  const { data: tickers = [] } = useTickerPrices();
  const [searchQuery, setSearchQuery] = useState('');

  
  
  

  const { prices: ssePrices, positions: ssePositions } = useSSE(statusTab === 'open');

      
  const strategyMap = useMemo(() => {
    const map = new Map<number, Strategy>();
    strategies.forEach((s) => map.set(s.id, s));
    return map;
  }, [strategies]);

  const enrichedPositions = useMemo(() => {
    return signals.map((sig) => {
      const strat = strategyMap.get(sig.strategy_id);
      const firstOrder = sig.orders?.[0];
      const entryPrice = firstOrder ? firstOrder.entry_price : 0;
      const stopLossPrice = firstOrder ? firstOrder.stop_loss_price : 0;
      const quantity = firstOrder ? firstOrder.quantity : 0;
      const invested = firstOrder ? firstOrder.invested_amount : 0;

      // Realtime or polled price
      const livePos = ssePositions[sig.id];
      const currentPrice =
        livePos?.current_price ||
        ssePrices[sig.symbol]?.price ||
        tickers.find((t) => t.Symbol === sig.symbol)?.Price ||
        entryPrice;

      let pnl = 0;
      let pnlPct = 0;

      if (sig.status === 'open') {
        if (livePos) {
          pnl = livePos.unrealized_pnl;
          pnlPct = livePos.unrealized_pnl_pct;
        } else if (currentPrice > 0 && entryPrice > 0) {
          pnl = (currentPrice - entryPrice) * quantity;
          pnlPct = ((currentPrice - entryPrice) / entryPrice) * 100;
        }
      } else {
        // Closed signal profit from orders
        pnl = sig.orders?.reduce((sum, o) => sum + (o.profit || 0), 0) || 0;
        pnlPct = invested > 0 ? (pnl / invested) * 100 : 0;
      }

      return {
        ...sig,
        strategyName: strat?.name || `Strategy #${sig.strategy_id}`,
        entryPrice,
        stopLossPrice,
        currentPrice,
        quantity,
        invested,
        pnl,
        pnlPct,
      };
    });
  }, [signals, strategyMap, ssePositions, ssePrices, tickers]);

  const filteredPositions = useMemo(() => {
    return enrichedPositions.filter((p) => {
      if (!searchQuery) return true;
      const q = searchQuery.toLowerCase();
      return (
        p.symbol.toLowerCase().includes(q) ||
        p.strategyName.toLowerCase().includes(q) ||
        String(p.id).includes(q)
      );
    });
  }, [enrichedPositions, searchQuery]);

  const totalPnL = useMemo(() => {
    return filteredPositions.reduce((sum, p) => sum + p.pnl, 0);
  }, [filteredPositions]);

  return (
    <div className="container-custom space-y-6">
      {/* Header */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-green-500 flex items-center gap-2">
            Positions & Signals
          </h1>
          <p className="text-xs text-green-700 mt-0.5">
            Active trade executions, stop loss management & marked-to-market performance
          </p>
        </div>

        {/* Tab switcher */}
        <div className="flex items-center gap-2 bg-green-950/20 p-1 rounded-lg border border-green-900/30">
          <button
            onClick={() => setStatusTab('open')}
            className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
              statusTab === 'open'
                ? 'bg-green-800 text-white shadow'
                : 'text-green-700 hover:text-green-400'
            }`}
          >
            Open Positions ({statusTab === 'open' ? signals.length : '...'})
          </button>
          <button
            onClick={() => setStatusTab('closed')}
            className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
              statusTab === 'closed'
                ? 'bg-green-800 text-white shadow'
                : 'text-green-700 hover:text-green-400'
            }`}
          >
            Closed History
          </button>
        </div>
      </div>

      {/* Filter and stats row */}
      <Card>
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4">
          <div className="flex items-center gap-3 flex-1 max-w-sm">
            <div className="relative w-full">
              <Search className="w-4 h-4 text-green-800 absolute left-3 top-2.5" />
              <input
                type="text"
                placeholder="Search symbol, strategy..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="form-input pl-9 text-xs"
              />
            </div>
          </div>

          <div className="flex items-center gap-4">
            <div className="text-right">
              <span className="text-[11px] text-green-700 block">
                Total {statusTab === 'open' ? 'Unrealized' : 'Realized'} P&L
              </span>
              <span
                className={`font-mono text-sm font-bold ${
                  totalPnL >= 0 ? 'text-emerald-400' : 'text-rose-400'
                }`}
              >
                {totalPnL >= 0 ? '+' : ''}${totalPnL.toFixed(2)}
              </span>
            </div>

            <button
              onClick={() => refetchSignals()}
              className="btn btn-secondary text-xs py-1"
              title="Refresh positions"
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      {/* Positions Table */}
      <Card>
        <CardHeader
          title={statusTab === 'open' ? 'Live Open Positions' : 'Closed Signal Audit'}
          subtitle={`Showing ${filteredPositions.length} positions`}
        />

        {isSignalsLoading && !signals ? (
          <LoadingScreen message="Loading position records..." />
        ) : filteredPositions.length === 0 ? (
          <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
            No {statusTab} positions found.
          </div>
        ) : (
          <div className="table-container">
            <table className="table">
              <thead>
                <tr>
                  <th>Signal ID</th>
                  <th>Symbol</th>
                  <th>Strategy</th>
                  <th>Entry Price</th>
                  {statusTab === 'open' ? <th>Current Price</th> : <th>Exit Price</th>}
                  <th>Stop Loss</th>
                  <th>Quantity</th>
                  <th>Invested</th>
                  <th>{statusTab === 'open' ? 'Unrealized P&L' : 'Realized P&L'}</th>
                  <th>Opened At</th>
                </tr>
              </thead>
              <tbody>
                {filteredPositions.map((pos) => {
                  const openedDate = new Date(pos.created_at);
                  const isPositive = pos.pnl >= 0;

                  return (
                    <tr key={pos.id}>
                      <td className="font-mono text-green-800">#{pos.id}</td>
                      <td className="font-mono font-bold text-green-500">{pos.symbol}</td>
                      <td>
                        <span className="font-medium text-green-400">{pos.strategyName}</span>
                      </td>
                      <td className="font-mono">${pos.entryPrice.toFixed(2)}</td>
                      <td className="font-mono">
                        ${pos.currentPrice.toFixed(2)}
                      </td>
                      <td className="font-mono text-green-700">
                        {pos.stopLossPrice > 0 ? (
                          <div className="flex items-center gap-1 text-amber-400/90">
                            <Shield className="w-3 h-3 flex-shrink-0" />
                            <span>${pos.stopLossPrice.toFixed(2)}</span>
                          </div>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="font-mono text-xs text-green-600">{pos.quantity.toFixed(4)}</td>
                      <td className="font-mono text-xs text-green-600">
                        ${pos.invested.toFixed(2)}
                      </td>
                      <td className="font-mono">
                        <div className="flex items-center gap-1">
                          {isPositive ? (
                            <ArrowUpRight className="w-3.5 h-3.5 text-emerald-400 flex-shrink-0" />
                          ) : (
                            <ArrowDownRight className="w-3.5 h-3.5 text-rose-400 flex-shrink-0" />
                          )}
                          <span className={`font-semibold ${isPositive ? 'text-emerald-400' : 'text-rose-400'}`}>
                            {isPositive ? '+' : ''}${pos.pnl.toFixed(2)} ({pos.pnlPct.toFixed(2)}%)
                          </span>
                        </div>
                      </td>
                      <td className="text-xs text-green-700">
                        <div className="flex items-center gap-1">
                          <Clock className="w-3 h-3 text-green-800" />
                          <span>
                            {openedDate.getMonth() + 1}/{openedDate.getDate()} {openedDate.getHours().toString().padStart(2, '0')}:{openedDate.getMinutes().toString().padStart(2, '0')}
                          </span>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}
