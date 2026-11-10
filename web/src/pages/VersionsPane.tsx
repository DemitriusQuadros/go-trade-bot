import React, { useMemo, useState } from 'react';
import { useOutletContext } from 'react-router-dom';
import {
  GitBranch,
  RotateCcw,
  Clock,
  Code2,
  Copy,
  Check,
  ChevronDown,
  ChevronRight,
  AlertCircle,
  FileCode2,
} from 'lucide-react';
import { useScriptVersions, useRevertScriptVersion } from '@/hooks/queries';
import { ScriptVersion } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { LoadingScreen } from '@/components/ui/Spinner';
import { useToast } from '@/context/ToastContext';
import { formatDateTime, formatRelative } from '@/lib/format';
import { useT } from '@/i18n';
import { WorkbenchContext } from './WorkbenchShell';

export function VersionsPane() {
  const t = useT();
  const ctx = useOutletContext<WorkbenchContext>();
  const { draft, setDraft, strategyId, appendConsoleEntry } = ctx;
  const { toast } = useToast();

  const sid = strategyId ?? 0;
  const { data: rawVersions = [], isLoading, error } = useScriptVersions(sid);
  const revertMutation = useRevertScriptVersion();

  const [expandedVersionId, setExpandedVersionId] = useState<number | null>(null);
  const [revertTarget, setRevertTarget] = useState<ScriptVersion | null>(null);
  const [copiedId, setCopiedId] = useState<number | null>(null);

  // Newest versions first
  const versions = useMemo(() => {
    return [...rawVersions].sort((a, b) => {
      const ta = new Date(a.created_at).getTime();
      const tb = new Date(b.created_at).getTime();
      return tb - ta;
    });
  }, [rawVersions]);

  // Current active version is the newest one or the one matching draft.source
  const activeVersionId = useMemo(() => {
    if (versions.length === 0) return null;
    const match = versions.find((v) => v.source === draft.source);
    if (match) return match.id;
    return versions[0].id;
  }, [versions, draft.source]);

  const handleCopy = (version: ScriptVersion) => {
    navigator.clipboard.writeText(version.source);
    setCopiedId(version.id);
    setTimeout(() => setCopiedId(null), 2000);
    toast(t('common.copied'), 'info');
  };

  const handleConfirmRevert = () => {
    if (!revertTarget || !sid) return;
    const target = revertTarget;
    setRevertTarget(null);

    revertMutation.mutate(
      { id: sid, versionId: target.id },
      {
        onSuccess: () => {
          // Update draft editor buffer immediately
          setDraft((prev) => ({
            ...prev,
            source: target.source,
          }));

          appendConsoleEntry?.({
            source: 'editor',
            kind: 'debug_log',
            label: 'version',
            message: t('workbench.revertedConsole', {
              strategyId: sid,
              versionId: target.id,
            }),
          });

          toast(t('workbench.revertSuccess', { id: target.id }), 'success');
        },
        onError: (err) => {
          toast(err instanceof Error ? err.message : t('common.renderError'), 'error');
        },
      }
    );
  };

  if (isLoading) {
    return <LoadingScreen message={t('workbench.versionHistory')} />;
  }

  if (error) {
    return (
      <div className="p-6 text-xs text-destructive bg-destructive/10 rounded-lg flex items-center gap-2">
        <AlertCircle className="w-4 h-4 shrink-0" />
        <span>{error instanceof Error ? error.message : t('common.renderError')}</span>
      </div>
    );
  }

  return (
    <div className="space-y-4 p-1 pb-16 font-sans">
      <div className="flex items-center justify-between gap-2 border-b border-border pb-3">
        <div>
          <h2 className="text-sm font-semibold text-foreground flex items-center gap-1.5">
            <GitBranch className="w-4 h-4 text-primary" />
            <span>{t('workbench.versionHistory')}</span>
            <span className="text-xs font-mono font-normal text-muted-foreground">
              ({versions.length})
            </span>
          </h2>
          <p className="text-[11px] text-muted-foreground mt-0.5">
            {t('workbench.versionHistorySubtitle')}
          </p>
        </div>
      </div>

      {versions.length === 0 ? (
        <Card className="p-8 text-center text-xs text-muted-foreground bg-card/40">
          <FileCode2 className="w-8 h-8 mx-auto mb-2 opacity-40" />
          <p>{t('workbench.noVersions')}</p>
        </Card>
      ) : (
        <div className="space-y-2.5">
          {versions.map((v) => {
            const isActive = v.id === activeVersionId;
            const isExpanded = v.id === expandedVersionId;
            const lineCount = (v.source.match(/\n/g) || []).length + 1;
            const byteSize = new Blob([v.source]).size;

            return (
              <Card
                key={v.id}
                className={`transition-colors border ${
                  isActive
                    ? 'border-primary/50 bg-primary/5'
                    : 'border-border/60 hover:border-border bg-card'
                }`}
              >
                <div className="p-3.5">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="font-semibold text-xs text-foreground font-mono">
                          {t('workbench.version', { id: v.id })}
                        </span>
                        {isActive && (
                          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-success/15 text-success border border-success/30">
                            {t('workbench.activeBadge')}
                          </span>
                        )}
                        <span className="text-[11px] text-muted-foreground font-mono">
                          {t('workbench.lines', { count: lineCount })} · {byteSize} B
                        </span>
                      </div>

                      <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                        <Clock className="w-3 h-3 shrink-0" />
                        <span title={formatDateTime(v.created_at, { utc: true }) + ' UTC'}>
                          {formatRelative(v.created_at)} ({formatDateTime(v.created_at)})
                        </span>
                      </div>
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0">
                      <button
                        type="button"
                        onClick={() => setExpandedVersionId(isExpanded ? null : v.id)}
                        className="px-2 py-1 rounded text-xs border border-border bg-secondary/50 hover:bg-secondary text-foreground flex items-center gap-1 transition-colors"
                        title={isExpanded ? t('workbench.hideCode') : t('workbench.viewCode')}
                      >
                        <Code2 className="w-3 h-3" />
                        <span className="hidden sm:inline">
                          {isExpanded ? t('workbench.hideCode') : t('workbench.viewCode')}
                        </span>
                        {isExpanded ? (
                          <ChevronDown className="w-3 h-3" />
                        ) : (
                          <ChevronRight className="w-3 h-3" />
                        )}
                      </button>

                      <button
                        type="button"
                        onClick={() => setRevertTarget(v)}
                        disabled={isActive || revertMutation.isPending}
                        className={`px-2.5 py-1 rounded text-xs font-medium flex items-center gap-1 transition-colors border ${
                          isActive
                            ? 'opacity-40 cursor-not-allowed border-border text-muted-foreground'
                            : 'bg-primary/10 hover:bg-primary/20 text-primary border-primary/30 active:scale-95'
                        }`}
                        title={isActive ? t('workbench.activeVersion') : t('workbench.revert')}
                      >
                        <RotateCcw className="w-3 h-3" />
                        <span>{t('workbench.revert')}</span>
                      </button>
                    </div>
                  </div>

                  {/* Expandable Source Preview */}
                  {isExpanded && (
                    <div className="mt-3 pt-3 border-t border-border/60 space-y-2">
                      <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                        <span className="font-mono">Lua Script Source ({v.id})</span>
                        <button
                          type="button"
                          onClick={() => handleCopy(v)}
                          className="flex items-center gap-1 hover:text-foreground p-1 rounded hover:bg-secondary transition-colors"
                          title={t('common.copy')}
                        >
                          {copiedId === v.id ? (
                            <Check className="w-3 h-3 text-success" />
                          ) : (
                            <Copy className="w-3 h-3" />
                          )}
                          <span>{copiedId === v.id ? t('common.copied') : t('common.copy')}</span>
                        </button>
                      </div>

                      <pre className="max-h-72 overflow-y-auto p-2.5 rounded bg-background/90 border border-border font-mono text-[11px] text-foreground leading-relaxed whitespace-pre-wrap selection:bg-primary/20">
                        {v.source}
                      </pre>
                    </div>
                  )}
                </div>
              </Card>
            );
          })}
        </div>
      )}

      {/* Confirmation Dialog before Revert */}
      <ConfirmDialog
        isOpen={revertTarget !== null}
        title={t('workbench.revertTitle')}
        message={
          revertTarget
            ? t('workbench.revertMessage', {
                strategyId: sid,
                versionId: revertTarget.id,
              })
            : ''
        }
        confirmText={t('workbench.revert')}
        cancelText={t('common.cancel')}
        isDangerous={false}
        onConfirm={handleConfirmRevert}
        onCancel={() => setRevertTarget(null)}
      />
    </div>
  );
}
