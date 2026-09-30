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
  rule_summary?: string;     // present only for template strategies
  script_source?: string;    // Lua source code for script strategies
  // Agents platform B-01 §1: set on a challenger (a dryrun clone an agent
  // iterates on) to its champion's id; null everywhere else.
  challenger_of_id?: number | null;
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
  script_source?: string;
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
  script_source?: string;
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
  strategy_name: string;
  symbol: string;
  // Not in the current BacktestRunResponse DTO; read when a backend adds it.
  timeframe?: string;
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
  execution_trace?: TraceRecord[];
  trade_log?: Array<{
    entry_time: string;
    exit_time: string;
    entry_price: number;
    exit_price: number;
    quantity: number;
    profit: number;
    exit_reason?: string;
    symbol: string;
  }>;
  created_at: string;
}

// Mirrors app/engine/montecarlo.go's DistributionStats exactly - Go's
// encoding/json defaults to the field names as-written (no json tags on
// that struct), so these stay PascalCase rather than the snake_case every
// other DTO in this file uses.
export interface MonteCarloDistributionStats {
  Mean: number;
  Median: number;
  Min: number;
  Max: number;
  P5: number;
  P95: number;
}

// Mirrors app/engine/montecarlo.go's MonteCarloResult. This previously
// assumed a histogram-shaped response (a `distribution` bin/count array)
// that the backend has never produced - RunMonteCarlo reorders the trade
// log Iterations times and reports percentile summary stats per metric,
// not per-iteration raw values, so no histogram is possible from this
// response.
export interface MonteCarloSummary {
  iterations: number;
  // The single backtest's own metrics, recomputed as a baseline - unused
  // by the frontend today (the BacktestRun already on screen has the
  // same figures), so left loose rather than fully mirroring
  // metrics_provider.BacktestMetrics field-for-field.
  original_metrics: Record<string, unknown>;
  sharpe_distribution: MonteCarloDistributionStats;
  max_drawdown_distribution: MonteCarloDistributionStats;
  total_return_distribution: MonteCarloDistributionStats;
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

// --- Strategy Template & Conditions ----------------------------------------
// Note: 'volume_filter' and 'price_change_pct' are documented in the original
// spec (docs/specs/platform-self-service/frontend-02-condition-builder.md) but
// are not implemented by app/strategies/template on the backend, which rejects
// them at save time. They are intentionally omitted here so the builder can
// never construct a condition the API will reject.
export type ConditionType = 'indicator_threshold' | 'indicator_crossing' | 'price_level';

export type Comparator = '>' | '<' | '>=' | '<=' | 'crosses_above' | 'crosses_below';

export type IndicatorKey =
  | 'rsi' | 'ema' | 'sma' | 'atr'
  | 'bollinger_upper' | 'bollinger_middle' | 'bollinger_lower'
  | 'macd' | 'macd_signal' | 'macd_histogram'
  | 'price';

export interface ConditionRef {
  indicator: IndicatorKey;
  params?: Record<string, number>;
}

export interface Condition {
  type: ConditionType;
  indicator?: IndicatorKey;
  params?: Record<string, number>;
  comparator: Comparator;
  value?: number | null;
  reference?: ConditionRef | null;
  field?: 'price';
}

export interface ConditionGroup {
  conditions: Condition[];
  combinator: 'AND' | 'OR';
}

// --- Platform Settings (Hot-swap & Safety) ---------------------------------
export interface DryRunSettings {
  slippage_pct: number;
  fee_pct: number;
  fill_delay_ms: number;
}

export interface PlatformSettings {
  broker_api_key: string;
  broker_api_secret: string;
  broker_testnet_api_key: string;
  broker_testnet_api_secret: string;
  mode: 'backtest' | 'dryrun' | 'paper' | 'live';
  testnet: boolean;
  webhook_url: string;
  dry_run: DryRunSettings;
  prometheus_url: string;
  grafana_url: string;
  asynqmon_url?: string;
  // Fix-02 B5: display-only link to cmd/agent's Asynqmon UI (:9194).
  agents_asynqmon_url?: string;
  // Agents platform global kill switch - READ-ONLY here (PUT /settings
  // ignores it). Changed via PUT /agents/kill-switch. true = no agent runs
  // start, and in-flight runs halt before their next model call.
  agents_paused: boolean;
}

export interface PlatformSettingsUpdateRequest {
  broker_api_key?: string;
  broker_api_secret?: string;
  broker_testnet_api_key?: string;
  broker_testnet_api_secret?: string;
  mode: PlatformSettings['mode'];
  confirm_live?: boolean;
  testnet: boolean;
  webhook_url: string;
  dry_run: DryRunSettings;
  prometheus_url: string;
  grafana_url: string;
  asynqmon_url?: string;
  // Fix-02 B5: display-only link to cmd/agent's Asynqmon UI (:9194).
  agents_asynqmon_url?: string;
}

export interface PlatformSettingsUpdateResponse {
  settings: PlatformSettings;
  applied: true;
}

export interface DrainTimeoutErrorBody {
  error: 'drain_timeout';
  message: string;
}

// --- Candle Import & Scheduling --------------------------------------------
export type CandleImportJobStatus = 'pending' | 'running' | 'completed' | 'failed';

export type CandleImportSource = 'rest' | 'archive';

export interface CandleImportRequest {
  symbols: string[];
  timeframes: string[];
  from: string;   // RFC3339
  to: string;     // RFC3339
  // 'rest' (default, omit to get this) walks the live Binance kline REST
  // API - correct for incremental/ongoing sync, but slow for a deep
  // historical range. 'archive' bulk-downloads Binance's public
  // data.binance.vision monthly kline dumps instead - no rate limits,
  // months of history in seconds per file, at month granularity.
  source?: CandleImportSource;
}

export interface CandleImportPairResult {
  symbol: string;
  timeframe: string;
  candles_imported: number;
  gaps_detected: number;
  error: string;
}

export interface CandleImportResult {
  per_pair: CandleImportPairResult[];
}

export interface CandleImportJob {
  job_id: string;
  status: CandleImportJobStatus;
  result: CandleImportResult | null;
  error: string | null;
}

export interface ImportSchedule {
  id: number;
  symbol: string;
  timeframe: string;
  cron_spec: string;
  enabled: boolean;
  last_run_at: string | null;
  created_at: string;
}

export interface ImportScheduleCreateRequest {
  symbol: string;
  timeframe: string;
  cron_spec: string;
}

// --- Strategy Scripting & Trace (frontend-01, frontend-02) -----------------
export interface TraceCandle {
  o: number;
  h: number;
  l: number;
  c: number;
  v: number;
  t: number;
}

export interface TraceIndicatorCall {
  name: string;
  params: Record<string, number>;
  value: number;
}

export interface TraceLogEntry {
  label: string;
  value: unknown;
}

export interface TraceSignal {
  buy?: { qty: number; price: number };
  sell?: { qty: number; price: number };
  stop_loss?: { qty: number; price: number };
  take_profit?: { qty: number; price: number };
}

export interface TracePlotPoint {
  name: string;
  value: number;
  color: string;
  overlay: boolean;
}

export interface TraceRecord {
  timestamp: string;
  candle?: TraceCandle;
  indicators: TraceIndicatorCall[];
  signal?: TraceSignal;
  log: TraceLogEntry[];
  plots: TracePlotPoint[];
}

export interface FastRerunRequest {
  strategy_id?: number;
  source: string;
  symbol: string;
  timeframe?: string;
  end_time?: string | null;
  window_candles?: number;
}

export interface FastRerunResponse {
  trace: TraceRecord[];
  error?: string;
  error_line?: number;
  data_available?: boolean;
}

export interface ReplRequest {
  source: string;
  symbol: string;
  timeframe?: string;
  end_time?: string | null;
  window_candles?: number;
}

export interface ReplResponse {
  result: unknown;
  trace: TraceRecord[];
  error?: string;
  error_line?: number;
  data_available?: boolean;
}


export interface ScriptVersion {
  id: number;
  strategy_id: number;
  source: string;
  created_at: string;
}

// AI Strategy Agent (docs/specs/ai-strategy-agent). AgentToolCall mirrors
// app/handler/web/agent's actual toolCallResponse DTO shape - the real
// persisted shape (tool/args/result/error/timestamp) rather than the
// {tool,args,result_summary} shape frontend-01's spec originally assumed
// before the backend was implemented.
export interface AgentToolCall {
  tool: string;
  args?: Record<string, unknown>;
  result?: string;
  error?: string;
  timestamp?: string;
  // Phase D-01 §3: structured references the backend extracts from the
  // result at response time. Absent on an older backend - read refs only
  // through toolRefs() (lib/toolRefs.ts), which falls back to parsing.
  // D-01 contract (reconciled): field name `refs`, always [] (never null) when present.
  refs?: ToolRef[];
}

// Phase D-01 §3. `role` only disambiguates two refs of the same kind from
// one call (challenger/champion, baseline/candidate, created).
export type ToolRefKind = 'strategy' | 'backtest' | 'report' | 'proposal' | 'agent' | 'memory' | 'optimization';

export interface ToolRef {
  kind: ToolRefKind;
  id: number;
  role?: string;
}

// GET /agent/runs query (Phase D-01 §1). Every field optional; `strategy_id:
// 'none'` returns only runs with no strategy. Newest first, before_id cursor.
// D-01 contract (reconciled): param names limit/strategy_id/trigger/agent_id/before_id,
// trigger sent comma-separated, strategy_id=none literal.
export interface AgentRunFilter {
  limit?: number;
  strategy_id?: number | 'none';
  trigger?: string[];
  agent_id?: number;
  before_id?: number;
  // auth-01 contract (reconciled): mine param - only the caller's own runs (§6).
  mine?: boolean;
}

// AgentHistoryTurn is the request-side shape sent as `history` on every
// POST /agent/runs call - ChatSessionContext builds this from its own
// prior turns of the current session (see app/usecase/agent.PriorTurn)
// so the agent has real conversational memory: without this, every message
// started a brand-new, context-free RunToolLoop with no idea what an
// earlier message in the same chat had discussed or drafted.
export interface AgentHistoryTurn {
  input: string;
  tool_calls?: AgentToolCall[];
  response_text?: string;
}

// 'running' from creation until the run finishes (older rows were created as 'ok').
export type AgentRunStatus = 'running' | 'ok' | 'error';
export type AgentRunTrigger = 'mcp_tool' | 'chat_ui' | 'monitor' | 'cron' | 'manual' | 'event' | 'market' | 'chain';

// Per-trigger AgentRun.trigger_detail shapes (C-01 §2.3, §3, §4). Every
// field is optional: detail JSON is written by the backend at enqueue time
// and older rows (or a partially populated detail) must still render.
export interface CronTriggerDetail {
  cron?: string;
}
export interface ManualTriggerDetail {
  requested_by?: string;
  prompt?: string;
}
export interface EventTriggerDetail {
  event?: AgentEventType | string;
  strategy_id?: number;
  symbol?: string;
  occurred_at?: string;
  data?: Record<string, unknown>;
}
export interface MarketTriggerDetail {
  rule_id?: string;
  symbol?: string;
  kind?: MarketRuleKind | string;
  window_minutes?: number;
  observed_pct?: number; // pct_move, signed (e.g. -3.4 for a drop)
  observed_multiplier?: number; // volatility_spike
  threshold?: number;
  price?: number;
  at?: string;
}
export interface ChainTriggerDetail {
  source_agent_id?: number;
  source_run_id?: number;
  report_ids?: number[];
  on?: ChainOn | string;
}

// Discriminated union of (trigger, trigger_detail). Built by
// runTriggerInfo() (lib/triggers.ts) so a narrowing switch on `trigger` types the detail.
export type AgentRunTriggerInfo =
  | { trigger: 'cron'; detail: CronTriggerDetail }
  | { trigger: 'manual'; detail: ManualTriggerDetail }
  | { trigger: 'event'; detail: EventTriggerDetail }
  | { trigger: 'market'; detail: MarketTriggerDetail }
  | { trigger: 'chain'; detail: ChainTriggerDetail }
  | { trigger: 'other'; raw: string; detail: Record<string, unknown> };


export interface AgentRun {
  id: number;
  // Agents platform (A-02 §5) additions - optional because rows recorded
  // before the platform landed carry none of them.
  agent_id?: number;
  agent_name?: string;
  // Raw JSON from the backend, shape keyed by `trigger` - narrow it with
  // runTriggerInfo(run) (lib/triggers.ts) rather than reading fields off it directly.
  trigger_detail?:
    | CronTriggerDetail
    | ManualTriggerDetail
    | EventTriggerDetail
    | MarketTriggerDetail
    | ChainTriggerDetail
    | null;
  // Chain runs only (C-01 §4); absent on other runs -> depth 0.
  chain_depth?: number;
  parent_run_id?: number | null;
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
  provider: string;
  model: string;
  trigger: AgentRunTrigger | string;
  status: AgentRunStatus;
  error_message?: string;
  input_summary: string;
  response_text?: string;
  tool_calls: AgentToolCall[];
  strategy_id?: number;
  started_at: string;
  finished_at?: string;
  // Fix-02 B1: true when the tool loop hit its iteration cap and the run
  // finished via the forced tools-disabled final turn.
  hit_iteration_cap?: boolean;
}

// --- Agents platform (docs/specs/agents-platform, A-02 §5) -----------------
// Every type below mirrors a snake_case DTO from A-02 §5 exactly - rename
// nothing here without updating the backend contract.

export type AgentPermission =
  | 'read'
  | 'backtest'
  | 'optimize'
  | 'edit_testing'
  | 'notify'
  | 'create_strategy' // B-01: create new testing/dryrun strategies (max 3/day)
  | 'propose_live' // B-01: propose challenger promotions (human approves each)
  | 'chain'; // C-01 §4: trigger_agent tool - start other agents (max 3 per run)

export type AgentProvider = '' | 'anthropic' | 'gemini';

// --- Phase C triggers (C-01 §1 - the exact JSON contract) -----------------

export type AgentEventType =
  | 'position.opened'
  | 'position.closed'
  | 'stoploss.hit'
  | 'strategy.error'
  | 'strategy.panic'
  | 'drawdown'
  | 'no_signal';

export interface EventTrigger {
  type: AgentEventType | string;
  threshold_pct?: number; // drawdown only
  window_hours?: number; // drawdown (default 24), no_signal (required)
  cooldown_minutes?: number; // default 15 (drawdown/no_signal: 240)
}

export type MarketRuleKind = 'pct_move' | 'volatility_spike';

export interface MarketRule {
  id: string; // stable client-generated uuid - never regenerate on edit
  symbol: string;
  kind: MarketRuleKind | string;
  window_minutes: number; // 1..1440
  threshold_pct?: number; // pct_move
  multiplier?: number; // volatility_spike
  cooldown_minutes: number; // default 60, min 5
}

export type ChainOn = 'report' | 'notify' | 'success';

export interface ChainTrigger {
  agent_id: number;
  on: ChainOn | string;
}

export interface AgentTriggers {
  cron?: string[];
  events?: EventTrigger[];
  market?: MarketRule[];
  chain_from?: ChainTrigger[];
}

// C-01 §6: per-agent trigger counts for the agents table.
export interface AgentTriggerSummary {
  cron: number;
  events: number;
  market: number;
  chain_from: number;
}

// C-01 §6: GET /agents/market-symbols row.
export interface MarketSymbolStatus {
  symbol: string;
  watchers: number;
  last_price: number | null;
  last_candle_at: string | null;
}

// GET /agents/market-symbols body. runtime_seen_at is null when cmd/agent
// has written no status snapshot.
export interface MarketSymbolsResponse {
  symbols: MarketSymbolStatus[];
  runtime_seen_at: string | null;
}

export interface AgentRequest {
  name: string;
  goal: string;
  provider: AgentProvider;
  model: string;
  permissions: AgentPermission[];
  triggers: AgentTriggers;
  strategy_ids: number[];
  webhook_target_ids: number[];
  daily_budget_usd: number;
  max_auto_deploys_per_day: number;
  // Fix-02 B1: tool-loop iteration cap. 0 = trigger default (24 for
  // scheduled/triggered runs, 8 for chat); otherwise 4-50.
  max_iterations?: number;
}

export interface AgentLastRun {
  id: number;
  status: AgentRunStatus | string;
  trigger: AgentRunTrigger | string;
  started_at: string;
}

export interface Agent extends AgentRequest {
  id: number;
  paused: boolean;
  is_default: boolean;
  created_at: string;
  updated_at: string;
  today_cost_usd: number;
  last_run: AgentLastRun | null;
  next_run_at: string | null; // UTC RFC3339; null if no cron specs or paused
  // C-01 §6. Optional so an API build without it degrades to counting
  // `triggers` client-side.
  trigger_summary?: AgentTriggerSummary;
}

export interface AgentRunEnqueuedResponse {
  enqueued: boolean;
  task_id: string;
}

export interface UsageDay {
  day: string; // YYYY-MM-DD (UTC)
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  runs: number;
}

export interface AgentUsage {
  today: UsageDay;
  days: UsageDay[];
}

export type ReportSeverity = 'info' | 'warning' | 'critical';

export interface AgentReportSummary {
  id: number;
  agent_id: number;
  agent_name: string;
  agent_run_id: number | null;
  strategy_ids: number[];
  title: string;
  severity: ReportSeverity | string;
  summary: string;
  created_at: string;
}

export interface AgentReport extends AgentReportSummary {
  blocks: unknown; // raw JSON block list - rendering is server-side (/html)
}

export interface AgentReportFilter {
  agent_id?: number;
  strategy_id?: number;
  severity?: ReportSeverity;
  limit?: number;
  before_id?: number;
}

export type WebhookTargetKind = 'generic' | 'discord' | 'slack' | 'telegram';

// url and secret come back MASKED on every read. Sending a masked value
// back unchanged on PUT means "keep the stored value" (same semantics as
// the broker secrets in /settings).
export interface WebhookTarget {
  id: number;
  name: string;
  kind: WebhookTargetKind;
  url: string; // generic/discord/slack only
  secret: string; // telegram bot token only
  chat_id: string; // telegram only
  enabled: boolean;
}

export type WebhookTargetRequest = Omit<WebhookTarget, 'id'>;

export interface WebhookTestResponse {
  ok: boolean;
  error?: string;
}

export type StrategyMemoryKind = 'journal' | 'chat_user' | 'chat_agent' | 'report_ref' | 'finding';

export interface StrategyMemoryEntry {
  id: number;
  strategy_id: number;
  author_agent_id: number | null;
  author_name: string; // agent name, or "operator"
  agent_run_id: number | null;
  kind: StrategyMemoryKind | string;
  content: string;
  ref_id: number | null; // AgentReport id for report_ref entries
  created_at: string;
}

// --- Agents platform Phase B: proposals + deploy gate (B-01 §5) -------------

export type ProposalStatus = 'pending' | 'approved' | 'rejected' | 'applied' | 'superseded' | 'failed';
export type ProposalKind = 'promote_challenger' | 'gate_failed_change';

// GET /proposals list item (B-01 §5).
export interface Proposal {
  id: number;
  kind: ProposalKind | string;
  target_strategy_id: number;
  target_strategy_name: string;
  challenger_strategy_id: number | null;
  agent_id: number;
  agent_name: string;
  rationale: string;
  status: ProposalStatus | string;
  early: boolean;
  created_at: string;
  decided_at: string | null;
  // auth-01 contract (reconciled): display name of the user who approved/rejected (§7);
  // absent on older backends and undecided proposals.
  decided_by?: string | null;
  applied_at: string | null;
  failure_reason: string;
  // Deploy-gate outcome recorded in the evidence; null when no gate ran.
  gate_passed?: boolean | null;
}

// GET /proposals/{id} - the list item plus the fields below (B-01 §5).
export interface ProposalDetail extends Proposal {
  base_source: string;
  proposed_source: string;
  evidence: unknown; // raw JSON - decode with parseProposalEvidence()
  report_id: number | null;
  decision_note: string;
  target_has_open_position: boolean; // computed live on every GET
  target_current_source_matches_base: boolean;
  // When apply_proposal last checked an approved proposal's target for a
  // flat position. Null before the first check - the UI then falls back to
  // the time this page last refetched (target_has_open_position is live).
  last_flat_check_at?: string | null;
}

export interface ProposalFilter {
  // One status, or a comma-separated list ("applied,rejected,...").
  status?: ProposalStatus | string;
  strategy_id?: number;
  agent_id?: number;
  limit?: number;
  before_id?: number;
}

export interface ProposalDecisionRequest {
  note?: string;
}

// Decoded `evidence` (B-01 §3/§4): {gate:{passed, checks, baseline_run_id,
// candidate_run_id}, forward_test?:{challenger, champion, since}}. Every
// field optional - the payload is raw JSON and the UI must survive partial
// shapes. Non-finite floats arrive as the strings "+Inf"/"-Inf"/"NaN".
export interface GateCheck {
  name?: string;
  passed?: boolean;
  candidate?: number | null;
  baseline?: number | null;
  threshold?: number | null;
  detail?: string;
}

export interface GateResult {
  passed?: boolean;
  checks?: GateCheck[];
  baseline_run_id?: number | null;
  candidate_run_id?: number | null;
}

export interface ForwardTestSide {
  trades?: number | null;
  win_rate_pct?: number | null;
  net_pnl?: number | null;
  max_adverse?: number | null;
}

export interface ForwardTestEvidence {
  challenger?: ForwardTestSide;
  champion?: ForwardTestSide;
  since?: string;
}

export interface ProposalEvidence {
  gate?: GateResult;
  forward_test?: ForwardTestEvidence;
}

// GET/PUT /deploy-gate (B-01 §1, §5).
export interface PendingProposalCount {
  count: number;
}

export interface DeployGateConfig {
  min_sharpe_delta: number;
  max_drawdown_ratio: number;
  min_trades: number;
  min_profit_factor: number;
  lookback_months: number;
  train_months: number;
  test_months: number;
  timeframe: string; // "" = the strategy's own cycle-derived timeframe
}

// --- Auth & users (auth-01 §1, §2, §4, §5) ---------------------------------

export type UserRole = 'admin' | 'friend' | 'viewer';

// `admin` implies every other capability (auth-01 §2).
export type Capability = 'view' | 'backtest' | 'edit_drafts' | 'agent_chat' | 'approve_proposals' | 'admin';

// GET /auth/me.
// auth-01 contract (reconciled): field names per §4.
export interface Me {
  id: number;
  username: string;
  display_name: string;
  email: string;
  role: UserRole;
  capabilities: Capability[];
  locale: string;
  daily_agent_budget_usd: number;
  today_agent_cost_usd: number;
}

// What GET /auth/me resolved to.
export type MeResult = { kind: 'user'; me: Me } | { kind: 'anonymous' } | { kind: 'setup_required' };

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  user: Me;
}

export interface UpdateMeRequest {
  display_name?: string;
  locale?: string;
  current_password?: string;
  new_password?: string;
}

// GET /users list item (never carries a password hash).
// auth-01 contract (reconciled): field names per §1/§5.
export interface User {
  id: number;
  username: string;
  display_name: string;
  email: string;
  role: UserRole;
  capabilities: Capability[];
  daily_agent_budget_usd: number;
  today_agent_cost_usd: number;
  disabled: boolean;
  last_login_at: string | null;
  created_at?: string;
}

export interface UserCreateRequest {
  username: string;
  display_name: string;
  email?: string;
  role: UserRole;
  password: string;
}

export interface UserUpdateRequest {
  display_name: string;
  email: string;
  role: UserRole;
  capabilities?: Capability[];
  daily_agent_budget_usd: number;
  disabled: boolean;
}
