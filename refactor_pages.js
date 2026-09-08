const fs = require('fs');
const path = require('path');

const pagesDir = path.join('web', 'src', 'pages');
const pages = fs.readdirSync(pagesDir).filter(f => f.endsWith('.tsx') && f !== 'Dashboard.tsx');

for (const page of pages) {
  let content = fs.readFileSync(path.join(pagesDir, page), 'utf8');

  // Remove usePolling import
  content = content.replace(/import\s+{\s*usePolling\s*}\s*from\s*'@\/hooks\/usePolling';\n?/g, '');

  // Add queries import if not exists
  if (!content.includes('@/hooks/queries')) {
    content = `import { useStrategies, useSignals, useTickerPrices, useBacktests, useAccount } from '@/hooks/queries';\n` + content;
  }

  // Replace usages
  content = content.replace(/const (\w+)Polling\s*=\s*usePolling\(\(\)\s*=>\s*api\.getStrategies\(\)[^;]+;/g, 'const { data: $1 = [], refetch: refetch$1 } = useStrategies();');
  content = content.replace(/const { data: strategies, loading, refetch } = usePolling\(\(\) => api.getStrategies\(\)[^;]+;/g, 'const { data: strategies = [], isLoading: loading, refetch } = useStrategies();');
  content = content.replace(/const (\w+)Polling\s*=\s*usePolling\(\(\)\s*=>\s*api\.listBacktests\(\)[^;]+;/g, 'const { data: $1 = [], refetch: refetch$1 } = useBacktests();');
  content = content.replace(/const (\w+)Polling\s*=\s*usePolling\(\s*\(\)\s*=>\s*api\.getSignals\('(\w+)'\)[^;]+;/g, 'const { data: $1 = [], refetch: refetch$1 } = useSignals(\'$2\');');
  content = content.replace(/const (\w+)Polling\s*=\s*usePolling\(\(\)\s*=>\s*api\.getTickerPrices\(\)[^;]+;/g, 'const { data: $1 = [], refetch: refetch$1 } = useTickerPrices();');

  // Fix data accesses
  content = content.replace(/(\w+)Polling\.data/g, '$1');
  content = content.replace(/(\w+)Polling\.refetch\(\)/g, 'refetch$1()');

  // There's one usePolling in Optimization.tsx for getOptimizationStatus that might need custom handling
  // Let's replace manually after script

  fs.writeFileSync(path.join(pagesDir, page), content);
}
