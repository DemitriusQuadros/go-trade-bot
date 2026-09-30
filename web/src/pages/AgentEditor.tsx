import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { AlertCircle, AlertTriangle, ArrowLeft, Clock, ExternalLink, History, Loader2, Plus, Save, Trash2 } from 'lucide-react';
import { Agent, AgentPermission, AgentProvider, AgentRequest } from '@/api/types';
import { apiErrorCode, apiErrorMessage } from '@/api/client';
import {
  useAgent,
  useAgents,
  useAgentUsage,
  useCreateAgent,
  useStrategies,
  useUpdateAgent,
  useWebhookTargets,
} from '@/hooks/queries';
import { useUnsavedChangesGuard } from '@/hooks/useUnsavedChangesGuard';
import { useAuth } from '@/context/AuthContext';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { HelpTooltip } from '@/components/ui/HelpTooltip';
import { LoadingScreen } from '@/components/ui/Spinner';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { DefaultAgentBadge } from '@/components/domain/AgentBadges';
import {
  AgentTriggersEditor,
  TriggersDraft,
  draftFromTriggers,
  toTriggerPayload,
  validateTriggers,
} from '@/components/domain/AgentTriggersEditor';
import { UsageSparkline } from '@/components/charts/UsageSparkline';
import { useToast } from '@/context/ToastContext';
import { AGENT_PERMISSIONS } from '@/lib/agentPermissions';
import { CRON_PRESETS, describeCron } from '@/lib/cron';
import { formatTokens, formatUsd } from '@/lib/time';
import { normalizeTriggers } from '@/lib/triggers';

// Form state mirrors AgentRequest, except the budget stays a string while
// typing (so "0." or "" don't get coerced mid-edit) and the Phase C
// triggers (events/market/chain_from) live in a string-typed draft owned by
// AgentTriggersEditor, converted back to the C-01 §1 shape on save.
interface FormState {
  name: string;
  goal: string;
  provider: AgentProvider;
  model: string;
  permissions: AgentPermission[];
  strategy_ids: number[];
  webhook_target_ids: number[];
  cron: string[];
  daily_budget_usd: string;
  max_auto_deploys_per_day: number;
  // Kept as text so a blank field means "use the trigger default" (0).
  max_iterations: string;
  triggers: TriggersDraft;
}

const EMPTY_FORM: FormState = {
  name: '',
  goal: '',
  provider: '',
  model: '',
  permissions: ['read'],
  strategy_ids: [],
  webhook_target_ids: [],
  cron: [],
  daily_budget_usd: '1',
  max_auto_deploys_per_day: 0,
  max_iterations: '',
  triggers: draftFromTriggers({ events: [], market: [], chain_from: [] }),
};

function formFromAgent(a: Agent): FormState {
  // normalizeTriggers also accepts the pre-C shapes (events: string[],
  // chain_from: number[]) in case the API hands one back unconverted.
  const { cron, ...otherTriggers } = normalizeTriggers(a.triggers);
  return {
    name: a.name,
    goal: a.goal,
    provider: a.provider ?? '',
    model: a.model ?? '',
    permissions: [...(a.permissions ?? [])],
    strategy_ids: [...(a.strategy_ids ?? [])],
    webhook_target_ids: [...(a.webhook_target_ids ?? [])],
    cron: [...cron],
    daily_budget_usd: String(a.daily_budget_usd ?? 0),
    max_auto_deploys_per_day: a.max_auto_deploys_per_day ?? 0,
    max_iterations: a.max_iterations ? String(a.max_iterations) : '',
    triggers: draftFromTriggers(otherTriggers),
  };
}

