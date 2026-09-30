import { Capability, Strategy, UserRole } from '@/api/types';

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

// User-facing strings (kept together for the i18n pass).
export const PERMISSION_STRINGS = {
  adminOnly: 'Only admins can change this.',
  capabilityLabels: {
    view: 'View',
    backtest: 'Backtest',
    edit_drafts: 'Edit drafts',
    agent_chat: 'Agent chat',
    approve_proposals: 'Approve proposals',
    admin: 'Admin',
  } as Record<Capability, string>,
  roleLabels: {
    admin: 'Admin',
    friend: 'Friend',
    viewer: 'Viewer',
  } as Record<UserRole, string>,
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
