import React, { useState } from 'react';
import { Power, PowerOff } from 'lucide-react';
import { useAgentsPausedState, useSetAgentsPaused } from '@/hooks/queries';
import { useAuth } from '@/context/AuthContext';
import { PERMISSION_STRINGS } from '@/lib/permissions';
import { apiErrorMessage } from '@/api/client';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { useToast } from '@/context/ToastContext';

// Global agents kill switch (A-03 §2), rendered in the sidebar footer.
// Reflects `agents_paused` from GET /settings, so it survives a reload.
// Pausing is immediate (the safe direction); resuming asks first, since it
// lets every non-paused agent's cron schedule start firing again.
// Admin only (auth-02 §4): everyone else sees the state read-only.
export function AgentKillSwitch({ expanded }: { expanded: boolean }) {
  const { can } = useAuth();
  const isAdmin = can('admin');
  const { paused: pausedState, isLoading } = useAgentsPausedState(isAdmin);
  const setPaused = useSetAgentsPaused();
  const { toast } = useToast();
  const [confirmResume, setConfirmResume] = useState(false);

  const paused = pausedState ?? false;
  const disabled = pausedState === undefined || isLoading || setPaused.isPending;

  if (!isAdmin) {
    // State unknown (the read-only endpoint isn't available) -> render nothing.
    if (pausedState === undefined) return null;
    const roLabel = paused ? 'Agents: paused' : 'Agents: running';
    return (
      <div
        role="status"
        aria-label={roLabel}
        title={expanded ? PERMISSION_STRINGS.adminOnly : `${roLabel}. ${PERMISSION_STRINGS.adminOnly}`}
        data-testid="kill-switch-readonly"
        className={`w-full flex items-center gap-3 px-2 py-2 rounded-md border text-xs overflow-hidden ${
          paused ? 'bg-warning/15 border-warning/40 text-warning' : 'border-transparent text-muted-foreground'
        }`}
      >
        <span className="shrink-0 relative">
          {paused ? <PowerOff className="w-5 h-5" /> : <Power className="w-5 h-5" />}
          {!paused && <span className="absolute -top-0.5 -right-0.5 w-1.5 h-1.5 rounded-full bg-success" />}
        </span>
        <span
          className={`whitespace-nowrap font-semibold transition-opacity duration-300 ${
            expanded ? 'opacity-100' : 'opacity-0 w-0'
          }`}
        >
          {roLabel}
        </span>
      </div>
    );
  }

  const apply = (next: boolean) => {
    setPaused.mutate(
      next,
      {
        onSuccess: () => toast(next ? 'All agents paused' : 'Agents resumed', next ? 'info' : 'success'),
        onError: (err) => toast(apiErrorMessage(err, 'Failed to update the agents kill switch'), 'error'),
      },
    );
  };

  const handleClick = () => {
    if (disabled) return;
    if (paused) {
      setConfirmResume(true);
    } else {
      apply(true);
    }
  };

  const label = paused ? 'Agents: paused' : 'Agents: running';

  return (
    <>
      <button
        type="button"
        role="switch"
        aria-checked={!paused}
        aria-label={`${label}. ${paused ? 'Resume agents' : 'Pause all agents'}`}
        title={expanded ? undefined : label}
        onClick={handleClick}
        disabled={disabled}
        className={`w-full flex items-center gap-3 px-2 py-2 rounded-md border text-xs transition-colors overflow-hidden disabled:opacity-50 ${
          paused
            ? 'bg-warning/15 border-warning/40 text-warning hover:bg-warning/25'
            : 'border-transparent text-muted-foreground hover:text-foreground hover:bg-accent/60'
        }`}
      >
        <span className="shrink-0 relative">
          {paused ? <PowerOff className="w-5 h-5" /> : <Power className="w-5 h-5" />}
          {!paused && <span className="absolute -top-0.5 -right-0.5 w-1.5 h-1.5 rounded-full bg-success" />}
        </span>
        <span
          className={`whitespace-nowrap font-semibold transition-opacity duration-300 ${
            expanded ? 'opacity-100' : 'opacity-0 w-0'
          }`}
        >
          {label}
        </span>
      </button>

      <ConfirmDialog
        isOpen={confirmResume}
        title="Resume all agents?"
        message="Scheduled agents will start running again on their next cron tick, and manual runs and persona chats will be accepted. Agents that are individually paused stay paused."
        confirmText="Resume agents"
        onConfirm={() => {
          setConfirmResume(false);
          apply(false);
        }}
        onCancel={() => setConfirmResume(false)}
      />
    </>
  );
}
