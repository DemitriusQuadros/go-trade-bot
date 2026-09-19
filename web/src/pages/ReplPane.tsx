import React, { useEffect, useState } from 'react';
import { useOutletContext } from 'react-router-dom';
import CodeMirror from '@uiw/react-codemirror';
import { StreamLanguage } from '@codemirror/language';
import { lua } from '@codemirror/legacy-modes/mode/lua';
import { luaEditorDarkTheme } from '@/lib/codeMirrorTheme';
import { luaAutocompletion } from '@/lib/luaCompletions';
import { useRepl } from '@/hooks/queries';
import { TraceRecord } from '@/api/types';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { WorkbenchContext } from './WorkbenchShell';
import { Play, Terminal, AlertCircle, CheckCircle2, XCircle, RotateCcw } from 'lucide-react';

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

export function ReplPane() {
  const ctx = useOutletContext<WorkbenchContext>();
  const { setReplTrace, setReplResult, setActiveTraceSource, draft, appendConsoleEntry } = ctx;

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

  const replMutation = useRepl();

  useEffect(() => {
    setActiveTraceSource('repl');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleEvaluate = async () => {
    setActiveError(null);
    setActiveNoData(false);

    try {
      const res = await replMutation.mutateAsync({
        source,
        symbol: symbol.trim().toUpperCase(),
        timeframe,
        window_candles: windowCandles,
      });

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
        timestamp: new Date().toLocaleTimeString(),
      };

      setHistory((prev) => [newEntry, ...prev.slice(0, 49)]); // keep up to 50 entries
      setSelectedHistoryId(entryId);
      setReplResult(res.result);
      setReplTrace(res.trace || []);
      setActiveError(res.error || null);
      setActiveNoData(isNoData);

      if (res.error) {
        appendConsoleEntry({ source: 'repl', kind: 'error', message: res.error });
      } else {
        const lastRecord = (res.trace || [])[(res.trace || []).length - 1];
        lastRecord?.log?.forEach((entry) =>
          appendConsoleEntry({ source: 'repl', kind: 'debug_log', label: entry.label, message: String(entry.value) })
        );
      }
    } catch (err: any) {
      const message = err.message || 'Failed to evaluate snippet';
      setActiveError(message);
      setReplTrace([]);
      setReplResult(null);
      setActiveNoData(false);
      appendConsoleEntry({ source: 'repl', kind: 'error', message });
    }
  };

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
          title="Lua Snippet"
          subtitle="(expression or hook)"
          defaultOpen
          action={
            <button
              onClick={() => setSource(DEFAULT_SNIPPET)}
              className="text-[11px] text-green-700 hover:text-green-400 flex items-center gap-1"
              title="Reset to template"
            >
              <RotateCcw className="w-3 h-3" />
              <span>Reset</span>
            </button>
          }
        >
          <div className="text-sm bg-black/95 -m-3">
            <CodeMirror
              value={source}
              height="220px"
              theme="dark"
              extensions={[StreamLanguage.define(lua), luaEditorDarkTheme, luaAutocompletion]}
              onChange={(val) => setSource(val)}
              className="font-mono text-xs"
              basicSetup={{ lineNumbers: true, highlightActiveLineGutter: true, foldGutter: false }}
            />
          </div>
        </CollapsibleSection>

        <div className="flex flex-wrap items-center gap-2 text-xs">
          <div className="flex items-center gap-1.5 bg-black/60 p-1 rounded border border-green-900/40">
            <span className="text-green-700 px-1 text-[11px]">Symbol:</span>
            <input
              type="text"
              value={symbol}
              onChange={(e) => setSymbol(e.target.value)}
              className="bg-black text-green-300 font-bold px-2 py-1 rounded border border-green-950 text-xs w-24 uppercase focus:outline-none focus:border-green-600"
              placeholder="BTCUSDT"
            />
          </div>
          <div className="flex items-center gap-1.5 bg-black/60 p-1 rounded border border-green-900/40">
            <span className="text-green-700 px-1 text-[11px]">TF:</span>
            <select
              value={timeframe}
              onChange={(e) => setTimeframe(e.target.value)}
              className="bg-black text-green-300 font-bold px-2 py-1 rounded border border-green-950 text-xs focus:outline-none focus:border-green-600"
            >
              {TIMEFRAME_OPTIONS.map((tf) => (
                <option key={tf} value={tf}>
                  {tf}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-1.5 bg-black/60 p-1 rounded border border-green-900/40">
            <span className="text-green-700 px-1 text-[11px]">Window:</span>
            <input
              type="number"
              value={windowCandles}
              onChange={(e) => setWindowCandles(Number(e.target.value) || 100)}
              className="bg-black text-green-300 font-bold px-2 py-1 rounded border border-green-950 text-xs w-16 focus:outline-none focus:border-green-600"
              min={10}
              max={500}
            />
          </div>
          <button
            onClick={handleEvaluate}
            disabled={replMutation.isPending}
            className="bg-green-700 hover:bg-green-600 text-white rounded border border-green-600 text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
          >
            <Play className="w-3.5 h-3.5 fill-current" />
            <span>{replMutation.isPending ? 'Evaluating...' : 'Evaluate'}</span>
          </button>
        </div>

        {activeError && (
          <div
            role="alert"
            className="flex items-start justify-between gap-3 p-3 bg-red-950/80 border border-red-800 text-red-300 rounded-lg text-xs"
          >
            <div className="flex items-start gap-2">
              <AlertCircle className="w-4 h-4 text-red-400 shrink-0 mt-0.5" />
              <div className="space-y-0.5">
                <span className="font-bold block text-red-200 uppercase tracking-wide text-[11px]">
                  Evaluation Error
                </span>
                <pre className="whitespace-pre-wrap font-mono text-[11px] text-red-300">{activeError}</pre>
              </div>
            </div>
            <button onClick={() => setActiveError(null)} className="text-red-400 hover:text-red-200 text-xs px-1">
              ✕
            </button>
          </div>
        )}

        <CollapsibleSection id="workbench.repl.history" title="Execution History" subtitle={`(${history.length})`} defaultOpen>
          {history.length === 0 ? (
            <div className="p-4 text-center text-xs text-green-800 bg-black/40 rounded">
              No evaluation history yet. Click "Evaluate" above to test snippets.
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
                        ? 'bg-green-950/60 border-green-600 text-green-200'
                        : 'bg-black/60 border-green-950/40 text-green-700 hover:bg-green-950/20 hover:text-green-300'
                    }`}
                  >
                    <div className="flex items-center gap-2 overflow-hidden">
                      {isErr ? (
                        <XCircle className="w-3.5 h-3.5 text-rose-500 shrink-0" />
                      ) : entry.noData ? (
                        <AlertCircle className="w-3.5 h-3.5 text-amber-500 shrink-0" />
                      ) : (
                        <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500 shrink-0" />
                      )}
                      <span className="font-mono truncate text-[11px] text-slate-300">
                        {entry.source.split('\n')[0] || '(empty)'}
                      </span>
                    </div>
                    <div className="flex items-center gap-2 text-[10px] shrink-0 text-slate-500">
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
        <CollapsibleSection id="workbench.repl.returnValue" title="Return Value" subtitle="(Runner.Eval output)" defaultOpen>
          <div className="p-3 bg-black/90 rounded border border-green-950/60 text-xs min-h-[70px] overflow-x-auto">
            {activeNoData ? (
              <div className="text-amber-400 text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 shrink-0" />
                <span>
                  No candle data for {symbol} / {timeframe}.
                </span>
              </div>
            ) : ctx.replResult !== null && ctx.replResult !== undefined ? (
              <pre className="text-emerald-400 font-mono text-xs">
                {typeof ctx.replResult === 'object' ? JSON.stringify(ctx.replResult, null, 2) : String(ctx.replResult)}
              </pre>
            ) : (
              <span className="text-slate-600 text-xs italic">
                {activeError ? 'Evaluation terminated with error' : 'No result (expression yielded nil)'}
              </span>
            )}
          </div>
        </CollapsibleSection>
      </div>
    </div>
  );
}
