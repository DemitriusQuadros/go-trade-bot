import React, { useState } from 'react';
import { Check, Copy, KeyRound, Loader2, Pencil, Plus, RefreshCw, Trash2, UserCheck, UserX, Users as UsersIcon } from 'lucide-react';
import { Capability, User, UserRole } from '@/api/types';
import { apiErrorMessage } from '@/api/client';
import { useCreateUser, useDeleteUser, useResetUserPassword, useUpdateUser, useUsers } from '@/hooks/queries';
import { useAuth } from '@/context/AuthContext';
import { useToast } from '@/context/ToastContext';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { DropdownMenu, DropdownMenuItem } from '@/components/ui/DropdownMenu';
import { LoadingScreen } from '@/components/ui/Spinner';
import { Modal, MODAL_BUTTON_PRIMARY, MODAL_BUTTON_SECONDARY } from '@/components/ui/Modal';
import { UserAvatar } from '@/components/layout/UserMenu';
import { ALL_CAPABILITIES, PERMISSION_STRINGS, ROLE_PRESETS, ROLES } from '@/lib/permissions';
import { formatRelative, formatUsd } from '@/lib/time';

// User-facing strings (kept together for the i18n pass).
const STRINGS = {
  title: 'Users',
  subtitle: 'Accounts that can sign in. Everyone shares the same bot; capabilities decide what each person can change.',
  newUser: 'New user',
  listTitle: 'Accounts',
  listSubtitle: (n: number) => `${n} user${n === 1 ? '' : 's'}`,
  refresh: 'Refresh',
  loadError: "Couldn't load users",
  empty: 'No users yet.',
  colUser: 'User',
  colEmail: 'Email',
  colRole: 'Role',
  colAgentToday: 'Agent today',
  colLastLogin: 'Last login',
  colStatus: 'Status',
  never: 'Never',
  active: 'Active',
  disabled: 'Disabled',
  you: 'you',
  edit: 'Edit',
  resetPassword: 'Reset password',
  disable: 'Disable',
  enable: 'Enable',
  delete: 'Delete',
  // create
  createTitle: 'New user',
  username: 'Username',
  usernameHelp: '3-32 characters: lowercase letters, digits, "_", "." or "-".',
  displayName: 'Display name',
  email: 'Email (optional)',
  role: 'Role',
  password: 'Password',
  passwordHelp: 'Generated for you - copy it now, it is shown only once. You can type your own (at least 10 characters).',
  regenerate: 'Generate',
  copy: 'Copy',
  copied: 'Copied',
  create: 'Create user',
  cancel: 'Cancel',
  created: (name: string) => `${name} created`,
  // edit
  editTitle: (name: string) => `Edit ${name}`,
  capabilities: 'Capabilities',
  capabilitiesHelp: 'Pre-filled from the role. Admin implies every other capability.',
  budget: 'Daily agent budget ($)',
  budgetHelp: '0 turns agent chat off for this user.',
  disabledToggle: 'Disabled (signs the user out and blocks sign-in)',
  save: 'Save',
  saved: (name: string) => `${name} saved`,
  // reset
  resetTitle: (name: string) => `Reset password for ${name}`,
  resetHelp: 'The user is signed out everywhere. Copy the new password now - it is shown only once.',
  resetConfirm: 'Reset password',
  resetDone: (name: string) => `Password reset for ${name}`,
  // delete
  deleteTitle: 'Delete user',
  deleteMessage: (name: string) =>
    `Delete "${name}"? They can no longer sign in. Their past chat runs, notes and decisions stay in the history.`,
  deleteDone: (name: string) => `${name} deleted`,
  toggled: (name: string, disabled: boolean) => `${name} ${disabled ? 'disabled' : 'enabled'}`,
  // validation
  usernameInvalid: 'Username must be 3-32 characters of a-z, 0-9, "_", "." or "-".',
  displayNameRequired: 'Display name is required.',
  passwordShort: 'Password must be at least 10 characters.',
  budgetInvalid: 'Budget must be a number of US dollars, 0 or more.',
};

const TH = 'px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground whitespace-nowrap';
const LABEL = 'block text-xs font-semibold text-foreground mb-1.5';
const HELP = 'text-[11px] text-muted-foreground mt-1';
const USERNAME_RE = /^[a-z0-9_.-]{3,32}$/;
const MIN_PASSWORD = 10;

