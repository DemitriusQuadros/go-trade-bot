package real

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/handler/mcptools"
	"go-trade-bot/app/handler/web/agentreports"
	backtesthandler "go-trade-bot/app/handler/web/backtest"
	repoagent "go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	backtest_repo "go-trade-bot/app/repository/backtest"
	candle_repo "go-trade-bot/app/repository/candle"
	settings_repo "go-trade-bot/app/repository/settings"
	strategy_repo "go-trade-bot/app/repository/strategy"
	"go-trade-bot/app/strategies"
	strategyscript "go-trade-bot/app/strategies/script"
	agentusecase "go-trade-bot/app/usecase/agent"
	platformusecase "go-trade-bot/app/usecase/agentplatform"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/cmd/mcp/server"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/metrics_provider"
	"go-trade-bot/internal/report/agentreport"

	"github.com/gorilla/mux"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// noTradeScript never enters a position: a run of it is fast and its result
// is deterministic (zero trades), which is all these scenarios need.
const noTradeScript = `function should_long(ctx) return false end`

type nopStateStore struct{}

func (nopStateStore) Load(context.Context, uint, string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (nopStateStore) Save(context.Context, uint, string, map[string]interface{}) error { return nil }

var registerScript sync.Once

// fakeQueue stands in for asynq: it records the enqueued backtest run ids and
// agent runs, and can simulate Redis being down.
type fakeQueue struct {
	mu        sync.Mutex
	down      bool
	backtests []uint
	agentRuns []agentworker.RunPayload
}

func (q *fakeQueue) EnqueueBacktestTask(id uint) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.down {
		return errors.New("redis: connection refused")
	}
	q.backtests = append(q.backtests, id)
	return nil
}

func (q *fakeQueue) EnqueueRun(_ context.Context, p agentworker.RunPayload) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.down {
		return "", errors.New("redis: connection refused")
	}
	q.agentRuns = append(q.agentRuns, p)
	return fmt.Sprintf("task-%d", len(q.agentRuns)), nil
}

// drainBacktests returns and clears the pending backtest ids (the "worker").
func (q *fakeQueue) drainBacktests() []uint {
	q.mu.Lock()
	defer q.mu.Unlock()
	ids := q.backtests
	q.backtests = nil
	return ids
}

type signalStub struct{}

func (signalStub) GetOpenSignals(context.Context) ([]entities.Signal, error) { return nil, nil }

type snapshotStub struct{}

func (snapshotStub) ListByStrategy(context.Context, uint, int) ([]entities.StrategyPerformanceSnapshot, error) {
	return nil, nil
}

// world is one scenario's isolated stack.
type world struct {
	db         *gorm.DB
	reportsDir string
	queue      *fakeQueue
	bt         *backtestusecase.BacktestUseCase
	api        *httptest.Server
	router     *mux.Router
	mcpSrv     *server.Server
	mcp        *mcp.ClientSession

	// last HTTP exchange
	status  int
	header  http.Header
	body    []byte
	lastID  uint
	lastMCP string // text of the last MCP tool result
	lastErr bool   // whether the last MCP call was flagged IsError
}

var dbSeq int
var dbSeqMu sync.Mutex

func newWorld() (*world, error) {
	dbSeqMu.Lock()
	dbSeq++
	n := dbSeq
	dbSeqMu.Unlock()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:real_e2e_%d?mode=memory&cache=shared", n)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(
		&entities.Strategy{}, &entities.StrategyExecution{}, &entities.Signal{}, &entities.Order{}, &entities.Account{},
		&entities.Candle{}, &entities.BacktestRun{}, &entities.Settings{}, &entities.ScriptVersion{}, &entities.ScriptState{},
		&entities.AgentInstruction{}, &entities.AgentRun{}, &entities.Agent{}, &entities.AgentStrategyBinding{},
		&entities.StrategyMemoryEntry{}, &entities.AgentReport{}, &entities.WebhookTarget{}, &entities.AgentUsage{},
	); err != nil {
		return nil, err
	}

	registerScript.Do(func() {
		runner := strategyscript.NewRunner(strategyscript.DefaultHookTimeout, nil)
		strategies.Register("script", func(s entities.Strategy) strategies.Strategy {
			return strategyscript.NewScriptStrategy(s, nopStateStore{}, runner)
		})
	})

	dir, err := os.MkdirTemp("", "real-e2e-reports-*")
	if err != nil {
		return nil, err
	}
	w := &world{db: db, reportsDir: dir, queue: &fakeQueue{}}
	w.bt = backtestusecase.NewBacktestUseCase(
		candle_repo.NewCandleRepository(db), backtest_repo.NewBacktestRepository(db), strategy_repo.NewStrategyRepository(db),
		metrics_provider.NewCinarMetricsAdapter(), backtestusecase.DefaultThresholdPolicy(), dir, 20,
	)

	// REST: the real backtest handler (with the async queue) behind the /api prefix.
	router := mux.NewRouter()
	h := backtesthandler.NewBacktestHandlerAsync(backtesthandler.AsyncParams{UseCase: w.bt, Async: w.bt, Tasks: w.queue})
	for _, c := range h.Handlers() {
		router.HandleFunc("/api"+c.Pattern, c.Action).Methods(c.Method)
	}
	w.router = router
	w.api = httptest.NewServer(router)
	return w, nil
}

