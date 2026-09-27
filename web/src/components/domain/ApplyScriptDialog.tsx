import React, { useMemo } from 'react';
import { X, FileDiff, Check } from 'lucide-react';
import { lineDiff } from '@/lib/lineDiff';

interface ApplyScriptDialogProps {
  currentSource: string;
  proposedSource: string;
  onConfirm: () => void;
  onCancel: () => void;
}

// Preview shown before AgentCopilotWidget pushes a save_strategy_script
// result into the live CodeMirror instance (via EditorBridgeContext). Never
// applied silently - the operator may have in-progress edits in that
// buffer, so this is the one required confirmation step between "the agent
// proposed a script" and "the editor's content changes".
export function ApplyScriptDialog({ currentSource, proposedSource, onConfirm, onCancel }: ApplyScriptDialogProps) {
  const diff = useMemo(() => lineDiff(currentSource, proposedSource), [currentSource, proposedSource]);
  const additions = diff.filter((d) => d.type === 'add').length;
  const removals = diff.filter((d) => d.type === 'remove').length;
  const identical = additions === 0 && removals === 0;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Apply agent script to editor"
      className="fixed inset-0 z-[70] flex items-center justify-center bg-background/70 p-4"
      onClick={onCancel}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-2xl max-h-[80vh] flex flex-col bg-background border border-border/60 rounded-lg shadow-2xl"
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b border-border/60 shrink-0">
          <FileDiff className="w-4 h-4 text-foreground" />
          <h2 className="text-sm font-bold text-foreground">Apply script to editor</h2>
          <span className="text-[11px] text-muted-foreground ml-1">
            {identical ? 'No changes' : (
              <>
                <span className="text-success">+{additions}</span>{' '}
                <span className="text-destructive">-{removals}</span>
              </>
            )}
          </span>
          <button
            onClick={onCancel}
            className="ml-auto text-muted-foreground hover:text-foreground p-1"
            aria-label="Cancel"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <p className="px-4 pt-3 text-xs text-muted-foreground">
          This replaces the Lua source in your open editor buffer with the agent's proposed script. Review the
          diff below before confirming - your current buffer is not saved until you press{' '}
          <span className="font-mono text-foreground">Save</span>/<span className="font-mono text-foreground">Ctrl+S</span>{' '}
          in the editor yourself.
        </p>

        <div className="flex-1 min-h-0 overflow-y-auto mx-4 my-3 rounded border border-border/60 bg-background/60 font-mono text-[11px]">
          {diff.map((line, i) => (
            <div
              key={i}
              className={`whitespace-pre-wrap break-words px-2 py-0.5 ${
                line.type === 'add'
                  ? 'bg-success/15 text-success'
                  : line.type === 'remove'
                  ? 'bg-destructive/15 text-destructive'
                  : 'text-muted-foreground'
              }`}
            >
              <span className="select-none inline-block w-3 text-muted-foreground">
                {line.type === 'add' ? '+' : line.type === 'remove' ? '-' : ' '}
              </span>
              {line.text || ' '}
            </div>
          ))}
        </div>

        <div className="flex items-center justify-end gap-2 px-4 py-3 border-t border-border/60 shrink-0">
          <button
            onClick={onCancel}
            className="bg-card/40 hover:bg-secondary/40 text-foreground rounded border border-border/40 text-xs py-1.5 px-3"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={identical}
            className="bg-primary hover:bg-primary disabled:opacity-50 disabled:cursor-not-allowed text-primary-foreground font-semibold rounded px-4 py-1.5 flex items-center gap-1.5 text-xs"
          >
            <Check className="w-3.5 h-3.5" />
            <span>Apply to editor</span>
          </button>
        </div>
      </div>
    </div>
  );
}
