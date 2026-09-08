const fs = require('fs');
let content = fs.readFileSync('web/src/pages/Dashboard.tsx', 'utf8');

content = content.replace(
  /\(account\.amount \?\? account\.Amount \?\? 0\)/g, 
  "(account.amount ?? (account as any).Amount ?? 0)"
);
content = content.replace(
  /\(account\.currency \?\? account\.Currency\)/g,
  "(account.currency ?? (account as any).Currency)"
);

fs.writeFileSync('web/src/pages/Dashboard.tsx', content);
