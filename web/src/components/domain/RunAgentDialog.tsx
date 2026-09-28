import React, { useEffect, useRef, useState } from 'react';
import { Play, X } from 'lucide-react';
import { Agent } from '@/api/types';
import { useRunAgent } from '@/hooks/queries';
import { apiErrorMessage } from '@/api/client';
import { useToast } from '@/context/ToastContext';
import { Spinner } from '@/components/ui/Spinner';

// "Run now" dialog (A-03 §3): an optional operator instruction, then
// POST /agents/{id}/run. The run itself happens asynchronously in cmd/agent;
// this only confirms it was queued. Styled to match ui/ConfirmDialog.
export function RunAgentDialog({ agent, onClose }: { agent: Agent | null; onClose: () => void }) {
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
          toast(`Run queued for ${agent.name}`);
          onClose();
        },
        onError: (err) => setError(apiErrorMessage(err, 'Failed to queue the run')),
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
            Run {agent.name} now
          </h3>
          <button type="button" onClick={onClose} aria-label="Close" className="text-muted-foreground hover:text-foreground p-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        <label htmlFor="run-agent-prompt" className="text-xs font-semibold text-foreground">
          Instruction <span className="font-normal text-muted-foreground">(optional)</span>
        </label>
        <textarea
          id="run-agent-prompt"
          ref={textareaRef}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={4}
          placeholder="Leave empty to run the agent's standard evaluation of its bound strategies."
          className="form-textarea mt-1.5 text-xs font-mono"
        />
        <p className="text-[11px] text-muted-foreground mt-1.5">
          The run is queued on the agent runtime and appears in the agent's run history when it finishes.
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
            Cancel
          </button>
          <button
            type="submit"
            disabled={runAgent.isPending}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary px-4 py-2 text-sm font-semibold flex items-center gap-1.5 disabled:opacity-50"
          >
            {runAgent.isPending ? <Spinner size="sm" className="text-primary-foreground" /> : <Play className="w-4 h-4" />}
            Queue run
          </button>
        </div>
      </form>
    </div>
  );
}
