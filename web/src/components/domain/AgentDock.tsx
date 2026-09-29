import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Link, useNavigate } from 'react-router-dom';
import { History, Maximize2, MessageSquareText, X } from 'lucide-react';
import { useChatSession, agentModeHref } from '@/context/ChatSessionContext';
import { useEditorBridge } from '@/context/EditorBridgeContext';
import { usePersistedNumber } from '@/hooks/usePersistedOpen';
import { ApplyScriptDialog } from '@/components/domain/ApplyScriptDialog';
import { ChatTranscript } from '@/components/domain/agentchat/ChatTranscript';
import { ChatComposer } from '@/components/domain/agentchat/ChatComposer';
import { ApplyTarget } from '@/components/domain/agentchat/types';

const MIN_WIDTH = 320;
const MAX_WIDTH = 720;
const DEFAULT_WIDTH = 400;
// Room the rest of the page keeps next to the dock (sidebar rail + a usable
// content column) so a wide dock can't force a horizontal page scroll.
const RESERVED_PX = 56 + 360;

function clampWidth(w: number): number {
  const viewportMax = typeof window === 'undefined' ? MAX_WIDTH : window.innerWidth - RESERVED_PX;
  return Math.round(Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, viewportMax, w)));
}

export const AGENT_DOCK_OPEN_KEY = 'gtb_agent_dock_open';
const AGENT_DOCK_WIDTH_KEY = 'gtb_agent_dock_width';

