import React from 'react';
import { ConsoleEntry } from '@/pages/WorkbenchShell';

interface ConsolePanelProps {
  entries: ConsoleEntry[];
}

export function ConsolePanel({ entries }: ConsolePanelProps) {
  return (
    <div
      className="console-panel border border-green-950 rounded-lg bg-black font-mono text-xs max-h-48 overflow-y-auto"
      data-testid="console-panel"
    >
      {entries.length === 0 ? (
        <div className="p-3 text-green-800">No console output yet.</div>
      ) : (
        entries.map((e) => (
          <div
            key={e.id}
            className={`px-3 py-1 flex gap-2 border-b border-green-950/40 last:border-0 ${
              e.kind === 'error' ? 'text-red-400' : 'text-green-400'
            }`}
          >
            <span className="text-green-800 shrink-0">{e.timestamp}</span>
            <span className="uppercase text-[10px] opacity-60 shrink-0">[{e.source}]</span>
            {e.label && <span className="text-amber-400 shrink-0">{e.label}:</span>}
            <span className="truncate">{e.message}</span>
          </div>
        ))
      )}
    </div>
  );
}
