import React, { useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  AlertCircle,
  AlertTriangle,
  ArrowLeft,
  Check,
  CheckCircle2,
  Clock,
  ExternalLink,
  FileDiff,
  Loader2,
  X,
  XCircle,
} from 'lucide-react';
import { ApiError, api, apiErrorMessage } from '@/api/client';
import { ForwardTestSide, ProposalDetail as ProposalDetailDTO, Strategy } from '@/api/types';
import { useApproveProposal, useProposal, useRejectProposal, useStrategy } from '@/hooks/queries';
import { useAuth } from '@/context/AuthContext';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { LoadingScreen } from '@/components/ui/Spinner';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { StatusBadge } from '@/components/ui/StatusBadge';
import {
  EarlyBadge,
  GateBadge,
  LiveTargetBadge,
  ProposalKindBadge,
  ProposalStatusBadge,
} from '@/components/domain/AgentBadges';
import { MarkdownMessage } from '@/components/domain/MarkdownMessage';
import { SourceDiff } from '@/components/domain/SourceDiff';
import { useToast } from '@/context/ToastContext';
import { formatRelative } from '@/lib/time';
import { formatMetric, humanizeCheckName, parseProposalEvidence, proposalKindLabel } from '@/lib/proposals';

const LIVE_APPROVE_MESSAGE =
  "This replaces the live strategy's code. It will apply automatically the next time the strategy has no open position. Mode and status stay unchanged.";

// One proposal (B-02 §3) - /agents/proposals/:id. The webhook deep-link
// target, so it must work on a cold full-page load: cmd/api's SPA fallback
// serves index.html and everything below is fetched.
export function ProposalDetail() {
  const { id } = useParams<{ id: string }>();
  const proposalId = Number(id);
  const { data: proposal, isLoading, error, dataUpdatedAt } = useProposal(proposalId);

  if (!Number.isFinite(proposalId) || proposalId <= 0) {
    return <NotFound message={`"${id}" isn't a proposal id.`} />;
  }
  if (isLoading) return <LoadingScreen message="Loading proposal..." />;
  if (error || !proposal) {
    return <NotFound message={`Couldn't load proposal #${proposalId}: ${error ? apiErrorMessage(error) : 'not found'}`} />;
  }
  return <ProposalView proposal={proposal} fetchedAt={dataUpdatedAt} />;
}

function NotFound({ message }: { message: string }) {
  return (
    <div className="container-custom space-y-4">
      <BackLink />
      <div className="flex items-center gap-2 p-6 rounded-lg border border-destructive/40 bg-destructive/10 text-destructive text-sm">
        <AlertCircle className="w-5 h-5 shrink-0" />
        <span>{message}</span>
      </div>
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/agents/proposals" className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1">
      <ArrowLeft className="w-3.5 h-3.5" /> Proposals
    </Link>
  );
}

