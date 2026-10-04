import { Capability, Strategy, UserRole } from '@/api/types';
import { tr } from '@/i18n';

// Capability helpers for the capability-aware UI (auth-02 §4). The backend
// is the source of truth: these only decide what to SHOW; every mutation
// still handles a 403 (see api/client.ts's forbidden handler).

export const ALL_CAPABILITIES: Capability[] = ['view', 'backtest', 'edit_drafts', 'agent_chat', 'approve_proposals', 'admin'];

// Role presets, mirrored from the backend so the user editor can pre-fill
// the capability checkboxes when the role changes.
// auth-01 contract (reconciled): presets per §2 (admin = all, friend = view/backtest/
// edit_drafts/agent_chat/approve_proposals, viewer = view).
export const ROLE_PRESETS: Record<UserRole, Capability[]> = {
  admin: [...ALL_CAPABILITIES],
  friend: ['view', 'backtest', 'edit_drafts', 'agent_chat', 'approve_proposals'],
  viewer: ['view'],
};

export const ROLES: UserRole[] = ['admin', 'friend', 'viewer'];

// Display labels live in the i18n catalog: t.enum('role', r),
// t.enum('capability', c), t('users.adminOnly').
export const PERMISSION_STRINGS = {
  get adminOnly(): string {
    return tr('users.adminOnly');
  },
};

export type CanFn = (cap: Capability) => boolean;

/** `admin` implies every other capability. */
export function hasCapability(caps: readonly Capability[] | undefined, cap: Capability): boolean {
  if (!caps) return false;
  return caps.includes('admin') || caps.includes(cap);
}

/** A strategy a non-admin with edit_drafts may change: backtest mode, not productive. */
export function isBacktestDraft(s: Pick<Strategy, 'mode' | 'status'>): boolean {
  return s.mode === 'backtest' && s.status !== 'productive';
}

/** Whether the current user may edit/delete this strategy (mirrors the backend draft-only guard). */
export function canEditStrategy(can: CanFn, s: Pick<Strategy, 'mode' | 'status'>): boolean {
  return can('admin') || (can('edit_drafts') && isBacktestDraft(s));
}
