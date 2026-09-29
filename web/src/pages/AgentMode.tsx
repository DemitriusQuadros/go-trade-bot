import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import {
  ArrowUpRight,
  Bot,
  Code2,
  FileText,
  GitBranch,
  GitPullRequest,
  Globe,
  MessageSquareText,
  PanelLeft,
  PanelRightClose,
  PanelRightOpen,
  Search,
  X,
} from 'lucide-react';
import { Agent, Strategy } from '@/api/types';
import { useChatSession } from '@/context/ChatSessionContext';
import { usePendingProposalCount, useProposals, useStrategies, useStrategy } from '@/hooks/queries';
import { usePersistedOpen } from '@/hooks/usePersistedOpen';
import { formatUsd, formatRelative } from '@/lib/time';
import { proposalKindLabel } from '@/lib/proposals';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { AgentStatusBadge, ProposalStatusBadge } from '@/components/domain/AgentBadges';
import { AgentNotesPanel } from '@/components/domain/AgentNotesPanel';
import { ChatTranscript } from '@/components/domain/agentchat/ChatTranscript';
import { ChatComposer, ChatComposerHandle } from '@/components/domain/agentchat/ChatComposer';

const STRATEGY_PROMPTS = [
  'Backtest this over the last 6 months',
  'Why did the last trades lose?',
  'Propose an improvement as a challenger',
];
const GENERAL_PROMPTS = [
  'Which strategies have no recent backtest?',
  'Summarize how my strategies performed this week',
  'Which strategy has the worst drawdown right now, and why?',
];

const RAIL_HEADING = 'px-3 mb-1.5 text-[10px] font-mono font-semibold uppercase tracking-wider text-muted-foreground';

