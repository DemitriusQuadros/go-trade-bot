// web/src/api/client.ts
import { ScriptVersion, 
  Account,
  Strategy,
  StrategyCreateRequest,
  StrategyUpdateRequest,
  Signal,
  TickerPrice,
  Candle,
  BacktestRun,
  RunBacktestRequest,
  WalkForwardRequest,
  MonteCarloSummary,
  OptimizationStatusResponse,
  OptimizationResults,
  CreateOptimizationRequest,
  StrategyPerformance,
  PerformanceSnapshot,
  PlatformSettings,
  PlatformSettingsUpdateRequest,
  PlatformSettingsUpdateResponse,
  CandleImportRequest,
  CandleImportJob,
  ImportSchedule,
  ImportScheduleCreateRequest,
  FastRerunRequest,
  FastRerunResponse,
  ReplRequest,
  ReplResponse,
  AgentRun,
  AgentRunFilter,
  AgentHistoryTurn,
  Agent,
  AgentRequest,
  AgentRunEnqueuedResponse,
  AgentUsage,
  MarketSymbolsResponse,
  AgentReportSummary,
  AgentReport,
  AgentReportFilter,
  WebhookTarget,
  WebhookTargetRequest,
  WebhookTestResponse,
  StrategyMemoryEntry,
  Proposal,
  ProposalDetail,
  ProposalFilter,
  ProposalDecisionRequest,
  DeployGateConfig,
  PendingProposalCount,
  Me,
  MeResult,
  LoginRequest,
  LoginResponse,
  UpdateMeRequest,
  User,
  UserCreateRequest,
  UserUpdateRequest,
} from './types';

// Migration hygiene (auth-02 §1): the shared-token login is gone - the session
// now lives in the HttpOnly `gtb_session` cookie. Drop the old token once.
try {
  localStorage.removeItem('gtb_api_token');
} catch {
  /* storage unavailable - nothing to clean up */
}

// CSRF (auth-01 §4): every cookie-authenticated non-GET request must carry
// this header; it is sent on every request so the rule never has to be
// remembered per call site.
// auth-01 contract (reconciled): header name/value `X-Requested-With: gtb`.
const CSRF_HEADER = 'X-Requested-With';
const CSRF_VALUE = 'gtb';

// Every backend route lives under "/api" (see cmd/api/main.go's
// NewServeMux) so it can never collide with an SPA client-side route of the
// same bare name (e.g. "/backtest", "/settings") - a full-page load on one
// of those used to hit the backend's JSON handler instead of the SPA. Call
// sites below pass bare paths ("/backtest", not "/api/backtest"); this is
// the one place that adds the prefix.
export const API_PREFIX = '/api';

export class ApiError extends Error {
  constructor(public status: number, public body: string) {
    super(`HTTP ${status}: ${body}`);
    this.name = 'ApiError';
  }
}

export class NetworkError extends Error {
  constructor(public cause: unknown) {
    super('Network request failed');
    this.name = 'NetworkError';
  }
}

// Session handlers, registered by AuthContext / the toast bridge:
// - onUnauthorized: any 401 -> back to the login screen.
// - onAccessRequired: 403 {"error":"access_required"} (Cloudflare Access
//   session expired) -> full-page "reload to sign in" message.
// - onForbidden: any other 403 on a mutation -> the backend's message in a
//   toast (the backend is the source of truth for permissions).
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: (() => void) | null): void {
  onUnauthorized = fn;
}

let onAccessRequired: (() => void) | null = null;
export function setAccessRequiredHandler(fn: (() => void) | null): void {
  onAccessRequired = fn;
}

let onForbidden: ((message: string) => void) | null = null;
export function setForbiddenHandler(fn: ((message: string) => void) | null): void {
  onForbidden = fn;
}

// auth-01 contract (reconciled): Cloudflare Access error code `access_required` (§4).
export const ACCESS_REQUIRED_CODE = 'access_required';

interface RequestOptions {
  signal?: AbortSignal;
  // Auth probes (/auth/me, /auth/login) handle 401 themselves - a wrong
  // password must not bounce the login screen through the global handler.
  skipUnauthorizedHandler?: boolean;
}

