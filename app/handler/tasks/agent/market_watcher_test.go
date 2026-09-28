package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	taskagent "go-trade-bot/app/handler/tasks/agent"
	"go-trade-bot/internal/cooldown"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/marketstatus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C-01 AC#6: fake candle stream, fake clock, no network.

type fakeFeed struct {
	ch        chan exchange.Candle
	done      chan struct{}
	closeOnce sync.Once
}

func newFakeFeed() *fakeFeed {
	return &fakeFeed{ch: make(chan exchange.Candle, 64), done: make(chan struct{})}
}

func (f *fakeFeed) Next() (exchange.Candle, bool) {
	select {
	case c := <-f.ch:
		return c, true
	case <-f.done:
		return exchange.Candle{}, false
	}
}

func (f *fakeFeed) Close() error {
	f.closeOnce.Do(func() { close(f.done) })
	return nil
}

func (f *fakeFeed) closed() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

type fakeFeeds struct {
	mu    sync.Mutex
	feeds map[string]*fakeFeed
	fail  map[string]bool
}

func (f *fakeFeeds) open(symbol string) (taskagent.KlineFeed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[symbol] {
		return nil, errors.New("invalid symbol")
	}
	ff := newFakeFeed()
	f.feeds[symbol] = ff
	return ff, nil
}

func (f *fakeFeeds) get(symbol string) *fakeFeed {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.feeds[symbol]
}

type fakeKlines struct {
	mu      sync.Mutex
	candles map[string][]exchange.Candle
	limits  []int
}

func (f *fakeKlines) ListKline(_ context.Context, symbol, interval string, limit int) ([]exchange.Candle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if interval != "1m" {
		return nil, errors.New("unexpected interval")
	}
	f.limits = append(f.limits, limit)
	all := f.candles[symbol]
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return append([]exchange.Candle(nil), all...), nil
}

type memStatus struct {
	mu   sync.Mutex
	last *marketstatus.Snapshot
}

func (m *memStatus) Write(_ context.Context, s marketstatus.Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last = &s
	return nil
}

func candleAt(sym string, open time.Time, o, h, l, c float64) exchange.Candle {
	return exchange.Candle{Symbol: sym, Timeframe: "1m", OpenTime: open, Open: o, High: h, Low: l, Close: c, Volume: 1}
}

// flat returns n closed candles ending the minute before end, range 1.
func flat(sym string, end time.Time, n int, price float64) []exchange.Candle {
	out := make([]exchange.Candle, 0, n)
	for i := n; i >= 1; i-- {
		out = append(out, candleAt(sym, end.Add(-time.Duration(i)*time.Minute), price, price+0.5, price-0.5, price))
	}
	return out
}

type watcherEnv struct {
	agents   *fakeAgents
	settings *fakeKillSwitch
	enq      *fakeRunEnqueuer
	metrics  *counterMetrics
	clock    *fakeClock
	feeds    *fakeFeeds
	klines   *fakeKlines
	status   *memStatus
	w        *taskagent.MarketWatcher
}

func newWatcherEnv(t *testing.T, strategies map[uint]entities.Strategy) *watcherEnv {
	e := &watcherEnv{
		agents: newFakeAgents(), settings: &fakeKillSwitch{}, enq: &fakeRunEnqueuer{}, metrics: newCounterMetrics(),
		clock: &fakeClock{now: t0.Add(30 * time.Second)}, feeds: &fakeFeeds{feeds: map[string]*fakeFeed{}, fail: map[string]bool{}},
		klines: &fakeKlines{candles: map[string][]exchange.Candle{}}, status: &memStatus{},
	}
	store := cooldown.NewMemoryStore()
	store.Now = e.clock.Now
	e.w = taskagent.NewMarketWatcher(e.agents, fakeStrategyStore{byID: strategies}, e.settings, e.klines, e.feeds.open,
		indicators.NewTalibAdapter(), store, e.enq, e.status, e.metrics, time.Hour)
	e.w.Now = e.clock.Now
	t.Cleanup(e.w.Stop)
	return e
}

// push sends a closed candle and waits until the watcher processed it.
func (e *watcherEnv) push(t *testing.T, sym string, c exchange.Candle) {
	t.Helper()
	before := e.w.CandlesProcessed()
	e.clock.Add(time.Minute)
	e.feeds.get(sym).ch <- c
	require.Eventually(t, func() bool { return e.w.CandlesProcessed() > before }, 2*time.Second, time.Millisecond)
}

