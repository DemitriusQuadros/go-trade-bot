import React, { useEffect, useRef, useState } from 'react';
import { Play, X } from 'lucide-react';
import { Agent } from '@/api/types';
import { useRunAgent } from '@/hooks/queries';
import { apiErrorMessage } from '@/api/client';
import { useToast } from '@/context/ToastContext';
import { Spinner } from '@/components/ui/Spinner';
import { useT } from '@/i18n';

// "Run now" dialog (A-03 §3): an optional operator instruction, then
// POST /agents/{id}/run. The run itself happens asynchronously in cmd/agent;
// this only confirms it was queued. Styled to match ui/ConfirmDialog.
export function RunAgentDialog({ agent, onClose }: { agent: Agent | null; onClose: () => void }) {
  const t = useT();
  const [prompt, setPrompt] = useState('');
  const [error, setError] = useState<string | null>(null);
  const runAgent = useRunAgent();
  const { toast } = useToast();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (agent) {
      setPrompt('');
      setError(null);
      setTimeout(() => textareaRef.current?.focus(), 0);
    }
  }, [agent]);

  useEffect(() => {
    if (!agent) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [agent, onClose]);

  if (!agent) return null;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    runAgent.mutate(
      { id: agent.id, prompt: prompt.trim() || undefined },
      {
        onSuccess: () => {
          toast(t('agents.runQueued', { name: agent.name }));
          onClose();
        },
        onError: (err) => setError(apiErrorMessage(err, t('agents.runQueueFailed'))),
      },
    );
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="run-agent-title"
    >
      <form onSubmit={submit} className="w-full max-w-lg rounded-lg border border-border bg-card p-5 shadow-xl">
        <div className="flex items-start justify-between mb-4">
          <h3 id="run-agent-title" className="text-base font-semibold text-foreground">
            {t('agents.runNowTitle', { name: agent.name })}
          </h3>
          <button type="button" onClick={onClose} aria-label={t('common.close')} className="text-muted-foreground hover:text-foreground p-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        <label htmlFor="run-agent-prompt" className="text-xs font-semibold text-foreground">
          {t('agents.instruction')} <span className="font-normal text-muted-foreground">{t('agents.optional')}</span>
        </label>
        <textarea
          id="run-agent-prompt"
          ref={textareaRef}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={4}
          placeholder={t('agents.instructionPlaceholder')}
          className="form-textarea mt-1.5 text-xs font-mono"
        />
        <p className="text-[11px] text-muted-foreground mt-1.5">
          {t('agents.runQueuedHelp')}
        </p>

        {error && (
          <div role="alert" className="mt-3 text-xs text-destructive bg-destructive/15 border border-destructive/40 rounded px-3 py-2">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-3 mt-5">
          <button
            type="button"
            onClick={onClose}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border px-4 py-2 text-sm"
          >
            {t('common.cancel')}
          </button>
          <button
            type="submit"
            disabled={runAgent.isPending}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary px-4 py-2 text-sm font-semibold flex items-center gap-1.5 disabled:opacity-50"
          >
            {runAgent.isPending ? <Spinner size="sm" className="text-primary-foreground" /> : <Play className="w-4 h-4" />}
            {t('agents.queueRun')}
          </button>
        </div>
      </form>
    </div>
  );
}
