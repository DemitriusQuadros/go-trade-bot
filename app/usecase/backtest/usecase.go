package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/app/strategies"
	signal_usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/feed"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/metrics_provider"
	"go-trade-bot/internal/report"

	"gorm.io/datatypes"
)

type StrategyRepository interface {
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
}

type BacktestRepository interface {
	Create(ctx context.Context, run *entities.BacktestRun) error
	GetByID(ctx context.Context, id uint) (entities.BacktestRun, error)
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
	ListRunsWithReport(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
	Update(ctx context.Context, run entities.BacktestRun) error
}

type ThresholdPolicy struct {
	MinSharpe      float64 `json:"min_sharpe"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
}

func DefaultThresholdPolicy() ThresholdPolicy {
	return ThresholdPolicy{
		MinSharpe:      0.8,
		MaxDrawdownPct: 20.0,
	}
}

type RunRequest struct {
	StrategyID     uint              `json:"strategy_id"`
	Symbol         string            `json:"symbol"`
	Timeframe      string            `json:"timeframe"`
	StartDate      time.Time         `json:"start_date"`
	EndDate        time.Time         `json:"end_date"`
	InitialCapital float64           `json:"initial_capital"`
	FillPolicy     engine.FillPolicy `json:"fill_policy"`
}

type WalkForwardRequest struct {
	StrategyID     uint              `json:"strategy_id"`
	Symbol         string            `json:"symbol"`
	Timeframe      string            `json:"timeframe"`
	StartDate      time.Time         `json:"start_date"`
	EndDate        time.Time         `json:"end_date"`
	InitialCapital float64           `json:"initial_capital"`
	TrainMonths    int               `json:"train_months"`
	TestMonths     int               `json:"test_months"`
	StepMonths     int               `json:"step_months"`
	FillPolicy     engine.FillPolicy `json:"fill_policy"`
}

type WalkForwardTradeLogPayload struct {
	Trades  []metrics_provider.TradeLogEntry `json:"trades"`
	Windows []engine.WalkForwardWindow       `json:"windows"`
}

type BacktestUseCase struct {
	candleRepo      candle.Repository
	backtestRepo    BacktestRepository
	strategyRepo    StrategyRepository
	metricsProvider metrics_provider.MetricsProvider
	policy          ThresholdPolicy
	reportsDir      string
	retentionLimit  int
	collector       *metrics.MetricsCollector
}

func NewBacktestUseCase(
	candleRepo candle.Repository,
	backtestRepo BacktestRepository,
	strategyRepo StrategyRepository,
	metricsProvider metrics_provider.MetricsProvider,
	policy ThresholdPolicy,
	reportsDir string,
	retentionLimit int,
) *BacktestUseCase {
	if reportsDir == "" {
		reportsDir = "reports"
	}
	if retentionLimit <= 0 {
		retentionLimit = 20
	}
	if policy.MinSharpe == 0 && policy.MaxDrawdownPct == 0 {
		policy = DefaultThresholdPolicy()
	}
	return &BacktestUseCase{
		candleRepo:      candleRepo,
		backtestRepo:    backtestRepo,
		strategyRepo:    strategyRepo,
		metricsProvider: metricsProvider,
		policy:          policy,
		reportsDir:      reportsDir,
		retentionLimit:  retentionLimit,
	}
}

func (u *BacktestUseCase) SetMetricsCollector(collector *metrics.MetricsCollector) {
	u.collector = collector
}

func (u *BacktestUseCase) Run(ctx context.Context, req RunRequest) (entities.BacktestRun, error) {
	startTimer := time.Now()
	strategyName := "unknown"
	defer func() {
		if u.collector != nil {
			duration := time.Since(startTimer).Seconds()
			u.collector.ObserveHistogram("backtest_run_duration_seconds", map[string]string{"strategy": strategyName, "is_walk_forward": "false"}, duration)
		}
	}()

	if req.StrategyID == 0 {
		return entities.BacktestRun{}, fmt.Errorf("strategy_id is required")
	}
	if req.Symbol == "" {
		return entities.BacktestRun{}, fmt.Errorf("symbol is required")
	}
	if req.Timeframe == "" {
		req.Timeframe = "1m"
	}
	if req.InitialCapital <= 0 {
		req.InitialCapital = 1000.0
	}

	strat, err := u.strategyRepo.GetByID(ctx, req.StrategyID)
	if err != nil {
		return entities.BacktestRun{}, fmt.Errorf("failed to load strategy %d: %w", req.StrategyID, err)
	}
	strategyName = strat.Name

	tradeLog, err := u.executeReplay(ctx, strat, req.Symbol, req.Timeframe, req.StartDate, req.EndDate, req.InitialCapital, req.FillPolicy)
	if err != nil {
		return entities.BacktestRun{}, fmt.Errorf("backtest execution failed: %w", err)
	}

	periodsPerYear := periodsPerYearForTimeframe(req.Timeframe)
	metrics := u.metricsProvider.Compute(tradeLog, req.InitialCapital, periodsPerYear)

	// Evaluate threshold policy
	passed := metrics.SharpeRatio >= u.policy.MinSharpe && metrics.MaxDrawdownPct <= u.policy.MaxDrawdownPct

	// HTML Report generation
	reportInput := report.BacktestReportInput{
		RunID:        0, // Set after save or generated uniquely
		StrategyName: strat.Name,
		Symbol:       req.Symbol,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		Metrics:      metrics,
		Trades:       tradeLog,
	}
	htmlPath, err := report.Generate(u.reportsDir, reportInput)
	if err != nil {
		// Non-fatal: AC#4
		htmlPath = ""
	}

	// Marshal trade log
	tradeLogBytes, _ := json.Marshal(tradeLog)

	// Marshal full metrics
	metricsBytes, _ := json.Marshal(metrics)

	// Safe Profit Factor for DB
	dbProfitFactor := metrics.ProfitFactor
	if math.IsInf(dbProfitFactor, 1) || dbProfitFactor > 1e15 {
		dbProfitFactor = math.MaxFloat64
	}

	run := entities.BacktestRun{
		StrategyID:     strat.ID,
		Symbol:         req.Symbol,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		IsWalkForward:  false,
		Sharpe:         metrics.SharpeRatio,
		MaxDrawdownPct: metrics.MaxDrawdownPct,
		WinRatePct:     metrics.WinRatePct,
		ProfitFactor:   dbProfitFactor,
		TotalTrades:    metrics.TotalTrades,
		TotalReturnPct: metrics.TotalReturnPct,
		Passed:         passed,
		HTMLReportPath: htmlPath,
		TradeLogJSON:   datatypes.JSON(tradeLogBytes),
		MetricsJSON:    datatypes.JSON(metricsBytes),
		InitialCapital: req.InitialCapital,
		CreatedAt:      time.Now().UTC(),
	}

	if err := u.backtestRepo.Create(ctx, &run); err != nil {
		return entities.BacktestRun{}, fmt.Errorf("failed to persist backtest run: %w", err)
	}

	u.pruneReports(ctx, strat.ID)
	return run, nil
}

func (u *BacktestUseCase) RunWalkForward(ctx context.Context, req WalkForwardRequest) (entities.BacktestRun, error) {
	startTimer := time.Now()
	strategyName := "unknown"
	defer func() {
		if u.collector != nil {
			duration := time.Since(startTimer).Seconds()
			u.collector.ObserveHistogram("backtest_run_duration_seconds", map[string]string{"strategy": strategyName, "is_walk_forward": "true"}, duration)
		}
	}()

	if req.StrategyID == 0 {
		return entities.BacktestRun{}, fmt.Errorf("strategy_id is required")
	}
	if req.Symbol == "" {
		return entities.BacktestRun{}, fmt.Errorf("symbol is required")
	}
	if req.Timeframe == "" {
		req.Timeframe = "1m"
	}
	if req.InitialCapital <= 0 {
		req.InitialCapital = 1000.0
	}
	if req.TrainMonths <= 0 {
		req.TrainMonths = 3
	}
	if req.TestMonths <= 0 {
		req.TestMonths = 1
	}
	if req.StepMonths <= 0 {
		req.StepMonths = req.TestMonths
	}

	strat, err := u.strategyRepo.GetByID(ctx, req.StrategyID)
	if err != nil {
		return entities.BacktestRun{}, fmt.Errorf("failed to load strategy %d: %w", req.StrategyID, err)
	}
	strategyName = strat.Name

	periodsPerYear := periodsPerYearForTimeframe(req.Timeframe)

	wfConfig := engine.WalkForwardConfig{
		TotalRange: engine.TimeRange{
			From: req.StartDate,
			To:   req.EndDate,
		},
		TrainWindow: time.Duration(req.TrainMonths*30*24) * time.Hour,
		TestWindow:  time.Duration(req.TestMonths*30*24) * time.Hour,
		StepSize:    time.Duration(req.StepMonths*30*24) * time.Hour,
	}

	var allOOSTrades []metrics_provider.TradeLogEntry
	runner := func(ctx context.Context, from, to time.Time) ([]metrics_provider.TradeLogEntry, error) {
		return u.executeReplay(ctx, strat, req.Symbol, req.Timeframe, from, to, req.InitialCapital, req.FillPolicy)
	}

	wfResult, err := engine.RunWalkForward(ctx, wfConfig, runner, u.metricsProvider, req.InitialCapital, periodsPerYear)
	if err != nil {
		return entities.BacktestRun{}, fmt.Errorf("walk-forward validation failed: %w", err)
	}

	for _, w := range wfResult.Windows {
		// Aggregate OOS trades
		testTrades, _ := runner(ctx, w.TestRange.From, w.TestRange.To)
		allOOSTrades = append(allOOSTrades, testTrades...)
	}

	metrics := wfResult.AggregateOOS
	passed := metrics.SharpeRatio >= u.policy.MinSharpe && metrics.MaxDrawdownPct <= u.policy.MaxDrawdownPct

	// HTML Report generation for OOS aggregate
	reportInput := report.BacktestReportInput{
		RunID:        0,
		StrategyName: strat.Name + " (Walk-Forward OOS)",
		Symbol:       req.Symbol,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		Metrics:      metrics,
		Trades:       allOOSTrades,
	}
	htmlPath, err := report.Generate(u.reportsDir, reportInput)
	if err != nil {
		htmlPath = ""
	}

	wfPayload := WalkForwardTradeLogPayload{
		Trades:  allOOSTrades,
		Windows: wfResult.Windows,
	}
	tradeLogBytes, _ := json.Marshal(wfPayload)
	metricsBytes, _ := json.Marshal(metrics)

	dbProfitFactor := metrics.ProfitFactor
	if math.IsInf(dbProfitFactor, 1) || dbProfitFactor > 1e15 {
		dbProfitFactor = math.MaxFloat64
	}

	run := entities.BacktestRun{
		StrategyID:     strat.ID,
		Symbol:         req.Symbol,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		IsWalkForward:  true,
		Sharpe:         metrics.SharpeRatio,
		MaxDrawdownPct: metrics.MaxDrawdownPct,
		WinRatePct:     metrics.WinRatePct,
		ProfitFactor:   dbProfitFactor,
		TotalTrades:    metrics.TotalTrades,
		TotalReturnPct: metrics.TotalReturnPct,
		Passed:         passed,
		HTMLReportPath: htmlPath,
		TradeLogJSON:   datatypes.JSON(tradeLogBytes),
		MetricsJSON:    datatypes.JSON(metricsBytes),
		InitialCapital: req.InitialCapital,
		CreatedAt:      time.Now().UTC(),
	}

	if err := u.backtestRepo.Create(ctx, &run); err != nil {
		return entities.BacktestRun{}, fmt.Errorf("failed to persist walk-forward run: %w", err)
	}

	u.pruneReports(ctx, strat.ID)
	return run, nil
}

// RunEphemeral executes one backtest against an in-memory strategy config
// override, WITHOUT persisting a BacktestRun row or generating an HTML
// report - the metrics-only, no-artifact sibling of Run(), built for
// backend-02's grid-search use case (dozens-to-hundreds of throwaway
// evaluations where persisting each as a full BacktestRun would flood the
// backtest_runs table and the reports/ directory for no operational value).
// baseStrategy is expected to be the caller's in-memory clone with
// Configuration already overridden - this method never mutates or persists
// it.
func (u *BacktestUseCase) RunEphemeral(
	ctx context.Context,
	baseStrategy entities.Strategy,
	symbol, timeframe string,
	from, to time.Time,
	initialCapital float64,
	fillPolicy engine.FillPolicy,
) (metrics_provider.BacktestMetrics, error) {
	if timeframe == "" {
		timeframe = "1m"
	}
	if initialCapital <= 0 {
		initialCapital = 1000.0
	}

	tradeLog, err := u.executeReplay(ctx, baseStrategy, symbol, timeframe, from, to, initialCapital, fillPolicy)
	if err != nil {
		return metrics_provider.BacktestMetrics{}, fmt.Errorf("ephemeral backtest execution failed: %w", err)
	}

	periodsPerYear := periodsPerYearForTimeframe(timeframe)
	return u.metricsProvider.Compute(tradeLog, initialCapital, periodsPerYear), nil
}

func (u *BacktestUseCase) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error) {
	return u.backtestRepo.GetByID(ctx, id)
}

func (u *BacktestUseCase) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	return u.backtestRepo.ListByStrategy(ctx, strategyID)
}

const (
	// MonteCarloMaxIterations is backend-03 AC#6's cap: Monte Carlo's
	// per-iteration cost is trivial (no candle reload, just a shuffle +
	// Compute call over a typically-small trade list), so this is basic
	// input hygiene, not a homelab-memory-ceiling concern the way
	// backend-02's grid-size cap is.
	MonteCarloMaxIterations = 10000
	// MonteCarloDefaultIterations is used when the caller passes 0/negative.
	MonteCarloDefaultIterations = 1000
	// MonteCarloMinTrades is backend-03 AC#7's floor: reordering a 0- or
	// 1-element trade list is meaningless.
	MonteCarloMinTrades = 2
)

var (
	ErrInvalidMonteCarloIterations     = fmt.Errorf("iterations must be a positive integer no greater than %d", MonteCarloMaxIterations)
	ErrInsufficientTradesForMonteCarlo = fmt.Errorf("backtest run has fewer than %d trades; Monte Carlo reordering is not meaningful", MonteCarloMinTrades)
	ErrMonteCarloNotYetComputed        = fmt.Errorf("Monte Carlo has not been computed for this backtest run yet")
	// ErrBacktestRunNotFound is a sentinel wrapped around the underlying
	// repository error (which for the GORM-backed implementation is
	// gorm.ErrRecordNotFound) so callers (the web handler) can distinguish
	// "run doesn't exist" from other failure modes via errors.Is, without
	// needing to import gorm.
	ErrBacktestRunNotFound = fmt.Errorf("backtest run not found")
)

// RunMonteCarlo loads the persisted trade log for `runID` (handling both a
// flat backtest's []TradeLogEntry and a walk-forward run's wrapped
// {trades, windows} shape - AC#8), runs engine.RunMonteCarlo over it, caches
// the result onto the run row's MonteCarloJSON column (so a subsequent
// GetMonteCarlo doesn't re-run the simulation), and returns it.
func (u *BacktestUseCase) RunMonteCarlo(ctx context.Context, runID uint, iterations int) (engine.MonteCarloResult, error) {
	if iterations < 0 || iterations > MonteCarloMaxIterations {
		return engine.MonteCarloResult{}, ErrInvalidMonteCarloIterations
	}
	if iterations == 0 {
		iterations = MonteCarloDefaultIterations
	}

	run, err := u.backtestRepo.GetByID(ctx, runID)
	if err != nil {
		return engine.MonteCarloResult{}, fmt.Errorf("%w: %v", ErrBacktestRunNotFound, err)
	}

	trades, err := extractTradeLog(run)
	if err != nil {
		return engine.MonteCarloResult{}, fmt.Errorf("failed to parse trade log: %w", err)
	}
	if len(trades) < MonteCarloMinTrades {
		return engine.MonteCarloResult{}, ErrInsufficientTradesForMonteCarlo
	}

	startingBalance := run.InitialCapital
	if startingBalance <= 0 {
		startingBalance = 1000.0
	}
	periodsPerYear := periodsPerYearForTimeframe("") // BacktestRun does not persist Timeframe; falls back to the 1m default.

	result := engine.RunMonteCarlo(trades, startingBalance, periodsPerYear, u.metricsProvider, engine.MonteCarloConfig{
		Iterations: iterations,
	})

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return engine.MonteCarloResult{}, fmt.Errorf("failed to marshal monte carlo result: %w", err)
	}
	run.MonteCarloJSON = datatypes.JSON(resultBytes)
	if err := u.backtestRepo.Update(ctx, run); err != nil {
		return engine.MonteCarloResult{}, fmt.Errorf("failed to cache monte carlo result: %w", err)
	}

	return result, nil
}

// GetMonteCarlo returns the most recently cached Monte Carlo result for
// `runID`, without recomputing it - ErrMonteCarloNotYetComputed if
// RunMonteCarlo has never been called for this run.
func (u *BacktestUseCase) GetMonteCarlo(ctx context.Context, runID uint) (engine.MonteCarloResult, error) {
	run, err := u.backtestRepo.GetByID(ctx, runID)
	if err != nil {
		return engine.MonteCarloResult{}, fmt.Errorf("%w: %v", ErrBacktestRunNotFound, err)
	}

	if len(run.MonteCarloJSON) == 0 {
		return engine.MonteCarloResult{}, ErrMonteCarloNotYetComputed
	}

	var result engine.MonteCarloResult
	if err := json.Unmarshal(run.MonteCarloJSON, &result); err != nil {
		return engine.MonteCarloResult{}, fmt.Errorf("failed to parse cached monte carlo result: %w", err)
	}
	return result, nil
}

// extractTradeLog handles backend-03 AC#8: a walk-forward run's
// TradeLogJSON is a WalkForwardTradeLogPayload ({trades, windows}), not a
// flat []TradeLogEntry - this must be parsed correctly rather than assuming
// every BacktestRun.TradeLogJSON has the same shape.
func extractTradeLog(run entities.BacktestRun) ([]metrics_provider.TradeLogEntry, error) {
	if run.IsWalkForward {
		var payload WalkForwardTradeLogPayload
		if err := json.Unmarshal(run.TradeLogJSON, &payload); err != nil {
			return nil, err
		}
		return payload.Trades, nil
	}

	var trades []metrics_provider.TradeLogEntry
	if err := json.Unmarshal(run.TradeLogJSON, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

func (u *BacktestUseCase) executeReplay(
	ctx context.Context,
	strat entities.Strategy,
	symbol, timeframe string,
	from, to time.Time,
	initialCapital float64,
	fillPolicy engine.FillPolicy,
) ([]metrics_provider.TradeLogEntry, error) {
	replayFeed, err := feed.NewReplayFeed(ctx, u.candleRepo, symbol, timeframe, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to create replay feed: %w", err)
	}

	dataSource := engine.NewCandleRepoMarketDataSource(u.candleRepo)
	simExchange := engine.NewSimulatedFillExchange(dataSource, fillPolicy)
	signalRepo := &memSignalRepo{}
	accountUC := &memAccountUC{amount: float32(initialCapital)}
	signalUC := signal_usecase.NewSignalUseCase(signalRepo, accountUC, simExchange, nil, nil, nil)
	indicatorProvider := indicators.NewTalibAdapter()
	cache := memcache.NewInMemoryCache()

	eng := engine.NewEngine(simExchange, indicatorProvider, signalUC, accountUC, nil, cache, nil)
	stratImpl, ok := strategies.Get(strat.StrategyName)
	if !ok {
		return nil, fmt.Errorf("strategy %q not registered", strat.StrategyName)
	}

	driver := engine.NewReplayDriver(replayFeed, simExchange, eng, stratImpl, strat, symbol, strategies.ModeBacktest, signalRepo)
	driver.WarmupSource = engine.NewCandleRepoWarmupSource(u.candleRepo, from)
	return driver.Run(ctx)
}

func (u *BacktestUseCase) pruneReports(ctx context.Context, strategyID uint) {
	runsWithReport, err := u.backtestRepo.ListRunsWithReport(ctx, strategyID)
	if err != nil || len(runsWithReport) <= u.retentionLimit {
		return
	}

	excess := len(runsWithReport) - u.retentionLimit
	for i := 0; i < excess; i++ {
		r := runsWithReport[i]
		if r.HTMLReportPath != "" {
			_ = os.Remove(r.HTMLReportPath)
			r.HTMLReportPath = ""
			_ = u.backtestRepo.Update(ctx, r)
		}
	}
}

func periodsPerYearForTimeframe(tf string) float64 {
	switch tf {
	case "1m":
		return 525600.0
	case "5m":
		return 105120.0
	case "15m":
		return 35040.0
	case "1h":
		return 8760.0
	case "4h":
		return 2190.0
	case "1d", "d":
		return 365.0
	default:
		return 525600.0
	}
}

// In-memory repositories for backtest run isolation
type memSignalRepo struct {
	signals []entities.Signal
}

func (m *memSignalRepo) Create(signal entities.Signal) error {
	signal.ID = uint(len(m.signals) + 1)
	m.signals = append(m.signals, signal)
	return nil
}

func (m *memSignalRepo) GetOpenSignals(symbol string, strategyId uint) (entities.Signal, error) {
	for _, s := range m.signals {
		if s.Symbol == symbol && s.StrategyID == strategyId && s.Status == entities.Open {
			return s, nil
		}
	}
	return entities.Signal{}, nil
}

func (m *memSignalRepo) Update(signal entities.Signal) error {
	for i, s := range m.signals {
		if s.ID == signal.ID {
			m.signals[i] = signal
			return nil
		}
	}
	return nil
}

func (m *memSignalRepo) GetByID(id uint) (entities.Signal, error) {
	for _, s := range m.signals {
		if s.ID == id {
			return s, nil
		}
	}
	return entities.Signal{}, nil
}

func (m *memSignalRepo) GetAll() ([]entities.Signal, error) {
	return m.signals, nil
}

func (m *memSignalRepo) GetAllOpenSignals() ([]entities.Signal, error) {
	var res []entities.Signal
	for _, s := range m.signals {
		if s.Status == entities.Open {
			res = append(res, s)
		}
	}
	return res, nil
}

func (m *memSignalRepo) GetAllClosedSignals() ([]entities.Signal, error) {
	var res []entities.Signal
	for _, s := range m.signals {
		if s.Status == entities.Closed {
			res = append(res, s)
		}
	}
	return res, nil
}

type memAccountUC struct {
	amount float32
}

func (a *memAccountUC) DeductOrder(entryPrice float32) error {
	a.amount -= entryPrice
	return nil
}

func (a *memAccountUC) AddOrder(exitPrice float32) error {
	a.amount += exitPrice
	return nil
}

func (a *memAccountUC) GetDisponibleAmout() (float32, error) {
	return a.amount, nil
}

func (a *memAccountUC) CanOpenOrder() (bool, error) {
	return a.amount > 0, nil
}

func (a *memAccountUC) GetAccount() (entities.Account, error) {
	return entities.Account{ID: 1, Amount: a.amount, AvailableOrders: 5, Currency: "USDT"}, nil
}
