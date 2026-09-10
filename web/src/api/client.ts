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
} from './types';

const TOKEN_STORAGE_KEY = 'gtb_api_token';

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

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_STORAGE_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_STORAGE_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY);
}

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  opts?: { signal?: AbortSignal }
): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
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
    onUnauthorized?.();
    throw new ApiError(401, await res.text().catch(() => ''));
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
  runMonteCarlo: (runId: number, iterations = 1000) =>
    api.post<MonteCarloSummary>(`/backtest/${runId}/montecarlo`, { iterations }),
  getMonteCarlo: (runId: number, opts?: { signal?: AbortSignal }) =>
    api.get<MonteCarloSummary>(`/backtest/${runId}/montecarlo`, opts),
  getReportUrl: (runId: number) => {
    const token = getToken();
    return `/backtest/${runId}/report${token ? `?token=${encodeURIComponent(token)}` : ''}`;
  },

  // Optimization
  startOptimization: (req: CreateOptimizationRequest) =>
    api.post<{ id: number }>('/optimize', req),
  getOptimizationStatus: (id: number, opts?: { signal?: AbortSignal }) =>
    api.get<OptimizationStatusResponse>(`/optimize/${id}/status`, opts),
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
    api.post<FastRerunResponse>('/api/script/fast-rerun', req, opts),
  repl: (req: ReplRequest, opts?: { signal?: AbortSignal }) =>
    api.post<ReplResponse>('/api/script/repl', req, opts),
};
