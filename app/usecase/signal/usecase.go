package usecase

import (
	"context"
	"errors"
	"fmt"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/notifier"
	"time"
)

const (
	metricOrderDuration = "order_execution_duration_seconds"
	metricOrderErrors   = "order_execution_errors_total"
)

type EntrySignal struct {
	Symbol     string
	StrategyID uint
	// StrategyName/Mode are used for metric labels and webhook payloads
	// (Spec 09/11) - not persisted on Signal/Order, which key on StrategyID.
	StrategyName string
	Mode         string
	EntryPrice   float32
	MarginType   entities.MarginType
	// StopLossPct is the strategy's configured stop_loss_pct, used to submit
	// the real exchange STOP_MARKET order at fill time (Spec 03). A value
	// <= 0 means "no stop-loss configured" - no STOP_MARKET is submitted,
	// unless StopLossPrice below is set.
	StopLossPct float64
	// StopLossPrice, if non-nil, is an explicit stop price supplied by the
	// strategy itself (Spec 05's GoLong hook contract: "if present, engine
	// uses the strategy-supplied price instead" of the stop_loss_pct
	// computation). No Phase 1 ported strategy sets this - the hook
	// contract is honored for forward-compatibility with future strategies.
	StopLossPrice *float64
}

type ExitSignal struct {
	Symbol       string
	StrategyID   uint
	StrategyName string
	Mode         string
	ExitPrice    float32
	// ExitReason documents why the position is being closed - "take_profit" |
	// "stop_loss" | "manual" | "band_cross" (Spec 09's position.closed
	// payload). Defaults to "manual" when unset.
	ExitReason string
}

type SignalRepository interface {
	Create(signal entities.Signal) error
	GetOpenSignals(symbol string, strategyId uint) (entities.Signal, error)
	Update(signal entities.Signal) error
	GetByID(id uint) (entities.Signal, error)
	GetAll() ([]entities.Signal, error)
}
type AccountUseCase interface {
	DeductOrder(entryPrice float32) error
	AddOrder(exitPrice float32) error
	GetDisponibleAmout() (float32, error)
	CanOpenOrder() (bool, error)
}

// SignalUseCase orchestrates trade signal persistence AND real order
// execution (Spec 02). Exchange replaces the old local `Broker` interface
// (Spec 01) - no github.com/adshao/go-binance/v2 type is referenced anywhere
// in this package anymore.
type SignalUseCase struct {
	Repository     SignalRepository
	AccountUseCase AccountUseCase
	Exchange       exchange.ExchangeClient
	Notifier       notifier.NotificationSender
	Metrics        *metrics.MetricsCollector
}

func NewSignalUseCase(
	repository SignalRepository,
	ac AccountUseCase,
	exchangeClient exchange.ExchangeClient,
	notifySender notifier.NotificationSender,
	collector *metrics.MetricsCollector,
) SignalUseCase {
	return SignalUseCase{
		Repository:     repository,
		AccountUseCase: ac,
		Exchange:       exchangeClient,
		Notifier:       notifySender,
		Metrics:        collector,
	}
}

