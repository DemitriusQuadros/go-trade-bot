import React from 'react';
import { GitPullRequest } from 'lucide-react';
import { useProposal, useStrategies } from '@/hooks/queries';
import { GateBadge, ProposalKindBadge, ProposalStatusBadge } from '@/components/domain/AgentBadges';
import { CardAction, CardFallback, CardSkeleton, ChatCard, ChatDensity } from './ChatCard';

// A proposal an agent filed (Phase D-02 §6). Approval stays on the
// proposal page - this card only links there.
export function ProposalCard({ id, density }: { id: number; density: ChatDensity }) {
  const { data: p, isLoading, error } = useProposal(id);
  const { data: strategies = [] } = useStrategies();

  if (isLoading) return <CardSkeleton label={`Loading proposal #${id}`} />;
  if (error || !p) return <CardFallback text={`Proposal #${id} couldn't be loaded.`} to={`/agents/proposals/${id}`} />;

  const challengerName =
    p.challenger_strategy_id != null ? strategies.find((s) => s.id === p.challenger_strategy_id)?.name : undefined;

  return (
    <ChatCard
      testId="chat-proposal-card"
      density={density}
      icon={<GitPullRequest className="w-3.5 h-3.5" />}
      title={`Proposal #${p.id}`}
      meta={<ProposalStatusBadge status={p.status} />}
      actions={<CardAction to={`/agents/proposals/${p.id}`}>Review</CardAction>}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <ProposalKindBadge kind={p.kind} />
        <GateBadge passed={p.gate_passed} />
      </div>
      <div className="text-xs text-foreground space-y-0.5">
        <div>
          <span className="text-muted-foreground">Target:</span> #{p.target_strategy_id} {p.target_strategy_name}
        </div>
        {p.challenger_strategy_id != null && (
          <div>
            <span className="text-muted-foreground">Challenger:</span> #{p.challenger_strategy_id} {challengerName ?? ''}
          </div>
        )}
      </div>
      {p.rationale && <p className="text-[11px] text-muted-foreground line-clamp-3 break-words">{p.rationale}</p>}
    </ChatCard>
  );
}
