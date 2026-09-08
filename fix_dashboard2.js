const fs = require('fs');

let content = fs.readFileSync('web/src/pages/Dashboard.tsx', 'utf8');

content = content.replace(
  /usePerformanceHistory/g, 
  "usePerformanceSnapshots"
);

content = content.replace(
  /periodStart: p\.snapshot_date,/,
  "periodStart: p.snapshot_date,"
);
content = content.replace(
  /profit: p\.realized_pnl,/,
  "profit: p.realized_pnl,"
);
content = content.replace(
  /\.sort\(\(a, b\) => a\.time - b\.time\);/g,
  ".sort((a, b) => new Date(a.periodStart).getTime() - new Date(b.periodStart).getTime());"
);

fs.writeFileSync('web/src/pages/Dashboard.tsx', content);
