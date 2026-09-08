const fs = require('fs');

let t = fs.readFileSync('web/src/pages/Optimization.tsx', 'utf8');
t = t.replace(/import \{ useStrategies, useQuery \} from '@\/hooks\/queries';/, "import { useStrategies } from '@/hooks/queries';");
t = t.replace(/const \{ data: strategies = \[\] \} = useStrategies\(\);\n/g, '');
t = t.replace(/const \{ data: statusData, refetch: refetchStatus \} = useQuery\([^)]+\}\);\n/g, '');
t = t.replace(/const statusData = statusData;\n/g, '');
t = t.replace(/const \[activeRunId, setActiveRunId\] = useState<number \| null>\(null\);\n/, "const [activeRunId, setActiveRunId] = useState<number | null>(null);\n  const { data: strategies = [] } = useStrategies();\n  const { data: statusData, refetch: refetchStatus } = useQuery({ queryKey: ['optStatus', activeRunId], queryFn: () => api.getOptimizationStatus(activeRunId, {}), enabled: !!activeRunId, refetchInterval: 2000 });\n");
fs.writeFileSync('web/src/pages/Optimization.tsx', t);

let b = fs.readFileSync('web/src/pages/BacktestLauncher.tsx', 'utf8');
b = b.replace(/const \{ data: strategies = \[\] \} = useStrategies\(\);\n/g, '');
b = b.replace(/const \{ data: recentRuns = \[\], refetch: refetchRecentRuns \} = useBacktests\(\);\n/g, '');
b = b.replace(/const \[strategyId, setStrategyId\] = useState<number>\(strategies\[0\]\?\.id \|\| 1\);/, "const { data: strategies = [] } = useStrategies();\n  const { data: recentRuns = [], refetch: refetchRecentRuns } = useBacktests();\n  const [strategyId, setStrategyId] = useState<number>(strategies[0]?.id || 1);");
fs.writeFileSync('web/src/pages/BacktestLauncher.tsx', b);
