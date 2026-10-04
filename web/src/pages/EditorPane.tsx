import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useOutletContext } from 'react-router-dom';
import { EditorView } from '@codemirror/view';
import { buildLuaDiagnostic } from '@/lib/luaLinting';
import { api, apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { useCreateStrategy, useUpdateStrategy } from '@/hooks/queries';
import { StrategyStatus, StrategyCreateRequest, StrategyUpdateRequest } from '@/api/types';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { StrategyMetadataForm } from '@/components/domain/StrategyMetadataForm';
import { LuaScriptEditor } from '@/components/domain/LuaScriptEditor';
import {
  DEFAULT_LUA_TEMPLATE,
  cycleTimeframe,
  toStrategyConfiguration,
  WorkbenchContext,
} from './WorkbenchShell';
import { Save, Rocket, CheckCircle2, AlertCircle, Code2, RotateCcw, RefreshCw, Maximize2, Minimize2 } from 'lucide-react';
import { useT } from '@/i18n';

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
  const t = useT();
  const navigate = useNavigate();
  const ctx = useOutletContext<WorkbenchContext>();
  const { draft, setDraft, setEditorTrace, setActiveTraceSource, appendConsoleEntry, readOnly, zenMode, setZenMode } = ctx;
  // auth-02 §4: the live preview (fast-rerun) needs `backtest`; non-admins
  // only create/save backtest drafts.
  const { can } = useAuth();
  const isAdmin = can('admin');
  const canPreview = can('backtest');
  const canPreviewRef = useRef(canPreview);
  canPreviewRef.current = canPreview;
  const readOnlyRef = useRef(readOnly);
  readOnlyRef.current = readOnly;

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
    if (!canPreviewRef.current) return;
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
          timeframe: cycleTimeframe(draft.cycleMinutes),
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
        setPreviewError(apiErrorMessage(err, t('workbench.previewFailed')));
        appendConsoleEntry({ source: 'editor', kind: 'error', message: apiErrorMessage(err, t('workbench.previewFailed')) });
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
    if (readOnlyRef.current) return null;
    if (!draft.name.trim()) {
      setActionMessage({ type: 'error', text: t('workbench.nameRequired') });
      return null;
    }
    if (draft.symbols.length === 0) {
      setActionMessage({ type: 'error', text: t('workbench.symbolRequired') });
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
        setActionMessage({ type: 'success', text: t('workbench.saved', { id: draft.strategyId }) });
        return draft.strategyId;
      } else {
        const createReq: StrategyCreateRequest = {
          name: draft.name.trim(),
          description: draft.description.trim(),
          strategy_name: 'script',
          status: finalStatus,
          // Non-admins may only create backtest drafts (auth-01 §3).
          mode: isAdmin ? 'dryrun' : 'backtest',
          monitored_symbols: draft.symbols,
          cycle: draft.cycleMinutes,
          configuration: config,
          script_source: draft.source,
        };
        const created = await createMutation.mutateAsync(createReq);
        setActionMessage({ type: 'success', text: t('workbench.created', { id: created.id }) });
        // create -> edit transition: re-derive strategyId from the new URL.
        navigate(`/strategies/${created.id}/edit`);
        return created.id;
      }
    } catch (err: any) {
      setActionMessage({ type: 'error', text: apiErrorMessage(err, t('workbench.saveFailed')) });
      return null;
    }
  };

  const handleRunFullBacktest = async () => {
    // Read-only: nothing to save - just open the Backtest tab.
    if (readOnly) {
      if (draft.strategyId) {
        const sym = draft.previewSymbol || draft.symbols[0] || 'BTCUSDT';
        navigate(`/strategies/${draft.strategyId}/edit/backtest?symbol=${encodeURIComponent(sym)}`);
      }
      return;
    }
    const savedId = await saveStrategy('disabled');
    if (savedId) {
      const sym = draft.previewSymbol || draft.symbols[0] || 'BTCUSDT';
      navigate(`/strategies/${savedId}/edit/backtest?symbol=${encodeURIComponent(sym)}`);
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
      {zenMode === 'editor' ? (
        /* VS Code Style Zen Mode Top Bar */
        <div className="flex flex-wrap items-center justify-between gap-3 pb-2.5 border-b border-border/60 shrink-0 font-sans">
          <div className="flex items-center gap-2.5">
            <div className="p-1.5 rounded-md bg-primary/10 border border-primary/20 text-primary">
              <Code2 className="w-4 h-4" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-bold text-sm text-foreground">
                  {isEdit ? draft.name || t('workbench.strategyNumber', { id: draft.strategyId }) : t('workbench.titleNew')}
                </span>
                <span className="text-[11px] px-2 py-0.5 rounded font-mono font-semibold bg-secondary/80 text-foreground border border-border/60">
                  {t('workbench.scriptFile')}
                </span>
                <span
                  className={`text-[10px] px-2 py-0.5 rounded font-bold uppercase tracking-wider ${
                    draft.mode === 'live'
                      ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30'
                      : draft.mode === 'dryrun'
                      ? 'bg-amber-500/20 text-amber-400 border border-amber-500/30'
                      : 'bg-sky-500/20 text-sky-400 border border-sky-500/30'
                  }`}
                >
                  {draft.mode}
                </span>
                <span className="text-[11px] px-1.5 py-0.5 rounded font-mono bg-accent/40 text-muted-foreground border border-border/40">
                  {cycleTimeframe(draft.cycleMinutes)}
                </span>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <label className="flex items-center gap-1.5 cursor-pointer select-none text-[11px] text-muted-foreground hover:text-foreground mr-1">
              <input
                type="checkbox"
                checked={liveRefresh}
                onChange={(e) => setLiveRefresh(e.target.checked)}
              />
              <span>{t('workbench.liveRefresh', { seconds: LIVE_REFRESH_MS / 1000 })}</span>
            </label>

            {canPreview && (
              <button
                type="button"
                onClick={handleReRunNow}
                title={t('workbench.rerunTitle')}
                className="bg-card/40 hover:bg-secondary/60 text-foreground rounded border border-border/50 text-xs flex items-center gap-1.5 px-2.5 py-1.5"
              >
                <RefreshCw className="w-3.5 h-3.5 text-primary" />
                <span>{t('workbench.rerun')}</span>
              </button>
            )}

            {!readOnly && (
              <button
                type="button"
                onClick={() => saveStrategy(isEdit ? undefined : 'disabled')}
                disabled={updateMutation.isPending || createMutation.isPending}
                title="Ctrl+S / ⌘S"
                data-testid="zen-save-btn"
                className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold shadow-sm"
              >
                {updateMutation.isPending || createMutation.isPending ? (
                  <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Save className="w-3.5 h-3.5" />
                )}
                <span>{isEdit ? t('workbench.saveChanges') : t('workbench.saveDraft')}</span>
                {/* i18n-ignore */}<kbd className="px-1 py-0.5 text-[9px] bg-black/20 rounded font-mono opacity-80">⌘S</kbd>
              </button>
            )}

            <button
              type="button"
              onClick={() => setZenMode('none')}
              title={t('workbench.exitZenMode')}
              data-testid="exit-editor-zen-btn"
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded-md bg-secondary hover:bg-secondary/80 text-foreground border border-border/60 transition-colors shadow-sm ml-1"
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
        <div className="flex flex-col gap-2 shrink-0">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-1.5">
              <Code2 className="w-3.5 h-3.5 text-foreground shrink-0" />
              <span className="text-[11px] text-muted-foreground" title={t('workbench.autoRunTitle')}>
                {t('workbench.autoRun')}
              </span>
            </div>
            <label className="flex items-center gap-1.5 cursor-pointer select-none text-[11px] text-muted-foreground hover:text-foreground">
              <input
                type="checkbox"
                checked={liveRefresh}
                onChange={(e) => setLiveRefresh(e.target.checked)}
              />
              <span>{t('workbench.liveRefresh', { seconds: LIVE_REFRESH_MS / 1000 })}</span>
            </label>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs">
            {canPreview && (!readOnly || isEdit) && (
            <button
              onClick={handleRunFullBacktest}
              disabled={createMutation.isPending || updateMutation.isPending}
              className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs flex items-center gap-1.5 px-3 py-1.5"
              title={t('workbench.runBacktestTitle')}
            >
              <Rocket className="w-3.5 h-3.5 text-accent-foreground" />
              <span>{readOnly ? t('workbench.openBacktest') : t('workbench.runBacktest')}</span>
            </button>
            )}
            {readOnly ? null : isEdit ? (
              <button
                onClick={() => saveStrategy()}
                disabled={updateMutation.isPending}
                title="Ctrl+S / ⌘S"
                className="bg-primary hover:bg-primary text-white rounded border border-primary text-xs flex items-center gap-1.5 px-4 py-1.5 font-bold"
              >
                <Save className="w-3.5 h-3.5" />
                <span>{updateMutation.isPending ? t('common.saving') : t('workbench.saveChanges')}</span>
              </button>
            ) : (
              <>
                <button
                  onClick={() => saveStrategy(isAdmin ? 'disabled' : undefined)}
                  disabled={createMutation.isPending}
                  title="Ctrl+S / ⌘S"
                  className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs flex items-center gap-1.5 px-3 py-1.5"
                >
                  <Save className="w-3.5 h-3.5" />
                  <span>{t('workbench.saveDraft')}</span>
                </button>
                {isAdmin && (
                <button
                  onClick={() => saveStrategy('productive')}
                  disabled={createMutation.isPending}
                  className="bg-primary hover:bg-primary text-white rounded border border-primary text-xs flex items-center gap-1.5 px-4 py-1.5 font-bold"
                >
                  <CheckCircle2 className="w-3.5 h-3.5" />
                  <span>{t('workbench.saveEnable')}</span>
                </button>
                )}
              </>
            )}
          </div>
        </div>
      )}

      {actionMessage && (
        <div
          className={`shrink-0 p-3 rounded-lg text-xs flex items-center justify-between border ${
            actionMessage.type === 'error'
              ? 'bg-destructive/15 border-destructive/40 text-destructive'
              : 'bg-card/70 border-border text-foreground'
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
          className="shrink-0 flex items-start justify-between gap-3 p-3 bg-destructive/15 border border-destructive/40 text-destructive rounded-lg text-xs"
        >
          <div className="flex items-start gap-2">
            <AlertCircle className="w-4 h-4 text-destructive shrink-0 mt-0.5" />
            <div className="space-y-0.5">
              <span className="font-bold block text-destructive uppercase tracking-wide text-[11px]">
                {t('workbench.scriptError')}
              </span>
              <pre className="whitespace-pre-wrap font-mono text-[11px] text-destructive">{previewError}</pre>
            </div>
          </div>
          <button onClick={() => setPreviewError(null)} className="text-destructive hover:text-destructive text-xs px-1">
            ✕
          </button>
        </div>
      )}

      {/* Metadata form: hidden in Zen Mode so the editor gets 100% height */}
      <div className={zenMode === 'editor' ? 'hidden' : 'shrink-0'}>
        <CollapsibleSection id="workbench.editor.config" title={t('workbench.configSection')} defaultOpen>
          <StrategyMetadataForm
            draft={draft}
            setDraft={setDraft}
            symbolInput={symbolInput}
            setSymbolInput={setSymbolInput}
            onAddSymbol={handleAddSymbol}
            onRemoveSymbol={handleRemoveSymbol}
            readOnly={readOnly}
            draftOnlyModeStatus={!isAdmin}
          />
        </CollapsibleSection>
      </div>

      <CollapsibleSection
        id="workbench.editor.luaSource"
        title={t('workbench.luaSection')}
        subtitle={t('workbench.luaSubtitle')}
        defaultOpen
        fill
        hideHeader={zenMode === 'editor'}
        className="flex-1 min-h-0"
        action={
          <>
            <button
              type="button"
              onClick={() => setZenMode('editor')}
              className="text-[11px] text-muted-foreground hover:text-foreground flex items-center gap-1 px-1.5 py-0.5 rounded hover:bg-secondary/40"
              title={t('workbench.fullscreenEditorTitle')}
              data-testid="enter-editor-zen-btn"
            >
              <Maximize2 className="w-3 h-3" />
              <span>{t('workbench.zenModeEditorShort')}</span>
            </button>
            {canPreview && (
            <button
              onClick={handleReRunNow}
              className="text-[11px] text-muted-foreground hover:text-foreground flex items-center gap-1"
              title={t('workbench.rerunTitle')}
            >
              <RefreshCw className="w-3 h-3" />
              <span>{t('workbench.rerun')}</span>
            </button>
            )}
            {!readOnly && (
            <button
              onClick={() => setDraft((prev) => ({ ...prev, source: DEFAULT_LUA_TEMPLATE }))}
              className="text-[11px] text-muted-foreground hover:text-foreground flex items-center gap-1"
              title={t('workbench.resetTitle')}
            >
              <RotateCcw className="w-3 h-3" />
              <span>{t('common.reset')}</span>
            </button>
            )}
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
          readOnly={readOnly}
        />
      </CollapsibleSection>

      {/* VS Code Style Status Bar in Zen Mode */}
      {zenMode === 'editor' && (
        <div className="flex items-center justify-between px-3 py-1 bg-secondary/40 border border-border/50 rounded-lg text-[11px] text-muted-foreground font-mono shrink-0 select-none">
          <div className="flex items-center gap-2.5">
            <span>Lua 5.1 (GopherLua)</span>
            <span>•</span>
            <span>{t('workbench.symbolLabel')} {draft.symbols.join(', ')}</span>
            <span>•</span>
            <span>{t('workbench.tfLabel')} {cycleTimeframe(draft.cycleMinutes)}</span>
            {draft.stopLossPct != null && (
              <>
                <span>•</span>
                <span>SL: {draft.stopLossPct}%</span>
              </>
            )}
          </div>
          <div className="flex items-center gap-2.5">
            {/* i18n-ignore */}<span>UTF-8</span>
            <span>•</span>
            <span>{t('workbench.pressEscToExit')}</span>
          </div>
        </div>
      )}
    </div>
  );
}
