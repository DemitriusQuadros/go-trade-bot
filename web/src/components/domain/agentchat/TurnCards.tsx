import React, { useMemo } from 'react';
import { AgentToolCall } from '@/api/types';
import { planTurnCards } from '@/lib/toolRefs';
import { BacktestCard } from './cards/BacktestCard';
import { GateCard } from './cards/GateCard';
import { CodeChangeCard } from './cards/CodeChangeCard';
import { ReportCard } from './cards/ReportCard';
import { ProposalCard } from './cards/ProposalCard';
import { RefChips } from './RefChips';
import { ApplyTarget, ChatDensity } from './types';

// The rich cards for one turn, one per distinct ref in tool-call order,
// plus a chip row for refs without a card (Phase D-02 §6).
export function TurnCards({
  toolCalls,
  density,
  apply,
}: {
  toolCalls: AgentToolCall[];
  density: ChatDensity;
  apply?: ApplyTarget;
}) {
  const plan = useMemo(() => planTurnCards(toolCalls), [toolCalls]);
  if (plan.cards.length === 0 && plan.chips.length === 0) return null;
  return (
    <div className="space-y-2 min-w-0">
      {plan.cards.map((item) => {
        switch (item.type) {
          case 'gate':
            return <GateCard key={item.key} item={item} density={density} />;
          case 'code':
            return <CodeChangeCard key={item.key} item={item} density={density} apply={apply} />;
          case 'backtest':
            return <BacktestCard key={item.key} id={item.id} strategyId={item.strategyId} call={item.call} density={density} />;
          case 'report':
            return <ReportCard key={item.key} id={item.id} density={density} />;
          case 'proposal':
            return <ProposalCard key={item.key} id={item.id} density={density} />;
          default:
            return null;
        }
      })}
      <RefChips chips={plan.chips} />
    </div>
  );
}
