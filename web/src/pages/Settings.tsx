import React, { useState, useEffect } from 'react';
import {
  Shield,
  Key,
  Globe,
  Bell,
  Activity,
  AlertCircle,
  Save,
  CheckCircle2,
  Lock,
  Edit3,
  RotateCcw,
  Loader2,
} from 'lucide-react';
import {
  PlatformSettings,
  PlatformSettingsUpdateRequest,
} from '@/api/types';
import { usePlatformSettings, useUpdateSettings } from '@/hooks/queries';
import { isRiskBearingChange } from '@/lib/settingsRiskTier';
import { ApiError } from '@/api/client';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { HelpTooltip } from '@/components/ui/HelpTooltip';
import { CapitalManagementSection } from '@/components/domain/CapitalManagementSection';
import { WebhookTargetsSection } from '@/components/domain/WebhookTargetsSection';
import { DeployGateSection } from '@/components/domain/DeployGateSection';
import { useLocation } from 'react-router-dom';
import { LOCALES, LOCALE_NATIVE, normalizeLocale, tr } from '@/i18n';
import { useT } from '@/i18n';

type SavePhase = 'idle' | 'saving-safe' | 'saving-risk' | 'error';

function humanizeSettingsError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 503) {
      return tr('settings.errDrain');
    }
    if (err.status === 502) {
      return tr('settings.errCredentials');
    }
    if (err.status === 400) {
      return err.body || tr('settings.errInvalid');
    }
  }
  if (err instanceof Error) {
    return err.message;
  }
  return tr('settings.errGeneric');
}

