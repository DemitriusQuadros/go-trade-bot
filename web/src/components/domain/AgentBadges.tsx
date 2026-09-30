import React from 'react';
import { permissionLabel } from '@/lib/agentPermissions';
import { formatUsd } from '@/lib/format';
import { proposalKindLabel } from '@/lib/proposals';
import { useT } from '@/i18n';

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
  const t = useT();
  const tone =
    severity === 'critical' ? TONE.destructive : severity === 'warning' ? TONE.warning : TONE.accent;
  return <span className={`${BADGE_BASE} ${tone} ${className}`}>{t.enum('severity', severity)}</span>;
}

export type AgentRuntimeStatus = 'active' | 'paused' | 'halted';

export function AgentStatusBadge({ status }: { status: AgentRuntimeStatus }) {
  const t = useT();
  if (status === 'halted') {
    return (
      <span className={`${BADGE_BASE} ${TONE.destructive}`} title={t('agents.haltedTitle')}>
        {t('agents.halted')}
      </span>
    );
  }
  if (status === 'paused') return <span className={`${BADGE_BASE} ${TONE.warning}`}>{t('agents.statusPaused')}</span>;
  return <span className={`${BADGE_BASE} ${TONE.success}`}>{t('agents.statusActive')}</span>;
}

export function PermissionBadge({ permission }: { permission: string }) {
  return (
    <span className={`${BADGE_BASE} ${TONE.muted} normal-case font-mono font-medium`}>{permissionLabel(permission)}</span>
  );
}

export function DefaultAgentBadge() {
  const t = useT();
  return <span className={`${BADGE_BASE} ${TONE.primary}`}>{t('agents.defaultBadge')}</span>;
}

export function MemoryKindBadge({ kind }: { kind: string }) {
  const t = useT();
  const tone =
    kind === 'finding'
      ? TONE.warning
      : kind === 'report_ref'
        ? TONE.primary
        : kind === 'journal'
          ? TONE.accent
          : TONE.muted;
  const label = t.enum('memoryKind', kind) || kind.replace('_', ' ');
  return <span className={`${BADGE_BASE} ${tone}`}>{label}</span>;
}

export function RunStatusChip({ status }: { status: string }) {
  const t = useT();
  const tone =
    status === 'error'
      ? TONE.destructive
      : status === 'ok'
        ? TONE.success
        : status === 'running'
          ? `${TONE.primary} animate-pulse`
          : TONE.muted;
  return <span className={`${BADGE_BASE} ${tone}`}>{t.enum('runStatus', status)}</span>;
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
  const t = useT();
  const ratio = budget > 0 ? spent / budget : 0;
  const tone = budget > 0 && ratio >= 1 ? 'text-destructive' : budget > 0 && ratio >= 0.8 ? 'text-warning' : 'text-foreground';
  return (
    <span className={`font-mono text-xs whitespace-nowrap ${tone}`} title={budget > 0 ? t('agents.budgetPct', { pct: Math.round(ratio * 100) }) : t('agents.noBudget')}>
      {formatUsd(spent)}
      <span className="text-muted-foreground"> / {budget > 0 ? formatUsd(budget) : '∞'}</span>
    </span>
  );
}

// --- Phase B: proposals (B-02 §2) ------------------------------------------

export function ProposalKindBadge({ kind }: { kind: string }) {
  const tone = kind === 'promote_challenger' ? TONE.primary : TONE.accent;
  const label = proposalKindLabel(kind);
  return <span className={`${BADGE_BASE} ${tone} normal-case`}>{label}</span>;
}

// passed === undefined/null -> gate result unknown (not shown in lists).
export function GateBadge({ passed }: { passed: boolean | null | undefined }) {
  const t = useT();
  if (passed == null) return null;
  return passed ? (
    <span className={`${BADGE_BASE} ${TONE.success}`} title={t('agents.gatePassedTitle')}>
      {t('agents.gatePassed')}
    </span>
  ) : (
    <span className={`${BADGE_BASE} ${TONE.destructive}`} title={t('agents.gateFailedTitle')}>
      {t('agents.gateFailed')}
    </span>
  );
}

export function EarlyBadge() {
  const t = useT();
  return (
    <span
      className={`${BADGE_BASE} ${TONE.warning}`}
      title={t('agents.earlyTitle')}
    >
      {t('agents.early')}
    </span>
  );
}

// Prominent marker for a proposal whose target trades real money.
export function LiveTargetBadge() {
  const t = useT();
  return (
    <span
      className={`${BADGE_BASE} bg-destructive text-destructive-foreground border-destructive`}
      title={t('agents.liveTitle')}
    >
      {t('agents.live')}
    </span>
  );
}

export function ProposalStatusBadge({ status }: { status: string }) {
  const t = useT();
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
  return <span className={`${BADGE_BASE} ${tone}`}>{t.enum('proposalStatus', status)}</span>;
}
