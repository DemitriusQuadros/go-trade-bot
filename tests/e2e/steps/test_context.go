package steps

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go-trade-bot/app/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type TestContext struct {
	T                   *testing.T
	DB                  *gorm.DB
	HTTPClient          *http.Client
	APIServer           *httptest.Server
	MockExchangeServer  *httptest.Server
	MockWebhookServer   *httptest.Server
	LastResponse        *http.Response
	LastBody            []byte
	LastError           error
	ReceivedWebhooks    [][]byte
	WebhookMutex        sync.Mutex
	MockOrders          map[string]map[string]interface{}
	CancelledStopOrders []string
	ProcessMode         string
	ConfirmLiveFlag     bool
	TestnetConfig       bool
	APIToken            string
	CurrentStrategy     *entities.Strategy
}

func NewTestContext() (*TestContext, error) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to memory db: %w", err)
	}

	err = db.AutoMigrate(
		&entities.Strategy{},
		&entities.Signal{},
		&entities.Order{},
		&entities.Account{},
		&entities.StrategyExecution{},
		&entities.Candle{},
		&entities.BacktestRun{},
		&entities.OptimizationRun{},
		&entities.StrategyPerformanceSnapshot{},
		&entities.StrategyPerformance{},
		&entities.ImportJob{},
		&entities.ImportSchedule{},
		&entities.Settings{},
		&entities.ScriptState{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to automigrate: %w", err)
	}

	tc := &TestContext{
		DB:                  db,
		HTTPClient:          &http.Client{},
		MockOrders:          make(map[string]map[string]interface{}),
		CancelledStopOrders: make([]string, 0),
		ReceivedWebhooks:    make([][]byte, 0),
		ProcessMode:         "paper",
	}

	return tc, nil
}

func (tc *TestContext) WipeDatabase() error {
	tables := []string{"orders", "signals", "strategy_executions", "strategies", "accounts", "candles", "backtest_runs", "optimization_runs", "strategy_performance_snapshots", "import_jobs", "import_schedules", "settings"}
	for _, t := range tables {
		tc.DB.Exec(fmt.Sprintf("DELETE FROM %s", t))
	}
	return nil
}

func (tc *TestContext) DoRequest(method, url string, body []byte) error {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewBuffer(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		tc.LastError = err
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.HTTPClient.Do(req)
	if err != nil {
		tc.LastError = err
		return err
	}

	tc.LastResponse = resp
	tc.LastBody, _ = io.ReadAll(resp.Body)
	defer resp.Body.Close()
	return nil
}