export function Settings() {
  const t = useT();
  const { data: loadedSettings, isLoading, isError, error: loadError } = usePlatformSettings();
  const updateSettingsMutation = useUpdateSettings();

  const [formState, setFormState] = useState<PlatformSettingsUpdateRequest>({
    mode: 'dryrun',
    testnet: true,
    webhook_url: '',
    dry_run: { slippage_pct: 0.05, fee_pct: 0.075, fill_delay_ms: 50 },
    prometheus_url: '',
    grafana_url: '',
    asynqmon_url: '',
    agents_asynqmon_url: '',
    default_locale: 'en',
  });

  const [dirtySecrets, setDirtySecrets] = useState<Record<string, string>>({});
  const [phase, setPhase] = useState<SavePhase>('idle');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [pendingLiveConfirm, setPendingLiveConfirm] = useState(false);
  const location = useLocation();

  // BrowserRouter doesn't scroll to #fragments on its own - honour deep
  // links like /settings#agent-notifications once the page has rendered.
  useEffect(() => {
    if (!location.hash || !loadedSettings) return;
    const el = document.getElementById(location.hash.slice(1));
    el?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [location.hash, loadedSettings]);

  useEffect(() => {
    if (loadedSettings) {
      setFormState({
        broker_api_key: loadedSettings.broker_api_key,
        broker_api_secret: loadedSettings.broker_api_secret,
        broker_testnet_api_key: loadedSettings.broker_testnet_api_key,
        broker_testnet_api_secret: loadedSettings.broker_testnet_api_secret,
        mode: loadedSettings.mode,
        testnet: loadedSettings.testnet,
        webhook_url: loadedSettings.webhook_url || '',
        dry_run: loadedSettings.dry_run || { slippage_pct: 0.05, fee_pct: 0.075, fill_delay_ms: 50 },
        prometheus_url: loadedSettings.prometheus_url || '',
        grafana_url: loadedSettings.grafana_url || '',
        asynqmon_url: loadedSettings.asynqmon_url || '',
        agents_asynqmon_url: loadedSettings.agents_asynqmon_url || '',
        default_locale: normalizeLocale(loadedSettings.default_locale) ?? 'en',
      });
      setDirtySecrets({});
    }
  }, [loadedSettings]);

  const handleSecretChange = (field: string, newValue: string) => {
    setDirtySecrets((prev) => ({ ...prev, [field]: newValue }));
  };

  const handleSecretReset = (field: string) => {
    setDirtySecrets((prev) => {
      const copy = { ...prev };
      delete copy[field];
      return copy;
    });
  };

  const executeSubmit = async (payload: PlatformSettingsUpdateRequest) => {
    if (!loadedSettings) return;

    const isRisky = isRiskBearingChange(loadedSettings, payload);
    setPhase(isRisky ? 'saving-risk' : 'saving-safe');
    setErrorMessage(null);
    setSuccessMessage(null);

    try {
      await updateSettingsMutation.mutateAsync(payload);
      setPhase('idle');
      setSuccessMessage(t('settings.saved'));
      setDirtySecrets({});
    } catch (err: unknown) {
      setPhase('error');
      setErrorMessage(humanizeSettingsError(err));
    }
  };

  const handleSave = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!loadedSettings) return;

    // Assemble payload
    const payload: PlatformSettingsUpdateRequest = {
      ...formState,
      broker_api_key: dirtySecrets.broker_api_key ?? loadedSettings.broker_api_key,
      broker_api_secret: dirtySecrets.broker_api_secret ?? loadedSettings.broker_api_secret,
      broker_testnet_api_key:
        dirtySecrets.broker_testnet_api_key ?? loadedSettings.broker_testnet_api_key,
      broker_testnet_api_secret:
        dirtySecrets.broker_testnet_api_secret ?? loadedSettings.broker_testnet_api_secret,
    };

    // Live mode guard check
    if (payload.mode === 'live' && loadedSettings.mode !== 'live') {
      setPendingLiveConfirm(true);
      return;
    }

    await executeSubmit(payload);
  };

  const handleConfirmLive = async () => {
    if (!loadedSettings) return;
    setPendingLiveConfirm(false);

    const payload: PlatformSettingsUpdateRequest = {
      ...formState,
      confirm_live: true,
      broker_api_key: dirtySecrets.broker_api_key ?? loadedSettings.broker_api_key,
      broker_api_secret: dirtySecrets.broker_api_secret ?? loadedSettings.broker_api_secret,
      broker_testnet_api_key:
        dirtySecrets.broker_testnet_api_key ?? loadedSettings.broker_testnet_api_key,
      broker_testnet_api_secret:
        dirtySecrets.broker_testnet_api_secret ?? loadedSettings.broker_testnet_api_secret,
    };

    await executeSubmit(payload);
  };

  const handleCancelLive = () => {
    setPendingLiveConfirm(false);
    if (loadedSettings) {
      setFormState((prev) => ({ ...prev, mode: loadedSettings.mode }));
    }
  };

  if (isError) {
    return (
      <div className="flex items-center gap-2 p-12 text-destructive">
        <AlertCircle className="w-6 h-6 shrink-0" />
        <span>
          {t('settings.loadFailed', { error: humanizeSettingsError(loadError) })}
        </span>
      </div>
    );
  }

  if (isLoading || !loadedSettings) {
    return (
      <div className="flex items-center justify-center p-12 text-muted-foreground">
        <Loader2 className="w-6 h-6 animate-spin mr-2" /> {t('settings.loading')}
      </div>
    );
  }

  const isSaving = phase === 'saving-safe' || phase === 'saving-risk';

  return (
    <div className="max-w-4xl mx-auto space-y-6 pb-20">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
          {t('settings.title')}
        </h1>
        <p className="text-xs text-muted-foreground mt-1">
          {t('settings.subtitle')}
        </p>
      </div>

      {/* Notifications */}
      {successMessage && (
        <div className="p-4 rounded-xl bg-success/15 border border-success/40 text-xs text-success flex items-center gap-2">
          <CheckCircle2 className="w-4 h-4 text-success shrink-0" />
          <span>{successMessage}</span>
        </div>
      )}

      {/* 1. Broker Credentials */}
      <Card>
        <CardHeader
          title={t('settings.brokerTitle')}
          subtitle={t('settings.brokerSubtitle')}
        />
        <div className="p-6 pt-0 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <MaskedSecretField
              label={t('settings.liveKey')}
              maskedValue={loadedSettings.broker_api_key}
              isDirty={dirtySecrets.broker_api_key !== undefined}
              onSaveChange={(val) => handleSecretChange('broker_api_key', val)}
              onReset={() => handleSecretReset('broker_api_key')}
            />
            <MaskedSecretField
              label={t('settings.liveSecret')}
              maskedValue={loadedSettings.broker_api_secret}
              isDirty={dirtySecrets.broker_api_secret !== undefined}
              onSaveChange={(val) => handleSecretChange('broker_api_secret', val)}
              onReset={() => handleSecretReset('broker_api_secret')}
            />
            <MaskedSecretField
              label={t('settings.testnetKey')}
              maskedValue={loadedSettings.broker_testnet_api_key}
              isDirty={dirtySecrets.broker_testnet_api_key !== undefined}
              onSaveChange={(val) => handleSecretChange('broker_testnet_api_key', val)}
              onReset={() => handleSecretReset('broker_testnet_api_key')}
            />
            <MaskedSecretField
              label={t('settings.testnetSecret')}
              maskedValue={loadedSettings.broker_testnet_api_secret}
              isDirty={dirtySecrets.broker_testnet_api_secret !== undefined}
              onSaveChange={(val) => handleSecretChange('broker_testnet_api_secret', val)}
              onReset={() => handleSecretReset('broker_testnet_api_secret')}
            />
          </div>
        </div>
      </Card>

      {/* 2. Mode & Safety Guards */}
      <Card>
        <CardHeader
          title={t('settings.modeTitle')}
          subtitle={t('settings.modeSubtitle')}
        />
        <div className="p-6 pt-0 space-y-5">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                {t('settings.modeCeiling')}
                <HelpTooltip>{t('settings.modeCeilingHelp')}</HelpTooltip>
              </label>
              <select
                value={formState.mode}
                onChange={(e) =>
                  setFormState({
                    ...formState,
                    mode: e.target.value as PlatformSettings['mode'],
                  })
                }
                className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
              >
                <option value="backtest">{t('settings.optBacktest')}</option>
                <option value="dryrun">{t('settings.optDryrun')}</option>
                <option value="paper">{t('settings.optPaper')}</option>
                <option value="live">{t('settings.optLive')}</option>
              </select>
            </div>

            <div className="flex flex-col justify-center pt-5">
              <label className="flex items-center gap-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={formState.testnet}
                  onChange={(e) => setFormState({ ...formState, testnet: e.target.checked })}
                  className="w-4 h-4 rounded border-border text-muted-foreground focus:ring-ring focus:ring-offset-background"
                />
                <span className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                  {t('settings.testnet')}
                  <HelpTooltip>{t('settings.testnetHelp')}</HelpTooltip>
                </span>
              </label>
            </div>
          </div>

          <div className="pt-2 border-t border-border">
            <h4 className="text-xs font-bold text-foreground uppercase tracking-wider mb-3">
              {t('settings.frictions')}
            </h4>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground flex items-center gap-1">
                  {t('settings.slippage')} <HelpTooltip>{t('settings.slippageHelp')}</HelpTooltip>
                </label>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={formState.dry_run.slippage_pct}
                  onChange={(e) =>
                    setFormState({
                      ...formState,
                      dry_run: {
                        ...formState.dry_run,
                        slippage_pct: parseFloat(e.target.value) || 0,
                      },
                    })
                  }
                  className="px-3 py-1.5 rounded bg-background border border-border text-xs text-foreground focus:outline-none focus:border-primary"
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground flex items-center gap-1">
                  {t('settings.fee')} <HelpTooltip>{t('settings.feeHelp')}</HelpTooltip>
                </label>
                <input
                  type="number"
                  step="0.005"
                  min="0"
                  value={formState.dry_run.fee_pct}
                  onChange={(e) =>
                    setFormState({
                      ...formState,
                      dry_run: {
                        ...formState.dry_run,
                        fee_pct: parseFloat(e.target.value) || 0,
                      },
                    })
                  }
                  className="px-3 py-1.5 rounded bg-background border border-border text-xs text-foreground focus:outline-none focus:border-primary"
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs text-muted-foreground flex items-center gap-1">
                  {t('settings.fillDelay')} <HelpTooltip>{t('settings.fillDelayHelp')}</HelpTooltip>
                </label>
                <input
                  type="number"
                  step="10"
                  min="0"
                  value={formState.dry_run.fill_delay_ms}
                  onChange={(e) =>
                    setFormState({
                      ...formState,
                      dry_run: {
                        ...formState.dry_run,
                        fill_delay_ms: parseInt(e.target.value, 10) || 0,
                      },
                    })
                  }
                  className="px-3 py-1.5 rounded bg-background border border-border text-xs text-foreground focus:outline-none focus:border-primary"
                />
              </div>
            </div>
          </div>
        </div>
      </Card>

      {/* 3. Webhook Notifications */}
      <Card>
        <CardHeader
          title={t('settings.webhookTitle')}
          subtitle={t('settings.webhookSubtitle')}
        />
        <div className="p-6 pt-0">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground">{t('settings.webhookUrl')}</label>
            <input
              type="url"
              value={formState.webhook_url}
              placeholder="https://discord.com/api/webhooks/..."
              onChange={(e) => setFormState({ ...formState, webhook_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
            />
          </div>
          {/* i18n-02 §1: Settings.DefaultLocale. */}
          <div className="flex flex-col gap-1.5 mt-4 max-w-xs">
            <label htmlFor="settings-default-locale" className="text-xs font-semibold text-foreground">
              {t('settings.defaultLocale')}
            </label>
            <select
              id="settings-default-locale"
              value={formState.default_locale ?? 'en'}
              onChange={(e) => setFormState({ ...formState, default_locale: e.target.value })}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
            >
              {LOCALES.map((l) => (
                <option key={l} value={l}>
                  {LOCALE_NATIVE[l]}
                </option>
              ))}
            </select>
            <p className="text-[11px] text-muted-foreground">{t('settings.defaultLocaleHelp')}</p>
          </div>
        </div>
      </Card>

      {/* 4. Monitoring */}
      <Card>
        <CardHeader
          title={t('settings.monitoringTitle')}
          subtitle={t('settings.monitoringSubtitle')}
        />
        <div className="p-6 pt-0 grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground">{t('settings.prometheusUrl')}</label>
            <input
              type="url"
              value={formState.prometheus_url}
              placeholder="http://localhost:9090"
              onChange={(e) => setFormState({ ...formState, prometheus_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground">{t('settings.grafanaUrl')}</label>
            <input
              type="url"
              value={formState.grafana_url}
              placeholder="http://localhost:3000"
              onChange={(e) => setFormState({ ...formState, grafana_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground">{t('settings.workerAsynqUrl')}</label>
            <input
              type="url"
              value={formState.asynqmon_url}
              placeholder="http://localhost:9191/tasks/monitoring"
              onChange={(e) => setFormState({ ...formState, asynqmon_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-foreground">{t('settings.agentsAsynqUrl')}</label>
            <input
              type="url"
              value={formState.agents_asynqmon_url}
              placeholder="http://localhost:9194/tasks/monitoring"
              onChange={(e) => setFormState({ ...formState, agents_asynqmon_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-background border border-border text-sm text-foreground focus:outline-none focus:border-primary"
            />
          </div>
        </div>
      </Card>

      {/* Save Button */}
      <div className="flex justify-end">
        <button
          type="button"
          onClick={handleSave}
          disabled={isSaving}
          className="px-5 py-2.5 rounded-lg bg-primary hover:bg-primary text-sm font-semibold text-white shadow-lg flex items-center gap-2 transition-colors disabled:opacity-50"
        >
          {isSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
          <span>{t('settings.save')}</span>
        </button>
      </div>

      {/* Capital & Account Management - Controls virtual money in dryrun or Binance live sync */}
      <CapitalManagementSection />

      {/* Agent notification targets - saved per target (own dialog), not by
          the Save Settings button above. Deep-linked as
          /settings#agent-notifications from the agent editor. */}
      <WebhookTargetsSection />

      {/* Agents platform Phase B deploy gate - saved on its own via
          PUT /deploy-gate, not by the Save Settings button. Deep-linkable as
          /settings#deploy-gate. */}
      <DeployGateSection />

      {/* Sticky Save Bar & Feedback */}
      <SettingsSaveBar phase={phase} error={errorMessage} />

      {/* Live Mode Confirmation Dialog */}
      <ConfirmDialog
        isOpen={pendingLiveConfirm}
        title={t('settings.liveTitle')}
        message={t('settings.liveMessage')}
        isDangerous={true}
        confirmText={t('settings.liveConfirm')}
        onConfirm={handleConfirmLive}
        onCancel={handleCancelLive}
      />
    </div>
  );
}

function SettingsSaveBar({ phase, error }: { phase: SavePhase; error: string | null }) {
  const t = useT();
  if (phase === 'saving-risk') {
    return (
      <div
        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-3 rounded-xl bg-card/95 border border-border text-xs text-foreground shadow-2xl flex items-center gap-3 animate-in fade-in slide-in-from-bottom-2"
        aria-live="polite"
      >
        <Loader2 className="w-4 h-4 animate-spin text-foreground shrink-0" />
        <span>
          {t('settings.applyingRisk')}
        </span>
      </div>
    );
  }

  if (phase === 'saving-safe') {
    return (
      <div
        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-2.5 rounded-xl bg-card/95 border border-border text-xs text-foreground shadow-2xl flex items-center gap-2 animate-in fade-in slide-in-from-bottom-2"
        aria-live="polite"
      >
        <Loader2 className="w-3.5 h-3.5 animate-spin text-foreground shrink-0" />
        <span>{t('settings.applying')}</span>
      </div>
    );
  }

  if (phase === 'error' && error) {
    return (
      <div
        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 max-w-xl px-4 py-3 rounded-xl bg-destructive/15 border border-destructive/40 text-xs text-destructive shadow-2xl flex items-center gap-2.5 animate-in fade-in slide-in-from-bottom-2"
        aria-live="polite"
      >
        <AlertCircle className="w-4 h-4 text-destructive shrink-0" />
        <span>{error}</span>
      </div>
    );
  }

  return null;
}

function MaskedSecretField({
  label,
  maskedValue,
  isDirty,
  onSaveChange,
  onReset,
}: {
  label: string;
  maskedValue: string;
  isDirty: boolean;
  onSaveChange: (val: string) => void;
  onReset: () => void;
}) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [typedValue, setTypedValue] = useState('');

  const handleApply = () => {
    if (typedValue.trim()) {
      onSaveChange(typedValue.trim());
    }
    setEditing(false);
  };

  const handleCancel = () => {
    setTypedValue('');
    setEditing(false);
  };

  const handleRevert = () => {
    setTypedValue('');
    setEditing(false);
    onReset();
  };

  return (
    <div className="flex flex-col gap-1.5 p-3 rounded-lg bg-background/70 border border-border">
      <div className="flex items-center justify-between">
        <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
          <Key className="w-3 h-3 text-muted-foreground" />
          {label}
        </label>
        {isDirty && (
          <span className="text-[10px] font-bold text-warning uppercase tracking-wider bg-warning/15 px-1.5 py-0.5 rounded border border-warning/40">
            {t('settings.modified')}
          </span>
        )}
      </div>

      {editing ? (
        <div className="space-y-2 pt-1">
          <input
            type="password"
            autoFocus
            value={typedValue}
            onChange={(e) => setTypedValue(e.target.value)}
            placeholder={t('settings.pasteSecret')}
            className="w-full px-2.5 py-1.5 rounded bg-card border border-border text-xs text-foreground focus:outline-none focus:border-primary font-mono"
          />
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={handleCancel}
              className="px-2 py-1 rounded text-xs text-muted-foreground hover:text-foreground"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              onClick={handleApply}
              className="px-2.5 py-1 rounded text-xs font-semibold bg-primary hover:bg-primary text-white"
            >
              {t('settings.confirmValue')}
            </button>
          </div>
        </div>
      ) : (
        <div className="flex items-center justify-between gap-2 pt-0.5">
          <span className="font-mono text-xs text-muted-foreground truncate">
            {isDirty ? t('settings.queued') : maskedValue || t('settings.notConfigured')}
          </span>
          <div className="flex items-center gap-1 shrink-0">
            {isDirty ? (
              <button
                type="button"
                onClick={handleRevert}
                aria-label={t('settings.revertAria', { label })}
                title={t('settings.revertTitle')}
                className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-secondary"
              >
                <RotateCcw className="w-3.5 h-3.5" />
              </button>
            ) : (
              <button
                type="button"
                onClick={() => setEditing(true)}
                aria-label={t('settings.changeAria', { label })}
                className="px-2 py-1 rounded text-xs font-medium text-foreground hover:text-foreground hover:bg-card/40 border border-border/60 flex items-center gap-1"
              >
                <Edit3 className="w-3 h-3" /> {t('settings.change')}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
