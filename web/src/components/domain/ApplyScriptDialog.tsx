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
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/70 p-4"
      onClick={onCancel}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-2xl max-h-[80vh] flex flex-col bg-black border border-green-800/60 rounded-lg shadow-2xl"
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b border-green-950/60 shrink-0">
          <FileDiff className="w-4 h-4 text-green-500" />
          <h2 className="text-sm font-bold text-green-300">Apply script to editor</h2>
          <span className="text-[11px] text-green-700 ml-1">
            {identical ? 'No changes' : (
              <>
                <span className="text-emerald-400">+{additions}</span>{' '}
                <span className="text-rose-400">-{removals}</span>
              </>
            )}
          </span>
          <button
            onClick={onCancel}
            className="ml-auto text-green-700 hover:text-green-400 p-1"
            aria-label="Cancel"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <p className="px-4 pt-3 text-xs text-green-700">
          This replaces the Lua source in your open editor buffer with the agent's proposed script. Review the
          diff below before confirming - your current buffer is not saved until you press{' '}
          <span className="font-mono text-green-500">Save</span>/<span className="font-mono text-green-500">Ctrl+S</span>{' '}
          in the editor yourself.
        </p>

        <div className="flex-1 min-h-0 overflow-y-auto mx-4 my-3 rounded border border-green-950/60 bg-black/60 font-mono text-[11px]">
          {diff.map((line, i) => (
            <div
              key={i}
              className={`whitespace-pre-wrap break-words px-2 py-0.5 ${
                line.type === 'add'
                  ? 'bg-emerald-950/40 text-emerald-300'
                  : line.type === 'remove'
                  ? 'bg-rose-950/40 text-rose-300'
                  : 'text-green-700'
              }`}
            >
              <span className="select-none inline-block w-3 text-green-800">
                {line.type === 'add' ? '+' : line.type === 'remove' ? '-' : ' '}
              </span>
              {line.text || ' '}
            </div>
          ))}
        </div>

        <div className="flex items-center justify-end gap-2 px-4 py-3 border-t border-green-950/60 shrink-0">
          <button
            onClick={onCancel}
            className="bg-green-950/40 hover:bg-green-900/40 text-green-300 rounded border border-green-800/40 text-xs py-1.5 px-3"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={identical}
            className="bg-green-700 hover:bg-green-600 disabled:opacity-50 disabled:cursor-not-allowed text-black font-semibold rounded px-4 py-1.5 flex items-center gap-1.5 text-xs"
          >
            <Check className="w-3.5 h-3.5" />
            <span>Apply to editor</span>
          </button>
        </div>
      </div>
    </div>
  );
}
