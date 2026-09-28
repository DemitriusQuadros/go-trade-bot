import React, { useMemo, useState } from 'react';
import { DiffLine, lineDiff } from '@/lib/lineDiff';

// Unified line diff of two Lua sources, built on the same lib/lineDiff LCS
// helper ApplyScriptDialog uses (no diff dependency). Long runs of
// unchanged lines are collapsed to CONTEXT lines around each change, with
// a toggle to show the whole file.
const CONTEXT = 3;

type Row =
  | { kind: 'line'; line: DiffLine; oldNo: number | null; newNo: number | null }
  | { kind: 'gap'; hidden: number };

function buildRows(diff: DiffLine[], expanded: boolean): Row[] {
  const numbered: Extract<Row, { kind: 'line' }>[] = [];
  let o = 0;
  let n = 0;
  for (const line of diff) {
    if (line.type === 'same') {
      o++;
      n++;
      numbered.push({ kind: 'line', line, oldNo: o, newNo: n });
    } else if (line.type === 'remove') {
      o++;
      numbered.push({ kind: 'line', line, oldNo: o, newNo: null });
    } else {
      n++;
      numbered.push({ kind: 'line', line, oldNo: null, newNo: n });
    }
  }
  if (expanded) return numbered;

  // Keep a line if it's a change or within CONTEXT of one.
  const keep = new Array(numbered.length).fill(false);
  numbered.forEach((r, i) => {
    if (r.line.type !== 'same') {
      for (let k = Math.max(0, i - CONTEXT); k <= Math.min(numbered.length - 1, i + CONTEXT); k++) keep[k] = true;
    }
  });
  const rows: Row[] = [];
  let hidden = 0;
  numbered.forEach((r, i) => {
    if (keep[i]) {
      if (hidden) rows.push({ kind: 'gap', hidden });
      hidden = 0;
      rows.push(r);
    } else {
      hidden++;
    }
  });
  if (hidden) rows.push({ kind: 'gap', hidden });
  return rows;
}

export function SourceDiff({
  before,
  after,
  beforeLabel = 'current',
  afterLabel = 'proposed',
}: {
  before: string;
  after: string;
  beforeLabel?: string;
  afterLabel?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const diff = useMemo(() => lineDiff(before ?? '', after ?? ''), [before, after]);
  const additions = diff.filter((d) => d.type === 'add').length;
  const removals = diff.filter((d) => d.type === 'remove').length;
  const rows = useMemo(() => buildRows(diff, expanded), [diff, expanded]);

  return (
    <div className="rounded border border-border/60 overflow-hidden">
      <div className="flex items-center gap-3 px-3 py-1.5 border-b border-border/60 bg-secondary/40 text-[11px] text-muted-foreground">
        <span>
          <span className="text-destructive font-mono">-</span> {beforeLabel}
          <span className="mx-2 text-muted-foreground/50">·</span>
          <span className="text-success font-mono">+</span> {afterLabel}
        </span>
        <span className="font-mono">
          {additions === 0 && removals === 0 ? (
            'No changes'
          ) : (
            <>
              <span className="text-success">+{additions}</span> <span className="text-destructive">-{removals}</span>
            </>
          )}
        </span>
        <button
          type="button"
          onClick={() => setExpanded((e) => !e)}
          className="ml-auto text-foreground hover:text-primary underline-offset-2 hover:underline"
        >
          {expanded ? 'Collapse unchanged' : 'Show full file'}
        </button>
      </div>
      <div className="max-h-[32rem] overflow-auto bg-background/60 font-mono text-[11px]">
        <table className="w-full border-collapse">
          <tbody>
            {rows.map((r, i) =>
              r.kind === 'gap' ? (
                <tr key={`gap-${i}`}>
                  <td colSpan={4} className="px-3 py-0.5 text-center text-muted-foreground bg-secondary/40 select-none">
                    <button type="button" onClick={() => setExpanded(true)} className="hover:text-foreground">
                      ··· {r.hidden} unchanged line{r.hidden === 1 ? '' : 's'} ···
                    </button>
                  </td>
                </tr>
              ) : (
                <tr
                  key={i}
                  className={
                    r.line.type === 'add'
                      ? 'bg-success/15 text-success'
                      : r.line.type === 'remove'
                        ? 'bg-destructive/15 text-destructive'
                        : 'text-muted-foreground'
                  }
                >
                  <td className="w-10 px-2 text-right select-none text-muted-foreground/60 align-top">{r.oldNo ?? ''}</td>
                  <td className="w-10 px-2 text-right select-none text-muted-foreground/60 align-top">{r.newNo ?? ''}</td>
                  <td className="w-4 select-none align-top">
                    {r.line.type === 'add' ? '+' : r.line.type === 'remove' ? '-' : ' '}
                  </td>
                  <td className="pr-3 whitespace-pre-wrap break-words">{r.line.text || ' '}</td>
                </tr>
              ),
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