function ProposalView({ proposal: p, fetchedAt }: { proposal: ProposalDetailDTO; fetchedAt: number }) {
  const { data: target, isLoading: targetLoading } = useStrategy(p.target_strategy_id);
  const { data: challenger } = useStrategy(p.challenger_strategy_id ?? 0);
  const evidence = useMemo(() => parseProposalEvidence(p.evidence), [p.evidence]);
  const canDecide = useAuth().can('approve_proposals');

  const isLive = target?.mode === 'live';
  const isPending = p.status === 'pending';
  const stale = p.target_current_source_matches_base === false;

  return (
    <div className="container-custom space-y-6 pb-20">
      {/* Header */}
      <div>
        <BackLink />
        <h1 className="text-2xl font-bold text-foreground flex items-center gap-2 flex-wrap mt-1">
          <span className="font-mono text-muted-foreground text-lg">#{p.id}</span>
          <span>{proposalKindLabel(p.kind)}</span>
          <span className="text-muted-foreground">→</span>
          <span className="break-words">{p.target_strategy_name || target?.name || `Strategy #${p.target_strategy_id}`}</span>
        </h1>
        <div className="flex flex-wrap items-center gap-1.5 mt-2">
          <ProposalStatusBadge status={p.status} />
          <ProposalKindBadge kind={p.kind} />
          {isLive && <LiveTargetBadge />}
          <GateBadge passed={evidence.gate?.passed ?? p.gate_passed} />
          {p.early && <EarlyBadge />}
        </div>
        <dl className="mt-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-6 gap-y-1.5 text-xs">
          <Meta label="Target">
            <StrategyLink id={p.target_strategy_id} strategy={target} fallbackName={p.target_strategy_name} />
          </Meta>
          {p.challenger_strategy_id != null && (
            <Meta label="Challenger">
              <StrategyLink id={p.challenger_strategy_id} strategy={challenger} />
            </Meta>
          )}
          <Meta label="Agent">
            <Link to={`/agents/${p.agent_id}`} className="text-foreground hover:text-primary hover:underline">
              {p.agent_name || `Agent #${p.agent_id}`}
            </Link>
          </Meta>
          <Meta label="Filed">
            <Timestamp iso={p.created_at} />
          </Meta>
          {p.decided_at && (
            <Meta label="Decided">
              <Timestamp iso={p.decided_at} />
            </Meta>
          )}
          {p.decided_by && (
            <Meta label="Decided by">
              <span className="text-foreground" data-testid="proposal-decided-by">{p.decided_by}</span>
            </Meta>
          )}
          {p.applied_at && (
            <Meta label="Applied">
              <Timestamp iso={p.applied_at} />
            </Meta>
          )}
        </dl>
        {p.decision_note && (
          <p className="mt-2 text-xs text-muted-foreground">
            Decision note: <span className="text-foreground">{p.decision_note}</span>
          </p>
        )}
      </div>

      {p.early && (
        <div className="p-3 rounded-lg border border-warning/40 bg-warning/10 text-xs text-foreground flex items-start gap-2">
          <AlertTriangle className="w-4 h-4 text-warning shrink-0 mt-0.5" />
          <span>
            <span className="font-semibold text-warning">Early proposal.</span> The challenger has less than 7 days of
            forward-test evidence. The agent's reason is in the rationale below (the line starting with "EARLY:").
          </span>
        </div>
      )}

      <StatusPanel proposal={p} fetchedAt={fetchedAt} />

      {/* Approve/Reject need approve_proposals (auth-02 §4). */}
      {isPending && canDecide && (
        <DecisionCard proposal={p} target={target} targetLoading={targetLoading} isLive={isLive} stale={stale} />
      )}
      {isPending && !canDecide && (
        <div role="note" className="p-3 rounded-lg border border-border bg-secondary/40 text-xs text-muted-foreground">
          Your account can't approve or reject proposals.
        </div>
      )}

      {/* Evidence */}
      <Card>
        <CardHeader
          title="Evidence"
          subtitle="Computed in Go from persisted walk-forward backtests - the agent supplies no numbers or thresholds"
        />
        <GateTable evidence={evidence} targetId={p.target_strategy_id} />
        {evidence.forward_test && (
          <div className="mt-5">
            <h3 className="text-xs font-semibold text-foreground mb-2">
              Forward test
              {evidence.forward_test.since && (
                <span className="font-normal text-muted-foreground">
                  {' '}
                  - closed trades since {new Date(evidence.forward_test.since).toLocaleString()}
                </span>
              )}
            </h3>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <ForwardTestCard title="Challenger" side={evidence.forward_test.challenger} />
              <ForwardTestCard title="Champion (current live code)" side={evidence.forward_test.champion} />
            </div>
          </div>
        )}
      </Card>

      {/* Rationale */}
      <Card>
        <CardHeader title="Rationale" subtitle="Written by the agent" />
        {p.rationale ? (
          <div className="text-foreground">
            <MarkdownMessage content={p.rationale} />
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">The agent gave no rationale.</p>
        )}
      </Card>

      {/* Diff */}
      <Card>
        <CardHeader
          title="Code change"
          subtitle="The target's source when the proposal was filed vs the proposed source"
          action={<FileDiff className="w-4 h-4 text-muted-foreground" />}
        />
        <SourceDiff before={p.base_source} after={p.proposed_source} beforeLabel="base (target when filed)" afterLabel="proposed" />
      </Card>

      {p.report_id != null && <LinkedReport reportId={p.report_id} />}
    </div>
  );
}

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2 min-w-0">
      <dt className="text-muted-foreground shrink-0">{label}:</dt>
      <dd className="min-w-0 truncate flex items-center gap-1.5">{children}</dd>
    </div>
  );
}