async function request<T>(method: string, path: string, body?: unknown, opts?: RequestOptions): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    [CSRF_HEADER]: CSRF_VALUE,
  };

  let res: Response;
  try {
    res = await fetch(`${API_PREFIX}${path}`, {
      method,
      headers,
      // The session cookie (gtb_session) - same origin only.
      credentials: 'same-origin',
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: opts?.signal,
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err;
    }
    throw new NetworkError(err);
  }

  if (res.status === 401) {
    const text = await res.text().catch(() => '');
    if (!opts?.skipUnauthorizedHandler) onUnauthorized?.();
    throw new ApiError(401, text);
  }
  if (res.status === 403) {
    const err = new ApiError(403, await res.text().catch(() => ''));
    if (apiErrorCode(err) === ACCESS_REQUIRED_CODE) {
      onAccessRequired?.();
    } else if (method !== 'GET' && method !== 'HEAD') {
      onForbidden?.(apiErrorMessage(err, 'You don\'t have permission to do that.'));
    }
    throw err;
  }
  if (!res.ok) {
    throw new ApiError(res.status, await res.text().catch(() => ''));
  }
  if (res.status === 204) {
    return null as T;
  }
  const text = await res.text();
  if (!text) {
    return null as T;
  }
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new Error(`Failed to parse response body from ${method} ${path}`);
  }
}

