import React from 'react';
import { ConsoleEntry } from '@/pages/WorkbenchShell';
import { useT } from '@/i18n';

interface ConsolePanelProps {
  entries: ConsoleEntry[];
}

export function ConsolePanel({ entries }: ConsolePanelProps) {
  const t = useT();
  return (
    <div
      className="console-panel border border-border rounded-lg bg-background font-mono text-xs max-h-48 overflow-y-auto"
      data-testid="console-panel"
    >
      {entries.length === 0 ? (
        <div className="p-3 text-muted-foreground">{t('workbench.noConsole')}</div>
      ) : (
        entries.map((e) => (
          <div
            key={e.id}
            className={`px-3 py-1 flex gap-2 border-b border-border/40 last:border-0 ${
              e.kind === 'error' ? 'text-destructive' : 'text-foreground'
            }`}
          >
            <span className="text-muted-foreground shrink-0">{e.timestamp}</span>
            <span className="uppercase text-[10px] opacity-60 shrink-0">[{e.source}]</span>
            {e.label && <span className="text-warning shrink-0">{e.label}:</span>}
            <span className="truncate">{e.message}</span>
          </div>
        ))
      )}
    </div>
  );
}
