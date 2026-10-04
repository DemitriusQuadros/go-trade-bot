package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go-trade-bot/app/entities"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/cooldown"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/marketstatus"
)

// Market watcher (agents-platform C-01 §3): cmd/agent keeps one 1m kline
// subscription per symbol watched by any non-paused agent's Market rules,
// a rolling buffer of closed candles per symbol, and evaluates every rule on
// each closed candle. It only reads market data (cmd/agent's exchange client
// is the read-only one) and only ever enqueues agent:run tasks.

const (
	marketInterval = "1m"
	// baselineCandles is the prior-24h baseline at 1m resolution.
	baselineCandles = 24 * 60
	// minBaselineCandles is the least history volatility_spike accepts
	// before the full 24h baseline has accumulated.
	minBaselineCandles = 60
	// maxBackfill is ListKline's per-call ceiling on Binance.
	maxBackfill = 1000
)

// KlineFeed is one symbol's closed-candle stream (*feed.LiveFeed).
type KlineFeed interface {
	Next() (exchange.Candle, bool)
	Close() error
}

// FeedFactory opens a 1m closed-candle feed for symbol.
type FeedFactory func(symbol string) (KlineFeed, error)

// KlineLister backfills recent candles (the read-only exchange client).
type KlineLister interface {
	ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error)
}

// ATRProvider is the internal/indicators ATR (IndicatorProvider satisfies it).
type ATRProvider interface {
	ATR(candles []exchange.Candle, period int) []float64
}

// GaugeMetrics is satisfied by *metrics.MetricsCollector.
type GaugeMetrics interface {
	IncrementCounter(name string, labels map[string]string)
	SetGauge(name string, labels map[string]string, value float64)
}

type watchedRule struct {
	agent      entities.Agent
	rule       entities.MarketRule
	strategyID *uint // exactly one bound strategy monitors the symbol
	monitored  bool  // at least one bound strategy monitors the symbol
}

type symbolWatch struct {
	symbol string
	feed   KlineFeed
	done   chan struct{}

	mu      sync.Mutex
	candles []exchange.Candle
	maxLen  int
}

// MarketWatcher is an fx lifecycle component (Start/Stop) in cmd/agent.
type MarketWatcher struct {
	agents     TriggerAgentSource
	strategies StrategyGetter
	settings   SettingsReader
	klines     KlineLister
	newFeed    FeedFactory
	atr        ATRProvider
	cooldowns  cooldown.Store
	enq        agentworker.TaskEnqueuer
	status     marketstatus.Writer // may be nil
	metrics    GaugeMetrics        // may be nil
	interval   time.Duration

	// Now is the clock (tests inject a fake one).
	Now func() time.Time

	mu    sync.Mutex
	subs  map[string]*symbolWatch
	rules map[string][]watchedRule

	processed atomic.Int64
	wg        sync.WaitGroup
	cancel    context.CancelFunc
}

// NewMarketWatcher builds a watcher that re-syncs every interval.
func NewMarketWatcher(agents TriggerAgentSource, strategies StrategyGetter, settings SettingsReader, klines KlineLister,
	newFeed FeedFactory, atr ATRProvider, cooldowns cooldown.Store, enq agentworker.TaskEnqueuer,
	status marketstatus.Writer, m GaugeMetrics, interval time.Duration) *MarketWatcher {
	return &MarketWatcher{
		agents: agents, strategies: strategies, settings: settings, klines: klines, newFeed: newFeed, atr: atr,
		cooldowns: cooldowns, enq: enq, status: status, metrics: m, interval: interval, Now: time.Now,
		subs: map[string]*symbolWatch{}, rules: map[string][]watchedRule{},
	}
}

// Start runs a first Sync and then re-syncs every interval until Stop.
func (w *MarketWatcher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.Sync(ctx)
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.Sync(ctx)
			}
		}
	}()
}

// Stop closes every subscription and waits for the goroutines.
func (w *MarketWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Lock()
	for sym, sw := range w.subs {
		_ = sw.feed.Close()
		delete(w.subs, sym)
	}
	w.rules = map[string][]watchedRule{}
	w.mu.Unlock()
	w.wg.Wait()
}

// CandlesProcessed is the number of closed candles evaluated so far (tests).
func (w *MarketWatcher) CandlesProcessed() int64 { return w.processed.Load() }

