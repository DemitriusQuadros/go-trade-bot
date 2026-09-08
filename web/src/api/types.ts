// web/src/api/types.ts

// --- Account -----------------------------------------------------------
export interface Account {
  id?: number;
  amount: number;
  available_orders?: number;
  currency?: string;
  created_at?: string;
  updated_at?: string;
}

// --- Strategy ------------------------------------------------------------
export type StrategyStatus = 'productive' | 'testing' | 'disabled';
export type StrategyMode = 'backtest' | 'dryrun' | 'paper' | 'live';

export interface Strategy {
  id: number;
  name: string;
  description: string;
  strategy_name: string;    // registry key — the field to display/filter by
  status: StrategyStatus;
  mode: StrategyMode;
  monitored_symbols: string[];
  cycle: number;             // minutes
  configuration: Record<string, unknown> | null; // raw JSONB, algorithm-specific
  created_at: string;
  updated_at: string;
}

export interface StrategyCreateRequest {
  name: string;
  description: string;
  strategy_name: string;
  status: StrategyStatus;
  mode: StrategyMode;
  monitored_symbols: string[];
  cycle: number;
  configuration: Record<string, unknown>;
}

export interface StrategyUpdateRequest {
  name: string;
  description: string;
  strategy_name: string;
  status: StrategyStatus;
  mode: StrategyMode;
  monitored_symbols: string[];
  cycle: number;
  configuration: Record<string, unknown>;
}

// --- Signal / Order (Positions) ------------------------------------------
export type SignalStatus = 'open' | 'closed';
export type MarginType = 'isolated' | 'cross';

export interface Order {
  id: number;
  signal_id: number;
  broker_order_id: string;
  stop_loss_order_id: string;
  stop_loss_price: number;
  entry_price: number;
  exit_price: number;
  quantity: number;
  invested_amount: number;
  margin_type: MarginType;
  entry_fee: number;
  exit_fee: number;
  leverage: number;
  executed_qty: number;
  is_closing: boolean;
  profit: number;
  created_at: string;
  updated_at: string;
}

export interface Signal {
  id: number;
  symbol: string;
  strategy_id: number;
  status: SignalStatus;
  orders: Order[];
  created_at: string;
  updated_at: string;
}

// --- Broker (market data) -------------------------------------------------
export interface TickerPrice {
  Symbol: string;
  Price: number;
}

export interface Candle {
  Symbol: string;
  Timeframe: string;
  OpenTime: string;
  Open: number;
  High: number;
  Low: number;
  Close: number;
  Volume: number;
}

// --- Backtest --------------------------------------------------------------
export interface RunBacktestRequest {
  strategy_id: number;
  symbol: string;
  timeframe: string;
  start_date: string; // RFC3339
  end_date: string;
  initial_capital?: number;
  fill_policy?: {
    slippage_pct?: number;
    fee_pct?: number;
    execution_delay_ms?: number;
  };
}

export interface WalkForwardRequest {
  strategy_id: number;
  symbol: string;
  timeframe: string;
  start_date: string;
  end_date: string;
  initial_capital?: number;
  train_months: number;
  test_months: number;
  step_months: number;
  fill_policy?: {
    slippage_pct?: number;
    fee_pct?: number;
    execution_delay_ms?: number;
  };
}

export interface EquityPoint {
  time: string;
  value: number;
}

export interface DrawdownPoint {
  time: string;
  drawdownPct: number;
}

export interface BacktestRun {
  id: number;
  strategy_id: number;
  symbol: string;
  start_date: string;
  end_date: string;
  is_walk_forward: boolean;
  sharpe: number;
  max_drawdown_pct: number;
  win_rate_pct: number;
  profit_factor: number | 'Infinity';
  total_trades: number;
  total_return_pct: number;
  passed: boolean;
  html_report_path: string;
  equity_curve: EquityPoint[];
  trade_log?: Array<{
    timestamp: string;
    action: string;
    price: number;
    quantity: number;
    pnl?: number;
    reason?: string;
  }>;
  created_at: string;
}

export interface MonteCarloSummary {
  simulations: number;
  mean_return: number;
  median_return: number;
  std_dev: number;
  var_95: number;
  cvar_95: number;
  max_drawdown_p95: number;
  ruin_probability: number;
  distribution: Array<{ bin: number; count: number }>;
}

// --- Optimization ----------------------------------------------------------
export type OptimizationStatus = 'pending' | 'running' | 'completed' | 'failed';

export interface ParamRange {
  min: number;
  max: number;
  step: number;
}

export interface CreateOptimizationRequest {
  strategy_id: number;
  symbol: string;
  timeframe: string;
  start_date: string;
  end_date: string;
  initial_capital?: number;
  param_grid: Record<string, ParamRange>;
}

export interface OptimizationStatusResponse {
  id: number;
  status: OptimizationStatus;
  progress: number;
  total_combinations: number;
  best_config: Record<string, number> | null;
  best_metrics: Record<string, unknown> | null;
  error_message: string | null;
}

export interface OptimizationGridPoint {
  params: Record<string, number>;
  metrics: {
    sharpe?: number;
    max_drawdown_pct?: number;
    win_rate_pct?: number;
    profit_factor?: number | string;
    total_trades?: number;
    total_return_pct?: number;
  } | null;
}

export interface OptimizationResults {
  id: number;
  best_config: Record<string, number>;
  best_metrics: Record<string, unknown>;
  grid: OptimizationGridPoint[];
}

// --- Performance History ---------------------------------------------------
export interface StrategyPerformance {
  strategy_id: number;
  strategy_name: string;
  symbol: string;
  total_pnl: number;
  win_count: number;
  loss_count: number;
  win_rate: number;
}

export interface PerformanceSnapshot {
  id: number;
  strategy_id: number;
  snapshot_date: string;
  realized_pnl: number;
  unrealized_pnl: number;
  win_rate: number;
  sharpe: number;
  max_drawdown_pct: number;
}

// --- SSE Realtime Events ---------------------------------------------------
export interface RealtimePriceEvent {
  symbol: string;
  price: number;
  timestamp: string;
}

export interface RealtimePositionEvent {
  signal_id: number;
  symbol: string;
  strategy_id: number;
  entry_price: number;
  current_price: number;
  unrealized_pnl: number;
  unrealized_pnl_pct: number;
  quantity: number;
  opened_at: string;
}

export interface RealtimeHeartbeatEvent {
  timestamp: string;
}
