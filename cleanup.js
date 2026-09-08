const fs = require('fs');

const pages = ['Positions.tsx', 'Optimization.tsx', 'ExecutionLog.tsx', 'BacktestLauncher.tsx', 'BacktestResults.tsx', 'Strategies.tsx'];

pages.forEach(page => {
  const path = 'web/src/pages/' + page;
  if (!fs.existsSync(path)) return;
  
  let text = fs.readFileSync(path, 'utf8');

  // Remove redundant self-assignments
  text = text.replace(/const signals = signals \|\| \[\];\n?/g, '');
  text = text.replace(/const strategies = strategies \|\| \[\];\n?/g, '');
  text = text.replace(/const tickers = tickers \|\| \[\];\n?/g, '');
  text = text.replace(/const recentRuns = recentRuns \|\| \[\];\n?/g, '');
  text = text.replace(/const backtest = backtest \|\| null;\n?/g, '');
  text = text.replace(/const statusData = statusData \|\| null;\n?/g, '');
  
  fs.writeFileSync(path, text);
});
