package backtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	backtesthandler "go-trade-bot/app/handler/web/backtest"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/customerror"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage makes mockCandleRepo satisfy candle.Repository (fix-02 B2).
func (m *mockCandleRepo) Coverage(_ context.Context, symbol string) ([]candle.Coverage, error) {
	byKey := map[string]*candle.Coverage{}
	var keys []string
	for _, c := range m.candles {
		if symbol != "" && c.Symbol != symbol {
			continue
		}
		k := c.Symbol + "|" + c.Timeframe
		cov, ok := byKey[k]
		if !ok {
			cov = &candle.Coverage{Symbol: c.Symbol, Timeframe: c.Timeframe, From: c.OpenTime, To: c.OpenTime}
			byKey[k] = cov
			keys = append(keys, k)
		}
		if c.OpenTime.Before(cov.From) {
			cov.From = c.OpenTime
		}
		if c.OpenTime.After(cov.To) {
			cov.To = c.OpenTime
		}
		cov.Count++
	}
	sort.Strings(keys)
	out := make([]candle.Coverage, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byKey[k])
	}
	return out, nil
}

// noCandleWindow is a range the oscillatingCandles fixture (2024-01-01, 200
// one-minute candles) does not touch.
var (
	noCandleFrom = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	noCandleTo   = time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
)

func TestBacktestUseCase_Run_NoCandlesIsValidationErrorWithCoverage(t *testing.T) {
	uc, _ := newScriptBacktestUseCase(t)
	_, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "15m", StartDate: noCandleFrom, EndDate: noCandleTo,
	})
	require.Error(t, err)
	var ce *customerror.CustomError
	require.True(t, errors.As(err, &ce), "must be a customerror so REST maps it to 400")
	assert.Equal(t, http.StatusBadRequest, ce.Code)
	assert.Equal(t, "no BTCUSDT 15m candles in 2024-06-01..2024-12-01; available: 1m 2024-01-01..2024-01-01 (200)", ce.Message)
}

func TestBacktestUseCase_Run_NoCandlesUnknownSymbol_AvailableNone(t *testing.T) {
	uc, _ := newScriptBacktestUseCase(t)
	_, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "DOGEUSDT", Timeframe: "1h", StartDate: noCandleFrom, EndDate: noCandleTo,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no DOGEUSDT 1h candles in 2024-06-01..2024-12-01; available: none")
}

func TestBacktestUseCase_RunWalkForward_NoCandlesIsValidationError(t *testing.T) {
	uc, _ := newScriptBacktestUseCase(t)
	_, err := uc.RunWalkForward(context.Background(), backtest.WalkForwardRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "15m", StartDate: noCandleFrom, EndDate: noCandleTo,
		TrainMonths: 3, TestMonths: 1,
	})
	var ce *customerror.CustomError
	require.True(t, errors.As(err, &ce), "got %v", err)
	assert.Equal(t, http.StatusBadRequest, ce.Code)
	assert.Contains(t, ce.Message, "available: 1m 2024-01-01..2024-01-01 (200)")

	_, err = uc.RunWalkForwardForStrategy(context.Background(), entities.Strategy{ID: 1, Name: "x", StrategyName: "script"}, backtest.WalkForwardRequest{
		Symbol: "BTCUSDT", Timeframe: "15m", StartDate: noCandleFrom, EndDate: noCandleTo,
	})
	require.True(t, errors.As(err, &ce), "got %v", err)
}

func TestBacktestHandler_NoCandlesReturns400(t *testing.T) {
	uc, _ := newScriptBacktestUseCase(t)
	h := backtesthandler.NewBacktestHandler(uc)
	body, _ := json.Marshal(map[string]any{
		"strategy_id": 1, "symbol": "BTCUSDT", "timeframe": "15m",
		"start_date": noCandleFrom, "end_date": noCandleTo,
	})
	rec := httptest.NewRecorder()
	h.RunBacktest(rec, httptest.NewRequest(http.MethodPost, "/backtest", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "no BTCUSDT 15m candles")
	assert.Contains(t, rec.Body.String(), "available: 1m")

	rec = httptest.NewRecorder()
	body, _ = json.Marshal(map[string]any{
		"strategy_id": 1, "symbol": "BTCUSDT", "timeframe": "15m",
		"start_date": noCandleFrom, "end_date": noCandleTo, "train_months": 3, "test_months": 1,
	})
	h.RunWalkForward(rec, httptest.NewRequest(http.MethodPost, "/backtest/walkforward", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
