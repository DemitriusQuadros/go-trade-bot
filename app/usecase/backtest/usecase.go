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
		CreatedAt:      time.Now().UTC(),
	}

	if err := u.backtestRepo.Create(ctx, &run); err != nil {
		return entities.BacktestRun{}, fmt.Errorf("failed to persist walk-forward run: %w", err)
	}

	u.pruneReports(ctx, strat.ID)
	return run, nil
}

func (u *BacktestUseCase) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error) {
	return u.backtestRepo.GetByID(ctx, id)
}

func (u *BacktestUseCase) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	return u.backtestRepo.ListByStrategy(ctx, strategyID)
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
	signalUC := signal_usecase.NewSignalUseCase(signalRepo, accountUC, simExchange, nil, nil)
	indicatorProvider := indicators.NewTalibAdapter()
	cache := memcache.NewInMemoryCache()

	eng := engine.NewEngine(simExchange, indicatorProvider, signalUC, accountUC, nil, cache)
	stratImpl, ok := strategies.Get(strat.StrategyName)
	if !ok {
		return nil, fmt.Errorf("strategy %q not registered", strat.StrategyName)
	}

	driver := engine.NewReplayDriver(replayFeed, simExchange, eng, stratImpl, strat, symbol, strategies.ModeBacktest, signalRepo)
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
