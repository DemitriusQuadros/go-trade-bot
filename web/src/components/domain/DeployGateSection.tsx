import React, { useEffect, useMemo, useState } from 'react';
import { AlertCircle, AlertTriangle, Loader2, RotateCcw, Save, ShieldCheck } from 'lucide-react';
import { DeployGateConfig } from '@/api/types';
import { apiErrorMessage } from '@/api/client';
import { useDeployGateConfig, useUpdateDeployGateConfig } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { useToast } from '@/context/ToastContext';
import { formatDecimal, parseDecimal, parseInteger } from '@/lib/decimal';
import { tr as i18nTr, type MessageKey } from '@/i18n';
import { useT } from '@/i18n';

// "Deploy gate" section on /settings (B-02 §6): the thresholds the Go
// deploy gate (B-01 §3) applies before any agent auto-deploy, and records
// as evidence on every promotion proposal. Saved on its own via
// PUT /deploy-gate - not by the page's "Save Settings" button.

type NumericKey = Exclude<keyof DeployGateConfig, 'timeframe'>;
type FormState = Record<NumericKey, string> & { timeframe: string };

interface FieldSpec {
  key: NumericKey;
  label: string;
  help: string;
  integer: boolean;
  // Returns an error message, or null when valid.
  validate: (n: number) => string | null;
}

const FIELDS: FieldSpec[] = [
  {
    key: 'min_sharpe_delta',
    get label() {
      return i18nTr('deployGate.field.min_sharpe_delta.label');
    },
    get help() {
      return i18nTr('deployGate.field.min_sharpe_delta.help');
    },
    integer: false,
    validate: () => null,
  },
  {
    key: 'max_drawdown_ratio',
    get label() {
      return i18nTr('deployGate.field.max_drawdown_ratio.label');
    },
    get help() {
      return i18nTr('deployGate.field.max_drawdown_ratio.help');
    },
    integer: false,
    validate: (n) => (n > 0 ? null : i18nTr('deployGate.errPositive')),
  },
  {
    key: 'min_profit_factor',
    get label() {
      return i18nTr('deployGate.field.min_profit_factor.label');
    },
    get help() {
      return i18nTr('deployGate.field.min_profit_factor.help');
    },
    integer: false,
    validate: (n) => (n > 0 ? null : i18nTr('deployGate.errPositive')),
  },
  {
    key: 'min_trades',
    get label() {
      return i18nTr('deployGate.field.min_trades.label');
    },
    get help() {
      return i18nTr('deployGate.field.min_trades.help');
    },
    integer: true,
    validate: (n) => (n >= 1 ? null : i18nTr('deployGate.errAtLeastOne')),
  },
  {
    key: 'lookback_months',
    get label() {
      return i18nTr('deployGate.field.lookback_months.label');
    },
    get help() {
      return i18nTr('deployGate.field.lookback_months.help');
    },
    integer: true,
    validate: (n) => (n >= 1 ? null : i18nTr('deployGate.errAtLeastMonth')),
  },
  {
    key: 'train_months',
    get label() {
      return i18nTr('deployGate.field.train_months.label');
    },
    get help() {
      return i18nTr('deployGate.field.train_months.help');
    },
    integer: true,
    validate: (n) => (n >= 1 ? null : i18nTr('deployGate.errAtLeastMonth')),
  },
  {
    key: 'test_months',
    get label() {
      return i18nTr('deployGate.field.test_months.label');
    },
    get help() {
      return i18nTr('deployGate.field.test_months.help');
    },
    integer: true,
    validate: (n) => (n >= 1 ? null : i18nTr('deployGate.errAtLeastMonth')),
  },
];

const TIMEFRAMES: { value: string; readonly label: string }[] = [
  { value: '', key: 'deployGate.tfStrategy' },
  { value: '1m', key: 'backtest.tf1m' },
  { value: '5m', key: 'backtest.tf5m' },
  { value: '15m', key: 'backtest.tf15m' },
  { value: '1h', key: 'backtest.tf1h' },
  { value: '4h', key: 'backtest.tf4h' },
  { value: '1d', key: 'backtest.tf1d' },
].map(({ value, key }) => ({
  value,
  get label() {
    return i18nTr(key as MessageKey);
  },
}));

