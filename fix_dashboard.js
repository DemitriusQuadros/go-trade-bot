const fs = require('fs');

let content = fs.readFileSync('web/src/pages/Dashboard.tsx', 'utf8');

content = content.replace(
  /const entryOrder = signal\.orders\?\.\[\?\];/, 
  "const entryOrder = signal.orders?.[0];"
);
content = content.replace(
  /const entryOrder = signal\.orders\?\.find\(\(o\) => o\.type === 'entry' && o\.status === 'filled'\);/,
  "const entryOrder = signal.orders?.[0];"
);
content = content.replace(
  /entryPrice = entryOrder\.price;/,
  "entryPrice = entryOrder.entry_price;"
);
content = content.replace(
  /unrealizedPnL = signal\.side === 'long' \? valueDiff : -valueDiff;/,
  "unrealizedPnL = valueDiff;" // default long if side isn't in DTO
);
content = content.replace(
  /const activeStrategiesCount = strategies\.filter\(\(s\) => s\.status === 'active'\)\.length;/,
  "const activeStrategiesCount = strategies.filter((s) => s.status === 'productive').length;"
);

content = content.replace(
  /time: new Date\(p\.timestamp\)\.getTime\(\) \/ 1000,/,
  "periodStart: p.snapshot_date,"
);
content = content.replace(
  /value: p\.realized_pnl,/,
  "profit: p.realized_pnl,"
);

fs.writeFileSync('web/src/pages/Dashboard.tsx', content);
