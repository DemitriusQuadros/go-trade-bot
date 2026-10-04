import { AgentToolCall, GateResult, ToolRef, ToolRefKind } from '@/api/types';
import { parseProposalEvidence } from '@/lib/proposals';

// The one place the chat reads structured references out of a tool call
// (Phase D-02 §1). Cards never parse result text themselves.
//
// A current backend sends `refs` on every tool call (Phase D-01 §3). An older
// build doesn't, so this falls back to what the backend extractor does:
// JSON-object results are mapped by their known keys, anything else is
// scanned for the `strategy_id=N` / `backtest_id=N` prefixes the tools print.

const MAX_TEXT_REFS = 20;

const OPTIMIZATION_TOOLS = new Set(['run_optimization', 'get_optimization_results', 'list_optimizations']);

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);

function posInt(v: unknown): number | null {
  const n = typeof v === 'number' ? v : typeof v === 'string' && /^\d+$/.test(v.trim()) ? Number(v) : NaN;
  return Number.isInteger(n) && n > 0 ? n : null;
}

/** The call's result parsed as a JSON object, or null (text result, error, array). */
export function toolResultObject(call: AgentToolCall): Record<string, unknown> | null {
  if (call.error || !call.result) return null;
  const text = call.result.trim();
  if (!text.startsWith('{')) return null;
  try {
    const parsed: unknown = JSON.parse(text);
    return isObj(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

// JSON key -> ref kind/role, mirroring D-01 §3's extraction table.
const JSON_KEYS: Record<string, { kind: ToolRefKind; role?: string }> = {
  strategy_id: { kind: 'strategy' },
  challenger_strategy_id: { kind: 'strategy', role: 'challenger' },
  champion_strategy_id: { kind: 'strategy', role: 'champion' },
  backtest_id: { kind: 'backtest' },
  baseline_run_id: { kind: 'backtest', role: 'baseline' },
  candidate_run_id: { kind: 'backtest', role: 'candidate' },
  report_id: { kind: 'report' },
  proposal_id: { kind: 'proposal' },
  agent_id: { kind: 'agent' },
  memory_id: { kind: 'memory' },
  optimization_run_id: { kind: 'optimization' },
};

// List tools whose result is a JSON array of objects: each element's `id`.
const ARRAY_ID_KINDS: Record<string, ToolRefKind> = {
  list_proposals: 'proposal',
  list_reports: 'report',
  read_memory: 'memory',
};

// summarizeStrategyForModel ends with the free-form Lua source; ids are
// never read past this marker (same as the backend).
const SCRIPT_SOURCE_MARKER = '\nscript_source:';

function fallbackRefs(call: AgentToolCall): ToolRef[] {
  const out: ToolRef[] = [];
  const trimmed = (call.result ?? '').trim();
  if (trimmed.startsWith('[')) {
    try {
      const arr: unknown = JSON.parse(trimmed);
      const kind = ARRAY_ID_KINDS[call.tool];
      if (Array.isArray(arr)) {
        if (kind) {
          for (const el of arr) {
            const id = isObj(el) ? posInt(el.id) : null;
            if (id != null && out.length < MAX_TEXT_REFS) out.push({ kind, id });
          }
        }
        return out;
      }
    } catch {
      /* not JSON - fall through to the text scan */
    }
  }
  const obj = toolResultObject(call);
  if (obj) {
    const add = (key: string, value: unknown) => {
      const spec = JSON_KEYS[key];
      const id = posInt(value);
      if (spec && id != null) out.push({ kind: spec.kind, id, ...(spec.role ? { role: spec.role } : {}) });
    };
    for (const [k, v] of Object.entries(obj)) add(k, v);
    if (OPTIMIZATION_TOOLS.has(call.tool)) {
      const id = posInt(obj.optimization_id) ?? posInt(obj.run_id);
      if (id != null) out.push({ kind: 'optimization', id });
    }
    if (isObj(obj.gate)) {
      const b = posInt(obj.gate.baseline_run_id);
      const c = posInt(obj.gate.candidate_run_id);
      if (b != null) out.push({ kind: 'backtest', id: b, role: 'baseline' });
      if (c != null) out.push({ kind: 'backtest', id: c, role: 'candidate' });
    }
    return out;
  }
  let text = call.result ?? '';
  const cut = text.indexOf(SCRIPT_SOURCE_MARKER);
  if (cut >= 0) text = text.slice(0, cut);
  const re = /(?:^|\s)(strategy_id|backtest_id|optimization_run_id)=(\d+)/g;
  let m: RegExpExecArray | null;
  let first = true;
  while ((m = re.exec(text)) !== null && out.length < MAX_TEXT_REFS) {
    const id = Number(m[2]);
    if (!(id > 0)) continue;
    if (m[1] === 'optimization_run_id') {
      out.push({ kind: 'optimization', id });
    } else if (m[1] === 'strategy_id') {
      // create_strategy's leading strategy_id is the one it created.
      const created = call.tool === 'create_strategy' && first;
      first = false;
      out.push({ kind: 'strategy', id, ...(created ? { role: 'created' } : {}) });
    } else {
      out.push({ kind: 'backtest', id });
    }
  }
  return out;
}

function dedupe(refs: ToolRef[]): ToolRef[] {
  const seen = new Set<string>();
  return refs.filter((r) => {
    const k = `${r.kind}:${r.id}:${r.role ?? ''}`;
    if (seen.has(k)) return false;
    seen.add(k);
    return true;
  });
}

/** Every entity the call referenced. [] for an errored call. */
export function toolRefs(call: AgentToolCall): ToolRef[] {
  if (call.error) return [];
  // D-01 contract (reconciled): trust server refs whenever the field is present (even []).
  if (Array.isArray(call.refs)) {
    return dedupe(call.refs.filter((r) => r && typeof r.kind === 'string' && posInt(r.id) != null));
  }
  return dedupe(fallbackRefs(call));
}

/** The deploy-gate result carried by a deploy_to_testing/propose_promotion result, if any. */
export function toolGate(call: AgentToolCall): GateResult | undefined {
  const obj = toolResultObject(call);
  if (!obj || !isObj(obj.gate)) return undefined;
  return parseProposalEvidence({ gate: obj.gate }).gate;
}

export interface ToolGateContext {
  symbol?: string;
  timeframe?: string;
  from?: string;
  to?: string;
}

/** The gate's evaluation window (`gate_context`), if the result carries one. */
export function toolGateContext(call: AgentToolCall): ToolGateContext | undefined {
  const obj = toolResultObject(call);
  if (!obj || !isObj(obj.gate_context)) return undefined;
  const g = obj.gate_context;
  const s = (v: unknown) => (typeof v === 'string' && v ? v : undefined);
  return { symbol: s(g.symbol), timeframe: s(g.timeframe), from: s(g.from), to: s(g.to) };
}

/** A string field of a JSON result (e.g. trigger_agent's agent_name). */
export function toolResultString(call: AgentToolCall, key: string): string | null {
  const v = toolResultObject(call)?.[key];
  return typeof v === 'string' && v ? v : null;
}

/** A numeric argument the model passed (e.g. args.strategy_id). */
export function toolArgNumber(call: AgentToolCall, key: string): number | null {
  return posInt(call.args?.[key]);
}

/** A string argument the model passed (e.g. args.script_source). */
export function toolArgString(call: AgentToolCall, key: string): string | null {
  const v = call.args?.[key];
  return typeof v === 'string' ? v : null;
}

// --- Turn card plan (Phase D-02 §6) ---------------------------------------

export const CODE_CHANGE_TOOLS = new Set(['save_strategy_script', 'deploy_to_testing', 'create_challenger', 'create_strategy']);
// A backtest ref renders a full BacktestCard only from tools about one run;
// list_backtests can return 20 of them - those become chips.
const BACKTEST_CARD_TOOLS = new Set(['run_backtest', 'get_backtest']);
const GATE_TOOLS = new Set(['deploy_to_testing', 'propose_promotion']);
// Read-only list tools can reference many reports/proposals at once - those
// refs become chips, not one full card each.
const LIST_TOOLS = new Set(['list_reports', 'list_proposals', 'read_memory', 'list_backtests', 'list_strategies', 'list_optimizations']);

export interface GateCardItem {
  type: 'gate';
  key: string;
  call: AgentToolCall;
  gate: GateResult;
  baselineId: number | null;
  candidateId: number | null;
  deployed: boolean | null;
  proposalId: number | null;
  targetStrategyId: number | null;
}

export interface CodeChangeCardItem {
  type: 'code';
  key: string;
  strategyId: number;
  championId: number | null;
  // Every code-changing call for this strategy in the turn, in order - the
  // card diffs the last proposed source against the version before the first.
  calls: AgentToolCall[];
}

export interface BacktestCardItem {
  type: 'backtest';
  key: string;
  id: number;
  strategyId: number | null;
  call: AgentToolCall;
}

export interface EntityCardItem {
  type: 'report' | 'proposal';
  key: string;
  id: number;
}

export type TurnCardItem = GateCardItem | CodeChangeCardItem | BacktestCardItem | EntityCardItem;

export interface TurnChip {
  key: string;
  ref: ToolRef;
  call: AgentToolCall;
}

export interface TurnPlan {
  cards: TurnCardItem[];
  chips: TurnChip[];
  // Every strategy a CodeChangeCard covers (for cache invalidation, §7).
  codeStrategyIds: number[];
  hasReport: boolean;
  hasProposal: boolean;
}

/**
 * One card per distinct ref, in tool-call order; refs without a dedicated
 * card become chips. A gate with baseline + candidate runs is one GateCard,
 * never two BacktestCards.
 */
export function planTurnCards(calls: AgentToolCall[]): TurnPlan {
  const cards: TurnCardItem[] = [];
  const cardKeys = new Set<string>();
  const codeByStrategy = new Map<number, CodeChangeCardItem>();
  const shownStrategies = new Set<number>();
  const pendingChips: TurnChip[] = [];
  let hasReport = false;
  let hasProposal = false;

  const pushCard = (item: TurnCardItem) => {
    if (cardKeys.has(item.key)) return;
    cardKeys.add(item.key);
    cards.push(item);
  };

  calls.forEach((call, idx) => {
    if (call.error) return;
    const refs = toolRefs(call);
    const consumed = new Set<ToolRef>();
    const find = (kind: ToolRefKind, role?: string | null) =>
      refs.find((r) => r.kind === kind && (role === undefined ? true : role === null ? !r.role : r.role === role));
    const obj = toolResultObject(call);
    const deployed = typeof obj?.deployed === 'boolean' ? (obj.deployed as boolean) : null;

    if (refs.some((r) => r.kind === 'report')) hasReport = true;
    if (refs.some((r) => r.kind === 'proposal')) hasProposal = true;

    // 1. Deploy gate.
    const gate = GATE_TOOLS.has(call.tool) ? toolGate(call) : undefined;
    if (gate) {
      const baseline = find('backtest', 'baseline');
      const candidate = find('backtest', 'candidate');
      const proposal = find('proposal');
      [baseline, candidate, proposal].forEach((r) => r && consumed.add(r));
      const target = toolArgNumber(call, 'strategy_id') ?? toolArgNumber(call, 'challenger_strategy_id');
      if (target != null) shownStrategies.add(target);
      pushCard({
        type: 'gate',
        key: `gate:${idx}`,
        call,
        gate,
        baselineId: baseline?.id ?? gate.baseline_run_id ?? null,
        candidateId: candidate?.id ?? gate.candidate_run_id ?? null,
        deployed,
        proposalId: proposal?.id ?? null,
        targetStrategyId: target,
      });
    }

    // 2. Code change (deploy_to_testing only when it actually deployed).
    if (CODE_CHANGE_TOOLS.has(call.tool) && (call.tool !== 'deploy_to_testing' || deployed === true)) {
      const primary =
        call.tool === 'create_challenger' ? find('strategy', 'challenger') : find('strategy', null) ?? find('strategy', 'created');
      const champion = call.tool === 'create_challenger' ? find('strategy', 'champion') : undefined;
      const sid = primary?.id ?? toolArgNumber(call, 'strategy_id');
      if (sid != null) {
        refs.filter((r) => r.kind === 'strategy').forEach((r) => consumed.add(r));
        shownStrategies.add(sid);
        if (champion) shownStrategies.add(champion.id);
        const existing = codeByStrategy.get(sid);
        if (existing) {
          existing.calls.push(call);
          if (champion && existing.championId == null) existing.championId = champion.id;
        } else {
          const item: CodeChangeCardItem = {
            type: 'code',
            key: `code:${sid}`,
            strategyId: sid,
            championId: champion?.id ?? null,
            calls: [call],
          };
          codeByStrategy.set(sid, item);
          pushCard(item);
        }
      }
    }

    // 3. Remaining refs.
    const strategyOfCall = find('strategy', null)?.id ?? null;
    for (const ref of refs) {
      if (consumed.has(ref)) continue;
      if (ref.kind === 'backtest' && BACKTEST_CARD_TOOLS.has(call.tool)) {
        if (strategyOfCall != null) shownStrategies.add(strategyOfCall);
        pushCard({ type: 'backtest', key: `backtest:${ref.id}`, id: ref.id, strategyId: strategyOfCall, call });
      } else if (ref.kind === 'report' && !LIST_TOOLS.has(call.tool)) {
        pushCard({ type: 'report', key: `report:${ref.id}`, id: ref.id });
      } else if (ref.kind === 'proposal' && !LIST_TOOLS.has(call.tool)) {
        pushCard({ type: 'proposal', key: `proposal:${ref.id}`, id: ref.id });
      } else {
        pendingChips.push({ key: `${ref.kind}:${ref.id}`, ref, call });
      }
    }
  });

  const chipKeys = new Set<string>();
  const chips = pendingChips.filter((c) => {
    if (c.ref.kind === 'strategy' && shownStrategies.has(c.ref.id)) return false;
    if (c.ref.kind !== 'strategy' && cardKeys.has(`${c.ref.kind}:${c.ref.id}`)) return false;
    if (chipKeys.has(c.key)) return false;
    chipKeys.add(c.key);
    return true;
  });

  return { cards, chips, codeStrategyIds: Array.from(codeByStrategy.keys()), hasReport, hasProposal };
}
