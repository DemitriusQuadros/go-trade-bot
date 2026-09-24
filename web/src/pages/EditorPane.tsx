import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useOutletContext } from 'react-router-dom';
import { EditorView } from '@codemirror/view';
import { buildLuaDiagnostic } from '@/lib/luaLinting';
import { api } from '@/api/client';
import { useCreateStrategy, useUpdateStrategy } from '@/hooks/queries';
import { StrategyStatus, StrategyCreateRequest, StrategyUpdateRequest } from '@/api/types';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { StrategyMetadataForm } from '@/components/domain/StrategyMetadataForm';
import { LuaScriptEditor } from '@/components/domain/LuaScriptEditor';
import {
  DEFAULT_LUA_TEMPLATE,
  toStrategyConfiguration,
  WorkbenchContext,
} from './WorkbenchShell';
import { Save, Rocket, CheckCircle2, AlertCircle, Code2, RotateCcw, RefreshCw } from 'lucide-react';

const LIVE_REFRESH_MS = 5000;

// Fast-rerun's default preview window - matches the engine's own per-cycle
// candleWindow (app/engine/engine.go) so a preview sees the same history
// depth a real live/backtest cycle would. Doubled (capped at
// MAX_WINDOW_CANDLES) each time the user zooms/pans toward the left edge of
// the chart - see SharedPriceChart's onNeedMoreHistory.
const DEFAULT_WINDOW_CANDLES = 100;
// Upper bound on how far a single preview will widen itself: past this, the
// per-cycle Lua sandbox cost (one hook invocation per candle) starts making
// the 600ms-debounced auto-run on every keystroke feel sluggish.
const MAX_WINDOW_CANDLES = 2000;

