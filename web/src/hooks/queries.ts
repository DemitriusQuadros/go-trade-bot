import { useQuery, useMutation, useQueryClient, useInfiniteQuery } from '@tanstack/react-query';
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
  AgentHistoryTurn,
  AgentRequest,
  AgentReportFilter,
  WebhookTargetRequest,
  ProposalFilter,
  ProposalDecisionRequest,
  DeployGateConfig,
} from '@/api/types';
import { ChatContextKey, strategyIdOfKey } from '@/lib/chatTurn';
import { planTurnCards } from '@/lib/toolRefs';

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
  agentRuns: (strategyId?: number) => ['agentRuns', strategyId],
  agentRun: (id: number) => ['agentRuns', 'detail', id],
  chatTranscript: (contextKey: string) => ['agentRuns', 'chat', contextKey],
  scriptVersions: (strategyId: number) => ['strategies', strategyId, 'versions'],
  // Agents platform (A-02 §5)
  agents: ['agents'],
  agent: (id: number) => ['agents', id],
  agentRunsForAgent: (id: number) => ['agents', id, 'runs'],
  agentUsage: (id: number, days: number) => ['agents', id, 'usage', days],
  // Deliberately NOT under the ['agents'] prefix: agent mutations
  // invalidate that prefix and shouldn't refetch the market status.
  agentMarketSymbols: ['agentMarketSymbols'],
  agentReports: (filter?: AgentReportFilter) => (filter ? ['agentReports', filter] : ['agentReports']),
  agentReport: (id: number) => ['agentReports', 'detail', id],
  webhookTargets: ['webhookTargets'],
  strategyMemory: (strategyId: number) => ['strategyMemory', strategyId],
  // Agents platform Phase B (B-01 §5). Every proposal key starts with
  // 'proposals', so invalidating QUERY_KEYS.proposals refreshes the inbox,
  // open detail pages AND the sidebar's pending-count badge at once.
  proposals: ['proposals'],
  proposalList: (filter: Omit<ProposalFilter, 'limit' | 'before_id'>) => ['proposals', 'list', filter],
  proposal: (id: number) => ['proposals', 'detail', id],
  pendingProposalCount: ['proposals', 'pendingCount'],
  deployGate: ['deployGate'],
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

export function useDeleteBacktest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteBacktest(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.backtests() });
    },
  });
}

// AI Strategy Agent (frontend-01/02)
export function useAgentRuns(strategyId?: number, limit = 20) {
  return useQuery({
    queryKey: QUERY_KEYS.agentRuns(strategyId),
    queryFn: ({ signal }) => api.listAgentRuns({ limit, strategy_id: strategyId || undefined }, { signal }),
  });
}

const CHAT_TRANSCRIPT_PAGE = 20;

// Chat history for one conversation context (Phase D-02 §1): chat_ui runs
// for a strategy, or with no strategy for `general`. Newest first, 20 per
// page; ChatSessionContext reverses it into a transcript. The `new` draft
// context has no server history.
export function useChatTranscript(contextKey: ChatContextKey) {
  const strategyId = strategyIdOfKey(contextKey);
  return useInfiniteQuery({
    queryKey: QUERY_KEYS.chatTranscript(contextKey),
    queryFn: ({ pageParam, signal }) =>
      api.listAgentRuns(
        {
          limit: CHAT_TRANSCRIPT_PAGE,
          trigger: ['chat_ui'],
          // D-01 contract (reconciled): strategy_id=none selects runs with no strategy.
          strategy_id: strategyId ?? 'none',
          before_id: pageParam,
        },
        { signal },
      ),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) =>
      lastPage && lastPage.length >= CHAT_TRANSCRIPT_PAGE ? lastPage[lastPage.length - 1].id : undefined,
    enabled: contextKey !== 'new',
    // The live session's turns are client state; history only needs a
    // refresh when the operator comes back to it later.
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
  });
}

export function useAgentRun(id: number) {
  return useQuery({
    queryKey: QUERY_KEYS.agentRun(id),
    queryFn: ({ signal }) => api.getAgentRun(id, { signal }),
    enabled: !!id,
  });
}