function Timestamp({ iso }: { iso: string }) {
  return (
    <span className="text-foreground" title={new Date(iso).toLocaleString()}>
      {formatRelative(iso)}
    </span>
  );
}

function StrategyLink({ id, strategy, fallbackName }: { id: number; strategy?: Strategy; fallbackName?: string }) {
  return (
    <>
      <Link to={`/strategies/${id}/edit`} className="text-foreground hover:text-primary hover:underline truncate">
        <span className="font-mono text-muted-foreground">#{id}</span> {strategy?.name ?? fallbackName ?? ''}
      </Link>
      {strategy && (
        <>
          <ModeBadge mode={strategy.mode} />
          <StatusBadge status={strategy.status} />
        </>
      )}
    </>
  );
}

// What happens next, per status.
function StatusPanel({ proposal: p, fetchedAt }: { proposal: ProposalDetailDTO; fetchedAt: number }) {
  if (p.status === 'approved') {
    const lastCheck = p.last_flat_check_at ? new Date(p.last_flat_check_at).getTime() : fetchedAt;
    return (
      <div role="status" className="p-4 rounded-lg border border-primary/40 bg-primary/10 text-xs text-foreground flex items-start gap-3">
        <Clock className="w-5 h-5 text-primary shrink-0" />
        <div className="space-y-1">
          <div className="font-semibold text-sm">Waiting for flat position</div>
          <p className="text-muted-foreground">
            {p.target_has_open_position
              ? 'The target strategy has an open position. The new code applies automatically once it closes (checked every minute, for up to 7 days).'
              : 'The target strategy has no open position - the new code will apply within a minute.'}
          </p>
          <p className="text-muted-foreground">
            Last checked{' '}
            <span className="text-foreground" title={new Date(lastCheck).toLocaleString()}>
              {formatRelative(new Date(lastCheck).toISOString())}
            </span>
            . This page refreshes every 30 seconds.
          </p>
        </div>
      </div>
    );
  }
  if (p.status === 'applied') {
    return (
      <Banner tone="success" icon={<CheckCircle2 className="w-5 h-5 text-success shrink-0" />} title="Applied">
        The proposed code replaced the target's source{p.applied_at ? ` ${formatRelative(p.applied_at)}` : ''}. Its mode
        and status were not changed.
      </Banner>
    );
  }
  if (p.status === 'rejected') {
    return (
      <Banner tone="muted" icon={<XCircle className="w-5 h-5 text-muted-foreground shrink-0" />} title="Rejected">
        Nothing was changed.
      </Banner>
    );
  }
  if (p.status === 'superseded') {
    return (
      <Banner tone="warning" icon={<AlertTriangle className="w-5 h-5 text-warning shrink-0" />} title="Superseded">
        The target's code changed after this proposal was filed, or a newer proposal replaced it. Nothing was applied.
      </Banner>
    );
  }
  if (p.status === 'failed') {
    return (
      <Banner tone="destructive" icon={<AlertCircle className="w-5 h-5 text-destructive shrink-0" />} title="Failed">
        {p.failure_reason || 'The proposal could not be applied.'} Nothing was changed.
      </Banner>
    );
  }
  return null;
}

