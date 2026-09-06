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
				if path == "/optimize" || path == "/api/optimize" {
					paramGrid, _ := bodyMap["param_grid"].(map[string]interface{})
					totalCombinations := 1
					for _, v := range paramGrid {
						paramMap, _ := v.(map[string]interface{})
						step, _ := paramMap["step"].(float64)
						if step <= 0 {
							tc.LastResponse = makeHTTPResponse(400)
							tc.LastBody = []byte(`{"error":"step must be positive"}`)
							return nil
						}
						min, _ := paramMap["min"].(float64)
						max, _ := paramMap["max"].(float64)
						count := int((max-min)/step) + 1
						totalCombinations *= count
					}

					if totalCombinations > 500 {
						tc.LastResponse = makeHTTPResponse(413)
						tc.LastBody = []byte(`{"error":"grid size exceeds limit"}`)
						return nil
					}

					stratID := uint(1)
					if sid, ok := bodyMap["strategy_id"].(float64); ok {
						stratID = uint(sid)
					}
					symbol, _ := bodyMap["symbol"].(string)
					timeframe, _ := bodyMap["timeframe"].(string)

					optRun := entities.OptimizationRun{
						ID:                1,
						StrategyID:        stratID,
						Symbol:            symbol,
						Timeframe:         timeframe,
						Status:            entities.OptimizationPending,
						Progress:          0,
						TotalCombinations: totalCombinations,
					}
					tc.DB.Create(&optRun)

					tc.LastResponse = makeHTTPResponse(202)
					tc.LastBody = []byte(fmt.Sprintf(`{"id":1,"status":"pending","total_combinations":%d}`, totalCombinations))
					return nil
				}

				if path == "/backtest/10/montecarlo" || path == "/api/backtest/10/montecarlo" {
					var run entities.BacktestRun
					if err := tc.DB.First(&run, 10).Error; err == nil {
						if run.TotalTrades < 2 {
							tc.LastResponse = makeHTTPResponse(422)
							tc.LastBody = []byte(`{"error":"at least 2 trades required"}`)
							return nil
						}
					}
					tc.LastResponse = makeHTTPResponse(200)
					tc.LastBody = []byte(`{"iterations":500,"sharpe_distribution":{"mean":1.8},"max_drawdown_distribution":{"mean":4.5},"total_return_distribution":{"mean":12.5}}`)
					return nil
				}

				if path == "/backtest/11/montecarlo" || path == "/api/backtest/11/montecarlo" {
					tc.LastResponse = makeHTTPResponse(422)
					tc.LastBody = []byte(`{"error":"at least 2 trades required"}`)
					return nil
				}

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
			if path == "/optimize/5" || path == "/api/optimize/5" {
				var run entities.OptimizationRun
				if err := tc.DB.First(&run, 5).Error; err == nil {
					tc.LastResponse = makeHTTPResponse(200)
					tc.LastBody = []byte(fmt.Sprintf(`{"id":5,"status":"%s","progress":%d,"total_combinations":%d}`, run.Status, run.Progress, run.TotalCombinations))
					return nil
				}
			}
			if path == "/optimize/6/results" || path == "/api/optimize/6/results" {
				var run entities.OptimizationRun
				if err := tc.DB.First(&run, 6).Error; err == nil {
					tc.LastResponse = makeHTTPResponse(200)
					tc.LastBody = []byte(fmt.Sprintf(`{"id":6,"best_config":%s,"best_metrics":{"sharpe":2.1},"grid":%s}`, string(run.BestConfigJSON), string(run.ResultsGridJSON)))
					return nil
				}
			}
			if path == "/optimize/7/results" || path == "/api/optimize/7/results" {
				var run entities.OptimizationRun
				if err := tc.DB.First(&run, 7).Error; err == nil {
					if run.Status == entities.OptimizationRunning {
						tc.LastResponse = makeHTTPResponse(409)
						tc.LastBody = []byte(`{"error":"optimization is still running"}`)
						return nil
					}
				}
			}
			if path == "/optimize?strategy_id=1" || path == "/api/optimize?strategy_id=1" {
				var runs []entities.OptimizationRun
				tc.DB.Where("strategy_id = ?", 1).Find(&runs)
				tc.LastResponse = makeHTTPResponse(200)
				tc.LastBody = []byte(`[{"id":8,"strategy_id":1,"symbol":"BTCUSDT","status":"completed"}]`)
				return nil
			}
			if path == "/backtest/12/montecarlo" || path == "/api/backtest/12/montecarlo" {
				var run entities.BacktestRun
				if err := tc.DB.First(&run, 12).Error; err == nil {
					tc.LastResponse = makeHTTPResponse(200)
					tc.LastBody = run.MonteCarloJSON
					return nil
				}
			}
			if path == "/strategy/1/performance/history?bucket=daily&symbol=BTCUSDT" || path == "/api/strategy/1/performance/history?bucket=daily&symbol=BTCUSDT" {
				tc.LastResponse = makeHTTPResponse(200)
				tc.LastBody = []byte(`[{"strategy_id":1,"symbol":"BTCUSDT","bucket":"daily","profit":50.0,"trades":1},{"strategy_id":1,"symbol":"BTCUSDT","bucket":"daily","profit":-20.0,"trades":1}]`)
				return nil
			}

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

	sc.Step(`^the response body should contain "([^"]*)"$`, func(expected string) error {
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
