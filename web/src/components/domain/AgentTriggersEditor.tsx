import React, { forwardRef } from 'react';
import { AlertCircle, Activity, Link2, Plus, Trash2, Zap } from 'lucide-react';
import { Agent, ChainTrigger, EventTrigger, MarketRule } from '@/api/types';
import { HelpTooltip } from '@/components/ui/HelpTooltip';
import {
  CHAIN_ON_OPTIONS,
  EVENT_CATALOG,
  MARKET_DEFAULT_COOLDOWN,
  MARKET_KINDS,
  MARKET_MAX_WINDOW,
  MARKET_MIN_COOLDOWN,
  MAX_CHAIN_DEPTH,
  WINDOW_PRESETS,
  describeMarketRule,
  newRuleId,
} from '@/lib/triggers';

// Phase C trigger builder (C-02 §2): strategy events, market watches and
// chained-from-agent triggers, rendered inside the editor's Schedule card.
//
// Draft state keeps every number as the string the user typed (same
// approach as the editor's budget field) so "", "0." or "1.5" never get
// coerced mid-edit; toTriggerPayload converts on save. Market rule ids are
// generated once on add and carried verbatim - never regenerated.

export interface EventDraft {
  enabled: boolean;
  cooldown: string;
  threshold: string; // drawdown only
  window: string; // drawdown / no_signal
}

export interface MarketDraft {
  id: string;
  symbol: string;
  kind: string;
  window: string;
  threshold: string; // threshold_pct or multiplier, depending on kind
  cooldown: string;
}

export interface ChainDraft {
  key: string; // client-only React key, never sent
  agent_id: number; // 0 = not selected yet
  on: string;
}

export interface TriggersDraft {
  events: Record<string, EventDraft>;
  // Event types the API returned that this UI doesn't know - passed through
  // untouched so saving never drops them.
  unknownEvents: EventTrigger[];
  market: MarketDraft[];
  chain: ChainDraft[];
}

const numStr = (v: number | undefined | null) => (v == null || v === 0 ? '' : String(v));

export function draftFromTriggers(t: {
  events: EventTrigger[];
  market: MarketRule[];
  chain_from: ChainTrigger[];
}): TriggersDraft {
  const events: Record<string, EventDraft> = {};
  for (const e of EVENT_CATALOG) {
    events[e.type] = { enabled: false, cooldown: '', threshold: '', window: '' };
  }
  const unknownEvents: EventTrigger[] = [];
  for (const e of t.events) {
    if (!events[e.type]) {
      unknownEvents.push(e);
      continue;
    }
    events[e.type] = {
      enabled: true,
      cooldown: numStr(e.cooldown_minutes),
      threshold: numStr(e.threshold_pct),
      window: numStr(e.window_hours),
    };
  }
  return {
    events,
    unknownEvents,
    market: t.market.map((m) => ({
      id: m.id,
      symbol: m.symbol,
      kind: m.kind,
      window: numStr(m.window_minutes),
      threshold: numStr(m.kind === 'volatility_spike' ? m.multiplier : m.threshold_pct),
      cooldown: numStr(m.cooldown_minutes),
    })),
    chain: t.chain_from.map((c, i) => ({ key: `c${i}-${c.agent_id}-${c.on}`, agent_id: c.agent_id, on: c.on })),
  };
}

const toNum = (s: string): number | undefined => {
  if (s.trim() === '') return undefined;
  const n = Number(s);
  return Number.isFinite(n) ? n : undefined;
};

export function toTriggerPayload(d: TriggersDraft): {
  events: EventTrigger[];
  market: MarketRule[];
  chain_from: ChainTrigger[];
} {
  const events: EventTrigger[] = [];
  for (const cat of EVENT_CATALOG) {
    const e = d.events[cat.type];
    if (!e?.enabled) continue;
    const out: EventTrigger = { type: cat.type };
    const cd = toNum(e.cooldown);
    if (cd != null) out.cooldown_minutes = Math.floor(cd);
    if (cat.needsThreshold) {
      const th = toNum(e.threshold);
      if (th != null) out.threshold_pct = th;
    }
    if (cat.needsWindow) {
      const w = toNum(e.window);
      if (w != null) out.window_hours = Math.floor(w);
    }
    events.push(out);
  }
  events.push(...d.unknownEvents);

  const market: MarketRule[] = d.market.map((m) => {
    const rule: MarketRule = {
      id: m.id,
      symbol: m.symbol.trim().toUpperCase(),
      kind: m.kind,
      window_minutes: Math.floor(toNum(m.window) ?? 0),
      cooldown_minutes: Math.floor(toNum(m.cooldown) ?? MARKET_DEFAULT_COOLDOWN),
    };
    const th = toNum(m.threshold);
    if (th != null) {
      if (m.kind === 'volatility_spike') rule.multiplier = th;
      else rule.threshold_pct = th;
    }
    return rule;
  });

  const chain_from: ChainTrigger[] = d.chain.map((c) => ({ agent_id: c.agent_id, on: c.on }));
  return { events, market, chain_from };
}