// GenerateBuySignal places a real market BUY order (Spec 02), submits the
// protective STOP_MARKET order immediately after the fill (Spec 03), and
// only then persists the Signal/Order rows - sized to what the exchange
// actually filled, not the caller's pre-trade estimate.
func (s SignalUseCase) GenerateBuySignal(e EntrySignal) error {
	ctx := context.Background()

	canOpen, err := s.AccountUseCase.CanOpenOrder()
	if err != nil {
		return fmt.Errorf("failed to check if order can be opened: %w", err)
	}

	if !canOpen {
		return nil
	}

	openSignal, err := s.Repository.GetOpenSignals(e.Symbol, e.StrategyID)
	if err != nil {
		return err
	}

	if openSignal.ID != 0 {
		return nil
	}

	investedAmount, _ := s.AccountUseCase.GetDisponibleAmout()
	requestedQty := float64(investedAmount) / float64(e.EntryPrice)

	ordinal := time.Now().UnixNano()
	buyClientOrderID := fmt.Sprintf("gtb-%d-buy-%d", e.StrategyID, ordinal)

	start := time.Now()
	result, err := s.Exchange.PlaceOrder(ctx, exchange.PlaceOrderRequest{
		Symbol:        e.Symbol,
		Side:          exchange.SideBuy,
		Type:          exchange.OrderTypeMarket,
		Quantity:      requestedQty,
		ClientOrderID: buyClientOrderID,
	})
	s.observeOrderDuration(e.StrategyName, "buy", start)

	if err != nil {
		s.incrementOrderError(e.StrategyName, classifyOrderErrorReason(err))
		s.notify(ctx, s.errorEvent(e, "PlaceOrder buy failed: "+err.Error()))
		return fmt.Errorf("failed to place buy order for %s: %w", e.Symbol, err)
	}

	if result.Status == exchange.OrderStatusRejected || result.ExecutedQty == 0 {
		s.incrementOrderError(e.StrategyName, "rejected")
		s.notify(ctx, s.errorEvent(e, fmt.Sprintf("buy order rejected for %s (status=%s)", e.Symbol, result.Status)))
		return fmt.Errorf("buy order rejected for symbol %s (status=%s)", e.Symbol, result.Status)
	}

	filledQty := result.ExecutedQty
	fillPrice := result.AvgFillPrice
	spentAmount := float32(filledQty * fillPrice)

	stopLossOrderID, stopLossPrice := s.submitStopLoss(ctx, e, ordinal, filledQty, fillPrice)

	signal := entities.Signal{
		Symbol:     e.Symbol,
		Status:     entities.Open,
		StrategyID: e.StrategyID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Orders: []entities.Order{
			{
				BrokerOrderID:   result.BrokerOrderID,
				StopLossOrderID: stopLossOrderID,
				EntryPrice:      float32(fillPrice),
				ExitPrice:       0,
				Quantity:        float32(filledQty),
				InvestedAmount:  spentAmount,
				MarginType:      e.MarginType,
				EntryFee:        calculateEntryFee(spentAmount),
				ExitFee:         0,
				Leverage:        0,
				ExecutedQty:     float32(filledQty),
				IsClosing:       false,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			},
		},
	}

	err = s.Repository.Create(signal)
	if err != nil {
		return err
	}

	s.AccountUseCase.DeductOrder(spentAmount)

	data := map[string]any{
		"entry_price":     fillPrice,
		"quantity":        filledQty,
		"broker_order_id": result.BrokerOrderID,
	}
	if stopLossPrice != nil {
		data["stop_loss_price"] = *stopLossPrice
	}
	s.notify(ctx, notifier.Event{
		Type:       notifier.EventPositionOpened,
		Timestamp:  time.Now(),
		StrategyID: e.StrategyID,
		Strategy:   e.StrategyName,
		Symbol:     e.Symbol,
		Mode:       e.Mode,
		Message:    fmt.Sprintf("Position opened for %s", e.Symbol),
		Data:       data,
	})

	return nil
}

// submitStopLoss places the protective STOP_MARKET order at position-open
// time (Spec 03). On failure it retries once immediately with the same
// ClientOrderID (a stop placement retry can't cause an unwanted fill, unlike
// a blind entry-order retry), and fires a strategy.error webhook identifying
// the position as unprotected if both attempts fail. It never fails the
// buy itself - the entry position is not rolled back.
func (s SignalUseCase) submitStopLoss(ctx context.Context, e EntrySignal, ordinal int64, filledQty, fillPrice float64) (string, *float64) {
	if e.StopLossPct <= 0 {
		return "", nil
	}

	stopPrice := fillPrice * (1 - e.StopLossPct/100)
	stopClientOrderID := fmt.Sprintf("gtb-%d-stop-%d", e.StrategyID, ordinal)

	req := exchange.PlaceOrderRequest{
		Symbol:        e.Symbol,
		Side:          exchange.SideSell,
		Type:          exchange.OrderTypeStopMarket,
		Quantity:      filledQty,
		StopPrice:     stopPrice,
		ClientOrderID: stopClientOrderID,
	}

	result, err := s.placeStopMarket(ctx, e.StrategyName, req)
	if err != nil || result.Status != exchange.OrderStatusNew {
		// One immediate retry, same ClientOrderID (Spec 03).
		result, err = s.placeStopMarket(ctx, e.StrategyName, req)
	}

	if err != nil || result.Status != exchange.OrderStatusNew {
		reason := "rejected"
		if err != nil {
			reason = classifyOrderErrorReason(err)
		}
		s.incrementOrderError(e.StrategyName, reason)
		s.notify(ctx, s.errorEvent(e, fmt.Sprintf(
			"position opened WITHOUT stop-loss protection for %s after retry", e.Symbol,
		)))
		return "", nil
	}

	return result.BrokerOrderID, &stopPrice
}