export function EditorPane() {
  const navigate = useNavigate();
  const ctx = useOutletContext<WorkbenchContext>();
  const { draft, setDraft, setEditorTrace, setActiveTraceSource, appendConsoleEntry } = ctx;

  const isEdit = draft.strategyId !== null;
  const createMutation = useCreateStrategy();
  const updateMutation = useUpdateStrategy(draft.strategyId || 0);

  const [symbolInput, setSymbolInput] = useState('');
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [errorLine, setErrorLine] = useState<number | undefined>(undefined);
  const [actionMessage, setActionMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [liveRefresh, setLiveRefresh] = useState(true);
  const [windowCandles, setWindowCandles] = useState(DEFAULT_WINDOW_CANDLES);

  const debounceRef = useRef<ReturnType<typeof setTimeout>>();
  const abortRef = useRef<AbortController | null>(null);
  const editorViewRef = useRef<EditorView | null>(null);
  const runPreviewNowRef = useRef<() => void>(() => {});
  // Mirrors windowCandles for synchronous reads inside runPreviewNow/
  // handleLoadMoreHistory closures (React state updates aren't visible
  // until the next render, but handleLoadMoreHistory needs the widened
  // value immediately to build the fetch it fires in the same tick).
  const windowCandlesRef = useRef(windowCandles);
  windowCandlesRef.current = windowCandles;

  useEffect(() => {
    setActiveTraceSource('editor');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const runPreviewNow = () => {
    abortRef.current?.abort(); // cancel any still-in-flight request from a prior keystroke
    const controller = new AbortController();
    abortRef.current = controller;
    const targetSymbol = draft.previewSymbol || draft.symbols[0] || 'BTCUSDT';
    const requestedWindow = windowCandlesRef.current;
    api
      .fastRerun(
        {
          strategy_id: draft.strategyId || undefined,
          source: draft.source,
          symbol: targetSymbol,
          timeframe: `${draft.cycleMinutes}m`,
          end_time: null,
          window_candles: requestedWindow,
        },
        { signal: controller.signal }
      )
      .then((res) => {
        ctx.setLoadingMoreHistory(false);
        if (res.error) {
          setPreviewError(res.error);
          setErrorLine(res.error_line);
          setEditorTrace([]);
          appendConsoleEntry({ source: 'editor', kind: 'error', message: res.error });
        } else {
          setPreviewError(null);
          setErrorLine(undefined);
          setEditorTrace(res.trace || []);
          // Fewer candles came back than requested - the exchange/DB simply
          // doesn't have any more history for this symbol/timeframe, so
          // stop the chart's zoom-out edge-detection from firing again.
          ctx.setHasMoreHistory((res.trace || []).length >= requestedWindow && requestedWindow < MAX_WINDOW_CANDLES);
          // Volume-shaping (frontend-06): only append debug_log entries from
          // the MOST RECENT cycle in this batch, not every cycle - a 100-
          // candle fast-rerun would otherwise flood the console panel with
          // 600ms-interval bursts while the operator is mid-edit.
          const lastRecord = (res.trace || [])[(res.trace || []).length - 1];
          lastRecord?.log?.forEach((entry) =>
            appendConsoleEntry({
              source: 'editor',
              kind: 'debug_log',
              label: entry.label,
              message: String(entry.value),
            })
          );
        }
      })
      .catch((err) => {
        ctx.setLoadingMoreHistory(false);
        if (err?.name === 'AbortError') return; // superseded by a newer keystroke - not a real error
        setPreviewError(err.message || 'Preview request failed');
        appendConsoleEntry({ source: 'editor', kind: 'error', message: err.message || 'Preview request failed' });
      });
  };

  // Zoom-out-loads-more-history (SharedPriceChart's onNeedMoreHistory, wired
  // through WorkbenchShell's registerLoadMoreHistory below): doubles the
  // preview window and immediately re-runs, rather than waiting for the
  // 600ms debounce - the user is actively looking at the chart edge, not
  // mid-keystroke.
  const handleLoadMoreHistory = () => {
    if (windowCandlesRef.current >= MAX_WINDOW_CANDLES) {
      ctx.setHasMoreHistory(false);
      return;
    }
    const nextWindow = Math.min(windowCandlesRef.current * 2, MAX_WINDOW_CANDLES);
    windowCandlesRef.current = nextWindow;
    setWindowCandles(nextWindow);
    ctx.setLoadingMoreHistory(true);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    runPreviewNowRef.current();
  };
  const handleLoadMoreHistoryRef = useRef(handleLoadMoreHistory);
  handleLoadMoreHistoryRef.current = handleLoadMoreHistory;

  // Only the actually-visible pane's handler should be wired into the
  // shared chart - Editor/REPL/Backtest are all mounted simultaneously
  // (WorkbenchShell just CSS-hides the inactive ones), so this re-registers
  // on every activeTraceSource change rather than once on mount.
  useEffect(() => {
    if (ctx.activeTraceSource !== 'editor') return;
    ctx.registerLoadMoreHistory(() => handleLoadMoreHistoryRef.current());
    return () => ctx.registerLoadMoreHistory(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ctx.activeTraceSource]);

  // A different symbol/timeframe/strategy has an unrelated history depth -
  // start its preview back at the default window rather than carrying over
  // a size the user only widened for the PREVIOUS symbol's chart.
  useEffect(() => {
    windowCandlesRef.current = DEFAULT_WINDOW_CANDLES;
    setWindowCandles(DEFAULT_WINDOW_CANDLES);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft.previewSymbol, draft.cycleMinutes, draft.strategyId]);

  // 600ms-debounced auto-run, cancelled (not queued) on each new keystroke.
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(runPreviewNow, 600);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft.source, draft.previewSymbol, draft.cycleMinutes, draft.strategyId]);

  runPreviewNowRef.current = runPreviewNow;

  // Live refresh: re-runs the SAME fast-rerun preview on a fixed interval,
  // independent of edits - this is the "TradingView-style ticking candle"
  // request in practice, without building a real tick-level SSE feed. Each
  // tick just re-fetches the latest 100-candle window and re-evaluates the
  // script against it, so the in-progress candle's close (and eventually a
  // brand new candle once the timeframe rolls over) updates on its own
  // while the tab is open. Coarser than true sub-second ticking, but reuses
  // 100% of the existing preview pipeline - no backend changes needed.
  // The ref indirection keeps this interval alive across re-renders without
  // fighting the debounce effect above (which intentionally DOES reset on
  // every keystroke) - only `liveRefresh` toggling recreates the interval.
  useEffect(() => {
    if (!liveRefresh) return;
    const interval = setInterval(() => runPreviewNowRef.current(), LIVE_REFRESH_MS);
    return () => clearInterval(interval);
  }, [liveRefresh]);

  const handleReRunNow = () => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    runPreviewNow();
  };

  const diagnostics = useMemo(
    () => (editorViewRef.current ? buildLuaDiagnostic(editorViewRef.current, previewError ?? undefined, errorLine) : []),
    [previewError, errorLine]
  );

  const handleAddSymbol = () => {
    const s = symbolInput.trim().toUpperCase();
    if (!s) return;
    if (!draft.symbols.includes(s)) {
      setDraft((prev) => ({ ...prev, symbols: [...prev.symbols, s] }));
    }
    setSymbolInput('');
  };

  const handleRemoveSymbol = (sym: string) => {
    setDraft((prev) => ({
      ...prev,
      symbols: prev.symbols.filter((s) => s !== sym),
      previewSymbol: prev.previewSymbol === sym ? prev.symbols.find((s) => s !== sym) || 'BTCUSDT' : prev.previewSymbol,
    }));
  };

  const saveStrategy = async (statusOverride?: StrategyStatus): Promise<number | null> => {
    setActionMessage(null);
    if (!draft.name.trim()) {
      setActionMessage({ type: 'error', text: 'Strategy name is required' });
      return null;
    }
    if (draft.symbols.length === 0) {
      setActionMessage({ type: 'error', text: 'At least one monitored symbol is required' });
      return null;
    }

    const finalStatus = statusOverride !== undefined ? statusOverride : draft.status;
    const config = toStrategyConfiguration(draft);

    try {
      if (isEdit && draft.strategyId) {
        const updateReq: StrategyUpdateRequest = {
          name: draft.name.trim(),
          description: draft.description.trim(),
          strategy_name: 'script',
          status: finalStatus,
          mode: draft.mode,
          monitored_symbols: draft.symbols,
          cycle: draft.cycleMinutes,
          configuration: config,
          script_source: draft.source,
        };
        await updateMutation.mutateAsync(updateReq);
        setActionMessage({ type: 'success', text: `Strategy #${draft.strategyId} saved successfully!` });
        return draft.strategyId;
      } else {
        const createReq: StrategyCreateRequest = {
          name: draft.name.trim(),
          description: draft.description.trim(),
          strategy_name: 'script',
          status: finalStatus,
          mode: 'dryrun',
          monitored_symbols: draft.symbols,
          cycle: draft.cycleMinutes,
          configuration: config,
          script_source: draft.source,
        };
        const created = await createMutation.mutateAsync(createReq);
        setActionMessage({ type: 'success', text: `Strategy created successfully with ID #${created.id}!` });
        // create -> edit transition: re-derive strategyId from the new URL.
        navigate(`/strategies/${created.id}/edit`);
        return created.id;
      }
    } catch (err: any) {
      setActionMessage({ type: 'error', text: err.message || 'Failed to save strategy' });
      return null;
    }
  };

  const handleRunFullBacktest = async () => {
    const savedId = await saveStrategy('disabled');
    if (savedId) {
      const sym = draft.previewSymbol || draft.symbols[0] || 'BTCUSDT';
      navigate(`/backtest?strategy_id=${savedId}&symbol=${encodeURIComponent(sym)}`);
    }
  };

  // Cmd/Ctrl+S saves without changing status: for an existing strategy that's
  // exactly the "Save Changes" button; for a brand new one (not yet
  // persisted) it mirrors "Save as Draft" - the shortcut should never be
  // what silently flips a strategy into "productive"/live-tradeable, only
  // the explicit "Save & Enable" button does that.
  const saveStrategyRef = useRef(saveStrategy);
  saveStrategyRef.current = saveStrategy;
  const isEditRef = useRef(isEdit);
  isEditRef.current = isEdit;
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
        e.preventDefault(); // stop the browser's own "Save Page As..." dialog
        saveStrategyRef.current(isEditRef.current ? undefined : 'disabled');
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  return (
    // h-full flex-col: lets the Lua editor section below (CollapsibleSection
    // fill mode) stretch to use whatever vertical space WorkbenchShell's
    // current view mode (Script/Split/Chart) gives this pane, instead of
    // stopping at a fixed pixel height regardless of available room.
    <div className="h-full flex flex-col gap-4">
      {/* Was md:flex-row - a viewport-width breakpoint that fires regardless
          of how narrow this container actually is once squeezed into
          WorkbenchShell's side panel, wrapping this whole row into a
          jumbled mess. Always-stacked, and the hint text is trimmed since
          this panel is narrow by design now (full detail: it auto-runs
          600ms after typing stops). */}
      <div className="flex flex-col gap-2 shrink-0">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-1.5">
            <Code2 className="w-3.5 h-3.5 text-green-500 shrink-0" />
            <span className="text-[11px] text-green-700" title="Auto-runs 600ms after you stop typing">
              Auto-runs on edit
            </span>
          </div>
          <label className="flex items-center gap-1.5 cursor-pointer select-none text-[11px] text-green-700 hover:text-green-400">
            <input
              type="checkbox"
              checked={liveRefresh}
              onChange={(e) => setLiveRefresh(e.target.checked)}
            />
            <span>Live refresh ({LIVE_REFRESH_MS / 1000}s)</span>
          </label>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <button
            onClick={handleRunFullBacktest}
            disabled={createMutation.isPending || updateMutation.isPending}
            className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs flex items-center gap-1.5 px-3 py-1.5"
            title="Save as draft and launch full historical backtest"
          >
            <Rocket className="w-3.5 h-3.5 text-purple-400" />
            <span>Run Full Backtest</span>
          </button>
          {isEdit ? (
            <button
              onClick={() => saveStrategy()}
              disabled={updateMutation.isPending}
              title="Ctrl+S / ⌘S"
              className="bg-green-700 hover:bg-green-600 text-white rounded border border-green-600 text-xs flex items-center gap-1.5 px-4 py-1.5 font-bold"
            >
              <Save className="w-3.5 h-3.5" />
              <span>{updateMutation.isPending ? 'Saving...' : 'Save Changes'}</span>
            </button>
          ) : (
            <>
              <button
                onClick={() => saveStrategy('disabled')}
                disabled={createMutation.isPending}
                title="Ctrl+S / ⌘S"
                className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs flex items-center gap-1.5 px-3 py-1.5"
              >
                <Save className="w-3.5 h-3.5" />
                <span>Save as Draft</span>
              </button>
              <button
                onClick={() => saveStrategy('productive')}
                disabled={createMutation.isPending}
                className="bg-green-700 hover:bg-green-600 text-white rounded border border-green-600 text-xs flex items-center gap-1.5 px-4 py-1.5 font-bold"
              >
                <CheckCircle2 className="w-3.5 h-3.5" />
                <span>Save & Enable</span>
              </button>
            </>
          )}
        </div>
      </div>

      {actionMessage && (
        <div
          className={`shrink-0 p-3 rounded-lg text-xs flex items-center justify-between border ${
            actionMessage.type === 'error'
              ? 'bg-red-950/70 border-red-800 text-red-300'
              : 'bg-green-950/70 border-green-800 text-green-300'
          }`}
        >
          <span>{actionMessage.text}</span>
          <button onClick={() => setActionMessage(null)} className="text-xs px-1">
            ✕
          </button>
        </div>
      )}

      {previewError && (
        <div
          role="alert"
          className="shrink-0 flex items-start justify-between gap-3 p-3 bg-red-950/80 border border-red-800 text-red-300 rounded-lg text-xs"
        >
          <div className="flex items-start gap-2">
            <AlertCircle className="w-4 h-4 text-red-400 shrink-0 mt-0.5" />
            <div className="space-y-0.5">
              <span className="font-bold block text-red-200 uppercase tracking-wide text-[11px]">
                Script Execution Error
              </span>
              <pre className="whitespace-pre-wrap font-mono text-[11px] text-red-300">{previewError}</pre>
            </div>
          </div>
          <button onClick={() => setPreviewError(null)} className="text-red-400 hover:text-red-200 text-xs px-1">
            ✕
          </button>
        </div>
      )}

      {/* Metadata form and Lua editor are separate components (see
          StrategyMetadataForm.tsx / LuaScriptEditor.tsx) in separate
          CollapsibleSections - collapsing the metadata card here is what
          actually maximizes the editor below it (flex-1 reclaims the
          space), independent of WorkbenchShell's own Script/Split/Chart
          view mode which controls horizontal space instead. */}
      <CollapsibleSection id="workbench.editor.config" title="Strategy Configuration" defaultOpen className="shrink-0">
        <StrategyMetadataForm
          draft={draft}
          setDraft={setDraft}
          symbolInput={symbolInput}
          setSymbolInput={setSymbolInput}
          onAddSymbol={handleAddSymbol}
          onRemoveSymbol={handleRemoveSymbol}
        />
      </CollapsibleSection>

      <CollapsibleSection
        id="workbench.editor.luaSource"
        title="Lua Source Code"
        subtitle="(sandboxed, 500ms timeout)"
        defaultOpen
        fill
        className="flex-1 min-h-0"
        action={
          <>
            <button
              onClick={handleReRunNow}
              className="text-[11px] text-green-700 hover:text-green-400 flex items-center gap-1"
              title="Re-run now (skip the debounce wait)"
            >
              <RefreshCw className="w-3 h-3" />
              <span>Re-run now</span>
            </button>
            <button
              onClick={() => setDraft((prev) => ({ ...prev, source: DEFAULT_LUA_TEMPLATE }))}
              className="text-[11px] text-green-700 hover:text-green-400 flex items-center gap-1"
              title="Reset to template"
            >
              <RotateCcw className="w-3 h-3" />
              <span>Reset</span>
            </button>
          </>
        }
      >
        <LuaScriptEditor
          source={draft.source}
          onChange={(val) => setDraft((prev) => ({ ...prev, source: val }))}
          diagnostics={diagnostics}
          onCreateEditor={(view) => {
            editorViewRef.current = view;
          }}
          fill
        />
      </CollapsibleSection>
    </div>
  );
}
