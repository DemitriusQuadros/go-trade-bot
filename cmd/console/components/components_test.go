package components

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/cmd/console/apiclient"

	ui "github.com/gizak/termui/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSparklineAndEMA20(t *testing.T) {
	closes := []float64{10, 11, 12, 11, 10, 9, 8, 9, 10, 11, 12, 13, 14, 15, 14, 13, 12, 11, 10, 11, 12, 13}
	ema := ComputeEMA20(closes)
	require.Len(t, ema, len(closes))
	assert.Greater(t, ema[len(ema)-1], 0.0)

	// Test empty / 1-element
	assert.Nil(t, ComputeEMA20(nil))
	assert.Len(t, ComputeEMA20([]float64{5.0}), 1)

	slg := BuildSparkline("BTCUSDT", closes, ui.ColorGreen)
	assert.NotNil(t, slg)
	assert.Equal(t, "BTCUSDT", slg.Title)
}

func TestAccountSummaryComponent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/account" {
			_ = json.NewEncoder(w).Encode(apiclient.AccountView{
				Amount:          10000,
				AvailableOrders: 5000,
				Currency:        "USDT",
			})
			return
		}
		if r.URL.Path == "/signal" {
			_ = json.NewEncoder(w).Encode([]apiclient.SignalView{{ID: 1}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := apiclient.NewClient(ts.URL)
	drawable := AccountSummary(context.Background(), client)
	assert.NotNil(t, drawable)
}

func TestStrategyTableComponents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/strategy" {
			_ = json.NewEncoder(w).Encode([]apiclient.StrategyView{
				{
					ID:           1,
					Name:         "grid_btc",
					StrategyName: "grid",
					Status:       "productive",
					Mode:         "live",
					MonitoredSymbols: []string{"BTCUSDT"},
					StrategyConfiguration: apiclient.StrategyConfigurationView{
						Cycle: 5,
					},
				},
			})
			return
		}
		if r.URL.Path == "/signal" {
			_ = json.NewEncoder(w).Encode([]apiclient.SignalView{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := apiclient.NewClient(ts.URL)
	compact := StrategyTableCompact(context.Background(), client)
	assert.NotNil(t, compact)

	table, list := StrategyTableFull(context.Background(), client)
	assert.NotNil(t, table)
	assert.Len(t, list, 1)
	assert.Equal(t, "grid_btc", list[0].Name)
}