func (s SignalUseCase) placeStopMarket(ctx context.Context, strategyName string, req exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	start := time.Now()
	result, err := s.Exchange.PlaceOrder(ctx, req)
	s.observeOrderDuration(strategyName, "stop_market", start)
	return result, err
}

// GenerateSellSignal closes an open position with a real market SELL order
// (Spec 02). If a stop-loss order still rests on the exchange, it is
// cancelled first (Spec 03) - if the cancel fails because the stop already
// triggered on the exchange, the position is reconciled as closed via the
// stop's own fill data instead of placing a second sell.
func (s SignalUseCase) GenerateSellSignal(e ExitSignal) error {
	ctx := context.Background()

	openSignal, err := s.Repository.GetOpenSignals(e.Symbol, e.StrategyID)
	if err != nil {
		return err
	}

	if openSignal.ID == 0 {
		return fmt.Errorf("signal not found for symbol %s and strategy ID %d", e.Symbol, e.StrategyID)
	}

	order := openSignal.Orders[0]

	if order.StopLossOrderID != "" {
		cancelErr := s.Exchange.CancelOrder(ctx, e.Symbol, order.StopLossOrderID)
		if cancelErr != nil {
			if errors.Is(cancelErr, exchange.ErrOrderNotFound) {
				return s.reconcileAlreadyStoppedPosition(ctx, e, openSignal)
			}
			s.notify(ctx, s.errorEventExit(e, fmt.Sprintf(
				"failed to cancel resting stop-loss order for %s: %v", e.Symbol, cancelErr,
			)))
			return fmt.Errorf("failed to cancel resting stop-loss order for %s: %w", e.Symbol, cancelErr)
		}
	}

	sellClientOrderID := fmt.Sprintf("gtb-%d-sell-%d", e.StrategyID, openSignal.ID)

	start := time.Now()
	result, err := s.Exchange.PlaceOrder(ctx, exchange.PlaceOrderRequest{
		Symbol:        e.Symbol,
		Side:          exchange.SideSell,
		Type:          exchange.OrderTypeMarket,
		Quantity:      float64(order.Quantity),
		ClientOrderID: sellClientOrderID,
	})
	s.observeOrderDuration(e.StrategyName, "sell", start)

	if err != nil {
		s.incrementOrderError(e.StrategyName, classifyOrderErrorReason(err))
		s.notify(ctx, s.errorEventExit(e, "PlaceOrder sell failed: "+err.Error()))
		// Signal.Status remains Open - a subsequent cycle retries the close.
		return fmt.Errorf("failed to place sell order for %s: %w", e.Symbol, err)
	}

	if result.Status == exchange.OrderStatusRejected || result.ExecutedQty == 0 {
		s.incrementOrderError(e.StrategyName, "rejected")
		s.notify(ctx, s.errorEventExit(e, fmt.Sprintf("sell order rejected for %s (status=%s)", e.Symbol, result.Status)))
		return fmt.Errorf("sell order rejected for symbol %s (status=%s)", e.Symbol, result.Status)
	}

	exitPrice := float32(result.AvgFillPrice)
	exitReason := exitReasonOrDefault(e.ExitReason)

	openSignal.Status = entities.SignalStatus(entities.Closed)
	openSignal.Orders[0].ExitPrice = exitPrice
	openSignal.Orders[0].ExitFee = calculateExitFee(openSignal.Orders[0], exitPrice)
	openSignal.Orders[0].UpdatedAt = time.Now()
	openSignal.Orders[0].IsClosing = true
	profit := (exitPrice - openSignal.Orders[0].EntryPrice) * float32(openSignal.Orders[0].Quantity)
	profit = profit - (openSignal.Orders[0].ExitFee + openSignal.Orders[0].EntryFee)
	openSignal.Orders[0].Profit = profit

	err = s.Repository.Update(openSignal)
	if err != nil {
		return err
	}

	if err := s.AccountUseCase.AddOrder(openSignal.Orders[0].InvestedAmount + profit); err != nil {
		return err
	}

	s.notify(ctx, notifier.Event{
		Type:       notifier.EventPositionClosed,
		Timestamp:  time.Now(),
		StrategyID: e.StrategyID,
		Strategy:   e.StrategyName,
		Symbol:     e.Symbol,
		Mode:       e.Mode,
		Message:    fmt.Sprintf("Position closed for %s", e.Symbol),
		Data: map[string]any{
			"entry_price": openSignal.Orders[0].EntryPrice,
			"exit_price":  exitPrice,
			"quantity":    openSignal.Orders[0].Quantity,
			"profit":      profit,
			"exit_reason": exitReason,
		},
	})

	return nil
}

