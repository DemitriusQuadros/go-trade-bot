import React, { useMemo, useState } from 'react';
import { FileCode2, FileDiff } from 'lucide-react';
import { AgentToolCall, ScriptVersion } from '@/api/types';
import { useScriptVersions, useStrategy } from '@/hooks/queries';
import { DiffLine, lineDiff } from '@/lib/lineDiff';
import { CodeChangeCardItem, toolArgNumber, toolArgString } from '@/lib/toolRefs';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { CardAction, CardButton, CardFallback, CardSkeleton, ChatCard, ChatDensity } from './ChatCard';
import { ApplyTarget } from '../types';

const COLLAPSED_CHANGED_LINES = 30;
const CONTEXT_LINES = 2;

function verbFor(calls: AgentToolCall[]): string {
  const tools = new Set(calls.map((c) => c.tool));
  if (tools.has('create_challenger')) return 'Created challenger';
  if (tools.has('create_strategy')) return 'Created strategy';
  if (tools.has('deploy_to_testing')) return 'Deployed to testing';
  // save_strategy_script without strategy_id creates a strategy.
  if (calls.every((c) => c.tool === 'save_strategy_script' && toolArgNumber(c, 'strategy_id') == null)) return 'Created strategy';
  return 'Updated script';
}

function time(iso: string | undefined): number {
  const t = iso ? new Date(iso).getTime() : NaN;
  return Number.isNaN(t) ? NaN : t;
}

// Newest version created before `beforeMs` (the pre-change script), and the
// first one created at/after it (what the change persisted).
function pickVersions(versions: ScriptVersion[] | undefined, beforeMs: number) {
  if (!versions?.length || Number.isNaN(beforeMs)) return { previous: undefined, persisted: undefined };
  const sorted = [...versions].sort((a, b) => time(a.created_at) - time(b.created_at));
  let previous: ScriptVersion | undefined;
  let persisted: ScriptVersion | undefined;
  for (const v of sorted) {
    if (time(v.created_at) < beforeMs) previous = v;
    else if (!persisted) persisted = v;
  }
  return { previous, persisted };
}

type Row = DiffLine | { type: 'gap' };

// Unified diff hunks: changed lines with a little context, gaps elided.
function hunks(diff: DiffLine[], maxChanged: number | null): { rows: Row[]; truncated: boolean } {
  const keep = new Array(diff.length).fill(false);
  diff.forEach((d, i) => {
    if (d.type === 'same') return;
    for (let j = Math.max(0, i - CONTEXT_LINES); j <= Math.min(diff.length - 1, i + CONTEXT_LINES); j++) keep[j] = true;
  });
  const rows: Row[] = [];
  let changed = 0;
  let truncated = false;
  let lastKept = -1;
  for (let i = 0; i < diff.length; i++) {
    if (!keep[i]) continue;
    if (maxChanged != null && diff[i].type !== 'same' && changed >= maxChanged) {
      truncated = true;
      break;
    }
    if (lastKept !== -1 && i > lastKept + 1) rows.push({ type: 'gap' });
    rows.push(diff[i]);
    if (diff[i].type !== 'same') changed++;
    lastKept = i;
  }
  return { rows, truncated };
}

