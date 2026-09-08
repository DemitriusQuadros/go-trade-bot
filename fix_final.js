const fs = require('fs');

// BacktestLauncher
let b = fs.readFileSync('web/src/pages/BacktestLauncher.tsx', 'utf8');
b = b.replace(/const \{ data: strategies = \[\] \} = useStrategies\(\);\n/g, '');
b = b.replace(/const \{ data: recentRuns = \[\], refetch: refetchRecentRuns \} = useBacktests\(\);\n/g, '');
b = b.replace(/const \[strategyId, setStrategyId\] = useState<number>\(strategies\[0\]\?\.id \|\| 1\);/, 
  "const { data: strategies = [] } = useStrategies();\n  const { data: recentRuns = [], refetch: refetchRecentRuns } = useBacktests();\n  const [strategyId, setStrategyId] = useState<number>(strategies[0]?.id || 1);"
);
b = b.replace(/recentRunsPolling\.refetch\(\)/g, "refetchRecentRuns()");
b = b.replace(/recentRunsPolling\.data/g, "recentRuns");
b = b.replace(/recentRunsPolling\.loading/g, "(!recentRuns)");
fs.writeFileSync('web/src/pages/BacktestLauncher.tsx', b);

// Optimization
let o = fs.readFileSync('web/src/pages/Optimization.tsx', 'utf8');
o = o.replace(/const \[activeRunId, setActiveRunId\] = useState<number \| null>\(null\);\n  const \{ data: strategies = \[\] \} = useStrategies\(\);\n  const \{ data: statusData, refetch: refetchStatus \} = useQuery\(\{\s*queryKey:\s*\['optStatus',\s*activeRunId\],\s*queryFn:\s*\(\)\s*=>\s*api.getOptimizationStatus\(activeRunId as number, \{\}\),\s*enabled:\s*!!activeRunId,\s*refetchInterval:\s*2000\s*\}\);\n/g, "const [activeRunId, setActiveRunId] = useState<number | null>(null);\n");

o = o.replace(/const \[strategyId, setStrategyId\] = useState<number>\(strategies\[0\]\?\.id \|\| 1\);/g, 
  "const { data: strategies = [] } = useStrategies();\n  const [strategyId, setStrategyId] = useState<number>(strategies[0]?.id || 1);"
);

o = o.replace(/const statusData = statusData;\n/g, '');
o = o.replace(/const statusData = statusPolling\.data \|\| null;\n/g, '');

o = o.replace(/const \[activeRunId, setActiveRunId\] = useState<number \| null>\(null\);/g, "const [activeRunId, setActiveRunId] = useState<number | null>(null);\n  const { data: statusData, refetch: refetchStatus } = useQuery({ queryKey: ['optStatus', activeRunId], queryFn: () => api.getOptimizationStatus(activeRunId as number, {}), enabled: !!activeRunId, refetchInterval: 2000 });");

fs.writeFileSync('web/src/pages/Optimization.tsx', o);

