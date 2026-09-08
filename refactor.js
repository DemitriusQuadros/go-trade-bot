const fs = require('fs');
const path = require('path');

const pages = ['Positions.tsx', 'Optimization.tsx', 'ExecutionLog.tsx', 'BacktestLauncher.tsx', 'BacktestResults.tsx', 'Strategies.tsx'];

pages.forEach(page => {
  const file = path.join('web', 'src', 'pages', page);
  if (!fs.existsSync(file)) return;
  
  let content = fs.readFileSync(file, 'utf8');

  // 1. Remove import { usePolling }
  content = content.replace(/import\s*\{\s*usePolling\s*\}\s*from\s*'@\/hooks\/usePolling';\n?/g, '');
  
  const neededHooks = new Set();
  
  // 2. Replace usePolling calls
  content = content.replace(/const (\w+)Polling = usePolling\([^)]+api\.getStrategies\(\)[^;]+;/g, (match, p1) => {
    neededHooks.add('useStrategies');
    return `const { data: ${p1} = [], refetch: refetch${p1} } = useStrategies();`;
  });
  
  content = content.replace(/const \{\s*data:\s*strategies,\s*loading,\s*refetch\s*\} = usePolling\([^)]+api\.getStrategies\(\)[^;]+;/g, () => {
    neededHooks.add('useStrategies');
    return `const { data: strategies = [], isLoading: loading, refetch } = useStrategies();`;
  });
  
  content = content.replace(/const (\w+)Polling = usePolling\([^)]+api\.listBacktests\(\)[^;]+;/g, (match, p1) => {
    neededHooks.add('useBacktests');
    return `const { data: ${p1} = [], refetch: refetch${p1} } = useBacktests();`;
  });
  
  content = content.replace(/const (\w+)Polling = usePolling\(\s*\(\)\s*=>\s*api\.getSignals\('([^']+)'\)[^;]+;/g, (match, p1, p2) => {
    neededHooks.add('useSignals');
    return `const { data: ${p1} = [], refetch: refetch${p1} } = useSignals('${p2}');`;
  });
  
  content = content.replace(/const (\w+)Polling = usePolling\([^)]+api\.getTickerPrices\(\)[^;]+;/g, (match, p1) => {
    neededHooks.add('useTickerPrices');
    return `const { data: ${p1} = [], refetch: refetch${p1} } = useTickerPrices();`;
  });

  content = content.replace(/const statusPolling = usePolling\([\s\S]+?intervalMs:\s*2000,\s*enabled:\s*!!activeRunId,\s*\}\s*\);/g, () => {
    neededHooks.add('useQuery');
    return `const { data: statusData, refetch: refetchStatus } = useQuery({ queryKey: ['optStatus', activeRunId], queryFn: () => api.getOptimizationStatus(activeRunId, {}), enabled: !!activeRunId, refetchInterval: 2000 });`;
  });
  
  content = content.replace(/const backtestPolling = usePolling\(\s*\(\)\s*=>\s*api\.getBacktest\(Number\(id\)\),\s*\{\s*intervalMs: 5000\s*\}\s*\);/g, () => {
    neededHooks.add('useBacktest');
    return `const { data: backtestData, refetch: refetchBacktest, isLoading: isBacktestLoading } = useBacktest(Number(id));`;
  });

  // Data access fixes
  content = content.replace(/(\w+)Polling\.data/g, '$1');
  content = content.replace(/(\w+)Polling\.refetch\(\)/g, 'refetch$1()');
  content = content.replace(/statusPolling\.refetch\(\)/g, 'refetchStatus()');
  content = content.replace(/statusPolling\.loading/g, '(!statusData)');
  content = content.replace(/backtestPolling\.data/g, 'backtestData');
  content = content.replace(/backtestPolling\.loading/g, 'isBacktestLoading');
  content = content.replace(/backtestPolling\.refetch\(\)/g, 'refetchBacktest()');
  content = content.replace(/(\w+)Polling\.loading/g, 'false');

  // Add the imports safely
  const queryImports = Array.from(neededHooks).filter(h => h !== 'useQuery');
  if (queryImports.length > 0) {
    if (!content.includes('@/hooks/queries')) {
      content = `import { ${queryImports.join(', ')} } from '@/hooks/queries';\n` + content;
    } else {
      // Just naively prepend it if there's no cleaner way
      content = `import { ${queryImports.join(', ')} } from '@/hooks/queries';\n` + content.replace(/import \{.*\} from '@\/hooks\/queries';\n/g, '');
    }
  }
  if (neededHooks.has('useQuery') && !content.includes('@tanstack/react-query')) {
    content = `import { useQuery } from '@tanstack/react-query';\n` + content;
  }

  // Visual terminal updates
  content = content.replace(/bg-slate-950/g, 'bg-black');
  content = content.replace(/bg-slate-900\/40/g, 'bg-black/40');
  content = content.replace(/bg-slate-900\/50/g, 'bg-black/50');
  content = content.replace(/bg-slate-900/g, 'bg-green-950/20');
  content = content.replace(/border-slate-800\/80/g, 'border-green-900/30');
  content = content.replace(/border-slate-800/g, 'border-green-900/30');
  content = content.replace(/text-slate-100/g, 'text-green-500');
  content = content.replace(/text-slate-200/g, 'text-green-400');
  content = content.replace(/text-slate-300/g, 'text-green-600');
  content = content.replace(/text-slate-400/g, 'text-green-700');
  content = content.replace(/text-slate-500/g, 'text-green-800');
  content = content.replace(/bg-blue-600/g, 'bg-green-800');
  content = content.replace(/text-blue-400/g, 'text-green-500');

  // Fix optimization status data usages
  content = content.replace(/statusData\?\.status/g, '(statusData as any)?.status');
  content = content.replace(/statusData\?\.progress/g, '(statusData as any)?.progress');
  content = content.replace(/statusData\?\.total_combinations/g, '(statusData as any)?.total_combinations');

  fs.writeFileSync(file, content);
});
