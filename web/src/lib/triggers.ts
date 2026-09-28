import {
  Agent,
  AgentRun,
  AgentRunTriggerInfo,
  AgentTriggers,
  AgentTriggerSummary,
  ChainOn,
  ChainTrigger,
  ChainTriggerDetail,
  CronTriggerDetail,
  EventTrigger,
  EventTriggerDetail,
  ManualTriggerDetail,
  MarketRule,
  MarketRuleKind,
  MarketTriggerDetail,
} from '@/api/types';
import { describeCron } from '@/lib/cron';

// Phase C trigger helpers (C-01 §1-§4, C-02). Display strings, defaults and
// a defensive normaliser for the `triggers` JSON. Validation is the
// backend's job; the checks here are only a convenience before saving.

// --- Strategy events (C-01 §2 table) ---------------------------------------

export interface EventCatalogEntry {
  type: string;
  label: string;
  help: string;
  defaultCooldown: number;
  // drawdown: threshold % + window (default 24h). no_signal: window required.
  needsThreshold?: boolean;
  needsWindow?: 'optional' | 'required';
  defaultWindowHours?: number;
}

export const EVENT_CATALOG: EventCatalogEntry[] = [
  {
    type: 'position.opened',
    label: 'A position was opened',
    help: 'Fires whenever a bound strategy opens a position.',
    defaultCooldown: 15,
  },
  {
    type: 'position.closed',
    label: 'A position was closed',
    help: 'Fires whenever a bound strategy closes a position, for any reason - including stop-losses.',
    defaultCooldown: 15,
  },
  {
    type: 'stoploss.hit',
    label: 'A stop-loss closed a position',
    help: 'A position.closed whose exit reason is a stop-loss (real or simulated in dryrun).',
    defaultCooldown: 15,
  },
  {
    type: 'strategy.error',
    label: 'A strategy cycle failed',
    help: 'A strategy cycle returned an error (not a crash).',
    defaultCooldown: 15,
  },
  {
    type: 'strategy.panic',
    label: 'A strategy crashed',
    help: 'A strategy cycle panicked and was recovered by the engine.',
    defaultCooldown: 15,
  },
  {
    type: 'drawdown',
    label: 'Drawdown crossed a threshold',
    help: 'Checked every 5 minutes: peak-to-trough of realized PnL over the window, as a % of the strategy\'s starting notional, reached the threshold.',
    defaultCooldown: 240,
    needsThreshold: true,
    needsWindow: 'optional',
    defaultWindowHours: 24,
  },
  {
    type: 'no_signal',
    label: 'No position opened for a while',
    help: 'Checked every 5 minutes: an enabled strategy has not opened a position within the window.',
    defaultCooldown: 240,
    needsWindow: 'required',
  },
];

export function eventLabel(type: string): string {
  return EVENT_CATALOG.find((e) => e.type === type)?.label ?? type;
}

// --- Market rules (C-01 §3) --------------------------------------------------

export const MARKET_KINDS: { kind: MarketRuleKind; label: string }[] = [
  { kind: 'pct_move', label: 'Price move %' },
  { kind: 'volatility_spike', label: 'Volatility spike ×' },
];

export const WINDOW_PRESETS: { label: string; minutes: number }[] = [
  { label: '15m', minutes: 15 },
  { label: '1h', minutes: 60 },
  { label: '4h', minutes: 240 },
  { label: '24h', minutes: 1440 },
];

export const MARKET_DEFAULT_COOLDOWN = 60;
export const MARKET_MIN_COOLDOWN = 5;
export const MARKET_MAX_WINDOW = 1440;

export function formatWindow(minutes: number | undefined): string {
  if (!minutes || !Number.isFinite(minutes)) return '?';
  if (minutes % 60 === 0) return `${minutes / 60}h`;
  return `${minutes}m`;
}

// "Fires when BTCUSDT moves ≥3% within 1h, at most once per 60m"
export function describeMarketRule(r: {
  symbol: string;
  kind: string;
  window_minutes: number;
  threshold_pct?: number;
  multiplier?: number;
  cooldown_minutes: number;
}): string {
  const sym = r.symbol.trim() || '<symbol>';
  const win = formatWindow(r.window_minutes);
  const cd = r.cooldown_minutes > 0 ? `${r.cooldown_minutes}m` : `${MARKET_DEFAULT_COOLDOWN}m`;
  if (r.kind === 'volatility_spike') {
    const m = r.multiplier && r.multiplier > 0 ? `${r.multiplier}×` : '?×';
    return `Fires when ${sym} volatility (ATR) over ${win} is ≥${m} its 24h baseline, at most once per ${cd}`;
  }
  const pct = r.threshold_pct && r.threshold_pct > 0 ? `${r.threshold_pct}%` : '?%';
  return `Fires when ${sym} moves ≥${pct} within ${win}, at most once per ${cd}`;
}

export function newRuleId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  // crypto.randomUUID needs a secure context (https or localhost); fall back
  // so the editor still works when the API is reached over plain http on a LAN.
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
}

// --- Chains (C-01 §4) ----------------------------------------------------------