// save_strategy_script / deploy_to_testing (deployed) / create_challenger /
// create_strategy (Phase D-02 §6): the script change as a unified diff
// against the previous script version.
export function CodeChangeCard({
  item,
  density,
  apply,
}: {
  item: CodeChangeCardItem;
  density: ChatDensity;
  apply?: ApplyTarget;
}) {
  const { strategyId, calls } = item;
  const strategy = useStrategy(strategyId);
  const versions = useScriptVersions(strategyId);
  const [full, setFull] = useState(false);

  const firstTs = time(calls[0]?.timestamp);
  const { previous, persisted } = useMemo(() => pickVersions(versions.data, firstTs), [versions.data, firstTs]);

  // Proposed source: the last call that carried one in its args, else what
  // was persisted (create_challenger clones the champion without args).
  const proposed = useMemo(() => {
    for (let i = calls.length - 1; i >= 0; i--) {
      const s = toolArgString(calls[i], 'script_source');
      if (s != null) return s;
    }
    return persisted?.source ?? strategy.data?.script_source ?? null;
  }, [calls, persisted, strategy.data]);

  const diff = useMemo(() => (proposed == null ? [] : lineDiff(previous?.source ?? '', proposed)), [previous, proposed]);
  const added = diff.filter((d) => d.type === 'add').length;
  const removed = diff.filter((d) => d.type === 'remove').length;
  const { rows, truncated } = useMemo(() => hunks(diff, full ? null : COLLAPSED_CHANGED_LINES), [diff, full]);

  if (strategy.isLoading || versions.isLoading) return <CardSkeleton label={`Loading strategy #${strategyId}`} />;
  if (strategy.error && proposed == null) {
    return <CardFallback text={`Strategy #${strategyId} couldn't be loaded.`} to={`/strategies/${strategyId}/edit`} />;
  }

  const s = strategy.data;
  const championId = item.championId ?? s?.challenger_of_id ?? null;
  const canApply = !!apply && proposed != null && (apply.strategyId == null || apply.strategyId === strategyId);

  return (
    <ChatCard
      testId="chat-code-card"
      density={density}
      icon={<FileCode2 className="w-3.5 h-3.5" />}
      title={
        <>
          {verbFor(calls)} · #{strategyId}
          {s?.name ? <span className="font-normal text-muted-foreground"> {s.name}</span> : null}
          {championId ? <span className="font-normal text-muted-foreground"> (challenger of #{championId})</span> : null}
        </>
      }
      meta={
        <>
          {s && <ModeBadge mode={s.mode} />}
          {s && <StatusBadge status={s.status} />}
          <span className="font-mono tabular-nums text-[11px]">
            <span className="text-success">+{added}</span> <span className="text-destructive">-{removed}</span>
          </span>
        </>
      }
      actions={
        <>
          {canApply && (
            <CardButton onClick={() => apply!.onApply(proposed!)} icon={<FileDiff className="w-3 h-3" />}>
              Apply to editor
            </CardButton>
          )}
          <CardAction to={`/strategies/${strategyId}/edit`}>Open in Code mode</CardAction>
        </>
      }
    >
      {proposed == null ? (
        <p className="text-xs text-muted-foreground">The script source isn't available for this change.</p>
      ) : added === 0 && removed === 0 ? (
        <p className="text-xs text-muted-foreground">No changes against the previous script version.</p>
      ) : (
        <>
          {!previous && <p className="text-[11px] text-muted-foreground">No previous version - the whole script is new.</p>}
          <div className="max-h-96 overflow-auto rounded border border-border/60 bg-background/60 font-mono text-[11px]">
            {rows.map((line, i) =>
              line.type === 'gap' ? (
                <div key={i} className="px-2 py-0.5 text-muted-foreground/70 bg-secondary/30 select-none">⋯</div>
              ) : (
                <div
                  key={i}
                  className={`whitespace-pre px-2 py-0.5 ${
                    line.type === 'add'
                      ? 'bg-success/15 text-success'
                      : line.type === 'remove'
                        ? 'bg-destructive/15 text-destructive'
                        : 'text-muted-foreground'
                  }`}
                >
                  <span className="select-none inline-block w-3">{line.type === 'add' ? '+' : line.type === 'remove' ? '-' : ' '}</span>
                  {line.text || ' '}
                </div>
              ),
            )}
          </div>
          {(truncated || full) && (
            <button
              type="button"
              onClick={() => setFull((f) => !f)}
              className="text-[11px] text-muted-foreground hover:text-foreground underline"
            >
              {full ? 'Show first 30 changed lines' : 'Show full diff'}
            </button>
          )}
        </>
      )}
    </ChatCard>
  );
}