function toForm(c: DeployGateConfig): FormState {
  return {
    // formatDecimal is String(): always "." and no grouping, whatever the
    // browser locale - never toLocaleString().
    min_sharpe_delta: formatDecimal(c.min_sharpe_delta ?? 0),
    max_drawdown_ratio: formatDecimal(c.max_drawdown_ratio),
    min_profit_factor: formatDecimal(c.min_profit_factor),
    min_trades: formatDecimal(c.min_trades),
    lookback_months: formatDecimal(c.lookback_months),
    train_months: formatDecimal(c.train_months),
    test_months: formatDecimal(c.test_months),
    timeframe: c.timeframe ?? '',
  };
}

function validateForm(f: FormState): { errors: Partial<Record<NumericKey, string>>; value: DeployGateConfig | null } {
  const errors: Partial<Record<NumericKey, string>> = {};
  const out: Partial<DeployGateConfig> = { timeframe: f.timeframe };
  for (const field of FIELDS) {
    // Explicit, locale-independent parsing (lib/decimal): "1.1" and "1,1"
    // both mean 1.1; thousands separators / mixed separators are rejected
    // instead of guessed, so a value is never sent as NaN or 10x off.
    const raw = f[field.key];
    const n = field.integer ? parseInteger(raw) : parseDecimal(raw);
    if (n === null) {
      if (field.integer && parseDecimal(raw) !== null) {
        errors[field.key] = i18nTr('deployGate.errWhole');
      } else {
        errors[field.key] = field.integer ? i18nTr('deployGate.errEnterWhole') : i18nTr('deployGate.errEnterNumber');
      }
      continue;
    }
    const msg = field.validate(n);
    if (msg) {
      errors[field.key] = msg;
      continue;
    }
    (out as Record<string, number | string>)[field.key] = n;
  }
  return { errors, value: Object.keys(errors).length ? null : (out as DeployGateConfig) };
}

