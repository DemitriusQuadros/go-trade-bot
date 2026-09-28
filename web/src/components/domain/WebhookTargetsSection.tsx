import React, { useEffect, useRef, useState } from 'react';
import { Edit3, Loader2, Plus, Send, Trash2, X, Key, RotateCcw } from 'lucide-react';
import { WebhookTarget, WebhookTargetKind, WebhookTargetRequest } from '@/api/types';
import { apiErrorMessage } from '@/api/client';
import {
  useDeleteWebhookTarget,
  useSaveWebhookTarget,
  useTestWebhookTarget,
  useWebhookTargets,
} from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { DropdownMenu } from '@/components/ui/DropdownMenu';
import { useToast } from '@/context/ToastContext';

const KINDS: { value: WebhookTargetKind; label: string; urlHint: string }[] = [
  { value: 'discord', label: 'Discord', urlHint: 'https://discord.com/api/webhooks/...' },
  { value: 'slack', label: 'Slack', urlHint: 'https://hooks.slack.com/services/...' },
  { value: 'telegram', label: 'Telegram', urlHint: '' },
  { value: 'generic', label: 'Generic JSON', urlHint: 'https://example.com/hooks/agents' },
];

const TH = 'px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground';

// "Agent notifications" section on /settings (A-03 §7): the webhook targets
// agents can notify through. url and secret come back MASKED from the API;
// they are only ever displayed masked, and an untouched masked value sent
// back on save means "keep the stored one" (A-02 §5).
export function WebhookTargetsSection() {
  const { data: targets = [], isLoading, error } = useWebhookTargets();
  const deleteTarget = useDeleteWebhookTarget();
  const testTarget = useTestWebhookTarget();
  const { toast } = useToast();

  const [editing, setEditing] = useState<WebhookTarget | 'new' | null>(null);
  const [deleting, setDeleting] = useState<WebhookTarget | null>(null);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const runTest = (t: WebhookTarget) => {
    setTestingId(t.id);
    testTarget.mutate(t.id, {
      onSuccess: (res) => {
        if (res?.ok) toast(`Test message sent to ${t.name}`);
        else toast(`Test to ${t.name} failed: ${res?.error || 'unknown error'}`, 'error');
      },
      onError: (err) => toast(`Test to ${t.name} failed: ${apiErrorMessage(err)}`, 'error'),
      onSettled: () => setTestingId(null),
    });
  };

  const confirmDelete = () => {
    const t = deleting;
    setDeleting(null);
    if (!t) return;
    setActionError(null);
    deleteTarget.mutate(t.id, {
      onSuccess: () => toast(`${t.name} deleted`),
      // 409 when agents still reference it - the message names them.
      onError: (err) => setActionError(apiErrorMessage(err, `Couldn't delete ${t.name}`)),
    });
  };

  return (
    <Card id="agent-notifications" className="scroll-mt-20">
      <CardHeader
        title="Agent Notifications"
        subtitle="Webhook targets agents can notify - messages carry a link to the report, never an action"
        action={
          <button
            type="button"
            onClick={() => setEditing('new')}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
          >
            <Plus className="w-3.5 h-3.5" /> Add target
          </button>
        }
      />

      {actionError && (
        <div role="alert" className="mb-3 p-3 rounded-lg border text-xs flex items-center justify-between bg-destructive/15 border-destructive/40 text-foreground">
          <span>{actionError}</span>
          <button onClick={() => setActionError(null)} aria-label="Dismiss" className="p-0.5 text-muted-foreground hover:text-foreground">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {isLoading ? (
        <div className="p-6 text-center text-xs text-muted-foreground">Loading targets...</div>
      ) : error ? (
        <div className="p-6 text-center text-xs text-destructive">Couldn't load webhook targets: {apiErrorMessage(error)}</div>
      ) : targets.length === 0 ? (
        <div className="p-6 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg">
          No targets yet. Add a Discord, Slack, Telegram or generic webhook, then select it on an agent.
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full border-collapse">
            <thead>
              <tr className="border-b border-border bg-secondary/40">
                <th className={TH}>Name</th>
                <th className={TH}>Kind</th>
                <th className={TH}>Destination</th>
                <th className={TH}>Enabled</th>
                <th className="w-24 px-2 py-2.5"></th>
              </tr>
            </thead>
            <tbody>
              {targets.map((t) => (
                <tr key={t.id} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2.5 text-xs font-semibold text-foreground">{t.name}</td>
                  <td className="px-4 py-2.5 text-[11px] font-mono uppercase text-muted-foreground">{t.kind}</td>
                  <td className="px-4 py-2.5 text-xs font-mono text-muted-foreground max-w-xs truncate">
                    {t.kind === 'telegram' ? `chat ${t.chat_id || '—'} · token ${t.secret || '—'}` : t.url || '—'}
                  </td>
                  <td className="px-4 py-2.5">
                    <span
                      className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border ${
                        t.enabled ? 'bg-success/15 text-success border-success/40' : 'bg-secondary text-muted-foreground border-border'
                      }`}
                    >
                      {t.enabled ? 'on' : 'off'}
                    </span>
                  </td>
                  <td className="px-2 py-2.5 text-right whitespace-nowrap">
                    <button
                      type="button"
                      onClick={() => runTest(t)}
                      disabled={testingId === t.id}
                      className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-[11px] px-2 py-1 inline-flex items-center gap-1 mr-1 disabled:opacity-50"
                      title="Send an info test message"
                    >
                      {testingId === t.id ? <Loader2 className="w-3 h-3 animate-spin" /> : <Send className="w-3 h-3" />}
                      Send test
                    </button>
                    <DropdownMenu
                      label={`Actions for ${t.name}`}
                      items={[
                        { label: 'Edit', icon: <Edit3 />, onClick: () => setEditing(t) },
                        { label: 'Delete', icon: <Trash2 />, onClick: () => setDeleting(t), destructive: true, separatorBefore: true },
                      ]}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing && <WebhookTargetDialog target={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />}

      <ConfirmDialog
        isOpen={deleting != null}
        title="Delete webhook target"
        message={`Delete "${deleting?.name ?? ''}"? Agents that still notify through it must be edited first.`}
        confirmText="Delete target"
        isDangerous
        onConfirm={confirmDelete}
        onCancel={() => setDeleting(null)}
      />
    </Card>
  );
}

function WebhookTargetDialog({ target, onClose }: { target: WebhookTarget | null; onClose: () => void }) {
  const isEdit = target != null;
  const save = useSaveWebhookTarget();
  const { toast } = useToast();
  const nameRef = useRef<HTMLInputElement>(null);

  const [name, setName] = useState(target?.name ?? '');
  const [kind, setKind] = useState<WebhookTargetKind>(target?.kind ?? 'discord');
  const [chatId, setChatId] = useState(target?.chat_id ?? '');
  const [enabled, setEnabled] = useState(target?.enabled ?? true);
  // null = untouched: the masked value from the server is sent back as-is,
  // which the API treats as "keep the stored secret".
  const [newUrl, setNewUrl] = useState<string | null>(isEdit ? null : '');
  const [newSecret, setNewSecret] = useState<string | null>(isEdit ? null : '');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    nameRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const isTelegram = kind === 'telegram';

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!name.trim()) {
      setError('Name is required.');
      return;
    }
    const url = newUrl ?? target?.url ?? '';
    const secret = newSecret ?? target?.secret ?? '';
    if (!isTelegram && !url.trim()) {
      setError('Webhook URL is required.');
      return;
    }
    if (isTelegram && (!secret.trim() || !chatId.trim())) {
      setError('Telegram needs both a bot token and a chat ID.');
      return;
    }
    const req: WebhookTargetRequest = {
      name: name.trim(),
      kind,
      url: isTelegram ? '' : url.trim(),
      secret: isTelegram ? secret.trim() : '',
      chat_id: isTelegram ? chatId.trim() : '',
      enabled,
    };
    save.mutate(
      { id: target?.id, req },
      {
        onSuccess: (saved) => {
          toast(`${saved?.name ?? req.name} saved`);
          onClose();
        },
        onError: (err) => setError(apiErrorMessage(err, 'Failed to save the target')),
      },
    );
  };

  const kindInfo = KINDS.find((k) => k.value === kind)!;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="webhook-dialog-title"
    >
      <form onSubmit={submit} className="w-full max-w-md rounded-lg border border-border bg-card p-5 shadow-xl space-y-4">
        <div className="flex items-start justify-between">
          <h3 id="webhook-dialog-title" className="text-base font-semibold text-foreground">
            {isEdit ? `Edit ${target!.name}` : 'Add webhook target'}
          </h3>
          <button type="button" onClick={onClose} aria-label="Close" className="text-muted-foreground hover:text-foreground p-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="wt-name" className="text-xs font-semibold text-foreground">Name</label>
          <input id="wt-name" ref={nameRef} value={name} onChange={(e) => setName(e.target.value)} className="form-input text-sm" placeholder="e.g. Ops Discord" />
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="wt-kind" className="text-xs font-semibold text-foreground">Kind</label>
          <select id="wt-kind" value={kind} onChange={(e) => setKind(e.target.value as WebhookTargetKind)} className="form-select text-sm">
            {KINDS.map((k) => (
              <option key={k.value} value={k.value}>{k.label}</option>
            ))}
          </select>
        </div>

        {!isTelegram && (
          <SecretInput
            id="wt-url"
            label="Webhook URL"
            masked={isEdit && target!.kind !== 'telegram' ? target!.url : ''}
            value={newUrl}
            onChange={setNewUrl}
            placeholder={kindInfo.urlHint}
            inputType="url"
          />
        )}

        {isTelegram && (
          <>
            <SecretInput
              id="wt-secret"
              label="Bot token"
              masked={isEdit && target!.kind === 'telegram' ? target!.secret : ''}
              value={newSecret}
              onChange={setNewSecret}
              placeholder="123456:ABC-DEF..."
              inputType="password"
            />
            <div className="flex flex-col gap-1.5">
              <label htmlFor="wt-chat" className="text-xs font-semibold text-foreground">Chat ID</label>
              <input id="wt-chat" value={chatId} onChange={(e) => setChatId(e.target.value)} className="form-input text-sm font-mono" placeholder="-1001234567890" />
            </div>
          </>
        )}

        <label className="flex items-center gap-2.5 cursor-pointer">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            className="w-4 h-4 rounded border-border focus:ring-ring focus:ring-offset-background"
          />
          <span className="text-xs font-semibold text-foreground">Enabled</span>
        </label>

        {error && (
          <div role="alert" className="text-xs text-destructive bg-destructive/15 border border-destructive/40 rounded px-3 py-2">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-3 pt-1">
          <button type="button" onClick={onClose} className="bg-secondary hover:bg-accent text-foreground rounded border border-border px-4 py-2 text-sm">
            Cancel
          </button>
          <button
            type="submit"
            disabled={save.isPending}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary px-4 py-2 text-sm font-semibold flex items-center gap-1.5 disabled:opacity-50"
          >
            {save.isPending && <Loader2 className="w-4 h-4 animate-spin" />}
            {isEdit ? 'Save' : 'Add target'}
          </button>
        </div>
      </form>
    </div>
  );
}

// A secret-bearing field that only ever DISPLAYS the server's masked value.
// value === null means "untouched - keep the stored secret"; clicking
// Change switches to a blank input for a replacement.
function SecretInput({
  id,
  label,
  masked,
  value,
  onChange,
  placeholder,
  inputType,
}: {
  id: string;
  label: string;
  masked: string;
  value: string | null;
  onChange: (v: string | null) => void;
  placeholder: string;
  inputType: 'url' | 'password';
}) {
  const hasStored = !!masked;
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-xs font-semibold text-foreground flex items-center gap-1.5">
        <Key className="w-3 h-3 text-muted-foreground" /> {label}
      </label>
      {value === null && hasStored ? (
        <div className="flex items-center justify-between gap-2 px-3 py-2 rounded-md border border-input bg-background/70">
          <span id={id} className="font-mono text-xs text-muted-foreground truncate">{masked}</span>
          <button
            type="button"
            onClick={() => onChange('')}
            className="px-2 py-1 rounded text-xs font-medium text-foreground hover:bg-accent/60 border border-border/60 flex items-center gap-1 shrink-0"
          >
            <Edit3 className="w-3 h-3" /> Change
          </button>
        </div>
      ) : (
        <div className="flex items-center gap-2">
          <input
            id={id}
            type={inputType}
            autoComplete="off"
            value={value ?? ''}
            onChange={(e) => onChange(e.target.value)}
            placeholder={placeholder}
            className="form-input text-sm font-mono"
          />
          {hasStored && (
            <button
              type="button"
              onClick={() => onChange(null)}
              title="Keep the stored value"
              aria-label={`Keep the stored ${label}`}
              className="p-2 rounded text-muted-foreground hover:text-foreground hover:bg-secondary shrink-0"
            >
              <RotateCcw className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
      )}
    </div>
  );
}
