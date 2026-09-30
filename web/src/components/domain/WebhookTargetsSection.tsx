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
import { useT } from '@/i18n';

// Format hint, not text.
const TELEGRAM_TOKEN_HINT = '123456:ABC-DEF...';

const KINDS: { value: WebhookTargetKind; label: string; urlHint: string }[] = [
  { value: 'discord', label: 'Discord', urlHint: 'https://discord.com/api/webhooks/...' },
  { value: 'slack', label: 'Slack', urlHint: 'https://hooks.slack.com/services/...' },
  { value: 'telegram', label: 'Telegram', urlHint: '' },
  { value: 'generic', label: '',  urlHint: 'https://example.com/hooks/agents' },
];

const TH = 'px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground';

// "Agent notifications" section on /settings (A-03 §7): the webhook targets
// agents can notify through. url and secret come back MASKED from the API;
// they are only ever displayed masked, and an untouched masked value sent
// back on save means "keep the stored one" (A-02 §5).
export function WebhookTargetsSection() {
  const t = useT();
  const { data: targets = [], isLoading, error } = useWebhookTargets();
  const deleteTarget = useDeleteWebhookTarget();
  const testTarget = useTestWebhookTarget();
  const { toast } = useToast();

  const [editing, setEditing] = useState<WebhookTarget | 'new' | null>(null);
  const [deleting, setDeleting] = useState<WebhookTarget | null>(null);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const runTest = (wt: WebhookTarget) => {
    setTestingId(wt.id);
    testTarget.mutate(wt.id, {
      onSuccess: (res) => {
        if (res?.ok) toast(t('webhooks.testSent', { name: wt.name }));
        else toast(t('webhooks.testFailed', { name: wt.name, error: res?.error || t('webhooks.unknownError') }), 'error');
      },
      onError: (err) => toast(t('webhooks.testFailed', { name: wt.name, error: apiErrorMessage(err) }), 'error'),
      onSettled: () => setTestingId(null),
    });
  };

  const confirmDelete = () => {
    const wt = deleting;
    setDeleting(null);
    if (!wt) return;
    setActionError(null);
    deleteTarget.mutate(wt.id, {
      onSuccess: () => toast(t('webhooks.deleted', { name: wt.name })),
      // 409 when agents still reference it - the message names them.
      onError: (err) => setActionError(apiErrorMessage(err, t('webhooks.deleteFailed', { name: wt.name }))),
    });
  };

  return (
    <Card id="agent-notifications" className="scroll-mt-20">
      <CardHeader
        title={t('webhooks.title')}
        subtitle={t('webhooks.subtitle')}
        action={
          <button
            type="button"
            onClick={() => setEditing('new')}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
          >
            <Plus className="w-3.5 h-3.5" /> {t('webhooks.addTarget')}
          </button>
        }
      />

      {actionError && (
        <div role="alert" className="mb-3 p-3 rounded-lg border text-xs flex items-center justify-between bg-destructive/15 border-destructive/40 text-foreground">
          <span>{actionError}</span>
          <button onClick={() => setActionError(null)} aria-label={t('common.dismiss')} className="p-0.5 text-muted-foreground hover:text-foreground">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {isLoading ? (
        <div className="p-6 text-center text-xs text-muted-foreground">{t('webhooks.loading')}</div>
      ) : error ? (
        <div className="p-6 text-center text-xs text-destructive">{t('webhooks.loadFailed', { error: apiErrorMessage(error) })}</div>
      ) : targets.length === 0 ? (
        <div className="p-6 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg">
          {t('webhooks.empty')}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full border-collapse">
            <thead>
              <tr className="border-b border-border bg-secondary/40">
                <th className={TH}>{t('webhooks.colName')}</th>
                <th className={TH}>{t('webhooks.colKind')}</th>
                <th className={TH}>{t('webhooks.colDestination')}</th>
                <th className={TH}>{t('webhooks.colEnabled')}</th>
                <th className="w-24 px-2 py-2.5"></th>
              </tr>
            </thead>
            <tbody>
              {targets.map((wt) => (
                <tr key={wt.id} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2.5 text-xs font-semibold text-foreground">{wt.name}</td>
                  <td className="px-4 py-2.5 text-[11px] font-mono uppercase text-muted-foreground">{wt.kind}</td>
                  <td className="px-4 py-2.5 text-xs font-mono text-muted-foreground max-w-xs truncate">
                    {wt.kind === 'telegram' ? t('webhooks.telegramDest', { chat: wt.chat_id || '—', token: wt.secret || '—' }) : wt.url || '—'}
                  </td>
                  <td className="px-4 py-2.5">
                    <span
                      className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border ${
                        wt.enabled ? 'bg-success/15 text-success border-success/40' : 'bg-secondary text-muted-foreground border-border'
                      }`}
                    >
                      {wt.enabled ? t('webhooks.on') : t('webhooks.off')}
                    </span>
                  </td>
                  <td className="px-2 py-2.5 text-right whitespace-nowrap">
                    <button
                      type="button"
                      onClick={() => runTest(wt)}
                      disabled={testingId === wt.id}
                      className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-[11px] px-2 py-1 inline-flex items-center gap-1 mr-1 disabled:opacity-50"
                      title={t('webhooks.testTitle')}
                    >
                      {testingId === wt.id ? <Loader2 className="w-3 h-3 animate-spin" /> : <Send className="w-3 h-3" />}
                      {t('webhooks.sendTest')}
                    </button>
                    <DropdownMenu
                      label={t('strategies.actionsFor', { name: wt.name })}
                      items={[
                        { label: t('common.edit'), icon: <Edit3 />, onClick: () => setEditing(wt) },
                        { label: t('common.delete'), icon: <Trash2 />, onClick: () => setDeleting(wt), destructive: true, separatorBefore: true },
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
        title={t('webhooks.deleteTitle')}
        message={t('webhooks.deleteMessage', { name: deleting?.name ?? '' })}
        confirmText={t('webhooks.deleteConfirm')}
        isDangerous
        onConfirm={confirmDelete}
        onCancel={() => setDeleting(null)}
      />
    </Card>
  );
}

function WebhookTargetDialog({ target, onClose }: { target: WebhookTarget | null; onClose: () => void }) {
  const t = useT();
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
      setError(t('webhooks.nameRequired'));
      return;
    }
    const url = newUrl ?? target?.url ?? '';
    const secret = newSecret ?? target?.secret ?? '';
    if (!isTelegram && !url.trim()) {
      setError(t('webhooks.urlRequired'));
      return;
    }
    if (isTelegram && (!secret.trim() || !chatId.trim())) {
      setError(t('webhooks.telegramRequired'));
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
        onError: (err) => setError(apiErrorMessage(err, t('webhooks.saveFailed'))),
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
            {isEdit ? t('webhooks.editTitle', { name: target!.name }) : t('webhooks.addTitle')}
          </h3>
          <button type="button" onClick={onClose} aria-label={t('common.close')} className="text-muted-foreground hover:text-foreground p-1">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="wt-name" className="text-xs font-semibold text-foreground">{t('webhooks.name')}</label>
          <input id="wt-name" ref={nameRef} value={name} onChange={(e) => setName(e.target.value)} className="form-input text-sm" placeholder={t('webhooks.namePlaceholder')} />
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="wt-kind" className="text-xs font-semibold text-foreground">{t('webhooks.kind')}</label>
          <select id="wt-kind" value={kind} onChange={(e) => setKind(e.target.value as WebhookTargetKind)} className="form-select text-sm">
            {KINDS.map((k) => (
              <option key={k.value} value={k.value}>{k.label || t('webhooks.generic')}</option>
            ))}
          </select>
        </div>

        {!isTelegram && (
          <SecretInput
            id="wt-url"
            label={t('webhooks.url')}
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
              label={t('webhooks.botToken')}
              masked={isEdit && target!.kind === 'telegram' ? target!.secret : ''}
              value={newSecret}
              onChange={setNewSecret}
              placeholder={TELEGRAM_TOKEN_HINT}
              inputType="password"
            />
            <div className="flex flex-col gap-1.5">
              <label htmlFor="wt-chat" className="text-xs font-semibold text-foreground">{t('webhooks.chatId')}</label>
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
          <span className="text-xs font-semibold text-foreground">{t('webhooks.enabled')}</span>
        </label>

        {error && (
          <div role="alert" className="text-xs text-destructive bg-destructive/15 border border-destructive/40 rounded px-3 py-2">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-3 pt-1">
          <button type="button" onClick={onClose} className="bg-secondary hover:bg-accent text-foreground rounded border border-border px-4 py-2 text-sm">
            {t('common.cancel')}
          </button>
          <button
            type="submit"
            disabled={save.isPending}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary px-4 py-2 text-sm font-semibold flex items-center gap-1.5 disabled:opacity-50"
          >
            {save.isPending && <Loader2 className="w-4 h-4 animate-spin" />}
            {isEdit ? t('common.save') : t('webhooks.addTarget')}
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
  const t = useT();
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
            <Edit3 className="w-3 h-3" /> {t('webhooks.change')}
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
              title={t('webhooks.keepStoredTitle')}
              aria-label={t('webhooks.keepStoredAria', { label })}
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
