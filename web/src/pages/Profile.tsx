import React, { useEffect, useState } from 'react';
import { Loader2, Save } from 'lucide-react';
import { api, apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { useToast } from '@/context/ToastContext';
import { Card, CardHeader } from '@/components/ui/Card';
import { UserAvatar } from '@/components/layout/UserMenu';
import { PERMISSION_STRINGS } from '@/lib/permissions';
import { formatUsd } from '@/lib/time';

// User-facing strings (kept together for the i18n pass).
const STRINGS = {
  title: 'Profile',
  subtitle: 'Your account on this bot.',
  account: 'Account',
  accountSubtitle: 'How you appear to other users (notes, proposal decisions).',
  username: 'Username',
  displayName: 'Display name',
  saveName: 'Save',
  nameSaved: 'Display name saved',
  nameRequired: 'Display name is required.',
  role: 'Role',
  capabilities: 'Capabilities',
  agentSpend: "Today's agent spend",
  agentSpendValue: (spent: string, budget: string) => `${spent} of ${budget}`,
  noChat: 'Agent chat is off for your account.',
  language: 'Language',
  languagePlaceholder: 'Coming soon',
  password: 'Change password',
  passwordSubtitle: 'Changing your password signs you out everywhere else.',
  currentPassword: 'Current password',
  newPassword: 'New password',
  confirmPassword: 'Confirm new password',
  changePassword: 'Change password',
  passwordChanged: 'Password changed - your other sessions were signed out.',
  passwordMissing: 'Fill in all three password fields.',
  passwordShort: 'The new password must be at least 10 characters.',
  passwordMismatch: "The new passwords don't match.",
};

const LABEL = 'block text-xs font-semibold text-foreground mb-1.5';
const MIN_PASSWORD = 10;
const BUTTON =
  'bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold disabled:opacity-50';

// /profile (auth-02 §6) - every signed-in user.
export function Profile() {
  const { me, refresh } = useAuth();
  const { toast } = useToast();

  const [displayName, setDisplayName] = useState(me?.display_name ?? '');
  const [savingName, setSavingName] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);

  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [savingPassword, setSavingPassword] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);

  // Keep today's spend fresh when the page opens.
  useEffect(() => {
    void refresh();
  }, [refresh]);

  if (!me) return null;

  const saveName = async (e: React.FormEvent) => {
    e.preventDefault();
    setNameError(null);
    if (!displayName.trim()) {
      setNameError(STRINGS.nameRequired);
      return;
    }
    setSavingName(true);
    try {
      await api.updateMe({ display_name: displayName.trim() });
      await refresh();
      toast(STRINGS.nameSaved);
    } catch (err) {
      setNameError(apiErrorMessage(err, 'Failed to save the display name'));
    } finally {
      setSavingName(false);
    }
  };

  const changePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setPasswordError(null);
    if (!currentPassword || !newPassword || !confirmPassword) return setPasswordError(STRINGS.passwordMissing);
    if (newPassword.length < MIN_PASSWORD) return setPasswordError(STRINGS.passwordShort);
    if (newPassword !== confirmPassword) return setPasswordError(STRINGS.passwordMismatch);
    setSavingPassword(true);
    try {
      await api.updateMe({ current_password: currentPassword, new_password: newPassword });
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      toast(STRINGS.passwordChanged);
    } catch (err) {
      setPasswordError(apiErrorMessage(err, 'Failed to change the password'));
    } finally {
      setSavingPassword(false);
    }
  };

  const caps = me.capabilities ?? [];

  return (
    <div className="container-custom max-w-3xl mx-auto space-y-6">
      <div className="flex items-center gap-3">
        <UserAvatar name={me.display_name || me.username} className="w-12 h-12 text-lg" />
        <div>
          <h1 className="text-2xl font-bold text-foreground">{STRINGS.title}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">{STRINGS.subtitle}</p>
        </div>
      </div>

      <Card>
        <CardHeader title={STRINGS.account} subtitle={STRINGS.accountSubtitle} />
        <dl className="grid grid-cols-1 sm:grid-cols-[10rem_1fr] gap-x-4 gap-y-3 text-xs items-center">
          <dt className="text-muted-foreground">{STRINGS.username}</dt>
          <dd className="font-mono text-foreground">{me.username}</dd>

          <dt className="text-muted-foreground">
            <label htmlFor="profile-display-name">{STRINGS.displayName}</label>
          </dt>
          <dd>
            <form onSubmit={saveName} className="flex items-center gap-2">
              <input
                id="profile-display-name"
                type="text"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                className="form-input text-xs flex-1 max-w-xs"
              />
              <button type="submit" disabled={savingName || displayName.trim() === me.display_name} className={BUTTON}>
                {savingName ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
                {STRINGS.saveName}
              </button>
            </form>
            {nameError && <p role="alert" className="mt-1 text-[11px] text-destructive">{nameError}</p>}
          </dd>

          <dt className="text-muted-foreground">{STRINGS.role}</dt>
          <dd className="text-foreground">{PERMISSION_STRINGS.roleLabels[me.role] ?? me.role}</dd>

          <dt className="text-muted-foreground">{STRINGS.capabilities}</dt>
          <dd className="flex flex-wrap gap-1" data-testid="profile-capabilities">
            {caps.map((c) => (
              <span
                key={c}
                className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-secondary text-foreground border-border"
              >
                {PERMISSION_STRINGS.capabilityLabels[c] ?? c}
              </span>
            ))}
          </dd>

          <dt className="text-muted-foreground">{STRINGS.agentSpend}</dt>
          <dd className="font-mono text-foreground">
            {me.daily_agent_budget_usd > 0
              ? STRINGS.agentSpendValue(formatUsd(me.today_agent_cost_usd), formatUsd(me.daily_agent_budget_usd))
              : STRINGS.noChat}
          </dd>

          {/* Placeholder - i18n-01 wires the language picker. */}
          <dt className="text-muted-foreground">{STRINGS.language}</dt>
          <dd className="text-muted-foreground" data-testid="profile-language">
            {STRINGS.languagePlaceholder}
          </dd>
        </dl>
      </Card>

      <Card>
        <CardHeader title={STRINGS.password} subtitle={STRINGS.passwordSubtitle} />
        <form onSubmit={changePassword} className="space-y-3 max-w-sm">
          {passwordError && (
            <div role="alert" className="p-2.5 rounded border border-destructive/40 bg-destructive/15 text-xs text-destructive">
              {passwordError}
            </div>
          )}
          <div>
            <label htmlFor="profile-current-password" className={LABEL}>{STRINGS.currentPassword}</label>
            <input
              id="profile-current-password"
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              className="form-input text-xs"
            />
          </div>
          <div>
            <label htmlFor="profile-new-password" className={LABEL}>{STRINGS.newPassword}</label>
            <input
              id="profile-new-password"
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              className="form-input text-xs"
            />
          </div>
          <div>
            <label htmlFor="profile-confirm-password" className={LABEL}>{STRINGS.confirmPassword}</label>
            <input
              id="profile-confirm-password"
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className="form-input text-xs"
            />
          </div>
          <button type="submit" disabled={savingPassword} className={BUTTON}>
            {savingPassword && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            {STRINGS.changePassword}
          </button>
        </form>
      </Card>
    </div>
  );
}
