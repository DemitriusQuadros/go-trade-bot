import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@/api/client';
import { 
  StrategyCreateRequest, 
  StrategyUpdateRequest, 
  RunBacktestRequest, 
  WalkForwardRequest,
  CreateOptimizationRequest,
  PlatformSettingsUpdateRequest,
  CandleImportRequest,
  CandleImportJobStatus,
  ImportScheduleCreateRequest,
  ImportSchedule,
  FastRerunRequest,
  ReplRequest,
} from '@/api/types';

export const QUERY_KEYS = {
  account: ['account'],
  strategies: ['strategies'],
  strategy: (id: number) => ['strategies', id],
  signals: (status?: string) => ['signals', status],
  tickerPrices: (symbol?: string) => ['tickerPrices', symbol],
  backtests: (strategyId?: number) => ['backtests', strategyId],
  backtest: (id: number) => ['backtests', 'detail', id],
  performanceHistory: ['performanceHistory'],
  performanceSnapshots: (strategyId?: number) => ['performanceSnapshots', strategyId],
  montecarlo: (id: number) => ['montecarlo', id],
  optimizationStatus: (id: number) => ['optimization', 'status', id],
  optimizationResults: (id: number) => ['optimization', 'results', id],
  settings: ['settings'],
  candleImportJob: (jobId: string | null) => ['candleImportJob', jobId],
  importSchedules: ['importSchedules'],
};

export function useAccount() {
  return useQuery({
    queryKey: QUERY_KEYS.account,
    queryFn: ({ signal }) => api.getAccount({ signal }),
  });
}

export function useStrategies() {
  return useQuery({
    queryKey: QUERY_KEYS.strategies,
    queryFn: ({ signal }) => api.getStrategies({ signal }),
  });
}

export function useStrategy(id: number) {
  return useQuery({
    queryKey: QUERY_KEYS.strategy(id),
    queryFn: ({ signal }) => api.getStrategy(id, { signal }),
    enabled: !!id,
  });
}

export function useSignals(status?: 'open' | 'closed') {
  return useQuery({
    queryKey: QUERY_KEYS.signals(status),
    queryFn: ({ signal }) => api.getSignals(status, { signal }),
  });
}

export function usePerformanceHistory() {
  return useQuery({
    queryKey: QUERY_KEYS.performanceHistory,
    queryFn: ({ signal }) => api.getPerformanceHistory({ signal }),
  });
}

export function usePerformanceSnapshots(strategyId?: number) {
  return useQuery({
    queryKey: QUERY_KEYS.performanceSnapshots(strategyId),
    queryFn: ({ signal }) => api.getPerformanceSnapshots(strategyId, { signal }),
  });
}

export function useBacktests(strategyId?: number) {
  return useQuery({
    queryKey: QUERY_KEYS.backtests(strategyId),
    queryFn: ({ signal }) => api.listBacktests(strategyId, { signal }),
  });
}

export function useBacktest(id: number) {
  return useQuery({
    queryKey: QUERY_KEYS.backtest(id),
    queryFn: ({ signal }) => api.getBacktest(id, { signal }),
    enabled: !!id,
  });
}

// Mutations
export function useCreateStrategy() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: StrategyCreateRequest) => api.createStrategy(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategies });
    },
  });
}

export function useUpdateStrategy(id: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: StrategyUpdateRequest) => api.updateStrategy(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategies });
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategy(id) });
    },
  });
}

export function usePatchStrategyStatus(id: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (status: string) => api.patchStrategyStatus(id, status),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategies });
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategy(id) });
    },
  });
}

export function useRunBacktest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: RunBacktestRequest) => api.runBacktest(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.backtests() });
    },
  });
}

export function useStartOptimization() {
  return useMutation({
    mutationFn: (req: CreateOptimizationRequest) => api.startOptimization(req),
  });
}

export function useTickerPrices(symbol?: string) {
  return useQuery({
    queryKey: QUERY_KEYS.tickerPrices(symbol),
    queryFn: ({ signal }) => api.getTickerPrices(symbol, { signal }),
  });
}

// Strategy Template
export function useRuleSummaryPreview(ruleDefinition: unknown, enabled: boolean) {
  return useQuery({
    queryKey: ['ruleSummaryPreview', ruleDefinition],
    queryFn: () => api.previewRuleSummary(ruleDefinition),
    enabled,
    staleTime: 0,
  });
}

// Platform Settings
export function usePlatformSettings() {
  return useQuery({
    queryKey: QUERY_KEYS.settings,
    queryFn: ({ signal }) => api.getSettings({ signal }),
  });
}

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: PlatformSettingsUpdateRequest) => api.updateSettings(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.settings });
    },
  });
}

// Candle Import & Scheduling
const TERMINAL_IMPORT_STATUSES: CandleImportJobStatus[] = ['completed', 'failed'];

export function useStartCandleImport() {
  return useMutation({
    mutationFn: (req: CandleImportRequest) => api.startCandleImport(req),
  });
}

export function useCandleImportJob(jobId: string | null) {
  return useQuery({
    queryKey: QUERY_KEYS.candleImportJob(jobId),
    queryFn: ({ signal }) => api.getCandleImportJob(jobId as string, { signal }),
    enabled: jobId !== null,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && TERMINAL_IMPORT_STATUSES.includes(status) ? false : 2000;
    },
  });
}

export function useImportSchedules() {
  return useQuery({
    queryKey: QUERY_KEYS.importSchedules,
    queryFn: ({ signal }) => api.listImportSchedules({ signal }),
  });
}

export function useCreateImportSchedule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ImportScheduleCreateRequest) => api.createImportSchedule(req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.importSchedules });
    },
  });
}

export function usePatchImportSchedule(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: Partial<Pick<ImportSchedule, 'enabled' | 'cron_spec'>>) =>
      api.patchImportSchedule(id, patch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.importSchedules });
    },
  });
}

export function useDeleteImportSchedule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteImportSchedule(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.importSchedules });
    },
  });
}

// Strategy Scripting & REPL (frontend-02)
export function useFastRerun() {
  return useMutation({
    mutationFn: (req: FastRerunRequest) => api.fastRerun(req),
  });
}

export function useRepl() {
  return useMutation({
    mutationFn: (req: ReplRequest) => api.repl(req),
  });
}


export const useScriptVersions = (id: number) => {
  return useQuery({
    queryKey: ['strategies', id, 'versions'],
    queryFn: ({ signal }) => api.getScriptVersions(id, { signal }),
    enabled: !!id,
  });
};

export const useRevertScriptVersion = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, versionId }: { id: number; versionId: number }) =>
      api.revertScriptVersion(id, versionId),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['strategies', variables.id] });
      queryClient.invalidateQueries({ queryKey: ['strategies', variables.id, 'versions'] });
    },
  });
};
