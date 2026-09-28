import React from 'react';
import { permissionLabel } from '@/lib/agentPermissions';
import { formatUsd } from '@/lib/time';

// Small status chips for the agents platform. Same base shape as
// ui/StatusBadge and ui/ModeBadge (Console Pro: tiny uppercase, 1px border,
// token-tinted background) so they read as one family.
const BADGE_BASE = 'inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border whitespace-nowrap';

const TONE = {
  success: 'bg-success/15 text-success border-success/40',
  warning: 'bg-warning/15 text-warning border-warning/40',
  destructive: 'bg-destructive/15 text-destructive border-destructive/40',
  muted: 'bg-secondary text-muted-foreground border-border',
  accent: 'bg-accent text-accent-foreground border-border',
  primary: 'bg-primary/15 text-primary border-primary/40',
} as const;

export function SeverityBadge({ severity, className = '' }: { severity: string; className?: string }) {
  const tone =
    severity === 'critical' ? TONE.destructive : severity === 'warning' ? TONE.warning : TONE.accent;
  return <span className={`${BADGE_BASE} ${tone} ${className}`}>{severity}</span>;
}

export type AgentRuntimeStatus = 'active' | 'paused' | 'halted';

export function AgentStatusBadge({ status }: { status: AgentRuntimeStatus }) {
  if (status === 'halted') {
    return (
      <span className={`${BADGE_BASE} ${TONE.destructive}`} title="Global kill switch is on - no agent runs start">
        Halted
      </span>
    );
  }
  if (status === 'paused') return <span className={`${BADGE_BASE} ${TONE.warning}`}>Paused</span>;
  return <span className={`${BADGE_BASE} ${TONE.success}`}>Active</span>;
}

export function PermissionBadge({ permission }: { permission: string }) {
  return (
    <span className={`${BADGE_BASE} ${TONE.muted} normal-case font-mono font-medium`}>{permissionLabel(permission)}</span>
  );
}

export function DefaultAgentBadge() {
  return <span className={`${BADGE_BASE} ${TONE.primary}`}>default</span>;
}

export function MemoryKindBadge({ kind }: { kind: string }) {
  const tone =
    kind === 'finding'
      ? TONE.warning
      : kind === 'report_ref'
        ? TONE.primary
        : kind === 'journal'
          ? TONE.accent
          : TONE.muted;
  const label = kind === 'report_ref' ? 'report' : kind.replace('_', ' ');
  return <span className={`${BADGE_BASE} ${tone}`}>{label}</span>;
}

export function RunStatusChip({ status }: { status: string }) {
  const tone = status === 'error' ? TONE.destructive : status === 'ok' ? TONE.success : TONE.muted;
  return <span className={`${BADGE_BASE} ${tone}`}>{status}</span>;
}

// ReactNode helper so callers can render a list of permission chips inline.
export function PermissionBadgeList({ permissions }: { permissions: string[] }) {
  if (!permissions?.length) return <span className="text-muted-foreground text-xs">—</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {permissions.map((p) => (
        <React.Fragment key={p}>
          <PermissionBadge permission={p} />
        </React.Fragment>
      ))}
    </div>
  );
}

// "$0.42 / $2.50" - warning tone from 80% of budget, destructive at 100%.
// A budget of 0 means unlimited.
export function BudgetCell({ spent, budget }: { spent: number; budget: number }) {
  const ratio = budget > 0 ? spent / budget : 0;
  const tone = budget > 0 && ratio >= 1 ? 'text-destructive' : budget > 0 && ratio >= 0.8 ? 'text-warning' : 'text-foreground';
  return (
    <span className={`font-mono text-xs whitespace-nowrap ${tone}`} title={budget > 0 ? `${Math.round(ratio * 100)}% of daily budget` : 'No daily budget'}>
      {formatUsd(spent)}
      <span className="text-muted-foreground"> / {budget > 0 ? formatUsd(budget) : '∞'}</span>
    </span>
  );
}

// --- Phase B: proposals (B-02 §2) ------------------------------------------

export function ProposalKindBadge({ kind }: { kind: string }) {
  const tone = kind === 'promote_challenger' ? TONE.primary : TONE.accent;
  const label = kind === 'promote_challenger' ? 'Promote challenger' : kind === 'gate_failed_change' ? 'Gate-failed change' : kind;
  return <span className={`${BADGE_BASE} ${tone} normal-case`}>{label}</span>;
}

// passed === undefined/null -> gate result unknown (not shown in lists).
export function GateBadge({ passed }: { passed: boolean | null | undefined }) {
  if (passed == null) return null;
  return passed ? (
    <span className={`${BADGE_BASE} ${TONE.success}`} title="Every deploy-gate check passed">
      Gate passed
    </span>
  ) : (
    <span className={`${BADGE_BASE} ${TONE.destructive}`} title="At least one deploy-gate check failed">
      Gate failed
    </span>
  );
}

export function EarlyBadge() {
  return (
    <span
      className={`${BADGE_BASE} ${TONE.warning}`}
      title="Filed before the challenger had 7 days of forward-test evidence - the agent gave an explicit EARLY: reason"
    >
      Early
    </span>
  );
}

// Prominent marker for a proposal whose target trades real money.
export function LiveTargetBadge() {
  return (
    <span
      className={`${BADGE_BASE} bg-destructive text-destructive-foreground border-destructive`}
      title="The target strategy trades live - approving replaces its code"
    >
      Live
    </span>
  );
}

export function ProposalStatusBadge({ status }: { status: string }) {
  const tone =
    status === 'pending'
      ? TONE.warning
      : status === 'approved'
        ? TONE.primary
        : status === 'applied'
          ? TONE.success
          : status === 'failed'
            ? TONE.destructive
            : TONE.muted;
  return <span className={`${BADGE_BASE} ${tone}`}>{status}</span>;
}
