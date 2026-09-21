import React, { useRef, useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { Bot, Send, AlertTriangle, History, X, MessageSquareText, Maximize2, Minimize2 } from 'lucide-react';
import { AgentRun } from '@/api/types';
import { useSendAgentMessage } from '@/hooks/queries';
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
  run?: AgentRun;
  failed?: string;
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
    setTurns((prev) => [...prev, { id: turnId, input: text }]);
    setInput('');

    sendMessage.mutate(
      { input: text, strategyId: effectiveStrategyId, history },
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
          className="fixed bottom-5 right-5 z-[60] w-14 h-14 rounded-full bg-green-700 hover:bg-green-600 text-black shadow-lg shadow-black/50 border border-green-500/50 flex items-center justify-center transition-transform hover:scale-105"
        >
          <Bot className="w-6 h-6" />
        </button>
      )}

      {open && (
        <div
          className={
            expanded
              ? 'fixed bottom-5 right-5 z-[60] w-[min(64rem,calc(100vw-2.5rem))] h-[calc(100vh-3rem)] flex flex-col bg-black border border-green-800/60 rounded-lg shadow-2xl shadow-black/60'
              : 'fixed bottom-5 right-5 z-[60] w-[26rem] max-w-[calc(100vw-2.5rem)] h-[36rem] max-h-[calc(100vh-3rem)] flex flex-col bg-black border border-green-800/60 rounded-lg shadow-2xl shadow-black/60'
          }
        >
          {/* Header */}
          <div className="flex items-center gap-2 px-3 py-2.5 border-b border-green-950/60 shrink-0">
            <Bot className="w-4 h-4 text-green-500 shrink-0" />
            <span className="text-sm font-bold text-green-300">AI Strategy Copilot</span>
            {editorBridge && (
              <span
                className="text-[10px] font-mono text-green-800 bg-green-950/40 border border-green-900/40 rounded px-1.5 py-0.5"
                title="Sent as context with every message"
              >
                {effectiveStrategyId != null ? `#${effectiveStrategyId}` : 'new strategy'}
              </span>
            )}
            <Link
              to="/agent/history"
              className="ml-auto text-green-700 hover:text-green-400 p-1 rounded hover:bg-green-950/30"
              title="View agent run history"
            >
              <History className="w-4 h-4" />
            </Link>
            <button
              onClick={() => setExpanded((e) => !e)}
              aria-label={expanded ? 'Shrink copilot panel' : 'Expand copilot panel'}
              title={expanded ? 'Shrink panel' : 'Expand panel'}
              className="text-green-700 hover:text-green-400 p-1 rounded hover:bg-green-950/30"
            >
              {expanded ? <Minimize2 className="w-4 h-4" /> : <Maximize2 className="w-4 h-4" />}
            </button>
            <button
              onClick={() => setOpen(false)}
              aria-label="Collapse copilot"
              className="text-green-700 hover:text-green-400 p-1 rounded hover:bg-green-950/30"
            >
              <X className="w-4 h-4" />
            </button>
          </div>

          <p className="px-3 pt-2 text-[10px] text-green-800 shrink-0">
            Can only save strategies as <span className="font-mono">testing</span>/
            <span className="font-mono">backtest</span>-or-<span className="font-mono">dryrun</span> - never live.
          </p>

          {/* Transcript */}
          <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2 space-y-3">
            {turns.length === 0 && (
              <div className="h-full flex flex-col items-center justify-center text-center text-green-800 gap-2 py-8">
                <MessageSquareText className="w-8 h-8 opacity-40" />
                <p className="text-xs">Ask the agent to inspect strategies, run backtests, or draft a script.</p>
                <p className="text-[11px] font-mono text-green-700 px-2">&ldquo;{EXAMPLE_PROMPT}&rdquo;</p>
              </div>
            )}

            {turns.map((turn) => (
              <div key={turn.id} className="space-y-2">
                <div className="flex justify-end">
                  <div className="max-w-[85%] rounded-lg bg-green-900/30 border border-green-800/40 text-green-100 text-xs px-3 py-2 whitespace-pre-wrap break-words">
                    {turn.input}
                  </div>
                </div>

                {!turn.run && !turn.failed && (
                  <div className="flex items-center gap-2 text-green-700 text-xs pl-1">
                    <Spinner size="sm" />
                    <span>Agent is working…</span>
                  </div>
                )}

                {turn.failed && (
                  <div className="flex items-start gap-2 rounded border border-rose-900/50 bg-rose-950/20 text-rose-300 text-xs px-3 py-2">
                    <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
                    <div>
                      <div className="font-semibold">Request failed</div>
                      <div className="text-rose-400/90 mt-0.5">{turn.failed}</div>
                    </div>
                  </div>
                )}

                {turn.run && (
                  <div className="space-y-2 pl-1">
                    {turn.run.status === 'error' && (
                      <div className="flex items-start gap-2 rounded border border-rose-900/50 bg-rose-950/20 text-rose-300 text-xs px-3 py-2">
                        <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
                        <div>
                          <div className="font-semibold">Agent run failed</div>
                          <div className="text-rose-400/90 mt-0.5">
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
                      <div className="max-w-[90%] rounded-lg bg-black/40 border border-green-950/60 text-green-100 px-3 py-2">
                        <MarkdownMessage content={turn.run.response_text} />
                      </div>
                    )}

                    {turn.run.status === 'ok' &&
                      !turn.run.response_text &&
                      turn.run.tool_calls.length === 0 && (
                        <div className="text-xs text-green-700 italic">Agent responded with no text and no tool calls.</div>
                      )}
                  </div>
                )}
              </div>
            ))}
            <div ref={transcriptEndRef} />
          </div>

          {/* Composer */}
          <form onSubmit={handleSubmit} className="flex items-center gap-2 p-2.5 border-t border-green-950/60 shrink-0">
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
              className="bg-green-700 hover:bg-green-600 disabled:opacity-50 disabled:cursor-not-allowed text-black font-semibold rounded px-3 py-1.5 flex items-center gap-1 text-xs shrink-0"
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
