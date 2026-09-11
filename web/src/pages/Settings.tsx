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

type SavePhase = 'idle' | 'saving-safe' | 'saving-risk' | 'error';

function humanizeSettingsError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 503) {
      return (
        "Couldn't apply — active trading cycles didn't finish within 30 seconds. " +
        'Nothing was changed; your previous settings are still in effect. Try again shortly.'
      );
    }
    if (err.status === 502) {
      return (
        'The new broker credentials were rejected when applying them. Nothing was changed — ' +
        'double-check the values and try again.'
      );
    }
    if (err.status === 400) {
      return err.body || 'Invalid settings — check the highlighted fields.';
    }
  }
  if (err instanceof Error) {
    return err.message;
  }
  return 'Failed to save settings. Please try again.';
}

export function Settings() {
  const { data: loadedSettings, isLoading, isError, error: loadError } = usePlatformSettings();
  const updateSettingsMutation = useUpdateSettings();

  const [formState, setFormState] = useState<PlatformSettingsUpdateRequest>({
    mode: 'dryrun',
    testnet: true,
    webhook_url: '',
    dry_run: { slippage_pct: 0.1, fee_pct: 0.1, fill_delay_ms: 50 },
    prometheus_url: '',
    grafana_url: '',
    asynqmon_url: '',
  });

  const [dirtySecrets, setDirtySecrets] = useState<Record<string, string>>({});
  const [phase, setPhase] = useState<SavePhase>('idle');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [pendingLiveConfirm, setPendingLiveConfirm] = useState(false);

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
        dry_run: loadedSettings.dry_run || { slippage_pct: 0.1, fee_pct: 0.1, fill_delay_ms: 50 },
        prometheus_url: loadedSettings.prometheus_url || '',
        grafana_url: loadedSettings.grafana_url || '',
        asynqmon_url: loadedSettings.asynqmon_url || '',
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
      setSuccessMessage('Settings successfully updated and applied in-process.');
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
      <div className="flex items-center gap-2 p-12 text-red-400">
        <AlertCircle className="w-6 h-6 shrink-0" />
        <span>
          Failed to load platform configuration: {humanizeSettingsError(loadError)}
        </span>
      </div>
    );
  }

  if (isLoading || !loadedSettings) {
    return (
      <div className="flex items-center justify-center p-12 text-slate-400">
        <Loader2 className="w-6 h-6 animate-spin mr-2" /> Loading platform configuration...
      </div>
    );
  }

  const isSaving = phase === 'saving-safe' || phase === 'saving-risk';

  return (
    <div className="max-w-4xl mx-auto space-y-6 pb-20">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-white flex items-center gap-2">
          Platform Settings
        </h1>
        <p className="text-xs text-slate-400 mt-1">
          Configure broker exchange API keys, trading modes, safety guards, and monitoring endpoints.
        </p>
      </div>

      {/* Notifications */}
      {successMessage && (
        <div className="p-4 rounded-xl bg-emerald-950/60 border border-emerald-800 text-xs text-emerald-300 flex items-center gap-2">
          <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
          <span>{successMessage}</span>
        </div>
      )}

      {/* 1. Broker Credentials */}
      <Card>
        <CardHeader
          title="Broker Exchange API Credentials"
          subtitle="Real and testnet API keys for order execution and market data streaming"
        />
        <div className="p-6 pt-0 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <MaskedSecretField
              label="Live API Key"
              maskedValue={loadedSettings.broker_api_key}
              isDirty={dirtySecrets.broker_api_key !== undefined}
              onSaveChange={(val) => handleSecretChange('broker_api_key', val)}
              onReset={() => handleSecretReset('broker_api_key')}
            />
            <MaskedSecretField
              label="Live API Secret"
              maskedValue={loadedSettings.broker_api_secret}
              isDirty={dirtySecrets.broker_api_secret !== undefined}
              onSaveChange={(val) => handleSecretChange('broker_api_secret', val)}
              onReset={() => handleSecretReset('broker_api_secret')}
            />
            <MaskedSecretField
              label="Testnet API Key"
              maskedValue={loadedSettings.broker_testnet_api_key}
              isDirty={dirtySecrets.broker_testnet_api_key !== undefined}
              onSaveChange={(val) => handleSecretChange('broker_testnet_api_key', val)}
              onReset={() => handleSecretReset('broker_testnet_api_key')}
            />
            <MaskedSecretField
              label="Testnet API Secret"
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
          title="Mode & Safety Guards"
          subtitle="System-wide operating mode ceiling and dry-run execution frictions"
        />
        <div className="p-6 pt-0 space-y-5">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-semibold text-slate-300 flex items-center gap-1.5">
                Operating Mode Ceiling
                <HelpTooltip>
                  Global mode ceiling. Individual strategies cannot exceed this operating mode.
                </HelpTooltip>
              </label>
              <select
                value={formState.mode}
                onChange={(e) =>
                  setFormState({
                    ...formState,
                    mode: e.target.value as PlatformSettings['mode'],
                  })
                }
                className="px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500"
              >
                <option value="backtest">Backtest (backtest)</option>
                <option value="dryrun">Dry Run (dryrun)</option>
                <option value="paper">Paper Trading (paper)</option>
                <option value="live">Real Live Trading (live)</option>
              </select>
            </div>

            <div className="flex flex-col justify-center pt-5">
              <label className="flex items-center gap-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={formState.testnet}
                  onChange={(e) => setFormState({ ...formState, testnet: e.target.checked })}
                  className="w-4 h-4 rounded border-slate-700 text-blue-600 focus:ring-blue-500 focus:ring-offset-slate-900"
                />
                <span className="text-xs font-semibold text-slate-200 flex items-center gap-1.5">
                  Use Exchange Testnet
                  <HelpTooltip>
                    When enabled, connects to Binance Testnet endpoints rather than production exchanges.
                  </HelpTooltip>
                </span>
              </label>
            </div>
          </div>

          <div className="pt-2 border-t border-slate-800">
            <h4 className="text-xs font-bold text-slate-300 uppercase tracking-wider mb-3">
              Dry-Run & Paper Simulation Frictions
            </h4>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="flex flex-col gap-1">
                <label className="text-xs text-slate-400 flex items-center gap-1">
                  Slippage (%) <HelpTooltip>Simulated adverse execution slippage per fill</HelpTooltip>
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
                  className="px-3 py-1.5 rounded bg-slate-950 border border-slate-700 text-xs text-slate-200 focus:outline-none focus:border-blue-500"
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs text-slate-400 flex items-center gap-1">
                  Fee (%) <HelpTooltip>Simulated exchange commission fee per trade</HelpTooltip>
                </label>
                <input
                  type="number"
                  step="0.01"
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
                  className="px-3 py-1.5 rounded bg-slate-950 border border-slate-700 text-xs text-slate-200 focus:outline-none focus:border-blue-500"
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs text-slate-400 flex items-center gap-1">
                  Fill Delay (ms) <HelpTooltip>Simulated network & queue latency before fill</HelpTooltip>
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
                  className="px-3 py-1.5 rounded bg-slate-950 border border-slate-700 text-xs text-slate-200 focus:outline-none focus:border-blue-500"
                />
              </div>
            </div>
          </div>
        </div>
      </Card>

      {/* 3. Webhook Notifications */}
      <Card>
        <CardHeader
          title="Webhook Notifications"
          subtitle="Dispatch trade execution and risk alert events to Discord, Telegram, or custom endpoints"
        />
        <div className="p-6 pt-0">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-slate-300">Webhook URL</label>
            <input
              type="url"
              value={formState.webhook_url}
              placeholder="https://discord.com/api/webhooks/..."
              onChange={(e) => setFormState({ ...formState, webhook_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500"
            />
          </div>
        </div>
      </Card>

      {/* 4. Monitoring */}
      <Card>
        <CardHeader
          title="Monitoring & Observability Endpoints"
          subtitle="External dashboards linked in the application navigation"
        />
        <div className="p-6 pt-0 grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-slate-300">Prometheus URL</label>
            <input
              type="url"
              value={formState.prometheus_url}
              placeholder="http://localhost:9090"
              onChange={(e) => setFormState({ ...formState, prometheus_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-slate-300">Grafana Dashboard URL</label>
            <input
              type="url"
              value={formState.grafana_url}
              placeholder="http://localhost:3000"
              onChange={(e) => setFormState({ ...formState, grafana_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-slate-300">Asynqmon URL</label>
            <input
              type="url"
              value={formState.asynqmon_url}
              placeholder="http://localhost:9191/tasks/monitoring"
              onChange={(e) => setFormState({ ...formState, asynqmon_url: e.target.value })}
              className="px-3 py-2 rounded-lg bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-blue-500"
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
          className="px-5 py-2.5 rounded-lg bg-blue-600 hover:bg-blue-500 text-sm font-semibold text-white shadow-lg flex items-center gap-2 transition-colors disabled:opacity-50"
        >
          {isSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
          <span>Save Settings</span>
        </button>
      </div>

      {/* Sticky Save Bar & Feedback */}
      <SettingsSaveBar phase={phase} error={errorMessage} />

      {/* Live Mode Confirmation Dialog */}
      <ConfirmDialog
        isOpen={pendingLiveConfirm}
        title="Confirm Switch to LIVE Mode"
        message="Switching to LIVE mode will place real orders with real capital using your configured broker credentials. This cannot be undone by canceling after the fact — you'll need to switch back to a safer mode explicitly. Continue?"
        isDangerous={true}
        confirmText="Confirm & Enable LIVE"
        onConfirm={handleConfirmLive}
        onCancel={handleCancelLive}
      />
    </div>
  );
}

function SettingsSaveBar({ phase, error }: { phase: SavePhase; error: string | null }) {
  if (phase === 'saving-risk') {
    return (
      <div
        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-3 rounded-xl bg-blue-950/95 border border-blue-800 text-xs text-blue-200 shadow-2xl flex items-center gap-3 animate-in fade-in slide-in-from-bottom-2"
        aria-live="polite"
      >
        <Loader2 className="w-4 h-4 animate-spin text-blue-400 shrink-0" />
        <span>
          Applying in-process — waiting for active trading cycles to drain before swapping credentials/mode.
          This can take up to 30 seconds.
        </span>
      </div>
    );
  }

  if (phase === 'saving-safe') {
    return (
      <div
        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-2.5 rounded-xl bg-slate-900/95 border border-slate-700 text-xs text-slate-200 shadow-2xl flex items-center gap-2 animate-in fade-in slide-in-from-bottom-2"
        aria-live="polite"
      >
        <Loader2 className="w-3.5 h-3.5 animate-spin text-blue-400 shrink-0" />
        <span>Applying settings…</span>
      </div>
    );
  }

  if (phase === 'error' && error) {
    return (
      <div
        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 max-w-xl px-4 py-3 rounded-xl bg-rose-950/95 border border-rose-800 text-xs text-rose-200 shadow-2xl flex items-center gap-2.5 animate-in fade-in slide-in-from-bottom-2"
        aria-live="polite"
      >
        <AlertCircle className="w-4 h-4 text-rose-400 shrink-0" />
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
    <div className="flex flex-col gap-1.5 p-3 rounded-lg bg-slate-950/70 border border-slate-800">
      <div className="flex items-center justify-between">
        <label className="text-xs font-semibold text-slate-300 flex items-center gap-1.5">
          <Key className="w-3 h-3 text-slate-500" />
          {label}
        </label>
        {isDirty && (
          <span className="text-[10px] font-bold text-amber-400 uppercase tracking-wider bg-amber-950/60 px-1.5 py-0.5 rounded border border-amber-800">
            Modified
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
            placeholder="Paste new secret here"
            className="w-full px-2.5 py-1.5 rounded bg-slate-900 border border-slate-700 text-xs text-slate-200 focus:outline-none focus:border-blue-500 font-mono"
          />
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={handleCancel}
              className="px-2 py-1 rounded text-xs text-slate-400 hover:text-slate-200"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleApply}
              className="px-2.5 py-1 rounded text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white"
            >
              Confirm New Value
            </button>
          </div>
        </div>
      ) : (
        <div className="flex items-center justify-between gap-2 pt-0.5">
          <span className="font-mono text-xs text-slate-400 truncate">
            {isDirty ? '•••••••••••••••• (New value queued)' : maskedValue || '(Not configured)'}
          </span>
          <div className="flex items-center gap-1 shrink-0">
            {isDirty ? (
              <button
                type="button"
                onClick={handleRevert}
                aria-label={`Revert ${label}`}
                title="Revert to original saved value"
                className="p-1 rounded text-slate-400 hover:text-slate-200 hover:bg-slate-800"
              >
                <RotateCcw className="w-3.5 h-3.5" />
              </button>
            ) : (
              <button
                type="button"
                onClick={() => setEditing(true)}
                aria-label={`Change ${label}`}
                className="px-2 py-1 rounded text-xs font-medium text-blue-400 hover:text-blue-300 hover:bg-blue-950/40 border border-blue-900/60 flex items-center gap-1"
              >
                <Edit3 className="w-3 h-3" /> Change
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