func TestMarketWatcher_PctMoveFiresOnceThenCooldown(t *testing.T) {
	btc := entities.Strategy{ID: 5, MonitoredSymbols: []string{"BTCUSDT"}}
	e := newWatcherEnv(t, map[uint]entities.Strategy{5: btc})
	e.agents.add(entities.Agent{ID: 1, Name: "Macro"}, entities.AgentTriggers{Market: []entities.MarketRule{
		{ID: "r-btc", Symbol: "BTCUSDT", Kind: "pct_move", WindowMinutes: 5, ThresholdPct: 2, CooldownMinutes: 60},
	}}, 5)
	// Backfill: 10 flat closed candles plus the still-forming t0 candle.
	e.klines.candles["BTCUSDT"] = append(flat("BTCUSDT", t0, 10, 100), candleAt("BTCUSDT", t0, 100, 150, 50, 150))

	e.w.Sync(context.Background())
	assert.Equal(t, []string{"BTCUSDT"}, e.w.Symbols())
	assert.Equal(t, 1.0, e.metrics.gauge(taskagent.MetricMarketSubscriptions))
	assert.Equal(t, []int{7}, e.klines.limits, "backfill sized to the largest window + 1 (+1 for the forming candle)")

	// +1% : below threshold (the forming t0 candle at 150 was dropped).
	e.push(t, "BTCUSDT", candleAt("BTCUSDT", t0, 100, 101, 100, 101))
	assert.Zero(t, e.enq.count())
	// -3% vs 5 minutes earlier: fires, observed_pct is signed.
	e.push(t, "BTCUSDT", candleAt("BTCUSDT", t0.Add(time.Minute), 101, 101, 97, 97))
	require.Equal(t, 1, e.enq.count())
	r := e.enq.runs()[0]
	assert.Equal(t, "market", r.Trigger)
	assert.Equal(t, uint(1), r.AgentID)
	require.NotNil(t, r.StrategyID, "exactly one bound strategy monitors BTCUSDT")
	assert.Equal(t, uint(5), *r.StrategyID)
	d := detailMap(t, r.Detail)
	assert.Equal(t, "r-btc", d["rule_id"])
	assert.Equal(t, "BTCUSDT", d["symbol"])
	assert.Equal(t, "pct_move", d["kind"])
	assert.Equal(t, float64(5), d["window_minutes"])
	assert.InDelta(t, -3.0, d["observed_pct"], 1e-9)
	assert.Equal(t, float64(2), d["threshold"])
	assert.Equal(t, float64(97), d["price"])
	assert.Equal(t, "2026-05-01T12:02:00Z", d["at"])
	assert.Equal(t, 1.0, e.metrics.get(taskagent.MetricTriggersFired+"/market"))

	// Still beyond the threshold, but inside the 60m cooldown: suppressed.
	e.push(t, "BTCUSDT", candleAt("BTCUSDT", t0.Add(2*time.Minute), 97, 97, 95, 95))
	assert.Equal(t, 1, e.enq.count())
	assert.Equal(t, 1.0, e.metrics.get(taskagent.MetricTriggersSuppressed+"/market"))

	// Status snapshot for GET /agents/market-symbols.
	e.w.Sync(context.Background())
	e.status.mu.Lock()
	snap := e.status.last
	e.status.mu.Unlock()
	require.NotNil(t, snap)
	require.Len(t, snap.Symbols, 1)
	assert.Equal(t, 1, snap.Symbols[0].Watchers)
	assert.Equal(t, 95.0, *snap.Symbols[0].LastPrice)
	assert.True(t, snap.Symbols[0].LastCandleAt.Equal(t0.Add(3*time.Minute)))
}

