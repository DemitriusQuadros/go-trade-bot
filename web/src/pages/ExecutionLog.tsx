import { useSignals, useStrategies } from '@/hooks/queries';
import React, { useState, useMemo } from 'react';
import { api } from '@/api/client';
import { Signal, Order, Strategy } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';
import {
  FileSpreadsheet,
  Download,
  Search,
  RefreshCw,
  ArrowUpRight,
  ArrowDownRight,
  Shield,
  Layers,
} from 'lucide-react';

export function ExecutionLog() {
  
  

    
  const [searchQuery, setSearchQuery] = useState('');
  const { data: signals = [], refetch: refetchSignals, isLoading: isSignalsLoading } = useSignals('closed');
  const { data: strategies = [] } = useStrategies();
  const [symbolFilter, setSymbolFilter] = useState('all');

  const strategyMap = useMemo(() => {
    const map = new Map<number, Strategy>();
    strategies.forEach((s) => map.set(s.id, s));
    return map;
  }, [strategies]);

  // Flatten signals into individual order fills
  const flattenedOrders = useMemo(() => {
    const list: Array<{
      order: Order;
      signal: Signal;
      strategyName: string;
    }> = [];

    signals.forEach((sig) => {
      const strat = strategyMap.get(sig.strategy_id);
      const strategyName = strat?.name || `Strategy #${sig.strategy_id}`;
      sig.orders?.forEach((ord) => {
        list.push({
          order: ord,
          signal: sig,
          strategyName,
        });
      });
    });

    return list.sort((a, b) => new Date(b.order.created_at).getTime() - new Date(a.order.created_at).getTime());
  }, [signals, strategyMap]);

  const uniqueSymbols = useMemo(() => {
    return Array.from(new Set(signals.map((s) => s.symbol))).filter(Boolean);
  }, [signals]);

  const filteredOrders = useMemo(() => {
    return flattenedOrders.filter((item) => {
      const matchSymbol = symbolFilter === 'all' || item.signal.symbol === symbolFilter;
      const q = searchQuery.toLowerCase();
      const matchSearch =
        searchQuery === '' ||
        item.signal.symbol.toLowerCase().includes(q) ||
        item.strategyName.toLowerCase().includes(q) ||
        item.order.broker_order_id.toLowerCase().includes(q);
      return matchSymbol && matchSearch;
    });
  }, [flattenedOrders, symbolFilter, searchQuery]);

  const handleExportCSV = () => {
    const headers = [
      'Order ID',
      'Broker Order ID',
      'Signal ID',
      'Strategy',
      'Symbol',
      'Entry Price',
      'Exit Price',
      'Quantity',
      'Invested',
      'Fees',
      'Profit',
      'Timestamp',
    ];
    const rows = filteredOrders.map(({ order, signal, strategyName }) => [
      order.id,
      `"${order.broker_order_id}"`,
      signal.id,
      `"${strategyName}"`,
      signal.symbol,
      order.entry_price,
      order.exit_price,
      order.quantity,
      order.invested_amount,
      (order.entry_fee + order.exit_fee).toFixed(4),
      order.profit,
      order.created_at,
    ]);

    const csvContent = [headers.join(','), ...rows.map((r) => r.join(','))].join('\n');
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', `execution_audit_log_${new Date().toISOString().split('T')[0]}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  if (isSignalsLoading && !signals) {
    return <LoadingScreen message="Loading order execution audit logs..." />;
  }

  return (
    <div className="container-custom space-y-6">
      {/* Header */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-green-500 flex items-center gap-2">
            Order Execution Log
          </h1>
          <p className="text-xs text-green-700 mt-0.5">
            Immutable trade ledger, broker fill confirmations, fee attribution & slippage audit
          </p>
        </div>

        <button
          onClick={handleExportCSV}
          disabled={filteredOrders.length === 0}
          className="btn btn-secondary text-xs flex items-center gap-1.5"
        >
          <Download className="w-3.5 h-3.5" />
          <span>Export Audit Log (CSV)</span>
        </button>
      </div>

      {/* Filter Row */}
      <Card>
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4">
          <div className="flex items-center gap-3 flex-1 max-w-sm">
            <div className="relative w-full">
              <Search className="w-4 h-4 text-green-800 absolute left-3 top-2.5" />
              <input
                type="text"
                placeholder="Search symbol, strategy, broker order ID..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="form-input pl-9 text-xs"
              />
            </div>
          </div>

          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <span className="text-xs text-green-700 font-medium">Symbol:</span>
              <select
                value={symbolFilter}
                onChange={(e) => setSymbolFilter(e.target.value)}
                className="form-select text-xs py-1"
              >
                <option value="all">All Symbols</option>
                {uniqueSymbols.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </div>

            <button
              onClick={() => refetchSignals()}
              className="btn btn-secondary text-xs py-1"
              title="Refresh logs"
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      {/* Orders Table */}
      <Card>
        <CardHeader
          title="Executed Order Fills"
          subtitle={`Auditing ${filteredOrders.length} confirmed orders`}
        />

        {filteredOrders.length === 0 ? (
          <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
            No execution logs found matching search criteria.
          </div>
        ) : (
          <div className="table-container">
            <table className="table">
              <thead>
                <tr>
                  <th>Order ID</th>
                  <th>Broker Ref</th>
                  <th>Symbol</th>
                  <th>Strategy</th>
                  <th>Entry Price</th>
                  <th>Exit Price</th>
                  <th>Quantity</th>
                  <th>Fees</th>
                  <th>Realized P&L</th>
                  <th>Fill Time</th>
                </tr>
              </thead>
              <tbody>
                {filteredOrders.map(({ order, signal, strategyName }) => {
                  const isProfit = order.profit >= 0;
                  const totalFee = (order.entry_fee || 0) + (order.exit_fee || 0);

                  return (
                    <tr key={order.id}>
                      <td className="font-mono text-green-800 text-xs">#{order.id}</td>
                      <td className="font-mono text-[11px] text-green-700">
                        {order.broker_order_id ? (
                          <span className="bg-green-950/20 px-1.5 py-0.5 rounded border border-green-900/30">
                            {order.broker_order_id}
                          </span>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="font-mono font-bold text-green-500 text-xs">{signal.symbol}</td>
                      <td className="text-xs text-green-600 font-medium">{strategyName}</td>
                      <td className="font-mono text-xs">${order.entry_price.toFixed(2)}</td>
                      <td className="font-mono text-xs">
                        {order.exit_price > 0 ? `$${order.exit_price.toFixed(2)}` : '—'}
                      </td>
                      <td className="font-mono text-xs">{order.quantity.toFixed(4)}</td>
                      <td className="font-mono text-xs text-green-700">${totalFee.toFixed(3)}</td>
                      <td className="font-mono text-xs">
                        <span
                          className={`font-semibold ${
                            isProfit ? 'text-emerald-400' : 'text-rose-400'
                          }`}
                        >
                          {isProfit ? '+' : ''}${order.profit.toFixed(2)}
                        </span>
                      </td>
                      <td className="text-xs text-green-700 font-mono">
                        {new Date(order.created_at).toLocaleString()}
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
