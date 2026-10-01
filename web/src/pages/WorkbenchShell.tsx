import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, Outlet, useNavigate, useParams } from 'react-router-dom';
import { NavLink } from 'react-router-dom';
import { useCloneStrategyAsDraft, useStrategy } from '@/hooks/queries';
import { apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { useToast } from '@/context/ToastContext';
import { canEditStrategy } from '@/lib/permissions';
import {
  Strategy,
  StrategyStatus,
  StrategyMode,
  TraceRecord,
  BacktestRun,
} from '@/api/types';
import { SharedPriceChart } from '@/components/charts/SharedPriceChart';
import { TraceAnnotationPanel } from '@/components/charts/TraceAnnotationPanel';
import { ConsolePanel } from '@/components/domain/ConsolePanel';
import { AgentNotesPanel } from '@/components/domain/AgentNotesPanel';
import { LoadingScreen } from '@/components/ui/Spinner';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { usePersistedOpen, usePersistedEnum, usePersistedNumber } from '@/hooks/usePersistedOpen';
import { useRegisterEditorBridge } from '@/context/EditorBridgeContext';
import {
  ArrowLeft,
  Code2,
  Terminal,
  History,
  PanelBottomClose,
  PanelBottomOpen,
  SquareCode,
  Columns2,
  ChartCandlestick,
  RefreshCw,
  GitBranch,
  Lock,
  Copy,
  Loader2,
  Maximize2,
  Minimize2,
  Activity,
} from 'lucide-react';
import { formatTime } from '@/lib/format';
import { useT } from '@/i18n';

// Editor tab content vs. the chart: which gets the screen. 'split' is the
// original side-by-side layout; 'script'/'chart' each give one of them the
// full remaining width (see the CONTENT_VIEWS toggle below) - this is
// separate from consolePanelOpen (the bottom panel), which stays a simple
// on/off.
type ContentView = 'script' | 'split' | 'chart';
const CONTENT_VIEWS: readonly ContentView[] = ['script', 'split', 'chart'];

// Split-view divider drag bounds, in px - keeps either side from being
// dragged down to something unusably thin regardless of window size.
const SPLIT_MIN_PX = 260;
const SPLIT_CHART_MIN_PX = 360;
// The drag handle's w-1.5 + mx-1 (6px + 2 x 4px).
const SPLIT_DIVIDER_PX = 14;
const SPLIT_DEFAULT_PX = 460;

// No 10: Binance has no 10m kline interval (see entities.IsValidCycle).
export const CYCLE_OPTIONS = [1, 5, 15, 30, 60] as const;
export type CycleMinutes = (typeof CYCLE_OPTIONS)[number];

// Kline interval for a cycle, mirroring entities.Strategy.GetBrokerInterval:
// Binance has no "60m" interval, so a 60-minute cycle is "1h".
export function cycleTimeframe(minutes: number): string {
  return minutes === 60 ? '1h' : `${minutes}m`;
}

export interface ScriptEditorState {
  strategyId: number | null;
  name: string;
  description: string;
  symbols: string[];
  cycleMinutes: CycleMinutes;
  source: string;
  stopLossPct: number | null;
  positionSizing: { type: 'fixed_amount' | 'pct_capital'; value: number | null };
  status: StrategyStatus;
  mode: StrategyMode;
  previewSymbol: string;
}

export const DEFAULT_LUA_TEMPLATE = `-- Lua Strategy Script (gopher-lua sandboxed)
-- Hooks called by the execution loop:
-- before(ctx), should_long(ctx), go_long(ctx), should_short(ctx), go_short(ctx),
-- update_position(ctx), after(ctx), terminate(ctx)
--
-- ind.rsi(period) RAISES a Lua error (not nil) when there isn't yet enough
-- candle history - always read it through pcall so an early-cycle "not
-- enough data" doesn't surface as a hard error every cycle.
--
-- go_long/go_short must return a table shaped {buy={qty=...}} / {sell={qty=...}}
-- - the field is \`qty\`, not \`quantity\`. (\`quantity\` is what an OPEN
-- position's size is called when reading ctx.position - a different field.)

local function safe_rsi(period)
  local ok, val = pcall(ind.rsi, period)
  if ok then
    return val
  end
  return nil
end

function before(ctx)
  -- Called at cycle initialization
end

function should_long(ctx)
  -- Example: buy when RSI(14) is below 30 (oversold)
  local rsi_val = safe_rsi(14)
  if rsi_val and rsi_val < 30 then
    debug.log("rsi_oversold", rsi_val)
    plot("rsi", rsi_val)
    return true
  end
  return false
end

function go_long(ctx)
  -- Return entry signal
  return {
    buy = { qty = 0.01, price = ctx.price },
  }
end

function should_short(ctx)
  local rsi_val = safe_rsi(14)
  if rsi_val and rsi_val > 70 then
    debug.log("rsi_overbought", rsi_val)
    plot("rsi", rsi_val)
    return true
  end
  return false
end

function go_short(ctx)
  return {
    sell = { qty = 0.01, price = ctx.price },
  }
end

function update_position(ctx)
  -- Without this hook, a position opened above never closes except via an
  -- exchange-level stop-loss. Example: close the long on RSI reversion.
  local rsi_val = safe_rsi(14)
  if not rsi_val or not ctx.position then
    return nil
  end
  if rsi_val >= 50 then
    return { sell = { qty = ctx.position.quantity, price = ctx.price } }
  end
  return nil
end
`;

export function toEditorState(strat: Strategy): ScriptEditorState {
  const cfg = strat.configuration || {};
  const sizing = (cfg.position_sizing as any) || { type: 'pct_capital', value: 10 };
  const stopLoss = typeof cfg.stop_loss_pct === 'number' ? cfg.stop_loss_pct : null;
  const symbols = strat.monitored_symbols || ['BTCUSDT'];

  return {
    strategyId: strat.id,
    name: strat.name || '',
    description: strat.description || '',
    symbols,
    cycleMinutes: (CYCLE_OPTIONS.includes(strat.cycle as any) ? strat.cycle : 15) as CycleMinutes,
    source: strat.script_source || DEFAULT_LUA_TEMPLATE,
    stopLossPct: stopLoss,
    positionSizing: {
      type: sizing.type === 'fixed_amount' ? 'fixed_amount' : 'pct_capital',
      value: typeof sizing.value === 'number' ? sizing.value : 10,
    },
    status: strat.status || 'disabled',
    mode: strat.mode || 'dryrun',
    previewSymbol: symbols[0] || 'BTCUSDT',
  };
}

export function toStrategyConfiguration(state: ScriptEditorState): Record<string, unknown> {
  return {
    stop_loss_pct: state.stopLossPct,
    position_sizing: {
      type: state.positionSizing.type,
      value: state.positionSizing.value,
    },
  };
}

// --- Console log (frontend-06) ---------------------------------------------
export interface ConsoleEntry {
  id: string;
  timestamp: string;
  source: 'editor' | 'repl' | 'backtest';
  kind: 'error' | 'debug_log';
  label?: string;
  message: string;
}

const CONSOLE_LOG_CAP = 200;

export type TraceSource = 'editor' | 'repl' | 'backtest';

export interface WorkbenchContext {
  draft: ScriptEditorState;
  setDraft: React.Dispatch<React.SetStateAction<ScriptEditorState>>;
  activeTraceSource: TraceSource;
  setActiveTraceSource: (s: TraceSource) => void;
  editorTrace: TraceRecord[];
  setEditorTrace: (t: TraceRecord[]) => void;
  replTrace: TraceRecord[];
  setReplTrace: (t: TraceRecord[]) => void;
  replResult: unknown;
  setReplResult: (r: unknown) => void;
  backtestRun: BacktestRun | null;
  setBacktestRun: (r: BacktestRun | null) => void;
  strategyId: number | null;
  consoleLog: ConsoleEntry[];
  appendConsoleEntry: (e: Omit<ConsoleEntry, 'id' | 'timestamp'>) => void;
  // Lets the active pane (Editor/REPL - each owns its own symbol/timeframe/
  // source and knows how to re-run its own preview) plug into the shared
  // chart's zoom-out-triggers-more-history behavior without WorkbenchShell
  // needing to know anything about fast-rerun/REPL request shapes. The pane
  // registers a "widen my window and re-run" callback on mount/update; the
  // chart calls it via SharedPriceChart's onNeedMoreHistory.
  registerLoadMoreHistory: (handler: (() => void) | null) => void;
  loadingMoreHistory: boolean;
  setLoadingMoreHistory: (loading: boolean) => void;
  hasMoreHistory: boolean;
  setHasMoreHistory: (has: boolean) => void;
  /** This user may not change this strategy (auth-02 §4): editor read-only,
   * no Save buttons. */
  readOnly: boolean;
  zenMode: 'none' | 'editor' | 'chart';
  setZenMode: (m: 'none' | 'editor' | 'chart') => void;
}

interface WorkbenchShellProps {
  mode: 'create' | 'edit';
}

const DEFAULT_DRAFT: ScriptEditorState = {
  strategyId: null,
  name: '',
  description: '',
  symbols: ['BTCUSDT'],
  cycleMinutes: 15,
  source: DEFAULT_LUA_TEMPLATE,
  stopLossPct: 2.5,
  positionSizing: { type: 'pct_capital', value: 10 },
  status: 'disabled',
  mode: 'dryrun',
  previewSymbol: 'BTCUSDT',
};


export function WorkbenchShell({ mode }: WorkbenchShellProps) {
  const t = useT();
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const strategyId = id ? Number(id) : null;
  const isEdit = mode === 'edit';
  const { can } = useAuth();
  const { toast } = useToast();
  const cloneMutation = useCloneStrategyAsDraft();

  const { data: existingStrategy, isLoading: isStrategyLoading } = useStrategy(strategyId || 0);
  // Read-only unless this user may change this strategy: admins always;
  // edit_drafts users only for backtest-mode, non-productive drafts.
  const readOnly = isEdit ? !!existingStrategy && !canEditStrategy(can, existingStrategy) : !can('edit_drafts');
  // Challenger banner (B-02 §4): a challenger is a dryrun clone an agent
  // iterates on; its champion is the (usually live) strategy it may one day
  // be promoted into. 0 disables the query for non-challengers.
  const championId = isEdit ? existingStrategy?.challenger_of_id ?? 0 : 0;
  const { data: champion } = useStrategy(championId);

  // Non-admins can only create backtest drafts (auth-01 §3 draft guard).
  const [draft, setDraft] = useState<ScriptEditorState>(() =>
    can('admin') ? DEFAULT_DRAFT : { ...DEFAULT_DRAFT, mode: 'backtest', status: 'testing' },
  );
  const [activeTraceSource, setActiveTraceSource] = useState<TraceSource>('editor');
  const [editorTrace, setEditorTrace] = useState<TraceRecord[]>([]);
  const [replTrace, setReplTrace] = useState<TraceRecord[]>([]);
  const [replResult, setReplResult] = useState<unknown>(null);
  const [backtestRun, setBacktestRun] = useState<BacktestRun | null>(null);
  const [consoleLog, setConsoleLog] = useState<ConsoleEntry[]>([]);
  const [selectedTraceRecord, setSelectedTraceRecord] = useState<TraceRecord | null>(null);
  const [contentView, setContentView] = usePersistedEnum<ContentView>('workbench.contentView', CONTENT_VIEWS, 'split');
  const [consolePanelOpen, setConsolePanelOpen] = usePersistedOpen('workbench.consolePanelOpen', true);
  const [zenMode, setZenMode] = useState<'none' | 'editor' | 'chart'>('none');
  const [showChartTickDetail, setShowChartTickDetail] = useState(false);

  // Esc key listener to exit full screen / zen mode immediately (VS Code pattern)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && zenMode !== 'none') {
        e.preventDefault();
        e.stopPropagation();
        setZenMode('none');
      }
    };
    window.addEventListener('keydown', handleKeyDown, true);
    return () => window.removeEventListener('keydown', handleKeyDown, true);
  }, [zenMode]);

  // Zoom-out-loads-more-history wiring (see WorkbenchContext.registerLoadMoreHistory
  // doc comment above): a ref, not state, since the handler itself never needs
  // to trigger a re-render - only loadingMoreHistory/hasMoreHistory do.
  const loadMoreHistoryHandlerRef = useRef<(() => void) | null>(null);
  const registerLoadMoreHistory = useCallback((handler: (() => void) | null) => {
    loadMoreHistoryHandlerRef.current = handler;
  }, []);
  const [loadingMoreHistory, setLoadingMoreHistory] = useState(false);
  const [hasMoreHistory, setHasMoreHistory] = useState(true);

  // Switching tabs (Editor/REPL/Backtest) points the chart at a different
  // trace source with its own window - stale "no more history" state from
  // whichever pane was active before would otherwise wrongly suppress
  // loading in the newly-active one.
  useEffect(() => {
    setHasMoreHistory(true);
    setLoadingMoreHistory(false);
  }, [activeTraceSource]);

  // Split-view divider: persisted width in px, dragged via handleDividerDown
  // below. `liveSplitWidth` mirrors it during an active drag (updated on
  // every mousemove) while the persisted value itself is only written once
  // at drag end (usePersistedNumber's `commit`), so dragging doesn't spam
  // localStorage.
  const [splitWidth, , commitSplitWidth] = usePersistedNumber('workbench.splitWidth', SPLIT_DEFAULT_PX);
  const [liveSplitWidth, setLiveSplitWidth] = useState<number | null>(null);
  const splitRowRef = useRef<HTMLDivElement | null>(null);
  // The persisted width is absolute px, saved at whatever window/dock width
  // was current then - opening the agent dock (or a smaller window) would
  // otherwise leave the chart a sliver. Clamp at render time to the row's
  // live width so the chart always keeps SPLIT_CHART_MIN_PX; the persisted
  // preference itself is untouched and comes back when space does.
  // A callback ref, not a mount effect: the row doesn't exist while the
  // strategy is still loading (early LoadingScreen return below).
  const [splitRowWidth, setSplitRowWidth] = useState<number | null>(null);
  const splitRowObserverRef = useRef<ResizeObserver | null>(null);
  const setSplitRowEl = useCallback((el: HTMLDivElement | null) => {
    splitRowRef.current = el;
    splitRowObserverRef.current?.disconnect();
    splitRowObserverRef.current = null;
    if (!el) return;
    const ro = new ResizeObserver((entries) => setSplitRowWidth(entries[0].contentRect.width));
    ro.observe(el);
    splitRowObserverRef.current = ro;
  }, []);
  const maxSplitWidth =
    splitRowWidth != null ? Math.max(SPLIT_MIN_PX, splitRowWidth - SPLIT_CHART_MIN_PX - SPLIT_DIVIDER_PX) : Infinity;
  const effectiveSplitWidth = Math.min(liveSplitWidth ?? splitWidth, maxSplitWidth);
  // Too narrow for both minimums (small window + agent dock): split would
  // clip the editor toolbar and crush the chart, so show the editor alone
  // until there's room again. The saved preference stays 'split'.
  const splitSqueezed =
    contentView === 'split' &&
    splitRowWidth != null &&
    splitRowWidth < SPLIT_MIN_PX + SPLIT_CHART_MIN_PX + SPLIT_DIVIDER_PX;
  const layoutView: ContentView = splitSqueezed ? 'script' : contentView;

  const handleDividerDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const startX = e.clientX;
      const startWidth = effectiveSplitWidth;
      const containerWidth = splitRowRef.current?.clientWidth ?? Infinity;
      const maxWidth = Math.max(SPLIT_MIN_PX, containerWidth - SPLIT_CHART_MIN_PX - SPLIT_DIVIDER_PX);
      const prevCursor = document.body.style.cursor;
      const prevUserSelect = document.body.style.userSelect;
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';

      const onMove = (moveEvent: MouseEvent) => {
        const next = Math.min(Math.max(startWidth + (moveEvent.clientX - startX), SPLIT_MIN_PX), maxWidth);
        setLiveSplitWidth(next);
      };
      const onUp = () => {
        window.removeEventListener('mousemove', onMove);
        window.removeEventListener('mouseup', onUp);
        document.body.style.cursor = prevCursor;
        document.body.style.userSelect = prevUserSelect;
        setLiveSplitWidth((w) => {
          if (w !== null) commitSplitWidth(w);
          return null;
        });
      };
      window.addEventListener('mousemove', onMove);
      window.addEventListener('mouseup', onUp);
    },
    [effectiveSplitWidth, commitSplitWidth]
  );

  // Mount persistence: this body runs exactly once per distinct `:id`
  // (React Router only remounts a parent route element when its own
  // matched path params change - switching Editor/REPL/Backtest tabs only
  // swaps what <Outlet/> renders, not this shell).
  useEffect(() => {
    // console.log('mount'); // AC#1/#2 verification hook, left as a comment
  }, []);

  useEffect(() => {
    if (isEdit && existingStrategy) {
      setDraft(toEditorState(existingStrategy));
    }
  }, [isEdit, existingStrategy]);

  const appendConsoleEntry = useCallback((e: Omit<ConsoleEntry, 'id' | 'timestamp'>) => {
    const entry: ConsoleEntry = {
      ...e,
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      timestamp: formatTime(new Date()),
    };
    setConsoleLog((prev) => [...prev, entry].slice(-CONSOLE_LOG_CAP));
  }, []);

  const ctx: WorkbenchContext = useMemo(
    () => ({
      draft,
      setDraft,
      activeTraceSource,
      setActiveTraceSource,
      editorTrace,
      setEditorTrace,
      replTrace,
      setReplTrace,
      replResult,
      setReplResult,
      backtestRun,
      setBacktestRun,
      strategyId,
      consoleLog,
      appendConsoleEntry,
      registerLoadMoreHistory,
      loadingMoreHistory,
      setLoadingMoreHistory,
      hasMoreHistory,
      setHasMoreHistory,
      readOnly,
      zenMode,
      setZenMode,
    }),
    [
      readOnly,
      draft,
      activeTraceSource,
      editorTrace,
      replTrace,
      replResult,
      backtestRun,
      strategyId,
      consoleLog,
      appendConsoleEntry,
      registerLoadMoreHistory,
      loadingMoreHistory,
      hasMoreHistory,
      zenMode,
    ]
  );

  const activeTrace = useMemo(() => {
    if (activeTraceSource === 'editor') return editorTrace;
    if (activeTraceSource === 'repl') return replTrace;
    return backtestRun?.execution_trace || [];
  }, [activeTraceSource, editorTrace, replTrace, backtestRun]);

  // Bridge for the agent chat dock (AgentDock, mounted outside this nested
  // route tree, in App.tsx) - see context/EditorBridgeContext.ts. Sourced
  // directly from `draft`, which this shell already owns; applyScript just
  // pushes into the same setDraft the Editor pane's own Save button uses,
  // so it flows through the exact same controlled-CodeMirror `value` prop
  // LuaScriptEditor already re-renders on.
  const editorBridge = useMemo(
    () => ({
      currentSource: draft.source,
      applyScript: (source: string) => setDraft((prev) => ({ ...prev, source })),
      strategyId: strategyId ?? undefined,
      readOnly,
    }),
    [draft.source, setDraft, strategyId, readOnly]
  );
  useRegisterEditorBridge(editorBridge);

  if (isEdit && isStrategyLoading) {
    return <LoadingScreen message={t('workbench.loading', { id: strategyId })} />;
  }

  const handleClone = () => {
    if (!existingStrategy) return;
    cloneMutation.mutate(
      {
        name: existingStrategy.name,
        description: existingStrategy.description ?? '',
        strategy_name: existingStrategy.strategy_name || 'script',
        monitored_symbols: existingStrategy.monitored_symbols ?? [],
        cycle: existingStrategy.cycle,
        configuration: existingStrategy.configuration,
        script_source: existingStrategy.script_source,
      },
      {
        onSuccess: (created) => {
          toast(t('workbench.cloned', { id: created.id }));
          navigate(`/strategies/${created.id}/edit`);
        },
        onError: (err) => toast(apiErrorMessage(err, t('workbench.cloneFailed')), 'error'),
      },
    );
  };

  const tabBase = isEdit ? `/strategies/${strategyId}/edit` : '/strategies/new';
  const backtestDisabled = strategyId === null;

  return (
    // TradingView-style: the chart dominates the screen instead of competing
    // with the editor/console for space in normal document flow. Root is
    // bounded to the viewport minus AppLayout's header (h-14) and its main's
    // vertical padding (p-6 = 1.5rem top + bottom) - overflow-hidden here so
    // the two columns below (each min-h-0) scroll internally instead of the
    // whole page growing past 100vh like it used to.
    <div className="h-[calc(100vh-6.5rem)] flex flex-col font-mono min-w-0">
      {/* flex-wrap: at narrow window widths the title+tabs+view-toggle no
          longer fit on one line - without wrap, the ml-auto view-mode
          buttons were simply overflowing past the container's right edge
          (invisible, not just clipped-but-scrollable) instead of dropping
          to their own row. shrink-0 throughout so wrapping is the response
          to tight space, not each item quietly shrinking into an unusable
          sliver first. */}
      <div className="flex flex-wrap items-center gap-3 border-b border-border pb-3 shrink-0">
        <button
          onClick={() => navigate(-1)}
          title={t('common.back')}
          className="p-1.5 text-muted-foreground hover:text-foreground hover:bg-card/30 rounded shrink-0"
        >
          <ArrowLeft className="w-5 h-5" />
        </button>
        <h1 className="text-lg font-bold text-foreground shrink-0 whitespace-nowrap">
          {isEdit ? t('workbench.titleEdit', { id: strategyId }) : t('workbench.titleNew')}
        </h1>

        <nav className="flex items-center gap-1 ml-4 text-xs shrink-0">
          <NavLink
            to={tabBase}
            end
            className={({ isActive }) =>
              `px-3 py-1.5 rounded flex items-center gap-1.5 ${
                isActive ? 'bg-secondary text-white font-semibold' : 'text-muted-foreground hover:text-foreground'
              }`
            }
          >
            <Code2 className="w-3.5 h-3.5" />
            <span>{t('workbench.tabEditor')}</span>
          </NavLink>
          <NavLink
            to={`${tabBase}/repl`}
            className={({ isActive }) =>
              `px-3 py-1.5 rounded flex items-center gap-1.5 ${
                isActive ? 'bg-secondary text-white font-semibold' : 'text-muted-foreground hover:text-foreground'
              }`
            }
          >
            <Terminal className="w-3.5 h-3.5" />
            <span>REPL</span>
          </NavLink>
          {backtestDisabled ? (
            <span
              className="px-3 py-1.5 rounded flex items-center gap-1.5 text-muted-foreground cursor-not-allowed"
              title={t('workbench.backtestNeedsSave')}
            >
              <History className="w-3.5 h-3.5" />
              <span>{t('workbench.tabBacktest')}</span>
            </span>
          ) : (
            <NavLink
              to={`${tabBase}/backtest`}
              className={({ isActive }) =>
                `px-3 py-1.5 rounded flex items-center gap-1.5 ${
                  isActive ? 'bg-secondary text-white font-semibold' : 'text-muted-foreground hover:text-foreground'
                }`
              }
            >
              <History className="w-3.5 h-3.5" />
              <span>{t('workbench.tabBacktest')}</span>
            </NavLink>
          )}
        </nav>

        {/* View mode: Script / Split / Chart - which of the side panel
            (Editor/REPL/Backtest content) and the chart gets the screen.
            Segmented control instead of two independent toggles because
            "full width" only makes sense for exactly one side at a time;
            console stays a simple on/off since it's a bottom drawer, not
            competing for the same horizontal space. */}
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          <div className="flex items-center bg-background border border-border/40 rounded overflow-hidden">
            <button
              onClick={() => setContentView('script')}
              title={t('workbench.viewScript')}
              className={`p-1.5 ${
                contentView === 'script' ? 'bg-secondary text-white' : 'text-muted-foreground hover:text-foreground hover:bg-card/30'
              }`}
            >
              <SquareCode className="w-4 h-4" />
            </button>
            <button
              onClick={() => setContentView('split')}
              title={t('workbench.viewSplit')}
              className={`p-1.5 border-l border-r border-border/40 ${
                contentView === 'split' ? 'bg-secondary text-white' : 'text-muted-foreground hover:text-foreground hover:bg-card/30'
              }`}
            >
              <Columns2 className="w-4 h-4" />
            </button>
            <button
              onClick={() => setContentView('chart')}
              title={t('workbench.viewChart')}
              className={`p-1.5 ${
                contentView === 'chart' ? 'bg-secondary text-white' : 'text-muted-foreground hover:text-foreground hover:bg-card/30'
              }`}
            >
              <ChartCandlestick className="w-4 h-4" />
            </button>
          </div>

          <div className="h-4 w-px bg-border/40 mx-0.5" />

          {/* Full Screen Zen Mode (VS Code Experience) */}
          <div className="flex items-center bg-background border border-border/40 rounded overflow-hidden">
            <button
              type="button"
              onClick={() => setZenMode(zenMode === 'editor' ? 'none' : 'editor')}
              title={t('workbench.fullscreenEditorTitle')}
              data-testid="zen-mode-editor-btn"
              className={`p-1.5 flex items-center gap-1 ${
                zenMode === 'editor'
                  ? 'bg-primary text-primary-foreground font-semibold'
                  : 'text-muted-foreground hover:text-foreground hover:bg-card/30'
              }`}
            >
              <SquareCode className="w-4 h-4" />
              <Maximize2 className="w-2.5 h-2.5 -ml-0.5 text-primary group-hover:text-foreground" />
            </button>
            <button
              type="button"
              onClick={() => setZenMode(zenMode === 'chart' ? 'none' : 'chart')}
              title={t('workbench.fullscreenChartTitle')}
              data-testid="zen-mode-chart-btn"
              className={`p-1.5 border-l border-border/40 flex items-center gap-1 ${
                zenMode === 'chart'
                  ? 'bg-primary text-primary-foreground font-semibold'
                  : 'text-muted-foreground hover:text-foreground hover:bg-card/30'
              }`}
            >
              <ChartCandlestick className="w-4 h-4" />
              <Maximize2 className="w-2.5 h-2.5 -ml-0.5 text-primary group-hover:text-foreground" />
            </button>
          </div>

          <button
            onClick={() => setConsolePanelOpen((o) => !o)}
            title={consolePanelOpen ? t('workbench.hideConsole') : t('workbench.showConsole')}
            className="p-1.5 text-muted-foreground hover:text-foreground hover:bg-card/30 rounded"
          >
            {consolePanelOpen ? <PanelBottomClose className="w-4 h-4" /> : <PanelBottomOpen className="w-4 h-4" />}
          </button>
        </div>
      </div>

      {readOnly && isEdit && (
        <div
          role="note"
          data-testid="workbench-readonly-banner"
          className="mt-3 shrink-0 px-3 py-2 rounded-lg border border-warning/40 bg-warning/10 text-xs text-foreground flex flex-wrap items-center gap-x-2 gap-y-1 font-sans"
        >
          <Lock className="w-4 h-4 text-warning shrink-0" />
          <span>{can('edit_drafts') ? t('workbench.readOnlyDraftable') : t('workbench.readOnlyNoEdit')}</span>
          {can('edit_drafts') && (
            <button
              type="button"
              onClick={handleClone}
              disabled={cloneMutation.isPending || !existingStrategy}
              data-testid="clone-as-draft-btn"
              className="ml-auto bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1 font-semibold disabled:opacity-50 whitespace-nowrap"
            >
              {cloneMutation.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Copy className="w-3.5 h-3.5" />}
              {cloneMutation.isPending ? t('workbench.cloning') : t('workbench.clone')}
            </button>
          )}
        </div>
      )}

      {championId > 0 && (
        <div
          role="note"
          className="mt-3 shrink-0 px-3 py-2 rounded-lg border border-primary/40 bg-primary/10 text-xs text-foreground flex flex-wrap items-center gap-x-2 gap-y-1 font-sans"
        >
          <GitBranch className="w-4 h-4 text-primary shrink-0" />
          <span>
            {t.rich('workbench.challengerBanner', {
              champion: (
                <Link to={`/strategies/${championId}/edit`} className="font-semibold hover:text-primary hover:underline">
                  {champion?.name ?? t('workbench.strategyNumber', { id: championId })}
                </Link>
              ),
              mode: existingStrategy?.mode ?? 'dryrun',
            })}
          </span>
          <Link
            to={`/agents/proposals?strategy_id=${championId}`}
            className="ml-auto text-primary hover:underline whitespace-nowrap"
          >
            {t('workbench.viewProposals')}
          </Link>
        </div>
      )}

      <div className="flex-1 min-h-0 flex flex-col gap-2 mt-4">
        {splitSqueezed && (
          <p role="note" className="shrink-0 text-[11px] text-muted-foreground font-sans">
            {t('workbench.splitSqueezed')}
          </p>
        )}
        <div ref={setSplitRowEl} className="flex-1 min-h-0 flex">
          {/* Side panel: editor/REPL/backtest content. Always mounted (CSS
              `hidden` in chart-only mode, not removed from the tree) so
              switching view modes never resets the Lua editor's undo
              history/cursor/scroll position or a REPL/Backtest pane's local
              state - only the CSS width/visibility changes. Width in split
              mode is user-dragged (the divider below), clamped so neither
              side can be dragged unusably thin. */}
          <div
            className={
              zenMode === 'editor'
                ? 'fixed inset-0 z-[70] bg-background flex flex-col p-4 overflow-hidden'
                : zenMode === 'chart'
                ? 'hidden'
                : layoutView === 'chart'
                ? 'hidden'
                : layoutView === 'script'
                ? 'flex-1 min-w-0 overflow-y-auto pr-1'
                : 'shrink-0 overflow-y-auto pr-1'
            }
            style={zenMode === 'none' && layoutView === 'split' ? { width: effectiveSplitWidth } : undefined}
          >
            <div className="workbench-outlet h-full">
              <Outlet context={ctx} />
            </div>
          </div>

          {/* Drag handle - only meaningful (and rendered) in split mode */}
          {zenMode === 'none' && layoutView === 'split' && (
            <div
              onMouseDown={handleDividerDown}
              title={t('workbench.dragResize')}
              className="w-1.5 shrink-0 mx-1 cursor-col-resize rounded bg-secondary/20 hover:bg-primary/50 active:bg-primary/60 transition-colors"
            />
          )}

          {/* Center: the chart */}
          <div
            className={
              zenMode === 'chart'
                ? 'fixed inset-0 z-[70] bg-background flex flex-col p-4 overflow-hidden'
                : zenMode === 'editor'
                ? 'hidden'
                : layoutView === 'script'
                ? 'hidden'
                : 'flex-1 min-w-0 flex flex-col gap-2'
            }
          >
            {zenMode === 'chart' ? (
              /* Full Screen Chart Header (TradingView / VS Code experience) */
              <div className="flex flex-wrap items-center justify-between gap-3 pb-2.5 mb-2 border-b border-border/60 shrink-0 font-sans">
                <div className="flex items-center gap-2.5">
                  <div className="p-1.5 rounded-md bg-primary/10 border border-primary/20 text-primary">
                    <ChartCandlestick className="w-4 h-4" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="font-bold text-sm text-foreground">
                        {isEdit ? draft.name || t('workbench.strategyNumber', { id: strategyId }) : t('workbench.titleNew')}
                      </span>
                      <span className="text-[11px] px-2 py-0.5 rounded font-mono font-bold bg-secondary/80 text-foreground border border-border/60">
                        {draft.previewSymbol}
                      </span>
                      <span className="text-[11px] px-1.5 py-0.5 rounded font-mono bg-accent/40 text-muted-foreground border border-border/40">
                        {cycleTimeframe(draft.cycleMinutes)}
                      </span>
                      <span className="text-[10px] px-2 py-0.5 rounded uppercase font-bold tracking-wider bg-primary/20 text-primary border border-primary/30">
                        {activeTraceSource}
                      </span>
                    </div>
                  </div>

                  {draft.symbols.length > 1 && (
                    <div className="ml-2 flex items-center gap-1.5">
                      <span className="text-[11px] text-muted-foreground">{t('workbench.symbolsSwitch')}:</span>
                      <select
                        value={draft.previewSymbol}
                        onChange={(e) => setDraft((prev) => ({ ...prev, previewSymbol: e.target.value }))}
                        className="bg-secondary text-foreground text-xs px-2 py-1 rounded border border-border focus:outline-none focus:border-primary font-mono"
                      >
                        {draft.symbols.map((s) => (
                          <option key={s} value={s}>{s}</option>
                        ))}
                      </select>
                    </div>
                  )}

                  {loadingMoreHistory && (
                    <span className="text-xs text-foreground flex items-center gap-1.5 ml-2">
                      <RefreshCw className="w-3.5 h-3.5 animate-spin text-primary" />
                      <span>{t('workbench.loadingHistory')}</span>
                    </span>
                  )}
                </div>

                {/* Real-time OHLCV Scrub bar when hovering candles */}
                {selectedTraceRecord?.candle && (
                  <div className="hidden lg:flex items-center gap-3 text-xs font-mono bg-card/60 px-3 py-1 rounded-md border border-border/40">
                    <span className="text-muted-foreground">{formatTime(new Date(selectedTraceRecord.timestamp))}</span>
                    <span><span className="text-muted-foreground">O:</span> {selectedTraceRecord.candle.o}</span>
                    <span><span className="text-muted-foreground">H:</span> {selectedTraceRecord.candle.h}</span>
                    <span><span className="text-muted-foreground">L:</span> {selectedTraceRecord.candle.l}</span>
                    <span>
                      <span className="text-muted-foreground">C:</span>{' '}
                      <span className={selectedTraceRecord.candle.c >= selectedTraceRecord.candle.o ? 'text-success font-semibold' : 'text-destructive font-semibold'}>
                        {selectedTraceRecord.candle.c}
                      </span>
                    </span>
                    <span><span className="text-muted-foreground">V:</span> {selectedTraceRecord.candle.v}</span>
                  </div>
                )}

                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setShowChartTickDetail((o) => !o)}
                    className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors border ${
                      showChartTickDetail
                        ? 'bg-secondary text-foreground border-primary/40 font-semibold'
                        : 'bg-card/40 text-muted-foreground hover:text-foreground border-border/50'
                    }`}
                  >
                    <Activity className="w-3.5 h-3.5" />
                    <span>{showChartTickDetail ? t('workbench.hideTickInspector') : t('workbench.showTickInspector')}</span>
                  </button>

                  <button
                    type="button"
                    onClick={() => setZenMode('none')}
                    title={t('workbench.exitZenMode')}
                    data-testid="exit-chart-zen-btn"
                    className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded-md bg-secondary hover:bg-secondary/80 text-foreground border border-border/60 transition-colors shadow-sm"
                  >
                    <Minimize2 className="w-3.5 h-3.5" />
                    <span>{t('workbench.exitZenMode')}</span>
                    <kbd className="px-1.5 py-0.5 text-[10px] bg-background border border-border/60 rounded font-mono text-muted-foreground ml-1">
                      Esc
                    </kbd>
                  </button>
                </div>
              </div>
            ) : (
              /* Regular Chart Header */
              <div className="px-1 text-[11px] text-muted-foreground uppercase font-semibold tracking-wide shrink-0 flex items-center gap-2">
                <span>{t('workbench.sharedChart', { source: activeTraceSource })}</span>
                {loadingMoreHistory && (
                  <span className="normal-case text-foreground font-normal tracking-normal flex items-center gap-1">
                    <RefreshCw className="w-3 h-3 animate-spin" />
                    {t('workbench.loadingHistory')}
                  </span>
                )}
                {!loadingMoreHistory && !hasMoreHistory && activeTraceSource !== 'backtest' && (
                  <span className="normal-case text-muted-foreground font-normal tracking-normal">
                    {t('workbench.fullHistory')}
                  </span>
                )}
                <button
                  type="button"
                  onClick={() => setZenMode('chart')}
                  title={t('workbench.fullscreenChartTitle')}
                  className="ml-auto text-[11px] normal-case text-muted-foreground hover:text-foreground flex items-center gap-1 px-1.5 py-0.5 rounded hover:bg-secondary/40 font-normal"
                >
                  <Maximize2 className="w-3 h-3" />
                  <span>{t('workbench.zenModeChartShort')}</span>
                </button>
              </div>
            )}

            <div className="flex-1 min-h-0">
              <SharedPriceChart
                trace={activeTrace}
                fill
                onScrub={setSelectedTraceRecord}
                symbol={draft.previewSymbol}
                onNeedMoreHistory={
                  activeTraceSource !== 'backtest' ? () => loadMoreHistoryHandlerRef.current?.() : undefined
                }
                loadingMoreHistory={loadingMoreHistory}
                hasMoreHistory={hasMoreHistory}
              />
            </div>

            {/* Tick detail panel */}
            {zenMode === 'chart' ? (
              showChartTickDetail && (
                <div className="shrink-0 max-h-48 overflow-y-auto border-t border-border/60 pt-2 animate-in slide-in-from-bottom-2 duration-150">
                  <TraceAnnotationPanel record={selectedTraceRecord} />
                </div>
              )
            ) : (
              <div className="shrink-0 max-h-40 overflow-y-auto">
                <CollapsibleSection id="workbench.tickDetail" title={t('workbench.tickDetail')} defaultOpen>
                  <TraceAnnotationPanel record={selectedTraceRecord} />
                </CollapsibleSection>
              </div>
            )}
          </div>
        </div>

        {/* Bottom panel: console, spans the full width under both the side
            panel and the chart */}
        {zenMode === 'none' && consolePanelOpen && (
          <div className="shrink-0 max-h-48 overflow-y-auto">
            <CollapsibleSection id="workbench.console" title={t('workbench.console')} defaultOpen>
              <ConsolePanel entries={consoleLog} />
            </CollapsibleSection>
          </div>
        )}

        {/* Shared per-strategy agent memory */}
        {zenMode === 'none' && strategyId != null && (
          <div className="shrink-0 max-h-64 overflow-y-auto">
            <AgentNotesPanel strategyId={strategyId} />
          </div>
        )}
      </div>
    </div>
  );
}
