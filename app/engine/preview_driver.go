package engine

import (
	"context"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/feed"
)

// PreviewDriver executes a strategy hook sequence against a live socket feed,
// emitting trace records without ever routing signals to the exchange client.
// Spec: docs/specs/strategy-scripting/phase-3-live-preview-and-authoring-polish.md
type PreviewDriver struct {
	Feed       feed.Feed
	Strategy   strategies.Strategy
	DBStrategy entities.Strategy
	Symbol     string
	Engine     *Engine // Embedded to reuse buildContext without duplicating all its dependencies
}

func (d *PreviewDriver) Run(ctx context.Context) error {
	for {
		candle, ok := d.Feed.Next()
		if !ok {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Reusing Engine.buildContext for parity.
		sCtx, err := d.Engine.buildContext(ctx, d.DBStrategy, d.Symbol, strategies.ModeDryRun)
		if err != nil {
			
			continue
		}

		// Append the latest feed candle to the historical window
		sCtx.Candles = append(sCtx.Candles, candle)

		d.Strategy.Before(sCtx)
		if d.Strategy.ShouldLong(sCtx) {
			_ = d.Strategy.GoLong(sCtx)
		} else if d.Strategy.ShouldShort(sCtx) {
			_ = d.Strategy.GoShort(sCtx)
		} else {
			_ = d.Strategy.UpdatePosition(sCtx)
		}
		d.Strategy.After(sCtx)
	}
	return nil
}