// Agent mode (Phase D-02 §4): a full-screen conversation with a persona.
// Left rail: persona + strategy context; center: the shared transcript;
// right rail: the selected strategy's summary, shared memory and pending
// proposals. Same conversation state as the Code-mode dock.
export function AgentMode() {
  const s = useChatSession();
  const [searchParams, setSearchParams] = useSearchParams();
  const composerRef = useRef<ChatComposerHandle>(null);
  const [leftDrawer, setLeftDrawer] = useState(false);
  const [rightDrawer, setRightDrawer] = useState(false);
  const [rightRailOpen, setRightRailOpen] = usePersistedOpen('gtb_agent_mode_right_rail', true);

  useEffect(() => {
    const prev = document.title;
    document.title = s.strategyId != null ? `Agent · strategy #${s.strategyId} · GTB` : 'Agent · GTB';
    return () => {
      document.title = prev;
    };
  }, [s.strategyId]);

  const setParam = (key: 'strategy' | 'agent', value: number | undefined) => {
    const next = new URLSearchParams(searchParams);
    if (value == null) next.delete(key);
    else next.set(key, String(value));
    setSearchParams(next);
  };

  const selectStrategy = (id: number | undefined) => {
    setParam('strategy', id);
    setLeftDrawer(false);
  };
  const selectPersona = (agent: Agent) => {
    s.selectAgent(agent.id);
    setParam('agent', agent.id);
    setLeftDrawer(false);
  };

  const prompts = s.strategyId != null ? STRATEGY_PROMPTS : GENERAL_PROMPTS;
  const hasStrategy = s.strategyId != null;

  const leftRail = (
    <LeftRail
      agents={s.agents}
      selectedAgentId={s.selectedAgent?.id}
      onSelectAgent={selectPersona}
      strategyId={s.strategyId}
      onSelectStrategy={selectStrategy}
    />
  );
  const rightRail = hasStrategy ? <RightRail strategyId={s.strategyId!} /> : null;

  return (
    <div className="h-[calc(100vh-3.5rem)] flex min-w-0 relative" data-testid="agent-mode">
      {/* Left rail: inline from lg, a drawer below. */}
      <aside className="hidden lg:flex w-64 shrink-0 border-r border-border bg-card flex-col min-h-0" aria-label="Conversation context">
        {leftRail}
      </aside>

      <section className="flex-1 min-w-0 flex flex-col" aria-label="Conversation">
        <div className="h-11 shrink-0 border-b border-border flex items-center gap-2 px-3 min-w-0">
          <button
            type="button"
            onClick={() => setLeftDrawer(true)}
            className="lg:hidden p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-accent/60"
            aria-label="Open conversation context"
            title="Persona and strategy"
          >
            <PanelLeft className="w-4 h-4" />
          </button>
          <ContextTitle strategyId={s.strategyId} agentName={s.selectedAgent?.name} paused={!!s.selectedAgent?.paused} />
          {hasStrategy && (
            <>
              <button
                type="button"
                onClick={() => setRightDrawer(true)}
                className="xl:hidden ml-auto p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-accent/60"
                aria-label="Open strategy info"
                title="Strategy info"
              >
                <PanelRightOpen className="w-4 h-4" />
              </button>
              <button
                type="button"
                onClick={() => setRightRailOpen((o) => !o)}
                className="hidden xl:inline-flex ml-auto p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-accent/60"
                aria-label={rightRailOpen ? 'Hide strategy info' : 'Show strategy info'}
                title={rightRailOpen ? 'Hide strategy info' : 'Show strategy info'}
              >
                {rightRailOpen ? <PanelRightClose className="w-4 h-4" /> : <PanelRightOpen className="w-4 h-4" />}
              </button>
            </>
          )}
        </div>

        <ChatTranscript
          density="comfortable"
          columnClassName="max-w-3xl mx-auto w-full"
          emptyState={
            <div className="flex flex-col items-center text-center gap-3 py-12">
              <MessageSquareText className="w-10 h-10 text-muted-foreground opacity-40" />
              <p className="text-sm text-muted-foreground">
                {hasStrategy
                  ? `Talk to ${s.selectedAgent?.name ?? 'the agent'} about strategy #${s.strategyId}.`
                  : `Ask ${s.selectedAgent?.name ?? 'the agent'} about your strategies, backtests and proposals.`}
              </p>
              <div className="flex flex-wrap justify-center gap-2 mt-1">
                {prompts.map((p) => (
                  <button
                    key={p}
                    type="button"
                    onClick={() => {
                      s.setDraft(p);
                      composerRef.current?.focus();
                    }}
                    className="rounded-full border border-border bg-card hover:bg-accent/60 hover:border-primary text-xs text-foreground px-3 py-1.5"
                  >
                    {p}
                  </button>
                ))}
              </div>
            </div>
          }
        />

        <div className="shrink-0 border-t border-border px-4 py-3">
          <div className="max-w-3xl mx-auto">
            <ChatComposer
              ref={composerRef}
              density="comfortable"
              value={s.draft}
              onChange={s.setDraft}
              disabled={s.sending}
              placeholder={hasStrategy ? `Message about strategy #${s.strategyId}… (Shift+Enter for a new line)` : 'Message the agent… (Shift+Enter for a new line)'}
              onSubmit={() => {
                s.send(s.draft);
                s.setDraft('');
              }}
            />
            <p className="mt-1.5 text-[11px] text-muted-foreground">
              Agents only change backtest/dryrun testing strategies; live changes need your approval.
            </p>
          </div>
        </div>
      </section>

      {/* Right rail: inline (collapsible) from xl, a drawer below. */}
      {rightRail && rightRailOpen && (
        <aside className="hidden xl:flex w-[22rem] shrink-0 border-l border-border bg-card flex-col min-h-0" aria-label="Strategy info">
          {rightRail}
        </aside>
      )}

      {leftDrawer && (
        <Drawer side="left" label="Conversation context" onClose={() => setLeftDrawer(false)}>
          {leftRail}
        </Drawer>
      )}
      {rightDrawer && rightRail && (
        <Drawer side="right" label="Strategy info" onClose={() => setRightDrawer(false)}>
          {rightRail}
        </Drawer>
      )}
    </div>
  );
}

function ContextTitle({ strategyId, agentName, paused }: { strategyId?: number; agentName?: string; paused: boolean }) {
  const { data: strategy } = useStrategy(strategyId ?? 0);
  return (
    <div className="min-w-0 flex items-center gap-2 text-sm">
      <Bot className="w-4 h-4 text-primary shrink-0" />
      <span className="font-semibold text-foreground truncate">{agentName ?? 'Agent'}</span>
      {paused && <AgentStatusBadge status="paused" />}
      <span className="text-muted-foreground shrink-0">·</span>
      <span className="text-muted-foreground truncate">
        {strategyId != null ? (
          <>
            <span className="font-mono">#{strategyId}</span> {strategy?.name ?? ''}
          </>
        ) : (
          'General'
        )}
      </span>
    </div>
  );
}

// Off-canvas rail for narrow screens: focus moves in, Escape or the
// backdrop closes it, and focus returns to the button that opened it.
function Drawer({
  side,
  label,
  onClose,
  children,
}: {
  side: 'left' | 'right';
  label: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    panelRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCloseRef.current();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      opener?.focus?.();
    };
  }, []);
  return (
    <div className="fixed inset-0 z-[60] flex" role="dialog" aria-modal="true" aria-label={label}>
      <div className="absolute inset-0 bg-background/70" onClick={onClose} />
      <div
        ref={panelRef}
        tabIndex={-1}
        className={`relative h-full bg-card border-border flex flex-col min-h-0 w-[min(22rem,calc(100vw-3rem))] focus:outline-none ${
          side === 'left' ? 'mr-auto border-r' : 'ml-auto border-l'
        }`}
      >
        <button
          type="button"
          onClick={onClose}
          aria-label={`Close ${label.toLowerCase()}`}
          className="absolute top-2 right-2 z-10 p-1 rounded text-muted-foreground hover:text-foreground hover:bg-accent/60"
        >
          <X className="w-4 h-4" />
        </button>
        {children}
      </div>
    </div>
  );
}

