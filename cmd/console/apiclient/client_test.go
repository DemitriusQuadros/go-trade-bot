package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Ping(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/account", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AccountView{ID: 1, Amount: 1000})
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	err := client.Ping(context.Background())
	assert.NoError(t, err)
}

func TestClient_GetAccount(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/account", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AccountView{
			ID:              1,
			Amount:          12480.55,
			AvailableOrders: 9120,
			Currency:        "USDT",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	acc, err := client.GetAccount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), acc.ID)
	assert.Equal(t, float32(12480.55), acc.Amount)
	assert.Equal(t, "USDT", acc.Currency)
}

func TestClient_ListStrategies_And_GetStrategy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/strategy" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]StrategyView{
				{ID: 1, Name: "grid_btc", Status: "productive", Mode: "live"},
				{ID: 2, Name: "scalp_eth", Status: "testing", Mode: "dryrun"},
			})
			return
		}
		if r.URL.Path == "/strategy/1" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(StrategyView{
				ID: 1, Name: "grid_btc", Status: "productive", Mode: "live",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	list, err := client.ListStrategies(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 2)

	strat, err := client.GetStrategy(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "grid_btc", strat.Name)
}

func TestClient_UpdateStrategyStatus_And_Mode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/strategy/1/status" && r.Method == http.MethodPatch {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "disabled", body["status"])
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(StrategyView{ID: 1, Name: "grid_btc", Status: "disabled"})
			return
		}
		if r.URL.Path == "/strategy/1/mode" && r.Method == http.MethodPatch {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "paper", body["mode"])
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(StrategyView{ID: 1, Name: "grid_btc", Mode: "paper"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	s1, err := client.UpdateStrategyStatus(context.Background(), 1, "disabled")
	require.NoError(t, err)
	assert.Equal(t, "disabled", s1.Status)

	s2, err := client.UpdateStrategyMode(context.Background(), 1, "paper")
	require.NoError(t, err)
	assert.Equal(t, "paper", s2.Mode)
}

func TestClient_ListTickerPrices_And_Klines(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broker/prices" {
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]TickerPriceView{{Symbol: "BTCUSDT", Price: 61204.10}})
			return
		}
		if r.URL.Path == "/broker/klines" {
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			assert.Equal(t, "1m", r.URL.Query().Get("interval"))
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]CandleView{{Close: 61204.10}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	prices, err := client.ListTickerPrices(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	assert.Len(t, prices, 1)
	assert.Equal(t, 61204.10, prices[0].Price)

	klines, err := client.ListKlines(context.Background(), "BTCUSDT", "1m", 60)
	require.NoError(t, err)
	assert.Len(t, klines, 1)
	assert.Equal(t, 61204.10, klines[0].Close)
}

func TestClient_Signals_And_CloseSignal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/signal" && r.URL.Query().Get("status") == "open" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]SignalView{{ID: 10, Status: "open", Symbol: "BTCUSDT"}})
			return
		}
		if r.URL.Path == "/signal" && r.URL.Query().Get("status") == "" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]SignalView{
				{ID: 10, Status: "open", Symbol: "BTCUSDT"},
				{ID: 9, Status: "closed", Symbol: "ETHUSDT"},
			})
			return
		}
		if r.URL.Path == "/signal/close/10" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	openSigs, err := client.GetOpenSignals(context.Background())
	require.NoError(t, err)
	assert.Len(t, openSigs, 1)

	allSigs, err := client.GetAllSignals(context.Background())
	require.NoError(t, err)
	assert.Len(t, allSigs, 2)

	err = client.CloseSignal(context.Background(), 10)
	assert.NoError(t, err)
}

func TestClient_Backtests(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backtest" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(BacktestRunView{
				ID:           1,
				StrategyID:   5,
				Symbol:       "BTCUSDT",
				Sharpe:       1.5,
				Passed:       true,
				ProfitFactor: "Infinity",
			})
			return
		}
		if r.URL.Path == "/backtest/walkforward" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(BacktestRunView{
				ID:            2,
				StrategyID:    5,
				Symbol:        "BTCUSDT",
				IsWalkForward: true,
			})
			return
		}
		if r.URL.Path == "/backtest/1" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(BacktestRunView{ID: 1, StrategyID: 5})
			return
		}
		if r.URL.Path == "/backtest" && r.URL.Query().Get("strategy_id") == "5" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]BacktestRunView{{ID: 1, StrategyID: 5}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	run, err := client.RunBacktest(context.Background(), RunBacktestRequest{StrategyID: 5, Symbol: "BTCUSDT"})
	require.NoError(t, err)
	assert.Equal(t, uint(1), run.ID)
	assert.Equal(t, "Infinity", run.ProfitFactor)

	wfRun, err := client.RunWalkForward(context.Background(), WalkForwardRequest{StrategyID: 5, Symbol: "BTCUSDT"})
	require.NoError(t, err)
	assert.True(t, wfRun.IsWalkForward)

	b1, err := client.GetBacktest(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, uint(1), b1.ID)

	bList, err := client.ListBacktests(context.Background(), 5)
	require.NoError(t, err)
	assert.Len(t, bList, 1)
}

func TestClient_ErrorsAndRetries(t *testing.T) {
	t.Run("unreachable host returns ErrUnreachable", func(t *testing.T) {
		client := NewClient("http://127.0.0.1:59999") // invalid port
		err := client.Ping(context.Background())
		assert.Error(t, err)
		var unreach ErrUnreachable
		assert.True(t, errors.As(err, &unreach))
	})

	t.Run("API 404 returns ErrAPI", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "strategy not found", http.StatusNotFound)
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		_, err := client.GetStrategy(context.Background(), 999)
		assert.Error(t, err)
		var apiErr ErrAPI
		if assert.True(t, errors.As(err, &apiErr)) {
			assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not-json"))
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		_, err := client.GetAccount(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse account response")
	})
}
