const fs = require('fs');

function processFile(path, hooksStr, replacementBlock, additional) {
  let text = fs.readFileSync(path, 'utf8');
  text = text.replace(/import \{ usePolling \} from '@\/hooks\/usePolling';\n?/g, '');
  text = `import { ${hooksStr} } from '@/hooks/queries';\n` + text;
  text = text.replace(/const \w+Polling = usePolling\([\s\S]*?intervalMs[^\)]+\)\);?/g, '');
  text = text.replace(/const \w+Polling = usePolling\([\s\S]*?intervalMs[^\}]+\}\s*\);?/g, '');
  text = text.replace(/const \{[^\}]+} = usePolling\([\s\S]*?intervalMs[^\}]+\}\s*\);?/g, '');
  
  // Custom manual replacements for the blocks we just deleted
  // We will inject replacementBlock right after the first `const [.*] = useState` or `const sse = useSSE()`
  text = text.replace(/(const \[[^\]]+\] = useState[^;]+;)/, `$1\n  ${replacementBlock}`);

  if (additional) {
    text = additional(text);
  }

  // Common UI replacements
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

  fs.writeFileSync(path, text);
}

// 1. Positions.tsx
processFile('web/src/pages/Positions.tsx', 'useSignals, useStrategies, useTickerPrices', 
  `const { data: signals = [], refetch: refetchSignals, isLoading: isSignalsLoading } = useSignals(statusTab as any);\n  const { data: strategies = [] } = useStrategies();\n  const { data: tickers = [] } = useTickerPrices();`,
  (t) => {
    t = t.replace(/signalsPolling\.data/g, 'signals');
    t = t.replace(/strategiesPolling\.data/g, 'strategies');
    t = t.replace(/tickersPolling\.data/g, 'tickers');
    t = t.replace(/signalsPolling\.refetch\(\)/g, 'refetchSignals()');
    t = t.replace(/signalsPolling\.loading/g, 'isSignalsLoading');
    return t;
  }
);

// 2. Strategies.tsx
processFile('web/src/pages/Strategies.tsx', 'useStrategies',
  `const { data: strategies = [], refetch: refetchStrategies, isLoading: isStrategiesLoading } = useStrategies();`,
  (t) => {
    t = t.replace(/loading/g, 'isStrategiesLoading');
    t = t.replace(/refetch\(\)/g, 'refetchStrategies()');
    return t;
  }
);

// 3. ExecutionLog.tsx
processFile('web/src/pages/ExecutionLog.tsx', 'useSignals, useStrategies',
  `const { data: signals = [], refetch: refetchSignals, isLoading: isSignalsLoading } = useSignals('closed');\n  const { data: strategies = [] } = useStrategies();`,
  (t) => {
    t = t.replace(/signalsPolling\.data/g, 'signals');
    t = t.replace(/strategiesPolling\.data/g, 'strategies');
    t = t.replace(/signalsPolling\.refetch\(\)/g, 'refetchSignals()');
    t = t.replace(/signalsPolling\.loading/g, 'isSignalsLoading');
    return t;
  }
);

// 4. BacktestLauncher.tsx
processFile('web/src/pages/BacktestLauncher.tsx', 'useStrategies, useBacktests',
  `const { data: strategies = [] } = useStrategies();\n  const { data: recentRuns = [], refetch: refetchRecentRuns } = useBacktests();`,
  (t) => {
    t = t.replace(/strategiesPolling\.data/g, 'strategies');
    t = t.replace(/recentRunsPolling\.data/g, 'recentRuns');
    t = t.replace(/recentRunsPolling\.refetch\(\)/g, 'refetchRecentRuns()');
    return t;
  }
);

// 5. BacktestResults.tsx
processFile('web/src/pages/BacktestResults.tsx', 'useBacktest',
  `const { data: backtest, refetch: refetchBacktest, isLoading: isBacktestLoading } = useBacktest(Number(id));`,
  (t) => {
    t = t.replace(/const backtestPolling = usePolling[^\;]+;/g, '');
    t = t.replace(/backtestPolling\.data/g, 'backtest');
    t = t.replace(/backtestPolling\.loading/g, 'isBacktestLoading');
    t = t.replace(/backtestPolling\.refetch\(\)/g, 'refetchBacktest()');
    return t;
  }
);

// 6. Optimization.tsx
processFile('web/src/pages/Optimization.tsx', 'useStrategies, useQuery',
  `const { data: strategies = [] } = useStrategies();\n  const { data: statusData, refetch: refetchStatus } = useQuery({ queryKey: ['optStatus', activeRunId], queryFn: () => api.getOptimizationStatus(activeRunId, {}), enabled: !!activeRunId, refetchInterval: 2000 });`,
  (t) => {
    t = t.replace(/strategiesPolling\.data/g, 'strategies');
    t = t.replace(/statusPolling\.data/g, 'statusData');
    t = t.replace(/statusPolling\.refetch\(\)/g, 'refetchStatus()');
    t = t.replace(/statusPolling\.loading/g, '(!statusData)');
    t = t.replace(/import \{ useQuery \} from '@\/hooks\/queries';\n/, ''); // remove useQuery from custom hooks import
    t = `import { useQuery } from '@tanstack/react-query';\n` + t;
    return t;
  }
);

