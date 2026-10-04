package candlesource

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/exchange"
)

// RateLimiter budgets Binance request weight across every worker replica.
// A nil limiter means unbounded (tests, tiny repairs).
type RateLimiter interface {
	Wait(ctx context.Context, weight int) error
}

// Banner is implemented by limiters that can block all callers for a while
// (ratelimit.WeightLimiter): the circuit breaker for 418/429 responses.
type Banner interface {
	Ban(ctx context.Context, d time.Duration) error
}

// RESTSource pages the Binance klines endpoint forward from `from`. It is
// authoritative: an empty page for a window means Binance has no candle there.
type RESTSource struct {
	Fetcher exchange.HistoricalKlineFetcher
	Limiter RateLimiter
	// PageSize is the klines page size (Binance max 1000).
	PageSize int
}

const klineWeight = 2

func NewRESTSource(f exchange.HistoricalKlineFetcher, l RateLimiter) *RESTSource {
	return &RESTSource{Fetcher: f, Limiter: l, PageSize: 1000}
}

func (r *RESTSource) Name() string        { return "rest" }
func (r *RESTSource) Authoritative() bool { return true }
func (r *RESTSource) CanServe(_, _ string, _, _, _ time.Time) bool {
	return r.Fetcher != nil
}

func (r *RESTSource) Fetch(ctx context.Context, symbol, timeframe string, from, to time.Time, emit func([]entities.Candle) error) (string, error) {
	tf, ok := intervalOf(timeframe)
	if !ok {
		return "", fmt.Errorf("candlesource: unsupported timeframe %q", timeframe)
	}
	cur := from.UTC()
	for cur.Before(to) {
		if r.Limiter != nil {
			if err := r.Limiter.Wait(ctx, klineWeight); err != nil {
				return "", err
			}
		}
		pageEnd := cur.Add(time.Duration(r.PageSize) * tf)
		if pageEnd.After(to) {
			pageEnd = to
		}
		ks, err := r.Fetcher.ListKlineRange(ctx, symbol, timeframe, cur, pageEnd, r.PageSize)
		if err != nil {
			cerr := classifyRESTError(err)
			// trip the shared circuit breaker so every replica backs off, not just this task
			if re, ok := Retryable(cerr); ok && re.After > 0 {
				if b, ok := r.Limiter.(Banner); ok {
					_ = b.Ban(ctx, re.After)
				}
			}
			return "", cerr
		}
		out := make([]entities.Candle, 0, len(ks))
		for _, k := range ks {
			if k.OpenTime.Before(cur) || !k.OpenTime.Before(to) {
				continue
			}
			out = append(out, entities.Candle{Symbol: symbol, Timeframe: timeframe, OpenTime: k.OpenTime.UTC(),
				Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
		}
		if len(out) > 0 {
			if err := emit(out); err != nil {
				return "", err
			}
		}
		// advance past the page whether or not it had candles: an empty window
		// is a real gap (or pre-listing), not the end.
		cur = pageEnd
	}
	return "klines", nil
}

func intervalOf(tf string) (time.Duration, bool) {
	m := map[string]time.Duration{"1m": time.Minute, "3m": 3 * time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute,
		"30m": 30 * time.Minute, "1h": time.Hour, "2h": 2 * time.Hour, "4h": 4 * time.Hour, "6h": 6 * time.Hour,
		"8h": 8 * time.Hour, "12h": 12 * time.Hour, "1d": 24 * time.Hour}
	d, ok := m[tf]
	return d, ok
}

// classifyRESTError marks rate limits, server errors and network failures as
// retryable; 418 (IP ban) gets a long Retry-After so the runner backs off hard.
func classifyRESTError(err error) error {
	msg := strings.ToLower(err.Error())
	var ne net.Error
	switch {
	case strings.Contains(msg, "418") || strings.Contains(msg, "banned"):
		return &RetryableError{Err: err, After: 2 * time.Minute}
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "-1003"):
		return &RetryableError{Err: err, After: 10 * time.Second}
	case strings.Contains(msg, "status code: 5") || strings.Contains(msg, "timeout") || strings.Contains(msg, "eof") || strings.Contains(msg, "connection reset"):
		return &RetryableError{Err: err}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne):
		return &RetryableError{Err: err}
	}
	return err
}