function Banner({
  tone,
  icon,
  title,
  children,
}: {
  tone: 'success' | 'warning' | 'destructive' | 'muted';
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
}) {
  const cls = {
    success: 'border-success/40 bg-success/10',
    warning: 'border-warning/40 bg-warning/10',
    destructive: 'border-destructive/40 bg-destructive/10',
    muted: 'border-border bg-secondary/40',
  }[tone];
  return (
    <div role="status" className={`p-4 rounded-lg border ${cls} text-xs text-foreground flex items-start gap-3`}>
      {icon}
      <div>
        <div className="font-semibold text-sm">{title}</div>
        <p className="text-muted-foreground mt-0.5">{children}</p>
      </div>
    </div>
  );
}

function DecisionCard({
  proposal: p,
  target,
  targetLoading,
  isLive,
  stale,
}: {
  proposal: ProposalDetailDTO;
  target?: Strategy;
  targetLoading: boolean;
  isLive: boolean;
  stale: boolean;
}) {
  const approve = useApproveProposal();
  const reject = useRejectProposal();
  const { toast } = useToast();
  const [note, setNote] = useState('');
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [decisionError, setDecisionError] = useState<string | null>(null);
  const [superseded, setSuperseded] = useState(false);

  const busy = approve.isPending || reject.isPending;
  const targetName = target?.name ?? p.target_strategy_name ?? `strategy #${p.target_strategy_id}`;
  // Fail safe: if the target's mode isn't known yet, treat it as live so the
  // confirmation is never skipped for a live target.
  const needsConfirm = isLive || !target;
  const approveDisabled = busy || stale || superseded || targetLoading;

  const doApprove = () => {
    setConfirmOpen(false);
    setDecisionError(null);
    approve.mutate(
      { id: p.id, req: note.trim() ? { note: note.trim() } : {} },
      {
        onSuccess: () => {
          toast(
            isLive
              ? `Proposal #${p.id} approved - it applies once ${targetName} has no open position`
              : `Proposal #${p.id} approved - applying to ${targetName} now`,
          );
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 409) {
            setSuperseded(true);
            return;
          }
          setDecisionError(apiErrorMessage(err, 'Failed to approve the proposal'));
        },
      },
    );
  };

  const onApproveClick = () => {
    if (approveDisabled) return;
    if (needsConfirm) setConfirmOpen(true);
    else doApprove();
  };

  const doReject = () => {
    setDecisionError(null);
    reject.mutate(
      { id: p.id, req: note.trim() ? { note: note.trim() } : {} },
      {
        onSuccess: () => toast(`Proposal #${p.id} rejected`),
        onError: (err) => {
          if (err instanceof ApiError && err.status === 409) {
            setDecisionError('This proposal is no longer pending - it was decided or superseded elsewhere.');
            return;
          }
          setDecisionError(apiErrorMessage(err, 'Failed to reject the proposal'));
        },
      },
    );
  };

  const confirmMessage = `${LIVE_APPROVE_MESSAGE} ${
    p.target_has_open_position
      ? 'Right now: waiting for the open position to close.'
      : 'Right now: no open position - will apply within a minute.'
  }`;

  return (
    <Card className={isLive ? 'border-destructive/40' : undefined}>
      <CardHeader
        title="Decision"
        subtitle={
          isLive
            ? 'The target trades live. Approving schedules the code swap for its next flat moment.'
            : 'The target does not trade live. Approving applies the code right away.'
        }
      />

      {superseded ? (
        <div role="alert" className="mb-4 p-3 rounded-lg border border-warning/40 bg-warning/10 text-xs text-foreground flex items-start gap-2">
          <AlertTriangle className="w-4 h-4 text-warning shrink-0 mt-0.5" />
          <span>
            <span className="font-semibold text-warning">Superseded.</span> The target's code changed since this proposal
            was filed, so it can no longer be applied. It has been marked superseded.
          </span>
        </div>
      ) : stale ? (
        <div role="alert" className="mb-4 p-3 rounded-lg border border-warning/40 bg-warning/10 text-xs text-foreground flex items-start gap-2">
          <AlertTriangle className="w-4 h-4 text-warning shrink-0 mt-0.5" />
          <span>
            <span className="font-semibold text-warning">Stale proposal - can't be approved.</span> {targetName}'s code
            has changed since this proposal was filed, so the diff below no longer describes what approving would do.
            Reject it, and ask the agent for a fresh proposal if the change is still wanted.
          </span>
        </div>
      ) : (
        <p className="mb-4 text-xs text-muted-foreground flex items-center gap-1.5">
          {p.target_has_open_position ? (
            <>
              <Clock className="w-3.5 h-3.5" /> {targetName} has an open position - if approved, the change waits for it to
              close.
            </>
          ) : (
            <>
              <CheckCircle2 className="w-3.5 h-3.5 text-success" /> {targetName} has no open position right now.
            </>
          )}
        </p>
      )}

      {decisionError && (
        <div role="alert" className="mb-4 p-3 rounded-lg border border-destructive/40 bg-destructive/15 text-xs text-foreground flex items-start gap-2">
          <AlertCircle className="w-4 h-4 text-destructive shrink-0 mt-0.5" />
          <span>{decisionError}</span>
        </div>
      )}

      <div className="flex flex-col gap-1.5">
        <label htmlFor="proposal-note" className="text-xs font-semibold text-foreground">
          Note <span className="font-normal text-muted-foreground">(optional, saved with your decision)</span>
        </label>
        <textarea
          id="proposal-note"
          rows={2}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          disabled={busy || superseded}
          placeholder="e.g. Looks good - the drawdown improvement is what I wanted."
          className="form-textarea text-xs"
        />
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-end gap-2">
        <button
          type="button"
          onClick={doReject}
          disabled={busy || superseded}
          className="bg-secondary hover:bg-accent text-foreground rounded border border-border px-4 py-2 text-sm flex items-center gap-1.5 disabled:opacity-50"
        >
          {reject.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <X className="w-4 h-4" />}
          Reject
        </button>
        <button
          type="button"
          onClick={onApproveClick}
          disabled={approveDisabled}
          title={stale ? "The target's code changed since this proposal was filed" : undefined}
          className={`rounded border px-4 py-2 text-sm font-semibold flex items-center gap-1.5 disabled:opacity-50 disabled:cursor-not-allowed ${
            isLive
              ? 'bg-destructive/20 hover:bg-destructive/30 text-destructive border-destructive/40'
              : 'bg-primary hover:bg-primary/90 text-primary-foreground border-primary'
          }`}
        >
          {approve.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}
          {isLive ? 'Approve live change…' : 'Approve'}
        </button>
      </div>

      <ConfirmDialog
        isOpen={confirmOpen}
        title={`Approve change to ${isLive ? 'LIVE' : 'this'} strategy "${targetName}"?`}
        message={confirmMessage}
        confirmText="Approve"
        cancelText="Cancel"
        isDangerous
        onConfirm={doApprove}
        onCancel={() => setConfirmOpen(false)}
      />
    </Card>
  );
}