export function useSendAgentMessage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      input,
      strategyId,
      history,
      agentId,
    }: {
      input: string;
      strategyId?: number;
      history?: AgentHistoryTurn[];
      agentId?: number;
    }) => api.sendAgentMessage(input, strategyId, history, undefined, agentId),
    onSuccess: (run) => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.agentRuns() });
      // A tool call in this run (e.g. save_strategy_script) may have written
      // straight to the DB out from under any cached strategy data - without
      // this, the Workbench's `useStrategy(id)` stays on the pre-agent
      // snapshot until the next full navigation, so the editor never reflects
      // what the agent just persisted, and a later "Save Changes" click
      // clobbers the agent's write with that stale snapshot.
      if (run.strategy_id != null) {
        queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategy(run.strategy_id) });
      }
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategies });
      // The run counts toward its agent's today_cost_usd / last_run.
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.agents });
      if (run.strategy_id != null) {
        queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategyMemory(run.strategy_id) });
      }
      // Phase D-02 §7: refresh whatever the turn's cards show.
      const plan = planTurnCards(run.tool_calls ?? []);
      if (plan.hasReport) queryClient.invalidateQueries({ queryKey: QUERY_KEYS.agentReports() });
      if (plan.hasProposal) queryClient.invalidateQueries({ queryKey: QUERY_KEYS.proposals });
      for (const id of plan.codeStrategyIds) {
        queryClient.invalidateQueries({ queryKey: QUERY_KEYS.scriptVersions(id) });
        queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategy(id), exact: true });
      }
    },
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

export function useDeleteStrategy() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteStrategy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategies });
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

export const useScriptVersions = (id: number) => {
  return useQuery({
    queryKey: QUERY_KEYS.scriptVersions(id),
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

// --- Agents platform (A-02 §5) ------------------------------------------

export function useAgents() {
  return useQuery({
    queryKey: QUERY_KEYS.agents,
    queryFn: ({ signal }) => api.listAgents({ signal }),
  });
}

export function useAgent(id: number) {
  return useQuery({
    queryKey: QUERY_KEYS.agent(id),
    queryFn: ({ signal }) => api.getAgent(id, { signal }),
    enabled: !!id,
  });
}

export function useCreateAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: AgentRequest) => api.createAgent(req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.agents });
    },
  });
}

export function useUpdateAgent(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: AgentRequest) => api.updateAgent(id, req),
    onSuccess: () => {
      // QUERY_KEYS.agents is a prefix of agent(id), so this covers both.
      qc.invalidateQueries({ queryKey: QUERY_KEYS.agents });
    },
  });
}

export function useDeleteAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteAgent(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.agents });
    },
  });
}

export function usePauseAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, paused }: { id: number; paused: boolean }) => api.pauseAgent(id, paused),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.agents });
    },
  });
}

export function useRunAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, prompt }: { id: number; prompt?: string }) => api.runAgent(id, prompt),
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.agentRunsForAgent(id) });
      qc.invalidateQueries({ queryKey: QUERY_KEYS.agents });
    },
  });
}

// C-01 §6 / C-02 §5: market-watch status, polled every 60s.
export function useMarketSymbols() {
  return useQuery({
    queryKey: QUERY_KEYS.agentMarketSymbols,
    queryFn: ({ signal }) => api.getMarketSymbols({ signal }),
    refetchInterval: 60_000,
  });
}

const AGENT_RUNS_PAGE = 20;

// Cursor-paginated (before_id) run history for one agent.
export function useAgentRunsForAgent(id: number) {
  return useInfiniteQuery({
    queryKey: QUERY_KEYS.agentRunsForAgent(id),
    queryFn: ({ pageParam, signal }) =>
      api.listAgentRunsForAgent(id, AGENT_RUNS_PAGE, pageParam, { signal }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) =>
      lastPage && lastPage.length >= AGENT_RUNS_PAGE ? lastPage[lastPage.length - 1].id : undefined,
    enabled: !!id,
  });
}

export function useAgentUsage(id: number, days = 30) {
  return useQuery({
    queryKey: QUERY_KEYS.agentUsage(id, days),
    queryFn: ({ signal }) => api.getAgentUsage(id, days, { signal }),
    enabled: !!id,
  });
}

const AGENT_REPORTS_PAGE = 30;

export function useAgentReports(filter: Omit<AgentReportFilter, 'limit' | 'before_id'>) {
  return useInfiniteQuery({
    queryKey: QUERY_KEYS.agentReports(filter),
    queryFn: ({ pageParam, signal }) =>
      api.listAgentReports({ ...filter, limit: AGENT_REPORTS_PAGE, before_id: pageParam }, { signal }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) =>
      lastPage && lastPage.length >= AGENT_REPORTS_PAGE ? lastPage[lastPage.length - 1].id : undefined,
  });
}

export function useAgentReport(id: number) {
  return useQuery({
    queryKey: QUERY_KEYS.agentReport(id),
    queryFn: ({ signal }) => api.getAgentReport(id, { signal }),
    enabled: !!id,
  });
}

export function useWebhookTargets() {
  return useQuery({
    queryKey: QUERY_KEYS.webhookTargets,
    queryFn: ({ signal }) => api.listWebhookTargets({ signal }),
  });
}

export function useSaveWebhookTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id?: number; req: WebhookTargetRequest }) =>
      id ? api.updateWebhookTarget(id, req) : api.createWebhookTarget(req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.webhookTargets });
    },
  });
}

export function useDeleteWebhookTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteWebhookTarget(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.webhookTargets });
    },
  });
}

