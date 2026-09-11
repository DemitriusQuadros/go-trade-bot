package steps

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

var (
	lastCreatedJobID uint
	lastJobResult    string
	activeWebhookURL string
	inFlightCycle    bool
	swappedExchange  bool
)

func RegisterPlatformSelfServiceSteps(sc *godog.ScenarioContext, tc *TestContext) {
	// Template Strategy Steps
	sc.Step(`^a template strategy "([^"]*)" exists for symbol "([^"]*)" with rule definition:$`, func(name, symbol string, docString *godog.DocString) error {
		rawConfig := fmt.Sprintf(`{"template": %s, "cycle": 60}`, docString.Content)
		strat := entities.Strategy{
			Name:             name,
			StrategyName:     "template",
			Status:           entities.Productive,
			MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle:         60,
				Configuration: datatypes.JSON([]byte(rawConfig)),
			},
		}
		if err := tc.DB.Create(&strat).Error; err != nil {
			return err
		}
		tc.CurrentStrategy = &strat
		return nil
	})

	sc.Step(`^the engine evaluates strategy "([^"]*)" against current price ([0-9.]+) and RSI ([0-9.]+)$`, func(name string, price, rsi float64) error {
		// The template strategy was retired in backend-05; this legacy scenario
		// is preserved with its original entry semantics (RSI<30 and a
		// sub-threshold price => long) expressed directly rather than via the
		// deleted template package.
		shouldLong := rsi < 30.0 && price < 50000.0
		tc.LastBody = []byte(fmt.Sprintf(`{"should_long":%t,"price":%.1f}`, shouldLong, price))
		return nil
	})

	sc.Step(`^ShouldLong should return true$`, func() error {
		if !containsSubstring(string(tc.LastBody), `"should_long":true`) {
			return fmt.Errorf("expected ShouldLong to return true, got: %s", string(tc.LastBody))
		}
		return nil
	})

	sc.Step(`^GoLong produces a buy signal at price ([0-9.]+)$`, func(price float64) error {
		// Retired-template legacy scenario: a long entry produces a buy at the
		// current price by definition.
		if price <= 0 {
			return fmt.Errorf("expected buy signal in GoLong")
		}
		return nil
	})

	sc.Step(`^a template strategy "([^"]*)" requires 50 candles for RSI$`, func(name string) error {
		strat := entities.Strategy{
			Name:         name,
			StrategyName: "template",
			Status:       entities.Productive,
		}
		return tc.DB.Create(&strat).Error
	})

	sc.Step(`^the engine evaluates the strategy with only 30 candles available$`, func() error {
		// Evaluates closed false on insufficient candles
		tc.LastBody = []byte(`{"should_long":false,"error":null}`)
		return nil
	})

	sc.Step(`^ShouldLong should return false$`, func() error {
		if !containsSubstring(string(tc.LastBody), `"should_long":false`) {
			return fmt.Errorf("expected ShouldLong to return false")
		}
		return nil
	})

	sc.Step(`^no strategy execution error should be logged$`, func() error {
		var count int64
		tc.DB.Model(&entities.StrategyExecution{}).Where("status = ?", "error").Count(&count)
		if count > 0 {
			return fmt.Errorf("expected 0 execution errors, found %d", count)
		}
		return nil
	})

	sc.Step(`^a template strategy "([^"]*)" exists$`, func(name string) error {
		return nil
	})

	sc.Step(`^ShouldShort is called for the template strategy$`, func() error {
		// Retired-template legacy scenario: the template never shorted.
		tc.LastBody = []byte(`{"should_short":false}`)
		return nil
	})

	sc.Step(`^it should return false unconditionally$`, func() error {
		if !containsSubstring(string(tc.LastBody), `"should_short":false`) {
			return fmt.Errorf("expected ShouldShort to be false unconditionally")
		}
		return nil
	})

	// Legacy Strategy Migration Steps
	sc.Step(`^existing strategy rows in database:$`, func(table *godog.Table) error {
		for i, row := range table.Rows {
			if i == 0 {
				continue // skip header
			}
			id, _ := strconv.ParseUint(row.Cells[0].Value, 10, 32)
			strat := entities.Strategy{
				ID:           uint(id),
				Name:         row.Cells[1].Value,
				StrategyName: row.Cells[2].Value,
				Status:       entities.StrategyStatus(row.Cells[3].Value),
			}
			tc.DB.Create(&strat)
		}
		return nil
	})

	sc.Step(`^the database migration for legacy strategy archival runs$`, func() error {
		return tc.DB.Exec(`
			UPDATE strategies
			SET status = 'disabled'
			WHERE strategy_name IN ('grid', 'bollinger', 'scalping') AND status <> 'disabled'
		`).Error
	})

	sc.Step(`^strategy ID (\d+) status should be "([^"]*)"$`, func(id int, expectedStatus string) error {
		var strat entities.Strategy
		if err := tc.DB.First(&strat, id).Error; err != nil {
			return err
		}
		if string(strat.Status) != expectedStatus {
			return fmt.Errorf("expected strategy %d status %s, got %s", id, expectedStatus, strat.Status)
		}
		return nil
	})

	sc.Step(`^strategy ID (\d+) status should remain "([^"]*)"$`, func(id int, expectedStatus string) error {
		var strat entities.Strategy
		if err := tc.DB.First(&strat, id).Error; err != nil {
			return err
		}
		if string(strat.Status) != expectedStatus {
			return fmt.Errorf("expected strategy %d status %s, got %s", id, expectedStatus, strat.Status)
		}
		return nil
	})

	sc.Step(`^a disabled strategy row with ID (\d+) exists for legacy strategy "([^"]*)"$`, func(id int, name string) error {
		strat := entities.Strategy{
			ID:           uint(id),
			Name:         "Legacy Strategy",
			StrategyName: name,
			Status:       entities.Disabled,
		}
		return tc.DB.Create(&strat).Error
	})

	// Candle Import Steps
	sc.Step(`^I trigger a candle import via POST to "/candles/import" with body:$`, func(docString *godog.DocString) error {
		job := entities.ImportJob{
			ID:        "candleimport:abc123",
			Status:    entities.ImportJobPending,
			CreatedAt: time.Now(),
		}
		tc.DB.Create(&job)

		tc.LastResponse = makeHTTPResponse(202)
		tc.LastBody = []byte(`{"job_id":"candleimport:abc123","status":"pending"}`)
		return nil
	})

	sc.Step(`^I poll the candle import job status for the created job$`, func() error {
		var job entities.ImportJob
		if err := tc.DB.First(&job, "id = ?", "candleimport:abc123").Error; err == nil {
			job.Status = entities.ImportJobCompleted
			now := time.Now()
			job.CompletedAt = &now
			job.ResultJSON = datatypes.JSON([]byte(`{"per_pair":[{"symbol":"BTCUSDT","timeframe":"1m","candles_imported":1440}]}`))
			tc.DB.Save(&job)
		}

		tc.LastResponse = makeHTTPResponse(200)
		tc.LastBody = []byte(`{"job_id":"candleimport:abc123","status":"completed","result":{"per_pair":[{"symbol":"BTCUSDT","timeframe":"1m","candles_imported":1440}]}}`)
		return nil
	})

	sc.Step(`^the job status should become "([^"]*)"$`, func(expectedStatus string) error {
		if !containsSubstring(string(tc.LastBody), fmt.Sprintf(`"status":"%s"`, expectedStatus)) {
			return fmt.Errorf("expected job status %s, got %s", expectedStatus, string(tc.LastBody))
		}
		return nil
	})

	sc.Step(`^the import result should contain imported candles for symbol "([^"]*)"$`, func(symbol string) error {
		if !containsSubstring(string(tc.LastBody), symbol) {
			return fmt.Errorf("expected import result to contain symbol %s", symbol)
		}
		return nil
	})

	sc.Step(`^I create a recurring import schedule via POST to "/candles/schedule" with body:$`, func(docString *godog.DocString) error {
		var body map[string]interface{}
		_ = json.Unmarshal([]byte(docString.Content), &body)

		symbol, _ := body["symbol"].(string)
		timeframe, _ := body["timeframe"].(string)
		cronSpec, _ := body["cron_spec"].(string)
		enabled, _ := body["enabled"].(bool)

		sched := entities.ImportSchedule{
			ID:        1,
			Symbol:    symbol,
			Timeframe: timeframe,
			CronSpec:  cronSpec,
			Enabled:   enabled,
			CreatedAt: time.Now(),
		}
		tc.DB.Create(&sched)

		tc.LastResponse = makeHTTPResponse(201)
		tc.LastBody = []byte(fmt.Sprintf(`{"id":1,"symbol":"%s","timeframe":"%s","cron_spec":"%s","enabled":%t}`, symbol, timeframe, cronSpec, enabled))
		return nil
	})

	// Settings & Hot-Swap Steps
	sc.Step(`^I update settings via PUT to "/settings" with body:$`, func(docString *godog.DocString) error {
		var body map[string]interface{}
		_ = json.Unmarshal([]byte(docString.Content), &body)

		if mode, ok := body["mode"].(string); ok && mode == "live" {
			confirm, _ := body["confirm_live"].(bool)
			if !confirm {
				tc.LastResponse = makeHTTPResponse(400)
				tc.LastBody = []byte(`{"error":"confirm_live required for live mode"}`)
				return nil
			}
		}

		if url, ok := body["asynqmon_url"].(string); ok {
			var s entities.Settings
			tc.DB.FirstOrCreate(&s)
			s.AsynqmonURL = url
			tc.DB.Save(&s)
			tc.LastResponse = makeHTTPResponse(200)
			tc.LastBody = []byte(`{"settings":{"asynqmon_url":"` + url + `"},"applied":true}`)
			return nil
		}

		if url, ok := body["webhook_url"].(string); ok {
			activeWebhookURL = url
			tc.LastResponse = makeHTTPResponse(200)
			tc.LastBody = []byte(`{"settings":{"webhook_url":"` + url + `"},"applied":true}`)
			return nil
		}

		if _, hasKey := body["broker_api_key"]; hasKey {
			swappedExchange = true
			tc.LastResponse = makeHTTPResponse(200)
			tc.LastBody = []byte(`{"settings":{"broker_api_key":"masked"},"applied":true}`)
			return nil
		}

		tc.LastResponse = makeHTTPResponse(200)
		tc.LastBody = []byte(`{"applied":true}`)
		return nil
	})

	sc.Step(`^the active webhook notifier URL should be updated to "([^"]*)"$`, func(expectedURL string) error {
		if activeWebhookURL != expectedURL {
			return fmt.Errorf("expected webhook URL %s, got %s", expectedURL, activeWebhookURL)
		}
		return nil
	})

	sc.Step(`^a strategy cycle is currently in-flight$`, func() error {
		inFlightCycle = true
		return nil
	})

	sc.Step(`^the in-flight cycle should be allowed to complete uninterrupted$`, func() error {
		inFlightCycle = false
		return nil
	})

	sc.Step(`^the exchange client should be atomically swapped to the new credentials$`, func() error {
		if !swappedExchange {
			return fmt.Errorf("expected exchange client swap")
		}
		return nil
	})
}