/** A strong random password (no look-alike characters). */
export function generatePassword(length = 18): string {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_!@#%';
  const bytes = new Uint32Array(length);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('');
}

// Users admin page (auth-02 §5).
export function Users() {
  const { me } = useAuth();
  const { data: users = [], isLoading, isFetching, error, refetch } = useUsers();
  const updateUser = useUpdateUser();
  const deleteUser = useDeleteUser();
  const { toast } = useToast();

  const [createOpen, setCreateOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<User | null>(null);
  const [resetTarget, setResetTarget] = useState<User | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);

  const nameOf = (u: User) => u.display_name || u.username;

  const toggleDisabled = (u: User) => {
    const next = !u.disabled;
    updateUser.mutate(
      {
        id: u.id,
        req: {
          display_name: u.display_name,
          email: u.email ?? '',
          role: u.role,
          capabilities: u.capabilities,
          daily_agent_budget_usd: u.daily_agent_budget_usd,
          disabled: next,
        },
      },
      {
        onSuccess: () => toast(STRINGS.toggled(nameOf(u), next), next ? 'info' : 'success'),
        onError: (err) => toast(apiErrorMessage(err, 'Failed to update the user'), 'error'),
      },
    );
  };

  const confirmDelete = () => {
    const target = deleteTarget;
    setDeleteTarget(null);
    if (!target) return;
    // The backend refuses deleting yourself or the last admin (400) - its
    // message is shown as-is.
    deleteUser.mutate(target.id, {
      onSuccess: () => toast(STRINGS.deleteDone(nameOf(target))),
      onError: (err) => toast(apiErrorMessage(err, 'Failed to delete the user'), 'error'),
    });
  };

  if (isLoading) return <LoadingScreen message="Loading users..." />;

  return (
    <div className="container-custom space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground">{STRINGS.title}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">{STRINGS.subtitle}</p>
        </div>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          data-testid="users-new-btn"
          className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
        >
          <Plus className="w-4 h-4" />
          <span>{STRINGS.newUser}</span>
        </button>
      </div>

      <Card>
        <div className="flex items-center justify-between gap-2 mb-4">
          <CardHeader className="mb-0" title={STRINGS.listTitle} subtitle={STRINGS.listSubtitle(users.length)} />
          <button
            type="button"
            onClick={() => refetch()}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
            title={STRINGS.refresh}
            aria-label={STRINGS.refresh}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>

        {error ? (
          <div className="p-8 text-center text-xs text-destructive bg-destructive/10 rounded-lg">
            {STRINGS.loadError}: {apiErrorMessage(error)}
          </div>
        ) : users.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg">
            <UsersIcon className="w-8 h-8 mx-auto opacity-40 mb-2" />
            {STRINGS.empty}
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full border-collapse" data-testid="users-table">
              <thead>
                <tr className="border-b border-border bg-secondary/40">
                  <th className={TH}>{STRINGS.colUser}</th>
                  <th className={TH}>{STRINGS.colEmail}</th>
                  <th className={TH}>{STRINGS.colRole}</th>
                  <th className={`${TH} text-right`}>{STRINGS.colAgentToday}</th>
                  <th className={TH}>{STRINGS.colLastLogin}</th>
                  <th className={TH}>{STRINGS.colStatus}</th>
                  <th className="w-10 px-2 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {users.map((u) => {
                  const isMe = me?.id === u.id;
                  const items: DropdownMenuItem[] = [
                    { label: STRINGS.edit, icon: <Pencil />, onClick: () => setEditTarget(u) },
                    { label: STRINGS.resetPassword, icon: <KeyRound />, onClick: () => setResetTarget(u) },
                    {
                      label: u.disabled ? STRINGS.enable : STRINGS.disable,
                      icon: u.disabled ? <UserCheck /> : <UserX />,
                      onClick: () => toggleDisabled(u),
                    },
                    {
                      label: STRINGS.delete,
                      icon: <Trash2 />,
                      onClick: () => setDeleteTarget(u),
                      destructive: true,
                      separatorBefore: true,
                    },
                  ];
                  return (
                    <tr key={u.id} className="border-b border-border last:border-b-0">
                      <td className="px-4 py-3 align-top">
                        <div className="flex items-center gap-2 min-w-0">
                          <UserAvatar name={nameOf(u)} className="w-7 h-7 text-xs shrink-0" />
                          <div className="min-w-0">
                            <div className="font-semibold text-sm text-foreground truncate">
                              {nameOf(u)}
                              {isMe && <span className="ml-1.5 text-[10px] font-normal text-muted-foreground">({STRINGS.you})</span>}
                            </div>
                            <div className="text-[11px] font-mono text-muted-foreground">{u.username}</div>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3 align-top text-xs text-muted-foreground">{u.email || '—'}</td>
                      <td className="px-4 py-3 align-top">
                        <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-secondary text-foreground border-border">
                          {PERMISSION_STRINGS.roleLabels[u.role] ?? u.role}
                        </span>
                      </td>
                      <td className="px-4 py-3 align-top text-right text-xs font-mono whitespace-nowrap">
                        <span className={u.daily_agent_budget_usd > 0 && u.today_agent_cost_usd >= u.daily_agent_budget_usd ? 'text-destructive' : 'text-foreground'}>
                          {formatUsd(u.today_agent_cost_usd)}
                        </span>
                        <span className="text-muted-foreground"> / {formatUsd(u.daily_agent_budget_usd)}</span>
                      </td>
                      <td className="px-4 py-3 align-top text-xs text-muted-foreground whitespace-nowrap">
                        {u.last_login_at ? (
                          <span title={new Date(u.last_login_at).toLocaleString()}>{formatRelative(u.last_login_at)}</span>
                        ) : (
                          STRINGS.never
                        )}
                      </td>
                      <td className="px-4 py-3 align-top">
                        {u.disabled ? (
                          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-destructive/15 text-destructive border-destructive/40">
                            {STRINGS.disabled}
                          </span>
                        ) : (
                          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-success/15 text-success border-success/40">
                            {STRINGS.active}
                          </span>
                        )}
                      </td>
                      <td className="px-2 py-3 align-top text-right">
                        <DropdownMenu label={`Actions for ${nameOf(u)}`} items={items} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {createOpen && <CreateUserModal onClose={() => setCreateOpen(false)} />}
      {editTarget && <EditUserModal user={editTarget} onClose={() => setEditTarget(null)} />}
      {resetTarget && <ResetPasswordModal user={resetTarget} onClose={() => setResetTarget(null)} />}

      <ConfirmDialog
        isOpen={deleteTarget != null}
        title={STRINGS.deleteTitle}
        message={STRINGS.deleteMessage(deleteTarget ? nameOf(deleteTarget) : '')}
        confirmText={STRINGS.delete}
        isDangerous
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}

function ErrorLine({ children }: { children: React.ReactNode }) {
  return (
    <div role="alert" className="mb-3 p-2.5 rounded border border-destructive/40 bg-destructive/15 text-xs text-destructive">
      {children}
    </div>
  );
}

// Password field with Generate + Copy - the value is shown in clear text on
// purpose (it's handed over once, out of band).
function PasswordField({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard blocked - the value is visible for manual copy */
    }
  };
  return (
    <div className="flex items-center gap-2">
      <input
        id={id}
        type="text"
        autoComplete="new-password"
        spellCheck={false}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="form-input text-xs font-mono flex-1"
      />
      <button type="button" onClick={copy} className={MODAL_BUTTON_SECONDARY + ' !px-2.5 !py-1.5 text-xs flex items-center gap-1'}>
        {copied ? <Check className="w-3.5 h-3.5 text-success" /> : <Copy className="w-3.5 h-3.5" />}
        {copied ? STRINGS.copied : STRINGS.copy}
      </button>
      <button
        type="button"
        onClick={() => onChange(generatePassword())}
        className={MODAL_BUTTON_SECONDARY + ' !px-2.5 !py-1.5 text-xs'}
      >
        {STRINGS.regenerate}
      </button>
    </div>
  );
}

function RoleSelect({ id, value, onChange }: { id: string; value: UserRole; onChange: (r: UserRole) => void }) {
  return (
    <select id={id} value={value} onChange={(e) => onChange(e.target.value as UserRole)} className="form-select text-xs">
      {ROLES.map((r) => (
        <option key={r} value={r}>
          {PERMISSION_STRINGS.roleLabels[r]}
        </option>
      ))}
    </select>
  );
}

function CreateUserModal({ onClose }: { onClose: () => void }) {
  const createUser = useCreateUser();
  const { toast } = useToast();
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [role, setRole] = useState<UserRole>('friend');
  const [password, setPassword] = useState(() => generatePassword());
  const [error, setError] = useState<string | null>(null);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    const uname = username.trim().toLowerCase();
    if (!USERNAME_RE.test(uname)) return setError(STRINGS.usernameInvalid);
    if (!displayName.trim()) return setError(STRINGS.displayNameRequired);
    if (password.length < MIN_PASSWORD) return setError(STRINGS.passwordShort);
    createUser.mutate(
      { username: uname, display_name: displayName.trim(), email: email.trim() || undefined, role, password },
      {
        onSuccess: (u) => {
          toast(STRINGS.created(u?.display_name || displayName.trim()));
          onClose();
        },
        onError: (err) => setError(apiErrorMessage(err, 'Failed to create the user')),
      },
    );
  };

  return (
    <Modal title={STRINGS.createTitle} onClose={onClose} testId="user-create-modal">
      <form onSubmit={submit} className="space-y-4">
        {error && <ErrorLine>{error}</ErrorLine>}
        <div>
          <label htmlFor="new-user-username" className={LABEL}>{STRINGS.username}</label>
          <input
            id="new-user-username"
            type="text"
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            className="form-input text-xs font-mono"
          />
          <p className={HELP}>{STRINGS.usernameHelp}</p>
        </div>
        <div>
          <label htmlFor="new-user-display" className={LABEL}>{STRINGS.displayName}</label>
          <input id="new-user-display" type="text" value={displayName} onChange={(e) => setDisplayName(e.target.value)} className="form-input text-xs" />
        </div>
        <div>
          <label htmlFor="new-user-email" className={LABEL}>{STRINGS.email}</label>
          <input id="new-user-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} className="form-input text-xs" />
        </div>
        <div>
          <label htmlFor="new-user-role" className={LABEL}>{STRINGS.role}</label>
          <RoleSelect id="new-user-role" value={role} onChange={setRole} />
        </div>
        <div>
          <label htmlFor="new-user-password" className={LABEL}>{STRINGS.password}</label>
          <PasswordField id="new-user-password" value={password} onChange={setPassword} />
          <p className={HELP}>{STRINGS.passwordHelp}</p>
        </div>
        <div className="flex justify-end gap-3 pt-1">
          <button type="button" onClick={onClose} className={MODAL_BUTTON_SECONDARY}>{STRINGS.cancel}</button>
          <button type="submit" disabled={createUser.isPending} className={MODAL_BUTTON_PRIMARY}>
            {createUser.isPending && <Loader2 className="w-4 h-4 animate-spin" />}
            {STRINGS.create}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function EditUserModal({ user, onClose }: { user: User; onClose: () => void }) {
  const updateUser = useUpdateUser();
  const { toast } = useToast();
  const [displayName, setDisplayName] = useState(user.display_name);
  const [email, setEmail] = useState(user.email ?? '');
  const [role, setRole] = useState<UserRole>(user.role);
  const [caps, setCaps] = useState<Capability[]>(user.capabilities ?? ROLE_PRESETS[user.role] ?? []);
  const [budget, setBudget] = useState(String(user.daily_agent_budget_usd ?? 0));
  const [disabled, setDisabled] = useState(user.disabled);
  const [error, setError] = useState<string | null>(null);

  const changeRole = (r: UserRole) => {
    setRole(r);
    setCaps([...ROLE_PRESETS[r]]);
  };
  const toggleCap = (c: Capability) => setCaps((prev) => (prev.includes(c) ? prev.filter((x) => x !== c) : [...prev, c]));

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!displayName.trim()) return setError(STRINGS.displayNameRequired);
    const b = Number(budget);
    if (budget.trim() === '' || !Number.isFinite(b) || b < 0) return setError(STRINGS.budgetInvalid);
    updateUser.mutate(
      {
        id: user.id,
        req: {
          display_name: displayName.trim(),
          email: email.trim(),
          role,
          // Always sent, so an edited set survives a role change (§5).
          capabilities: caps,
          daily_agent_budget_usd: b,
          disabled,
        },
      },
      {
        onSuccess: () => {
          toast(STRINGS.saved(displayName.trim()));
          onClose();
        },
        onError: (err) => setError(apiErrorMessage(err, 'Failed to save the user')),
      },
    );
  };

  const isAdminCaps = caps.includes('admin');

  return (
    <Modal title={STRINGS.editTitle(user.display_name || user.username)} onClose={onClose} testId="user-edit-modal">
      <form onSubmit={submit} className="space-y-4">
        {error && <ErrorLine>{error}</ErrorLine>}
        <div>
          <label htmlFor="edit-user-display" className={LABEL}>{STRINGS.displayName}</label>
          <input id="edit-user-display" type="text" value={displayName} onChange={(e) => setDisplayName(e.target.value)} className="form-input text-xs" />
        </div>
        <div>
          <label htmlFor="edit-user-email" className={LABEL}>{STRINGS.email}</label>
          <input id="edit-user-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} className="form-input text-xs" />
        </div>
        <div>
          <label htmlFor="edit-user-role" className={LABEL}>{STRINGS.role}</label>
          <RoleSelect id="edit-user-role" value={role} onChange={changeRole} />
        </div>
        <fieldset>
          <legend className={LABEL}>{STRINGS.capabilities}</legend>
          <div className="grid grid-cols-2 gap-1.5">
            {ALL_CAPABILITIES.map((c) => (
              <label key={c} className="flex items-center gap-2 text-xs text-foreground cursor-pointer">
                <input
                  type="checkbox"
                  checked={caps.includes(c) || (isAdminCaps && c !== 'admin')}
                  disabled={isAdminCaps && c !== 'admin'}
                  onChange={() => toggleCap(c)}
                  className="w-4 h-4 rounded border-border"
                />
                <span>{PERMISSION_STRINGS.capabilityLabels[c]}</span>
              </label>
            ))}
          </div>
          <p className={HELP}>{STRINGS.capabilitiesHelp}</p>
        </fieldset>
        <div className="max-w-xs">
          <label htmlFor="edit-user-budget" className={LABEL}>{STRINGS.budget}</label>
          <input
            id="edit-user-budget"
            type="number"
            min={0}
            step="0.01"
            value={budget}
            onChange={(e) => setBudget(e.target.value)}
            className="form-input text-xs"
          />
          <p className={HELP}>{STRINGS.budgetHelp}</p>
        </div>
        <label className="flex items-center gap-2 text-xs text-foreground cursor-pointer">
          <input type="checkbox" checked={disabled} onChange={(e) => setDisabled(e.target.checked)} className="w-4 h-4 rounded border-border" />
          <span>{STRINGS.disabledToggle}</span>
        </label>
        <div className="flex justify-end gap-3 pt-1">
          <button type="button" onClick={onClose} className={MODAL_BUTTON_SECONDARY}>{STRINGS.cancel}</button>
          <button type="submit" disabled={updateUser.isPending} className={MODAL_BUTTON_PRIMARY}>
            {updateUser.isPending && <Loader2 className="w-4 h-4 animate-spin" />}
            {STRINGS.save}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function ResetPasswordModal({ user, onClose }: { user: User; onClose: () => void }) {
  const reset = useResetUserPassword();
  const { toast } = useToast();
  const [password, setPassword] = useState(() => generatePassword());
  const [error, setError] = useState<string | null>(null);
  const name = user.display_name || user.username;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < MIN_PASSWORD) return setError(STRINGS.passwordShort);
    reset.mutate(
      { id: user.id, password },
      {
        onSuccess: () => {
          toast(STRINGS.resetDone(name));
          onClose();
        },
        onError: (err) => setError(apiErrorMessage(err, 'Failed to reset the password')),
      },
    );
  };

  return (
    <Modal title={STRINGS.resetTitle(name)} onClose={onClose} testId="user-reset-modal">
      <form onSubmit={submit} className="space-y-4">
        {error && <ErrorLine>{error}</ErrorLine>}
        <div>
          <label htmlFor="reset-user-password" className={LABEL}>{STRINGS.password}</label>
          <PasswordField id="reset-user-password" value={password} onChange={setPassword} />
          <p className={HELP}>{STRINGS.resetHelp}</p>
        </div>
        <div className="flex justify-end gap-3 pt-1">
          <button type="button" onClick={onClose} className={MODAL_BUTTON_SECONDARY}>{STRINGS.cancel}</button>
          <button type="submit" disabled={reset.isPending} className={MODAL_BUTTON_PRIMARY}>
            {reset.isPending && <Loader2 className="w-4 h-4 animate-spin" />}
            {STRINGS.resetConfirm}
          </button>
        </div>
      </form>
    </Modal>
  );
}
