package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/agent/deploygate"
	backtestusecase "go-trade-bot/app/usecase/backtest"
)

// WalkForwardRunner is the slice of app/usecase/backtest the gate needs.
type WalkForwardRunner interface {
	RunWalkForwardForStrategy(ctx context.Context, strat entities.Strategy, req backtestusecase.WalkForwardRequest) (entities.BacktestRun, error)
}

// CandleCounter is the slice of app/repository/candle the gate uses to
// detect insufficient history.
type CandleCounter interface {
	CountInRange(ctx context.Context, symbol, timeframe string, from, to time.Time) (int64, error)
}

// GateConfigReader reads the DeployGateConfig singleton. There is
// deliberately no write method here: thresholds are operator-only (REST).
type GateConfigReader interface {
	GetGateConfig(ctx context.Context) (entities.DeployGateConfig, error)
}

// DeployGate runs the Phase B deploy gate. Implemented by *GateRunner;
// tests may fake it.
type DeployGate interface {
	// Run evaluates candidateSource against target's CURRENT ScriptSource.
	Run(ctx context.Context, target entities.Strategy, candidateSource string) (GateOutcome, error)
	// Config returns the current thresholds.
	Config(ctx context.Context) (entities.DeployGateConfig, error)
}

// GateOutcome is a gate verdict plus the context it was computed in.
type GateOutcome struct {
	Result    deploygate.GateResult
	Config    entities.DeployGateConfig
	Symbol    string
	Timeframe string
	From, To  time.Time
}

// minHistoryCoverage is the fraction of expected candles (for the gate's
// range and timeframe) that must exist, or the gate fails with
// insufficient_history rather than evaluating on thin data.
const minHistoryCoverage = 0.9

// gateInitialCapital is the fixed starting capital for gate runs (the
// same default BacktestUseCase applies).
const gateInitialCapital = 1000.0

// GateRunner does the gate's I/O (B-01 §3): two walk-forward runs over the
// same range - baseline = the target's current source, candidate = the
// candidate source - both persisted under the TARGET strategy's id, then
// deploygate.Evaluate on their persisted metrics. The model supplies no
// numbers and no thresholds; thresholds come only from DeployGateConfig.
type GateRunner struct {
	WalkForward WalkForwardRunner
	Candles     CandleCounter
	Configs     GateConfigReader
	Now         func() time.Time
}

// NewGateRunner builds a GateRunner.
func NewGateRunner(wf WalkForwardRunner, candles CandleCounter, configs GateConfigReader) *GateRunner {
	return &GateRunner{WalkForward: wf, Candles: candles, Configs: configs, Now: time.Now}
}

// Config implements DeployGate.
func (g *GateRunner) Config(ctx context.Context) (entities.DeployGateConfig, error) {
	return g.Configs.GetGateConfig(ctx)
}

// SourceHash is the sha256 hex of a script source.
func SourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// GateTimeframe is the timeframe the gate replays at: the configured one,
// or the strategy's own cycle-derived interval (like the live worker's
// candle fetch), falling back to 1m.
func GateTimeframe(cfg entities.DeployGateConfig, s entities.Strategy) string {
	if cfg.Timeframe != "" {
		return cfg.Timeframe
	}
	if tf := s.GetBrokerInterval(); tf != "" {
		return tf
	}
	return "1m"
}

// TimeframeDuration parses the timeframes the platform imports candles at.
func TimeframeDuration(tf string) (time.Duration, bool) {
	switch tf {
	case "1m":
		return time.Minute, true
	case "5m":
		return 5 * time.Minute, true
	case "10m":
		return 10 * time.Minute, true
	case "15m":
		return 15 * time.Minute, true
	case "30m":
		return 30 * time.Minute, true
	case "1h":
		return time.Hour, true
	case "4h":
		return 4 * time.Hour, true
	case "1d":
		return 24 * time.Hour, true
	}
	return 0, false
}

