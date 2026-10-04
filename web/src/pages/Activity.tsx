import React, { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useSignals, useStrategies, useTickerPrices, useAgentRuns } from '@/hooks/queries';
import { useSSE } from '@/hooks/useSSE';
import { Signal, Order, Strategy } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { LoadingScreen } from '@/components/ui/Spinner';
import { AgentRunsTable } from '@/components/domain/AgentRunsTable';
import {
  Layers,
  ArrowUpRight,
  ArrowDownRight,
  Clock,
  Shield,
  RefreshCw,
  Search,
  Download,
  X,
  Bot,
} from 'lucide-react';
import { formatDateTime, formatNumber, formatPct, formatUsd } from '@/lib/format';
import type { MessageKey } from '@/i18n';
import { useT } from '@/i18n';

// Was three separate top-level pages (Positions, Execution Log, Agent
// History) - all three are filtered views over the same underlying object,
// one strategy's trading activity. Merged into one page with tabs, the
// pattern TradingView/MetaTrader use for "many actions/views on one
// object" instead of paging between unrelated-looking top-level routes.
type ActivityTab = 'positions' | 'fills' | 'agent';

const TABS: { key: ActivityTab; label: MessageKey }[] = [
  { key: 'positions', label: 'activity.tabPositions' },
  { key: 'fills', label: 'activity.tabFills' },
  { key: 'agent', label: 'activity.tabAgent' },
];