function toRequest(f: FormState): AgentRequest {
  const budget = parseFloat(f.daily_budget_usd);
  return {
    name: f.name.trim(),
    goal: f.goal,
    provider: f.provider,
    model: f.model.trim(),
    permissions: f.permissions,
    triggers: { ...toTriggerPayload(f.triggers), cron: f.cron.map((c) => c.trim()).filter(Boolean) },
    strategy_ids: f.strategy_ids,
    webhook_target_ids: f.webhook_target_ids,
    // handleSave has already validated this is a finite number >= 0.
    daily_budget_usd: Number.isFinite(budget) ? budget : 0,
    max_auto_deploys_per_day: f.max_auto_deploys_per_day,
    // handleSave has already validated this is blank, 0, or an integer 4-50.
    max_iterations: parseMaxIterations(f.max_iterations) ?? 0,
  };
}

const MIN_ITERATIONS = 4;
const MAX_ITERATIONS = 50;

// Blank -> 0 (trigger default). Returns null when the text isn't a whole
// number or falls outside 0 / 4-50, so handleSave can reject it.
function parseMaxIterations(raw: string): number | null {
  const t = raw.trim();
  if (t === '') return 0;
  if (!/^\d+$/.test(t)) return null;
  const n = Number(t);
  if (n === 0 || (n >= MIN_ITERATIONS && n <= MAX_ITERATIONS)) return n;
  return null;
}

function toggle<T>(list: T[], item: T): T[] {
  return list.includes(item) ? list.filter((x) => x !== item) : [...list, item];
}

const LABEL = 'text-xs font-semibold text-foreground flex items-center gap-1.5';
const HELP = 'text-[11px] text-muted-foreground';