export function DeployGateSection() {
  const t = useT();
  const { data: config, isLoading, error: loadError } = useDeployGateConfig();
  const update = useUpdateDeployGateConfig();
  const { toast } = useToast();

  const [form, setForm] = useState<FormState | null>(null);
  const [baseline, setBaseline] = useState<FormState | null>(null);
  const [showErrors, setShowErrors] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Seed once; later refetches must not clobber in-progress edits.
  useEffect(() => {
    if (config && !baseline) {
      const f = toForm(config);
      setForm(f);
      setBaseline(f);
    }
  }, [config, baseline]);

  const errors = useMemo<Partial<Record<NumericKey, string>>>(() => (form ? validateForm(form).errors : {}), [form]);
  const dirty = !!form && !!baseline && JSON.stringify(form) !== JSON.stringify(baseline);

  const windowWarning = useMemo(() => {
    if (!form) return null;
    const lb = parseInteger(form.lookback_months);
    const tr = parseInteger(form.train_months);
    const te = parseInteger(form.test_months);
    if (lb !== null && tr !== null && te !== null && lb > 0 && tr > 0 && te > 0 && tr + te > lb) {
      return t('deployGate.windowWarning', { sum: tr + te, lookback: lb });
    }
    return null;
  }, [form]);

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault();
    if (!form) return;
    setShowErrors(true);
    setSaveError(null);
    const { value } = validateForm(form);
    if (!value) return;
    update.mutate(value, {
      onSuccess: (saved) => {
        const f = toForm(saved ?? value);
        setForm(f);
        setBaseline(f);
        setShowErrors(false);
        toast(t('deployGate.saved'));
      },
      // 400 (server-side validation) comes back as {"error","message"}.
      onError: (err) => setSaveError(apiErrorMessage(err, t('deployGate.saveFailed'))),
    });
  };

  const set = (key: keyof FormState, value: string) => setForm((f) => (f ? { ...f, [key]: value } : f));

  return (
    <Card id="deploy-gate" className="scroll-mt-20">
      <CardHeader
        title={t('deployGate.title')}
        subtitle={t('deployGate.subtitle')}
        action={<ShieldCheck className="w-5 h-5 text-muted-foreground" />}
      />

      {isLoading || (!form && !loadError) ? (
        <div className="text-xs text-muted-foreground flex items-center gap-2">
          <Loader2 className="w-3.5 h-3.5 animate-spin" /> {t('deployGate.loading')}
        </div>
      ) : loadError || !form ? (
        <div className="p-3 rounded-lg border border-destructive/40 bg-destructive/10 text-xs text-destructive flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0" />
          {t('deployGate.loadFailed', { error: apiErrorMessage(loadError) })}
        </div>
      ) : (
        <form onSubmit={handleSave} noValidate className="space-y-4">
          <p className="text-xs text-muted-foreground">
            {t('deployGate.intro')}
          </p>

          {saveError && (
            <div role="alert" className="p-3 rounded-lg border border-destructive/40 bg-destructive/15 text-xs text-foreground flex items-start gap-2">
              <AlertCircle className="w-4 h-4 text-destructive shrink-0 mt-0.5" />
              <span>{saveError}</span>
            </div>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            {FIELDS.map((field) => {
              const err = showErrors ? errors[field.key] : undefined;
              const id = `deploy-gate-${field.key}`;
              return (
                <div key={field.key} className="flex flex-col gap-1.5">
                  <label htmlFor={id} className="text-xs font-semibold text-foreground">
                    {field.label}
                  </label>
                  <input
                    id={id}
                    // Text, not type="number": a number input formats and
                    // parses with the browser locale (1.1 rendered as "1,1"
                    // under pt-BR). validateForm parses explicitly instead.
                    type="text"
                    inputMode={field.integer ? 'numeric' : 'decimal'}
                    autoComplete="off"
                    spellCheck={false}
                    value={form[field.key]}
                    onChange={(e) => set(field.key, e.target.value)}
                    aria-invalid={!!err}
                    aria-describedby={`${id}-help`}
                    className={`form-input text-sm font-mono ${err ? 'border-destructive' : ''}`}
                  />
                  <p id={`${id}-help`} className={`text-[11px] ${err ? 'text-destructive' : 'text-muted-foreground'}`}>
                    {err ?? field.help}
                  </p>
                </div>
              );
            })}
            <div className="flex flex-col gap-1.5">
              <label htmlFor="deploy-gate-timeframe" className="text-xs font-semibold text-foreground">
                {t('deployGate.timeframe')}
              </label>
              <select
                id="deploy-gate-timeframe"
                value={form.timeframe}
                onChange={(e) => set('timeframe', e.target.value)}
                className="form-select text-sm"
                aria-describedby="deploy-gate-timeframe-help"
              >
                {/* Keep an unknown stored value selectable rather than
                    silently rewriting it on the next save. */}
                {!TIMEFRAMES.some((tf) => tf.value === form.timeframe) && (
                  <option value={form.timeframe}>{form.timeframe}</option>
                )}
                {TIMEFRAMES.map((tf) => (
                  <option key={tf.value} value={tf.value}>
                    {tf.label}
                  </option>
                ))}
              </select>
              <p id="deploy-gate-timeframe-help" className="text-[11px] text-muted-foreground">
                {t('deployGate.timeframeHelp')}
              </p>
            </div>
          </div>

          {windowWarning && (
            <p className="text-[11px] text-warning flex items-start gap-1.5">
              <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-px" />
              {windowWarning}
            </p>
          )}

          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => {
                setForm(baseline);
                setShowErrors(false);
                setSaveError(null);
              }}
              disabled={!dirty || update.isPending}
              className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs flex items-center gap-1.5 px-3 py-1.5 disabled:opacity-50"
            >
              <RotateCcw className="w-3.5 h-3.5" /> {t('deployGate.discard')}
            </button>
            <button
              type="submit"
              disabled={!dirty || update.isPending}
              className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold disabled:opacity-50"
            >
              {update.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
              {update.isPending ? t('common.saving') : t('deployGate.save')}
            </button>
          </div>
        </form>
      )}
    </Card>
  );
}