function LeftRail({
  agents,
  selectedAgentId,
  onSelectAgent,
  strategyId,
  onSelectStrategy,
}: {
  agents: Agent[];
  selectedAgentId?: number;
  onSelectAgent: (a: Agent) => void;
  strategyId?: number;
  onSelectStrategy: (id: number | undefined) => void;
}) {
  const { data: strategies = [], isLoading } = useStrategies();
  const { data: pending = 0 } = usePendingProposalCount();
  const [query, setQuery] = useState('');

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = [...strategies].sort((a, b) => a.name.localeCompare(b.name));
    if (!q) return list;
    return list.filter((st) => st.name.toLowerCase().includes(q) || String(st.id) === q.replace(/^#/, ''));
  }, [strategies, query]);

  return (
    <div className="flex-1 min-h-0 overflow-y-auto py-3 space-y-4">
      <section aria-label="Personas">
        <h2 className={RAIL_HEADING}>Persona</h2>
        <ul className="px-1.5 space-y-0.5">
          {agents.length === 0 && <li className="px-2 text-xs text-muted-foreground">Default agent</li>}
          {agents.map((a) => {
            const selected = a.id === selectedAgentId;
            return (
              <li key={a.id}>
                <button
                  type="button"
                  onClick={() => onSelectAgent(a)}
                  aria-pressed={selected}
                  title={a.paused ? `${a.name} is paused - it can't answer until it is resumed on the Agents page.` : a.goal || a.name}
                  className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-xs transition-colors ${
                    selected ? 'bg-accent text-accent-foreground font-semibold' : 'text-foreground hover:bg-accent/60'
                  } ${a.paused ? 'opacity-60' : ''}`}
                >
                  <Bot className="w-3.5 h-3.5 shrink-0" />
                  <span className="truncate">{a.name}</span>
                  {a.paused && <AgentStatusBadge status="paused" />}
                  <span className="ml-auto font-mono tabular-nums text-[10px] text-muted-foreground" title="Model cost today (UTC)">
                    {formatUsd(a.today_cost_usd)}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      </section>

      <section aria-label="Strategy context">
        <h2 className={RAIL_HEADING}>Context</h2>
        <div className="px-3 mb-1.5 relative">
          <Search className="w-3.5 h-3.5 absolute left-5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search strategies"
            aria-label="Search strategies"
            className="form-input w-full text-xs py-1.5 pl-7"
          />
        </div>
        <ul className="px-1.5 space-y-0.5">
          <li>
            <StrategyOption selected={strategyId == null} onClick={() => onSelectStrategy(undefined)}>
              <Globe className="w-3.5 h-3.5 shrink-0" />
              <span className="truncate">General</span>
            </StrategyOption>
          </li>
          {isLoading && <li className="px-2 text-xs text-muted-foreground">Loading strategies…</li>}
          {filtered.map((st) => (
            <li key={st.id}>
              <StrategyOption selected={st.id === strategyId} onClick={() => onSelectStrategy(st.id)}>
                <StrategyRow strategy={st} />
              </StrategyOption>
            </li>
          ))}
          {!isLoading && filtered.length === 0 && query && (
            <li className="px-2 text-xs text-muted-foreground">No strategy matches “{query}”.</li>
          )}
        </ul>
      </section>

      <nav aria-label="Agents platform" className="px-1.5 space-y-0.5 border-t border-border pt-3">
        <RailLink to="/agents" icon={<Bot className="w-3.5 h-3.5" />} label="Agents" />
        <RailLink to="/agents/reports" icon={<FileText className="w-3.5 h-3.5" />} label="Reports" />
        <RailLink
          to="/agents/proposals"
          icon={<GitPullRequest className="w-3.5 h-3.5" />}
          label="Proposals"
          badge={pending > 0 ? pending : undefined}
        />
      </nav>
    </div>
  );
}

function StrategyOption({ selected, onClick, children }: { selected: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={selected}
      className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-xs min-w-0 transition-colors ${
        selected ? 'bg-accent text-accent-foreground font-semibold' : 'text-foreground hover:bg-accent/60'
      }`}
    >
      {children}
    </button>
  );
}

function StrategyRow({ strategy: st }: { strategy: Strategy }) {
  return (
    <span className="flex flex-col gap-1 min-w-0 w-full">
      <span className="flex items-center gap-1.5 min-w-0">
        <span className="font-mono text-muted-foreground shrink-0">#{st.id}</span>
        <span className="truncate">{st.name}</span>
        {st.challenger_of_id ? (
          <span className="shrink-0 inline-flex items-center gap-0.5 text-[10px] text-primary" title={`Challenger of #${st.challenger_of_id}`}>
            <GitBranch className="w-3 h-3" />#{st.challenger_of_id}
          </span>
        ) : null}
      </span>
      <span className="flex items-center gap-1">
        <StatusBadge status={st.status} />
        <ModeBadge mode={st.mode} />
      </span>
    </span>
  );
}

function RailLink({ to, icon, label, badge }: { to: string; icon: React.ReactNode; label: string; badge?: number }) {
  return (
    <Link to={to} className="flex items-center gap-2 px-2 py-1.5 rounded-md text-xs text-muted-foreground hover:text-foreground hover:bg-accent/60">
      {icon}
      <span>{label}</span>
      {badge != null && (
        <span
          className="ml-auto min-w-[1.25rem] h-5 px-1.5 rounded-full bg-warning text-warning-foreground text-[10px] font-bold leading-5 text-center"
          aria-label={`${badge} pending`}
        >
          {badge > 99 ? '99+' : badge}
        </span>
      )}
    </Link>
  );
}

function RightRail({ strategyId }: { strategyId: number }) {
  const { data: st, isLoading, error } = useStrategy(strategyId);
  const proposals = useProposals({ status: 'pending', strategy_id: strategyId });
  const pending = useMemo(() => proposals.data?.pages.flat() ?? [], [proposals.data]);

  return (
    <div className="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
      <section aria-label="Strategy summary" className="rounded-lg border border-border bg-background/40 p-3 space-y-2">
        {isLoading ? (
          <div className="text-xs text-muted-foreground">Loading strategy #{strategyId}…</div>
        ) : error || !st ? (
          <div className="text-xs text-destructive">Strategy #{strategyId} couldn't be loaded.</div>
        ) : (
          <>
            <div className="pr-6">
              <div className="text-[10px] font-mono text-muted-foreground">#{st.id}</div>
              <h2 className="text-sm font-semibold text-foreground break-words">{st.name}</h2>
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              <ModeBadge mode={st.mode} />
              <StatusBadge status={st.status} />
              {st.challenger_of_id ? (
                <Link
                  to={`/agent?strategy=${st.challenger_of_id}`}
                  className="inline-flex items-center gap-0.5 text-[10px] text-primary hover:underline"
                >
                  <GitBranch className="w-3 h-3" /> challenger of #{st.challenger_of_id}
                </Link>
              ) : null}
            </div>
            <div className="text-xs text-muted-foreground">
              <span className="font-mono text-foreground">{(st.monitored_symbols ?? []).join(', ') || '—'}</span>
              {st.cycle ? <span> · every {st.cycle}m</span> : null}
            </div>
            <Link
              to={`/strategies/${st.id}/edit`}
              className="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:underline"
            >
              <Code2 className="w-3.5 h-3.5" /> Open in Code mode
            </Link>
          </>
        )}
      </section>

      {/* Same component + query key as the Workbench's panel, so a note
          added here shows up there without a reload (and vice versa). */}
      <AgentNotesPanel strategyId={strategyId} />

      <section aria-label="Pending proposals" className="rounded-lg border border-border bg-card">
        <h2 className="px-3 py-2 bg-secondary/60 border-b border-border text-[11px] font-semibold uppercase tracking-wider text-foreground">
          Pending proposals
        </h2>
        <div className="p-2">
          {proposals.isLoading ? (
            <div className="px-1 text-xs text-muted-foreground">Loading…</div>
          ) : pending.length === 0 ? (
            <div className="px-1 text-xs text-muted-foreground">None waiting for approval.</div>
          ) : (
            <ul className="space-y-1">
              {pending.map((p) => (
                <li key={p.id}>
                  <Link
                    to={`/agents/proposals/${p.id}`}
                    className="flex items-center gap-2 px-1.5 py-1 rounded text-xs hover:bg-accent/60 min-w-0"
                  >
                    <span className="font-mono text-muted-foreground shrink-0">#{p.id}</span>
                    <span className="truncate text-foreground">{proposalKindLabel(p.kind)}</span>
                    <ProposalStatusBadge status={p.status} />
                    <span className="ml-auto text-[10px] text-muted-foreground whitespace-nowrap">{formatRelative(p.created_at)}</span>
                    <ArrowUpRight className="w-3 h-3 shrink-0 text-muted-foreground" />
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>
    </div>
  );
}