func TestMarketWatcher_VolatilitySpikeFiresOnSyntheticATRJump(t *testing.T) {
	e := newWatcherEnv(t, map[uint]entities.Strategy{})
	e.agents.add(entities.Agent{ID: 2, Name: "Vol"}, entities.AgentTriggers{Market: []entities.MarketRule{
		{ID: "v", Symbol: "ETHUSDT", Kind: "volatility_spike", WindowMinutes: 5, Multiplier: 3, CooldownMinutes: 60},
	}}) // no bound strategy: a macro watch
	e.klines.candles["ETHUSDT"] = flat("ETHUSDT", t0, 200, 100)
	e.w.Sync(context.Background())

	for i := 0; i < 5 && e.enq.count() == 0; i++ {
		e.push(t, "ETHUSDT", candleAt("ETHUSDT", t0.Add(time.Duration(i)*time.Minute), 100, 106, 94, 100)) // range 12 vs 1
	}
	require.Equal(t, 1, e.enq.count())
	r := e.enq.runs()[0]
	assert.Nil(t, r.StrategyID, "no bound strategy monitors ETHUSDT")
	d := detailMap(t, r.Detail)
	assert.Equal(t, "volatility_spike", d["kind"])
	assert.GreaterOrEqual(t, d["observed_multiplier"].(float64), 3.0)
	assert.Equal(t, float64(3), d["threshold"])
	assert.Equal(t, false, d["monitored_by_bound_strategy"])
	assert.NotContains(t, d, "observed_pct")

	// Flat candles alone never spike.
	obs := taskagent.EvaluateMarketRule(entities.MarketRule{Kind: "volatility_spike", WindowMinutes: 5, Multiplier: 1.5}, flat("X", t0, 300, 100), indicators.NewTalibAdapter())
	assert.True(t, obs.Evaluated)
	assert.False(t, obs.Fired)
	assert.InDelta(t, 1.0, obs.Observed, 1e-6)
}

func TestMarketWatcher_RemovingRuleClosesSubscriptionOnNextSync(t *testing.T) {
	e := newWatcherEnv(t, map[uint]entities.Strategy{})
	rule := entities.MarketRule{ID: "r", Symbol: "SOLUSDT", Kind: "pct_move", WindowMinutes: 1, ThresholdPct: 1, CooldownMinutes: 60}
	e.agents.add(entities.Agent{ID: 3}, entities.AgentTriggers{Market: []entities.MarketRule{rule}})
	e.w.Sync(context.Background())
	feed := e.feeds.get("SOLUSDT")
	require.NotNil(t, feed)
	assert.False(t, feed.closed())

	e.agents.setTriggers(3, entities.AgentTriggers{})
	e.w.Sync(context.Background())
	assert.True(t, feed.closed(), "subscription closed on the next sync")
	assert.Empty(t, e.w.Symbols())
	assert.Equal(t, 0.0, e.metrics.gauge(taskagent.MetricMarketSubscriptions))

	// Kill switch: nothing is watched.
	e.agents.setTriggers(3, entities.AgentTriggers{Market: []entities.MarketRule{rule}})
	e.w.Sync(context.Background())
	require.Equal(t, []string{"SOLUSDT"}, e.w.Symbols())
	e.settings.set(true)
	e.w.Sync(context.Background())
	assert.Empty(t, e.w.Symbols())
}

func TestMarketWatcher_PausedAgentsAreNotWatchedAndGapIsBackfilled(t *testing.T) {
	e := newWatcherEnv(t, map[uint]entities.Strategy{})
	e.agents.add(entities.Agent{ID: 4, Paused: true}, entities.AgentTriggers{Market: []entities.MarketRule{
		{ID: "p", Symbol: "XRPUSDT", Kind: "pct_move", WindowMinutes: 1, ThresholdPct: 1, CooldownMinutes: 60}}})
	e.agents.add(entities.Agent{ID: 5}, entities.AgentTriggers{Market: []entities.MarketRule{
		{ID: "g", Symbol: "ADAUSDT", Kind: "pct_move", WindowMinutes: 3, ThresholdPct: 50, CooldownMinutes: 60}}})
	e.klines.candles["ADAUSDT"] = flat("ADAUSDT", t0, 5, 1)
	e.w.Sync(context.Background())
	assert.Equal(t, []string{"ADAUSDT"}, e.w.Symbols())

	// A reconnect gap: the next candle arrives 3 minutes late; the missing
	// ones are backfilled from ListKline before evaluation.
	e.clock.Add(3 * time.Minute)
	e.klines.mu.Lock()
	e.klines.candles["ADAUSDT"] = append(flat("ADAUSDT", t0, 5, 1), flat("ADAUSDT", t0.Add(3*time.Minute), 3, 1)...)
	e.klines.mu.Unlock()
	e.push(t, "ADAUSDT", candleAt("ADAUSDT", t0.Add(3*time.Minute), 1, 1, 1, 1))
	snap := e.w.Snapshot()
	require.Len(t, snap.Symbols, 1)
	e.klines.mu.Lock()
	assert.Len(t, e.klines.limits, 2, "one startup backfill + one gap backfill")
	e.klines.mu.Unlock()
}
