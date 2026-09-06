package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is the sole HTTP boundary between cmd/console and the rest of the
// system (ADR-007). Every page/component in cmd/console depends on this
// client, never on app/repository or internal/exchange directly.
type Client struct {
	baseURL        string
	httpClient     *http.Client
	longHttpClient *http.Client
	maxRetries     int
}

// NewClient creates a new apiclient.Client instance.
func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return &Client{
		baseURL:        baseURL,
		httpClient:     &http.Client{Timeout: 3 * time.Second},
		longHttpClient: &http.Client{Timeout: 10 * time.Minute},
		maxRetries:     2,
	}
}

// SetHTTPClient allows overriding http client in tests.
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// SetLongHTTPClient allows overriding long http client in tests.
func (c *Client) SetLongHTTPClient(client *http.Client) {
	c.longHttpClient = client
}

// doRequest performs the HTTP call. For GET requests, retries up to maxRetries
// with exponential backoff on network errors. POST/PATCH requests are not retried.
func (c *Client) doRequest(ctx context.Context, client *http.Client, method, path string, bodyBytes []byte) ([]byte, int, error) {
	fullURL := c.baseURL + path

	isRetryable := method == http.MethodGet
	attempts := 1
	if isRetryable {
		attempts = 1 + c.maxRetries
	}

	backoffs := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			backoff := backoffs[0]
			if i-1 < len(backoffs) {
				backoff = backoffs[i-1]
			}
			select {
			case <-ctx.Done():
				return nil, 0, ErrUnreachable{Cause: ctx.Err()}
			case <-time.After(backoff):
			}
		}

		var bodyReader io.Reader
		if len(bodyBytes) > 0 {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if len(bodyBytes) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if isRetryable {
				continue
			}
			return nil, 0, ErrUnreachable{Cause: err}
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if isRetryable {
				continue
			}
			return nil, 0, ErrUnreachable{Cause: readErr}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return respBody, resp.StatusCode, ErrAPI{
				StatusCode: resp.StatusCode,
				Body:       string(respBody),
			}
		}

		return respBody, resp.StatusCode, nil
	}

	return nil, 0, ErrUnreachable{Cause: lastErr}
}

// --- Connectivity -------------------------------------------------------

// Ping performs a lightweight GET /account and reports reachability only.
func (c *Client) Ping(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/account", nil)
	return err
}

// --- Account (Page 1) -----------------------------------------------------

// GetAccount fetches the account summary via GET /account.
func (c *Client) GetAccount(ctx context.Context) (AccountView, error) {
	var view AccountView
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/account", nil)
	if err != nil {
		return view, err
	}
	if err := json.Unmarshal(body, &view); err != nil {
		return view, fmt.Errorf("failed to parse account response: %w", err)
	}
	return view, nil
}

// --- Strategies (Page 1 right panel, Page 2) -------------------------------

// ListStrategies fetches all strategies via GET /strategy.
func (c *Client) ListStrategies(ctx context.Context) ([]StrategyView, error) {
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/strategy", nil)
	if err != nil {
		return nil, err
	}
	var views []StrategyView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse strategies response: %w", err)
	}
	return views, nil
}

// GetStrategy fetches a single strategy by ID via GET /strategy/{id}.
func (c *Client) GetStrategy(ctx context.Context, id uint) (StrategyView, error) {
	var view StrategyView
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, fmt.Sprintf("/strategy/%d", id), nil)
	if err != nil {
		return view, err
	}
	if err := json.Unmarshal(body, &view); err != nil {
		return view, fmt.Errorf("failed to parse strategy response: %w", err)
	}
	return view, nil
}

// ListStrategyPerformance fetches performance aggregated by symbol via GET /strategy/performance.
func (c *Client) ListStrategyPerformance(ctx context.Context) ([]StrategyPerformanceView, error) {
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/strategy/performance", nil)
	if err != nil {
		return nil, err
	}
	var views []StrategyPerformanceView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse strategy performance response: %w", err)
	}
	return views, nil
}

// UpdateStrategyStatus updates strategy status via PATCH /strategy/{id}/status.
func (c *Client) UpdateStrategyStatus(ctx context.Context, id uint, status string) (StrategyView, error) {
	var view StrategyView
	reqBody, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return view, err
	}

	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodPatch, fmt.Sprintf("/strategy/%d/status", id), reqBody)
	if err != nil {
		return view, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &view); err != nil {
			return view, fmt.Errorf("failed to parse updated strategy response: %w", err)
		}
	}
	return view, nil
}

// UpdateStrategyMode updates strategy mode via PATCH /strategy/{id}/mode.
func (c *Client) UpdateStrategyMode(ctx context.Context, id uint, mode string) (StrategyView, error) {
	var view StrategyView
	reqBody, err := json.Marshal(map[string]string{"mode": mode})
	if err != nil {
		return view, err
	}

	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodPatch, fmt.Sprintf("/strategy/%d/mode", id), reqBody)
	if err != nil {
		return view, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &view); err != nil {
			return view, fmt.Errorf("failed to parse updated strategy response: %w", err)
		}
	}
	return view, nil
}