// Run implements DeployGate. An insufficient candle history or a failed
// backtest yields a FAILING GateResult (never a pass); only a failure to
// even read the configuration is returned as an error.
func (g *GateRunner) Run(ctx context.Context, target entities.Strategy, candidateSource string) (GateOutcome, error) {
	cfg, err := g.Configs.GetGateConfig(ctx)
	if err != nil {
		return GateOutcome{}, fmt.Errorf("deploy gate: could not read thresholds: %w", err)
	}
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	to := now().UTC()
	from := to.AddDate(0, -cfg.LookbackMonths, 0)
	tf := GateTimeframe(cfg, target)
	out := GateOutcome{Config: cfg, Timeframe: tf, From: from, To: to}

	if len(target.MonitoredSymbols) == 0 {
		out.Result = deploygate.InsufficientHistory("the strategy has no monitored symbols")
		return out, nil
	}
	symbol := target.MonitoredSymbols[0]
	out.Symbol = symbol

	if cfg.LookbackMonths < cfg.TrainMonths+cfg.TestMonths || cfg.LookbackMonths <= 0 {
		out.Result = deploygate.InsufficientHistory(fmt.Sprintf("lookback of %d months is shorter than train+test (%d+%d months)", cfg.LookbackMonths, cfg.TrainMonths, cfg.TestMonths))
		return out, nil
	}
	step, ok := TimeframeDuration(tf)
	if !ok {
		out.Result = deploygate.InsufficientHistory(fmt.Sprintf("unsupported timeframe %q", tf))
		return out, nil
	}
	have, err := g.Candles.CountInRange(ctx, symbol, tf, from, to)
	if err != nil {
		out.Result = deploygate.Errored(fmt.Sprintf("could not count candles: %v", err))
		return out, nil
	}
	expected := int64(to.Sub(from) / step)
	if expected <= 0 || float64(have) < minHistoryCoverage*float64(expected) {
		out.Result = deploygate.InsufficientHistory(fmt.Sprintf("%s %s has %d candles between %s and %s, need at least %.0f%% of %d - import more history first%s",
			symbol, tf, have, from.Format(time.RFC3339), to.Format(time.RFC3339), minHistoryCoverage*100, expected,
			coverageSuffix(ctx, g.Candles, symbol)))
		return out, nil
	}

	req := backtestusecase.WalkForwardRequest{
		StrategyID: target.ID, Symbol: symbol, Timeframe: tf, StartDate: from, EndDate: to,
		InitialCapital: gateInitialCapital, TrainMonths: cfg.TrainMonths, TestMonths: cfg.TestMonths, StepMonths: cfg.TestMonths,
	}

	baselineRun, err := g.WalkForward.RunWalkForwardForStrategy(ctx, target, req)
	if err != nil {
		out.Result = deploygate.Errored(fmt.Sprintf("baseline walk-forward failed: %v", err))
		return out, nil
	}

	candidate := target
	candidate.ScriptSource = candidateSource
	creq := req
	creq.CandidateSourceHash = SourceHash(candidateSource)
	candidateRun, err := g.WalkForward.RunWalkForwardForStrategy(ctx, candidate, creq)
	if err != nil {
		out.Result = deploygate.Errored(fmt.Sprintf("candidate walk-forward failed: %v", err))
		out.Result.BaselineRunID = baselineRun.ID
		return out, nil
	}

	out.Result = deploygate.Evaluate(metricsFromRun(candidateRun), metricsFromRun(baselineRun), deploygate.Thresholds{
		MinSharpeDelta:   cfg.MinSharpeDelta,
		MaxDrawdownRatio: cfg.MaxDrawdownRatio,
		MinProfitFactor:  cfg.MinProfitFactor,
		MinTrades:        cfg.MinTrades,
	})
	out.Result.BaselineRunID = baselineRun.ID
	out.Result.CandidateRunID = candidateRun.ID
	return out, nil
}

// metricsFromRun reads a persisted run's aggregate metrics. BacktestUseCase
// stores a +Inf profit factor as math.MaxFloat64; map it back to +Inf.
func metricsFromRun(r entities.BacktestRun) deploygate.Metrics {
	pf := r.ProfitFactor
	if pf >= math.MaxFloat64/2 {
		pf = math.Inf(1)
	}
	return deploygate.Metrics{
		Sharpe:         r.Sharpe,
		MaxDrawdownPct: r.MaxDrawdownPct,
		ProfitFactor:   pf,
		TotalReturnPct: r.TotalReturnPct,
		Trades:         r.TotalTrades,
	}
}
