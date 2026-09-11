import { CompletionContext, CompletionResult, autocompletion } from '@codemirror/autocomplete';

interface Snippet {
  label: string;
  type: 'function' | 'keyword' | 'property';
  info: string;
  apply?: string;
}

const INDICATOR_COMPLETIONS: Snippet[] = [
  { label: 'ind.rsi', type: 'function', info: 'ind.rsi(period) -> number', apply: 'ind.rsi(' },
  { label: 'ind.bollinger', type: 'function', info: 'ind.bollinger(period, stddev_up[, stddev_down]) -> upper, mid, lower', apply: 'ind.bollinger(' },
  { label: 'ind.ema', type: 'function', info: 'ind.ema(period) -> number', apply: 'ind.ema(' },
  { label: 'ind.sma', type: 'function', info: 'ind.sma(period) -> number', apply: 'ind.sma(' },
  { label: 'ind.macd', type: 'function', info: 'ind.macd(fast, slow, signal) -> macd, signal, hist', apply: 'ind.macd(' },
  { label: 'ind.atr', type: 'function', info: 'ind.atr(period) -> number', apply: 'ind.atr(' },
];

const DEBUG_COMPLETIONS: Snippet[] = [
  { label: 'debug.log', type: 'function', info: 'debug.log(label, value) - records a trace entry (no-op outside preview/backtest)', apply: 'debug.log(' },
];

const HOOK_COMPLETIONS: Snippet[] = [
  { label: 'before', type: 'keyword', info: 'function before(ctx) - called at cycle start', apply: 'function before(ctx)\n  \nend' },
  { label: 'should_long', type: 'keyword', info: 'function should_long(ctx) -> bool', apply: 'function should_long(ctx)\n  \nend' },
  { label: 'go_long', type: 'keyword', info: 'function go_long(ctx) -> signal table', apply: 'function go_long(ctx)\n  \nend' },
  { label: 'should_short', type: 'keyword', info: 'function should_short(ctx) -> bool', apply: 'function should_short(ctx)\n  \nend' },
  { label: 'go_short', type: 'keyword', info: 'function go_short(ctx) -> signal table', apply: 'function go_short(ctx)\n  \nend' },
  { label: 'update_position', type: 'keyword', info: 'function update_position(ctx) -> signal table or nil', apply: 'function update_position(ctx)\n  \nend' },
  { label: 'after', type: 'keyword', info: 'function after(ctx) - called at cycle end', apply: 'function after(ctx)\n  \nend' },
  { label: 'terminate', type: 'keyword', info: 'function terminate(ctx) - called once when a strategy is disabled', apply: 'function terminate(ctx)\n  \nend' },
];

const CTX_COMPLETIONS: Snippet[] = [
  { label: 'ctx.symbol', type: 'property', info: 'string - the monitored symbol, e.g. "BTCUSDT"' },
  { label: 'ctx.timeframe', type: 'property', info: 'string - e.g. "15m"' },
  { label: 'ctx.mode', type: 'property', info: 'string - execution mode ("dryrun" | "paper" | "live" | "backtest")' },
  { label: 'ctx.price', type: 'property', info: 'number - current close price' },
  { label: 'ctx.candles', type: 'property', info: 'array of {o,h,l,c,v,t} - candle window for this cycle' },
  { label: 'ctx.position', type: 'property', info: 'nil, or {symbol, entry_price, quantity, stop_loss_price, opened_at}' },
  { label: 'ctx.account', type: 'property', info: '{available} - available account balance' },
  { label: 'ctx.config', type: 'property', info: "table - this strategy's Configuration JSON" },
];

const ALL_COMPLETIONS: Snippet[] = [
  ...INDICATOR_COMPLETIONS,
  ...DEBUG_COMPLETIONS,
  ...HOOK_COMPLETIONS,
  ...CTX_COMPLETIONS,
];

function luaCompletionSource(context: CompletionContext): CompletionResult | null {
  const word = context.matchBefore(/[\w.]*/);
  if (!word || (word.from === word.to && !context.explicit)) return null;

  return {
    from: word.from,
    options: ALL_COMPLETIONS.map((c) => ({
      label: c.label,
      type: c.type,
      info: c.info,
      apply: c.apply ?? c.label,
    })),
    validFor: /^[\w.]*$/,
  };
}

export const luaAutocompletion = autocompletion({ override: [luaCompletionSource] });
