import React from 'react';
import { useScriptVersions, useRevertScriptVersion } from '@/hooks/queries';
import { Card, CardHeader } from '@/components/ui/Card';
import { RotateCcw, Clock } from 'lucide-react';
import { LoadingScreen } from '@/components/ui/Spinner';

export function ScriptVersionsPanel({ strategyId }: { strategyId: number }) {
  const { data: versions, isLoading } = useScriptVersions(strategyId);
  const revertMutation = useRevertScriptVersion();

  if (isLoading) return <LoadingScreen />;
  if (!versions || versions.length === 0) return <div className="p-4 text-green-700">No versions found.</div>;

  return (
    <Card className="border-green-900/50 bg-black">
      <CardHeader title="Version History" className="border-b border-green-900/50 pb-3" />
      <div className="p-4 space-y-4 max-h-[400px] overflow-y-auto">
        {versions.map((v) => (
          <div key={v.id} className="p-3 border border-green-900/50 rounded flex justify-between items-center hover:bg-green-950/20">
            <div>
              <p className="text-sm font-semibold text-green-400">Version {v.id}</p>
              <p className="text-xs text-green-700">{new Date(v.created_at).toLocaleString()}</p>
            </div>
            <button
              onClick={() => {
                if (confirm('Revert to this version? Unsaved changes will be lost.')) {
                  revertMutation.mutate({ id: strategyId, versionId: v.id });
                }
              }}
              className="text-xs flex items-center gap-1 bg-green-950/50 hover:bg-green-900 text-green-400 px-2 py-1 rounded"
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
