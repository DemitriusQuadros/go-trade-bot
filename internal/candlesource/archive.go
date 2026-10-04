package candlesource

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/binancearchive"
)

// ArchiveSource loads from data.binance.vision. Daily=false serves whole
// finished UTC months from monthly zips; Daily=true serves whole finished UTC
// days from daily zips (the current month's way in). Both verify checksums.
type ArchiveSource struct {
	Daily  bool
	Client *http.Client
}

func NewArchiveSource(daily bool, c *http.Client) *ArchiveSource {
	if c == nil {
		c = &http.Client{Timeout: 120 * time.Second}
	}
	return &ArchiveSource{Daily: daily, Client: c}
}

func (a *ArchiveSource) Name() string {
	if a.Daily {
		return "archive_daily"
	}
	return "archive_monthly"
}

// Authoritative is false: a downloaded file can be short or contain holes.
func (a *ArchiveSource) Authoritative() bool { return false }

func (a *ArchiveSource) CanServe(_, _ string, from, to, now time.Time) bool {
	from, to, now = from.UTC(), to.UTC(), now.UTC()
	if a.Daily {
		return isMidnight(from) && isMidnight(to) && !to.After(dayStart(now))
	}
	firstOfMonth := func(t time.Time) bool { return isMidnight(t) && t.Day() == 1 }
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return firstOfMonth(from) && to.Equal(from.AddDate(0, 1, 0)) && !to.After(monthStart)
}

func (a *ArchiveSource) Fetch(ctx context.Context, symbol, timeframe string, from, to time.Time, emit func([]entities.Candle) error) (string, error) {
	step := func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }
	if a.Daily {
		step = func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }
	}
	ref := ""
	for t := from.UTC(); t.Before(to); t = step(t) {
		candles, r, err := binancearchive.FetchVerified(ctx, a.Client, symbol, timeframe, t, a.Daily)
		if err != nil {
			return "", mapArchiveErr(err)
		}
		if err := emit(candles); err != nil {
			return "", err
		}
		ref = r
	}
	return ref, nil
}

func mapArchiveErr(err error) error {
	var se *binancearchive.StatusError
	var ne net.Error
	switch {
	case errors.Is(err, binancearchive.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case errors.Is(err, binancearchive.ErrChecksum):
		return fmt.Errorf("%w: %v", ErrInvalidData, err)
	case errors.As(err, &se) && (se.Code == http.StatusTooManyRequests || se.Code >= 500):
		return &RetryableError{Err: err}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne):
		return &RetryableError{Err: err}
	}
	return err
}

func isMidnight(t time.Time) bool { return t.Equal(dayStart(t)) }

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
