const fs = require('fs');
let content = fs.readFileSync('web/src/pages/Dashboard.tsx', 'utf8');

content = content.replace(
  /\$\{ticker\.Price\.toLocaleString/g, 
  "${(ticker.Price ?? 0).toLocaleString"
);

fs.writeFileSync('web/src/pages/Dashboard.tsx', content);