// Code-mode chat (Phase D-02 §5): a resizable right-hand panel in
// AppLayout's flex row, so it pushes the page aside instead of covering it.
// Same conversation as Agent mode (ChatSessionContext). Inside a Strategy
// Workbench the context is that strategy and code cards can be applied to
// the editor via EditorBridgeContext + ApplyScriptDialog.
export function AgentDock({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const s = useChatSession();
  const navigate = useNavigate();
  const bridge = useEditorBridge();
  const [storedWidth, setWidth, commitWidth] = usePersistedNumber(AGENT_DOCK_WIDTH_KEY, DEFAULT_WIDTH);
  const width = clampWidth(storedWidth);
  const [pendingApply, setPendingApply] = useState<string | null>(null);
  const [, forceRerender] = useState(0);

  // Re-clamp on window resize.
  useEffect(() => {
    const onResize = () => forceRerender((n) => n + 1);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  // --- drag-to-resize ---------------------------------------------------
  const dragging = useRef(false);
  const onHandleDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      dragging.current = true;
      let latest = width;
      const move = (ev: MouseEvent) => {
        latest = clampWidth(window.innerWidth - ev.clientX);
        setWidth(latest);
      };
      const up = () => {
        dragging.current = false;
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        commitWidth(latest);
      };
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';
      window.addEventListener('mousemove', move);
      window.addEventListener('mouseup', up);
    },
    [width, setWidth, commitWidth],
  );
  const onHandleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      commitWidth(clampWidth(width + (e.key === 'ArrowLeft' ? 24 : -24)));
    }
  };

  if (!open) {
    return (
      <div className="shrink-0 w-9 sticky top-14 h-[calc(100vh-3.5rem)] self-start border-l border-border bg-card flex flex-col items-center pt-3">
        <button
          type="button"
          onClick={() => onOpenChange(true)}
          title="Open the agent chat (Ctrl+.)"
          aria-label="Open the agent chat"
          data-testid="agent-dock-open"
          className="flex flex-col items-center gap-2 px-1 py-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60"
        >
          <img src="/gopher-face.png" alt="" className="w-6 h-6 rounded-full object-cover" />
          <span className="text-[11px] font-semibold tracking-wide [writing-mode:vertical-rl] rotate-180">Agent</span>
        </button>
      </div>
    );
  }

  const applyTarget: ApplyTarget | undefined = bridge
    ? { strategyId: bridge.strategyId ?? s.strategyId, onApply: (source) => setPendingApply(source) }
    : undefined;

  const chip = s.strategyId != null ? `#${s.strategyId}` : s.contextKey === 'new' ? 'new strategy' : 'General';

  return (
    <aside
      aria-label="Agent chat"
      data-testid="agent-dock"
      className="shrink-0 sticky top-14 h-[calc(100vh-3.5rem)] self-start border-l border-border bg-background flex"
      style={{ width }}
    >
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize agent chat"
        aria-valuemin={MIN_WIDTH}
        aria-valuemax={MAX_WIDTH}
        aria-valuenow={width}
        tabIndex={0}
        onMouseDown={onHandleDown}
        onKeyDown={onHandleKey}
        title="Drag to resize"
        className="w-1.5 shrink-0 cursor-col-resize bg-transparent hover:bg-primary/40 focus-visible:bg-primary/40 active:bg-primary/60 transition-colors focus:outline-none"
      />
      <div className="flex-1 min-w-0 flex flex-col">
        <div className="flex items-center gap-1.5 px-2.5 h-11 border-b border-border shrink-0 min-w-0">
          <img src="/gopher-face.png" alt="" className="w-5 h-5 rounded-full object-cover shrink-0" />
          {s.agents.length > 0 ? (
            <select
              value={s.selectedAgent?.id ?? ''}
              onChange={(e) => s.selectAgent(Number(e.target.value))}
              aria-label="Agent persona answering in this chat"
              title="Which agent persona answers"
              className="min-w-0 max-w-[10rem] truncate bg-secondary border border-border text-[11px] rounded px-1.5 py-0.5 text-foreground focus:outline-none focus:border-primary"
            >
              {s.agents.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                  {a.paused ? ' (paused)' : ''}
                </option>
              ))}
            </select>
          ) : (
            <span className="text-sm font-bold text-foreground">Agent</span>
          )}
          <span
            className="text-[10px] font-mono text-muted-foreground bg-card border border-border rounded px-1.5 py-0.5 whitespace-nowrap"
            title="Conversation context - sent with every message"
          >
            {chip}
          </span>
          <div className="ml-auto flex items-center gap-0.5 shrink-0">
            <button
              type="button"
              onClick={() => navigate(agentModeHref(s.strategyId, s.selectedAgent?.id))}
              title="Expand to Agent mode (Ctrl+Shift+.)"
              aria-label="Expand to Agent mode"
              data-testid="agent-dock-expand"
              className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/60"
            >
              <Maximize2 className="w-4 h-4" />
            </button>
            <Link
              to="/activity?tab=agent"
              className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/60"
              title="Agent run history"
              aria-label="Agent run history"
            >
              <History className="w-4 h-4" />
            </Link>
            <button
              type="button"
              onClick={() => onOpenChange(false)}
              title="Close (Ctrl+.)"
              aria-label="Close the agent chat"
              className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/60"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        <p className="px-3 pt-2 text-[10px] text-muted-foreground shrink-0">
          Agents only change backtest/dryrun testing strategies; live changes need your approval.
        </p>

        <ChatTranscript
          density="compact"
          apply={applyTarget}
          emptyState={
            <div className="flex flex-col items-center justify-center text-center text-muted-foreground gap-2 py-10">
              <MessageSquareText className="w-8 h-8 opacity-40" />
              <p className="text-xs">
                {s.strategyId != null
                  ? `Ask about strategy #${s.strategyId}: backtest it, explain its trades, or draft a change.`
                  : 'Ask the agent to inspect strategies, run backtests, or draft a script.'}
              </p>
            </div>
          }
        />

        <div className="p-2.5 border-t border-border shrink-0">
          <ChatComposer
            density="compact"
            value={s.draft}
            onChange={s.setDraft}
            disabled={s.sending}
            onSubmit={() => {
              s.send(s.draft);
              s.setDraft('');
            }}
          />
        </div>
      </div>

      {/* Portaled: the sticky dock is its own stacking context, which would
          otherwise leave the header/sidebar painted over the dialog. */}
      {pendingApply != null &&
        bridge &&
        createPortal(
          <ApplyScriptDialog
            currentSource={bridge.currentSource}
            proposedSource={pendingApply}
            onCancel={() => setPendingApply(null)}
            onConfirm={() => {
              bridge.applyScript(pendingApply);
              setPendingApply(null);
            }}
          />,
          document.body,
        )}
    </aside>
  );
}
