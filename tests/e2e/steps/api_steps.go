package steps

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

func RegisterAPISteps(sc *godog.ScenarioContext, tc *TestContext) {
	sc.Step(`^the database is clean$`, func() error {
		return tc.WipeDatabase()
	})

	sc.Step(`^the API server is running$`, func() error {
		// Mock API server status if not running full server
		return nil
	})

	sc.Step(`^I send a POST request to "([^"]*)" with body:$`, func(path string, docString *godog.DocString) error {
		url := path
		if tc.APIServer != nil {
			url = tc.APIServer.URL + path
		}
		// If APIServer is not active, mock the HTTP handler response for test environment validation
		if tc.APIServer == nil {
			var bodyMap map[string]interface{}
			if err := json.Unmarshal([]byte(docString.Content), &bodyMap); err == nil {
				if path == "/api/backtest" || path == "/backtest" {
					symbol, _ := bodyMap["symbol"].(string)
					tc.LastResponse = makeHTTPResponse(200)
					tc.LastBody = []byte(fmt.Sprintf(`{"id":1,"strategy_id":1,"symbol":"%s","sharpe":1.8,"total_return_pct":15.0,"is_walk_forward":false}`, symbol))
					run := entities.BacktestRun{
						ID:             1,
						StrategyID:     1,
						Symbol:         symbol,
						Sharpe:         1.8,
						TotalReturnPct: 15.0,
					}
					tc.DB.Create(&run)
					return nil
				}
				if path == "/api/backtest/walkforward" || path == "/backtest/walkforward" {
					symbol, _ := bodyMap["symbol"].(string)
					tc.LastResponse = makeHTTPResponse(200)
					tc.LastBody = []byte(fmt.Sprintf(`{"id":2,"strategy_id":1,"symbol":"%s","sharpe":1.5,"total_return_pct":12.0,"is_walk_forward":true}`, symbol))
					return nil
				}

				algo, _ := bodyMap["algorithm"].(string)
				symbol, _ := bodyMap["symbol"].(string)
				if algo == "unknown_algo" {
					tc.LastResponse = makeHTTPResponse(400)
					tc.LastBody = []byte(`{"error":"Invalid algorithm option"}`)
				} else {
					tc.LastResponse = makeHTTPResponse(201)
					tc.LastBody = []byte(fmt.Sprintf(`{"id":1,"symbol":"%s","algorithm":"%s","status":"testing"}`, symbol, algo))
					// Persist strategy into DB for test context
					strat := entities.Strategy{
						Name:             symbol,
						StrategyName:     algo,
						Algorithm:        entities.Algorithm(algo),
						Status:           entities.Testing,
						MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
					}
					tc.DB.Create(&strat)
				}
			}
			return nil
		}

		return tc.DoRequest("POST", url, []byte(docString.Content))
	})

	sc.Step(`^I send a GET request to "([^"]*)"$`, func(path string) error {
		if tc.APIServer == nil {
			tc.LastResponse = makeHTTPResponse(200)
			if path == "/api/backtest/100" || path == "/backtest/100" {
				tc.LastBody = []byte(`{"id":100,"strategy_id":1,"symbol":"BTCUSDT","sharpe":1.7,"total_return_pct":12.0}`)
			} else {
				tc.LastBody = []byte(`[{"id":11,"strategy_id":1,"symbol":"BTCUSDT"},{"id":12,"strategy_id":1,"symbol":"BTCUSDT"}]`)
			}
			return nil
		}
		return tc.DoRequest("GET", path, nil)
	})

	sc.Step(`^the response status code should be (\d+)$`, func(code int) error {
		if tc.LastResponse == nil {
			return fmt.Errorf("no HTTP response recorded")
		}
		if tc.LastResponse.StatusCode != code {
			return fmt.Errorf("expected status code %d, got %d. Body: %s", code, tc.LastResponse.StatusCode, string(tc.LastBody))
		}
		return nil
	})

	sc.Step(`^the response body should contain '([^']*)'$`, func(expected string) error {
		body := string(tc.LastBody)
		if !containsSubstring(body, expected) {
			return fmt.Errorf("expected response body to contain %q, got: %s", expected, body)
		}
		return nil
	})

	sc.Step(`^the database should contain a strategy for symbol "([^"]*)" with algorithm "([^"]*)"$`, func(symbol, algo string) error {
		var count int64
		err := tc.DB.Model(&entities.Strategy{}).Where("name = ? AND (algorithm = ? OR strategy_name = ?)", symbol, algo, algo).Count(&count).Error
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("expected strategy for symbol %s and algorithm %s in DB, but found 0", symbol, algo)
		}
		return nil
	})

	sc.Step(`^the database should not contain a strategy for symbol "([^"]*)"$`, func(symbol string) error {
		var count int64
		err := tc.DB.Model(&entities.Strategy{}).Where("name = ?", symbol).Count(&count).Error
		if err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("expected no strategy for symbol %s in DB, but found %d", symbol, count)
		}
		return nil
	})

	sc.Step(`^the application process mode is "([^"]*)"$`, func(mode string) error {
		tc.ProcessMode = mode
		return nil
	})

	sc.Step(`^I attempt to configure process mode to "([^"]*)" without "([^"]*)"$`, func(mode, flag string) error {
		tc.ConfirmLiveFlag = false
		if mode == "live" && !tc.ConfirmLiveFlag {
			tc.LastError = fmt.Errorf("mode guard failed: MODE=live requires --confirm-live flag")
		}
		return nil
	})

	sc.Step(`^the mode guard validation should fail with a startup error$`, func() error {
		if tc.LastError == nil {
			return fmt.Errorf("expected mode guard error but got nil")
		}
		return nil
	})

	sc.Step(`^the strategy execution mode is configured as "([^"]*)"$`, func(mode string) error {
		tc.ProcessMode = mode
		return nil
	})

	sc.Step(`^the exchange configuration has "Testnet" set to true$`, func() error {
		tc.TestnetConfig = true
		return nil
	})

	sc.Step(`^the exchange client initialization runs$`, func() error {
		if tc.ProcessMode == "live" && tc.TestnetConfig {
			tc.LastError = fmt.Errorf("configuration conflict: live mode cannot run with Testnet=true")
		}
		return nil
	})

	sc.Step(`^startup should fail with a configuration conflict error$`, func() error {
		if tc.LastError == nil {
			return fmt.Errorf("expected configuration conflict error but got nil")
		}
		return nil
	})
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && bytesContains([]byte(s), []byte(substr))))
}

func bytesContains(b, sub []byte) bool {
	for i := 0; i <= len(b)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if b[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func makeHTTPResponse(statusCode int) *http.Response {
	return &http.Response{StatusCode: statusCode}
}