export const api = {
  get: <T>(path: string, opts?: { signal?: AbortSignal }) =>
    request<T>('GET', path, undefined, opts),
  post: <T>(path: string, body?: unknown, opts?: { signal?: AbortSignal }) =>
    request<T>('POST', path, body, opts),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  delete: <T>(path: string, opts?: { signal?: AbortSignal }) =>
    request<T>('DELETE', path, undefined, opts),

  // Account
  getAccount: (opts?: { signal?: AbortSignal }) =>
    api.get<Account>('/account', opts),

  // Strategies
  getStrategies: (opts?: { signal?: AbortSignal }) =>
    api.get<Strategy[]>('/strategy', opts),
  getStrategy: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<Strategy>(`/strategy/${id}`, opts),
  createStrategy: (data: StrategyCreateRequest) =>
    api.post<Strategy>('/strategy', data),
  updateStrategy: (id: number, data: StrategyUpdateRequest) =>
    api.put<Strategy>(`/strategy/${id}`, data),
  // Permanently removes the strategy AND everything that references it
  // (signals, orders, executions, backtests, optimization runs,
  // performance snapshots, script state/versions, agent chat history -
  // see app/repository/strategy.Delete's doc comment). No undo. The
  // backend blocks this with a 409 for a productive strategy or one with
  // open positions - surfaced to the caller as a thrown ApiError.
  deleteStrategy: (id: number) => api.delete<void>(`/strategy/${id}`),
  patchStrategyStatus: (id: number, status: string) =>
    api.patch<Strategy>(`/strategy/${id}/status`, { status }),
  patchStrategyMode: (id: number, mode: string) =>
    api.patch<Strategy>(`/strategy/${id}/mode`, { mode }),
  enqueueStrategy: () =>
    api.post<{ status: string }>('/strategy/enqueue'),
  previewRuleSummary: (ruleDefinition: unknown) =>
    api.post<{ summary: string }>('/strategy/template/preview', { rule_definition: ruleDefinition }),
  getStrategySummary: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<{ summary: string }>(`/strategy/${id}/summary`, opts),
  getScriptVersions: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<ScriptVersion[]>(`/strategy/${id}/versions`, opts),
  revertScriptVersion: (id: number, versionId: number) =>
    api.post<{ message: string }>(`/strategy/${id}/versions/${versionId}/revert`),


  // Platform Settings
  getSettings: (opts?: { signal?: AbortSignal }) =>
    api.get<PlatformSettings>('/settings', opts),
  updateSettings: (data: PlatformSettingsUpdateRequest) =>
    api.put<PlatformSettingsUpdateResponse>('/settings', data),

  // Candle Import & Scheduling
  startCandleImport: (req: CandleImportRequest) =>
    api.post<{ job_id: string; status: 'pending' }>('/candles/import', req),
  getCandleImportJob: (jobId: string, opts?: { signal?: AbortSignal }) =>
    api.get<CandleImportJob>(`/candles/import/${jobId}`, opts),
  listImportSchedules: (opts?: { signal?: AbortSignal }) =>
    api.get<ImportSchedule[]>('/candles/schedule', opts),
  createImportSchedule: (req: ImportScheduleCreateRequest) =>
    api.post<ImportSchedule>('/candles/schedule', req),
  patchImportSchedule: (id: number, patch: Partial<Pick<ImportSchedule, 'enabled' | 'cron_spec'>>) =>
    api.patch<ImportSchedule>(`/candles/schedule/${id}`, patch),
  deleteImportSchedule: (id: number) =>
    api.delete<void>(`/candles/schedule/${id}`),

  // Signals / Positions
  getSignals: (status?: 'open' | 'closed', opts?: { signal?: AbortSignal }) => {
    const query = status ? `?status=${status}` : '';
    return api.get<Signal[]>(`/signal${query}`, opts);
  },
  getSignal: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<Signal>(`/signal/${id}`, opts),

  // Market data
  getTickerPrices: (symbol?: string, opts?: { signal?: AbortSignal }) => {
    const query = symbol ? `?symbol=${symbol}` : '';
    return api.get<TickerPrice[]>(`/broker/prices${query}`, opts);
  },
  getKlines: (symbol: string, interval = '1m', limit = 100, opts?: { signal?: AbortSignal }) =>
    api.get<Candle[]>(`/broker/klines?symbol=${symbol}&interval=${interval}&limit=${limit}`, opts),

  // Backtest
  runBacktest: (req: RunBacktestRequest) =>
    api.post<BacktestRun>('/backtest', req),
  runWalkForward: (req: WalkForwardRequest) =>
    api.post<BacktestRun>('/backtest/walkforward', req),
  getBacktest: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<BacktestRun>(`/backtest/${id}`, opts),
  listBacktests: (strategyId?: number, opts?: { signal?: AbortSignal }) => {
    const query = strategyId ? `?strategy_id=${strategyId}` : '';
    return api.get<BacktestRun[]>(`/backtest${query}`, opts);
  },
  deleteBacktest: (id: number) => api.delete<void>(`/backtest/${id}`),
  runMonteCarlo: (runId: number, iterations = 1000) =>
    api.post<MonteCarloSummary>(`/backtest/${runId}/montecarlo`, { iterations }),
  getMonteCarlo: (runId: number, opts?: { signal?: AbortSignal }) =>
    api.get<MonteCarloSummary>(`/backtest/${runId}/montecarlo`, opts),
  // iframe / new-tab URL. The session cookie rides along (same origin), so
  // no credential is ever put in the URL.
  getReportUrl: (runId: number) => `${API_PREFIX}/backtest/${runId}/report`,

  // Optimization
  startOptimization: (req: CreateOptimizationRequest) =>
    api.post<{ id: number }>('/optimize', req),
  // GET /optimize/{id} (not /optimize/{id}/status - see
  // app/handler/web/optimize/handler.go's GetByID doc comment: this route
  // itself is the poll target) returns the StatusResponse shape.
  getOptimizationStatus: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<OptimizationStatusResponse>(`/optimize/${id}`, opts),
  getOptimizationResults: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<OptimizationResults>(`/optimize/${id}/results`, opts),

  // Performance
  getPerformanceHistory: (opts?: { signal?: AbortSignal }) =>
    api.get<StrategyPerformance[]>('/performance', opts),
  getPerformanceSnapshots: (strategyId?: number, opts?: { signal?: AbortSignal }) => {
    const query = strategyId ? `?strategy_id=${strategyId}` : '';
    return api.get<PerformanceSnapshot[]>(`/performance/snapshots${query}`, opts);
  },

  // Strategy Scripting & REPL (frontend-02)
  fastRerun: (req: FastRerunRequest, opts?: { signal?: AbortSignal }) =>
    api.post<FastRerunResponse>('/script/fast-rerun', req, opts),
  repl: (req: ReplRequest, opts?: { signal?: AbortSignal }) =>
    api.post<ReplResponse>('/script/repl', req, opts),

  // AI Strategy Agent (frontend-01/02) - a second transport onto the same
  // AgentUseCase.RunToolLoop cmd/mcp exposes over MCP. sendAgentMessage
  // blocks until the full RunToolLoop turn completes (no streaming in v1).
  // strategyId is an optional additive hint (see app/handler/web/agent's
  // sendMessageRequest.StrategyID) - sent by the chat (ChatSessionContext)
  // whenever its context is a strategy (the Workbench dock, or Agent mode's
  // strategy picker), so the agent's answers can be strategy-aware without
  // the operator repeating "for strategy #N".
  // history is every prior turn of the CURRENT session (built by
  // ChatSessionContext from its own transcript) - without it, every message started a
  // brand-new RunToolLoop with zero memory of anything discussed or
  // drafted earlier in the same chat, which made iterating on a script
  // ("draft this, now tighten the stop loss") impossible.
  sendAgentMessage: (
    input: string,
    strategyId?: number,
    history?: AgentHistoryTurn[],
    opts?: { signal?: AbortSignal },
    // agent_id (A-02 §5): which persona answers - omitted means the default
    // "Copilot" agent. A paused/halted agent answers 409.
    agentId?: number,
  ) => api.post<AgentRun>('/agent/runs', { input, strategy_id: strategyId, history, agent_id: agentId }, opts),
  // Filtered, cursor-paginated run listing (Phase D-01 §1), newest first.
  // D-01 contract (reconciled): trigger is comma-separated; strategy_id may be the literal "none".
  listAgentRuns: (filter: AgentRunFilter = {}, opts?: { signal?: AbortSignal }) =>
    api.get<AgentRun[]>(
      `/agent/runs${buildQuery({
        limit: filter.limit ?? 20,
        strategy_id: filter.strategy_id,
        trigger: filter.trigger?.length ? filter.trigger.join(',') : undefined,
        agent_id: filter.agent_id,
        before_id: filter.before_id,
        // auth-01 contract (reconciled): mine param - `mine=true` filters to the caller's own runs (§6).
        mine: filter.mine ? 'true' : undefined,
      })}`,
      opts,
    ),
  getAgentRun: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<AgentRun>(`/agent/runs/${id}`, opts),

  // --- Agents platform (A-02 §5) --------------------------------------
  listAgents: (opts?: { signal?: AbortSignal }) => api.get<Agent[]>('/agents', opts),
  getAgent: (id: number, opts?: { signal?: AbortSignal }) => api.get<Agent>(`/agents/${id}`, opts),
  createAgent: (req: AgentRequest) => api.post<Agent>('/agents', req),
  updateAgent: (id: number, req: AgentRequest) => api.put<Agent>(`/agents/${id}`, req),
  deleteAgent: (id: number) => api.delete<void>(`/agents/${id}`),
  pauseAgent: (id: number, paused: boolean) => api.post<Agent>(`/agents/${id}/pause`, { paused }),
  runAgent: (id: number, prompt?: string) =>
    api.post<AgentRunEnqueuedResponse>(`/agents/${id}/run`, prompt ? { prompt } : {}),
  listAgentRunsForAgent: (id: number, limit = 20, beforeId?: number, opts?: { signal?: AbortSignal }) =>
    api.get<AgentRun[]>(
      `/agents/${id}/runs${buildQuery({ limit, before_id: beforeId })}`,
      opts,
    ),
  getAgentUsage: (id: number, days = 30, opts?: { signal?: AbortSignal }) =>
    api.get<AgentUsage>(`/agents/${id}/usage?days=${days}`, opts),

  // C-01 §6: cmd/agent's market-watch status snapshot (read from Redis by cmd/api).
  getMarketSymbols: async (opts?: { signal?: AbortSignal }): Promise<MarketSymbolsResponse> => {
    const body = await api.get<MarketSymbolsResponse | null>('/agents/market-symbols', opts);
    return { symbols: body?.symbols ?? [], runtime_seen_at: body?.runtime_seen_at ?? null };
  },

  // Global agents kill switch. PUT /settings ignores agents_paused; this is
  // the only way to change it.
  setAgentsKillSwitch: (paused: boolean) =>
    api.put<{ agents_paused: boolean }>('/agents/kill-switch', { paused }),

  // Agent reports
  listAgentReports: (filter: AgentReportFilter = {}, opts?: { signal?: AbortSignal }) =>
    api.get<AgentReportSummary[]>(`/agent-reports${buildQuery({ ...filter })}`, opts),
  getAgentReport: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<AgentReport>(`/agent-reports/${id}`, opts),
  // iframe src - authenticated by the same-origin session cookie. theme
  // makes the server-rendered report match the app's light/dark mode.
  getAgentReportHtmlUrl: (id: number, theme?: 'dark' | 'light') =>
    `${API_PREFIX}/agent-reports/${id}/html${buildQuery({ theme })}`,

  // Webhook targets (agent notifications)
  listWebhookTargets: (opts?: { signal?: AbortSignal }) =>
    api.get<WebhookTarget[]>('/webhook-targets', opts),
  createWebhookTarget: (req: WebhookTargetRequest) =>
    api.post<WebhookTarget>('/webhook-targets', req),
  updateWebhookTarget: (id: number, req: WebhookTargetRequest) =>
    api.put<WebhookTarget>(`/webhook-targets/${id}`, req),
  deleteWebhookTarget: (id: number) => api.delete<void>(`/webhook-targets/${id}`),
  testWebhookTarget: (id: number) =>
    api.post<WebhookTestResponse>(`/webhook-targets/${id}/test`),

  // Shared per-strategy memory
  listStrategyMemory: (
    strategyId: number,
    params: { kinds?: string[]; limit?: number; before_id?: number } = {},
    opts?: { signal?: AbortSignal },
  ) =>
    api.get<StrategyMemoryEntry[]>(
      `/strategies/${strategyId}/memory${buildQuery({
        kinds: params.kinds?.length ? params.kinds.join(',') : undefined,
        limit: params.limit,
        before_id: params.before_id,
      })}`,
      opts,
    ),
  addStrategyMemoryNote: (strategyId: number, content: string) =>
    api.post<StrategyMemoryEntry>(`/strategies/${strategyId}/memory`, { content }),

  // --- Agents platform Phase B: proposals + deploy gate (B-01 §5) -------
  listProposals: (filter: ProposalFilter = {}, opts?: { signal?: AbortSignal }) =>
    api.get<Proposal[]>(`/proposals${buildQuery({ ...filter })}`, opts),
  getProposal: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<ProposalDetail>(`/proposals/${id}`, opts),
  // Only from `pending`. Returns the detail DTO; 409 `superseded` when the
  // target's source changed since the proposal was filed (the backend marks
  // it superseded) - surfaced as a thrown ApiError(409).
  approveProposal: (id: number, req: ProposalDecisionRequest = {}) =>
    api.post<ProposalDetail>(`/proposals/${id}/approve`, req),
  rejectProposal: (id: number, req: ProposalDecisionRequest = {}) =>
    api.post<ProposalDetail>(`/proposals/${id}/reject`, req),
  getPendingProposalCount: (opts?: { signal?: AbortSignal }) =>
    api.get<PendingProposalCount>('/proposals/pending-count', opts),
  getDeployGateConfig: (opts?: { signal?: AbortSignal }) =>
    api.get<DeployGateConfig>('/deploy-gate', opts),
  updateDeployGateConfig: (req: DeployGateConfig) =>
    api.put<DeployGateConfig>('/deploy-gate', req),

  // Read-only kill switch state for non-admins (GET /settings is admin-only).
  // auth-01 contract (reconciled): GET /agents/kill-switch is not in the §3 route map -
  // callers treat any error as "state unknown" and render nothing.
  getAgentsKillSwitch: (opts?: { signal?: AbortSignal }) =>
    api.get<{ agents_paused: boolean }>('/agents/kill-switch', opts),

  // --- Auth (auth-01 §4) -----------------------------------------------
  // GET /auth/me decides between the login screen and the app.
  // auth-01 contract (reconciled): setup_required may come back as a 200 or a 401 body;
  // both are handled. A 401 without it means "not signed in".
  getMe: async (opts?: { signal?: AbortSignal }): Promise<MeResult> => {
    try {
      const body = await request<(Me & { setup_required?: boolean }) | null>('GET', '/auth/me', undefined, {
        ...opts,
        skipUnauthorizedHandler: true,
      });
      if (body?.setup_required) return { kind: 'setup_required' };
      if (!body || typeof body.id !== 'number') return { kind: 'anonymous' };
      return { kind: 'user', me: body };
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        return apiErrorBodyFlag(err, 'setup_required') ? { kind: 'setup_required' } : { kind: 'anonymous' };
      }
      throw err;
    }
  },
  // auth-01 contract (reconciled): 200 {user} (§4); 401 invalid_credentials, 429 rate_limited.
  login: (req: LoginRequest) =>
    request<LoginResponse>('POST', '/auth/login', req, { skipUnauthorizedHandler: true }),
  logout: () => request<void>('POST', '/auth/logout', undefined, { skipUnauthorizedHandler: true }),
  // auth-01 contract (reconciled): PATCH /auth/me returns the updated `me` shape.
  updateMe: (req: UpdateMeRequest) => api.patch<Me>('/auth/me', req),

  // --- Users (auth-01 §5, admin) ------------------------------------------
  // auth-01 contract (reconciled): list is a bare array; create/update return the user.
  listUsers: (opts?: { signal?: AbortSignal }) => api.get<User[]>('/users', opts),
  createUser: (req: UserCreateRequest) => api.post<User>('/users', req),
  updateUser: (id: number, req: UserUpdateRequest) => api.put<User>(`/users/${id}`, req),
  resetUserPassword: (id: number, password: string) =>
    api.post<void>(`/users/${id}/reset-password`, { password }),
  deleteUser: (id: number) => api.delete<void>(`/users/${id}`),
};

