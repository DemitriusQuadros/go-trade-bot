import React, { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { ArrowUpRight, Bot, BookMarked, Cpu, FileText, GitPullRequest, History, TrendingUp } from 'lucide-react';
import { useAgents, useStrategies } from '@/hooks/queries';
import { TurnChip, toolRefs, toolResultString } from '@/lib/toolRefs';

const CHIP =
  'inline-flex items-center gap-1 max-w-full rounded border border-border bg-secondary/60 px-1.5 py-0.5 text-[11px] text-foreground';

// Refs without a dedicated card (Phase D-02 §6): read-only strategy lookups,
// agents, memory entries, optimizations - one compact row of links.
export function RefChips({ chips }: { chips: TurnChip[] }) {
  const { data: strategies = [] } = useStrategies();
  const { data: agents = [] } = useAgents();
  const strategyName = useMemo(() => new Map(strategies.map((s) => [s.id, s.name])), [strategies]);
  const agentName = useMemo(() => new Map(agents.map((a) => [a.id, a.name])), [agents]);

  if (chips.length === 0) return null;
  // read_memory can reference up to 20 entries - more than a few collapse
  // into one summary chip.
  const memory = chips.filter((c) => c.ref.kind === 'memory');
  const groupMemory = memory.length > 3;
  const shown = groupMemory ? chips.filter((c) => c.ref.kind !== 'memory') : chips;
  return (
    <div className="flex flex-wrap gap-1.5" aria-label="Referenced items">
      {groupMemory && (
        <span className={CHIP}>
          <BookMarked className="w-3 h-3 shrink-0 text-muted-foreground" />
          <span>{memory.length} memory entries</span>
        </span>
      )}
      {shown.map(({ key, ref, call }) => {
        switch (ref.kind) {
          case 'report':
            return (
              <Link key={key} to={`/agents/reports/${ref.id}`} className={`${CHIP} hover:border-primary`}>
                <FileText className="w-3 h-3 shrink-0 text-muted-foreground" />
                <span className="font-mono">Report #{ref.id}</span>
                <ArrowUpRight className="w-3 h-3 shrink-0" />
              </Link>
            );
          case 'proposal':
            return (
              <Link key={key} to={`/agents/proposals/${ref.id}`} className={`${CHIP} hover:border-primary`}>
                <GitPullRequest className="w-3 h-3 shrink-0 text-muted-foreground" />
                <span className="font-mono">Proposal #{ref.id}</span>
                <ArrowUpRight className="w-3 h-3 shrink-0" />
              </Link>
            );
          case 'strategy':
            return (
              <Link key={key} to={`/strategies/${ref.id}/edit`} className={`${CHIP} hover:border-primary`}>
                <Cpu className="w-3 h-3 shrink-0 text-muted-foreground" />
                <span className="font-mono">#{ref.id}</span>
                <span className="truncate">{strategyName.get(ref.id) ?? ''}</span>
                <ArrowUpRight className="w-3 h-3 shrink-0" />
              </Link>
            );
          case 'backtest':
            return (
              <Link key={key} to="/backtest" className={`${CHIP} hover:border-primary`}>
                <History className="w-3 h-3 shrink-0 text-muted-foreground" />
                <span className="font-mono">Backtest #{ref.id}</span>
                <ArrowUpRight className="w-3 h-3 shrink-0" />
              </Link>
            );
          case 'agent': {
            const name = toolResultString(call, 'agent_name') ?? agentName.get(ref.id) ?? `#${ref.id}`;
            return (
              <Link key={key} to={`/agents/${ref.id}`} className={`${CHIP} hover:border-primary`}>
                <Bot className="w-3 h-3 shrink-0 text-muted-foreground" />
                <span className="truncate">{call.tool === 'trigger_agent' ? `Triggered agent ${name}` : `Agent ${name}`}</span>
                <ArrowUpRight className="w-3 h-3 shrink-0" />
              </Link>
            );
          }
          case 'memory': {
            const sid = toolRefs(call).find((r) => r.kind === 'strategy')?.id;
            const label = (
              <>
                <BookMarked className="w-3 h-3 shrink-0 text-muted-foreground" />
                <span className="truncate">Memory entry #{ref.id}{sid ? ` · strategy #${sid}` : ''}</span>
              </>
            );
            return sid ? (
              <Link key={key} to={`/agent?strategy=${sid}`} className={`${CHIP} hover:border-primary`} title="Open this strategy's notes">
                {label}
              </Link>
            ) : (
              <span key={key} className={CHIP}>
                {label}
              </span>
            );
          }
          case 'optimization':
            return (
              <Link key={key} to="/optimization" className={`${CHIP} hover:border-primary`}>
                <TrendingUp className="w-3 h-3 shrink-0 text-muted-foreground" />
                <span className="font-mono">Optimization #{ref.id}</span>
                <ArrowUpRight className="w-3 h-3 shrink-0" />
              </Link>
            );
          default:
            return null;
        }
      })}
    </div>
  );
}