export function Activity() {
  const t = useT();
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
          <h1 className="text-2xl font-bold text-foreground">{t('activity.title')}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('activity.subtitle')}
          </p>
        </div>

        <div className="flex items-center gap-2 bg-card/20 p-1 rounded-lg border border-border/30">
          {TABS.map((tb) => (
            <button
              key={tb.key}
              onClick={() => setTab(tb.key)}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
                tab === tb.key ? 'bg-secondary text-foreground shadow' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {t(tb.label)}
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
  const t = useT();
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
        tickers.find((tk) => tk.Symbol === sig.symbol)?.Price ||
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
              {t('activity.open')}
            </button>
            <button
              onClick={() => setStatusTab('closed')}
              className={`px-3 py-1.5 rounded-md text-xs font-semibold transition-all ${
                statusTab === 'closed' ? 'bg-secondary text-foreground shadow' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {t('activity.closed')}
            </button>
          </div>

          <div className="flex items-center gap-3 flex-1 max-w-sm">
            <div className="relative w-full">
              <Search className="w-4 h-4 text-muted-foreground absolute left-3 top-2.5" />
              <input
                type="text"
                placeholder={t('activity.searchPositions')}
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="form-input pl-9 text-xs"
              />
            </div>
          </div>

          <div className="flex items-center gap-4">
            <div className="text-right">
              <span className="text-[11px] text-muted-foreground block">
                {statusTab === 'open' ? t('activity.totalUnrealized') : t('activity.totalRealized')}
              </span>
              <span className={`font-mono text-sm font-bold ${totalPnL >= 0 ? 'text-success' : 'text-destructive'}`}>
                {formatUsd(totalPnL, { signed: true, digits: 2 })}
              </span>
            </div>
            <button
              onClick={() => refetchSignals()}
              className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs p-1.5"
              title={t('activity.refreshPositions')}
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      <Card>
        <CardHeader
          title={statusTab === 'open' ? t('activity.liveOpen') : t('activity.closedAudit')}
          subtitle={t('activity.showingPositions', { count: filteredPositions.length })}
        />

        {isSignalsLoading && !signals ? (
          <LoadingScreen message={t('activity.loadingPositions')} />
        ) : filteredPositions.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
            {statusTab === 'open' ? t('activity.noOpen') : t('activity.noClosed')}
          </div>
        ) : (
          <div className="overflow-x-auto rounded border border-border/60">
            <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_td]:whitespace-nowrap [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40 [&_thead]:bg-card/40">
              <thead>
                <tr>
                  <th>{t('activity.colSignalId')}</th>
                  <th>{t('activity.colSymbol')}</th>
                  <th>{t('activity.colStrategy')}</th>
                  <th>{t('activity.colEntryPrice')}</th>
                  {statusTab === 'open' ? <th>{t('activity.colCurrentPrice')}</th> : <th>{t('activity.colExitPrice')}</th>}
                  <th>{t('activity.colStopLoss')}</th>
                  <th>{t('activity.colQuantity')}</th>
                  <th>{t('activity.colInvested')}</th>
                  <th>{statusTab === 'open' ? t('activity.colUnrealized') : t('activity.colRealized')}</th>
                  <th>{t('activity.colOpenedAt')}</th>
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
                      <td className="font-mono">${formatNumber(pos.entryPrice, { digits: 2 })}</td>
                      <td className="font-mono">${formatNumber(pos.currentPrice, { digits: 2 })}</td>
                      <td className="font-mono text-muted-foreground">
                        {pos.stopLossPrice > 0 ? (
                          <div className="flex items-center gap-1 text-warning/90">
                            <Shield className="w-3 h-3 flex-shrink-0" />
                            <span>${formatNumber(pos.stopLossPrice, { digits: 2 })}</span>
                          </div>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="font-mono text-xs text-muted-foreground">{formatNumber(pos.quantity, { digits: 4 })}</td>
                      <td className="font-mono text-xs text-muted-foreground">${formatNumber(pos.invested, { digits: 2 })}</td>
                      <td className="font-mono">
                        <div className="flex items-center gap-1">
                          {isPositive ? (
                            <ArrowUpRight className="w-3.5 h-3.5 text-success flex-shrink-0" />
                          ) : (
                            <ArrowDownRight className="w-3.5 h-3.5 text-destructive flex-shrink-0" />
                          )}
                          <span className={`font-semibold ${isPositive ? 'text-success' : 'text-destructive'}`}>
                            {formatUsd(pos.pnl, { signed: true, digits: 2 })} ({formatPct(pos.pnlPct)})
                          </span>
                        </div>
                      </td>
                      <td className="text-xs text-muted-foreground">
                        <div className="flex items-center gap-1">
                          <Clock className="w-3 h-3 text-muted-foreground" />
                          <span>
                            {formatDateTime(openedDate)}
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
  const t = useT();
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
      t('activity.csvOrderId'), t('activity.csvBrokerId'), t('activity.colSignalId'), t('activity.colStrategy'),
      t('activity.colSymbol'), t('activity.colEntryPrice'), t('activity.colExitPrice'), t('activity.colQuantity'),
      t('activity.colInvested'), t('activity.csvFees'), t('activity.csvProfit'), t('activity.csvTimestamp'),
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
    return <LoadingScreen message={t('activity.loadingFills')} />;
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
                placeholder={t('activity.searchFills')}
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="form-input pl-9 text-xs"
              />
            </div>
          </div>
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground font-medium">{t('activity.symbolFilter')}</span>
              <select value={symbolFilter} onChange={(e) => setSymbolFilter(e.target.value)} className="form-select text-xs py-1">
                <option value="all">{t('activity.allSymbols')}</option>
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
              <span>{t('activity.exportCsv')}</span>
            </button>
            <button
              onClick={() => refetchSignals()}
              className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs p-1.5"
              title={t('activity.refreshLogs')}
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      <Card>
        <CardHeader title={t('activity.fillsTitle')} subtitle={t('activity.fillsSubtitle', { count: filteredOrders.length })} />

        {filteredOrders.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
            {t('activity.noFills')}
          </div>
        ) : (
          <div className="overflow-x-auto rounded border border-border/60">
            <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-foreground [&_th]:uppercase [&_th]:text-[10px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_td]:whitespace-nowrap [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40 [&_thead]:bg-card/40">
              <thead>
                <tr>
                  <th>{t('activity.colOrderId')}</th>
                  <th>{t('activity.colBrokerRef')}</th>
                  <th>{t('activity.colSymbol')}</th>
                  <th>{t('activity.colStrategy')}</th>
                  <th>{t('activity.colEntryPrice')}</th>
                  <th>{t('activity.colExitPrice')}</th>
                  <th>{t('activity.colQuantity')}</th>
                  <th>{t('activity.colFees')}</th>
                  <th>{t('activity.colRealized')}</th>
                  <th>{t('activity.colFillTime')}</th>
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
                      <td className="font-mono text-xs">${formatNumber(order.entry_price, { digits: 2 })}</td>
                      <td className="font-mono text-xs">{order.exit_price > 0 ? `$${formatNumber(order.exit_price, { digits: 2 })}` : '—'}</td>
                      <td className="font-mono text-xs">{formatNumber(order.quantity, { digits: 4 })}</td>
                      <td className="font-mono text-xs text-muted-foreground">${formatNumber(totalFee, { digits: 3 })}</td>
                      <td className="font-mono text-xs">
                        <span className={`font-semibold ${isProfit ? 'text-success' : 'text-destructive'}`}>
                          {formatUsd(order.profit, { signed: true, digits: 2 })}
                        </span>
                      </td>
                      <td className="text-xs text-muted-foreground font-mono">{formatDateTime(order.created_at)}</td>
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
  const t = useT();
  const strategyIdParam = searchParams.get('strategy_id');
  const strategyId = strategyIdParam ? parseInt(strategyIdParam, 10) : undefined;

  const [limit, setLimit] = useState(20);
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
    return <LoadingScreen message={t('activity.loadingRuns')} />;
  }

  return (
    <Card>
      <div className="flex items-center justify-between gap-2 mb-4">
        <CardHeader
          className="mb-0"
          title={t('activity.agentRuns')}
          subtitle={t('activity.agentRunsSubtitle', { count: sorted.length, scope: strategyId != null ? t('activity.forStrategy', { id: strategyId }) : '' })}
        />
        <div className="flex items-center gap-2 shrink-0">
          {strategyId != null && (
            <button
              onClick={clearFilter}
              className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs py-1.5 px-2.5 flex items-center gap-1.5"
              title={t('activity.clearFilter')}
            >
              <span>{t('activity.strategyChip', { id: strategyId })}</span>
              <X className="w-3 h-3" />
            </button>
          )}
          <button
            onClick={() => refetch()}
            className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs p-1.5"
            title={t('common.refresh')}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {sorted.length === 0 ? (
        <div className="p-8 text-center text-xs text-muted-foreground bg-background/40 rounded-lg">
          {t('activity.noRuns')}
        </div>
      ) : (
        <AgentRunsTable runs={sorted} />
      )}

      {sorted.length >= limit && (
        <div className="mt-3 flex justify-center">
          <button
            onClick={() => setLimit((l) => l + 20)}
            className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs py-1.5 px-4"
          >
            {t('activity.loadMore')}
          </button>
        </div>
      )}
    </Card>
  );
}
