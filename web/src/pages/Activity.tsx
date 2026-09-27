import React, { useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useSignals, useStrategies, useTickerPrices, useAgentRuns } from '@/hooks/queries';
import { useSSE } from '@/hooks/useSSE';
import { Signal, Order, Strategy } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';
import { AgentToolCallCard } from '@/components/domain/AgentToolCallCard';
import { MarkdownMessage } from '@/components/domain/MarkdownMessage';
import {
  Layers,
  ArrowUpRight,
  ArrowDownRight,
  Clock,
  Shield,
  RefreshCw,
  Search,
  Download,
  ChevronDown,
  ChevronRight,
  X,
  Bot,
} from 'lucide-react';

// Was three separate top-level pages (Positions, Execution Log, Agent
// History) - all three are filtered views over the same underlying object,
// one strategy's trading activity. Merged into one page with tabs, the
// pattern TradingView/MetaTrader use for "many actions/views on one
// object" instead of paging between unrelated-looking top-level routes.
type ActivityTab = 'positions' | 'fills' | 'agent';

const TABS: { key: ActivityTab; label: string }[] = [
  { key: 'positions', label: 'Positions' },
  { key: 'fills', label: 'Order Fills' },
  { key: 'agent', label: 'Agent Log' },
];

export function Activity() {
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = (searchParams.get('tab') as ActivityTab) || 'positions';

  const setTab = (next: ActivityTab) => {
    const params = new URLSearchParams(searchParams);
    params.set('tab', next);
    setSearchParams(params);
  };

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground">Activity</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Everything that's happened to your strategies - live positions, confirmed order fills, and what the
            AI copilot has done.
          </p>
        </div>

        <div className="flex items-center gap-2 bg-card/20 p-1 rounded-lg border border-border/30">
          {TABS.map((t) => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
                tab === t.key ? 'bg-secondary text-foreground shadow' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {tab === 'positions' && <PositionsTab />}
      {tab === 'fills' && <FillsTab />}
      {tab === 'agent' && <AgentLogTab searchParams={searchParams} setSearchParams={setSearchParams} />}
    </div>
  );
}

// --- Positions ---------------------------------------------------------
function PositionsTab() {
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
      return p.symbol.toLowerCase().includes(q) || p.strategyName.toLowerCase().includes(q) || String(p.id).includes(q);
    });
  }, [enrichedPositions, searchQuery]);

  const totalPnL = useMemo(() => filteredPositions.reduce((sum, p) => sum + p.pnl, 0), [filteredPositions]);

  return (
    <>
      <Card>
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4">
          <div className="flex items-center gap-2 bg-card/20 p-1 rounded-lg border border-border/30 w-fit">
            <button
              onClick={() => setStatusTab('open')}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
                statusTab === 'open' ? 'bg-secondary text-foreground shadow' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              Open
            </button>
            <button
              onClick={() => setStatusTab('closed')}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
                statusTab === 'closed' ? 'bg-secondary text-foreground shadow' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              Closed
            </button>
          </div>

          <div className="flex items-center gap-3 flex-1 max-w-sm">
            <div className="relative w-full">
              <Search className="w-4 h-4 text-muted-foreground absolute left-3 top-2.5" />
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
              <span className="text-[11px] text-muted-foreground block">
                Total {statusTab === 'open' ? 'Unrealized' : 'Realized'} P&amp;L
              </span>
              <span className={`font-mono text-sm font-bold ${totalPnL >= 0 ? 'text-success' : 'text-destructive'}`}>
                {totalPnL >= 0 ? '+' : ''}${totalPnL.toFixed(2)}
              </span>
            </div>
            <button
              onClick={() => refetchSignals()}
              className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs p-1.5"
              title="Refresh positions"
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      <Card>
        <CardHeader
          title={statusTab === 'open' ? 'Live Open Positions' : 'Closed Signal Audit'}
          subtitle={`Showing ${filteredPositions.length} positions`}
        />

        {isSignalsLoading && !signals ? (
          <LoadingScreen message="Loading position records..." />
        ) : filteredPositions.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
            No {statusTab} positions found.
          </div>
        ) : (
          <div className="overflow-x-auto rounded border border-border/60">
            <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_td]:whitespace-nowrap [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40 [&_thead]:bg-card/40">
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
                      <td className="font-mono text-muted-foreground">#{pos.id}</td>
                      <td className="font-mono font-bold text-foreground">{pos.symbol}</td>
                      <td>
                        <span className="font-medium text-foreground">{pos.strategyName}</span>
                      </td>
                      <td className="font-mono">${pos.entryPrice.toFixed(2)}</td>
                      <td className="font-mono">${pos.currentPrice.toFixed(2)}</td>
                      <td className="font-mono text-muted-foreground">
                        {pos.stopLossPrice > 0 ? (
                          <div className="flex items-center gap-1 text-warning/90">
                            <Shield className="w-3 h-3 flex-shrink-0" />
                            <span>${pos.stopLossPrice.toFixed(2)}</span>
                          </div>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="font-mono text-xs text-muted-foreground">{pos.quantity.toFixed(4)}</td>
                      <td className="font-mono text-xs text-muted-foreground">${pos.invested.toFixed(2)}</td>
                      <td className="font-mono">
                        <div className="flex items-center gap-1">
                          {isPositive ? (
                            <ArrowUpRight className="w-3.5 h-3.5 text-success flex-shrink-0" />
                          ) : (
                            <ArrowDownRight className="w-3.5 h-3.5 text-destructive flex-shrink-0" />
                          )}
                          <span className={`font-semibold ${isPositive ? 'text-success' : 'text-destructive'}`}>
                            {isPositive ? '+' : ''}${pos.pnl.toFixed(2)} ({pos.pnlPct.toFixed(2)}%)
                          </span>
                        </div>
                      </td>
                      <td className="text-xs text-muted-foreground">
                        <div className="flex items-center gap-1">
                          <Clock className="w-3 h-3 text-muted-foreground" />
                          <span>
                            {openedDate.getMonth() + 1}/{openedDate.getDate()} {openedDate.getHours().toString().padStart(2, '0')}:
                            {openedDate.getMinutes().toString().padStart(2, '0')}
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
    </>
  );
}

// --- Order Fills ---------------------------------------------------------
function FillsTab() {
  const [searchQuery, setSearchQuery] = useState('');
  const { data: signals = [], refetch: refetchSignals, isLoading: isSignalsLoading } = useSignals('closed');
  const { data: strategies = [] } = useStrategies();
  const [symbolFilter, setSymbolFilter] = useState('all');

  const strategyMap = useMemo(() => {
    const map = new Map<number, Strategy>();
    strategies.forEach((s) => map.set(s.id, s));
    return map;
  }, [strategies]);

  const flattenedOrders = useMemo(() => {
    const list: Array<{ order: Order; signal: Signal; strategyName: string }> = [];
    signals.forEach((sig) => {
      const strat = strategyMap.get(sig.strategy_id);
      const strategyName = strat?.name || `Strategy #${sig.strategy_id}`;
      sig.orders?.forEach((ord) => list.push({ order: ord, signal: sig, strategyName }));
    });
    return list.sort((a, b) => new Date(b.order.created_at).getTime() - new Date(a.order.created_at).getTime());
  }, [signals, strategyMap]);

  const uniqueSymbols = useMemo(() => Array.from(new Set(signals.map((s) => s.symbol))).filter(Boolean), [signals]);

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
      'Order ID', 'Broker Order ID', 'Signal ID', 'Strategy', 'Symbol', 'Entry Price', 'Exit Price', 'Quantity',
      'Invested', 'Fees', 'Profit', 'Timestamp',
    ];
    const rows = filteredOrders.map(({ order, signal, strategyName }) => [
      order.id, `"${order.broker_order_id}"`, signal.id, `"${strategyName}"`, signal.symbol, order.entry_price,
      order.exit_price, order.quantity, order.invested_amount, (order.entry_fee + order.exit_fee).toFixed(4),
      order.profit, order.created_at,
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
    <>
      <Card>
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4">
          <div className="flex items-center gap-3 flex-1 max-w-sm">
            <div className="relative w-full">
              <Search className="w-4 h-4 text-muted-foreground absolute left-3 top-2.5" />
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
              <span className="text-xs text-muted-foreground font-medium">Symbol:</span>
              <select value={symbolFilter} onChange={(e) => setSymbolFilter(e.target.value)} className="form-select text-xs py-1">
                <option value="all">All Symbols</option>
                {uniqueSymbols.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </div>
            <button
              onClick={handleExportCSV}
              disabled={filteredOrders.length === 0}
              className="bg-card/40 hover:bg-secondary/40 disabled:opacity-50 text-foreground rounded border border-border/40 text-xs flex items-center gap-1.5 px-3 py-1.5"
            >
              <Download className="w-3.5 h-3.5" />
              <span>Export CSV</span>
            </button>
            <button
              onClick={() => refetchSignals()}
              className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs p-1.5"
              title="Refresh logs"
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      <Card>
        <CardHeader title="Executed Order Fills" subtitle={`Auditing ${filteredOrders.length} confirmed orders`} />

        {filteredOrders.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
            No execution logs found matching search criteria.
          </div>
        ) : (
          <div className="overflow-x-auto rounded border border-border/60">
            <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_td]:whitespace-nowrap [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40 [&_thead]:bg-card/40">
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
                      <td className="font-mono text-muted-foreground text-xs">#{order.id}</td>
                      <td className="font-mono text-[11px] text-muted-foreground">
                        {order.broker_order_id ? (
                          <span className="bg-card/20 px-1.5 py-0.5 rounded border border-border/30">{order.broker_order_id}</span>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="font-mono font-bold text-foreground text-xs">{signal.symbol}</td>
                      <td className="text-xs text-muted-foreground font-medium">{strategyName}</td>
                      <td className="font-mono text-xs">${order.entry_price.toFixed(2)}</td>
                      <td className="font-mono text-xs">{order.exit_price > 0 ? `$${order.exit_price.toFixed(2)}` : '—'}</td>
                      <td className="font-mono text-xs">{order.quantity.toFixed(4)}</td>
                      <td className="font-mono text-xs text-muted-foreground">${totalFee.toFixed(3)}</td>
                      <td className="font-mono text-xs">
                        <span className={`font-semibold ${isProfit ? 'text-success' : 'text-destructive'}`}>
                          {isProfit ? '+' : ''}${order.profit.toFixed(2)}
                        </span>
                      </td>
                      <td className="text-xs text-muted-foreground font-mono">{new Date(order.created_at).toLocaleString()}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </>
  );
}

// --- Agent Log ---------------------------------------------------------
function AgentLogTab({
  searchParams,
  setSearchParams,
}: {
  searchParams: URLSearchParams;
  setSearchParams: (params: URLSearchParams) => void;
}) {
  const strategyIdParam = searchParams.get('strategy_id');
  const strategyId = strategyIdParam ? parseInt(strategyIdParam, 10) : undefined;

  const [limit, setLimit] = useState(20);
  const [expandedId, setExpandedId] = useState<number | null>(null);

  const { data: runs = [], isLoading, isFetching, refetch } = useAgentRuns(strategyId, limit);

  const clearFilter = () => {
    const next = new URLSearchParams(searchParams);
    next.delete('strategy_id');
    setSearchParams(next);
  };

  const sorted = useMemo(
    () => [...runs].sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime()),
    [runs]
  );

  if (isLoading) {
    return <LoadingScreen message="Loading agent run history..." />;
  }

  return (
    <Card>
      <div className="flex items-center justify-between gap-2 mb-4">
        <CardHeader
          className="mb-0"
          title="Agent Runs"
          subtitle={`${sorted.length} run${sorted.length === 1 ? '' : 's'}${strategyId != null ? ` for strategy #${strategyId}` : ''} - what was asked, which tools ran, what was actually persisted`}
        />
        <div className="flex items-center gap-2 shrink-0">
          {strategyId != null && (
            <button
              onClick={clearFilter}
              className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs py-1.5 px-2.5 flex items-center gap-1.5"
              title="Clear strategy filter"
            >
              <span>Strategy #{strategyId}</span>
              <X className="w-3 h-3" />
            </button>
          )}
          <button
            onClick={() => refetch()}
            className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs p-1.5"
            title="Refresh"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {sorted.length === 0 ? (
        <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
          No agent runs recorded yet.
        </div>
      ) : (
        <div className="overflow-x-auto rounded border border-border/60">
          <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40 [&_thead]:bg-card/40">
            <thead>
              <tr>
                <th></th>
                <th>Started</th>
                <th>Trigger</th>
                <th>Provider / Model</th>
                <th>Status</th>
                <th>Strategy</th>
                <th>Input</th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((run) => {
                const isExpanded = expandedId === run.id;
                const isError = run.status === 'error';
                return (
                  <React.Fragment key={run.id}>
                    <tr
                      onClick={() => setExpandedId(isExpanded ? null : run.id)}
                      className={`cursor-pointer ${isError ? 'bg-destructive/15 hover:bg-destructive/15' : 'hover:bg-card/20'}`}
                    >
                      <td className="text-muted-foreground">
                        {isExpanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
                      </td>
                      <td className="font-mono text-muted-foreground whitespace-nowrap">{new Date(run.started_at).toLocaleString()}</td>
                      <td className="font-mono text-muted-foreground">{run.trigger}</td>
                      <td className="text-muted-foreground">
                        {run.provider || '—'}
                        {run.model ? <span className="text-muted-foreground">/{run.model}</span> : null}
                      </td>
                      <td>
                        <span
                          className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase ${
                            isError ? 'bg-destructive/15 text-destructive border border-destructive/40' : 'bg-success/15 text-success border border-success/40'
                          }`}
                        >
                          {run.status}
                        </span>
                      </td>
                      <td>
                        {run.strategy_id ? (
                          <Link
                            to={`/strategies/${run.strategy_id}/edit`}
                            onClick={(e) => e.stopPropagation()}
                            className="text-success hover:text-success underline underline-offset-2 font-mono"
                          >
                            #{run.strategy_id}
                          </Link>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="max-w-xs truncate text-foreground">{run.input_summary}</td>
                    </tr>
                    {isExpanded && (
                      <tr>
                        <td colSpan={7} className="bg-background/40 p-3">
                          {isError && run.error_message && (
                            <div className="mb-2 text-xs text-destructive bg-destructive/15 border border-destructive/40 rounded px-2 py-1.5">
                              {run.error_message}
                            </div>
                          )}
                          {run.tool_calls.length === 0 ? (
                            <div className="text-xs text-muted-foreground italic">No tool calls in this run.</div>
                          ) : (
                            <div className="space-y-2">
                              {run.tool_calls.map((call, i) => (
                                <AgentToolCallCard key={i} call={call} />
                              ))}
                            </div>
                          )}
                          {run.response_text && (
                            <div className="mt-2 rounded border border-border/60 bg-background/40 text-foreground px-3 py-2">
                              <MarkdownMessage content={run.response_text} />
                            </div>
                          )}
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {sorted.length >= limit && (
        <div className="mt-3 flex justify-center">
          <button
            onClick={() => setLimit((l) => l + 20)}
            className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs py-1.5 px-4"
          >
            Load more
          </button>
        </div>
      )}
    </Card>
  );
}
