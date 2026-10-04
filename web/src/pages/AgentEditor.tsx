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
import { describeCron } from '@/lib/cron';
import { CronScheduleBuilder } from '@/components/domain/CronScheduleBuilder';
import { formatTokens, formatUsd } from '@/lib/format';
import { normalizeTriggers } from '@/lib/triggers';
import { formatUtcDay } from '@/lib/format';
import { useT } from '@/i18n';

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
  const t = useT();
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
      setSaveError(t('agentEditor.errBudget'));
      return;
    }
    if (parseMaxIterations(form.max_iterations) === null) {
      setSaveError(
        t('agentEditor.errIterations', { min: MIN_ITERATIONS, max: MAX_ITERATIONS }),
      );
      window.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }
    if (triggerErrors.count > 0) {
      setSaveError(
        t('agentEditor.errTriggers', { count: triggerErrors.count }),
      );
      window.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }
    const req = toRequest(form);
    const onError = (err: unknown) => {
      // 400 (invalid cron / permission / budget / trigger, chain cycle) and
      // 409 (duplicate name) come back as {"error","message"} - shown
      // inline, form state kept.
      const msg = apiErrorMessage(err, t('agentEditor.saveFailed'));
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
          toast(t('agentEditor.saved', { name: saved.name }));
        },
        onError,
      });
    } else {
      createAgent.mutate(req, {
        onSuccess: (saved) => {
          toast(t('agentEditor.created', { name: saved.name }));
          // Programmatic navigation isn't intercepted by the guard (it only
          // catches link clicks and unloads). replace: Back shouldn't return
          // to an empty "new" form.
          navigate(`/agents/${saved.id}`, { replace: true });
        },
        onError,
      });
    }
  };

  if (isEdit && isLoading) return <LoadingScreen message={t('agentEditor.loading')} />;
  if (isEdit && loadError) {
    return (
      <div className="flex items-center gap-2 p-12 text-destructive">
        <AlertCircle className="w-6 h-6 shrink-0" />
        <span>{t('agentEditor.loadFailed', { id: agentId, error: apiErrorMessage(loadError) })}</span>
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
            <ArrowLeft className="w-3.5 h-3.5" /> {t('agentEditor.back')}
          </Link>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            {isEdit ? agent?.name || t('agents.agentNumber', { id: agentId }) : t('agentEditor.newAgent')}
            {isDefault && <DefaultAgentBadge />}
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {isEdit
              ? t('agentEditor.editSubtitle')
              : t('agentEditor.newSubtitle')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {isEdit && (
            <Link
              to={`/agents/${agentId}/runs`}
              className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs flex items-center gap-1.5 px-3 py-1.5"
            >
              <History className="w-3.5 h-3.5" /> {t('agentEditor.runs')}
            </Link>
          )}
          {!readOnly && <SaveButton saving={saving} dirty={dirty || !isEdit} />}
        </div>
      </div>

      {readOnly && (
        <div role="note" data-testid="agent-readonly-banner" className="p-3 rounded-lg border border-warning/40 bg-warning/10 text-xs text-foreground flex items-center gap-2">
          <AlertTriangle className="w-4 h-4 text-warning shrink-0" />
          <span>{t('agentEditor.readOnly')}</span>
        </div>
      )}

      {saveError && (
        <div role="alert" className="p-3 rounded-lg border border-destructive/40 bg-destructive/15 text-xs text-foreground flex items-start gap-2">
          <AlertCircle className="w-4 h-4 text-destructive shrink-0 mt-0.5" />
          <div>
            <div className="font-semibold text-destructive">{t('agentEditor.couldntSave')}</div>
            <div className="mt-0.5">{saveError}</div>
          </div>
        </div>
      )}

      {/* A disabled fieldset disables every control below for non-admins. */}
      <fieldset disabled={readOnly} className="min-w-0 border-0 p-0 m-0 space-y-6">
      {/* 1. Identity */}
      <Card>
        <CardHeader title={t('agentEditor.identity')} subtitle={t('agentEditor.identitySubtitle')} />
        <div className="space-y-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-name" className={LABEL}>
              {t('agentEditor.name')}
            </label>
            <input
              id="agent-name"
              type="text"
              required
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              disabled={isDefault}
              placeholder={t('agentEditor.namePlaceholder')}
              className="form-input text-sm max-w-md"
            />
            {isDefault && <p className={HELP}>{t('agentEditor.defaultNoRename')}</p>}
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-goal" className={LABEL}>
              {t('agentEditor.goal')}
            </label>
            <textarea
              id="agent-goal"
              rows={10}
              value={form.goal}
              onChange={(e) => set('goal', e.target.value)}
              placeholder={t('agentEditor.goalPlaceholder')}
              className="form-textarea font-mono text-xs leading-relaxed"
              aria-describedby="agent-goal-help"
            />
            <p id="agent-goal-help" className={HELP}>
              {t('agentEditor.goalHelp')}
            </p>
          </div>
        </div>
      </Card>

      {/* 2. Model */}
      <Card>
        <CardHeader title={t('agentEditor.model')} subtitle={t('agentEditor.modelSubtitle')} />
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-provider" className={LABEL}>
              {t('agentEditor.provider')}
            </label>
            <select
              id="agent-provider"
              value={form.provider}
              onChange={(e) => set('provider', e.target.value as AgentProvider)}
              className="form-select text-sm"
            >
              <option value="">{t('agentEditor.providerDefault')}</option>
              <option value="anthropic">Anthropic</option>
              <option value="gemini">Gemini</option>
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="agent-model" className={LABEL}>
              {t('agentEditor.model')}
            </label>
            <input
              id="agent-model"
              type="text"
              value={form.model}
              onChange={(e) => set('model', e.target.value)}
              placeholder={t('agentEditor.modelPlaceholder')}
              className="form-input text-sm font-mono"
            />
          </div>
        </div>
      </Card>

      {/* 3. Permissions */}
      <Card>
        <CardHeader
          title={t('agentEditor.permissions')}
          subtitle={t('agentEditor.permissionsSubtitle')}
        />
        <fieldset className="space-y-1">
          <legend className="sr-only">{t('agentEditor.permissions')}</legend>
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
          title={t('agentEditor.strategies')}
          subtitle={t('agentEditor.strategiesSubtitle', { count: form.strategy_ids.length })}
        />
        <div className="p-3 mb-3 rounded-lg border border-border bg-secondary/40 text-[11px] text-muted-foreground flex items-start gap-2">
          <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5 text-warning" />
          <span>
            {t.rich('agentEditor.liveNotice', {
              challenger: <span className="font-mono">challenger</span>,
              proposals: (
                <Link to="/agents/proposals" className="text-primary hover:underline">
                  {t('agentEditor.proposalsLink')}
                </Link>
              ),
              editTesting: <span className="font-mono">edit_testing</span>,
            })}
          </span>
        </div>
        {strategies.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('agentEditor.noStrategies')}</p>
        ) : (
          <>
            {strategies.length > 8 && (
              <input
                type="search"
                value={strategySearch}
                onChange={(e) => setStrategySearch(e.target.value)}
                placeholder={t('agentEditor.filterStrategies')}
                aria-label={t('agentEditor.filterStrategiesAria')}
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
        <CardHeader title={t('agentEditor.schedule')} subtitle={t('agentEditor.scheduleSubtitle')} />
        <div className="space-y-3">
          {form.cron.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t('agentEditor.noSchedule')}</p>
          ) : (
            <ul className="space-y-1.5">
              {form.cron.map((spec, i) => (
                <li key={`${spec}-${i}`} className="flex items-center gap-2">
                  <Clock className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                  <input
                    type="text"
                    value={spec}
                    aria-label={t('agentEditor.cronAria', { n: i + 1 })}
                    onChange={(e) => set('cron', form.cron.map((c, j) => (j === i ? e.target.value : c)))}
                    className="form-input text-xs font-mono py-1.5 max-w-[12rem]"
                  />
                  <span className="text-xs text-foreground truncate">
                    {describeCron(spec)} <span className="text-muted-foreground">UTC</span>
                  </span>
                  <button
                    type="button"
                    onClick={() => set('cron', form.cron.filter((_, j) => j !== i))}
                    aria-label={t('agentEditor.removeSchedule', { spec })}
                    className="ml-auto p-1.5 rounded text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </li>
              ))}
            </ul>
          )}

          <div className="pt-3 border-t border-border flex flex-col gap-2">
            <CronScheduleBuilder existingSchedules={form.cron} onAddSchedule={addCron} />
            <p className={HELP}>
              {t('agentEditor.cronHelp')}
            </p>
          </div>

          <div className="pt-3 border-t border-border">
            <div className="text-xs font-semibold text-foreground">{t('agentEditor.triggers')}</div>
            <p className={HELP}>
              {t('agentEditor.triggersHelp')}
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
          title={t('agentEditor.notifications')}
          subtitle={t('agentEditor.notificationsSubtitle')}
          action={
            isAdmin ? (
              <Link to="/settings#agent-notifications" className="text-xs text-primary hover:underline inline-flex items-center gap-1">
                {t('agentEditor.manageTargets')} <ExternalLink className="w-3 h-3" />
              </Link>
            ) : undefined
          }
        />
        {!isAdmin ? (
          <p className="text-xs text-muted-foreground">
            {t('agentEditor.targetsSelected', { count: form.webhook_target_ids.length })}
          </p>
        ) : targets.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t.rich('agentEditor.noTargets', {
              link: (
                <Link to="/settings#agent-notifications" className="text-primary hover:underline">
                  {t('agentEditor.addInSettings')}
                </Link>
              ),
            })}
          </p>
        ) : (
          <div className="rounded-lg border border-border divide-y divide-border">
            {targets.map((wt) => (
              <label key={wt.id} className="flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-accent/40">
                <input
                  type="checkbox"
                  checked={form.webhook_target_ids.includes(wt.id)}
                  onChange={() => set('webhook_target_ids', toggle(form.webhook_target_ids, wt.id))}
                  className="w-4 h-4 rounded border-border focus:ring-ring focus:ring-offset-background"
                />
                <span className="flex-1 text-xs text-foreground">{wt.name}</span>
                <span className="text-[10px] font-mono uppercase text-muted-foreground">{wt.kind}</span>
                {!wt.enabled && <span className="text-[10px] uppercase font-semibold text-muted-foreground">{t('agentEditor.targetDisabled')}</span>}
              </label>
            ))}
          </div>
        )}
        {form.webhook_target_ids.length > 0 && !form.permissions.includes('notify') && (
          <p className="mt-2 text-[11px] text-warning">
            {t.rich('agentEditor.notifyOff', { notify: <span className="font-mono">notify</span> })}
          </p>
        )}
      </Card>

      {/* 7. Limits */}
      <Card>
        <CardHeader title={t('agentEditor.limits')} subtitle={t('agentEditor.limitsSubtitle')} />
        <div className="space-y-4">
          <div className="flex flex-col gap-1.5 max-w-xs">
            <label htmlFor="agent-budget" className={LABEL}>
              {t('agentEditor.dailyBudget')}
              <HelpTooltip>{t('agentEditor.dailyBudgetHelp')}</HelpTooltip>
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
                <AlertTriangle className="w-3 h-3" /> {t('agentEditor.unlimitedWarning')}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-1.5 max-w-xs">
            <label htmlFor="agent-max-deploys" className={LABEL}>
              {t('agentEditor.maxDeploys')}
              <HelpTooltip>{t('agentEditor.maxDeploysHelp')}</HelpTooltip>
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
                ? t('agentEditor.autoDeployOff')
                : t('agentEditor.autoDeployNeeds')}
            </p>
            {form.max_auto_deploys_per_day > 0 && !form.permissions.includes('edit_testing') && (
              <p className="text-[11px] text-warning">
                {t.rich('agentEditor.editTestingOff', { perm: <span className="font-mono">edit_testing</span> })}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-1.5 max-w-xs">
            <label htmlFor="agent-max-iterations" className={LABEL}>
              {t('agentEditor.maxIterations')}
              <HelpTooltip>{t('agentEditor.maxIterationsHelp')}</HelpTooltip>
            </label>
            <input
              id="agent-max-iterations"
              type="text"
              inputMode="numeric"
              placeholder={t('agentEditor.maxIterationsPlaceholder')}
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
                ? t('agentEditor.iterationsInvalid', { min: MIN_ITERATIONS, max: MAX_ITERATIONS })
                : t('agentEditor.iterationsHelp', { min: MIN_ITERATIONS, max: MAX_ITERATIONS })}
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
          {dirty ? t('common.cancel') : t('common.back')}
        </button>
        {!readOnly && <SaveButton saving={saving} dirty={dirty || !isEdit} large />}
      </div>
    </form>

      {/* Outside the <form>: ConfirmDialog's buttons have no type, so inside
          it they'd default to submit. */}
      <ConfirmDialog
        isOpen={guard.pendingPath != null}
        title={t('agentEditor.discardTitle')}
        message={t('agentEditor.discardMessage')}
        confirmText={t('agentEditor.discardConfirm')}
        cancelText={t('agentEditor.keepEditing')}
        isDangerous
        onConfirm={guard.confirmLeave}
        onCancel={guard.cancelLeave}
      />
    </>
  );
}

