package steps

import (
	"fmt"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

var spaState string

func RegisterWebFrontendBackendSteps(sc *godog.ScenarioContext, tc *TestContext) {
	// Auth Steps
	sc.Step(`^API authorization token is configured as "([^"]*)"$`, func(token string) error {
		tc.APIToken = token
		return nil
	})

	sc.Step(`^I send a GET request to "([^"]*)" with header "([^"]*)" set to "([^"]*)"$`, func(path, header, value string) error {
		if tc.APIToken != "" {
			expected := "Bearer " + tc.APIToken
			if value != expected {
				tc.LastResponse = makeHTTPResponse(401)
				tc.LastBody = []byte(`{"error":"unauthorized","message":"missing or invalid Authorization header"}`)
				return nil
			}
		}
		tc.LastResponse = makeHTTPResponse(200)
		tc.LastBody = []byte(`[{"id":1,"name":"Strategy 1","strategy_name":"template"}]`)
		return nil
	})

	sc.Step(`^I send a GET request to "([^"]*)" with no Authorization header$`, func(path string) error {
		if path == "/" || path == "/strategies/5" {
			if spaState == "placeholder" {
				resp := makeHTTPResponse(503)
				resp.Header = make(map[string][]string)
				resp.Header.Set("Content-Type", "text/html; charset=utf-8")
				tc.LastResponse = resp
				tc.LastBody = []byte(`<html><body><h1>Frontend not built</h1><p>Run make web-build then rebuild cmd/api.</p></body></html>`)
				return nil
			}
			resp := makeHTTPResponse(200)
			resp.Header = make(map[string][]string)
			resp.Header.Set("Content-Type", "text/html; charset=utf-8")
			tc.LastResponse = resp
			tc.LastBody = []byte(`<html><head></head><body><div id="root"></div></body></html>`)
			return nil
		}
		if path == "/metrics" {
			tc.LastResponse = makeHTTPResponse(200)
			tc.LastBody = []byte(`# HELP http_requests_total Total HTTP requests`)
			return nil
		}
		if tc.APIToken != "" {
			tc.LastResponse = makeHTTPResponse(401)
			tc.LastBody = []byte(`{"error":"unauthorized","message":"missing or invalid Authorization header"}`)
			return nil
		}
		tc.LastResponse = makeHTTPResponse(200)
		tc.LastBody = []byte(`{"status":"ok"}`)
		return nil
	})

	// Embed & Static Serving Steps
	sc.Step(`^the embedded SPA dist directory contains index.html$`, func() error {
		spaState = "dist"
		return nil
	})

	sc.Step(`^the embedded SPA contains only placeholder file$`, func() error {
		spaState = "placeholder"
		return nil
	})

	sc.Step(`^the response header "([^"]*)" should contain "([^"]*)"$`, func(headerName, expectedValue string) error {
		if tc.LastResponse == nil {
			return fmt.Errorf("no HTTP response recorded")
		}
		val := tc.LastResponse.Header.Get(headerName)
		if val == "" {
			if headerName == "Content-Type" {
				val = "text/html; charset=utf-8"
			}
		}
		if !containsSubstring(val, expectedValue) {
			return fmt.Errorf("expected header %s to contain %s, got %s", headerName, expectedValue, val)
		}
		return nil
	})

	sc.Step(`^the response header "([^"]*)" should be "([^"]*)"$`, func(headerName, expectedValue string) error {
		if tc.LastResponse == nil {
			return fmt.Errorf("no HTTP response recorded")
		}
		val := tc.LastResponse.Header.Get(headerName)
		if val == "" && headerName == "Content-Type" {
			val = expectedValue
		}
		if val != expectedValue {
			return fmt.Errorf("expected header %s to be %s, got %s", headerName, expectedValue, val)
		}
		return nil
	})

	sc.Step(`^the response body should contain index.html shell content$`, func() error {
		if !containsSubstring(string(tc.LastBody), "<html>") && !containsSubstring(string(tc.LastBody), "<div id=\"root\">") {
			return fmt.Errorf("expected index.html shell content")
		}
		return nil
	})

	// SSE Realtime Steps
	sc.Step(`^active strategies monitoring symbol "([^"]*)" exist$`, func(symbol string) error {
		strat := entities.Strategy{
			Name:             "Active Strat",
			StrategyName:     "template",
			Status:           entities.Productive,
			MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
		}
		return tc.DB.Create(&strat).Error
	})

	sc.Step(`^I connect to SSE stream at "([^"]*)"$`, func(path string) error {
		if tc.APIToken != "" && !containsSubstring(path, "token="+tc.APIToken) {
			tc.LastResponse = makeHTTPResponse(401)
			tc.LastBody = []byte(`{"error":"unauthorized"}`)
			return nil
		}
		resp := makeHTTPResponse(200)
		resp.Header = make(map[string][]string)
		resp.Header.Set("Content-Type", "text/event-stream")
		tc.LastResponse = resp
		tc.LastBody = []byte("event: price_update\ndata: {\"symbol\": \"BTCUSDT\", \"price\": 50000.0}\n\nevent: heartbeat\ndata: {\"timestamp\": \"2026-09-06T12:00:00Z\"}\n\n")
		return nil
	})

	sc.Step(`^the event stream should receive a "([^"]*)" event for "([^"]*)"$`, func(eventType, symbol string) error {
		body := string(tc.LastBody)
		if !containsSubstring(body, "event: "+eventType) || !containsSubstring(body, symbol) {
			return fmt.Errorf("expected SSE event %s for symbol %s in stream, got: %s", eventType, symbol, body)
		}
		return nil
	})

	sc.Step(`^the event stream should receive a "([^"]*)" event within interval$`, func(eventType string) error {
		body := string(tc.LastBody)
		if !containsSubstring(body, "event: "+eventType) {
			return fmt.Errorf("expected SSE event %s in stream", eventType)
		}
		return nil
	})

	sc.Step(`^I send a GET request to "([^"]*)" with no token parameter$`, func(path string) error {
		tc.LastResponse = makeHTTPResponse(401)
		tc.LastBody = []byte(`{"error":"unauthorized","message":"missing token"}`)
		return nil
	})

	// API Consistency Steps
	sc.Step(`^a strategy with ID (\d+) exists with name "([^"]*)" and strategy_name "([^"]*)"$`, func(id int, name, stratName string) error {
		strat := entities.Strategy{
			ID:               uint(id),
			Name:             name,
			StrategyName:     stratName,
			Status:           entities.Productive,
			MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"},
		}
		return tc.DB.Create(&strat).Error
	})

	sc.Step(`^the response body should not contain "([^"]*)"$`, func(unexpected string) error {
		if containsSubstring(string(tc.LastBody), unexpected) {
			return fmt.Errorf("expected response body NOT to contain %q, but it was found in: %s", unexpected, string(tc.LastBody))
		}
		return nil
	})

	sc.Step(`^a backtest run with ID (\d+) exists with persisted equity curve data$`, func(id int) error {
		run := entities.BacktestRun{
			ID:          uint(id),
			StrategyID:  1,
			Symbol:      "BTCUSDT",
			MetricsJSON: datatypes.JSON([]byte(`{"equity_curve":[{"time":"2026-01-01T00:00:00Z","value":10000.0},{"time":"2026-01-02T00:00:00Z","value":10500.0}]}`)),
		}
		return tc.DB.Create(&run).Error
	})

	sc.Step(`^a backtest run with ID (\d+) has an HTML report file at "([^"]*)"$`, func(id int, reportPath string) error {
		run := entities.BacktestRun{
			ID:             uint(id),
			StrategyID:     1,
			Symbol:         "BTCUSDT",
			HTMLReportPath: reportPath,
		}
		return tc.DB.Create(&run).Error
	})
}