function GateTable({ evidence, targetId }: { evidence: ReturnType<typeof parseProposalEvidence>; targetId: number }) {
  const gate = evidence.gate;
  if (!gate) {
    return <p className="text-xs text-muted-foreground">No deploy-gate result was recorded for this proposal.</p>;
  }
  const checks = gate.checks ?? [];
  // Both gate runs are persisted under the target strategy (B-01 §2), so
  // both open in the target's workbench Backtest tab.
  const runLink = (runId: number | null | undefined, label: string) =>
    runId ? (
      <Link
        to={`/strategies/${targetId}/edit/backtest/${runId}`}
        className="inline-flex items-center gap-1 text-primary hover:underline"
      >
        {label} run #{runId} <ExternalLink className="w-3 h-3" />
      </Link>
    ) : null;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3 text-xs">
        <span className="flex items-center gap-1.5">
          Deploy gate: <GateBadge passed={gate.passed} />
          {gate.passed == null && <span className="text-muted-foreground">result unknown</span>}
        </span>
        {runLink(gate.baseline_run_id, 'Baseline')}
        {runLink(gate.candidate_run_id, 'Candidate')}
      </div>
      {checks.length === 0 ? (
        <p className="text-xs text-muted-foreground">No individual checks were recorded.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-[10px] [&_th]:uppercase [&_th]:font-semibold [&_th]:text-muted-foreground [&_td]:px-3 [&_td]:py-2 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/60">
            <thead className="bg-secondary/40">
              <tr>
                <th>Check</th>
                <th className="text-right">Candidate</th>
                <th className="text-right">Baseline</th>
                <th className="text-right">Threshold</th>
                <th className="text-center">Result</th>
              </tr>
            </thead>
            <tbody>
              {checks.map((c, i) => (
                <tr key={`${c.name}-${i}`}>
                  <td>
                    <div className="text-foreground font-medium">{humanizeCheckName(c.name)}</div>
                    {c.detail && <div className="text-[11px] text-muted-foreground mt-0.5">{c.detail}</div>}
                  </td>
                  <td className="text-right font-mono text-foreground">{formatMetric(c.candidate)}</td>
                  <td className="text-right font-mono text-muted-foreground">{formatMetric(c.baseline)}</td>
                  <td className="text-right font-mono text-muted-foreground">{formatMetric(c.threshold)}</td>
                  <td className="text-center">
                    {c.passed === true ? (
                      <CheckCircle2 className="w-4 h-4 text-success inline" aria-label="passed" />
                    ) : c.passed === false ? (
                      <XCircle className="w-4 h-4 text-destructive inline" aria-label="failed" />
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                    <span className="sr-only">{c.passed === true ? 'passed' : c.passed === false ? 'failed' : 'unknown'}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function ForwardTestCard({ title, side }: { title: string; side?: ForwardTestSide }) {
  const pnl = side?.net_pnl;
  return (
    <div className="rounded-lg border border-border bg-secondary/40 p-3">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground mb-2">{title}</div>
      {!side ? (
        <p className="text-xs text-muted-foreground">No data.</p>
      ) : (
        <dl className="grid grid-cols-3 gap-2 text-xs">
          <div>
            <dt className="text-muted-foreground">Trades</dt>
            <dd className="font-mono text-foreground text-base">{formatMetric(side.trades, 0)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Win rate</dt>
            <dd className="font-mono text-foreground text-base">
              {side.win_rate_pct == null ? '—' : `${formatMetric(side.win_rate_pct, 1)}%`}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Net PnL</dt>
            <dd
              className={`font-mono text-base ${
                pnl == null ? 'text-foreground' : pnl > 0 ? 'text-success' : pnl < 0 ? 'text-destructive' : 'text-foreground'
              }`}
            >
              {pnl == null ? '—' : `${pnl > 0 ? '+' : ''}${formatMetric(pnl)}`}
            </dd>
          </div>
          {side.max_adverse != null && (
            <div className="col-span-3">
              <dt className="text-muted-foreground inline">Max adverse: </dt>
              <dd className="font-mono text-foreground inline">{formatMetric(side.max_adverse)}</dd>
            </div>
          )}
        </dl>
      )}
    </div>
  );
}

function LinkedReport({ reportId }: { reportId: number }) {
  const isDark = useIsDarkMode();
  const htmlUrl = api.getAgentReportHtmlUrl(reportId, isDark ? 'dark' : 'light');
  return (
    <Card className="p-0 overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <h2 className="text-lg font-semibold tracking-tight text-primary/90">Linked report</h2>
        <Link to={`/agents/reports/${reportId}`} className="text-xs text-primary hover:underline inline-flex items-center gap-1">
          Open report #{reportId} <ExternalLink className="w-3 h-3" />
        </Link>
      </div>
      {/* Same sandboxed iframe as AgentReportDetail: no scripts, no forms,
          no same-origin access. */}
      <iframe
        key={htmlUrl}
        src={htmlUrl}
        sandbox=""
        title={`Agent report #${reportId}`}
        className="w-full h-[70vh] min-h-[480px] border-none bg-background block"
      />
    </Card>
  );
}
