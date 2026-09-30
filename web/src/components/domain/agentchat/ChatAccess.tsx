import React from 'react';
import { MessageSquareOff } from 'lucide-react';
import { useAuth } from '@/context/AuthContext';
import { formatUsd } from '@/lib/format';
import { ChatDensity } from './types';
import { useT } from '@/i18n';


// Replaces the composer when the user lacks `agent_chat` (auth-02 §4).
export function ChatDisabledNotice({ density }: { density: ChatDensity }) {
  const t = useT();
  return (
    <div
      role="note"
      data-testid="chat-disabled-notice"
      className={`flex items-center gap-2 rounded border border-border bg-secondary/40 text-muted-foreground px-3 py-2 ${
        density === 'compact' ? 'text-xs' : 'text-sm'
      }`}
    >
      <MessageSquareOff className="w-4 h-4 shrink-0" />
      <span>{t('chat.disabled')}</span>
    </div>
  );
}

// "Today: $0.34 of $1.00" under the composer - from /auth/me, refreshed
// after each turn by ChatSessionContext.
export function ChatBudgetLine({ className = '' }: { className?: string }) {
  const t = useT();
  const { me } = useAuth();
  if (!me) return null;
  const exhausted = me.daily_agent_budget_usd > 0 && me.today_agent_cost_usd >= me.daily_agent_budget_usd;
  return (
    <span
      data-testid="chat-budget-line"
      className={`font-mono tabular-nums ${exhausted ? 'text-destructive' : 'text-muted-foreground'} ${className}`}
      title={t('chat.budgetTitle')}
    >
      {t('chat.budgetLine', { spent: formatUsd(me.today_agent_cost_usd), budget: formatUsd(me.daily_agent_budget_usd) })}
    </span>
  );
}
