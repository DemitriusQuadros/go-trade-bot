package agent_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	taskagent "go-trade-bot/app/handler/tasks/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSweepSignals struct {
	closed     map[uint][]entities.Signal
	lastOpened map[uint]*time.Time
	gotFrom    time.Time
}

func (f *fakeSweepSignals) ListClosedBetween(_ context.Context, id uint, from, to time.Time) ([]entities.Signal, error) {
	f.gotFrom = from
	var out []entities.Signal
	for _, s := range f.closed[id] {
		if !s.UpdatedAt.Before(from) && s.UpdatedAt.Before(to) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSweepSignals) LastOpenedAt(_ context.Context, id uint) (*time.Time, error) {
	return f.lastOpened[id], nil
}

func closedTrade(closedAt time.Time, profit, invested float32) entities.Signal {
	return entities.Signal{Status: entities.Closed, UpdatedAt: closedAt, Orders: []entities.Order{{Profit: profit, InvestedAmount: invested}}}
}

func TestRealizedDrawdown(t *testing.T) {
	// cum: +10, -10 (dd 20), +5, -25 (dd 35 from peak 10)
	dd := taskagent.RealizedDrawdown([]entities.Signal{
		closedTrade(t0.Add(1*time.Hour), 10, 500),
		closedTrade(t0.Add(2*time.Hour), -20, 500),
		closedTrade(t0.Add(3*time.Hour), 15, 500),
		closedTrade(t0.Add(4*time.Hour), -30, 500),
	})
	assert.Equal(t, 4, dd.Trades)
	assert.InDelta(t, 35, dd.DrawdownAbs, 1e-9)
	assert.InDelta(t, 500, dd.NotionalBasis, 1e-9)
	assert.InDelta(t, 7, dd.DrawdownPct, 1e-9)

	assert.Equal(t, taskagent.Drawdown{}, taskagent.RealizedDrawdown(nil))
	// A loss from the very first trade counts (peak starts at 0).
	first := taskagent.RealizedDrawdown([]entities.Signal{closedTrade(t0, -50, 1000)})
	assert.InDelta(t, 5, first.DrawdownPct, 1e-9)
}

// C-01 AC#5.
func TestSweeper_Drawdown(t *testing.T) {
	cases := []struct {
		name   string
		trades []entities.Signal
		fires  bool
	}{
		{"threshold crossed", []entities.Signal{closedTrade(t0.Add(-5*time.Hour), 10, 1000), closedTrade(t0.Add(-2*time.Hour), -70, 1000)}, true}, // dd 70 = 7%
		{"not crossed", []entities.Signal{closedTrade(t0.Add(-5*time.Hour), 10, 1000), closedTrade(t0.Add(-2*time.Hour), -30, 1000)}, false},      // dd 3%
		{"no trades", nil, false},
		{"loss outside window", []entities.Signal{closedTrade(t0.Add(-30*time.Hour), -500, 1000)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newDispatchEnv(map[uint]entities.Strategy{5: {ID: 5, Status: entities.Testing, Mode: "dryrun", MonitoredSymbols: []string{"ETHUSDT"}}})
			e.agents.add(entities.Agent{ID: 1, Name: "DD"}, entities.AgentTriggers{Events: []entities.EventTrigger{{Type: "drawdown", ThresholdPct: 5, WindowHours: 24}}}, 5)
			signals := &fakeSweepSignals{closed: map[uint][]entities.Signal{5: tc.trades}}
			s := taskagent.NewSweeper(e.agents, fakeStrategyStore{byID: map[uint]entities.Strategy{5: {ID: 5, Status: entities.Testing, Mode: "dryrun", MonitoredSymbols: []string{"ETHUSDT"}}}}, signals, e.d)
			s.Now = e.clock.Now
			n, err := s.Sweep(context.Background())
			require.NoError(t, err)
			assert.True(t, signals.gotFrom.Equal(t0.Add(-24*time.Hour)), "window_hours bounds the query")
			if !tc.fires {
				assert.Zero(t, n)
				assert.Zero(t, e.enq.count())
				return
			}
			require.Equal(t, 1, n)
			r := e.enq.runs()[0]
			assert.Equal(t, "event", r.Trigger)
			assert.Equal(t, uint(5), *r.StrategyID)
			d := detailMap(t, r.Detail)
			assert.Equal(t, "drawdown", d["event"])
			assert.Equal(t, "ETHUSDT", d["symbol"])
			assert.InDelta(t, 7.0, d["data"].(map[string]any)["drawdown_pct"], 1e-9)

			// Sweeps every 5 minutes don't re-fire inside the 240m default cooldown.
			e.clock.Add(5 * time.Minute)
			n, err = s.Sweep(context.Background())
			require.NoError(t, err)
			assert.Zero(t, n)
			assert.Equal(t, 1.0, e.metrics.get(taskagent.MetricTriggersSuppressed+"/event"))
		})
	}
}

// C-01 AC#5.
func TestSweeper_NoSignal(t *testing.T) {
	recent, stale := t0.Add(-2*time.Hour), t0.Add(-13*time.Hour)
	cases := []struct {
		name     string
		strategy entities.Strategy
		last     *time.Time
		fires    bool
	}{
		{"fresh", entities.Strategy{ID: 5, Status: entities.Testing, Mode: "dryrun", CreatedAt: t0.Add(-100 * time.Hour)}, &recent, false},
		{"stale", entities.Strategy{ID: 5, Status: entities.Productive, Mode: "live", CreatedAt: t0.Add(-100 * time.Hour)}, &stale, true},
		{"never traded, created long ago", entities.Strategy{ID: 5, Status: entities.Testing, Mode: "dryrun", CreatedAt: t0.Add(-20 * time.Hour)}, nil, true},
		{"never traded, just created", entities.Strategy{ID: 5, Status: entities.Testing, Mode: "dryrun", CreatedAt: t0.Add(-1 * time.Hour)}, nil, false},
		{"disabled skipped", entities.Strategy{ID: 5, Status: entities.Disabled, Mode: "dryrun", CreatedAt: t0.Add(-100 * time.Hour)}, &stale, false},
		{"backtest-mode skipped", entities.Strategy{ID: 5, Status: entities.Testing, Mode: "backtest", CreatedAt: t0.Add(-100 * time.Hour)}, &stale, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			byID := map[uint]entities.Strategy{5: tc.strategy}
			e := newDispatchEnv(byID)
			e.agents.add(entities.Agent{ID: 1}, entities.AgentTriggers{Events: []entities.EventTrigger{{Type: "no_signal", WindowHours: 12}}}, 5)
			s := taskagent.NewSweeper(e.agents, fakeStrategyStore{byID: byID}, &fakeSweepSignals{lastOpened: map[uint]*time.Time{5: tc.last}}, e.d)
			s.Now = e.clock.Now
			n, err := s.Sweep(context.Background())
			require.NoError(t, err)
			if tc.fires {
				require.Equal(t, 1, n)
				assert.Equal(t, "no_signal", detailMap(t, e.enq.runs()[0].Detail)["event"])
			} else {
				assert.Zero(t, n)
			}
		})
	}
}

func TestSweeper_SkipsPausedAgentsAndKillSwitch(t *testing.T) {
	byID := map[uint]entities.Strategy{5: {ID: 5, Status: entities.Testing, CreatedAt: t0.Add(-100 * time.Hour)}}
	e := newDispatchEnv(byID)
	trig := entities.AgentTriggers{Events: []entities.EventTrigger{{Type: "no_signal", WindowHours: 1}}}
	e.agents.add(entities.Agent{ID: 1, Paused: true}, trig, 5)
	e.agents.add(entities.Agent{ID: 2}, trig) // unbound: 5 not in scope
	s := taskagent.NewSweeper(e.agents, fakeStrategyStore{byID: byID}, &fakeSweepSignals{}, e.d)
	s.Now = e.clock.Now
	n, err := s.Sweep(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)

	e.agents.add(entities.Agent{ID: 3}, trig, 5)
	e.settings.set(true)
	n, _ = s.Sweep(context.Background())
	assert.Zero(t, n, "kill switch")
	e.settings.set(false)
	n, _ = s.Sweep(context.Background())
	assert.Equal(t, 1, n)
}
