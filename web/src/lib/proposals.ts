import {
  ForwardTestEvidence,
  ForwardTestSide,
  GateCheck,
  GateResult,
  ProposalEvidence,
  ProposalStatus,
} from '@/api/types';
import { hasKey, tr, type MessageKey } from '@/i18n';
import { formatNumber } from '@/lib/format';

// Helpers for the Phase B proposals UI (B-02). Pure functions only.

export function proposalKindLabel(kind: string): string {
  return hasKey(`proposals.kind.${kind}`) ? tr(`proposals.kind.${kind}` as MessageKey) : kind;
}

// Inbox tabs (B-02 §2). History covers every terminal status.
export type ProposalTab = 'pending' | 'approved' | 'history';

export const HISTORY_STATUSES: ProposalStatus[] = ['applied', 'rejected', 'superseded', 'failed'];

export function isHistoryStatus(status: string): boolean {
  return (HISTORY_STATUSES as string[]).includes(status);
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);

// Numbers arrive as JSON numbers, or - for values JSON can't represent
// (e.g. a +Inf profit factor) - as the strings "+Inf"/"-Inf"/"NaN".
function num(v: unknown): number | null {
  if (typeof v === 'number') return v;
  if (typeof v === 'string') {
    const t = v.trim().toLowerCase();
    if (t === '+inf' || t === 'inf' || t === 'infinity' || t === '+infinity') return Infinity;
    if (t === '-inf' || t === '-infinity') return -Infinity;
    if (t === 'nan') return NaN;
    const n = Number(t);
    return t !== '' && !Number.isNaN(n) ? n : null;
  }
  return null;
}

function parseCheck(v: unknown): GateCheck | null {
  if (!isObj(v)) return null;
  return {
    name: typeof v.name === 'string' ? v.name : undefined,
    passed: typeof v.passed === 'boolean' ? v.passed : undefined,
    candidate: num(v.candidate),
    baseline: num(v.baseline),
    threshold: num(v.threshold),
    detail: typeof v.detail === 'string' ? v.detail : undefined,
  };
}

function parseGate(v: unknown): GateResult | undefined {
  if (!isObj(v)) return undefined;
  const checks = Array.isArray(v.checks) ? v.checks.map(parseCheck).filter((c): c is GateCheck => c !== null) : [];
  return {
    passed: typeof v.passed === 'boolean' ? v.passed : undefined,
    checks,
    baseline_run_id: num(v.baseline_run_id),
    candidate_run_id: num(v.candidate_run_id),
  };
}

function parseSide(v: unknown): ForwardTestSide | undefined {
  if (!isObj(v)) return undefined;
  return {
    trades: num(v.trades),
    win_rate_pct: num(v.win_rate_pct),
    net_pnl: num(v.net_pnl),
    max_adverse: num(v.max_adverse),
  };
}

function parseForward(v: unknown): ForwardTestEvidence | undefined {
  if (!isObj(v)) return undefined;
  const challenger = parseSide(v.challenger);
  const champion = parseSide(v.champion);
  if (!challenger && !champion) return undefined;
  return { challenger, champion, since: typeof v.since === 'string' ? v.since : undefined };
}

// Decodes a proposal's raw `evidence` JSON. Tolerates a JSON string, null,
// or a partial object - every field of the result is optional.
export function parseProposalEvidence(raw: unknown): ProposalEvidence {
  let v = raw;
  if (typeof v === 'string') {
    try {
      v = JSON.parse(v);
    } catch {
      return {};
    }
  }
  if (!isObj(v)) return {};
  return { gate: parseGate(v.gate), forward_test: parseForward(v.forward_test) };
}

// Gate metric formatting. ±Inf / NaN are shown literally since they're
// meaningful (a +Inf profit factor passes; NaN always fails).
export function formatMetric(v: number | null | undefined, digits = 2): string {
  if (v == null) return '—';
  if (Number.isNaN(v)) return 'NaN';
  if (v === Infinity) return '+∞';
  if (v === -Infinity) return '-∞';
  if (Number.isInteger(v)) return String(v);
  return formatNumber(v, { digits });
}

// "max_drawdown_ratio" -> "Max drawdown ratio"
export function humanizeCheckName(name: string | undefined): string {
  if (!name) return tr('proposals.unnamedCheck');
  if (hasKey(`proposals.check.${name}`)) return tr(`proposals.check.${name}` as MessageKey);
  const s = name.replace(/[_-]+/g, ' ').trim();
  return s.charAt(0).toUpperCase() + s.slice(1);
}
