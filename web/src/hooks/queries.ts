import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@/api/client';
import { 
  StrategyCreateRequest, 
  StrategyUpdateRequest, 
  RunBacktestRequest, 
  WalkForwardRequest,
  CreateOptimizationRequest 
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
