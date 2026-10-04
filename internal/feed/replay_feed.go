package feed

import (
	"context"
	"time"

	"go-trade-bot/app/repository/candle"
	"go-trade-bot/internal/exchange"
)

// ReplayFeed implements Feed by iterating stored candles sequentially,
// ascending by OpenTime, for one (symbol, timeframe) pair over a fixed
// [from, to) range.
type ReplayFeed struct {
	candles []exchange.Candle
	cursor  int
}

// NewReplayFeed loads the full [from, to) range for (symbol, timeframe) into
// memory once at construction via CandleRepository.Range.
func NewReplayFeed(ctx context.Context, repo candle.Repository, symbol, timeframe string, from, to time.Time) (*ReplayFeed, error) {
	if from.Equal(to) || from.After(to) {
		return &ReplayFeed{
			candles: []exchange.Candle{},
			cursor:  0,
		}, nil
	}

	candles, err := repo.Range(ctx, symbol, timeframe, from, to)
	if err != nil {
		return nil, err
	}

	return &ReplayFeed{
		candles: candles,
		cursor:  0,
	}, nil
}

// Next delivers the next candle in the sequence. Returns (zero, false) when exhausted.
func (f *ReplayFeed) Next() (exchange.Candle, bool) {
	if f.cursor >= len(f.candles) {
		return exchange.Candle{}, false
	}

	c := f.candles[f.cursor]
	f.cursor++
	return c, true
}

// Close is a no-op for ReplayFeed, matching the Feed lifecycle conventions.
func (f *ReplayFeed) Close() error {
	return nil
}
