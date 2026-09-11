import React, { useState, useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import CodeMirror from '@uiw/react-codemirror';
import { StreamLanguage } from '@codemirror/language';
import { lua } from '@codemirror/legacy-modes/mode/lua';
import { luaEditorDarkTheme } from '@/lib/codeMirrorTheme';
import { luaAutocompletion } from '@/lib/luaCompletions';
import {
  useStrategy,
  useCreateStrategy,
  useUpdateStrategy,
  useFastRerun,
} from '@/hooks/queries';
import {
  Strategy,
  StrategyStatus,
  StrategyMode,
  TraceRecord,
  StrategyCreateRequest,
  StrategyUpdateRequest,
} from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { ExecutionTraceChart } from '@/components/charts/ExecutionTraceChart';
import { TraceAnnotationPanel } from '@/components/charts/TraceAnnotationPanel';
import { LoadingScreen } from '@/components/ui/Spinner';
import {
  ArrowLeft,
  Play,
  Save,
  Rocket,
  CheckCircle2,
  AlertCircle,
  Code2,
  Sliders,
  History,
  RotateCcw,
} from 'lucide-react';

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
}

const DEFAULT_LUA_TEMPLATE = `-- Lua Strategy Script (gopher-lua sandboxed)
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

  return {
    strategyId: strat.id,
    name: strat.name || '',
    description: strat.description || '',
    symbols: strat.monitored_symbols || ['BTCUSDT'],
    cycleMinutes: (CYCLE_OPTIONS.includes(strat.cycle as any) ? strat.cycle : 15) as CycleMinutes,
    source: strat.script_source || DEFAULT_LUA_TEMPLATE,
    stopLossPct: stopLoss,
    positionSizing: {
      type: sizing.type === 'fixed_amount' ? 'fixed_amount' : 'pct_capital',
      value: typeof sizing.value === 'number' ? sizing.value : 10,
    },
    status: strat.status || 'disabled',
    mode: strat.mode || 'dryrun',
  };
}

function toStrategyConfiguration(state: ScriptEditorState): Record<string, unknown> {
  return {
    stop_loss_pct: state.stopLossPct,
    position_sizing: {
      type: state.positionSizing.type,
      value: state.positionSizing.value,
    },
  };
}

interface ScriptEditorProps {
  mode: 'create' | 'edit';
}

export function ScriptEditor({ mode }: ScriptEditorProps) {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const strategyId = id ? Number(id) : null;

  const isEdit = mode === 'edit';

  const { data: existingStrategy, isLoading: isStrategyLoading } = useStrategy(
    strategyId || 0
  );

  const createMutation = useCreateStrategy();
  const updateMutation = useUpdateStrategy(strategyId || 0);
  const fastRerunMutation = useFastRerun();

  const [form, setForm] = useState<ScriptEditorState>({
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
  });

  const [symbolInput, setSymbolInput] = useState('');
  const [selectedPreviewSymbol, setSelectedPreviewSymbol] = useState('BTCUSDT');

  // Preview & Error states
  const [previewTrace, setPreviewTrace] = useState<TraceRecord[]>([]);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [selectedTraceRecord, setSelectedTraceRecord] = useState<TraceRecord | null>(null);

  // Status message
  const [actionMessage, setActionMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  useEffect(() => {
    if (isEdit && existingStrategy) {
      const state = toEditorState(existingStrategy);
      setForm(state);
      if (state.symbols.length > 0) {
        setSelectedPreviewSymbol(state.symbols[0]);
      }
    }
  }, [isEdit, existingStrategy]);

  const handleAddSymbol = () => {
    const s = symbolInput.trim().toUpperCase();
    if (!s) return;
    if (!form.symbols.includes(s)) {
      setForm((prev) => ({ ...prev, symbols: [...prev.symbols, s] }));
      if (form.symbols.length === 0) setSelectedPreviewSymbol(s);
    }
    setSymbolInput('');
  };

  const handleRemoveSymbol = (sym: string) => {
    setForm((prev) => ({
      ...prev,
      symbols: prev.symbols.filter((s) => s !== sym),
    }));
    if (selectedPreviewSymbol === sym && form.symbols.length > 1) {
      setSelectedPreviewSymbol(form.symbols.find((s) => s !== sym) || 'BTCUSDT');
    }
  };

  // Quick Preview action (POST /api/script/fast-rerun)
  const handleQuickPreview = async () => {
    setPreviewError(null);
    setSelectedTraceRecord(null);
    setActionMessage(null);

    const targetSymbol = selectedPreviewSymbol || form.symbols[0] || 'BTCUSDT';
    try {
      const res = await fastRerunMutation.mutateAsync({
        strategy_id: form.strategyId || undefined,
        source: form.source,
        symbol: targetSymbol,
        timeframe: `${form.cycleMinutes}m`,
        end_time: null, // live "now" window
        window_candles: 100,
      });

      if (res.error) {
        setPreviewError(res.error);
        setPreviewTrace([]);
      } else if (res.data_available === false) {
        setPreviewTrace([]);
        setActionMessage({
          type: 'error',
          text: `No candle data available for ${targetSymbol} / ${form.cycleMinutes}m. Check the symbol is valid and the live feed is connected, or import historical candles for this pair.`,
        });
      } else {
        setPreviewTrace(res.trace || []);
        if (!res.trace || res.trace.length === 0) {
          setActionMessage({
            type: 'success',
            text: 'Preview evaluated cleanly (no signals over this window).',
          });
        }
      }
    } catch (err: any) {
      setPreviewError(err.message || 'Quick preview request failed');
      setPreviewTrace([]);
    }
  };

  // Save actions
  const saveStrategy = async (statusOverride?: StrategyStatus): Promise<number | null> => {
    setActionMessage(null);

    if (!form.name.trim()) {
      setActionMessage({ type: 'error', text: 'Strategy name is required' });
      return null;
    }
    if (form.symbols.length === 0) {
      setActionMessage({ type: 'error', text: 'At least one monitored symbol is required' });
      return null;
    }

    const finalStatus = statusOverride !== undefined ? statusOverride : form.status;
    const config = toStrategyConfiguration(form);

    try {
      if (isEdit && form.strategyId) {
        const updateReq: StrategyUpdateRequest = {
          name: form.name.trim(),
          description: form.description.trim(),
          strategy_name: 'script',
          status: finalStatus,
          mode: form.mode,
          monitored_symbols: form.symbols,
          cycle: form.cycleMinutes,
          configuration: config,
          script_source: form.source,
        };
        await updateMutation.mutateAsync(updateReq);
        setActionMessage({ type: 'success', text: `Strategy #${form.strategyId} saved successfully!` });
        return form.strategyId;
      } else {
        const createReq: StrategyCreateRequest = {
          name: form.name.trim(),
          description: form.description.trim(),
          strategy_name: 'script',
          status: finalStatus,
          mode: 'dryrun',
          monitored_symbols: form.symbols,
          cycle: form.cycleMinutes,
          configuration: config,
          script_source: form.source,
        };
        const created = await createMutation.mutateAsync(createReq);
        setActionMessage({ type: 'success', text: `Strategy created successfully with ID #${created.id}!` });
        return created.id;
      }
    } catch (err: any) {
      setActionMessage({ type: 'error', text: err.message || 'Failed to save strategy' });
      return null;
    }
  };

  // Run Full Backtest: saves as disabled draft first, then navigates
  const handleRunFullBacktest = async () => {
    const savedId = await saveStrategy('disabled');
    if (savedId) {
      const sym = selectedPreviewSymbol || form.symbols[0] || 'BTCUSDT';
      navigate(`/backtest?strategy_id=${savedId}&symbol=${encodeURIComponent(sym)}`);
    }
  };

  if (isEdit && isStrategyLoading) {
    return <LoadingScreen message={`Loading strategy #${strategyId}...`} />;
  }

  return (
    <div className="container-custom space-y-5 font-mono">
      {/* Header */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4 border-b border-green-950 pb-4">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate('/strategies')}
            className="p-1.5 text-green-700 hover:text-green-400 hover:bg-green-950/30 rounded"
          >
            <ArrowLeft className="w-5 h-5" />
          </button>
          <div>
            <div className="flex items-center gap-2">
              <Code2 className="w-5 h-5 text-green-500" />
              <h1 className="text-xl font-bold text-green-500">
                {isEdit ? `Edit Script Strategy #${form.strategyId}` : 'New Script Strategy'}
              </h1>
            </div>
            <p className="text-xs text-green-700 mt-0.5">
              Author full trading algorithms in Lua with built-in telemetry, indicator closures, and fast replay.
            </p>
          </div>
        </div>

        {/* Primary Action Buttons */}
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <button
            onClick={handleQuickPreview}
            disabled={fastRerunMutation.isPending}
            className="btn btn-secondary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
          >
            <Play className="w-3.5 h-3.5 fill-current text-green-400" />
            <span>{fastRerunMutation.isPending ? 'Previewing...' : 'Quick Preview'}</span>
          </button>

          <button
            onClick={handleRunFullBacktest}
            disabled={createMutation.isPending || updateMutation.isPending}
            className="btn btn-secondary text-xs flex items-center gap-1.5 px-3 py-1.5"
            title="Save as draft and launch full historical backtest"
          >
            <Rocket className="w-3.5 h-3.5 text-purple-400" />
            <span>Run Full Backtest</span>
          </button>

          {isEdit ? (
            <button
              onClick={() => saveStrategy()}
              disabled={updateMutation.isPending}
              className="btn btn-primary text-xs flex items-center gap-1.5 px-4 py-1.5 font-bold"
            >
              <Save className="w-3.5 h-3.5" />
              <span>{updateMutation.isPending ? 'Saving...' : 'Save Changes'}</span>
            </button>
          ) : (
            <>
              <button
                onClick={() => saveStrategy('disabled')}
                disabled={createMutation.isPending}
                className="btn btn-secondary text-xs flex items-center gap-1.5 px-3 py-1.5"
              >
                <Save className="w-3.5 h-3.5" />
                <span>Save as Draft</span>
              </button>
              <button
                onClick={() => saveStrategy('productive')}
                disabled={createMutation.isPending}
                className="btn btn-primary text-xs flex items-center gap-1.5 px-4 py-1.5 font-bold"
              >
                <CheckCircle2 className="w-3.5 h-3.5" />
                <span>Save & Enable</span>
              </button>
            </>
          )}
        </div>
      </div>

      {/* Action status message banner */}
      {actionMessage && (
        <div
          className={`p-3 rounded-lg text-xs flex items-center justify-between border ${
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

      {/* Raw Error Banner (from fast-rerun) */}
      {previewError && (
        <div
          role="alert"
          className="flex items-start justify-between gap-3 p-3 bg-red-950/80 border border-red-800 text-red-300 rounded-lg text-xs"
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
          <button
            onClick={() => setPreviewError(null)}
            className="text-red-400 hover:text-red-200 text-xs px-1"
          >
            ✕
          </button>
        </div>
      )}

      {/* Configuration Scope Row */}
      <Card className="p-4 border border-green-900/40 bg-black">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 text-xs">
          {/* Strategy Name */}
          <div className="space-y-1">
            <label className="block text-[11px] text-green-700 uppercase font-semibold">
              Strategy Name *
            </label>
            <input
              type="text"
              value={form.name}
              onChange={(e) => setForm((prev) => ({ ...prev, name: e.target.value }))}
              placeholder="e.g. BTC Trend Follower"
              className="w-full bg-black border border-green-950 rounded px-2.5 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
            />
          </div>

          {/* Description */}
          <div className="space-y-1">
            <label className="block text-[11px] text-green-700 uppercase font-semibold">
              Description
            </label>
            <input
              type="text"
              value={form.description}
              onChange={(e) => setForm((prev) => ({ ...prev, description: e.target.value }))}
              placeholder="Brief rationale or indicator note"
              className="w-full bg-black border border-green-950 rounded px-2.5 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
            />
          </div>

          {/* Cycle Minutes */}
          <div className="space-y-1">
            <label className="block text-[11px] text-green-700 uppercase font-semibold">
              Execution Cycle
            </label>
            <select
              value={form.cycleMinutes}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  cycleMinutes: Number(e.target.value) as CycleMinutes,
                }))
              }
              className="w-full bg-black border border-green-950 rounded px-2.5 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
            >
              {CYCLE_OPTIONS.map((c) => (
                <option key={c} value={c}>
                  Every {c} minute{c > 1 ? 's' : ''}
                </option>
              ))}
            </select>
          </div>

          {/* Risk: Stop Loss & Sizing */}
          <div className="grid grid-cols-2 gap-2">
            <div className="space-y-1">
              <label className="block text-[11px] text-green-700 uppercase font-semibold">
                Stop Loss %
              </label>
              <input
                type="number"
                step="0.1"
                value={form.stopLossPct ?? ''}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    stopLossPct: e.target.value ? Number(e.target.value) : null,
                  }))
                }
                placeholder="e.g. 2.5"
                className="w-full bg-black border border-green-950 rounded px-2 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
              />
            </div>
            <div className="space-y-1">
              <label className="block text-[11px] text-green-700 uppercase font-semibold">
                Size ({form.positionSizing.type === 'pct_capital' ? '%' : '$'})
              </label>
              <input
                type="number"
                step="1"
                value={form.positionSizing.value ?? ''}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    positionSizing: {
                      ...prev.positionSizing,
                      value: e.target.value ? Number(e.target.value) : null,
                    },
                  }))
                }
                placeholder="10"
                className="w-full bg-black border border-green-950 rounded px-2 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
              />
            </div>
          </div>
        </div>

        {/* Symbols Bar */}
        <div className="mt-3 pt-3 border-t border-green-950 flex flex-wrap items-center gap-2 text-xs">
          <span className="text-[11px] text-green-700 uppercase font-semibold">
            Monitored Symbols:
          </span>
          {form.symbols.map((sym) => (
            <span
              key={sym}
              className="inline-flex items-center gap-1 px-2 py-0.5 bg-green-950/40 border border-green-800/40 text-green-300 rounded text-xs"
            >
              <span>{sym}</span>
              <button
                onClick={() => handleRemoveSymbol(sym)}
                className="text-green-700 hover:text-rose-400 font-bold ml-0.5"
              >
                ×
              </button>
            </span>
          ))}

          <div className="flex items-center gap-1">
            <input
              type="text"
              value={symbolInput}
              onChange={(e) => setSymbolInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  handleAddSymbol();
                }
              }}
              placeholder="+ Add symbol (ETHUSDT)"
              className="bg-black border border-green-950 rounded px-2 py-0.5 text-xs text-green-300 uppercase focus:outline-none focus:border-green-600 w-44"
            />
            <button
              onClick={handleAddSymbol}
              className="px-2 py-0.5 bg-green-950 border border-green-800 text-green-400 rounded text-xs hover:bg-green-900"
            >
              Add
            </button>
          </div>
        </div>
      </Card>

      {/* Main Split Layout: CodeMirror Editor on Left, Quick Preview on Right */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-5">
        {/* Editor (6 cols) */}
        <div className="lg:col-span-6 space-y-2">
          <Card className="p-0 overflow-hidden border border-green-900/40 bg-black">
            <div className="px-3 py-2 bg-green-950/40 border-b border-green-900/40 flex items-center justify-between text-xs">
              <div className="flex items-center gap-2">
                <span className="text-green-400 font-semibold text-[11px] uppercase tracking-wider">
                  Lua Source Code
                </span>
                <span className="text-[10px] text-green-700">(auto-sandboxed, 500ms timeout)</span>
              </div>
              <button
                onClick={() => setForm((prev) => ({ ...prev, source: DEFAULT_LUA_TEMPLATE }))}
                className="text-[11px] text-green-700 hover:text-green-400 flex items-center gap-1"
                title="Reset to template"
              >
                <RotateCcw className="w-3 h-3" />
                <span>Reset</span>
              </button>
            </div>

            <div className="text-sm bg-black/95">
              <CodeMirror
                value={form.source}
                height="500px"
                theme="dark"
                extensions={[StreamLanguage.define(lua), luaEditorDarkTheme, luaAutocompletion]}
                onChange={(val) => setForm((prev) => ({ ...prev, source: val }))}
                className="font-mono text-xs"
                basicSetup={{
                  lineNumbers: true,
                  highlightActiveLineGutter: true,
                  foldGutter: true,
                }}
              />
            </div>
          </Card>
        </div>

        {/* Quick Preview & Trace (6 cols) */}
        <div className="lg:col-span-6 space-y-4">
          <Card className="border border-green-900/40 bg-black">
            <CardHeader
              title="Quick Preview Telemetry"
              subtitle="Fast execution across 100 recent candles without persisting"
              action={
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-green-700 text-[11px]">Symbol:</span>
                  <select
                    value={selectedPreviewSymbol}
                    onChange={(e) => setSelectedPreviewSymbol(e.target.value)}
                    className="bg-black text-green-300 font-bold px-2 py-0.5 rounded border border-green-950 text-xs focus:outline-none focus:border-green-600"
                  >
                    {form.symbols.map((sym) => (
                      <option key={sym} value={sym}>
                        {sym}
                      </option>
                    ))}
                  </select>
                </div>
              }
            />

            {previewTrace.length > 0 ? (
              <div className="space-y-3">
                <ExecutionTraceChart
                  trace={previewTrace}
                  height={320}
                  onScrub={setSelectedTraceRecord}
                />
                <TraceAnnotationPanel record={selectedTraceRecord} />
              </div>
            ) : (
              <div className="p-12 text-center text-xs text-green-800 bg-black/40 rounded-lg space-y-2">
                <p>Click "Quick Preview" to simulate execution against the latest 100 market candles.</p>
                <p className="text-[11px] text-green-900">
                  Observe buy/sell marker positioning, indicator curves, and debug logs candle-by-candle.
                </p>
              </div>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}