export const CHAIN_ON_OPTIONS: { on: ChainOn; label: string }[] = [
  { on: 'report', label: 'writes a report' },
  { on: 'notify', label: 'sends a notification' },
  { on: 'success', label: 'finishes successfully' },
];

export function chainOnLabel(on: string): string {
  return CHAIN_ON_OPTIONS.find((o) => o.on === on)?.label ?? on;
}

export const MAX_CHAIN_DEPTH = 3;

// --- Normalisation --------------------------------------------------------------

// normalizeTriggers accepts whatever `triggers` JSON the API returns and
// yields the C-01 §1 shape. C-01 makes the backend convert the old forms
// (events: string[], chain_from: number[]) itself; this is a second line of
// defence so an older API build (or a row the backend didn't migrate) still
// loads instead of crashing the editor.
export function normalizeTriggers(raw: AgentTriggers | null | undefined): Required<AgentTriggers> {
  const t = (raw ?? {}) as Record<string, unknown>;
  const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

  const cron = arr(t.cron).filter((c): c is string => typeof c === 'string');

  const events: EventTrigger[] = arr(t.events)
    .map((e) => (typeof e === 'string' ? { type: e } : e))
    .filter((e): e is EventTrigger => !!e && typeof e === 'object' && typeof (e as EventTrigger).type === 'string');

  const market: MarketRule[] = arr(t.market)
    .filter((m): m is MarketRule => !!m && typeof m === 'object')
    .map((m) => ({
      ...m,
      // The backend requires unique rule ids; a rule stored without one
      // (pre-C placeholder data) gets a fresh id here so it can be saved.
      // Ids that exist are never touched.
      id: typeof m.id === 'string' && m.id ? m.id : newRuleId(),
      symbol: typeof m.symbol === 'string' ? m.symbol : '',
    }));

  const chain_from: ChainTrigger[] = arr(t.chain_from)
    .map((c) => (typeof c === 'number' ? { agent_id: c, on: 'report' } : c))
    .filter((c): c is ChainTrigger => !!c && typeof c === 'object' && typeof (c as ChainTrigger).agent_id === 'number');

  return { cron, events, market, chain_from };
}

export function triggerSummary(agent: Agent): AgentTriggerSummary {
  if (agent.trigger_summary) return agent.trigger_summary;
  const t = normalizeTriggers(agent.triggers);
  return { cron: t.cron.length, events: t.events.length, market: t.market.length, chain_from: t.chain_from.length };
}

// Multi-line detail for the agents table's trigger-summary tooltip.
export function triggerDetailLines(agent: Agent, agentName: (id: number) => string): string[] {
  const t = normalizeTriggers(agent.triggers);
  const lines: string[] = [];
  t.cron.forEach((c) => lines.push(`Schedule: ${describeCron(c)} UTC (${c})`));
  t.events.forEach((e) => {
    const extra: string[] = [];
    if (e.threshold_pct) extra.push(`≥${e.threshold_pct}%`);
    if (e.window_hours) extra.push(`${e.window_hours}h window`);
    lines.push(`Event: ${e.type}${extra.length ? ` (${extra.join(', ')})` : ''}`);
  });
  t.market.forEach((m) => lines.push(`Market: ${describeMarketRule(m)}`));
  t.chain_from.forEach((c) => lines.push(`Chained: after ${agentName(c.agent_id)} ${chainOnLabel(c.on)}`));
  return lines;
}

// --- Runs ----------------------------------------------------------------------

export function runTriggerInfo(run: Pick<AgentRun, 'trigger' | 'trigger_detail'>): AgentRunTriggerInfo {
  const detail = (run.trigger_detail ?? {}) as Record<string, unknown>;
  switch (run.trigger) {
    case 'cron':
      return { trigger: 'cron', detail: detail as CronTriggerDetail };
    case 'manual':
      return { trigger: 'manual', detail: detail as ManualTriggerDetail };
    case 'event':
      return { trigger: 'event', detail: detail as EventTriggerDetail };
    case 'market':
      return { trigger: 'market', detail: detail as MarketTriggerDetail };
    case 'chain':
      return { trigger: 'chain', detail: detail as ChainTriggerDetail };
    default:
      return { trigger: 'other', raw: run.trigger, detail };
  }
}

function signedPct(v: number): string {
  const rounded = Math.round(v * 100) / 100;
  return `${rounded > 0 ? '+' : ''}${rounded}%`;
}

// One-line market detail: "BTCUSDT +3.4% in 60m" / "ETHUSDT 2.1× vol in 1h".
export function describeMarketDetail(d: MarketTriggerDetail): string {
  const sym = d.symbol ?? '?';
  const win = d.window_minutes ? `${d.window_minutes}m` : '?';
  if (d.kind === 'volatility_spike') {
    const m = d.observed_multiplier != null ? `${Math.round(d.observed_multiplier * 100) / 100}×` : '?×';
    return `${sym} ${m} volatility in ${win}`;
  }
  const pct = d.observed_pct != null ? signedPct(d.observed_pct) : '?%';
  return `${sym} ${pct} in ${win}`;
}