function SaveButton({ saving, dirty, large }: { saving: boolean; dirty: boolean; large?: boolean }) {
  const t = useT();
  return (
    <button
      type="submit"
      disabled={saving || !dirty}
      className={`bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary font-bold flex items-center gap-1.5 disabled:opacity-50 ${
        large ? 'px-4 py-2 text-sm' : 'px-3 py-1.5 text-xs'
      }`}
    >
      {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
      <span>{saving ? t('common.saving') : t('agentEditor.saveAgent')}</span>
    </button>
  );
}

function UsagePanel({ agentId, budget }: { agentId: number; budget: number }) {
  const t = useT();
  const { data: usage, isLoading, error } = useAgentUsage(agentId, 30);

  if (isLoading) {
    return <div className="text-xs text-muted-foreground flex items-center gap-2"><Loader2 className="w-3.5 h-3.5 animate-spin" /> {t('agentEditor.loadingUsage')}</div>;
  }
  if (error || !usage) {
    return <div className="text-xs text-muted-foreground">{error ? t('agentEditor.usageUnavailableErr', { error: apiErrorMessage(error) }) : t('agentEditor.usageUnavailable')}</div>;
  }

  const activeDays = [...usage.days].filter((d) => d.runs > 0).sort((a, b) => b.day.localeCompare(a.day));

  return (
    <div className="pt-3 border-t border-border space-y-3">
      <div className="flex items-baseline gap-4 text-xs">
        <span className="text-muted-foreground">
          {t.rich('agentEditor.today', { cost: <span className="font-mono text-foreground">{formatUsd(usage.today.cost_usd)}</span> })}
          {budget > 0 && <span className="font-mono text-muted-foreground"> / {formatUsd(budget)}</span>}
        </span>
        <span className="text-muted-foreground">
          {t('agentEditor.todayRuns', { count: usage.today.runs })}
        </span>
        <span className="text-muted-foreground">
          {t.rich('agentEditor.tokensInOut', {
            tokens: (
              <span className="font-mono text-foreground">
                {formatTokens(usage.today.input_tokens)} / {formatTokens(usage.today.output_tokens)}
              </span>
            ),
          })}
        </span>
      </div>
      <UsageSparkline days={usage.days} budget={budget} />
      {activeDays.length > 0 && (
        <div className="max-h-48 overflow-y-auto rounded border border-border/60">
          <table className="w-full text-xs border-collapse [&_th]:px-3 [&_th]:py-1.5 [&_th]:text-left [&_th]:text-[10px] [&_th]:uppercase [&_th]:font-semibold [&_th]:text-muted-foreground [&_td]:px-3 [&_td]:py-1.5 [&_tbody_tr]:border-t [&_tbody_tr]:border-border/40">
            <thead className="bg-secondary/40 sticky top-0">
              <tr>
                <th>{t('agentEditor.colDay')}</th>
                <th className="text-right">{t('agentEditor.colRuns')}</th>
                <th className="text-right">{t('agentEditor.colTokens')}</th>
                <th className="text-right">{t('agentEditor.colCost')}</th>
              </tr>
            </thead>
            <tbody className="font-mono">
              {activeDays.map((d) => (
                <tr key={d.day}>
                  <td className="text-muted-foreground">{formatUtcDay(d.day)}</td>
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
