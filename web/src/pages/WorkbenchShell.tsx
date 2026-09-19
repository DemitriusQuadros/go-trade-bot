import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Outlet, useNavigate, useParams } from 'react-router-dom';
import { NavLink } from 'react-router-dom';
import { useStrategy } from '@/hooks/queries';
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
import { LoadingScreen } from '@/components/ui/Spinner';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { usePersistedOpen, usePersistedEnum, usePersistedNumber } from '@/hooks/usePersistedOpen';
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
} from 'lucide-react';

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
const SPLIT_CHART_MIN_PX = 280;
const SPLIT_DEFAULT_PX = 460;

export const CYCLE_OPTIONS = [1, 5, 10, 15, 30, 60] as const;
export type CycleMinutes = (typeof CYCLE_OPTIONS)[number];

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
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const strategyId = id ? Number(id) : null;
  const isEdit = mode === 'edit';

  const { data: existingStrategy, isLoading: isStrategyLoading } = useStrategy(strategyId || 0);

  const [draft, setDraft] = useState<ScriptEditorState>(DEFAULT_DRAFT);
  const [activeTraceSource, setActiveTraceSource] = useState<TraceSource>('editor');
  const [editorTrace, setEditorTrace] = useState<TraceRecord[]>([]);
  const [replTrace, setReplTrace] = useState<TraceRecord[]>([]);
  const [replResult, setReplResult] = useState<unknown>(null);
  const [backtestRun, setBacktestRun] = useState<BacktestRun | null>(null);
  const [consoleLog, setConsoleLog] = useState<ConsoleEntry[]>([]);
  const [selectedTraceRecord, setSelectedTraceRecord] = useState<TraceRecord | null>(null);
  const [contentView, setContentView] = usePersistedEnum<ContentView>('workbench.contentView', CONTENT_VIEWS, 'split');
  const [consolePanelOpen, setConsolePanelOpen] = usePersistedOpen('workbench.consolePanelOpen', true);

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
  const splitRowRef = useRef<HTMLDivElement>(null);
  const effectiveSplitWidth = liveSplitWidth ?? splitWidth;

  const handleDividerDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const startX = e.clientX;
      const startWidth = effectiveSplitWidth;
      const containerWidth = splitRowRef.current?.clientWidth ?? Infinity;
      const maxWidth = Math.max(SPLIT_MIN_PX, containerWidth - SPLIT_CHART_MIN_PX);
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
      timestamp: new Date().toLocaleTimeString(),
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
    }),
    [
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
    ]
  );

  const activeTrace = useMemo(() => {
    if (activeTraceSource === 'editor') return editorTrace;
    if (activeTraceSource === 'repl') return replTrace;
    return backtestRun?.execution_trace || [];
  }, [activeTraceSource, editorTrace, replTrace, backtestRun]);

  if (isEdit && isStrategyLoading) {
    return <LoadingScreen message={`Loading strategy #${strategyId}...`} />;
  }

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
      <div className="flex flex-wrap items-center gap-3 border-b border-green-950 pb-3 shrink-0">
        <button
          onClick={() => navigate('/strategies')}
          className="p-1.5 text-green-700 hover:text-green-400 hover:bg-green-950/30 rounded shrink-0"
        >
          <ArrowLeft className="w-5 h-5" />
        </button>
        <h1 className="text-lg font-bold text-green-500 shrink-0 whitespace-nowrap">
          {isEdit ? `Strategy Workbench #${strategyId}` : 'New Script Strategy'}
        </h1>

        <nav className="flex items-center gap-1 ml-4 text-xs shrink-0">
          <NavLink
            to={tabBase}
            end
            className={({ isActive }) =>
              `px-3 py-1.5 rounded flex items-center gap-1.5 ${
                isActive ? 'bg-green-800 text-white font-semibold' : 'text-green-700 hover:text-green-400'
              }`
            }
          >
            <Code2 className="w-3.5 h-3.5" />
            <span>Editor</span>
          </NavLink>
          <NavLink
            to={`${tabBase}/repl`}
            className={({ isActive }) =>
              `px-3 py-1.5 rounded flex items-center gap-1.5 ${
                isActive ? 'bg-green-800 text-white font-semibold' : 'text-green-700 hover:text-green-400'
              }`
            }
          >
            <Terminal className="w-3.5 h-3.5" />
            <span>REPL</span>
          </NavLink>
          {backtestDisabled ? (
            <span
              className="px-3 py-1.5 rounded flex items-center gap-1.5 text-green-950 cursor-not-allowed"
              title="Save this strategy before running a full backtest"
            >
              <History className="w-3.5 h-3.5" />
              <span>Backtest</span>
            </span>
          ) : (
            <NavLink
              to={`${tabBase}/backtest`}
              className={({ isActive }) =>
                `px-3 py-1.5 rounded flex items-center gap-1.5 ${
                  isActive ? 'bg-green-800 text-white font-semibold' : 'text-green-700 hover:text-green-400'
                }`
              }
            >
              <History className="w-3.5 h-3.5" />
              <span>Backtest</span>
            </NavLink>
          )}
        </nav>

        {/* View mode: Script / Split / Chart - which of the side panel
            (Editor/REPL/Backtest content) and the chart gets the screen.
            Segmented control instead of two independent toggles because
            "full width" only makes sense for exactly one side at a time;
            console stays a simple on/off since it's a bottom drawer, not
            competing for the same horizontal space. */}
        <div className="ml-auto flex items-center gap-1 shrink-0">
          <div className="flex items-center bg-black border border-green-900/40 rounded overflow-hidden">
            <button
              onClick={() => setContentView('script')}
              title="Full-screen script (Editor/REPL/Backtest content fills the width)"
              className={`p-1.5 ${
                contentView === 'script' ? 'bg-green-800 text-white' : 'text-green-700 hover:text-green-400 hover:bg-green-950/30'
              }`}
            >
              <SquareCode className="w-4 h-4" />
            </button>
            <button
              onClick={() => setContentView('split')}
              title="Split view"
              className={`p-1.5 border-l border-r border-green-900/40 ${
                contentView === 'split' ? 'bg-green-800 text-white' : 'text-green-700 hover:text-green-400 hover:bg-green-950/30'
              }`}
            >
              <Columns2 className="w-4 h-4" />
            </button>
            <button
              onClick={() => setContentView('chart')}
              title="Full-screen chart"
              className={`p-1.5 ${
                contentView === 'chart' ? 'bg-green-800 text-white' : 'text-green-700 hover:text-green-400 hover:bg-green-950/30'
              }`}
            >
              <ChartCandlestick className="w-4 h-4" />
            </button>
          </div>
          <button
            onClick={() => setConsolePanelOpen((o) => !o)}
            title={consolePanelOpen ? 'Hide console' : 'Show console'}
            className="p-1.5 text-green-700 hover:text-green-400 hover:bg-green-950/30 rounded"
          >
            {consolePanelOpen ? <PanelBottomClose className="w-4 h-4" /> : <PanelBottomOpen className="w-4 h-4" />}
          </button>
        </div>
      </div>

      <div className="flex-1 min-h-0 flex flex-col gap-2 mt-4">
        <div ref={splitRowRef} className="flex-1 min-h-0 flex">
          {/* Side panel: editor/REPL/backtest content. Always mounted (CSS
              `hidden` in chart-only mode, not removed from the tree) so
              switching view modes never resets the Lua editor's undo
              history/cursor/scroll position or a REPL/Backtest pane's local
              state - only the CSS width/visibility changes. Width in split
              mode is user-dragged (the divider below), clamped so neither
              side can be dragged unusably thin. */}
          <div
            className={
              contentView === 'chart'
                ? 'hidden'
                : contentView === 'script'
                ? 'flex-1 min-w-0 overflow-y-auto pr-1'
                : 'shrink-0 overflow-y-auto pr-1'
            }
            style={contentView === 'split' ? { width: effectiveSplitWidth } : undefined}
          >
            <div className="workbench-outlet h-full">
              <Outlet context={ctx} />
            </div>
          </div>

          {/* Drag handle - only meaningful (and rendered) in split mode,
              where there are two panes to divide. A mousedown here tracks
              window mousemove/mouseup to resize the side panel above;
              SharedPriceChart's own ResizeObserver (fill mode) picks up the
              chart's new width live as the divider moves, no extra wiring
              needed on that side. */}
          {contentView === 'split' && (
            <div
              onMouseDown={handleDividerDown}
              title="Drag to resize"
              className="w-1.5 shrink-0 mx-1 cursor-col-resize rounded bg-green-900/20 hover:bg-green-700/50 active:bg-green-600/60 transition-colors"
            />
          )}

          {/* Center: the chart - always mounted too (same reasoning: CSS
              `hidden` in script-only mode preserves zoom/pan instead of
              resetting it on every view-mode toggle). Fills whatever space
              the side/bottom panels don't claim via SharedPriceChart's fill
              mode (ResizeObserver-driven). */}
          <div className={contentView === 'script' ? 'hidden' : 'flex-1 min-w-0 flex flex-col gap-2'}>
            <div className="px-1 text-[11px] text-green-700 uppercase font-semibold tracking-wide shrink-0 flex items-center gap-2">
              <span>Shared Price Chart ({activeTraceSource})</span>
              {loadingMoreHistory && (
                <span className="normal-case text-green-500 font-normal tracking-normal flex items-center gap-1">
                  <RefreshCw className="w-3 h-3 animate-spin" />
                  Loading more history...
                </span>
              )}
              {!loadingMoreHistory && !hasMoreHistory && activeTraceSource !== 'backtest' && (
                <span className="normal-case text-green-800 font-normal tracking-normal">
                  (full available history loaded)
                </span>
              )}
            </div>
            <div className="flex-1 min-h-0">
              <SharedPriceChart
                trace={activeTrace}
                fill
                onScrub={setSelectedTraceRecord}
                symbol={draft.previewSymbol}
                // A finished backtest already holds its full requested date
                // range (no bounded preview window to widen), so only wire
                // the zoom-out-loads-more-history behavior for Editor/REPL.
                onNeedMoreHistory={
                  activeTraceSource !== 'backtest' ? () => loadMoreHistoryHandlerRef.current?.() : undefined
                }
                loadingMoreHistory={loadingMoreHistory}
                hasMoreHistory={hasMoreHistory}
              />
            </div>
            <div className="shrink-0 max-h-40 overflow-y-auto">
              <CollapsibleSection id="workbench.tickDetail" title="Tick Detail" defaultOpen>
                <TraceAnnotationPanel record={selectedTraceRecord} />
              </CollapsibleSection>
            </div>
          </div>
        </div>

        {/* Bottom panel: console, spans the full width under both the side
            panel and the chart - collapses to zero height via
            consolePanelOpen (the top-bar toggle). CollapsibleSection here is
            a SEPARATE, finer-grained per-component hide, independent of
            that top-bar one. */}
        {consolePanelOpen && (
          <div className="shrink-0 max-h-48 overflow-y-auto">
            <CollapsibleSection id="workbench.console" title="Console" defaultOpen>
              <ConsolePanel entries={consoleLog} />
            </CollapsibleSection>
          </div>
        )}
      </div>
    </div>
  );
}
