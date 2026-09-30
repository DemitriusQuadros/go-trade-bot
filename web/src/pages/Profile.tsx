import React, { useEffect, useState } from 'react';
import { Loader2, Save } from 'lucide-react';
import { api, apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { useToast } from '@/context/ToastContext';
import { Card, CardHeader } from '@/components/ui/Card';
import { UserAvatar } from '@/components/layout/UserMenu';
import { formatUsd } from '@/lib/format';
import { useT } from '@/i18n';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';


const LABEL = 'block text-xs font-semibold text-foreground mb-1.5';
const MIN_PASSWORD = 10;
const BUTTON =
  'bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold disabled:opacity-50';

// /profile (auth-02 §6) - every signed-in user.
export function Profile() {
  const { me, refresh } = useAuth();
  const { toast } = useToast();
  const t = useT();

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
      setNameError(t('profile.nameRequired'));
      return;
    }
    setSavingName(true);
    try {
      await api.updateMe({ display_name: displayName.trim() });
      await refresh();
      toast(t('profile.nameSaved'));
    } catch (err) {
      setNameError(apiErrorMessage(err, t('profile.nameSaveFailed')));
    } finally {
      setSavingName(false);
    }
  };

  const changePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setPasswordError(null);
    if (!currentPassword || !newPassword || !confirmPassword) return setPasswordError(t('profile.passwordMissing'));
    if (newPassword.length < MIN_PASSWORD) return setPasswordError(t('profile.passwordShort'));
    if (newPassword !== confirmPassword) return setPasswordError(t('profile.passwordMismatch'));
    setSavingPassword(true);
    try {
      await api.updateMe({ current_password: currentPassword, new_password: newPassword });
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      toast(t('profile.passwordChanged'));
    } catch (err) {
      setPasswordError(apiErrorMessage(err, t('profile.passwordFailed')));
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
          <h1 className="text-2xl font-bold text-foreground">{t('profile.title')}</h1>
          <p className="text-xs text-muted-foreground mt-0.5">{t('profile.subtitle')}</p>
        </div>
      </div>

      <Card>
        <CardHeader title={t('profile.account')} subtitle={t('profile.accountSubtitle')} />
        <dl className="grid grid-cols-1 sm:grid-cols-[10rem_1fr] gap-x-4 gap-y-3 text-xs items-center">
          <dt className="text-muted-foreground">{t('profile.username')}</dt>
          <dd className="font-mono text-foreground">{me.username}</dd>

          <dt className="text-muted-foreground">
            <label htmlFor="profile-display-name">{t('profile.displayName')}</label>
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
                {t('common.save')}
              </button>
            </form>
            {nameError && <p role="alert" className="mt-1 text-[11px] text-destructive">{nameError}</p>}
          </dd>

          <dt className="text-muted-foreground">{t('profile.role')}</dt>
          <dd className="text-foreground">{t.enum('role', me.role)}</dd>

          <dt className="text-muted-foreground">{t('profile.capabilities')}</dt>
          <dd className="flex flex-wrap gap-1" data-testid="profile-capabilities">
            {caps.map((c) => (
              <span
                key={c}
                className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border bg-secondary text-foreground border-border"
              >
                {t.enum('capability', c)}
              </span>
            ))}
          </dd>

          <dt className="text-muted-foreground">{t('profile.agentSpend')}</dt>
          <dd className="font-mono text-foreground">
            {me.daily_agent_budget_usd > 0
              ? t('profile.agentSpendValue', { spent: formatUsd(me.today_agent_cost_usd), budget: formatUsd(me.daily_agent_budget_usd) })
              : t('profile.noChat')}
          </dd>

          {/* i18n-01 §2: saved on the account via PATCH /auth/me {locale}. */}
          <dt className="text-muted-foreground">{t('profile.language')}</dt>
          <dd data-testid="profile-language">
            <LanguageSwitcher testId="profile-language-switcher" />
            <p className="mt-1 text-[11px] text-muted-foreground">{t('profile.languageHelp')}</p>
          </dd>
        </dl>
      </Card>

      <Card>
        <CardHeader title={t('profile.password')} subtitle={t('profile.passwordSubtitle')} />
        <form onSubmit={changePassword} className="space-y-3 max-w-sm">
          {passwordError && (
            <div role="alert" className="p-2.5 rounded border border-destructive/40 bg-destructive/15 text-xs text-destructive">
              {passwordError}
            </div>
          )}
          <div>
            <label htmlFor="profile-current-password" className={LABEL}>{t('profile.currentPassword')}</label>
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
            <label htmlFor="profile-new-password" className={LABEL}>{t('profile.newPassword')}</label>
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
            <label htmlFor="profile-confirm-password" className={LABEL}>{t('profile.confirmPassword')}</label>
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
            {t('profile.changePassword')}
          </button>
        </form>
      </Card>
    </div>
  );
}