// reconcileAlreadyStoppedPosition handles the race where the resting stop
// already triggered on the exchange between the last price read and the
// cancel attempt (Spec 03). It queries the stop order's own fill data and
// closes the Signal using it, rather than placing a second sell.
func (s SignalUseCase) reconcileAlreadyStoppedPosition(ctx context.Context, e ExitSignal, openSignal entities.Signal) error {
	order := openSignal.Orders[0]

	stopResult, err := s.Exchange.GetOrder(ctx, e.Symbol, order.StopLossOrderID)
	if err != nil {
		s.notify(ctx, s.errorEventExit(e, fmt.Sprintf(
			"could not reconcile already-triggered stop-loss for %s: %v", e.Symbol, err,
		)))
		return fmt.Errorf("failed to reconcile stop-loss order for %s: %w", e.Symbol, err)
	}

	exitPrice := float32(stopResult.AvgFillPrice)

	openSignal.Status = entities.SignalStatus(entities.Closed)
	openSignal.Orders[0].ExitPrice = exitPrice
	openSignal.Orders[0].ExitFee = calculateExitFee(openSignal.Orders[0], exitPrice)
	openSignal.Orders[0].UpdatedAt = time.Now()
	openSignal.Orders[0].IsClosing = true
	profit := (exitPrice - openSignal.Orders[0].EntryPrice) * float32(openSignal.Orders[0].Quantity)
	profit = profit - (openSignal.Orders[0].ExitFee + openSignal.Orders[0].EntryFee)
	openSignal.Orders[0].Profit = profit

	if err := s.Repository.Update(openSignal); err != nil {
		return err
	}
	if err := s.AccountUseCase.AddOrder(openSignal.Orders[0].InvestedAmount + profit); err != nil {
		return err
	}

	s.notify(ctx, notifier.Event{
		Type:       notifier.EventPositionClosed,
		Timestamp:  time.Now(),
		StrategyID: e.StrategyID,
		Strategy:   e.StrategyName,
		Symbol:     e.Symbol,
		Mode:       e.Mode,
		Message:    fmt.Sprintf("Position closed for %s via exchange stop-loss (reconciled on race)", e.Symbol),
		Data: map[string]any{
			"entry_price": openSignal.Orders[0].EntryPrice,
			"exit_price":  exitPrice,
			"quantity":    openSignal.Orders[0].Quantity,
			"profit":      profit,
			"exit_reason": "stop_loss",
			"reconciled":  true,
		},
	})

	return nil
}

func exitReasonOrDefault(reason string) string {
	if reason == "" {
		return "manual"
	}
	return reason
}

