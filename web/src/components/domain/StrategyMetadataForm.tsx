import React from 'react';
import { ScriptEditorState } from '@/pages/WorkbenchShell';
import { StrategyStatus } from '@/api/types';
import { useT } from '@/i18n';

interface StrategyMetadataFormProps {
  draft: ScriptEditorState;
  setDraft: React.Dispatch<React.SetStateAction<ScriptEditorState>>;
  symbolInput: string;
  setSymbolInput: (v: string) => void;
  onAddSymbol: () => void;
  onRemoveSymbol: (sym: string) => void;
  // auth-02 §4: every field disabled (a strategy this user can't change).
  readOnly?: boolean;
  // auth-02 §4: non-admins get mode/status selects limited to backtest and
  // testing/disabled (the backend's draft-only guard).
  draftOnlyModeStatus?: boolean;
}

// Non-admin status choices (auth-01 §3 draft guard).
const DRAFT_STATUSES: StrategyStatus[] = ['testing', 'disabled'];

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
  readOnly = false,
  draftOnlyModeStatus = false,
}: StrategyMetadataFormProps) {
  const t = useT();
  return (
    // A disabled fieldset disables every control inside it natively.
    <fieldset disabled={readOnly} className="min-w-0 border-0 p-0 m-0 disabled:opacity-80">
      <div className="grid grid-cols-1 gap-4 text-xs">
        {draftOnlyModeStatus && !readOnly && (
          <div className="grid grid-cols-2 gap-2">
            <div className="space-y-1">
              <label htmlFor="strategy-mode" className="block text-[11px] text-muted-foreground uppercase font-semibold">
                {t('workbench.mode')}
              </label>
              <select
                id="strategy-mode"
                value="backtest"
                disabled
                title={t('users.adminOnly')}
                className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs disabled:opacity-60 disabled:cursor-not-allowed"
              >
                <option value="backtest">{t.enum('modeOption', 'backtest')}</option>
              </select>
            </div>
            <div className="space-y-1">
              <label htmlFor="strategy-status" className="block text-[11px] text-muted-foreground uppercase font-semibold">
                {t('workbench.status')}
              </label>
              <select
                id="strategy-status"
                value={draft.status === 'disabled' ? 'disabled' : 'testing'}
                onChange={(e) => setDraft((prev) => ({ ...prev, status: e.target.value as StrategyStatus }))}
                className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
              >
                {DRAFT_STATUSES.map((o) => (
                  <option key={o} value={o}>
                    {t.enum('statusOption', o)}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}
        <div className="space-y-1">
          <label className="block text-[11px] text-muted-foreground uppercase font-semibold">{t('workbench.strategyName')}</label>
          <input
            type="text"
            value={draft.name}
            onChange={(e) => setDraft((prev) => ({ ...prev, name: e.target.value }))}
            placeholder={t('workbench.namePlaceholder')}
            className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
          />
        </div>
        <div className="space-y-1">
          <label className="block text-[11px] text-muted-foreground uppercase font-semibold">{t('workbench.description')}</label>
          <input
            type="text"
            value={draft.description}
            onChange={(e) => setDraft((prev) => ({ ...prev, description: e.target.value }))}
            placeholder={t('workbench.descriptionPlaceholder')}
            className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
          />
        </div>
        <div className="space-y-1">
          <label className="block text-[11px] text-muted-foreground uppercase font-semibold">{t('workbench.cycle')}</label>
          <select
            value={draft.cycleMinutes}
            onChange={(e) =>
              setDraft((prev) => ({ ...prev, cycleMinutes: Number(e.target.value) as typeof prev.cycleMinutes }))
            }
            className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
          >
            {[1, 5, 15, 30, 60].map((c) => (
              <option key={c} value={c}>
                {t('workbench.everyMinutes', { count: c })}
              </option>
            ))}
          </select>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <div className="space-y-1">
            <label className="block text-[11px] text-muted-foreground uppercase font-semibold">{t('workbench.stopLossPct')}</label>
            <input
              type="number"
              step="0.1"
              value={draft.stopLossPct ?? ''}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, stopLossPct: e.target.value ? Number(e.target.value) : null }))
              }
              placeholder={t('workbench.stopLossPlaceholder')}
              className="w-full bg-background border border-border rounded px-2 py-1.5 text-foreground text-xs focus:outline-none focus:border-primary"
            />
          </div>
          <div className="space-y-1">
            <label className="block text-[11px] text-muted-foreground uppercase font-semibold">
              {t('workbench.size', { unit: draft.positionSizing.type === 'pct_capital' ? '%' : '$' })}
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
        <span className="text-[11px] text-muted-foreground uppercase font-semibold">{t('workbench.monitoredSymbols')}</span>
        {draft.symbols.map((sym) => (
          <span
            key={sym}
            className="inline-flex items-center gap-1 px-2 py-0.5 bg-card/40 border border-border/40 text-foreground rounded text-xs"
          >
            <span>{sym}</span>
            <button onClick={() => onRemoveSymbol(sym)} aria-label={t('workbench.removeSymbol', { symbol: sym })} className="text-muted-foreground hover:text-destructive font-bold ml-0.5">
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
            placeholder={t('workbench.addSymbolPlaceholder')}
            className="bg-background border border-border rounded px-2 py-0.5 text-xs text-foreground uppercase focus:outline-none focus:border-primary w-44"
          />
          <button
            onClick={onAddSymbol}
            className="px-2 py-0.5 bg-card border border-border text-foreground rounded text-xs hover:bg-secondary"
          >
            {t('common.add')}
          </button>
        </div>

        <span className="ml-auto text-[11px] text-muted-foreground uppercase font-semibold">{t('workbench.previewSymbol')}</span>
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
    </fieldset>
  );
}