// True when a JSON error body carries `flag: true` (e.g. setup_required).
function apiErrorBodyFlag(err: ApiError, flag: string): boolean {
  try {
    const parsed = JSON.parse(err.body ?? '') as Record<string, unknown>;
    return parsed[flag] === true;
  } catch {
    return false;
  }
}

// buildQuery renders "?a=1&b=x" from the defined (non-undefined, non-empty)
// entries of params, or "" when there are none.
function buildQuery(params: Record<string, string | number | undefined | null>): string {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue;
    qs.set(k, String(v));
  }
  const str = qs.toString();
  return str ? `?${str}` : '';
}

// apiErrorCode returns the machine-readable `error` code of a
// {"error": code, "message": text} API error body, or null.
export function apiErrorCode(err: unknown): string | null {
  if (!(err instanceof ApiError)) return null;
  try {
    const parsed = JSON.parse(err.body ?? '') as { error?: unknown };
    return typeof parsed.error === 'string' && parsed.error ? parsed.error : null;
  } catch {
    return null;
  }
}

// apiErrorMessage extracts the human-readable message from a failed call.
// Handlers answer errors as {"error": code, "message": text} (A-02 §5);
// older ones answer plain text. Falls back to the Error's own message.
export function apiErrorMessage(err: unknown, fallback = 'Request failed'): string {
  if (err instanceof ApiError) {
    const body = err.body?.trim();
    if (body) {
      try {
        const parsed = JSON.parse(body) as { message?: unknown; error?: unknown };
        if (typeof parsed.message === 'string' && parsed.message) return parsed.message;
        if (typeof parsed.error === 'string' && parsed.error) return parsed.error;
      } catch {
        return body;
      }
    }
    return `${fallback} (HTTP ${err.status})`;
  }
  if (err instanceof NetworkError) return 'Could not reach the server. Check that the API is running.';
  if (err instanceof Error && err.message) return err.message;
  return fallback;
}
