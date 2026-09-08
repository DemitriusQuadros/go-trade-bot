import os
import re

pages_dir = 'web/src/pages'
pages = ['Positions.tsx', 'Optimization.tsx', 'ExecutionLog.tsx', 'BacktestLauncher.tsx', 'BacktestResults.tsx', 'Strategies.tsx']

for p in pages:
    file = os.path.join(pages_dir, p)
    if not os.path.exists(file): continue
    with open(file, 'r') as f:
        content = f.read()

    # Imports
    content = re.sub(r"import\s*\{\s*usePolling\s*\}\s*from\s*'@/hooks/usePolling';\n?", '', content)
    
    needed = set()
    
    if 'api.getStrategies(' in content:
        needed.add('useStrategies')
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\([\s\S]*?api\.getStrategies\(\)[\s\S]*?\);",
            r"const { data: \1 = [], refetch: refetch\1 } = useStrategies();",
            content
        )
        content = re.sub(
            r"const\s+\{\s*data:\s*strategies,\s*loading,\s*refetch\s*\}\s*=\s*usePolling\([\s\S]*?api\.getStrategies\(\{[\s\S]*?\);",
            r"const { data: strategies = [], isLoading: loading, refetch } = useStrategies();",
            content
        )
    
    if 'api.listBacktests(' in content:
        needed.add('useBacktests')
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\([\s\S]*?api\.listBacktests\(\)[\s\S]*?\);",
            r"const { data: \1 = [], refetch: refetch\1 } = useBacktests();",
            content
        )

    if 'api.getSignals(' in content:
        needed.add('useSignals')
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\([\s\S]*?api\.getSignals\('([^']+)'\)[\s\S]*?\);",
            r"const { data: \1 = [], refetch: refetch\1 } = useSignals('\2');",
            content
        )
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\([\s\S]*?api\.getSignals\(\)[\s\S]*?\);",
            r"const { data: \1 = [], refetch: refetch\1 } = useSignals();",
            content
        )
        
    if 'api.getTickerPrices(' in content:
        needed.add('useTickerPrices')
        content = re.sub(
            r"const\s+(\w+)Polling\s*=\s*usePolling\([\s\S]*?api\.getTickerPrices\(\)[\s\S]*?\);",
            r"const { data: \1 = [], refetch: refetch\1 } = useTickerPrices();",
            content
        )

    if 'api.getOptimizationStatus(' in content:
        needed.add('useQuery')
        content = re.sub(
            r"const\s+statusPolling\s*=\s*usePolling\([\s\S]*?api\.getOptimizationStatus[\s\S]*?intervalMs:\s*2000,\s*enabled:\s*!!activeRunId,\s*\}\s*\);",
            r"const { data: statusData, refetch: refetchStatus } = useQuery({ queryKey: ['optStatus', activeRunId], queryFn: () => api.getOptimizationStatus(activeRunId, {}), enabled: !!activeRunId, refetchInterval: 2000 });",
            content
        )

    if 'api.getBacktest(' in content:
        needed.add('useBacktest')
        content = re.sub(
            r"const\s+backtestPolling\s*=\s*usePolling\([\s\S]*?api\.getBacktest\(Number\(id\)\)[\s\S]*?\);",
            r"const { data: backtestData, refetch: refetchBacktest, isLoading: isBacktestLoading } = useBacktest(Number(id));",
            content
        )

    content = re.sub(r"(\w+)Polling\.data", r"\1", content)
    content = re.sub(r"(\w+)Polling\.refetch\(\)", r"refetch\1()", content)
    content = re.sub(r"(\w+)Polling\.loading", r"(!\1 || \1.length === 0)", content)
    content = re.sub(r"refetchsignals\(\)", r"refetchSignals()", content)
    content = re.sub(r"statusPolling\.refetch\(\)", r"refetchStatus()", content)
    content = re.sub(r"statusPolling\.loading", r"(!statusData)", content)
    content = re.sub(r"backtestPolling\.loading", r"isBacktestLoading", content)
    content = re.sub(r"backtestPolling\.refetch\(\)", r"refetchBacktest()", content)

    # Safe TS fixes for implicit any only on parameters using =>
    content = re.sub(r"\(s\s*=>", r"(s: any) =>", content)
    content = re.sub(r"\(sig\s*=>", r"(sig: any) =>", content)
    content = re.sub(r"\(t\s*=>", r"(t: any) =>", content)
    content = re.sub(r"\(sum,\s*o\)\s*=>", r"(sum: any, o: any) =>", content)
    content = re.sub(r"\(p\s*=>", r"(p: any) =>", content)
    content = re.sub(r"\(pos\s*=>", r"(pos: any) =>", content)
    content = re.sub(r"\(sym\s*=>", r"(sym: any) =>", content)
    content = re.sub(r"\(strat\s*=>", r"(strat: any) =>", content)
    content = re.sub(r"\(run\s*=>", r"(run: any) =>", content)
    content = re.sub(r"\(ord\s*=>", r"(ord: any) =>", content)

    content = re.sub(r"statusData\?\.status", r"(statusData as any)?.status", content)
    content = re.sub(r"statusData\?\.progress", r"(statusData as any)?.progress", content)
    content = re.sub(r"statusData\?\.total_combinations", r"(statusData as any)?.total_combinations", content)

    # Styling
    content = content.replace('bg-slate-950', 'bg-black')
    content = content.replace('bg-slate-900/40', 'bg-black/40')
    content = content.replace('bg-slate-900/50', 'bg-black/50')
    content = content.replace('bg-slate-900', 'bg-green-950/20')
    content = content.replace('border-slate-800/80', 'border-green-900/30')
    content = content.replace('border-slate-800', 'border-green-900/30')
    content = content.replace('text-slate-100', 'text-green-500')
    content = content.replace('text-slate-200', 'text-green-400')
    content = content.replace('text-slate-300', 'text-green-600')
    content = content.replace('text-slate-400', 'text-green-700')
    content = content.replace('text-slate-500', 'text-green-800')
    content = content.replace('bg-blue-600', 'bg-green-800')
    content = content.replace('text-blue-400', 'text-green-500')
    content = content.replace('bg-slate-800', 'bg-green-900/30')
    content = content.replace('border-b border-slate-800', 'border-b border-green-900/30')
    content = content.replace('hover:bg-slate-800/50', 'hover:bg-green-900/30')

    queries_imp = [h for h in needed if h != 'useQuery']
    if queries_imp:
        content = f"import {{ {', '.join(queries_imp)} }} from '@/hooks/queries';\n" + content
    if 'useQuery' in needed:
        content = "import { useQuery } from '@tanstack/react-query';\n" + content

    with open(file, 'w') as f:
        f.write(content)
