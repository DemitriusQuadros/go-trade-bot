import { AgentRun } from '@/api/types';

// One transcript entry: the operator's prompt paired with the AgentRun it
// produced (undefined while the request is in flight). Shared by both chat
// views (Agent mode and the Code-mode dock) through ChatSessionContext.
export interface Turn {
  id: string;
  input: string;
  // The persona this turn was sent to (A-03 §8) - shown on the reply even
  // before/without the run's own agent_name.
  agentName?: string;
  run?: AgentRun;
  failed?: string;
  // 409 from POST /agent/runs: the agent (or every agent, via the global
  // kill switch) is paused - rendered as a notice, not a failure.
  blocked?: string;
  // Loaded from GET /agent/runs (a previous session), not sent this session.
  hydrated?: boolean;
}

// Which conversation a transcript belongs to. `new` is the Workbench's
// "new strategy" draft before the agent has created the strategy.
export type ChatContextKey = 'general' | 'new' | `strategy:${number}`;

export function strategyContextKey(id: number): ChatContextKey {
  return `strategy:${id}`;
}

export function strategyIdOfKey(key: ChatContextKey): number | undefined {
  if (!key.startsWith('strategy:')) return undefined;
  const n = Number(key.slice('strategy:'.length));
  return Number.isInteger(n) && n > 0 ? n : undefined;
}

// Mirrors the backend's stripStrategyContextMarker: a strategy-scoped chat
// input may be persisted as "[Context: the operator currently has strategy
// #N ...]\n\n<what the operator typed>".
const CONTEXT_MARKER = '[Context: the operator currently has strategy #';

export function stripContextMarker(input: string): string {
  // D-01 contract (reconciled): D-01 §2 makes input_summary clean server-side; this stays as a harmless fallback.
  if (!input.startsWith(CONTEXT_MARKER)) return input;
  const idx = input.indexOf(']\n\n');
  return idx === -1 ? input : input.slice(idx + 3);
}

/** A persisted chat run as a completed transcript turn. */
export function runToTurn(run: AgentRun): Turn {
  return {
    id: `run-${run.id}`,
    input: stripContextMarker(run.input_summary ?? ''),
    agentName: run.agent_name,
    run,
    hydrated: true,
  };
}
