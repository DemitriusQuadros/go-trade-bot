import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronDown, ChevronRight, Wrench, AlertTriangle, ArrowUpRight, FileDiff } from 'lucide-react';
import { AgentToolCall } from '@/api/types';

// Both entities.Strategy row IDs and BacktestRun IDs are surfaced back to
// the model as machine-parseable "strategy_id=N"/"backtest_id=N" prefixes
// in the tool result text (see app/usecase/agent/tools.go's
// summarizeStrategyForModel/summarizeBacktestForModel and
// strategyIDFromToolResult) - this is the only place those IDs live in the
// audit trail, so the card parses them the same way the backend does to
// build its "open in editor"/"open backtest" links.
function extractID(text: string | undefined, key: 'strategy_id' | 'backtest_id'): number | null {
  if (!text) return null;
  const match = text.match(new RegExp(`${key}=(\\d+)`));
  return match ? parseInt(match[1], 10) : null;
}

const MAX_INLINE_LENGTH = 400;

function Collapsible({ label, content }: { label: string; content: string }) {
  const [expanded, setExpanded] = useState(false);
  const isLong = content.length > MAX_INLINE_LENGTH;
  const shown = expanded || !isLong ? content : content.slice(0, MAX_INLINE_LENGTH) + '…';

  return (
    <div className="mt-1.5">
      <button
        onClick={() => setExpanded((e) => !e)}
        className="flex items-center gap-1 text-[10px] uppercase tracking-wide text-green-700 hover:text-green-400 font-semibold"
      >
        {expanded ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
        {label}
      </button>
      {expanded || content.length > 0 ? (
        <pre className="mt-1 whitespace-pre-wrap break-words bg-black/50 border border-green-950/60 rounded p-2 text-[11px] text-green-400 font-mono max-h-72 overflow-y-auto">
          {shown}
        </pre>
      ) : null}
      {isLong && (
        <button
          onClick={() => setExpanded((e) => !e)}
          className="mt-1 text-[10px] text-green-600 hover:text-green-400 underline"
        >
          {expanded ? 'Show less' : 'Show full output'}
        </button>
      )}
    </div>
  );
}

// AgentToolCallCard is the shared, distinct-card rendering for one tool call
// - used by both the live chat transcript (Frontend Spec 01) and the
// read-only run history's expanded row (Frontend Spec 02), per Spec 02's
// explicit "reused here rather than reimplemented" requirement.
interface AgentToolCallCardProps {
  call: AgentToolCall;
  // Only wired by AgentCopilotWidget.tsx, when the floating widget is open
  // AND the current route is inside the strategy workbench (EditorBridgeContext
  // is present there) - AgentHistory.tsx's read-only audit view never passes
  // this, so its cards never show the affordance. Receives the model-proposed
  // script_source (from call.args, pre-clamp) - the caller owns showing a
  // diff/confirmation before actually touching the editor.
  onApplyToEditor?: (scriptSource: string) => void;
}

export function AgentToolCallCard({ call, onApplyToEditor }: AgentToolCallCardProps) {
  const isError = !!call.error;
  const argsText = call.args ? JSON.stringify(call.args, null, 2) : '';
  const resultText = call.result || call.error || '';

  const proposedScriptSource =
    call.tool === 'save_strategy_script' && typeof call.args?.script_source === 'string'
      ? (call.args.script_source as string)
      : null;

  const strategyId = call.tool === 'save_strategy_script' ? extractID(resultText, 'strategy_id') : null;
  const backtestId = call.tool === 'run_backtest' ? extractID(resultText, 'backtest_id') : null;
  const backtestStrategyId = call.tool === 'run_backtest' ? extractID(resultText, 'strategy_id') : null;

  return (
    <div
      className={`rounded border text-xs ${
        isError ? 'border-rose-900/50 bg-rose-950/10' : 'border-green-900/40 bg-green-950/10'
      } p-3`}
    >
      <div className="flex items-center gap-2">
        {isError ? (
          <AlertTriangle className="w-3.5 h-3.5 text-rose-400 shrink-0" />
        ) : (
          <Wrench className="w-3.5 h-3.5 text-green-500 shrink-0" />
        )}
        <span className="font-mono font-semibold text-green-300">{call.tool}</span>
        {isError && <span className="text-[10px] uppercase text-rose-400 font-semibold">failed</span>}
        {call.timestamp && (
          <span className="ml-auto text-[10px] text-green-800 font-mono">
            {new Date(call.timestamp).toLocaleTimeString()}
          </span>
        )}
      </div>

      {argsText && (
        <Collapsible
          label={call.tool === 'save_strategy_script' ? 'Requested arguments (model-supplied, pre-clamp)' : 'Arguments'}
          content={argsText}
        />
      )}
      {resultText && (
        <Collapsible
          label={
            isError
              ? 'Error'
              : call.tool === 'save_strategy_script'
                ? 'Persisted strategy (applied, post safety-clamp)'
                : 'Result'
          }
          content={resultText}
        />
      )}

      {!isError && proposedScriptSource != null && onApplyToEditor && (
        <div className="mt-2">
          <button
            onClick={() => onApplyToEditor(proposedScriptSource)}
            className="inline-flex items-center gap-1.5 text-[11px] bg-green-700 hover:bg-green-600 text-black font-semibold rounded px-2.5 py-1"
          >
            <FileDiff className="w-3 h-3" />
            Apply to editor
          </button>
        </div>
      )}

      {(strategyId != null || backtestId != null) && (
        <div className="mt-2 flex flex-wrap gap-2">
          {strategyId != null && (
            <Link
              to={`/strategies/${strategyId}/edit`}
              className="inline-flex items-center gap-1 text-[11px] text-emerald-400 hover:text-emerald-300 underline underline-offset-2"
            >
              Open strategy #{strategyId} in editor
              <ArrowUpRight className="w-3 h-3" />
            </Link>
          )}
          {backtestId != null && backtestStrategyId != null && (
            <Link
              to={`/strategies/${backtestStrategyId}/edit/backtest/${backtestId}`}
              className="inline-flex items-center gap-1 text-[11px] text-emerald-400 hover:text-emerald-300 underline underline-offset-2"
            >
              Open backtest #{backtestId}
              <ArrowUpRight className="w-3 h-3" />
            </Link>
          )}
        </div>
      )}
    </div>
  );
}
