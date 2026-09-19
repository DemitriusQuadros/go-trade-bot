import { CompletionContext, CompletionResult, autocompletion } from '@codemirror/autocomplete';

interface Snippet {
  label: string;
  type: 'function' | 'keyword' | 'property';
  info: string;
  apply?: string;
}

// Kept in sync by hand with app/strategies/script/indicators.go's
// bindIndicators - every ind.* closure exposed there needs an entry here,
// with the same argument order/count and return arity, or the editor's
// intellisense drifts from what the sandboxed Lua VM actually accepts.
const INDICATOR_COMPLETIONS: Snippet[] = [
  // Original six.
  { label: 'ind.rsi', type: 'function', info: 'ind.rsi(period) -> number', apply: 'ind.rsi(' },
  { label: 'ind.bollinger', type: 'function', info: 'ind.bollinger(period, stddev_up[, stddev_down]) -> upper, mid, lower', apply: 'ind.bollinger(' },
  { label: 'ind.ema', type: 'function', info: 'ind.ema(period) -> number', apply: 'ind.ema(' },
  { label: 'ind.sma', type: 'function', info: 'ind.sma(period) -> number', apply: 'ind.sma(' },
  { label: 'ind.macd', type: 'function', info: 'ind.macd(fast, slow, signal) -> macd, signal, hist', apply: 'ind.macd(' },
  { label: 'ind.atr', type: 'function', info: 'ind.atr(period) -> number', apply: 'ind.atr(' },

  // Overlap studies.
  { label: 'ind.dema', type: 'function', info: 'ind.dema(period) -> number', apply: 'ind.dema(' },
  { label: 'ind.httrendline', type: 'function', info: 'ind.httrendline() -> number', apply: 'ind.httrendline()' },
  { label: 'ind.kama', type: 'function', info: 'ind.kama(period) -> number', apply: 'ind.kama(' },
  { label: 'ind.ma', type: 'function', info: 'ind.ma(period) -> number (SMA-based)', apply: 'ind.ma(' },
  { label: 'ind.mama', type: 'function', info: 'ind.mama(fast_limit, slow_limit) -> mama, fama', apply: 'ind.mama(' },
  { label: 'ind.midpoint', type: 'function', info: 'ind.midpoint(period) -> number', apply: 'ind.midpoint(' },
  { label: 'ind.midprice', type: 'function', info: 'ind.midprice(period) -> number', apply: 'ind.midprice(' },
  { label: 'ind.sar', type: 'function', info: 'ind.sar(acceleration, maximum) -> number', apply: 'ind.sar(' },
  { label: 'ind.sarext', type: 'function', info: 'ind.sarext(start_value, offset_on_reverse, accel_init_long, accel_long, accel_max_long, accel_init_short, accel_short, accel_max_short) -> number', apply: 'ind.sarext(' },
  { label: 'ind.t3', type: 'function', info: 'ind.t3(period, v_factor) -> number', apply: 'ind.t3(' },
  { label: 'ind.tema', type: 'function', info: 'ind.tema(period) -> number', apply: 'ind.tema(' },
  { label: 'ind.trima', type: 'function', info: 'ind.trima(period) -> number', apply: 'ind.trima(' },
  { label: 'ind.wma', type: 'function', info: 'ind.wma(period) -> number', apply: 'ind.wma(' },

  // Momentum indicators.
  { label: 'ind.adx', type: 'function', info: 'ind.adx(period) -> number', apply: 'ind.adx(' },
  { label: 'ind.adxr', type: 'function', info: 'ind.adxr(period) -> number', apply: 'ind.adxr(' },
  { label: 'ind.apo', type: 'function', info: 'ind.apo(fast_period, slow_period) -> number (SMA-based)', apply: 'ind.apo(' },
  { label: 'ind.aroon', type: 'function', info: 'ind.aroon(period) -> down, up', apply: 'ind.aroon(' },
  { label: 'ind.aroonosc', type: 'function', info: 'ind.aroonosc(period) -> number', apply: 'ind.aroonosc(' },
  { label: 'ind.bop', type: 'function', info: 'ind.bop() -> number', apply: 'ind.bop()' },
  { label: 'ind.cmo', type: 'function', info: 'ind.cmo(period) -> number', apply: 'ind.cmo(' },
  { label: 'ind.cci', type: 'function', info: 'ind.cci(period) -> number', apply: 'ind.cci(' },
  { label: 'ind.dx', type: 'function', info: 'ind.dx(period) -> number', apply: 'ind.dx(' },
  { label: 'ind.macdext', type: 'function', info: 'ind.macdext(fast_period, slow_period, signal_period) -> macd, signal, hist (SMA-based)', apply: 'ind.macdext(' },
  { label: 'ind.macdfix', type: 'function', info: 'ind.macdfix(signal_period) -> macd, signal, hist', apply: 'ind.macdfix(' },
  { label: 'ind.minusdi', type: 'function', info: 'ind.minusdi(period) -> number', apply: 'ind.minusdi(' },
  { label: 'ind.minusdm', type: 'function', info: 'ind.minusdm(period) -> number', apply: 'ind.minusdm(' },
  { label: 'ind.mfi', type: 'function', info: 'ind.mfi(period) -> number', apply: 'ind.mfi(' },
  { label: 'ind.mom', type: 'function', info: 'ind.mom(period) -> number', apply: 'ind.mom(' },
  { label: 'ind.plusdi', type: 'function', info: 'ind.plusdi(period) -> number', apply: 'ind.plusdi(' },
  { label: 'ind.plusdm', type: 'function', info: 'ind.plusdm(period) -> number', apply: 'ind.plusdm(' },
  { label: 'ind.ppo', type: 'function', info: 'ind.ppo(fast_period, slow_period) -> number (SMA-based)', apply: 'ind.ppo(' },
  { label: 'ind.rocp', type: 'function', info: 'ind.rocp(period) -> number', apply: 'ind.rocp(' },
  { label: 'ind.roc', type: 'function', info: 'ind.roc(period) -> number', apply: 'ind.roc(' },
  { label: 'ind.rocr', type: 'function', info: 'ind.rocr(period) -> number', apply: 'ind.rocr(' },
  { label: 'ind.rocr100', type: 'function', info: 'ind.rocr100(period) -> number', apply: 'ind.rocr100(' },
  { label: 'ind.stoch', type: 'function', info: 'ind.stoch(fast_k_period, slow_k_period, slow_d_period) -> k, d (SMA-based)', apply: 'ind.stoch(' },
  { label: 'ind.stochf', type: 'function', info: 'ind.stochf(fast_k_period, fast_d_period) -> k, d (SMA-based)', apply: 'ind.stochf(' },
  { label: 'ind.stochrsi', type: 'function', info: 'ind.stochrsi(period, fast_k_period, fast_d_period) -> k, d (SMA-based)', apply: 'ind.stochrsi(' },
  { label: 'ind.trix', type: 'function', info: 'ind.trix(period) -> number', apply: 'ind.trix(' },
  { label: 'ind.ultosc', type: 'function', info: 'ind.ultosc(period1, period2, period3) -> number', apply: 'ind.ultosc(' },
  { label: 'ind.willr', type: 'function', info: 'ind.willr(period) -> number', apply: 'ind.willr(' },

  // Volume indicators.
  { label: 'ind.ad', type: 'function', info: 'ind.ad() -> number', apply: 'ind.ad()' },
  { label: 'ind.adosc', type: 'function', info: 'ind.adosc(fast_period, slow_period) -> number', apply: 'ind.adosc(' },
  { label: 'ind.obv', type: 'function', info: 'ind.obv() -> number', apply: 'ind.obv()' },

  // Volatility indicators.
  { label: 'ind.natr', type: 'function', info: 'ind.natr(period) -> number', apply: 'ind.natr(' },
  { label: 'ind.trange', type: 'function', info: 'ind.trange() -> number', apply: 'ind.trange()' },

  // Price transform.
  { label: 'ind.avgprice', type: 'function', info: 'ind.avgprice() -> number', apply: 'ind.avgprice()' },
  { label: 'ind.medprice', type: 'function', info: 'ind.medprice() -> number', apply: 'ind.medprice()' },
  { label: 'ind.typprice', type: 'function', info: 'ind.typprice() -> number', apply: 'ind.typprice()' },
  { label: 'ind.wclprice', type: 'function', info: 'ind.wclprice() -> number', apply: 'ind.wclprice()' },

  // Cycle indicators (Hilbert Transform).
  { label: 'ind.htdcperiod', type: 'function', info: 'ind.htdcperiod() -> number', apply: 'ind.htdcperiod()' },
  { label: 'ind.htdcphase', type: 'function', info: 'ind.htdcphase() -> number', apply: 'ind.htdcphase()' },
  { label: 'ind.htphasor', type: 'function', info: 'ind.htphasor() -> inphase, quadrature', apply: 'ind.htphasor()' },
  { label: 'ind.htsine', type: 'function', info: 'ind.htsine() -> sine, leadsine', apply: 'ind.htsine()' },
  { label: 'ind.httrendmode', type: 'function', info: 'ind.httrendmode() -> number', apply: 'ind.httrendmode()' },

  // Statistic functions.
  { label: 'ind.linearreg', type: 'function', info: 'ind.linearreg(period) -> number', apply: 'ind.linearreg(' },
  { label: 'ind.linearregangle', type: 'function', info: 'ind.linearregangle(period) -> number', apply: 'ind.linearregangle(' },
  { label: 'ind.linearregintercept', type: 'function', info: 'ind.linearregintercept(period) -> number', apply: 'ind.linearregintercept(' },
  { label: 'ind.linearregslope', type: 'function', info: 'ind.linearregslope(period) -> number', apply: 'ind.linearregslope(' },
  { label: 'ind.stddev', type: 'function', info: 'ind.stddev(period, nb_dev) -> number', apply: 'ind.stddev(' },
  { label: 'ind.tsf', type: 'function', info: 'ind.tsf(period) -> number', apply: 'ind.tsf(' },
  { label: 'ind.var', type: 'function', info: 'ind.var(period) -> number', apply: 'ind.var(' },

  // Window-math functions.
  { label: 'ind.max', type: 'function', info: 'ind.max(period) -> number', apply: 'ind.max(' },
  { label: 'ind.maxindex', type: 'function', info: 'ind.maxindex(period) -> number (bar index)', apply: 'ind.maxindex(' },
  { label: 'ind.min', type: 'function', info: 'ind.min(period) -> number', apply: 'ind.min(' },
  { label: 'ind.minindex', type: 'function', info: 'ind.minindex(period) -> number (bar index)', apply: 'ind.minindex(' },
  { label: 'ind.minmax', type: 'function', info: 'ind.minmax(period) -> min, max', apply: 'ind.minmax(' },
  { label: 'ind.minmaxindex', type: 'function', info: 'ind.minmaxindex(period) -> minidx, maxidx (bar indexes)', apply: 'ind.minmaxindex(' },
  { label: 'ind.sum', type: 'function', info: 'ind.sum(period) -> number', apply: 'ind.sum(' },
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
