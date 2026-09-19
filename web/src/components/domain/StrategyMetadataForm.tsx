import React from 'react';
import { ScriptEditorState } from '@/pages/WorkbenchShell';

interface StrategyMetadataFormProps {
  draft: ScriptEditorState;
  setDraft: React.Dispatch<React.SetStateAction<ScriptEditorState>>;
  symbolInput: string;
  setSymbolInput: (v: string) => void;
  onAddSymbol: () => void;
  onRemoveSymbol: (sym: string) => void;
}

// Strategy metadata (name/description/cycle/risk/symbols) - deliberately its
// own component, separate from LuaScriptEditor, so the two can be laid out,
// collapsed, and reasoned about independently (see WorkbenchShell's
// Script/Split/Chart view modes).
export function StrategyMetadataForm({
  draft,
  setDraft,
  symbolInput,
  setSymbolInput,
  onAddSymbol,
  onRemoveSymbol,
}: StrategyMetadataFormProps) {
  return (
    <>
      <div className="grid grid-cols-1 gap-4 text-xs">
        <div className="space-y-1">
          <label className="block text-[11px] text-green-700 uppercase font-semibold">Strategy Name *</label>
          <input
            type="text"
            value={draft.name}
            onChange={(e) => setDraft((prev) => ({ ...prev, name: e.target.value }))}
            placeholder="e.g. BTC Trend Follower"
            className="w-full bg-black border border-green-950 rounded px-2.5 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
          />
        </div>
        <div className="space-y-1">
          <label className="block text-[11px] text-green-700 uppercase font-semibold">Description</label>
          <input
            type="text"
            value={draft.description}
            onChange={(e) => setDraft((prev) => ({ ...prev, description: e.target.value }))}
            placeholder="Brief rationale or indicator note"
            className="w-full bg-black border border-green-950 rounded px-2.5 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
          />
        </div>
        <div className="space-y-1">
          <label className="block text-[11px] text-green-700 uppercase font-semibold">Execution Cycle</label>
          <select
            value={draft.cycleMinutes}
            onChange={(e) =>
              setDraft((prev) => ({ ...prev, cycleMinutes: Number(e.target.value) as typeof prev.cycleMinutes }))
            }
            className="w-full bg-black border border-green-950 rounded px-2.5 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
          >
            {[1, 5, 10, 15, 30, 60].map((c) => (
              <option key={c} value={c}>
                Every {c} minute{c > 1 ? 's' : ''}
              </option>
            ))}
          </select>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <div className="space-y-1">
            <label className="block text-[11px] text-green-700 uppercase font-semibold">Stop Loss %</label>
            <input
              type="number"
              step="0.1"
              value={draft.stopLossPct ?? ''}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, stopLossPct: e.target.value ? Number(e.target.value) : null }))
              }
              placeholder="e.g. 2.5"
              className="w-full bg-black border border-green-950 rounded px-2 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
            />
          </div>
          <div className="space-y-1">
            <label className="block text-[11px] text-green-700 uppercase font-semibold">
              Size ({draft.positionSizing.type === 'pct_capital' ? '%' : '$'})
            </label>
            <input
              type="number"
              step="1"
              value={draft.positionSizing.value ?? ''}
              onChange={(e) =>
                setDraft((prev) => ({
                  ...prev,
                  positionSizing: { ...prev.positionSizing, value: e.target.value ? Number(e.target.value) : null },
                }))
              }
              placeholder="10"
              className="w-full bg-black border border-green-950 rounded px-2 py-1.5 text-green-300 text-xs focus:outline-none focus:border-green-600"
            />
          </div>
        </div>
      </div>

      <div className="mt-3 pt-3 border-t border-green-950 flex flex-wrap items-center gap-2 text-xs">
        <span className="text-[11px] text-green-700 uppercase font-semibold">Monitored Symbols:</span>
        {draft.symbols.map((sym) => (
          <span
            key={sym}
            className="inline-flex items-center gap-1 px-2 py-0.5 bg-green-950/40 border border-green-800/40 text-green-300 rounded text-xs"
          >
            <span>{sym}</span>
            <button onClick={() => onRemoveSymbol(sym)} className="text-green-700 hover:text-rose-400 font-bold ml-0.5">
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
                onAddSymbol();
              }
            }}
            placeholder="+ Add symbol (ETHUSDT)"
            className="bg-black border border-green-950 rounded px-2 py-0.5 text-xs text-green-300 uppercase focus:outline-none focus:border-green-600 w-44"
          />
          <button
            onClick={onAddSymbol}
            className="px-2 py-0.5 bg-green-950 border border-green-800 text-green-400 rounded text-xs hover:bg-green-900"
          >
            Add
          </button>
        </div>

        <span className="ml-auto text-[11px] text-green-700 uppercase font-semibold">Preview Symbol:</span>
        <select
          value={draft.previewSymbol}
          onChange={(e) => setDraft((prev) => ({ ...prev, previewSymbol: e.target.value }))}
          className="bg-black text-green-300 font-bold px-2 py-0.5 rounded border border-green-950 text-xs focus:outline-none focus:border-green-600"
        >
          {draft.symbols.map((sym) => (
            <option key={sym} value={sym}>
              {sym}
            </option>
          ))}
        </select>
      </div>
    </>
  );
}
