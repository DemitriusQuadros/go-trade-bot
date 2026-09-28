import React, { useRef, useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { Send, AlertTriangle, History, X, MessageSquareText, Maximize2, Minimize2 } from 'lucide-react';
import { AgentRun } from '@/api/types';
import { ApiError, apiErrorMessage } from '@/api/client';
import { useAgents, useSendAgentMessage } from '@/hooks/queries';
import { formatUsd } from '@/lib/time';
import { AgentToolCallCard } from '@/components/domain/AgentToolCallCard';
import { MarkdownMessage } from '@/components/domain/MarkdownMessage';
import { ApplyScriptDialog } from '@/components/domain/ApplyScriptDialog';
import { Spinner } from '@/components/ui/Spinner';
import { useEditorBridge } from '@/context/EditorBridgeContext';

// One transcript entry: the operator's prompt paired with the AgentRun it
// produced (undefined while the request is in flight). Mirrors the old
// full-page AgentChat.tsx's Turn shape - this widget replaces that page
// entirely, just mounted once at the app-shell level (App.tsx) instead of
// behind its own route, so the transcript survives client-side navigation
// for free (React state, not persisted - see the task's persistence scope).
interface Turn {
  id: string;
  input: string;
  // The persona this turn was sent to (A-03 §8) - shown on the reply even
  // before/without the run's own agent_name.
  agentName?: string;
  run?: AgentRun;
  failed?: string;
  // 409 from POST /agent/runs: the agent (or every agent, via the global
  // kill switch) is paused - rendered as a notice, not a failure.
  blocked?: string;
}

// Which persona answers in the widget, remembered in this browser.
// localStorage can throw (private mode, blocked storage) - never fatal.
const AGENT_STORAGE_KEY = 'gtb_copilot_agent_id';

function loadStoredAgentId(): number | undefined {
  try {
    const raw = localStorage.getItem(AGENT_STORAGE_KEY);
    const n = raw ? Number(raw) : NaN;
    return Number.isFinite(n) && n > 0 ? n : undefined;
  } catch {
    return undefined;
  }
}

function storeAgentId(id: number | undefined) {
  try {
    if (id == null) localStorage.removeItem(AGENT_STORAGE_KEY);
    else localStorage.setItem(AGENT_STORAGE_KEY, String(id));
  } catch {
    /* storage unavailable - selection just won't persist */
  }
}

const EXAMPLE_PROMPT = 'List my strategies and tell me which ones have no recent backtests.';

// AgentCopilotWidget is the floating "copilot" launcher + panel that
// replaces the old standalone /agent page and sidebar entry. Mounted once in
// App.tsx alongside <Routes> so it persists across every route instead of
// being tied to one page's lifecycle.
export function AgentCopilotWidget() {
  const [open, setOpen] = useState(false);
  // Long, markdown-formatted answers (a full strategy summary, a script
  // diff walkthrough) cramp badly in the default compact panel - this lets
  // the operator expand it to a much larger size on demand rather than
  // scrolling a tiny box, without permanently changing the default
  // (unobtrusive, out-of-the-way) footprint every other page interaction
  // expects.
  const [expanded, setExpanded] = useState(false);
  const [turns, setTurns] = useState<Turn[]>([]);
  const [input, setInput] = useState('');
  const [pendingApply, setPendingApply] = useState<string | null>(null);
  // Closes the "drafting a brand-new strategy" memory gap: while on
  // /strategies/new, editorBridge.strategyId is undefined (nothing exists
  // yet), so memory for that draft can only live in this tab's transcript.
  // The moment the agent actually creates the strategy mid-conversation
  // (save_strategy_script succeeds, the resulting AgentRun carries a real
  // strategy_id), this switches the session's memory key over to that id -
  // every message from then on is DB-backed per-strategy memory (see
  // historyFromPersistedRuns on the backend), same as if the operator had
  // been editing an already-existing strategy the whole time.
  const [createdStrategyId, setCreatedStrategyId] = useState<number | undefined>(undefined);
  const sendMessage = useSendAgentMessage();
  const { data: agents = [] } = useAgents();
  const [storedAgentId, setStoredAgentId] = useState<number | undefined>(() => loadStoredAgentId());
  const defaultAgent = agents.find((a) => a.is_default);
  // A remembered id that no longer exists (agent deleted) falls back to the
  // default "Copilot" persona.
  const selectedAgent = agents.find((a) => a.id === storedAgentId) ?? defaultAgent;
  const selectAgent = (id: number) => {
    setStoredAgentId(id);
    storeAgentId(id);
  };
  const transcriptEndRef = useRef<HTMLDivElement>(null);
  const editorBridge = useEditorBridge();

  // editorBridge.strategyId (an already-existing strategy the operator
  // navigated to directly) always wins when present; createdStrategyId only
  // fills in for a still-in-progress "new strategy" draft, and only while
  // still inside the workbench at all - leaving it (editorBridge becomes
  // undefined) resets the tracked id so a later, unrelated "new strategy"
  // draft doesn't inherit a previous one's memory by mistake.
  const effectiveStrategyId = editorBridge?.strategyId ?? (editorBridge ? createdStrategyId : undefined);

  useEffect(() => {
    if (!editorBridge) setCreatedStrategyId(undefined);
  }, [editorBridge]);

  useEffect(() => {
    if (open) transcriptEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [turns, open]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const text = input.trim();
    if (!text || sendMessage.isPending) return;

    // Every prior turn of THIS session that actually completed - the
    // agent's only memory of the conversation so far. Without this, every
    // message started a brand-new RunToolLoop from scratch: asking it to
    // draft a script and then, in the next message, "now tighten the stop
    // loss" had no script to refer to at all. A failed/still-pending turn
    // has nothing coherent to replay, so it's excluded.
    const history = turns
      .filter((t) => t.run && t.run.status === 'ok')
      .map((t) => ({
        input: t.input,
        tool_calls: t.run!.tool_calls,
        response_text: t.run!.response_text,
      }));

    const turnId = `${Date.now()}-${Math.random()}`;
    const agentName = selectedAgent?.name;
    setTurns((prev) => [...prev, { id: turnId, input: text, agentName }]);
    setInput('');

    sendMessage.mutate(
      // Omitted agent_id = the backend's default agent (agents list not
      // loaded yet, or an older backend without personas).
      { input: text, strategyId: effectiveStrategyId, history, agentId: selectedAgent?.id },
      {
        onSuccess: (run) => {
          setTurns((prev) => prev.map((t) => (t.id === turnId ? { ...t, run } : t)));
          // The draft just became a real strategy - switch this session's
          // memory key over to it (see createdStrategyId's doc comment).
          if (editorBridge && !effectiveStrategyId && run.strategy_id != null) {
            setCreatedStrategyId(run.strategy_id);
          }
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 409) {
            const msg = apiErrorMessage(err, 'This agent is paused.');
            setTurns((prev) => prev.map((t) => (t.id === turnId ? { ...t, blocked: msg } : t)));
            return;
          }
          setTurns((prev) =>
            prev.map((t) => (t.id === turnId ? { ...t, failed: err instanceof Error ? err.message : String(err) } : t))
          );
        },
      }
    );
  };

  return (
    <>
      {/* Launcher bubble - collapsed state, persistent bottom-right on every
          route. Hidden while the panel is open so there's exactly one
          floating control at a time. */}
      {!open && (
        <button
          onClick={() => setOpen(true)}
          aria-label="Open AI strategy copilot"
          title="AI Strategy Copilot"
          className="fixed bottom-5 right-5 z-[60] w-14 h-14 rounded-full bg-primary shadow-lg shadow-black/50 border border-primary/50 flex items-center justify-center overflow-hidden transition-transform hover:scale-105"
        >
          <img src="/gopher-face.png" alt="" className="w-full h-full object-cover" />
        </button>
      )}

      {open && (
        <div
          className={
            expanded
              ? 'fixed bottom-5 right-5 z-[60] w-[min(64rem,calc(100vw-2.5rem))] h-[calc(100vh-3rem)] flex flex-col bg-background border border-border/60 rounded-lg shadow-2xl shadow-black/60'
              : 'fixed bottom-5 right-5 z-[60] w-[26rem] max-w-[calc(100vw-2.5rem)] h-[36rem] max-h-[calc(100vh-3rem)] flex flex-col bg-background border border-border/60 rounded-lg shadow-2xl shadow-black/60'
          }
        >
          {/* Header */}
          <div className="flex items-center gap-2 px-3 py-2.5 border-b border-border/60 shrink-0">
            <img src="/gopher-face.png" alt="" className="w-5 h-5 rounded-full object-cover shrink-0" />
            <span className="text-sm font-bold text-foreground whitespace-nowrap">AI Copilot</span>
            {agents.length > 0 && (
              <select
                value={selectedAgent?.id ?? ''}
                onChange={(e) => selectAgent(Number(e.target.value))}
                aria-label="Agent persona answering in this chat"
                title="Which agent persona answers"
                className="min-w-0 max-w-[9rem] truncate bg-secondary border border-border text-[11px] rounded px-1.5 py-0.5 text-foreground focus:outline-none focus:border-primary"
              >
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                    {a.paused ? ' (paused)' : ''}
                  </option>
                ))}
              </select>
            )}
            {editorBridge && (
              <span
                className="text-[10px] font-mono text-muted-foreground bg-card/40 border border-border/40 rounded px-1.5 py-0.5"
                title="Sent as context with every message"
              >
                {effectiveStrategyId != null ? `#${effectiveStrategyId}` : 'new strategy'}
              </span>
            )}
            <Link
              to="/activity?tab=agent"
              className="ml-auto text-muted-foreground hover:text-foreground p-1 rounded hover:bg-card/30"
              title="View agent run history"
            >
              <History className="w-4 h-4" />
            </Link>
            <button
              onClick={() => setExpanded((e) => !e)}
              aria-label={expanded ? 'Shrink copilot panel' : 'Expand copilot panel'}
              title={expanded ? 'Shrink panel' : 'Expand panel'}
              className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-card/30"
            >
              {expanded ? <Minimize2 className="w-4 h-4" /> : <Maximize2 className="w-4 h-4" />}
            </button>
            <button
              onClick={() => setOpen(false)}
              aria-label="Collapse copilot"
              className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-card/30"
            >
              <X className="w-4 h-4" />
            </button>
          </div>

          <p className="px-3 pt-2 text-[10px] text-muted-foreground shrink-0">
            Can only save strategies as <span className="font-mono">testing</span>/
            <span className="font-mono">backtest</span>-or-<span className="font-mono">dryrun</span> - never live.
          </p>

          {/* Transcript */}
          <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2 space-y-3">
            {turns.length === 0 && (
              <div className="h-full flex flex-col items-center justify-center text-center text-muted-foreground gap-2 py-8">
                <MessageSquareText className="w-8 h-8 opacity-40" />
                <p className="text-xs">Ask the agent to inspect strategies, run backtests, or draft a script.</p>
                <p className="text-[11px] font-mono text-muted-foreground px-2">&ldquo;{EXAMPLE_PROMPT}&rdquo;</p>
              </div>
            )}

            {turns.map((turn) => (
              <div key={turn.id} className="space-y-2">
                <div className="flex justify-end">
                  <div className="max-w-[85%] rounded-lg bg-secondary/30 border border-border/40 text-foreground text-xs px-3 py-2 whitespace-pre-wrap break-words">
                    {turn.input}
                  </div>
                </div>

                {!turn.run && !turn.failed && !turn.blocked && (
                  <div className="flex items-center gap-2 text-muted-foreground text-xs pl-1">
                    <Spinner size="sm" />
                    <span>{turn.agentName ?? 'Agent'} is working…</span>
                  </div>
                )}

                {turn.blocked && (
                  <div role="status" className="flex items-start gap-2 rounded border border-warning/40 bg-warning/15 text-foreground text-xs px-3 py-2">
                    <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5 text-warning" />
                    <div>
                      <div className="font-semibold">{turn.agentName ?? 'This agent'} can't answer right now</div>
                      <div className="text-muted-foreground mt-0.5">{turn.blocked}</div>
                      <Link to="/agents" className="inline-block mt-1 text-primary hover:underline">
                        Open Agents
                      </Link>
                    </div>
                  </div>
                )}

                {turn.failed && (
                  <div className="flex items-start gap-2 rounded border border-destructive/40 bg-destructive/15 text-destructive text-xs px-3 py-2">
                    <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
                    <div>
                      <div className="font-semibold">Request failed</div>
                      <div className="text-destructive/90 mt-0.5">{turn.failed}</div>
                    </div>
                  </div>
                )}

                {turn.run && (
                  <div className="space-y-2 pl-1">
                    <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                      <span className="font-semibold text-foreground">{turn.run.agent_name || turn.agentName || 'Copilot'}</span>
                      {turn.run.cost_usd != null && (
                        <span className="font-mono" title="Model cost of this turn">
                          {formatUsd(turn.run.cost_usd)}
                        </span>
                      )}
                    </div>
                    {turn.run.status === 'error' && (
                      <div className="flex items-start gap-2 rounded border border-destructive/40 bg-destructive/15 text-destructive text-xs px-3 py-2">
                        <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
                        <div>
                          <div className="font-semibold">Agent run failed</div>
                          <div className="text-destructive/90 mt-0.5">
                            {turn.run.error_message || 'No error message was recorded for this run.'}
                          </div>
                        </div>
                      </div>
                    )}

                    {turn.run.tool_calls.length > 0 && (
                      <div className="space-y-2">
                        {turn.run.tool_calls.map((call, i) => (
                          <AgentToolCallCard
                            key={i}
                            call={call}
                            onApplyToEditor={editorBridge ? (source) => setPendingApply(source) : undefined}
                          />
                        ))}
                      </div>
                    )}

                    {/* The model's actual answer - previously computed
                        server-side and silently discarded, so this bubble
                        never rendered no matter how many tool calls
                        preceded it. See entities.AgentRun.ResponseText. */}
                    {turn.run.status === 'ok' && turn.run.response_text && (
                      <div className="max-w-[90%] rounded-lg bg-background/40 border border-border/60 text-foreground px-3 py-2">
                        <MarkdownMessage content={turn.run.response_text} />
                      </div>
                    )}

                    {turn.run.status === 'ok' &&
                      !turn.run.response_text &&
                      turn.run.tool_calls.length === 0 && (
                        <div className="text-xs text-muted-foreground italic">Agent responded with no text and no tool calls.</div>
                      )}
                  </div>
                )}
              </div>
            ))}
            <div ref={transcriptEndRef} />
          </div>

          {/* Composer */}
          <form onSubmit={handleSubmit} className="flex items-center gap-2 p-2.5 border-t border-border/60 shrink-0">
            <input
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="Ask the agent…"
              disabled={sendMessage.isPending}
              className="form-input flex-1 text-xs py-1.5"
              aria-label="Message to the AI strategy agent"
            />
            <button
              type="submit"
              disabled={sendMessage.isPending || !input.trim()}
              className="bg-primary hover:bg-primary disabled:opacity-50 disabled:cursor-not-allowed text-primary-foreground font-semibold rounded px-3 py-1.5 flex items-center gap-1 text-xs shrink-0"
            >
              {sendMessage.isPending ? <Spinner size="sm" /> : <Send className="w-3.5 h-3.5" />}
            </button>
          </form>
        </div>
      )}

      {pendingApply != null && editorBridge && (
        <ApplyScriptDialog
          currentSource={editorBridge.currentSource}
          proposedSource={pendingApply}
          onCancel={() => setPendingApply(null)}
          onConfirm={() => {
            editorBridge.applyScript(pendingApply);
            setPendingApply(null);
          }}
        />
      )}
    </>
  );
}