// --- Market data (Page 1 pairs grid, Page 3 live price polling) -----------

// ListTickerPrices fetches ticker prices for a symbol via GET /broker/prices?symbol={symbol}.
func (c *Client) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPriceView, error) {
	v := url.Values{}
	v.Set("symbol", symbol)
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/broker/prices?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var views []TickerPriceView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse ticker prices response: %w", err)
	}
	return views, nil
}

// ListKlines fetches klines for a symbol via GET /broker/klines?symbol={symbol}&interval={interval}&limit={limit}.
func (c *Client) ListKlines(ctx context.Context, symbol, interval string, limit int) ([]CandleView, error) {
	v := url.Values{}
	v.Set("symbol", symbol)
	v.Set("interval", interval)
	v.Set("limit", strconv.Itoa(limit))
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/broker/klines?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var views []CandleView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse klines response: %w", err)
	}
	return views, nil
}

// --- Positions (Page 3) ----------------------------------------------------

// GetOpenSignals fetches all open signals via GET /signal?status=open.
func (c *Client) GetOpenSignals(ctx context.Context) ([]SignalView, error) {
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/signal?status=open", nil)
	if err != nil {
		return nil, err
	}
	var views []SignalView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse open signals response: %w", err)
	}
	return views, nil
}

// GetAllSignals fetches all signals via GET /signal.
func (c *Client) GetAllSignals(ctx context.Context) ([]SignalView, error) {
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/signal", nil)
	if err != nil {
		return nil, err
	}
	var views []SignalView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse signals response: %w", err)
	}
	return views, nil
}

// CloseSignal closes an open signal via POST /signal/close/{id}.
func (c *Client) CloseSignal(ctx context.Context, id uint) error {
	_, _, err := c.doRequest(ctx, c.httpClient, http.MethodPost, fmt.Sprintf("/signal/close/%d", id), nil)
	return err
}

// --- Backtest (Pages 4/5) --------------------------------------------------

// RunBacktest runs a backtest synchronously via POST /backtest.
func (c *Client) RunBacktest(ctx context.Context, req RunBacktestRequest) (BacktestRunView, error) {
	var view BacktestRunView
	reqBody, err := json.Marshal(req)
	if err != nil {
		return view, err
	}

	body, _, err := c.doRequest(ctx, c.longHttpClient, http.MethodPost, "/backtest", reqBody)
	if err != nil {
		return view, err
	}
	if err := json.Unmarshal(body, &view); err != nil {
		return view, fmt.Errorf("failed to parse backtest response: %w", err)
	}
	return view, nil
}

// RunWalkForward runs walk-forward validation synchronously via POST /backtest/walkforward.
func (c *Client) RunWalkForward(ctx context.Context, req WalkForwardRequest) (BacktestRunView, error) {
	var view BacktestRunView
	reqBody, err := json.Marshal(req)
	if err != nil {
		return view, err
	}

	body, _, err := c.doRequest(ctx, c.longHttpClient, http.MethodPost, "/backtest/walkforward", reqBody)
	if err != nil {
		return view, err
	}
	if err := json.Unmarshal(body, &view); err != nil {
		return view, fmt.Errorf("failed to parse walk-forward response: %w", err)
	}
	return view, nil
}

// GetBacktest fetches a backtest run by ID via GET /backtest/{id}.
func (c *Client) GetBacktest(ctx context.Context, id uint) (BacktestRunView, error) {
	var view BacktestRunView
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, fmt.Sprintf("/backtest/%d", id), nil)
	if err != nil {
		return view, err
	}
	if err := json.Unmarshal(body, &view); err != nil {
		return view, fmt.Errorf("failed to parse backtest run response: %w", err)
	}
	return view, nil
}

// ListBacktests fetches recent backtests for a strategy via GET /backtest?strategy_id={strategyID}.
func (c *Client) ListBacktests(ctx context.Context, strategyID uint) ([]BacktestRunView, error) {
	v := url.Values{}
	v.Set("strategy_id", strconv.Itoa(int(strategyID)))
	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/backtest?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var views []BacktestRunView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse backtest list response: %w", err)
	}
	return views, nil
}

// --- Execution log (Page 6) ------------------------------------------------

// ListStrategyExecutions fetches execution logs via GET /strategy/executions?since={since}.
func (c *Client) ListStrategyExecutions(ctx context.Context, since time.Time) ([]ExecutionEventView, error) {
	v := url.Values{}
	if !since.IsZero() {
		v.Set("since", since.Format(time.RFC3339))
	}
	queryStr := ""
	if len(v) > 0 {
		queryStr = "?" + v.Encode()
	}

	body, _, err := c.doRequest(ctx, c.httpClient, http.MethodGet, "/strategy/executions"+queryStr, nil)
	if err != nil {
		return nil, err
	}
	var views []ExecutionEventView
	if err := json.Unmarshal(body, &views); err != nil {
		return nil, fmt.Errorf("failed to parse executions response: %w", err)
	}
	return views, nil
}
