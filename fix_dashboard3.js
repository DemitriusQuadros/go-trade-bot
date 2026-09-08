const fs = require('fs');
let content = fs.readFileSync('web/src/pages/Dashboard.tsx', 'utf8');

// Fix 1: account.amount might be account.Amount
content = content.replace(
  /account\.amount/g, 
  "(account.amount ?? account.Amount ?? 0)"
);
content = content.replace(
  /account\.currency/g,
  "(account.currency ?? account.Currency)"
);

// Fix 2: Remove useTickerPrices if it throws 400 on empty
content = content.replace(
  /const { data: baseTickers = \[\], isLoading: isTickersLoading, refetch: refetchTickers } = useTickerPrices\(\);/,
  "const baseTickers: {Symbol: string, Price: number}[] = []; const refetchTickers = () => {};"
);

fs.writeFileSync('web/src/pages/Dashboard.tsx', content);