// Agent editor (A-03 §4) - /agents/new and /agents/:id.
export function AgentEditor() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const agentId = id ? Number(id) : 0;
  const isEdit = agentId > 0;

  const { data: agent, isLoading, error: loadError } = useAgent(agentId);
  const { data: strategies = [] } = useStrategies();
  // Only admins create/edit agents (auth-02 §4); everyone else gets a
  // read-only view. Webhook targets are admin-only data.
  const isAdmin = useAuth().can('admin');
  const readOnly = !isAdmin;
  const { data: targets = [] } = useWebhookTargets(isAdmin);
  const { data: allAgents = [] } = useAgents();
  const createAgent = useCreateAgent();
  const updateAgent = useUpdateAgent(agentId);
  const { toast } = useToast();

  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [baseline, setBaseline] = useState<FormState>(EMPTY_FORM);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [newCron, setNewCron] = useState('');
  const [strategySearch, setStrategySearch] = useState('');
  // Trigger field errors appear only after the first save attempt, then
  // track edits live. chainError holds the backend's 400 chain/cycle
  // message so it can be shown inline in the chain sub-section.
  const [showTriggerErrors, setShowTriggerErrors] = useState(false);
  const [chainError, setChainError] = useState<string | null>(null);
  const chainRef = useRef<HTMLElement>(null);

  // Seed the form once per agent. Later refetches (window refocus, list
  // invalidations) must not clobber in-progress edits; after a save the
  // form is re-seeded explicitly from the response.
  const seededFor = useRef<number | null>(null);
  useEffect(() => {
    if (agent && seededFor.current !== agent.id) {
      seededFor.current = agent.id;
      const f = formFromAgent(agent);
      setForm(f);
      setBaseline(f);
    }
  }, [agent]);

  const dirty = useMemo(() => JSON.stringify(form) !== JSON.stringify(baseline), [form, baseline]);
  const guard = useUnsavedChangesGuard(dirty);

  const triggerErrors = useMemo(() => validateTriggers(form.triggers), [form.triggers]);
  // Suggest the monitored symbols of the strategies bound to this agent.
  const symbolSuggestions = useMemo(() => {
    const set = new Set<string>();
    for (const s of strategies) {
      if (form.strategy_ids.includes(s.id)) (s.monitored_symbols ?? []).forEach((sym) => set.add(sym.toUpperCase()));
    }
    return [...set].sort();
  }, [strategies, form.strategy_ids]);

  const saving = createAgent.isPending || updateAgent.isPending;
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => setForm((f) => ({ ...f, [key]: value }));

  const addCron = (spec: string) => {
    const s = spec.trim();
    if (!s || form.cron.includes(s)) return;
    set('cron', [...form.cron, s]);
  };

  const handleSave = (e?: React.FormEvent) => {
    e?.preventDefault();
    if (readOnly) return;
    setSaveError(null);
    setChainError(null);
    setShowTriggerErrors(true);
    const budget = Number(form.daily_budget_usd);
    if (form.daily_budget_usd.trim() === '' || !Number.isFinite(budget) || budget < 0) {
      setSaveError('Daily budget must be a number of US dollars, 0 or more (0 = unlimited).');
      return;
    }
    if (parseMaxIterations(form.max_iterations) === null) {
      setSaveError(
        `Max tool iterations must be blank (use the default) or a whole number from ${MIN_ITERATIONS} to ${MAX_ITERATIONS}.`,
      );
      window.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }
    if (triggerErrors.count > 0) {
      setSaveError(
        `Fix the ${triggerErrors.count} highlighted trigger field${triggerErrors.count === 1 ? '' : 's'} in the Schedule section.`,
      );
      window.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }
    const req = toRequest(form);
    const onError = (err: unknown) => {
      // 400 (invalid cron / permission / budget / trigger, chain cycle) and
      // 409 (duplicate name) come back as {"error","message"} - shown
      // inline, form state kept.
      const msg = apiErrorMessage(err, 'Failed to save the agent');
      setSaveError(msg);
      // Chain-cycle and self-chain rejections: 400 {"error":"chain_cycle"}.
      if (apiErrorCode(err) === 'chain_cycle') {
        setChainError(msg);
        chainRef.current?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      } else {
        window.scrollTo({ top: 0, behavior: 'smooth' });
      }
    };
    if (isEdit) {
      updateAgent.mutate(req, {
        onSuccess: (saved) => {
          const f = formFromAgent(saved);
          setForm(f);
          setBaseline(f);
          toast(`${saved.name} saved`);
        },
        onError,
      });
    } else {
      createAgent.mutate(req, {
        onSuccess: (saved) => {
          toast(`${saved.name} created`);
          // Programmatic navigation isn't intercepted by the guard (it only
          // catches link clicks and unloads). replace: Back shouldn't return
          // to an empty "new" form.
          navigate(`/agents/${saved.id}`, { replace: true });
        },
        onError,
      });
    }
  };

  if (isEdit && isLoading) return <LoadingScreen message="Loading agent..." />;
  if (isEdit && loadError) {
    return (
      <div className="flex items-center gap-2 p-12 text-destructive">
        <AlertCircle className="w-6 h-6 shrink-0" />
        <span>Couldn't load agent #{agentId}: {apiErrorMessage(loadError)}</span>
      </div>
    );
  }

  const budgetNum = parseFloat(form.daily_budget_usd);
  const filteredStrategies = strategies.filter(
    (s) => !strategySearch || s.name.toLowerCase().includes(strategySearch.toLowerCase()) || String(s.id) === strategySearch.trim(),
  );
  const isDefault = agent?.is_default ?? false;

  return (
    <>
    <form onSubmit={handleSave} className="max-w-4xl mx-auto space-y-6 pb-20">
      {/* Header */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <Link to="/agents" className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1 mb-1">
            <ArrowLeft className="w-3.5 h-3.5" /> Agents
          </Link>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            {isEdit ? agent?.name || `Agent #${agentId}` : 'New Agent'}
            {isDefault && <DefaultAgentBadge />}
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {isEdit
              ? 'Changes to schedule, permissions and strategies apply from the agent\'s next run.'
              : 'Define a persona: what it should look for, which strategies it watches and when it runs.'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {isEdit && (
            <Link
              to={`/agents/${agentId}/runs`}
              className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs flex items-center gap-1.5 px-3 py-1.5"
            >
              <History className="w-3.5 h-3.5" /> Runs
            </Link>
          )}
          {!readOnly && <SaveButton saving={saving} dirty={dirty || !isEdit} />}
        </div>
      </div>

      {readOnly && (
        <div role="note" data-testid="agent-readonly-banner" className="p-3 rounded-lg border border-warning/40 bg-warning/10 text-xs text-foreground flex items-center gap-2">
          <AlertTriangle className="w-4 h-4 text-warning shrink-0" />
          <span>Read-only — only admins can change agents.</span>
        </div>
      )}

      {saveError && (
        <div role="alert" className="p-3 rounded-lg border border-destructive/40 bg-destructive/15 text-xs text-foreground flex items-start gap-2">
          <AlertCircle className="w-4 h-4 text-destructive shrink-0 mt-0.5" />
          <div>
            <div className="font-semibold text-destructive">Couldn't save</div>
            <div className="mt-0.5">{saveError}</div>
          </div>
        </div>
      )}

      {/* A disabled fieldset disables every control below for non-admins. */}
      <fieldset disabled={readOnly} className="min-w-0 border-0 p-0 m-0 space-y-6">
      {/* 1. Identity */}
      <Card>
        <CardHeader title="Identity" subtitle="Who this agent is and what it's for" />
        <div className="space-y-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-name" className={LABEL}>
              Name
            </label>
            <input
              id="agent-name"
              type="text"
              required
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              disabled={isDefault}
              placeholder="e.g. Risk Monitor"
              className="form-input text-sm max-w-md"
            />
            {isDefault && <p className={HELP}>The default Copilot agent can't be renamed.</p>}
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-goal" className={LABEL}>
              Goal
            </label>
            <textarea
              id="agent-goal"
              rows={10}
              value={form.goal}
              onChange={(e) => set('goal', e.target.value)}
              placeholder={'e.g. Watch drawdown and win-rate drift on my bound strategies. Flag anything that departs from its last backtest by more than 20%, and write a report explaining why.'}
              className="form-textarea font-mono text-xs leading-relaxed"
              aria-describedby="agent-goal-help"
            />
            <p id="agent-goal-help" className={HELP}>
              The agent's standing instructions. They're added on top of the global house rules every agent follows
              (no order placement, never modifying live strategies, cite data by reference) - you don't need to repeat
              those here.
            </p>
          </div>
        </div>
      </Card>

      {/* 2. Model */}
      <Card>
        <CardHeader title="Model" subtitle="Which LLM answers for this agent" />
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-provider" className={LABEL}>
              Provider
            </label>
            <select
              id="agent-provider"
              value={form.provider}
              onChange={(e) => set('provider', e.target.value as AgentProvider)}
              className="form-select text-sm"
            >
              <option value="">Default</option>
              <option value="anthropic">Anthropic</option>
              <option value="gemini">Gemini</option>
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-model" className={LABEL}>
              Model
            </label>
            <input
              id="agent-model"
              type="text"
              value={form.model}
              onChange={(e) => set('model', e.target.value)}
              placeholder="provider default"
              className="form-input text-sm font-mono"
            />
          </div>
        </div>
      </Card>

      {/* 3. Permissions */}
      <Card>
        <CardHeader
          title="Permissions"
          subtitle="What this agent may do. Reading memory, writing journal notes and writing reports are always allowed."
        />
        <fieldset className="space-y-1">
          <legend className="sr-only">Permissions</legend>
          {AGENT_PERMISSIONS.map((p) => {
            const disabled = !!p.phase;
            const checked = form.permissions.includes(p.key);
            return (
              <label
                key={p.key}
                className={`flex items-start gap-3 p-2 rounded-md ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:bg-accent/40'}`}
              >
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={disabled}
                  onChange={() => set('permissions', toggle(form.permissions, p.key))}
                  className="mt-0.5 w-4 h-4 rounded border-border focus:ring-ring focus:ring-offset-background"
                />
                <span className="flex-1">
                  <span className="text-xs font-semibold text-foreground font-mono">{p.key}</span>
                  <span className="text-xs text-muted-foreground"> - {p.description}</span>
                  {p.phase && (
                    <span className="ml-2 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                      {p.phase}
                    </span>
                  )}
                </span>
              </label>
            );
          })}
        </fieldset>
      </Card>

      {/* 4. Strategies */}
      <Card>
        <CardHeader
          title="Strategies"
          subtitle={`${form.strategy_ids.length} selected - the strategies this agent evaluates on each run`}
        />
        <div className="p-3 mb-3 rounded-lg border border-border bg-secondary/40 text-[11px] text-muted-foreground flex items-start gap-2">
          <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5 text-warning" />
          <span>
            Agents never modify a live or productive strategy directly. They improve one through a dryrun{' '}
            <span className="font-mono">challenger</span> copy and a promotion proposal that you approve in{' '}
            <Link to="/agents/proposals" className="text-primary hover:underline">
              Proposals
            </Link>
            . Non-live strategies can be auto-deployed with <span className="font-mono">edit_testing</span>, but only when
            the deploy gate passes.
          </span>
        </div>
        {strategies.length === 0 ? (
          <p className="text-xs text-muted-foreground">No strategies exist yet.</p>
        ) : (
          <>
            {strategies.length > 8 && (
              <input
                type="search"
                value={strategySearch}
                onChange={(e) => setStrategySearch(e.target.value)}
                placeholder="Filter strategies..."
                aria-label="Filter strategies"
                className="form-input text-xs mb-2 max-w-xs"
              />
            )}
            <div className="max-h-72 overflow-y-auto rounded-lg border border-border divide-y divide-border">
              {filteredStrategies.map((s) => (
                <label key={s.id} className="flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-accent/40">
                  <input
                    type="checkbox"
                    checked={form.strategy_ids.includes(s.id)}
                    onChange={() => set('strategy_ids', toggle(form.strategy_ids, s.id))}
                    className="w-4 h-4 rounded border-border focus:ring-ring focus:ring-offset-background"
                  />
                  <span className="flex-1 min-w-0 truncate text-xs text-foreground">
                    <span className="font-mono text-muted-foreground">#{s.id}</span> {s.name}
                  </span>
                  <StatusBadge status={s.status} />
                  <ModeBadge mode={s.mode} />
                </label>
              ))}
            </div>
          </>
        )}
      </Card>

      {/* 5. Schedule */}
      <Card>
        <CardHeader title="Schedule" subtitle="Cron schedules (UTC) and event, market and chained triggers. With none, the agent only runs manually or in chat." />
        <div className="space-y-3">
          {form.cron.length === 0 ? (
            <p className="text-xs text-muted-foreground">No schedule - manual runs only.</p>
          ) : (
            <ul className="space-y-1.5">
              {form.cron.map((spec, i) => (
                <li key={`${spec}-${i}`} className="flex items-center gap-2">
                  <Clock className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                  <input
                    type="text"
                    value={spec}
                    aria-label={`Cron schedule ${i + 1}`}
                    onChange={(e) => set('cron', form.cron.map((c, j) => (j === i ? e.target.value : c)))}
                    className="form-input text-xs font-mono py-1.5 max-w-[12rem]"
                  />
                  <span className="text-xs text-foreground truncate">
                    {describeCron(spec)} <span className="text-muted-foreground">UTC</span>
                  </span>
                  <button
                    type="button"
                    onClick={() => set('cron', form.cron.filter((_, j) => j !== i))}
                    aria-label={`Remove schedule ${spec}`}
                    className="ml-auto p-1.5 rounded text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </li>
              ))}
            </ul>
          )}

          <div className="pt-3 border-t border-border flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted-foreground font-medium">Add:</span>
              {CRON_PRESETS.map((p) => (
                <button
                  key={p.spec}
                  type="button"
                  onClick={() => addCron(p.spec)}
                  disabled={form.cron.includes(p.spec)}
                  className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs px-2.5 py-1 disabled:opacity-40"
                  title={p.spec}
                >
                  {p.label}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-2">
              <input
                type="text"
                value={newCron}
                onChange={(e) => setNewCron(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    addCron(newCron);
                    setNewCron('');
                  }
                }}
                placeholder="*/30 * * * *"
                aria-label="Custom cron expression"
                className="form-input text-xs font-mono py-1.5 max-w-[12rem]"
              />
              <button
                type="button"
                onClick={() => {
                  addCron(newCron);
                  setNewCron('');
                }}
                disabled={!newCron.trim()}
                className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs px-2.5 py-1.5 flex items-center gap-1 disabled:opacity-40"
              >
                <Plus className="w-3.5 h-3.5" /> Add
              </button>
              {newCron.trim() && (
                <span className="text-xs text-muted-foreground truncate">
                  {describeCron(newCron)} UTC
                </span>
              )}
            </div>
            <p className={HELP}>
              Standard 5-field cron (minute hour day month weekday). Invalid expressions are rejected on save.
            </p>
          </div>

          <div className="pt-3 border-t border-border">
            <div className="text-xs font-semibold text-foreground">Triggers</div>
            <p className={HELP}>
              Besides the schedule, run this agent when a bound strategy emits an event, when a watched market moves,
              or after another agent finishes. Every trigger runs unattended under the same permissions, budget and
              kill switch.
            </p>
          </div>
          <AgentTriggersEditor
            ref={chainRef}
            draft={form.triggers}
            onChange={(next) => set('triggers', next)}
            errors={showTriggerErrors ? triggerErrors : null}
            chainError={chainError}
            currentAgentId={agentId}
            agents={allAgents}
            symbolSuggestions={symbolSuggestions}
          />
        </div>
      </Card>

      {/* 6. Notifications */}
      <Card>
        <CardHeader
          title="Notifications"
          subtitle="Where this agent's notify tool sends messages (needs the notify permission)"
          action={
            isAdmin ? (
              <Link to="/settings#agent-notifications" className="text-xs text-primary hover:underline inline-flex items-center gap-1">
                Manage targets <ExternalLink className="w-3 h-3" />
              </Link>
            ) : undefined
          }
        />
        {!isAdmin ? (
          <p className="text-xs text-muted-foreground">
            {form.webhook_target_ids.length} notification target{form.webhook_target_ids.length === 1 ? '' : 's'} selected.
          </p>
        ) : targets.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            No webhook targets yet -{' '}
            <Link to="/settings#agent-notifications" className="text-primary hover:underline">
              add one in Settings
            </Link>
            .
          </p>
        ) : (
          <div className="rounded-lg border border-border divide-y divide-border">
            {targets.map((t) => (
              <label key={t.id} className="flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-accent/40">
                <input
                  type="checkbox"
                  checked={form.webhook_target_ids.includes(t.id)}
                  onChange={() => set('webhook_target_ids', toggle(form.webhook_target_ids, t.id))}
                  className="w-4 h-4 rounded border-border focus:ring-ring focus:ring-offset-background"
                />
                <span className="flex-1 text-xs text-foreground">{t.name}</span>
                <span className="text-[10px] font-mono uppercase text-muted-foreground">{t.kind}</span>
                {!t.enabled && <span className="text-[10px] uppercase font-semibold text-muted-foreground">disabled</span>}
              </label>
            ))}
          </div>
        )}
        {form.webhook_target_ids.length > 0 && !form.permissions.includes('notify') && (
          <p className="mt-2 text-[11px] text-warning">
            Targets are selected but the <span className="font-mono">notify</span> permission is off - this agent won't
            send anything.
          </p>
        )}
      </Card>

      {/* 7. Limits */}
      <Card>
        <CardHeader title="Limits" subtitle="Spend and deploy guardrails - a run that would exceed the budget halts" />
        <div className="space-y-4">
          <div className="flex flex-col gap-1.5 max-w-xs">
            <label htmlFor="agent-budget" className={LABEL}>
              Daily budget (USD)
              <HelpTooltip>
                Model spend allowed per UTC day. Once today's cost reaches it, further runs are refused until midnight
                UTC. 0 means unlimited.
              </HelpTooltip>
            </label>
            <input
              id="agent-budget"
              type="number"
              min="0"
              step="0.01"
              value={form.daily_budget_usd}
              onChange={(e) => set('daily_budget_usd', e.target.value)}
              className="form-input text-sm"
            />
            {budgetNum === 0 && (
              <p className="text-[11px] text-warning flex items-center gap-1">
                <AlertTriangle className="w-3 h-3" /> Unlimited - a looping or chatty agent can run up real cost.
              </p>
            )}
          </div>

          <div className="flex flex-col gap-1.5 max-w-xs">
            <label htmlFor="agent-max-deploys" className={LABEL}>
              Max auto-deploys per day
              <HelpTooltip>
                How many times per UTC day this agent may push code to a non-live strategy on its own via
                deploy_to_testing. Each one must pass the deploy gate first; a change that fails becomes a proposal
                instead. Live strategies are never auto-deployed.
              </HelpTooltip>
            </label>
            <input
              id="agent-max-deploys"
              type="number"
              min="0"
              step="1"
              value={form.max_auto_deploys_per_day}
              onChange={(e) => {
                const n = Math.floor(Number(e.target.value));
                set('max_auto_deploys_per_day', Number.isFinite(n) && n > 0 ? n : 0);
              }}
              className="form-input text-sm"
              aria-describedby="agent-max-deploys-help"
            />
            <p id="agent-max-deploys-help" className={HELP}>
              {form.max_auto_deploys_per_day === 0
                ? 'Auto-deploy disabled - every change becomes a proposal for you to approve.'
                : 'Needs the edit_testing permission. 0 disables auto-deploy (proposals only).'}
            </p>
            {form.max_auto_deploys_per_day > 0 && !form.permissions.includes('edit_testing') && (
              <p className="text-[11px] text-warning">
                The <span className="font-mono">edit_testing</span> permission is off - this agent can't auto-deploy.
              </p>
            )}
          </div>

          <div className="flex flex-col gap-1.5 max-w-xs">
            <label htmlFor="agent-max-iterations" className={LABEL}>
              Max tool iterations
              <HelpTooltip>
                How many model-to-tool round trips one run may take. When a run reaches the cap, the model gets one
                last turn with tools disabled to give its final answer, and the run is marked "hit iteration cap".
                Leave blank to use the default: 24 for scheduled and triggered runs, 8 for chat.
              </HelpTooltip>
            </label>
            <input
              id="agent-max-iterations"
              type="text"
              inputMode="numeric"
              placeholder="Default (24 scheduled / 8 chat)"
              value={form.max_iterations}
              onChange={(e) => set('max_iterations', e.target.value)}
              className="form-input text-sm"
              aria-invalid={parseMaxIterations(form.max_iterations) === null}
              aria-describedby="agent-max-iterations-help"
            />
            <p
              id="agent-max-iterations-help"
              className={parseMaxIterations(form.max_iterations) === null ? 'text-[11px] text-destructive' : HELP}
            >
              {parseMaxIterations(form.max_iterations) === null
                ? `Enter a whole number from ${MIN_ITERATIONS} to ${MAX_ITERATIONS}, or leave blank for the default.`
                : `Blank or 0 = default (24 scheduled/triggered, 8 chat). Allowed: ${MIN_ITERATIONS}-${MAX_ITERATIONS}.`}
            </p>
          </div>

          {isEdit && <UsagePanel agentId={agentId} budget={budgetNum > 0 ? budgetNum : 0} />}
        </div>
      </Card>

      </fieldset>

      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={() => guard.requestNavigate('/agents')}
          className="bg-secondary hover:bg-accent text-foreground rounded border border-border px-4 py-2 text-sm"
        >
          {dirty ? 'Cancel' : 'Back'}
        </button>
        {!readOnly && <SaveButton saving={saving} dirty={dirty || !isEdit} large />}
      </div>
    </form>

      {/* Outside the <form>: ConfirmDialog's buttons have no type, so inside
          it they'd default to submit. */}
      <ConfirmDialog
        isOpen={guard.pendingPath != null}
        title="Discard unsaved changes?"
        message="You have unsaved changes to this agent. Leave the page and discard them?"
        confirmText="Discard changes"
        cancelText="Keep editing"
        isDangerous
        onConfirm={guard.confirmLeave}
        onCancel={guard.cancelLeave}
      />
    </>
  );
}

function SaveButton({ saving, dirty, large }: { saving: boolean; dirty: boolean; large?: boolean }) {
  return (
    <button
      type="submit"
      disabled={saving || !dirty}
      className={`bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary font-bold flex items-center gap-1.5 disabled:opacity-50 ${
        large ? 'px-4 py-2 text-sm' : 'px-3 py-1.5 text-xs'
      }`}
    >
      {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
      <span>{saving ? 'Saving...' : 'Save agent'}</span>
    </button>
  );
}

function UsagePanel({ agentId, budget }: { agentId: number; budget: number }) {
  const { data: usage, isLoading, error } = useAgentUsage(agentId, 30);

  if (isLoading) {
    return <div className="text-xs text-muted-foreground flex items-center gap-2"><Loader2 className="w-3.5 h-3.5 animate-spin" /> Loading usage...</div>;
  }
  if (error || !usage) {
    return <div className="text-xs text-muted-foreground">Usage unavailable{error ? `: ${apiErrorMessage(error)}` : ''}.</div>;
  }

  const activeDays = [...usage.days].filter((d) => d.runs > 0).sort((a, b) => b.day.localeCompare(a.day));

  return (
    <div className="pt-3 border-t border-border space-y-3">
      <div className="flex items-baseline gap-4 text-xs">
        <span className="text-muted-foreground">
          Today: <span className="font-mono text-foreground">{formatUsd(usage.today.cost_usd)}</span>
          {budget > 0 && <span className="font-mono text-muted-foreground"> / {formatUsd(budget)}</span>}
        </span>
        <span className="text-muted-foreground">
          <span className="font-mono text-foreground">{usage.today.runs}</span> run{usage.today.runs === 1 ? '' : 's'}
        </span>
        <span className="text-muted-foreground">
          <span className="font-mono text-foreground">
            {formatTokens(usage.today.input_tokens)} / {formatTokens(usage.today.output_tokens)}
          </span>{' '}
          tokens in / out
        </span>
      </div>
      <UsageSparkline days={usage.days} budget={budget} />
      {activeDays.length > 0 && (
        <div className="max-h-48 overflow-y-auto rounded border border-border/60">
          <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-1.5 [&_th]:text-left [&_th]:text-[10px] [&_th]:uppercase [&_th]:font-semibold [&_th]:text-muted-foreground [&_td]:px-3 [&_td]:py-1.5 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40">
            <thead className="bg-secondary/40 sticky top-0">
              <tr>
                <th>Day (UTC)</th>
                <th className="text-right">Runs</th>
                <th className="text-right">Tokens in / out</th>
                <th className="text-right">Cost</th>
              </tr>
            </thead>
            <tbody className="font-mono">
              {activeDays.map((d) => (
                <tr key={d.day}>
                  <td className="text-muted-foreground">{d.day}</td>
                  <td className="text-right text-foreground">{d.runs}</td>
                  <td className="text-right text-muted-foreground">
                    {formatTokens(d.input_tokens)} / {formatTokens(d.output_tokens)}
                  </td>
                  <td className={`text-right ${budget > 0 && d.cost_usd >= budget ? 'text-destructive' : 'text-foreground'}`}>
                    {formatUsd(d.cost_usd)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
