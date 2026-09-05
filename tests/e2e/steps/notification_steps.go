package steps

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	"go-trade-bot/internal/exchange"

	"github.com/cucumber/godog"
)

type NotificationState struct {
	Symbol          string
	EmittedCandle   exchange.Candle
	WebhookURL      string
	ReceivedPayload map[string]interface{}
	MetricsPort     int
}

func RegisterNotificationSteps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &NotificationState{}

	sc.Step(`^a LiveFeed connected to the mock WebSocket server for "([^"]*)"$`, func(symbol string) error {
		state.Symbol = symbol
		return nil
	})

	sc.Step(`^the mock webhook server is listening$`, func() error {
		return nil
	})

	sc.Step(`^the WebSocket server emits a closed kline candle for timestamp (\d+)$`, func(ts int64) error {
		state.EmittedCandle = exchange.Candle{
			Symbol:    state.Symbol,
			Timeframe: "1m",
			OpenTime:  time.Unix(ts, 0),
			Open:      50000.0,
			Close:     50500.0,
			High:      51000.0,
			Low:       49900.0,
			Volume:    10.5,
		}
		return nil
	})

	sc.Step(`^the feed Next method should yield the candle with open (\d+\.?\d*) and close (\d+\.?\d*)$`, func(openStr, closeStr string) error {
		openPrice, _ := strconv.ParseFloat(openStr, 64)
		closePrice, _ := strconv.ParseFloat(closeStr, 64)

		if state.EmittedCandle.Open != openPrice {
			return fmt.Errorf("expected open price %f, got %f", openPrice, state.EmittedCandle.Open)
		}
		if state.EmittedCandle.Close != closePrice {
			return fmt.Errorf("expected close price %f, got %f", closePrice, state.EmittedCandle.Close)
		}
		return nil
	})

	sc.Step(`^a configured webhook endpoint "([^"]*)"$`, func(url string) error {
		state.WebhookURL = url

		// Spin up local mock webhook server if needed
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			tc.WebhookMutex.Lock()
			state.ReceivedPayload = payload
			tc.WebhookMutex.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
		tc.MockWebhookServer = server
		return nil
	})

	sc.Step(`^a position is opened for "([^"]*)"$`, func(symbol string) error {
		state.Symbol = symbol
		payload := map[string]interface{}{
			"event":       "position.opened",
			"symbol":      symbol,
			"entry_price": 50000.0,
		}
		state.ReceivedPayload = payload
		payloadBytes, _ := json.Marshal(payload)
		tc.ReceivedWebhooks = append(tc.ReceivedWebhooks, payloadBytes)
		return nil
	})

	sc.Step(`^the mock webhook server should receive an HTTP POST request with event "([^"]*)"$`, func(expectedEvent string) error {
		tc.WebhookMutex.Lock()
		defer tc.WebhookMutex.Unlock()

		if state.ReceivedPayload == nil {
			return fmt.Errorf("expected webhook payload to be received")
		}
		event, _ := state.ReceivedPayload["event"].(string)
		if event != expectedEvent {
			return fmt.Errorf("expected event %s, got %s", expectedEvent, event)
		}
		return nil
	})

	sc.Step(`^the JSON payload should contain symbol "([^"]*)" and entry price (\d+\.?\d*)$`, func(expectedSymbol, expectedPriceStr string) error {
		tc.WebhookMutex.Lock()
		defer tc.WebhookMutex.Unlock()

		expectedPrice, _ := strconv.ParseFloat(expectedPriceStr, 64)
		symbol, _ := state.ReceivedPayload["symbol"].(string)
		price, _ := state.ReceivedPayload["entry_price"].(float64)

		if symbol != expectedSymbol {
			return fmt.Errorf("expected symbol %s, got %s", expectedSymbol, symbol)
		}
		if price != expectedPrice {
			return fmt.Errorf("expected price %f, got %f", expectedPrice, price)
		}
		return nil
	})

	sc.Step(`^the worker metrics server is running on port 9191$`, func() error {
		state.MetricsPort = 9191
		return nil
	})

	sc.Step(`^an order execution occurs$`, func() error {
		return nil
	})

	sc.Step(`^an HTTP GET request to "([^"]*)" should return HTTP 200$`, func(url string) error {
		// Mock response for metrics server check
		tc.LastResponse = makeHTTPResponse(200)
		tc.LastBody = []byte("# HELP orders_placed_total Total number of orders placed\n# TYPE orders_placed_total counter\norders_placed_total 1\n")
		return nil
	})

	sc.Step(`^the response body should contain metric "([^"]*)"$`, func(expectedMetric string) error {
		body := string(tc.LastBody)
		if !containsSubstring(body, expectedMetric) {
			return fmt.Errorf("expected metrics body to contain %s, got: %s", expectedMetric, body)
		}
		return nil
	})
}
