import React, { useEffect, useLayoutEffect, useRef } from 'react';
import { AlertTriangle, History } from 'lucide-react';
import { apiErrorMessage } from '@/api/client';
import { useChatSession } from '@/context/ChatSessionContext';
import { Spinner } from '@/components/ui/Spinner';
import { ChatTurn } from './ChatTurn';
import { ApplyTarget, ChatDensity } from './types';

// The shared transcript of the current conversation context (Phase D-02 §6):
// server-hydrated turns (with "Load earlier" at the top), then this
// session's turns. Owns its scroll container.
export function ChatTranscript({
  density,
  apply,
  emptyState,
  columnClassName = '',
}: {
  density: ChatDensity;
  apply?: ApplyTarget;
  emptyState?: React.ReactNode;
  /** Classes for the inner reading column (e.g. a max-width in Agent mode). */
  columnClassName?: string;
}) {
  const s = useChatSession();
  const scrollRef = useRef<HTMLDivElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const compact = density === 'compact';

  // Keep the viewport anchored when older turns are prepended.
  const prependAnchor = useRef<number | null>(null);
  const loadEarlier = () => {
    prependAnchor.current = scrollRef.current ? scrollRef.current.scrollHeight - scrollRef.current.scrollTop : null;
    s.loadEarlier();
  };
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && prependAnchor.current != null && !s.loadingEarlier) {
      el.scrollTop = el.scrollHeight - prependAnchor.current;
      prependAnchor.current = null;
    }
  }, [s.hydratedTurns.length, s.loadingEarlier]);

  // New live turns (or a reply arriving) scroll to the bottom; so does
  // switching context or the first hydrated page landing.
  const lastLive = s.liveTurns[s.liveTurns.length - 1];
  const liveSignature = `${s.liveTurns.length}:${lastLive?.run ? 'r' : lastLive?.failed || lastLive?.blocked ? 'x' : 'p'}`;
  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'end' });
  }, [liveSignature, s.contextKey]);
  const hydratedLoaded = !s.historyLoading;
  useEffect(() => {
    if (hydratedLoaded && prependAnchor.current == null) endRef.current?.scrollIntoView({ block: 'end' });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hydratedLoaded, s.contextKey]);

  const empty = s.hydratedTurns.length === 0 && s.liveTurns.length === 0;

  return (
    <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto" data-testid="chat-transcript" aria-live="polite">
      <div className={`${compact ? 'px-3 py-2 space-y-3' : 'px-4 py-4 space-y-5'} ${columnClassName}`}>
        {s.hasEarlier && (
          <div className="flex justify-center">
            <button
              type="button"
              onClick={loadEarlier}
              disabled={s.loadingEarlier}
              className="inline-flex items-center gap-1.5 bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs py-1 px-3 disabled:opacity-50"
            >
              {s.loadingEarlier ? <Spinner size="sm" /> : <History className="w-3.5 h-3.5" />}
              Load earlier
            </button>
          </div>
        )}

        {s.historyLoading && (
          <div role="status" className="flex items-center justify-center gap-2 text-xs text-muted-foreground py-2">
            <Spinner size="sm" /> Loading conversation…
          </div>
        )}

        {s.historyError != null && (
          <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
            <AlertTriangle className="w-3.5 h-3.5 text-warning shrink-0" />
            <span>Earlier conversations couldn't be loaded: {apiErrorMessage(s.historyError)}</span>
          </div>
        )}

        {s.hydratedTurns.length > 0 && (
          <Divider
            label={
              s.hydratedInModelContext
                ? 'Earlier conversations'
                : "Earlier conversations (not in the agent's context)"
            }
          />
        )}

        {s.hydratedTurns.map((t) => (
          <ChatTurn key={t.id} turn={t} density={density} apply={apply} />
        ))}

        {s.hydratedTurns.length > 0 && s.liveTurns.length > 0 && <Divider label="This session" />}

        {s.liveTurns.map((t) => (
          <ChatTurn key={t.id} turn={t} density={density} apply={apply} />
        ))}

        {empty && !s.historyLoading && emptyState}
        <div ref={endRef} />
      </div>
    </div>
  );
}

function Divider({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 text-[10px] uppercase tracking-wide text-muted-foreground" role="separator">
      <span className="flex-1 border-t border-border" />
      <span className="text-center">{label}</span>
      <span className="flex-1 border-t border-border" />
    </div>
  );
}