func calculateEntryFee(InvestedAmount float32) float32 {
	feePct := 0.1
	fee := float32(float64(InvestedAmount) * feePct / 100)
	return fee
}

func calculateExitFee(order entities.Order, sellPrice float32) float32 {
	feePct := 0.1
	total := float64(order.Quantity) * float64(sellPrice)
	fee := float32(total * feePct / 100)
	return fee
}

func (s SignalUseCase) GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error) {
	openSignal, err := s.Repository.GetOpenSignals(symbol, strategyId)
	if err != nil {
		return entities.Signal{}, err
	}
	if openSignal.ID == 0 {
		return entities.Signal{}, nil
	}
	return openSignal, nil
}

func (s SignalUseCase) GetAll(ctx context.Context) ([]entities.Signal, error) {
	signals, err := s.Repository.GetAll()
	return signals, err
}

func (s SignalUseCase) GetByID(ctx context.Context, id uint) (entities.Signal, error) {
	signal, err := s.Repository.GetByID(id)
	return signal, err
}

func (s SignalUseCase) Close(ctx context.Context, id uint) error {
	signal, err := s.Repository.GetByID(id)

	if err != nil {
		return err
	}

	if signal.Status == entities.SignalStatus(entities.Closed) {
		return fmt.Errorf("Signal is already closed")
	}

	prices, err := s.Exchange.ListTickerPrices(ctx, signal.Symbol)
	if err != nil {
		return err
	}
	if len(prices) == 0 {
		return fmt.Errorf("no ticker prices found for symbol %s", signal.Symbol)
	}

	return s.GenerateSellSignal(ExitSignal{
		Symbol:       signal.Symbol,
		StrategyID:   signal.StrategyID,
		StrategyName: signal.Strategy.Name,
		ExitPrice:    float32(prices[0].Price),
		ExitReason:   "manual",
	})
}

func (s SignalUseCase) observeOrderDuration(strategyName, side string, start time.Time) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.ObserveHistogram(metricOrderDuration, map[string]string{
		"strategy": strategyName,
		"side":     side,
	}, time.Since(start).Seconds())
}

func (s SignalUseCase) incrementOrderError(strategyName, reason string) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.IncrementCounter(metricOrderErrors, map[string]string{
		"strategy": strategyName,
		"reason":   reason,
	})
}

func (s SignalUseCase) notify(ctx context.Context, event notifier.Event) {
	if s.Notifier == nil {
		return
	}
	_ = s.Notifier.Send(ctx, event)
}

func (s SignalUseCase) errorEvent(e EntrySignal, message string) notifier.Event {
	return notifier.Event{
		Type:       notifier.EventStrategyError,
		Timestamp:  time.Now(),
		StrategyID: e.StrategyID,
		Strategy:   e.StrategyName,
		Symbol:     e.Symbol,
		Mode:       e.Mode,
		Message:    message,
		Data:       map[string]any{"error": message, "context": "GenerateBuySignal"},
	}
}

func (s SignalUseCase) errorEventExit(e ExitSignal, message string) notifier.Event {
	return notifier.Event{
		Type:       notifier.EventStrategyError,
		Timestamp:  time.Now(),
		StrategyID: e.StrategyID,
		Strategy:   e.StrategyName,
		Symbol:     e.Symbol,
		Mode:       e.Mode,
		Message:    message,
		Data:       map[string]any{"error": message, "context": "GenerateSellSignal"},
	}
}

// classifyOrderErrorReason derives an order_execution_errors_total{reason}
// label from an ExchangeClient error without importing any exchange SDK
// type - the ACL sentinel errors (internal/exchange/errors.go) are the only
// contract this package depends on (Spec 11's "reason: derived from ... the
// error type").
func classifyOrderErrorReason(err error) string {
	switch {
	case errors.Is(err, exchange.ErrInsufficientBalance):
		return "insufficient_balance"
	case errors.Is(err, exchange.ErrFilterViolation):
		return "filter_violation"
	case errors.Is(err, exchange.ErrOrderRejected):
		return "rejected"
	default:
		return "network_error"
	}
}