export function useTestWebhookTarget() {
  return useMutation({
    mutationFn: (id: number) => api.testWebhookTarget(id),
  });
}

const MEMORY_PAGE = 20;

// Newest-first shared memory for one strategy, paginated with before_id.
export function useStrategyMemory(strategyId: number) {
  return useInfiniteQuery({
    queryKey: QUERY_KEYS.strategyMemory(strategyId),
    queryFn: ({ pageParam, signal }) =>
      api.listStrategyMemory(strategyId, { limit: MEMORY_PAGE, before_id: pageParam }, { signal }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) =>
      lastPage && lastPage.length >= MEMORY_PAGE ? lastPage[lastPage.length - 1].id : undefined,
    enabled: !!strategyId,
  });
}

export function useAddStrategyMemoryNote(strategyId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (content: string) => api.addStrategyMemoryNote(strategyId, content),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.strategyMemory(strategyId) });
    },
  });
}

// Global kill switch: PUT /agents/kill-switch {"paused"} (A-02). GET
// /settings still reports agents_paused read-only, so the settings query is
// invalidated to reflect the new state.
export function useSetAgentsPaused() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (paused: boolean) => api.setAgentsKillSwitch(paused),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.settings });
      qc.invalidateQueries({ queryKey: QUERY_KEYS.agents });
    },
  });
}

// --- Agents platform Phase B: proposals + deploy gate (B-01 §5) ----------

const PROPOSALS_PAGE = 30;

// Cursor-paginated (before_id) proposal list. `filter.status` is sent to
// the API as-is (one status or a comma-separated list).
export function useProposals(filter: Omit<ProposalFilter, 'limit' | 'before_id'>, enabled = true) {
  return useInfiniteQuery({
    queryKey: QUERY_KEYS.proposalList(filter),
    queryFn: ({ pageParam, signal }) =>
      api.listProposals({ ...filter, limit: PROPOSALS_PAGE, before_id: pageParam }, { signal }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) =>
      lastPage && lastPage.length >= PROPOSALS_PAGE ? lastPage[lastPage.length - 1].id : undefined,
    enabled,
  });
}

// Pending-proposal count for the sidebar badge, polled every 60s (B-02 §2).
export function usePendingProposalCount() {
  return useQuery({
    queryKey: QUERY_KEYS.pendingProposalCount,
    queryFn: async ({ signal }) => (await api.getPendingProposalCount({ signal }))?.count ?? 0,
    refetchInterval: 60_000,
    // A 404 while the backend route doesn't exist yet shouldn't spam retries.
    retry: false,
  });
}

// One proposal. While it's `approved` (waiting for the target to go flat)
// it refetches every 30s so target_has_open_position and the status flip to
// `applied` show up without a reload (B-02 §3).
export function useProposal(id: number) {
  return useQuery({
    queryKey: QUERY_KEYS.proposal(id),
    queryFn: ({ signal }) => api.getProposal(id, { signal }),
    enabled: !!id,
    refetchInterval: (query) => (query.state.data?.status === 'approved' ? 30_000 : false),
  });
}

// Approve/reject may change a strategy's source (immediate apply for a
// non-live gate_failed_change) and write memory entries, so both refresh
// proposals, strategies and every strategy's memory (B-02 §1).
function invalidateAfterDecision(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: QUERY_KEYS.proposals });
  qc.invalidateQueries({ queryKey: QUERY_KEYS.strategies });
  qc.invalidateQueries({ queryKey: ['strategyMemory'] });
}

export function useApproveProposal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: number; req?: ProposalDecisionRequest }) => api.approveProposal(id, req),
    onSuccess: (detail, { id }) => {
      if (detail) qc.setQueryData(QUERY_KEYS.proposal(id), detail);
      invalidateAfterDecision(qc);
    },
    // A 409 (superseded) also changed server state - refetch either way.
    onError: () => invalidateAfterDecision(qc),
  });
}

export function useRejectProposal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: number; req?: ProposalDecisionRequest }) => api.rejectProposal(id, req),
    onSuccess: (detail, { id }) => {
      if (detail) qc.setQueryData(QUERY_KEYS.proposal(id), detail);
      invalidateAfterDecision(qc);
    },
    onError: () => invalidateAfterDecision(qc),
  });
}

export function useDeployGateConfig() {
  return useQuery({
    queryKey: QUERY_KEYS.deployGate,
    queryFn: ({ signal }) => api.getDeployGateConfig({ signal }),
  });
}

export function useUpdateDeployGateConfig() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: DeployGateConfig) => api.updateDeployGateConfig(req),
    onSuccess: (saved) => {
      if (saved) qc.setQueryData(QUERY_KEYS.deployGate, saved);
      qc.invalidateQueries({ queryKey: QUERY_KEYS.deployGate });
    },
  });
}
