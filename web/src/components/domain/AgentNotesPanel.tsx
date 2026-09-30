import React, { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowUpRight, Loader2, Send } from 'lucide-react';
import { apiErrorMessage } from '@/api/client';
import { useAddStrategyMemoryNote, useStrategyMemory } from '@/hooks/queries';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import { MemoryKindBadge } from '@/components/domain/AgentBadges';
import { formatRelative } from '@/lib/time';
import { useAuth } from '@/context/AuthContext';

// "Agent notes" (A-03 §9): the strategy's shared memory - journal entries,
// findings and report references written by every agent, plus operator
// notes - newest first, with "Load more" paging via before_id. Operator
// notes added here are part of what agents read on their next run.
export function AgentNotesPanel({ strategyId }: { strategyId: number }) {
  const { data, isLoading, error, fetchNextPage, hasNextPage, isFetchingNextPage } = useStrategyMemory(strategyId);
  const addNote = useAddStrategyMemoryNote(strategyId);
  const [draft, setDraft] = useState('');
  const [addError, setAddError] = useState<string | null>(null);
  // "Add note" needs `edit_drafts` (auth-02 §4); reading is for everyone.
  const canNote = useAuth().can('edit_drafts');

  const entries = useMemo(() => data?.pages.flat() ?? [], [data]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const content = draft.trim();
    if (!content || addNote.isPending) return;
    setAddError(null);
    addNote.mutate(content, {
      onSuccess: () => setDraft(''),
      onError: (err) => setAddError(apiErrorMessage(err, 'Failed to add the note')),
    });
  };

  return (
    <CollapsibleSection
      id="workbench.agentNotes"
      title="Agent Notes"
      subtitle={entries.length ? `(${entries.length}${hasNextPage ? '+' : ''})` : undefined}
      defaultOpen={false}
    >
      {canNote && (
      <form onSubmit={submit} className="flex items-center gap-2 mb-2">
        <input
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Add a note for agents working on this strategy..."
          aria-label="New agent note"
          maxLength={4000}
          className="form-input text-xs py-1.5 flex-1"
        />
        <button
          type="submit"
          disabled={!draft.trim() || addNote.isPending}
          className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs px-2.5 py-1.5 flex items-center gap-1 disabled:opacity-50 shrink-0"
        >
          {addNote.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Send className="w-3.5 h-3.5" />}
          Add note
        </button>
      </form>
      )}
      {addError && (
        <div role="alert" className="mb-2 text-xs text-destructive bg-destructive/15 border border-destructive/40 rounded px-2 py-1.5">
          {addError}
        </div>
      )}

      {isLoading ? (
        <div className="text-xs text-muted-foreground flex items-center gap-2">
          <Loader2 className="w-3.5 h-3.5 animate-spin" /> Loading notes...
        </div>
      ) : error ? (
        <div className="text-xs text-destructive">Couldn't load notes: {apiErrorMessage(error)}</div>
      ) : entries.length === 0 ? (
        <div className="text-xs text-muted-foreground">
          No notes yet. Agents record findings here as they evaluate this strategy.
        </div>
      ) : (
        <ul className="space-y-1.5">
          {entries.map((m) => (
            <li key={m.id} className="rounded border border-border/60 bg-background/40 px-2.5 py-1.5">
              <div className="flex items-center gap-2 text-[11px]">
                <span className="font-semibold text-foreground">{m.author_name || 'operator'}</span>
                <MemoryKindBadge kind={m.kind} />
                <span className="ml-auto text-muted-foreground whitespace-nowrap" title={new Date(m.created_at).toLocaleString()}>
                  {formatRelative(m.created_at)}
                </span>
              </div>
              <div className="mt-1 text-xs text-foreground whitespace-pre-wrap break-words">{m.content}</div>
              {m.kind === 'report_ref' && m.ref_id != null && (
                <Link
                  to={`/agents/reports/${m.ref_id}`}
                  className="mt-1 inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
                >
                  Open report #{m.ref_id} <ArrowUpRight className="w-3 h-3" />
                </Link>
              )}
            </li>
          ))}
        </ul>
      )}

      {hasNextPage && (
        <div className="mt-2 flex justify-center">
          <button
            type="button"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs py-1 px-3 disabled:opacity-50"
          >
            {isFetchingNextPage ? 'Loading...' : 'Load more'}
          </button>
        </div>
      )}
    </CollapsibleSection>
  );
}