// Convenience checks only (C-02 §2) - the backend's 400 is authoritative.
export interface TriggerErrors {
  events: Record<string, string>;
  market: Record<string, string>; // by rule id
  chain: Record<string, string>; // by chain row key
  count: number;
}

export function validateTriggers(d: TriggersDraft): TriggerErrors {
  const errs: TriggerErrors = { events: {}, market: {}, chain: {}, count: 0 };
  const put = (bucket: Record<string, string>, k: string, msg: string) => {
    if (!bucket[k]) {
      bucket[k] = msg;
      errs.count++;
    }
  };
  for (const cat of EVENT_CATALOG) {
    const e = d.events[cat.type];
    if (!e?.enabled) continue;
    const cd = toNum(e.cooldown);
    if (e.cooldown.trim() && (cd == null || cd < 0)) put(errs.events, cat.type, 'Cooldown must be 0 or more minutes.');
    if (cat.needsThreshold) {
      const th = toNum(e.threshold);
      if (th == null || th <= 0) put(errs.events, cat.type, 'Threshold must be greater than 0%.');
    }
    if (cat.needsWindow) {
      const w = toNum(e.window);
      if (cat.needsWindow === 'required' && (w == null || w <= 0)) put(errs.events, cat.type, 'Window (hours) is required.');
      else if (e.window.trim() && (w == null || w <= 0)) put(errs.events, cat.type, 'Window must be greater than 0 hours.');
    }
  }
  for (const m of d.market) {
    if (!m.symbol.trim()) put(errs.market, m.id, 'Symbol is required.');
    const w = toNum(m.window);
    if (w == null || w < 1 || w > MARKET_MAX_WINDOW) put(errs.market, m.id, `Window must be 1-${MARKET_MAX_WINDOW} minutes.`);
    const th = toNum(m.threshold);
    if (th == null || th <= 0)
      put(errs.market, m.id, m.kind === 'volatility_spike' ? 'Multiplier must be greater than 0.' : 'Threshold must be greater than 0%.');
    const cd = toNum(m.cooldown);
    if (m.cooldown.trim() && (cd == null || cd < MARKET_MIN_COOLDOWN))
      put(errs.market, m.id, `Cooldown must be at least ${MARKET_MIN_COOLDOWN} minutes.`);
  }
  const seen = new Set<string>();
  for (const c of d.chain) {
    if (!c.agent_id) put(errs.chain, c.key, 'Pick a source agent.');
    const k = `${c.agent_id}:${c.on}`;
    if (c.agent_id && seen.has(k)) put(errs.chain, c.key, 'Duplicate of another chain row.');
    seen.add(k);
  }
  return errs;
}

const SUB_TITLE = 'text-xs font-semibold text-foreground flex items-center gap-1.5';
const HELP = 'text-[11px] text-muted-foreground';
const SMALL_BTN =
  'bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs px-2.5 py-1 flex items-center gap-1 disabled:opacity-40';
const FIELD_ERR = 'text-[11px] text-destructive flex items-center gap-1';
const REMOVE_BTN = 'p-1.5 rounded text-muted-foreground hover:text-destructive hover:bg-destructive/10';

interface AgentTriggersEditorProps {
  draft: TriggersDraft;
  onChange: (next: TriggersDraft) => void;
  errors: TriggerErrors | null; // null until the user first tries to save
  chainError: string | null; // the backend's 400 cycle message, shown inline
  currentAgentId: number; // 0 for a new agent
  agents: Agent[];
  symbolSuggestions: string[];
}

