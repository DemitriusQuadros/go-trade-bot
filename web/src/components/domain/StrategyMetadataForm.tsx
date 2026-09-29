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
          <label className="block text-[11px] text-muted-foreground uppercase font-semibold">Strategy Name *</label>
          <input
            type="text"
            value={draft.name}
            onChange={(e) => setDraft((prev) => ({ ...prev, name: e.target.value }))}
            placeholder="e.g. BTC Trend Follower"
            className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
          />
        </div>
        <div className="space-y-1">
          <label className="block text-[11px] text-muted-foreground uppercase font-semibold">Description</label>
          <input
            type="text"
            value={draft.description}
            onChange={(e) => setDraft((prev) => ({ ...prev, description: e.target.value }))}
            placeholder="Brief rationale or indicator note"
            className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
          />
        </div>
        <div className="space-y-1">
          <label className="block text-[11px] text-muted-foreground uppercase font-semibold">Execution Cycle</label>
          <select
            value={draft.cycleMinutes}
            onChange={(e) =>
              setDraft((prev) => ({ ...prev, cycleMinutes: Number(e.target.value) as typeof prev.cycleMinutes }))
            }
            className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
          >
            {[1, 5, 15, 30, 60].map((c) => (
              <option key={c} value={c}>
                Every {c} minute{c > 1 ? 's' : ''}
              </option>
            ))}
          </select>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <div className="space-y-1">
            <label className="block text-[11px] text-muted-foreground uppercase font-semibold">Stop Loss %</label>
            <input
              type="number"
              step="0.1"
              value={draft.stopLossPct ?? ''}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, stopLossPct: e.target.value ? Number(e.target.value) : null }))
              }
              placeholder="e.g. 2.5"
              className="w-full bg-background border border-border rounded px-2 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
            />
          </div>
          <div className="space-y-1">
            <label className="block text-[11px] text-muted-foreground uppercase font-semibold">
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
              className="w-full bg-background border border-border rounded px-2 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
            />
          </div>
        </div>
      </div>

      <div className="mt-3 pt-3 border-t border-border flex flex-wrap items-center gap-2 text-xs">
        <span className="text-[11px] text-muted-foreground uppercase font-semibold">Monitored Symbols:</span>
        {draft.symbols.map((sym) => (
          <span
            key={sym}
            className="inline-flex items-center gap-1 px-2 py-0.5 bg-card/40 border border-border/40 text-foreground rounded text-xs"
          >
            <span>{sym}</span>
            <button onClick={() => onRemoveSymbol(sym)} className="text-muted-foreground hover:text-destructive font-bold ml-0.5">
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
            className="bg-background border border-border rounded px-2 py-0.5 text-xs text-foreground uppercase focus:outline-none focus:border-primary w-44"
          />
          <button
            onClick={onAddSymbol}
            className="px-2 py-0.5 bg-card border border-border text-foreground rounded text-xs hover:bg-secondary"
          >
            Add
          </button>
        </div>

        <span className="ml-auto text-[11px] text-muted-foreground uppercase font-semibold">Preview Symbol:</span>
        <select
          value={draft.previewSymbol}
          onChange={(e) => setDraft((prev) => ({ ...prev, previewSymbol: e.target.value }))}
          className="bg-background text-foreground font-bold px-2 py-0.5 rounded border border-border text-xs focus:outline-none focus:border-primary"
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