// startMCP builds the real MCP server (agent tools + the in-process adapter)
// and connects an in-memory client.
func (w *world) startMCP(ctx context.Context) error {
	platformRepo := agentplatform.NewGormRepository(w.db)
	if _, err := platformRepo.EnsureDefaultAgent(ctx); err != nil {
		return err
	}
	stratRepo := strategy_repo.NewStrategyRepository(w.db)
	settingsRepo := settings_repo.NewRepository(w.db)
	platform := platformusecase.NewUseCase(platformRepo, stratRepo, repoagent.NewGormRepository(w.db), w.queue, settingsRepo, nil)

	agentUC := agentusecase.NewAgentUseCase(nil, repoagent.NewGormRepository(w.db), strategyusecase.NewStrategyUseCase(stratRepo, nil), w.bt, signalStub{}, snapshotStub{})
	agentUC.BacktestQueue = w.bt
	agentUC.BacktestWorker = backtestWorkerAdapter{w.queue}
	agentUC.Platform = platformRepo
	agentUC.Reports = agentreport.NewHTMLRenderer(agentplatform.NewReportDataSource(w.db))
	agentUC.APIBaseURL = w.api.URL
	agentUC.Locales = settings_repo.NewDefaultLocaleSource(settingsRepo)

	// The report pages the report-link tools point at.
	for _, c := range agentreports.NewAgentReportsHandler(platform).Handlers() {
		w.router.HandleFunc("/api"+c.Pattern, c.Action).Methods(c.Method)
	}

	deps := mcptools.Deps{
		Settings: settingsRepo, MonteCarlo: w.bt, Agents: platform, Webhooks: platform,
		WalkForward: w.bt, Tasks: w.queue, Reports: platform, Backtests: w.bt, BaseURL: w.api.URL,
	}
	w.mcpSrv = server.NewServer(agentUC, &configuration.Configuration{}, deps)
	c, err := w.mcpSrv.InMemoryClient(ctx)
	if err != nil {
		return err
	}
	w.mcp = c
	return nil
}

// backtestWorkerAdapter adapts the fake queue to agentusecase.BacktestWorker.
type backtestWorkerAdapter struct{ q *fakeQueue }

func (a backtestWorkerAdapter) EnqueueBacktestTask(id uint) error { return a.q.EnqueueBacktestTask(id) }

func (w *world) close() {
	if w.mcp != nil {
		_ = w.mcp.Close()
	}
	if w.api != nil {
		w.api.Close()
	}
	_ = os.RemoveAll(w.reportsDir)
}

// seedStrategy stores a no-trade script strategy with id 1.
func (w *world) seedStrategy() error {
	return w.db.Create(&entities.Strategy{
		ID: 1, Name: "Trader", StrategyName: "script", ScriptSource: noTradeScript,
		Mode: "backtest", Status: "testing",
		StrategyConfiguration: entities.StrategyConfiguration{Cycle: entities.OneHour},
	}).Error
}

// seedCandles stores 1h BTCUSDT candles for [from, from+days).
func (w *world) seedCandles(from time.Time, days int) error {
	n := days * 24
	candles := make([]entities.Candle, n)
	for i := range candles {
		price := 100.0 + float64(i%10)
		candles[i] = entities.Candle{
			Symbol: "BTCUSDT", Timeframe: "1h", OpenTime: from.Add(time.Duration(i) * time.Hour),
			Open: price, High: price + 1, Low: price - 1, Close: price + 0.5, Volume: 10,
		}
	}
	return w.db.CreateInBatches(&candles, 500).Error
}

func (w *world) runWorker(ctx context.Context) error {
	for _, id := range w.queue.drainBacktests() {
		// Same call cmd/worker's BacktestProcessor makes. A failed run is
		// recorded on the row; the error itself is only the processor's log.
		_ = w.bt.ExecuteQueued(ctx, id)
	}
	return nil
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
