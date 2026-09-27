import React from 'react';
import { useScriptVersions, useRevertScriptVersion } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { RotateCcw, Clock } from 'lucide-react';
import { LoadingScreen } from '@/components/ui/Spinner';

export function ScriptVersionsPanel({ strategyId }: { strategyId: number }) {
  const { data: versions, isLoading } = useScriptVersions(strategyId);
  const revertMutation = useRevertScriptVersion();

  if (isLoading) return <LoadingScreen />;
  if (!versions || versions.length === 0) return <div className="p-4 text-muted-foreground">No versions found.</div>;

  return (
    <Card className="border-border/50 bg-background">
      <CardHeader title="Version History" className="border-b border-border/50 pb-3" />
      <div className="p-4 space-y-4 max-h-[400px] overflow-y-auto">
        {versions.map((v) => (
          <div key={v.id} className="p-3 border border-border/50 rounded flex justify-between items-center hover:bg-card/20">
            <div>
              <p className="text-sm font-semibold text-foreground">Version {v.id}</p>
              <p className="text-xs text-muted-foreground">{new Date(v.created_at).toLocaleString()}</p>
            </div>
            <button
              onClick={() => {
                if (confirm('Revert to this version? Unsaved changes will be lost.')) {
                  revertMutation.mutate({ id: strategyId, versionId: v.id });
                }
              }}
              className="text-xs flex items-center gap-1 bg-card/50 hover:bg-secondary text-foreground px-2 py-1 rounded"
              disabled={revertMutation.isPending}
            >
              <RotateCcw className="w-3 h-3" /> Revert
            </button>
          </div>
        ))}
      </div>
    </Card>
  );
}
