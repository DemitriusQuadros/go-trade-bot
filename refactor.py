import os
import re

pages_dir = 'web/src/pages'
for filename in os.listdir(pages_dir):
    if not filename.endswith('.tsx'): continue
    filepath = os.path.join(pages_dir, filename)
    with open(filepath, 'r') as f:
        content = f.read()

    # Remove usePolling import
    content = re.sub(r"import\s*\{\s*usePolling\s*\}\s*from\s*'@/hooks/usePolling';\n", '', content)
    
    if 'usePolling' in content or filename == 'Dashboard.tsx':
        # Add queries import
        imports = ["useStrategies", "useSignals", "useTickerPrices", "useBacktests", "useAccount", "usePerformanceHistory"]
        # check which ones are used
        needed = []
        for imp in imports:
            if imp in content or f'api.get{imp.replace("use", "")}' in content or f'api.listBacktests' in content or filename == 'Dashboard.tsx':
                needed.append(imp)
        
        # also we might need `useQuery` for arbitrary polling?
        # let's just do custom replacements
        
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\(\(\)\s*=>\s*api\.getStrategies\(\)[^;]+;",
            r"const { data: \1 = [], refetch: refetchStrategies } = useStrategies();",
            content
        )
        content = re.sub(
            r"const\s+\{\s*data:\s*strategies,\s*loading,\s*refetch\s*\}\s*=\s*usePolling\(\(\)\s*=>\s*api\.getStrategies\(\)[^;]+;",
            r"const { data: strategies = [], isLoading: loading, refetch } = useStrategies();",
            content
        )
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\(\(\)\s*=>\s*api\.listBacktests\(\)[^;]+;",
            r"const { data: \1 = [], refetch: refetchBacktests } = useBacktests();",
            content
        )
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\(\(\)\s*=>\s*api\.getSignals\('(\w+)'\)[^;]+;",
            r"const { data: \1 = [], refetch: refetchSignals } = useSignals('\2');",
            content
        )
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\(\s*\(\)\s*=>\s*api\.getSignals\('(\w+)'\)[^;]+;",
            r"const { data: \1 = [], refetch: refetchSignals } = useSignals('\2');",
            content
        )
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\(\(\)\s*=>\s*api\.getTickerPrices\(\)[^;]+;",
            r"const { data: \1 = [], refetch: refetchTickers } = useTickerPrices();",
            content
        )
        
        # Dashboard specifically used usePolling
        if filename == 'Dashboard.tsx':
            content = re.sub(
                r"const\s+(\w+)Polling\s*=\s*usePolling[^\)]+\)[^;]+;",
                r"",
                content
            )
            # We already provided Dashboard.tsx rewrite but we reverted.
            # I should just copy my Dashboard.tsx back.
            pass

        # For Optimization.tsx
        content = re.sub(
            r"const\s+statusPolling\s*=\s*usePolling\([\s\S]*?intervalMs:\s*2000,\s*enabled:\s*!!activeRunId,\s*\}\s*\);",
            r"const { data: statusData, refetch: refetchStatus } = useQuery({\n    queryKey: ['optimizationStatus', activeRunId],\n    queryFn: () => api.getOptimizationStatus(activeRunId),\n    enabled: !!activeRunId,\n    refetchInterval: 2000,\n  });",
            content
        )
        
        content = re.sub(r"(\w+)Polling\.data", r"\1", content)
        content = re.sub(r"strategiesPolling\.refetch\(\)", r"refetchStrategies()", content)
        content = re.sub(r"recentRunsPolling\.refetch\(\)", r"refetchBacktests()", content)
        content = re.sub(r"signalsPolling\.refetch\(\)", r"refetchSignals()", content)
        content = re.sub(r"tickersPolling\.refetch\(\)", r"refetchTickers()", content)
        content = re.sub(r"statusPolling\.refetch\(\)", r"refetchStatus()", content)
        
        if 'useQuery(' in content and 'import { useQuery' not in content:
            content = "import { useQuery } from '@tanstack/react-query';\n" + content
            
        imports_str = ", ".join(needed)
        if imports_str and "import { " + imports_str not in content:
            content = f"import {{ {imports_str} }} from '@/hooks/queries';\n" + content

    with open(filepath, 'w') as f:
        f.write(content)