export const AgentTriggersEditor = forwardRef<HTMLElement, AgentTriggersEditorProps>(function AgentTriggersEditor(
  { draft, onChange, errors, chainError, currentAgentId, agents, symbolSuggestions },
  chainRef,
) {
  const setEvent = (type: string, patch: Partial<EventDraft>) =>
    onChange({ ...draft, events: { ...draft.events, [type]: { ...draft.events[type], ...patch } } });
  const setRule = (id: string, patch: Partial<MarketDraft>) =>
    onChange({ ...draft, market: draft.market.map((m) => (m.id === id ? { ...m, ...patch } : m)) });
  const setChain = (key: string, patch: Partial<ChainDraft>) =>
    onChange({ ...draft, chain: draft.chain.map((c) => (c.key === key ? { ...c, ...patch } : c)) });

  const addRule = () =>
    onChange({
      ...draft,
      market: [
        ...draft.market,
        {
          id: newRuleId(),
          symbol: symbolSuggestions[0] ?? '',
          kind: 'pct_move',
          window: '60',
          threshold: '',
          cooldown: String(MARKET_DEFAULT_COOLDOWN),
        },
      ],
    });
  const addChain = () =>
    onChange({ ...draft, chain: [...draft.chain, { key: newRuleId(), agent_id: 0, on: 'report' }] });

  const sourceAgents = agents.filter((a) => a.id !== currentAgentId);
  const datalistId = 'agent-trigger-symbols';

  return (
    <div className="space-y-5">
      {/* Strategy events */}
      <section aria-labelledby="trig-events" className="pt-3 border-t border-border space-y-2">
        <h3 id="trig-events" className={SUB_TITLE}>
          <Zap className="w-3.5 h-3.5 text-muted-foreground" /> Strategy events
        </h3>
        <p className={HELP}>Events only fire for strategies this agent is attached to.</p>
        <div className="rounded-lg border border-border divide-y divide-border">
          {EVENT_CATALOG.map((cat) => {
            const e = draft.events[cat.type];
            const err = errors?.events[cat.type];
            const idBase = `evt-${cat.type.replace('.', '-')}`;
            return (
              <div key={cat.type} className="px-3 py-2 space-y-2">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                  <label className="flex items-center gap-2 cursor-pointer min-w-[16rem] flex-1">
                    <input
                      type="checkbox"
                      checked={e.enabled}
                      onChange={() => setEvent(cat.type, { enabled: !e.enabled })}
                      className="w-4 h-4 rounded border-border focus:ring-ring focus:ring-offset-background"
                    />
                    <span className="text-xs text-foreground">{cat.label}</span>
                    <span className="text-[10px] font-mono text-muted-foreground">{cat.type}</span>
                  </label>
                  <HelpTooltip label={`About ${cat.type}`}>{cat.help}</HelpTooltip>
                  {e.enabled && (
                    <div className="flex flex-wrap items-center gap-3 ml-auto">
                      {cat.needsThreshold && (
                        <NumField
                          id={`${idBase}-threshold`}
                          label="Threshold %"
                          value={e.threshold}
                          onChange={(v) => setEvent(cat.type, { threshold: v })}
                          placeholder="e.g. 5"
                          step="0.1"
                        />
                      )}
                      {cat.needsWindow && (
                        <NumField
                          id={`${idBase}-window`}
                          label={cat.needsWindow === 'required' ? 'Window (h)*' : 'Window (h)'}
                          value={e.window}
                          onChange={(v) => setEvent(cat.type, { window: v })}
                          placeholder={cat.defaultWindowHours ? String(cat.defaultWindowHours) : 'required'}
                          step="1"
                        />
                      )}
                      <NumField
                        id={`${idBase}-cooldown`}
                        label="Cooldown (min)"
                        value={e.cooldown}
                        onChange={(v) => setEvent(cat.type, { cooldown: v })}
                        placeholder={String(cat.defaultCooldown)}
                        step="1"
                      />
                    </div>
                  )}
                </div>
                {e.enabled && err && (
                  <p className={FIELD_ERR} role="alert">
                    <AlertCircle className="w-3 h-3" /> {err}
                  </p>
                )}
              </div>
            );
          })}
        </div>
        {draft.unknownEvents.length > 0 && (
          <p className={HELP}>
            Also kept as-is (not editable here):{' '}
            <span className="font-mono">{draft.unknownEvents.map((e) => e.type).join(', ')}</span>
          </p>
        )}
      </section>

      {/* Market watches */}
      <section aria-labelledby="trig-market" className="pt-3 border-t border-border space-y-2">
        <div className="flex items-center justify-between gap-2">
          <h3 id="trig-market" className={SUB_TITLE}>
            <Activity className="w-3.5 h-3.5 text-muted-foreground" /> Market watches
            <HelpTooltip label="About market watches">
              Watches closed 1-minute candles. A symbol none of this agent's strategies trade is allowed (e.g. a macro
              BTC watch). Cooldown is at least {MARKET_MIN_COOLDOWN} minutes.
            </HelpTooltip>
          </h3>
          <button type="button" onClick={addRule} className={SMALL_BTN}>
            <Plus className="w-3.5 h-3.5" /> Add watch
          </button>
        </div>
        <datalist id={datalistId}>
          {symbolSuggestions.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        {draft.market.length === 0 ? (
          <p className={HELP}>No market watches.</p>
        ) : (
          <ul className="space-y-2">
            {draft.market.map((m, i) => {
              const err = errors?.market[m.id];
              const isVol = m.kind === 'volatility_spike';
              const knownKind = MARKET_KINDS.some((k) => k.kind === m.kind);
              const idBase = `mkt-${i}`;
              return (
                <li key={m.id} className="rounded-lg border border-border p-3 space-y-2">
                  <div className="flex flex-wrap items-end gap-3">
                    <div className="flex flex-col gap-1">
                      <label htmlFor={`${idBase}-symbol`} className={HELP}>
                        Symbol
                      </label>
                      <input
                        id={`${idBase}-symbol`}
                        type="text"
                        list={datalistId}
                        value={m.symbol}
                        onChange={(e) => setRule(m.id, { symbol: e.target.value.toUpperCase().replace(/\s/g, '') })}
                        placeholder="BTCUSDT"
                        className="form-input text-xs font-mono py-1.5 w-32"
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label htmlFor={`${idBase}-kind`} className={HELP}>
                        Kind
                      </label>
                      <select
                        id={`${idBase}-kind`}
                        value={m.kind}
                        onChange={(e) => setRule(m.id, { kind: e.target.value })}
                        className="form-select text-xs py-1.5"
                      >
                        {MARKET_KINDS.map((k) => (
                          <option key={k.kind} value={k.kind}>
                            {k.label}
                          </option>
                        ))}
                        {!knownKind && <option value={m.kind}>{m.kind}</option>}
                      </select>
                    </div>
                    <div className="flex flex-col gap-1">
                      <label htmlFor={`${idBase}-window`} className={HELP}>
                        Window (min)
                      </label>
                      <div className="flex items-center gap-1">
                        <input
                          id={`${idBase}-window`}
                          type="number"
                          min="1"
                          max={MARKET_MAX_WINDOW}
                          step="1"
                          value={m.window}
                          onChange={(e) => setRule(m.id, { window: e.target.value })}
                          className="form-input text-xs font-mono py-1.5 w-20"
                        />
                        {WINDOW_PRESETS.map((p) => (
                          <button
                            key={p.label}
                            type="button"
                            onClick={() => setRule(m.id, { window: String(p.minutes) })}
                            aria-pressed={m.window === String(p.minutes)}
                            className={`rounded border text-[11px] px-1.5 py-1 ${
                              m.window === String(p.minutes)
                                ? 'bg-primary/15 text-primary border-primary/40'
                                : 'bg-secondary hover:bg-accent text-foreground border-border'
                            }`}
                          >
                            {p.label}
                          </button>
                        ))}
                      </div>
                    </div>
                    <NumField
                      id={`${idBase}-threshold`}
                      label={isVol ? 'Multiplier ×' : 'Threshold %'}
                      value={m.threshold}
                      onChange={(v) => setRule(m.id, { threshold: v })}
                      placeholder={isVol ? 'e.g. 2' : 'e.g. 3'}
                      step="0.1"
                      stacked
                    />
                    <NumField
                      id={`${idBase}-cooldown`}
                      label="Cooldown (min)"
                      value={m.cooldown}
                      onChange={(v) => setRule(m.id, { cooldown: v })}
                      placeholder={String(MARKET_DEFAULT_COOLDOWN)}
                      step="1"
                      min={MARKET_MIN_COOLDOWN}
                      stacked
                    />
                    <button
                      type="button"
                      onClick={() => onChange({ ...draft, market: draft.market.filter((x) => x.id !== m.id) })}
                      aria-label={`Remove market watch ${m.symbol || i + 1}`}
                      className={`${REMOVE_BTN} ml-auto`}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  <p className="text-[11px] text-foreground">
                    {describeMarketRule({
                      symbol: m.symbol,
                      kind: m.kind,
                      window_minutes: Number(m.window) || 0,
                      threshold_pct: isVol ? undefined : Number(m.threshold) || undefined,
                      multiplier: isVol ? Number(m.threshold) || undefined : undefined,
                      cooldown_minutes: Number(m.cooldown) || MARKET_DEFAULT_COOLDOWN,
                    })}
                  </p>
                  {err && (
                    <p className={FIELD_ERR} role="alert">
                      <AlertCircle className="w-3 h-3" /> {err}
                    </p>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>

      {/* Chained from agents */}
      <section ref={chainRef} aria-labelledby="trig-chain" className="pt-3 border-t border-border space-y-2 scroll-mt-4">
        <div className="flex items-center justify-between gap-2">
          <h3 id="trig-chain" className={SUB_TITLE}>
            <Link2 className="w-3.5 h-3.5 text-muted-foreground" /> Chained from agents
          </h3>
          <button type="button" onClick={addChain} disabled={sourceAgents.length === 0} className={SMALL_BTN}>
            <Plus className="w-3.5 h-3.5" /> Add chain
          </button>
        </div>
        <p className={HELP}>
          Chains stop after {MAX_CHAIN_DEPTH} hops and never revisit an agent. The chained run reads the source run's
          reports.
        </p>
        {chainError && (
          <div role="alert" className="p-2 rounded border border-destructive/40 bg-destructive/15 text-[11px] text-foreground flex items-start gap-2">
            <AlertCircle className="w-3.5 h-3.5 text-destructive shrink-0 mt-0.5" />
            <span className="font-mono">{chainError}</span>
          </div>
        )}
        {draft.chain.length === 0 ? (
          <p className={HELP}>{sourceAgents.length === 0 ? 'No other agents to chain from yet.' : 'Not chained from any agent.'}</p>
        ) : (
          <ul className="space-y-2">
            {draft.chain.map((c, i) => {
              const err = errors?.chain[c.key];
              const known = c.agent_id === 0 || sourceAgents.some((a) => a.id === c.agent_id);
              return (
                <li key={c.key} className="space-y-1">
                  <div className="flex flex-wrap items-center gap-2 text-xs text-foreground">
                    <span className="text-muted-foreground">Run after</span>
                    <select
                      value={c.agent_id}
                      onChange={(e) => setChain(c.key, { agent_id: Number(e.target.value) })}
                      aria-label={`Source agent for chain ${i + 1}`}
                      className="form-select text-xs py-1.5 max-w-[14rem]"
                    >
                      <option value={0} disabled>
                        Select an agent...
                      </option>
                      {sourceAgents.map((a) => (
                        <option key={a.id} value={a.id}>
                          {a.name} (#{a.id})
                        </option>
                      ))}
                      {!known && <option value={c.agent_id}>Agent #{c.agent_id} (missing)</option>}
                    </select>
                    <select
                      value={c.on}
                      onChange={(e) => setChain(c.key, { on: e.target.value })}
                      aria-label={`Chain condition ${i + 1}`}
                      className="form-select text-xs py-1.5"
                    >
                      {CHAIN_ON_OPTIONS.map((o) => (
                        <option key={o.on} value={o.on}>
                          {o.label}
                        </option>
                      ))}
                      {!CHAIN_ON_OPTIONS.some((o) => o.on === c.on) && <option value={c.on}>{c.on}</option>}
                    </select>
                    <button
                      type="button"
                      onClick={() => onChange({ ...draft, chain: draft.chain.filter((x) => x.key !== c.key) })}
                      aria-label={`Remove chain ${i + 1}`}
                      className={`${REMOVE_BTN} ml-auto`}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  {err && (
                    <p className={FIELD_ERR} role="alert">
                      <AlertCircle className="w-3 h-3" /> {err}
                    </p>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
});

function NumField({
  id,
  label,
  value,
  onChange,
  placeholder,
  step,
  min = 0,
  stacked,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  step?: string;
  min?: number;
  stacked?: boolean;
}) {
  return (
    <div className={stacked ? 'flex flex-col gap-1' : 'flex items-center gap-1.5'}>
      <label htmlFor={id} className={HELP}>
        {label}
      </label>
      <input
        id={id}
        type="number"
        min={min}
        step={step}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="form-input text-xs font-mono py-1.5 w-20"
      />
    </div>
  );
}
