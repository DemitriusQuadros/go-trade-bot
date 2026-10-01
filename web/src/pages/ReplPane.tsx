import React, { useEffect, useRef, useState } from 'react';
import { useOutletContext } from 'react-router-dom';
import CodeMirror from '@uiw/react-codemirror';
import { StreamLanguage } from '@codemirror/language';
import { lua } from '@codemirror/legacy-modes/mode/lua';
import { luaEditorDarkTheme, luaEditorLightTheme } from '@/lib/codeMirrorTheme';
import { luaAutocompletion } from '@/lib/luaCompletions';
import { api } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { TraceRecord } from '@/api/types';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { WorkbenchContext } from './WorkbenchShell';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';
import { Play, Terminal, AlertCircle, CheckCircle2, XCircle, RotateCcw, RefreshCw, Maximize2, Minimize2 } from 'lucide-react';
import { formatTime } from '@/lib/format';
import { useT } from '@/i18n';

interface ReplHistoryEntry {
  id: string;
  source: string;
  symbol: string;
  timeframe: string;
  result: unknown;
  trace: TraceRecord[];
  error?: string;
  noData?: boolean;
  timestamp: string;
}

const DEFAULT_SNIPPET = `-- Evaluate Lua expressions or test indicator values:
return ind.rsi(14)
`;

const TIMEFRAME_OPTIONS = ['1m', '5m', '15m', '1h', '4h', '1d'];

// Same cap as EditorPane.tsx's fast-rerun preview - kept identical so
// zooming out behaves consistently regardless of which pane's chart the
// operator is looking at.
const MAX_WINDOW_CANDLES = 2000;

// The REPL evaluates on the backend (POST /script/repl) - needs `backtest`
// (auth-02 §4). Without it, show a notice instead of an editor that would
// 403 on every keystroke.
export function ReplPane() {
  const t = useT();
  const canRun = useAuth().can('backtest');
  if (!canRun) {
    return (
      <div className="p-8 text-center text-xs text-muted-foreground font-sans" data-testid="repl-no-permission">
        {t('workbench.replNoPermission')}
      </div>
    );
  }
  return <ReplPaneInner />;
}

