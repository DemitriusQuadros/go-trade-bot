import React, { useState } from 'react';
import { ChevronDown, ChevronRight, Wrench } from 'lucide-react';
import { AgentToolCall } from '@/api/types';
import { AgentToolCallCard } from '@/components/domain/AgentToolCallCard';
import { ApplyTarget, ChatDensity } from './types';

// The raw audit view of a turn (Phase D-02 §6): a collapsed "N tool calls"
// disclosure listing every call with the unchanged AgentToolCallCard.
export function ToolActivity({
  toolCalls,
  density,
  apply,
}: {
  toolCalls: AgentToolCall[];
  density: ChatDensity;
  apply?: ApplyTarget;
}) {
  const [open, setOpen] = useState(false);
  if (toolCalls.length === 0) return null;
  const failed = toolCalls.filter((c) => c.error).length;
  return (
    <div className="min-w-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className={`inline-flex items-center gap-1.5 text-muted-foreground hover:text-foreground ${
          density === 'compact' ? 'text-[11px]' : 'text-xs'
        }`}
      >
        {open ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        <Wrench className="w-3.5 h-3.5" />
        {toolCalls.length} tool call{toolCalls.length === 1 ? '' : 's'}
        {failed > 0 && <span className="text-destructive">({failed} failed)</span>}
      </button>
      {open && (
        <div className="mt-2 space-y-2">
          {toolCalls.map((call, i) => (
            <AgentToolCallCard key={i} call={call} onApplyToEditor={apply?.onApply} />
          ))}
        </div>
      )}
    </div>
  );
}
