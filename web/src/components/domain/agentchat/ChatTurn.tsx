import React from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle } from 'lucide-react';
import { Turn } from '@/lib/chatTurn';
import { formatDateTime, formatRelative, formatUsd } from '@/lib/format';
import { MarkdownMessage } from '@/components/domain/MarkdownMessage';
import { Spinner } from '@/components/ui/Spinner';
import { TurnCards } from './TurnCards';
import { ToolActivity } from './ToolActivity';
import { ApplyTarget, ChatDensity } from './types';
import { SaveToNotes } from './SaveToNotes';
import { useChatSession } from '@/context/ChatSessionContext';
import { useAuth } from '@/context/AuthContext';
import { useT } from '@/i18n';

// One transcript turn (Phase D-02 §6), in this order: the operator's
// message, the rich cards, the model's markdown answer, then the raw
// "N tool calls" disclosure.
export function ChatTurn({ turn, density, apply }: { turn: Turn; density: ChatDensity; apply?: ApplyTarget }) {
  const t = useT();
  const { me } = useAuth();
  const isAdmin = me?.role === 'admin';
  const compact = density === 'compact';
  const text = compact ? 'text-xs' : 'text-sm';
  const run = turn.run;
  const pending = !run && !turn.failed && !turn.blocked;
  // Only a strategy conversation has notes to save into.
  const noteStrategyId = useChatSession().strategyId;

  return (
    <div className="space-y-2 min-w-0" data-testid="chat-turn">
      <div className="group flex flex-col items-end gap-1">
        <div
          className={`max-w-[85%] rounded-lg bg-secondary/60 border border-border text-foreground px-3 py-2 whitespace-pre-wrap break-words ${text}`}
        >
          {turn.input}
        </div>
        {noteStrategyId != null && (
          <SaveToNotes
            strategyId={noteStrategyId}
            content={turn.input}
            className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-opacity"
          />
        )}
      </div>

      {pending && (
        <div role="status" className={`flex items-center gap-2 text-muted-foreground pl-1 ${text}`}>
          <Spinner size="sm" />
          <span>{t('chat.working', { agent: turn.agentName ?? t('chat.agent') })}</span>
        </div>
      )}

      {turn.blocked && (
        <div role="status" className={`flex items-start gap-2 rounded border border-warning/40 bg-warning/15 text-foreground px-3 py-2 ${text}`}>
          <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5 text-warning" />
          <div>
            <div className="font-semibold">
              {/* Over budget is the user's limit, not the agent's state. */}
              {turn.blockedCode === 'user_budget_exceeded'
                ? t('chat.budgetUsedUp')
                : t('chat.cantAnswer', { agent: turn.agentName ?? t('chat.thisAgent') })}
            </div>
            <div className="text-muted-foreground mt-0.5">{turn.blocked}</div>
            {turn.blockedCode === 'user_budget_exceeded' ? (
              <Link to={isAdmin ? '/users' : '/profile'} className="inline-block mt-1 text-primary hover:underline">
                {isAdmin ? t('chat.openUsers') : t('chat.openProfile')}
              </Link>
            ) : (
              <Link to="/agents" className="inline-block mt-1 text-primary hover:underline">
                {t('chat.openAgents')}
              </Link>
            )}
          </div>
        </div>
      )}

      {turn.failed && (
        <div role="alert" className={`flex items-start gap-2 rounded border border-destructive/40 bg-destructive/15 text-destructive px-3 py-2 ${text}`}>
          <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
          <div>
            <div className="font-semibold">{t('chat.requestFailed')}</div>
            <div className="text-destructive/90 mt-0.5 break-words">{turn.failed}</div>
          </div>
        </div>
      )}

      {run && (
        <div className="space-y-2 pl-1 min-w-0">
          <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
            <span className="font-semibold text-foreground">{run.agent_name || turn.agentName || 'Copilot'}</span>
            {run.cost_usd != null && (
              <span className="font-mono tabular-nums" title={t('chat.turnCost')}>
                {formatUsd(run.cost_usd)}
              </span>
            )}
            {run.hit_iteration_cap && (
              <span
                className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-warning/15 text-warning border-warning/40"
                title={t('chat.iterationCapTitle')}
              >
                {t('chat.iterationCap')}
              </span>
            )}
            {turn.hydrated && run.started_at && (
              <span title={formatDateTime(run.started_at, { seconds: true })}>{formatRelative(run.started_at)}</span>
            )}
          </div>

          {run.status === 'running' && (
            <div className={`flex items-center gap-2 text-muted-foreground ${text}`}>
              <Spinner size="sm" />
              <span>{t('chat.stillRunning')}</span>
            </div>
          )}

          {run.status === 'error' && (
            <div role="alert" className={`flex items-start gap-2 rounded border border-destructive/40 bg-destructive/15 text-destructive px-3 py-2 ${text}`}>
              <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
              <div>
                <div className="font-semibold">{t('chat.runFailed')}</div>
                <div className="text-destructive/90 mt-0.5 break-words">
                  {run.error_message || t('chat.noErrorMessage')}
                </div>
              </div>
            </div>
          )}

          <TurnCards toolCalls={run.tool_calls ?? []} density={density} apply={apply} />

          {run.status === 'ok' && run.response_text && (
            <div
              className={`rounded-lg bg-card border border-border text-foreground px-3 py-2 min-w-0 ${
                compact ? 'max-w-[95%]' : ''
              }`}
            >
              <MarkdownMessage content={run.response_text} />
            </div>
          )}
          {run.status === 'ok' && run.response_text && noteStrategyId != null && (
            <SaveToNotes strategyId={noteStrategyId} content={run.response_text} />
          )}

          {run.status === 'ok' && !run.response_text && (run.tool_calls ?? []).length === 0 && (
            <div className={`${text} text-muted-foreground italic`}>{t('chat.emptyResponse')}</div>
          )}

          <ToolActivity toolCalls={run.tool_calls ?? []} density={density} apply={apply} />
        </div>
      )}
    </div>
  );
}
