import React, { useState } from 'react';
import { BookmarkPlus, Check } from 'lucide-react';
import { useAddStrategyMemoryNote } from '@/hooks/queries';
import { apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { useT } from '@/i18n';

// Operator notes are capped server-side (agentplatform.maxOperatorNoteLen).
const MAX_NOTE_CHARS = 4000;

// "Save to notes": chat is no longer written to shared strategy memory on its
// own, so the operator picks what becomes a note every agent sees. Saved via
// the operator-note endpoint (POST /strategies/{id}/memory -> a journal entry
// authored by the operator).
export function SaveToNotes({ strategyId, content, className = '' }: { strategyId: number; content: string; className?: string }) {
  const t = useT();
  const addNote = useAddStrategyMemoryNote(strategyId);
  const [saved, setSaved] = useState(false);
  // Adding notes needs `edit_drafts` (auth-02 §4).
  const canNote = useAuth().can('edit_drafts');
  const text = content.trim();
  if (!text || !canNote) return null;

  const tooLong = text.length > MAX_NOTE_CHARS;
  const save = () => {
    const body = tooLong ? `${text.slice(0, MAX_NOTE_CHARS - 1)}…` : text;
    addNote.mutate(body, { onSuccess: () => setSaved(true) });
  };

  if (saved) {
    return (
      <span className={`inline-flex items-center gap-1 text-[11px] text-success ${className}`} role="status">
        <Check className="w-3 h-3" />
        {t('chat.savedToNotes')}
      </span>
    );
  }
  return (
    <button
      type="button"
      onClick={save}
      disabled={addNote.isPending}
      title={
        addNote.isError
          ? apiErrorMessage(addNote.error, t('chat.saveNoteFailed'))
          : tooLong
            ? t('chat.saveTrimmed', { max: MAX_NOTE_CHARS })
            : t('chat.saveNoteTitle')
      }
      className={`inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground disabled:opacity-50 ${
        addNote.isError ? 'text-destructive hover:text-destructive' : ''
      } ${className}`}
    >
      <BookmarkPlus className="w-3 h-3" />
      {addNote.isPending ? t('common.saving') : addNote.isError ? t('chat.retrySave') : t('chat.saveToNotes')}
    </button>
  );
}
