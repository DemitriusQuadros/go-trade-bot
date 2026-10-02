// Package candlesource is the ACL for places candles can be loaded from
// (Binance's public archive, the Binance REST klines API, ...). The reconciler
// (app/usecase/candledata) walks an ordered chain of Sources per chunk and
// falls through on failure or a short answer.
package candlesource

import (
	"context"
	"errors"
	"time"

	"go-trade-bot/app/entities"
)

// Source loads closed candles for one (symbol, timeframe) over [from, to).
type Source interface {
	Name() string

	// Authoritative reports whether a successful Fetch that returned fewer
	// candles than expected proves the exchange emitted none for the rest
	// (REST klines: yes; a downloaded file: no - it can be incomplete).
	Authoritative() bool

	// CanServe says whether this source is a sensible way to load the range
	// right now (e.g. a monthly archive only serves a whole, finished month).
	CanServe(symbol, timeframe string, from, to, now time.Time) bool

	// Fetch streams candles in batches through emit. It must emit nothing
	// from data that failed verification. The returned ref identifies what
	// was loaded (file name + checksum) and is stored in the coverage ledger.
	Fetch(ctx context.Context, symbol, timeframe string, from, to time.Time, emit func([]entities.Candle) error) (ref string, err error)
}

var (
	// ErrNotFound: the source has nothing for this range (HTTP 404, ...).
	ErrNotFound = errors.New("candlesource: not found")
	// ErrInvalidData: the source returned data that failed verification
	// (checksum mismatch, malformed rows).
	ErrInvalidData = errors.New("candlesource: invalid data")
)

// RetryableError marks a transient failure (429, 5xx, timeout). After, when
// positive, is the server-requested wait (Retry-After).
type RetryableError struct {
	Err   error
	After time.Duration
}

func (e *RetryableError) Error() string { return "retryable: " + e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// Retryable returns the RetryableError in err's chain, if any.
func Retryable(err error) (*RetryableError, bool) {
	var re *RetryableError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}
