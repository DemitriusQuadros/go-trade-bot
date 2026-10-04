import React from 'react';
import { useScriptVersions, useRevertScriptVersion } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { RotateCcw, Clock } from 'lucide-react';
import { LoadingScreen } from '@/components/ui/Spinner';
import { formatDateTime } from '@/lib/format';
import { useT } from '@/i18n';

export function ScriptVersionsPanel({ strategyId }: { strategyId: number }) {
  const t = useT();
  const { data: versions, isLoading } = useScriptVersions(strategyId);
  const revertMutation = useRevertScriptVersion();

  if (isLoading) return <LoadingScreen />;
  if (!versions || versions.length === 0) return <div className="p-4 text-muted-foreground">{t('workbench.noVersions')}</div>;

  return (
    <Card className="border-border/50 bg-background">
      <CardHeader title={t('workbench.versionHistory')} className="border-b border-border/50 pb-3" />
      <div className="p-4 space-y-4 max-h-[400px] overflow-y-auto">
        {versions.map((v) => (
          <div key={v.id} className="p-3 border border-border/50 rounded flex justify-between items-center hover:bg-card/20">
            <div>
              <p className="text-sm font-semibold text-foreground">{t('workbench.version', { id: v.id })}</p>
              <p className="text-xs text-muted-foreground">{formatDateTime(v.created_at)}</p>
            </div>
            <button
              onClick={() => {
                if (confirm(t('workbench.revertConfirm'))) {
                  revertMutation.mutate({ id: strategyId, versionId: v.id });
                }
              }}
              className="text-xs flex items-center gap-1 bg-card/50 hover:bg-secondary text-foreground px-2 py-1 rounded"
              disabled={revertMutation.isPending}
            >
              <RotateCcw className="w-3 h-3" /> {t('workbench.revert')}
            </button>
          </div>
        ))}
      </div>
    </Card>
  );
}