function ReplPaneInner() {
  const t = useT();
  const ctx = useOutletContext<WorkbenchContext>();
  const { setReplTrace, setReplResult, setActiveTraceSource, draft, appendConsoleEntry, zenMode, setZenMode } = ctx;
  const isDark = useIsDarkMode();

  // source/symbol/timeframe/windowCandles/history/selectedHistoryId stay
  // PANE-LOCAL - only replTrace/replResult are lifted to the shell, since
  // those are what SharedPriceChart (shell-level) and the Return Value
  // panel both need.
  const [source, setSource] = useState(DEFAULT_SNIPPET);
  // Pre-fill symbol from the draft's monitored symbols - a one-time
  // initialization, not a synced/lifted field.
  const [symbol, setSymbol] = useState(draft.symbols[0] || 'BTCUSDT');
  const [timeframe, setTimeframe] = useState('15m');
  const [windowCandles, setWindowCandles] = useState<number>(100);

  const [activeError, setActiveError] = useState<string | null>(null);
  const [activeNoData, setActiveNoData] = useState<boolean>(false);

  const [history, setHistory] = useState<ReplHistoryEntry[]>([]);
  const [selectedHistoryId, setSelectedHistoryId] = useState<string | null>(null);
  const [isEvaluating, setIsEvaluating] = useState(false);

  // Mirrors windowCandles for synchronous reads inside handleLoadMoreHistory
  // (needs the widened value immediately, before the next render commits it).
  const windowCandlesRef = useRef(windowCandles);
  windowCandlesRef.current = windowCandles;
  const handleEvaluateRef = useRef<() => void>(() => {});
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();
  // Cancels any still-in-flight request from a prior keystroke, same as
  // EditorPane.tsx's fast-rerun preview - api.repl (unlike the old useRepl()
  // mutation hook this replaced) takes an AbortSignal for exactly this.
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    setActiveTraceSource('repl');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleEvaluate = async () => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setIsEvaluating(true);
    setActiveError(null);
    setActiveNoData(false);

    try {
      const res = await api.repl(
        {
          source,
          symbol: symbol.trim().toUpperCase(),
          timeframe,
          window_candles: windowCandlesRef.current,
        },
        { signal: controller.signal }
      );

      const entryId = `${Date.now()}-${Math.random().toString(36).substring(2, 6)}`;
      const isNoData = res.data_available === false;
      const newEntry: ReplHistoryEntry = {
        id: entryId,
        source,
        symbol: symbol.trim().toUpperCase(),
        timeframe,
        result: res.result,
        trace: res.trace || [],
        error: res.error,
        noData: isNoData,
        timestamp: formatTime(new Date()),
      };

      setHistory((prev) => [newEntry, ...prev.slice(0, 49)]); // keep up to 50 entries
      setSelectedHistoryId(entryId);
      setReplResult(res.result);
      setReplTrace(res.trace || []);
      setActiveError(res.error || null);
      setActiveNoData(isNoData);
      setIsEvaluating(false);
      ctx.setLoadingMoreHistory(false);
      // The REPL response has no candle count to compare against the
      // requested window (unlike fast-rerun's one-record-per-candle trace,
      // EvalREPL always returns at most one) - data_available===false is the
      // only signal that there's nothing further back to fetch.
      ctx.setHasMoreHistory(!isNoData && windowCandlesRef.current < MAX_WINDOW_CANDLES);

      if (res.error) {
        appendConsoleEntry({ source: 'repl', kind: 'error', message: res.error });
      } else {
        const lastRecord = (res.trace || [])[(res.trace || []).length - 1];
        lastRecord?.log?.forEach((entry) =>
          appendConsoleEntry({ source: 'repl', kind: 'debug_log', label: entry.label, message: String(entry.value) })
        );
      }
    } catch (err: any) {
      if (err?.name === 'AbortError') return; // superseded by a newer keystroke - not a real error
      const message = err.message || t('workbench.evalFailed');
      setActiveError(message);
      setReplTrace([]);
      setReplResult(null);
      setActiveNoData(false);
      setIsEvaluating(false);
      ctx.setLoadingMoreHistory(false);
      appendConsoleEntry({ source: 'repl', kind: 'error', message });
    }
  };
  handleEvaluateRef.current = handleEvaluate;

  // Auto-runs 600ms after the snippet/symbol/timeframe stop changing - same
  // debounce as EditorPane.tsx's fast-rerun preview, so the chart and Return
  // Value panel populate on their own instead of requiring an explicit
  // "Evaluate" click first. Fires once on mount too (an empty deps-diff is
  // still a "change" the first time), which is what makes the chart/candles
  // load automatically as soon as the REPL tab opens. windowCandles is
  // deliberately excluded - zoom-out-triggered widenings re-run immediately
  // via handleLoadMoreHistory below, not through this debounce.
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => handleEvaluateRef.current(), 600);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [source, symbol, timeframe]);

  const handleEvaluateNow = () => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    handleEvaluate();
  };

  // Zoom-out-loads-more-history (SharedPriceChart's onNeedMoreHistory, wired
  // through WorkbenchShell's registerLoadMoreHistory below): doubles the
  // window and re-evaluates the same snippet immediately.
  const handleLoadMoreHistory = () => {
    if (windowCandlesRef.current >= MAX_WINDOW_CANDLES) {
      ctx.setHasMoreHistory(false);
      return;
    }
    const nextWindow = Math.min(windowCandlesRef.current * 2, MAX_WINDOW_CANDLES);
    windowCandlesRef.current = nextWindow;
    setWindowCandles(nextWindow);
    ctx.setLoadingMoreHistory(true);
    handleEvaluateRef.current();
  };
  const handleLoadMoreHistoryRef = useRef(handleLoadMoreHistory);
  handleLoadMoreHistoryRef.current = handleLoadMoreHistory;

  // Only the actually-visible pane's handler should be wired into the
  // shared chart - see EditorPane.tsx's identical registration effect.
  useEffect(() => {
    if (ctx.activeTraceSource !== 'repl') return;
    ctx.registerLoadMoreHistory(() => handleLoadMoreHistoryRef.current());
    return () => ctx.registerLoadMoreHistory(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ctx.activeTraceSource]);

  // A different symbol/timeframe has an unrelated history depth - start its
  // window back at the default rather than carrying over a size only
  // meaningful for the previous symbol.
  useEffect(() => {
    windowCandlesRef.current = 100;
    setWindowCandles(100);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [symbol, timeframe]);

  const handleSelectHistoryEntry = (entry: ReplHistoryEntry) => {
    setSelectedHistoryId(entry.id);
    setSource(entry.source);
    setSymbol(entry.symbol);
    setTimeframe(entry.timeframe);
    setReplResult(entry.result);
    setReplTrace(entry.trace);
    setActiveError(entry.error || null);
    setActiveNoData(!!entry.noData);
  };

  return (
    // Was a viewport-width-keyed lg:grid-cols-12 split - this pane only ever
    // renders inside WorkbenchShell's narrow left panel now (see routes in
    // App.tsx), where a `lg:` breakpoint fires off the *window* width, not
    // this already-narrow container's, and would squeeze a 6/6 split into
    // ~220px each. Always-stacked avoids that regardless of window size.
    <div className="flex flex-col gap-5">
      {/* Left column: editor + history */}
      <div className="space-y-4">
        <CollapsibleSection
          id="workbench.repl.snippet"
          title={t('workbench.luaSnippet')}
          subtitle={t('workbench.snippetSubtitle')}
          defaultOpen
          action={
            <>
              <button
                type="button"
                onClick={() => setZenMode(zenMode === 'editor' ? 'none' : 'editor')}
                className="text-[11px] text-muted-foreground hover:text-foreground flex items-center gap-1 px-1.5 py-0.5 rounded hover:bg-secondary/40"
                title={t('workbench.fullscreenEditorTitle')}
                data-testid="repl-zen-btn"
              >
                {zenMode === 'editor' ? <Minimize2 className="w-3 h-3" /> : <Maximize2 className="w-3 h-3" />}
                <span>{zenMode === 'editor' ? t('workbench.exitZenModeShort') : t('workbench.zenModeEditorShort')}</span>
              </button>
              <button
                onClick={handleEvaluateNow}
                className="text-[11px] text-muted-foreground hover:text-foreground flex items-center gap-1"
                title={t('workbench.rerunTitle')}
              >
                <RefreshCw className="w-3 h-3" />
                <span>{t('workbench.rerun')}</span>
              </button>
              <button
                onClick={() => setSource(DEFAULT_SNIPPET)}
                className="text-[11px] text-muted-foreground hover:text-foreground flex items-center gap-1"
                title={t('workbench.resetTitle')}
              >
                <RotateCcw className="w-3 h-3" />
                <span>{t('common.reset')}</span>
              </button>
            </>
          }
        >
          <div className="text-sm bg-background/95 -m-3">
            <CodeMirror
              value={source}
              height="220px"
              theme={isDark ? 'dark' : 'light'}
              extensions={[StreamLanguage.define(lua), isDark ? luaEditorDarkTheme : luaEditorLightTheme, luaAutocompletion]}
              onChange={(val) => setSource(val)}
              className="font-mono text-xs"
              basicSetup={{ lineNumbers: true, highlightActiveLineGutter: true, foldGutter: false }}
            />
          </div>
        </CollapsibleSection>

        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground" title={t('workbench.replAutoRunTitle')}>
          <Terminal className="w-3.5 h-3.5 text-foreground shrink-0" />
          <span>{t('workbench.autoRun')}</span>
        </div>

        <div className="flex flex-wrap items-center gap-2 text-xs">
          <div className="flex items-center gap-1.5 bg-background/60 p-1 rounded border border-border/40">
            <span className="text-muted-foreground px-1 text-[11px]">{t('workbench.symbolLabel')}</span>
            <input
              type="text"
              value={symbol}
              onChange={(e) => setSymbol(e.target.value)}
              className="bg-background text-foreground font-bold px-2 py-1 rounded border border-border text-xs w-24 uppercase focus:outline-none focus:border-primary"
              placeholder="BTCUSDT"
            />
          </div>
          <div className="flex items-center gap-1.5 bg-background/60 p-1 rounded border border-border/40">
            <span className="text-muted-foreground px-1 text-[11px]">{t('workbench.tfLabel')}</span>
            <select
              value={timeframe}
              onChange={(e) => setTimeframe(e.target.value)}
              className="bg-background text-foreground font-bold px-2 py-1 rounded border border-border text-xs focus:outline-none focus:border-primary"
            >
              {TIMEFRAME_OPTIONS.map((tf) => (
                <option key={tf} value={tf}>
                  {tf}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-1.5 bg-background/60 p-1 rounded border border-border/40">
            <span className="text-muted-foreground px-1 text-[11px]">{t('workbench.windowLabel')}</span>
            <input
              type="number"
              value={windowCandles}
              onChange={(e) => setWindowCandles(Number(e.target.value) || 100)}
              className="bg-background text-foreground font-bold px-2 py-1 rounded border border-border text-xs w-16 focus:outline-none focus:border-primary"
              min={10}
              max={MAX_WINDOW_CANDLES}
              title={t('workbench.windowTitle')}
            />
          </div>
          <button
            onClick={handleEvaluateNow}
            disabled={isEvaluating}
            className="bg-primary hover:bg-primary text-white rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
          >
            <Play className="w-3.5 h-3.5 fill-current" />
            <span>{isEvaluating ? t('workbench.evaluating') : t('workbench.evaluate')}</span>
          </button>
        </div>

        {activeError && (
          <div
            role="alert"
            className="flex items-start justify-between gap-3 p-3 bg-destructive/15 border border-destructive/40 text-destructive rounded-lg text-xs"
          >
            <div className="flex items-start gap-2">
              <AlertCircle className="w-4 h-4 text-destructive shrink-0 mt-0.5" />
              <div className="space-y-0.5">
                <span className="font-bold block text-destructive uppercase tracking-wide text-[11px]">
                  {t('workbench.evalError')}
                </span>
                <pre className="whitespace-pre-wrap font-mono text-[11px] text-destructive">{activeError}</pre>
              </div>
            </div>
            <button onClick={() => setActiveError(null)} className="text-destructive hover:text-destructive text-xs px-1">
              ✕
            </button>
          </div>
        )}

        <CollapsibleSection id="workbench.repl.history" title={t('workbench.history')} subtitle={`(${history.length})`} defaultOpen>
          {history.length === 0 ? (
            <div className="p-4 text-center text-xs text-muted-foreground bg-background/40 rounded">
              {t('workbench.historyEmpty')}
            </div>
          ) : (
            <div className="space-y-1.5 max-h-56 overflow-y-auto pr-1">
              {history.map((entry) => {
                const isSelected = entry.id === selectedHistoryId;
                const isErr = !!entry.error;
                return (
                  <button
                    key={entry.id}
                    onClick={() => handleSelectHistoryEntry(entry)}
                    className={`w-full text-left p-2 rounded text-xs transition-colors flex items-center justify-between gap-2 border ${
                      isSelected
                        ? 'bg-card/60 border-primary text-foreground'
                        : 'bg-background/60 border-border/40 text-muted-foreground hover:bg-card/20 hover:text-foreground'
                    }`}
                  >
                    <div className="flex items-center gap-2 overflow-hidden">
                      {isErr ? (
                        <XCircle className="w-3.5 h-3.5 text-destructive shrink-0" />
                      ) : entry.noData ? (
                        <AlertCircle className="w-3.5 h-3.5 text-warning shrink-0" />
                      ) : (
                        <CheckCircle2 className="w-3.5 h-3.5 text-success shrink-0" />
                      )}
                      <span className="font-mono truncate text-[11px] text-foreground">
                        {entry.source.split('\n')[0] || t('workbench.emptySnippet')}
                      </span>
                    </div>
                    <div className="flex items-center gap-2 text-[10px] shrink-0 text-muted-foreground">
                      <span>{entry.symbol}</span>
                      <span>{entry.timeframe}</span>
                      <span>{entry.timestamp}</span>
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </CollapsibleSection>
      </div>

      {/* Right column: Return Value panel ONLY - no chart here anymore;
          SharedPriceChart (shell-level) now renders replTrace. */}
      <div className="space-y-4">
        <CollapsibleSection id="workbench.repl.returnValue" title={t('workbench.returnValue')} subtitle="(Runner.Eval output)" defaultOpen>
          <div className="p-3 bg-background/90 rounded border border-border/60 text-xs min-h-[70px] overflow-x-auto">
            {activeNoData ? (
              <div className="text-warning text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 shrink-0" />
                <span>{t('workbench.noCandles', { symbol, timeframe })}</span>
              </div>
            ) : ctx.replResult !== null && ctx.replResult !== undefined ? (
              <pre className="text-success font-mono text-xs">
                {typeof ctx.replResult === 'object' ? JSON.stringify(ctx.replResult, null, 2) : String(ctx.replResult)}
              </pre>
            ) : (
              <span className="text-muted-foreground text-xs italic">
                {activeError ? t('workbench.evalTerminated') : t('workbench.noResult')}
              </span>
            )}
          </div>
        </CollapsibleSection>
      </div>
    </div>
  );
}
