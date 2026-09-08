const fs = require('fs');

let text = fs.readFileSync('web/src/pages/Optimization.tsx', 'utf8');

text = text.replace(/import \{ usePolling \} from '@\/hooks\/usePolling';\n?/, "import { useStrategies } from '@/hooks/queries';\nimport { useQuery } from '@tanstack/react-query';\n");

text = text.replace(/const strategiesPolling = usePolling\([\s\S]*?intervalMs[^\}]+\}\s*\);/, '');
text = text.replace(/const statusPolling = usePolling\([\s\S]*?intervalMs[^\}]+\}\s*\);/, '');
text = text.replace(/const strategies = strategiesPolling\.data \|\| \[\];/, '');
text = text.replace(/const statusData = statusPolling\.data \|\| null;/, '');

text = text.replace(/const \[activeRunId, setActiveRunId\] = useState<number \| null>\(null\);/, 
  "const [activeRunId, setActiveRunId] = useState<number | null>(null);\n  const { data: strategies = [] } = useStrategies();\n  const { data: statusData, refetch: refetchStatus } = useQuery({ queryKey: ['optStatus', activeRunId], queryFn: () => api.getOptimizationStatus(activeRunId as number, {}), enabled: !!activeRunId, refetchInterval: 2000 });"
);

text = text.replace(/statusPolling\.refetch\(\)/g, "refetchStatus()");
text = text.replace(/statusPolling\.loading/g, "(!statusData)");

// styles
text = text.replace(/bg-slate-950/g, 'bg-black');
text = text.replace(/bg-slate-900\/40/g, 'bg-black/40');
text = text.replace(/bg-slate-900\/50/g, 'bg-black/50');
text = text.replace(/bg-slate-900/g, 'bg-green-950/20');
text = text.replace(/border-slate-800\/80/g, 'border-green-900/30');
text = text.replace(/border-slate-800/g, 'border-green-900/30');
text = text.replace(/text-slate-100/g, 'text-green-500');
text = text.replace(/text-slate-200/g, 'text-green-400');
text = text.replace(/text-slate-300/g, 'text-green-600');
text = text.replace(/text-slate-400/g, 'text-green-700');
text = text.replace(/text-slate-500/g, 'text-green-800');
text = text.replace(/bg-blue-600/g, 'bg-green-800');
text = text.replace(/text-blue-400/g, 'text-green-500');

text = text.replace(/\(s\)/g, "(s: any)");

fs.writeFileSync('web/src/pages/Optimization.tsx', text);

let text2 = fs.readFileSync('web/src/pages/BacktestLauncher.tsx', 'utf8');

text2 = text2.replace(/import \{ usePolling \} from '@\/hooks\/usePolling';\n?/, "import { useStrategies, useBacktests } from '@/hooks/queries';\n");

text2 = text2.replace(/const strategiesPolling = usePolling\([\s\S]*?intervalMs[^\}]+\}\s*\);/, '');
text2 = text2.replace(/const recentRunsPolling = usePolling\([\s\S]*?intervalMs[^\}]+\}\s*\);/, '');
text2 = text2.replace(/const strategies = strategiesPolling\.data \|\| \[\];/, '');
text2 = text2.replace(/const recentRuns = recentRunsPolling\.data \|\| \[\];/, '');

text2 = text2.replace(/const \[strategyId, setStrategyId\] = useState<number>\(strategies\[0\]\?\.id \|\| 1\);/, 
  "const { data: strategies = [] } = useStrategies();\n  const { data: recentRuns = [], refetch: refetchRecentRuns } = useBacktests();\n  const [strategyId, setStrategyId] = useState<number>(strategies[0]?.id || 1);"
);

text2 = text2.replace(/recentRunsPolling\.refetch\(\)/g, "refetchRecentRuns()");
text2 = text2.replace(/recentRunsPolling\.loading/g, "(false)");

// styles
text2 = text2.replace(/bg-slate-950/g, 'bg-black');
text2 = text2.replace(/bg-slate-900\/40/g, 'bg-black/40');
text2 = text2.replace(/bg-slate-900\/50/g, 'bg-black/50');
text2 = text2.replace(/bg-slate-900/g, 'bg-green-950/20');
text2 = text2.replace(/border-slate-800\/80/g, 'border-green-900/30');
text2 = text2.replace(/border-slate-800/g, 'border-green-900/30');
text2 = text2.replace(/text-slate-100/g, 'text-green-500');
text2 = text2.replace(/text-slate-200/g, 'text-green-400');
text2 = text2.replace(/text-slate-300/g, 'text-green-600');
text2 = text2.replace(/text-slate-400/g, 'text-green-700');
text2 = text2.replace(/text-slate-500/g, 'text-green-800');
text2 = text2.replace(/bg-blue-600/g, 'bg-green-800');
text2 = text2.replace(/text-blue-400/g, 'text-green-500');

text2 = text2.replace(/\(s\)/g, "(s: any)");
text2 = text2.replace(/\(run\)/g, "(run: any)");

fs.writeFileSync('web/src/pages/BacktestLauncher.tsx', text2);

