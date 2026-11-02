package exchange

import "errors"

// Sentinel errors let callers (app/usecase/signal, Spec 02/03) classify a
// PlaceOrder/CancelOrder/GetOrder failure without importing
// github.com/adshao/go-binance/v2 themselves - that import stays confined to
// this package (the ACL boundary), while callers still get semantic
// classification via errors.Is, matching Spec 11's "reason: derived from the
// exchange.OrderResult.Status or the error type" metric-labeling guidance.
var (
	// ErrOrderNotFound is returned by CancelOrder/GetOrder when the exchange
	// reports the order doesn't exist (e.g. it already triggered/filled, or
	// was already cancelled) - Spec 03's "stop already filled" race and
	// restart-reconciliation both branch on this.
	ErrOrderNotFound = errors.New("exchange: order not found")

	// ErrOrderRejected is returned by PlaceOrder for a clean rejection that
	// isn't more specifically classified below.
	ErrOrderRejected = errors.New("exchange: order rejected")

	// ErrInsufficientBalance is returned by PlaceOrder when the exchange
	// rejects an order for insufficient account balance.
	ErrInsufficientBalance = errors.New("exchange: insufficient balance")

	// ErrFilterViolation is returned by PlaceOrder when the exchange rejects
	// an order for violating a symbol trading filter (e.g. LOT_SIZE,
	// PRICE_FILTER, PERCENT_PRICE).
	ErrFilterViolation = errors.New("exchange: order violates exchange filter")

	// ErrReduceOnlyRejected is returned by PlaceOrder on Futures when a
	// ReduceOnly exit order (or Hedge Mode closing order) is rejected because
	// the position is already flat (-2022).
	ErrReduceOnlyRejected = errors.New("exchange: reduce-only order rejected")

	// ErrFuturesNotSupported is returned when a FuturesClient capability method
	// is invoked against a Spot-only adapter.
	ErrFuturesNotSupported = errors.New("exchange: futures operations not supported on spot client")
)
