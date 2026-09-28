import React, { useEffect, useMemo, useState } from 'react';
import { AlertCircle, AlertTriangle, Loader2, RotateCcw, Save, ShieldCheck } from 'lucide-react';
import { DeployGateConfig } from '@/api/types';
import { apiErrorMessage } from '@/api/client';
import { useDeployGateConfig, useUpdateDeployGateConfig } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { useToast } from '@/context/ToastContext';

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
  step: string;
  // Returns an error message, or null when valid.
  validate: (n: number) => string | null;
}

const FIELDS: FieldSpec[] = [
  {
    key: 'min_sharpe_delta',
    label: 'Min Sharpe delta',
    help: "Candidate Sharpe ratio must be at least the current version's plus this. 0 = no worse than today.",
    integer: false,
    step: '0.01',
    validate: () => null,
  },
  {
    key: 'max_drawdown_ratio',
    label: 'Max drawdown ratio',
    help: "Candidate max drawdown may be at most this multiple of the current version's (1.10 = up to 10% deeper).",
    integer: false,
    step: '0.01',
    validate: (n) => (n > 0 ? null : 'Must be greater than 0.'),
  },
  {
    key: 'min_profit_factor',
    label: 'Min profit factor',
    help: 'Candidate gross profit divided by gross loss must be at least this. 1.0 = break-even.',
    integer: false,
    step: '0.01',
    validate: (n) => (n > 0 ? null : 'Must be greater than 0.'),
  },
  {
    key: 'min_trades',
    label: 'Min trades',
    help: 'Fewest out-of-sample trades the candidate must make for its numbers to count at all.',
    integer: true,
    step: '1',
    validate: (n) => (n >= 1 ? null : 'Must be at least 1.'),
  },
  {
    key: 'lookback_months',
    label: 'Lookback (months)',
    help: 'How much recent history both versions are walk-forward tested over, ending now.',
    integer: true,
    step: '1',
    validate: (n) => (n >= 1 ? null : 'Must be at least 1 month.'),
  },
  {
    key: 'train_months',
    label: 'Train window (months)',
    help: 'Length of each walk-forward in-sample window.',
    integer: true,
    step: '1',
    validate: (n) => (n >= 1 ? null : 'Must be at least 1 month.'),
  },
  {
    key: 'test_months',
    label: 'Test window (months)',
    help: 'Length of each out-of-sample window - the metrics above are measured on these.',
    integer: true,
    step: '1',
    validate: (n) => (n >= 1 ? null : 'Must be at least 1 month.'),
  },
];

const TIMEFRAMES: { value: string; label: string }[] = [
  { value: '', label: "Strategy's own (from its cycle)" },
  { value: '1m', label: '1 Minute (1m)' },
  { value: '5m', label: '5 Minutes (5m)' },
  { value: '15m', label: '15 Minutes (15m)' },
  { value: '1h', label: '1 Hour (1h)' },
  { value: '4h', label: '4 Hours (4h)' },
  { value: '1d', label: '1 Day (1d)' },
];

function toForm(c: DeployGateConfig): FormState {
  return {
    min_sharpe_delta: String(c.min_sharpe_delta ?? 0),
    max_drawdown_ratio: String(c.max_drawdown_ratio ?? ''),
    min_profit_factor: String(c.min_profit_factor ?? ''),
    min_trades: String(c.min_trades ?? ''),
    lookback_months: String(c.lookback_months ?? ''),
    train_months: String(c.train_months ?? ''),
    test_months: String(c.test_months ?? ''),
    timeframe: c.timeframe ?? '',
  };
}

function validateForm(f: FormState): { errors: Partial<Record<NumericKey, string>>; value: DeployGateConfig | null } {
  const errors: Partial<Record<NumericKey, string>> = {};
  const out: Partial<DeployGateConfig> = { timeframe: f.timeframe };
  for (const field of FIELDS) {
    const raw = f[field.key].trim();
    const n = Number(raw);
    if (raw === '' || !Number.isFinite(n)) {
      errors[field.key] = 'Enter a number.';
      continue;
    }
    if (field.integer && !Number.isInteger(n)) {
      errors[field.key] = 'Must be a whole number.';
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
    const lb = Number(form.lookback_months);
    const tr = Number(form.train_months);
    const te = Number(form.test_months);
    if ([lb, tr, te].every((n) => Number.isFinite(n) && n > 0) && tr + te > lb) {
      return `Train + test (${tr + te} months) is longer than the lookback (${lb} months) - a complete walk-forward window may not fit, and the gate fails when there isn't enough history.`;
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
        toast('Deploy gate saved');
      },
      // 400 (server-side validation) comes back as {"error","message"}.
      onError: (err) => setSaveError(apiErrorMessage(err, "Couldn't save the deploy gate")),
    });
  };

  const set = (key: keyof FormState, value: string) => setForm((f) => (f ? { ...f, [key]: value } : f));

  return (
    <Card id="deploy-gate" className="scroll-mt-20">
      <CardHeader
        title="Deploy Gate"
        subtitle="Hard checks, computed in Go, that an agent's code change must pass before it can auto-deploy to a non-live strategy"
        action={<ShieldCheck className="w-5 h-5 text-muted-foreground" />}
      />

      {isLoading || (!form && !loadError) ? (
        <div className="text-xs text-muted-foreground flex items-center gap-2">
          <Loader2 className="w-3.5 h-3.5 animate-spin" /> Loading deploy gate...
        </div>
      ) : loadError || !form ? (
        <div className="p-3 rounded-lg border border-destructive/40 bg-destructive/10 text-xs text-destructive flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0" />
          Couldn't load the deploy gate: {apiErrorMessage(loadError)}
        </div>
      ) : (
        <form onSubmit={handleSave} noValidate className="space-y-4">
          <p className="text-xs text-muted-foreground">
            Each check compares the candidate code against the strategy's current code, both walk-forward tested over
            the same window. A change that fails any check is never deployed - it's filed as a proposal for you
            instead. Agents can read these values but can't change them.
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
                    type="number"
                    step={field.step}
                    min={field.key === 'min_sharpe_delta' ? undefined : field.integer ? 1 : 0}
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
                Timeframe
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
                {!TIMEFRAMES.some((t) => t.value === form.timeframe) && (
                  <option value={form.timeframe}>{form.timeframe}</option>
                )}
                {TIMEFRAMES.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
              </select>
              <p id="deploy-gate-timeframe-help" className="text-[11px] text-muted-foreground">
                Candle resolution for the gate's backtests.
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
              <RotateCcw className="w-3.5 h-3.5" /> Discard
            </button>
            <button
              type="submit"
              disabled={!dirty || update.isPending}
              className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold disabled:opacity-50"
            >
              {update.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
              {update.isPending ? 'Saving...' : 'Save deploy gate'}
            </button>
          </div>
        </form>
      )}
    </Card>
  );
}
