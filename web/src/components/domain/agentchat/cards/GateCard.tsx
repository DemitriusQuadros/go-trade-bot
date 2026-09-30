import React from 'react';
import { Link } from 'react-router-dom';
import { CheckCircle2, ShieldCheck, XCircle } from 'lucide-react';
import { useBacktest } from '@/hooks/queries';
import { GateBadge } from '@/components/domain/AgentBadges';
import { EquityComparisonChart } from '@/components/charts/EquityComparisonChart';
import { humanizeCheckName } from '@/lib/proposals';
import { GateCardItem, toolGateContext } from '@/lib/toolRefs';
import { CardAction, ChatCard, ChatDensity } from './ChatCard';
import { formatDay } from './BacktestCard';
import { formatNumber } from '@/lib/format';
import { useT } from '@/i18n';

// Gate metrics as the backend wrote them: non-finite values are shown
// literally as +Inf / -Inf / NaN (parseProposalEvidence turns the strings
// into numbers; this turns them back).
function gateValue(v: number | null | undefined): string {
  if (v == null) return '—';
  if (Number.isNaN(v)) return 'NaN';
  if (v === Infinity) return '+Inf';
  if (v === -Infinity) return '-Inf';
  return Number.isInteger(v) ? formatNumber(v) : formatNumber(v, { digits: 2 });
}

// deploy_to_testing / propose_promotion with a deploy-gate result (Phase
// D-02 §6): one card for both gate runs instead of two BacktestCards.
export function GateCard({ item, density }: { item: GateCardItem; density: ChatDensity }) {
  const t = useT();
  const { gate, baselineId, candidateId, deployed, proposalId, targetStrategyId, call } = item;
  const baseline = useBacktest(baselineId ?? 0);
  const candidate = useBacktest(candidateId ?? 0);
  const ctx = toolGateContext(call);
  const checks = gate.checks ?? [];
  const failedCount = checks.filter((c) => c.passed === false).length;
  // Both gate runs persist under the target strategy (B-01 §2).
  const runSid = targetStrategyId ?? baseline.data?.strategy_id ?? candidate.data?.strategy_id ?? null;

  let outcome: React.ReactNode = null;
  if (deployed === true) {
    outcome = (
      <span className="text-success">
        {t.rich('cards.deployedTo', {
          link: targetStrategyId ? (
            <Link to={`/strategies/${targetStrategyId}/edit`} className="font-semibold hover:underline">
              #{targetStrategyId}
            </Link>
          ) : null,
        })}
      </span>
    );
  } else if (proposalId != null) {
    outcome = (
      <span className="text-foreground">
        {gate.passed === false ? t('cards.gateFailedArrow') : ''}
        {t.rich('cards.proposalCreated', {
          link: (
            <Link to={`/agents/proposals/${proposalId}`} className="font-semibold text-primary hover:underline">
              {t('cards.proposalLink', { id: proposalId })}
            </Link>
          ),
        })}
      </span>
    );
  } else if (deployed === false) {
    outcome = <span className="text-muted-foreground">{t('cards.notDeployed')}</span>;
  }

  return (
    <ChatCard
      testId="chat-gate-card"
      density={density}
      icon={<ShieldCheck className="w-3.5 h-3.5" />}
      title={
        <>
          {t('cards.deployGate')}
          {targetStrategyId ? <span className="font-normal text-muted-foreground"> · #{targetStrategyId}</span> : null}
        </>
      }
      meta={
        <>
          {failedCount > 0 && (
            <span className="text-[11px] text-muted-foreground font-mono">{t('cards.failedCount', { count: failedCount })}</span>
          )}
          <GateBadge passed={gate.passed} />
        </>
      }
      actions={
        runSid && candidateId ? (
          <>
            <CardAction to={`/strategies/${runSid}/edit/backtest/${candidateId}`}>{t('cards.openCandidate')}</CardAction>
            {baselineId ? (
              <CardAction to={`/strategies/${runSid}/edit/backtest/${baselineId}`}>{t('cards.baselineRun')}</CardAction>
            ) : null}
          </>
        ) : undefined
      }
    >
      {outcome && <div className="text-xs">{outcome}</div>}
      {ctx && (ctx.symbol || ctx.from) && (
        <div className="text-[11px] text-muted-foreground font-mono">
          {[ctx.symbol, ctx.timeframe].filter(Boolean).join(' ')}
          {ctx.from ? ` · ${formatDay(ctx.from)} – ${formatDay(ctx.to)}` : ''}
          {t('cards.walkForwardSuffix')}
        </div>
      )}
      {checks.length === 0 ? (
        <p className="text-xs text-muted-foreground">{t('cards.noChecks')}</p>
      ) : (
        <div className="overflow-x-auto rounded border border-border">
          <table className="w-full text-[11px] border-collapse [&_th]:px-2 [&_th]:py-1.5 [&_th]:text-left [&_th]:text-[10px] [&_th]:uppercase [&_th]:font-semibold [&_th]:text-muted-foreground [&_td]:px-2 [&_td]:py-1.5 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/60">
            <thead className="bg-secondary/40">
              <tr>
                <th>{t('cards.colCheck')}</th>
                <th className="text-right">{t('cards.colCandidate')}</th>
                <th className="text-right">{t('cards.colBaseline')}</th>
                <th className="text-right">{t('cards.colThreshold')}</th>
                <th className="text-center">{t('cards.colPass')}</th>
              </tr>
            </thead>
            <tbody>
              {checks.map((c, i) => (
                <tr key={`${c.name}-${i}`}>
                  <td className="text-foreground" title={c.detail}>
                    {humanizeCheckName(c.name)}
                  </td>
                  <td className="text-right font-mono tabular-nums text-foreground">{gateValue(c.candidate)}</td>
                  <td className="text-right font-mono tabular-nums text-muted-foreground">{gateValue(c.baseline)}</td>
                  <td className="text-right font-mono tabular-nums text-muted-foreground">{gateValue(c.threshold)}</td>
                  <td className="text-center">
                    {c.passed === true ? (
                      <CheckCircle2 className="w-3.5 h-3.5 text-success inline" aria-label={t('cards.passed')} />
                    ) : c.passed === false ? (
                      <XCircle className="w-3.5 h-3.5 text-destructive inline" aria-label={t('cards.failed')} />
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {checks.some((c) => c.detail && c.passed === false) && (
        <ul className="text-[11px] text-muted-foreground space-y-0.5">
          {checks
            .filter((c) => c.detail && c.passed === false)
            .map((c, i) => (
              <li key={i} className="break-words">
                <span className="text-foreground">{humanizeCheckName(c.name)}:</span> {c.detail}
              </li>
            ))}
        </ul>
      )}
      {baseline.data && candidate.data && (
        <EquityComparisonChart
          baseline={baseline.data.equity_curve ?? []}
          candidate={candidate.data.equity_curve ?? []}
          height={150}
          summary={t('cards.gateSummary', { baseline: baselineId, candidate: candidateId })}
        />
      )}
    </ChatCard>
  );
}