// Symbols returns the currently subscribed symbols, sorted.
func (w *MarketWatcher) Symbols() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.subs))
	for s := range w.subs {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func requiredCandles(r entities.MarketRule) int {
	if r.Kind == entities.MarketKindVolatilitySpike {
		return r.WindowMinutes + baselineCandles + 1
	}
	return r.WindowMinutes + 1
}

// desiredRules computes symbol -> rules from every non-paused agent (nothing
// while the kill switch is on). ok=false when the state could not be read -
// the caller then keeps the previous state (firing still re-checks the kill
// switch).
func (w *MarketWatcher) desiredRules(ctx context.Context) (map[string][]watchedRule, bool) {
	s, err := w.settings.Get(ctx)
	if err != nil {
		log.Printf("market watcher: cannot read settings: %v", err)
		return nil, false
	}
	out := map[string][]watchedRule{}
	if s != nil && s.AgentsPaused {
		return out, true
	}
	agents, err := w.agents.ListAgents(ctx)
	if err != nil {
		log.Printf("market watcher: cannot list agents: %v", err)
		return nil, false
	}
	for _, a := range agents {
		rules := a.ParsedTriggers().Market
		if a.Paused || len(rules) == 0 {
			continue
		}
		monitors := map[string][]uint{} // symbol -> bound strategies trading it
		if bindings, err := w.agents.ListBindingsByAgent(ctx, a.ID); err == nil {
			for _, b := range bindings {
				strat, err := w.strategies.GetByID(ctx, b.StrategyID)
				if err != nil {
					continue
				}
				for _, sym := range strat.MonitoredSymbols {
					monitors[sym] = append(monitors[sym], strat.ID)
				}
			}
		}
		for _, r := range rules {
			if r.Symbol == "" || r.WindowMinutes < entities.MinMarketWindowMinutes || r.WindowMinutes > entities.MaxMarketWindowMinutes {
				continue
			}
			wr := watchedRule{agent: a, rule: r, monitored: len(monitors[r.Symbol]) > 0}
			if ids := monitors[r.Symbol]; len(ids) == 1 {
				id := ids[0]
				wr.strategyID = &id
			}
			out[r.Symbol] = append(out[r.Symbol], wr)
		}
	}
	return out, true
}

// Sync recomputes the watched set: opens (and backfills) subscriptions for
// new symbols, closes those no longer watched, updates the gauge and writes
// the status snapshot.
func (w *MarketWatcher) Sync(ctx context.Context) {
	desired, ok := w.desiredRules(ctx)
	if ok {
		w.mu.Lock()
		w.rules = desired
		var toClose []*symbolWatch
		for sym, sw := range w.subs {
			if _, keep := desired[sym]; !keep {
				toClose = append(toClose, sw)
				delete(w.subs, sym)
			}
		}
		var toOpen []string
		for sym, rules := range desired {
			need := 0
			for _, r := range rules {
				if n := requiredCandles(r.rule); n > need {
					need = n
				}
			}
			if sw, exists := w.subs[sym]; exists {
				sw.mu.Lock()
				sw.maxLen = need
				sw.mu.Unlock()
			} else {
				toOpen = append(toOpen, sym)
			}
		}
		w.mu.Unlock()
		for _, sw := range toClose {
			_ = sw.feed.Close()
			log.Printf("market watcher: unsubscribed %s", sw.symbol)
		}
		sort.Strings(toOpen)
		for _, sym := range toOpen {
			w.open(ctx, sym)
		}
	}
	w.mu.Lock()
	n := len(w.subs)
	w.mu.Unlock()
	if w.metrics != nil {
		w.metrics.SetGauge(MetricMarketSubscriptions, map[string]string{}, float64(n))
	}
	w.writeStatus(ctx)
}

func (w *MarketWatcher) maxLenFor(sym string) int {
	need := 0
	for _, r := range w.rules[sym] {
		if n := requiredCandles(r.rule); n > need {
			need = n
		}
	}
	return need
}

func (w *MarketWatcher) open(ctx context.Context, sym string) {
	w.mu.Lock()
	maxLen := w.maxLenFor(sym)
	w.mu.Unlock()
	sw := &symbolWatch{symbol: sym, maxLen: maxLen, done: make(chan struct{})}
	sw.candles = w.backfill(ctx, sym, maxLen, time.Time{})
	f, err := w.newFeed(sym)
	if err != nil {
		log.Printf("market watcher: cannot subscribe %s (retrying next sync): %v", sym, err)
		return
	}
	sw.feed = f
	w.mu.Lock()
	if _, still := w.rules[sym]; !still {
		w.mu.Unlock()
		_ = f.Close()
		return
	}
	w.subs[sym] = sw
	w.mu.Unlock()
	log.Printf("market watcher: subscribed %s 1m (%d candles backfilled)", sym, len(sw.candles))
	w.wg.Add(1)
	go w.consume(sw)
}

// backfill returns up to want recent CLOSED candles opened after `after`
// (oldest first); the forming candle is dropped.
func (w *MarketWatcher) backfill(ctx context.Context, sym string, want int, after time.Time) []exchange.Candle {
	if want <= 0 {
		return nil
	}
	limit := want + 1
	if limit > maxBackfill {
		limit = maxBackfill
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	got, err := w.klines.ListKline(cctx, sym, marketInterval, limit)
	if err != nil {
		log.Printf("market watcher: backfill %s failed: %v", sym, err)
		return nil
	}
	now := w.Now()
	out := make([]exchange.Candle, 0, len(got))
	for _, c := range got {
		if c.OpenTime.Add(time.Minute).After(now) {
			continue // still forming
		}
		if !after.IsZero() && !c.OpenTime.After(after) {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTime.Before(out[j].OpenTime) })
	return out
}

func (w *MarketWatcher) consume(sw *symbolWatch) {
	defer w.wg.Done()
	for {
		c, ok := sw.feed.Next()
		if !ok {
			return
		}
		w.onCandle(sw, c)
	}
}

// onCandle appends a closed candle (backfilling a reconnect gap first) and
// evaluates the symbol's rules.
func (w *MarketWatcher) onCandle(sw *symbolWatch, c exchange.Candle) {
	defer w.processed.Add(1)
	sw.mu.Lock()
	if n := len(sw.candles); n > 0 {
		last := sw.candles[n-1].OpenTime
		if !c.OpenTime.After(last) {
			sw.mu.Unlock()
			return // duplicate / stale
		}
		if gap := int(c.OpenTime.Sub(last)/time.Minute) - 1; gap > 0 {
			for _, g := range w.backfill(context.Background(), sw.symbol, gap+1, last) {
				if g.OpenTime.Before(c.OpenTime) {
					sw.candles = append(sw.candles, g)
				}
			}
		}
	}
	sw.candles = append(sw.candles, c)
	if sw.maxLen > 0 && len(sw.candles) > sw.maxLen {
		sw.candles = append([]exchange.Candle(nil), sw.candles[len(sw.candles)-sw.maxLen:]...)
	}
	candles := append([]exchange.Candle(nil), sw.candles...)
	sw.mu.Unlock()

	w.mu.Lock()
	rules := append([]watchedRule(nil), w.rules[sw.symbol]...)
	w.mu.Unlock()
	for _, r := range rules {
		w.evaluate(r, candles)
	}
}

// MarketObservation is a rule evaluation result.
type MarketObservation struct {
	Fired      bool
	Observed   float64 // pct_move: signed % change; volatility_spike: ATR ratio
	Price      float64
	At         time.Time
	Evaluated  bool // enough history to evaluate
	WindowATR  float64
	BaseATR    float64
	BaseLength int
}

// EvaluateMarketRule evaluates r over closed 1m candles (oldest first).
//   - pct_move: (close_now / close_{now-window} - 1) * 100, fires on its
//     absolute value >= threshold_pct.
//   - volatility_spike: ATR over the window divided by the ATR over the
//     prior 24h (or the history available, at least 60 candles) at the same
//     1m resolution, fires when >= multiplier.
func EvaluateMarketRule(r entities.MarketRule, candles []exchange.Candle, atr ATRProvider) MarketObservation {
	n := len(candles)
	win := r.WindowMinutes
	if n < win+1 || win < 1 {
		return MarketObservation{}
	}
	last := candles[n-1]
	obs := MarketObservation{Price: last.Close, At: last.OpenTime.Add(time.Minute), Evaluated: true}
	switch r.Kind {
	case entities.MarketKindPctMove:
		ref := candles[n-1-win].Close
		if ref <= 0 {
			return MarketObservation{}
		}
		obs.Observed = (last.Close/ref - 1) * 100
		obs.Fired = r.ThresholdPct > 0 && math.Abs(obs.Observed) >= r.ThresholdPct
	case entities.MarketKindVolatilitySpike:
		if atr == nil {
			return MarketObservation{}
		}
		base := n - win - 1
		if base > baselineCandles {
			base = baselineCandles
		}
		if base < minBaselineCandles || base < win {
			return MarketObservation{}
		}
		windowSeries := atr.ATR(candles[n-win-1:], win)
		baseSeries := atr.ATR(candles[n-win-1-base:n-win], base)
		if len(windowSeries) == 0 || len(baseSeries) == 0 {
			return MarketObservation{}
		}
		obs.WindowATR, obs.BaseATR, obs.BaseLength = windowSeries[len(windowSeries)-1], baseSeries[len(baseSeries)-1], base
		if !(obs.BaseATR > 0) || math.IsNaN(obs.WindowATR) {
			return MarketObservation{}
		}
		obs.Observed = obs.WindowATR / obs.BaseATR
		obs.Fired = r.Multiplier > 0 && obs.Observed >= r.Multiplier
	default:
		return MarketObservation{}
	}
	return obs
}

// CooldownKeyMarket is the Redis cooldown key of a market rule.
func CooldownKeyMarket(agentID uint, ruleID string) string {
	return fmt.Sprintf("agent-market:%d:%s", agentID, ruleID)
}

func (w *MarketWatcher) evaluate(r watchedRule, candles []exchange.Candle) {
	obs := EvaluateMarketRule(r.rule, candles, w.atr)
	if !obs.Fired {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Re-check the kill switch at fire time (the rule set is up to one sync
	// interval old); unreadable = on.
	if s, err := w.settings.Get(ctx); err != nil || (s != nil && s.AgentsPaused) {
		return
	}
	claimed, err := w.cooldowns.Claim(ctx, CooldownKeyMarket(r.agent.ID, r.rule.ID), r.rule.EffectiveCooldown())
	if err != nil {
		log.Printf("market watcher: cooldown for agent %d rule %s: %v", r.agent.ID, r.rule.ID, err)
		return
	}
	if !claimed {
		w.count(MetricTriggersSuppressed)
		return
	}
	detail := map[string]any{
		"rule_id":                     r.rule.ID,
		"symbol":                      r.rule.Symbol,
		"kind":                        r.rule.Kind,
		"window_minutes":              r.rule.WindowMinutes,
		"price":                       obs.Price,
		"at":                          obs.At.UTC().Format(time.RFC3339),
		"monitored_by_bound_strategy": r.monitored,
	}
	if r.rule.Kind == entities.MarketKindVolatilitySpike {
		detail["observed_multiplier"] = round4(obs.Observed)
		detail["threshold"] = r.rule.Multiplier
	} else {
		detail["observed_pct"] = round4(obs.Observed)
		detail["threshold"] = r.rule.ThresholdPct
	}
	b, _ := json.Marshal(detail)
	if _, err := agentworker.EnqueueRunWith(ctx, w.enq, agentworker.RunPayload{
		AgentID: r.agent.ID, Trigger: entities.TriggerKindMarket, Detail: b, StrategyID: r.strategyID,
	}); err != nil {
		log.Printf("market watcher: enqueue run for agent %d rule %s failed: %v", r.agent.ID, r.rule.ID, err)
		return
	}
	w.count(MetricTriggersFired)
	log.Printf("market watcher: %s %s %.4f -> agent %d (%s)", r.rule.Symbol, r.rule.Kind, obs.Observed, r.agent.ID, r.agent.Name)
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

func (w *MarketWatcher) count(metric string) {
	if w.metrics != nil {
		w.metrics.IncrementCounter(metric, map[string]string{"kind": entities.TriggerKindMarket})
	}
}

// Snapshot is the current market-watch status.
func (w *MarketWatcher) Snapshot() marketstatus.Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	snap := marketstatus.Snapshot{UpdatedAt: w.Now().UTC(), Symbols: []marketstatus.SymbolStatus{}}
	syms := make([]string, 0, len(w.subs))
	for s := range w.subs {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	for _, sym := range syms {
		agents := map[uint]bool{}
		for _, r := range w.rules[sym] {
			agents[r.agent.ID] = true
		}
		st := marketstatus.SymbolStatus{Symbol: sym, Watchers: len(agents)}
		sw := w.subs[sym]
		sw.mu.Lock()
		if n := len(sw.candles); n > 0 {
			price := sw.candles[n-1].Close
			at := sw.candles[n-1].OpenTime.Add(time.Minute).UTC()
			st.LastPrice, st.LastCandleAt = &price, &at
		}
		sw.mu.Unlock()
		snap.Symbols = append(snap.Symbols, st)
	}
	return snap
}

func (w *MarketWatcher) writeStatus(ctx context.Context) {
	if w.status == nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := w.status.Write(wctx, w.Snapshot()); err != nil {
		log.Printf("market watcher: cannot write status snapshot: %v", err)
	}
}
