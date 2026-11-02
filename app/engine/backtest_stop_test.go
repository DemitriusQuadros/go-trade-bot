package engine

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type settleCall struct {
	symbol     string
	strategyID uint
	payment    float32
	newLiq     float32
	at         time.Time
}

type fakeBacktestSignals struct {
	open    entities.Signal
	sells   []usecase.ExitSignal
	settled []settleCall
}

func (f *fakeBacktestSignals) GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error) {
	return f.open, nil
}

func (f *fakeBacktestSignals) GenerateSellSignal(e usecase.ExitSignal) error {
	f.sells = append(f.sells, e)
	return nil
}

func (f *fakeBacktestSignals) SettleFunding(symbol string, strategyID uint, payment float32, newLiq float32, at time.Time) error {
	f.settled = append(f.settled, settleCall{symbol, strategyID, payment, newLiq, at})
	return nil
}

func openSignalWithStop(stopID string) entities.Signal {
	return entities.Signal{
		ID:     1,
		Status: entities.Open,
		Orders: []entities.Order{{StopLossOrderID: stopID, StopLossPrice: 95, Quantity: 1}},
	}
}

func TestBacktestStopEvaluator_ClosesPositionWhenStopFilled(t *testing.T) {
	ex := NewSimulatedFillExchange(nil, FillPolicy{})
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ex.SetSimulatedTime(t0, exchange.Candle{OpenTime: t0, Open: 100, High: 101, Low: 99, Close: 100})

	res, err := ex.PlaceOrder(context.Background(), exchange.PlaceOrderRequest{
		Symbol: "SOLUSDT", Side: exchange.SideSell, Type: exchange.OrderTypeStopMarket,
		Quantity: 1, StopPrice: 95,
	})
	require.NoError(t, err)

	sigs := &fakeBacktestSignals{open: openSignalWithStop(res.BrokerOrderID)}
	ev := NewBacktestStopEvaluator(sigs, ex)
	strat := entities.Strategy{ID: 7, Name: "s"}

	// A candle that does not touch the stop leaves the position alone.
	t1 := t0.Add(5 * time.Minute)
	ex.SetSimulatedTime(t1, exchange.Candle{OpenTime: t1, Open: 100, High: 102, Low: 97, Close: 101})
	require.NoError(t, ev.BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
	assert.Empty(t, sigs.sells)

	// A candle whose Low pierces the stop closes it at the stop's fill price.
	t2 := t1.Add(5 * time.Minute)
	ex.SetSimulatedTime(t2, exchange.Candle{OpenTime: t2, Open: 100, High: 100, Low: 90, Close: 92})
	require.NoError(t, ev.BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
	require.Len(t, sigs.sells, 1)
	assert.Equal(t, "stop_loss", sigs.sells[0].ExitReason)
	assert.Equal(t, uint(7), sigs.sells[0].StrategyID)
	assert.InDelta(t, 95.0, float64(sigs.sells[0].ExitPrice), 1e-6)
}

func TestBacktestStopEvaluator_NoOpWithoutOpenPositionOrStop(t *testing.T) {
	ex := NewSimulatedFillExchange(nil, FillPolicy{})
	strat := entities.Strategy{ID: 1}

	sigs := &fakeBacktestSignals{}
	require.NoError(t, NewBacktestStopEvaluator(sigs, ex).BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
	assert.Empty(t, sigs.sells)

	sigs = &fakeBacktestSignals{open: openSignalWithStop("")}
	require.NoError(t, NewBacktestStopEvaluator(sigs, ex).BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
	assert.Empty(t, sigs.sells)

	// An unknown stop id (e.g. already cancelled) is not an error.
	sigs = &fakeBacktestSignals{open: openSignalWithStop("sim_ord_999")}
	require.NoError(t, NewBacktestStopEvaluator(sigs, ex).BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
	assert.Empty(t, sigs.sells)
}

func TestSimulatedFillExchange_TriggeredStopIsNotRetriggered(t *testing.T) {
	ex := NewSimulatedFillExchange(nil, FillPolicy{})
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ex.SetSimulatedTime(t0, exchange.Candle{OpenTime: t0, Close: 100})

	res, err := ex.PlaceOrder(context.Background(), exchange.PlaceOrderRequest{
		Symbol: "SOLUSDT", Side: exchange.SideSell, Type: exchange.OrderTypeStopMarket,
		Quantity: 1, StopPrice: 95, ClientOrderID: "gtb-stop-1",
	})
	require.NoError(t, err)

	t1 := t0.Add(5 * time.Minute)
	ex.SetSimulatedTime(t1, exchange.Candle{OpenTime: t1, Low: 90, Close: 92})
	first, err := ex.GetOrder(context.Background(), "SOLUSDT", res.BrokerOrderID)
	require.NoError(t, err)
	assert.Equal(t, exchange.OrderStatusFilled, first.Status)

	// Both id aliases are gone, so a later candle cannot overwrite the fill.
	t2 := t1.Add(5 * time.Minute)
	ex.SetSimulatedTime(t2, exchange.Candle{OpenTime: t2, Low: 80, Close: 81})
	again, err := ex.GetOrder(context.Background(), "SOLUSDT", res.BrokerOrderID)
	require.NoError(t, err)
	assert.Equal(t, first.FilledAt, again.FilledAt)
	assert.Empty(t, ex.restingOrders)
}

func TestBacktestStopEvaluator_LiquidationPriority_Long(t *testing.T) {
	strat := entities.Strategy{ID: 7, Name: "s"}
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("liq above stop: liquidation executes first", func(t *testing.T) {
		ex := NewSimulatedFillExchange(nil, FillPolicy{})
		ex.SetSimulatedTime(t0, exchange.Candle{OpenTime: t0, Open: 100, High: 100, Low: 85, Close: 86})

		sigs := &fakeBacktestSignals{open: entities.Signal{
			ID:           1,
			Status:       entities.Open,
			PositionSide: entities.PositionSideLong,
			Orders: []entities.Order{{
				PositionSide:     entities.PositionSideLong,
				StopLossOrderID:  "stop_1",
				StopLossPrice:    90,
				LiquidationPrice: 95,
				Leverage:         10,
				MarginType:       entities.Isolated,
				Quantity:         1,
				EntryPrice:       100,
				CreatedAt:        t0,
			}},
		}}
		ev := NewBacktestStopEvaluator(sigs, ex)

		require.NoError(t, ev.BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
		require.Len(t, sigs.sells, 1)
		assert.Equal(t, entities.ExitReasonLiquidation, sigs.sells[0].ExitReason)
		assert.InDelta(t, 95.0, float64(sigs.sells[0].ExitPrice), 1e-4)
	})

	t.Run("stop above liq: stop executes first", func(t *testing.T) {
		ex := NewSimulatedFillExchange(nil, FillPolicy{})
		ex.SetSimulatedTime(t0, exchange.Candle{OpenTime: t0, Open: 100, High: 101, Low: 99, Close: 100})
		res, err := ex.PlaceOrder(context.Background(), exchange.PlaceOrderRequest{
			Symbol: "SOLUSDT", Side: exchange.SideSell, Type: exchange.OrderTypeStopMarket,
			Quantity: 1, StopPrice: 95,
		})
		require.NoError(t, err)

		sigs := &fakeBacktestSignals{open: entities.Signal{
			ID:           1,
			Status:       entities.Open,
			PositionSide: entities.PositionSideLong,
			Orders: []entities.Order{{
				PositionSide:     entities.PositionSideLong,
				StopLossOrderID:  res.BrokerOrderID,
				StopLossPrice:    95,
				LiquidationPrice: 90,
				Leverage:         10,
				MarginType:       entities.Isolated,
				Quantity:         1,
				EntryPrice:       100,
				CreatedAt:        t0,
			}},
		}}
		ev := NewBacktestStopEvaluator(sigs, ex)

		t1 := t0.Add(5 * time.Minute)
		ex.SetSimulatedTime(t1, exchange.Candle{OpenTime: t1, Open: 98, High: 98, Low: 85, Close: 86})

		require.NoError(t, ev.BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
		require.Len(t, sigs.sells, 1)
		assert.Equal(t, "stop_loss", sigs.sells[0].ExitReason)
	})
}

func TestBacktestStopEvaluator_LiquidationPriority_Short(t *testing.T) {
	strat := entities.Strategy{ID: 7, Name: "s"}
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("liq below stop: liquidation executes first", func(t *testing.T) {
		ex := NewSimulatedFillExchange(nil, FillPolicy{})
		ex.SetSimulatedTime(t0, exchange.Candle{OpenTime: t0, Open: 100, High: 115, Low: 99, Close: 114})

		sigs := &fakeBacktestSignals{open: entities.Signal{
			ID:           1,
			Status:       entities.Open,
			PositionSide: entities.PositionSideShort,
			Orders: []entities.Order{{
				PositionSide:     entities.PositionSideShort,
				StopLossOrderID:  "stop_short_1",
				StopLossPrice:    110,
				LiquidationPrice: 105,
				Leverage:         10,
				MarginType:       entities.Isolated,
				Quantity:         1,
				EntryPrice:       100,
				CreatedAt:        t0,
			}},
		}}
		ev := NewBacktestStopEvaluator(sigs, ex)

		require.NoError(t, ev.BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
		require.Len(t, sigs.sells, 1)
		assert.Equal(t, entities.ExitReasonLiquidation, sigs.sells[0].ExitReason)
		assert.InDelta(t, 105.0, float64(sigs.sells[0].ExitPrice), 1e-4)
	})

	t.Run("stop below liq: stop executes first", func(t *testing.T) {
		ex := NewSimulatedFillExchange(nil, FillPolicy{})
		ex.SetSimulatedTime(t0, exchange.Candle{OpenTime: t0, Open: 100, High: 101, Low: 99, Close: 100})
		res, err := ex.PlaceOrder(context.Background(), exchange.PlaceOrderRequest{
			Symbol: "SOLUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeStopMarket,
			Quantity: 1, StopPrice: 105,
		})
		require.NoError(t, err)

		sigs := &fakeBacktestSignals{open: entities.Signal{
			ID:           1,
			Status:       entities.Open,
			PositionSide: entities.PositionSideShort,
			Orders: []entities.Order{{
				PositionSide:     entities.PositionSideShort,
				StopLossOrderID:  res.BrokerOrderID,
				StopLossPrice:    105,
				LiquidationPrice: 110,
				Leverage:         10,
				MarginType:       entities.Isolated,
				Quantity:         1,
				EntryPrice:       100,
				CreatedAt:        t0,
			}},
		}}
		ev := NewBacktestStopEvaluator(sigs, ex)

		t1 := t0.Add(5 * time.Minute)
		ex.SetSimulatedTime(t1, exchange.Candle{OpenTime: t1, Open: 102, High: 115, Low: 101, Close: 112})

		require.NoError(t, ev.BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
		require.Len(t, sigs.sells, 1)
		assert.Equal(t, "stop_loss", sigs.sells[0].ExitReason)
	})
}

func TestBacktestStopEvaluator_FundingSettlement(t *testing.T) {
	strat := entities.Strategy{ID: 7, Name: "s"}
	entryT := time.Date(2025, 1, 1, 7, 0, 0, 0, time.UTC)
	candle8h := exchange.Candle{
		OpenTime: time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC),
		Open:     100, High: 101, Low: 99, Close: 100,
	}

	ex := NewSimulatedFillExchange(nil, FillPolicy{})
	ex.SetSimulatedTime(candle8h.OpenTime, candle8h)

	sigs := &fakeBacktestSignals{open: entities.Signal{
		ID:           1,
		Status:       entities.Open,
		PositionSide: entities.PositionSideLong,
		Orders: []entities.Order{{
			PositionSide:     entities.PositionSideLong,
			Quantity:         2,
			EntryPrice:       100,
			Leverage:         5,
			MarginType:       entities.Isolated,
			LiquidationPrice: 80,
			CreatedAt:        entryT,
		}},
	}}

	ev := NewBacktestStopEvaluator(sigs, ex)
	ev.FundingRateLookup = func(symbol string, at time.Time) (float64, error) {
		return 0.0002, nil
	}

	require.NoError(t, ev.BeforeCycle(context.Background(), strat, "SOLUSDT", strategies.ModeBacktest))
	require.Len(t, sigs.settled, 1)
	assert.Equal(t, "SOLUSDT", sigs.settled[0].symbol)
	assert.Equal(t, uint(7), sigs.settled[0].strategyID)
	assert.True(t, sigs.settled[0].payment > 0)
	assert.Equal(t, time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC), sigs.settled[0].at)
}
