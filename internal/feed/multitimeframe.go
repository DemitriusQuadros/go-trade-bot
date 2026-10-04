package feed

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/internal/exchange"
)

// ParseTimeframeDuration parses interval strings like "1m", "5m", "15m", "1h", "1d" into time.Duration.
func ParseTimeframeDuration(tf string) (time.Duration, error) {
	tf = strings.TrimSpace(strings.ToLower(tf))
	if len(tf) < 2 {
		return 0, fmt.Errorf("invalid timeframe format: %s", tf)
	}

	unit := tf[len(tf)-1]
	valStr := tf[:len(tf)-1]
	val, err := strconv.Atoi(valStr)
	if err != nil || val <= 0 {
		return 0, fmt.Errorf("invalid timeframe value in %s: %w", tf, err)
	}

	switch unit {
	case 'm':
		return time.Duration(val) * time.Minute, nil
	case 'h':
		return time.Duration(val) * time.Hour, nil
	case 'd':
		return time.Duration(val) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe unit in %s", tf)
	}
}

// AggregateCandles derives higher-timeframe candles from a lower-timeframe
// (base) series - e.g. 15 consecutive 1m candles -> one 15m candle.
// Standard OHLCV aggregation:
// - Open = first candle's Open
// - Close = last candle's Close
// - High = max(Highs)
// - Low = min(Lows)
// - Volume = sum(Volumes)
// - OpenTime = first candle's OpenTime
// Trailing incomplete windows are discarded. Gaps in timestamps within a window cause the window to be skipped.
func AggregateCandles(base []exchange.Candle, targetTimeframe string) ([]exchange.Candle, error) {
	if len(base) == 0 {
		return []exchange.Candle{}, nil
	}

	targetDur, err := ParseTimeframeDuration(targetTimeframe)
	if err != nil {
		return nil, fmt.Errorf("invalid target timeframe %q: %w", targetTimeframe, err)
	}

	// Detect base duration from first consecutive candles or default to 1m
	baseDur := time.Minute
	if len(base) > 1 {
		diff := base[1].OpenTime.Sub(base[0].OpenTime)
		if diff > 0 {
			baseDur = diff
		}
	}

	if targetDur < baseDur {
		return nil, fmt.Errorf("cannot aggregate to a smaller timeframe (%s < base %s)", targetTimeframe, baseDur)
	}

	if targetDur%baseDur != 0 {
		return nil, fmt.Errorf("target duration %s is not an exact multiple of base duration %s", targetDur, baseDur)
	}

	ratio := int(targetDur / baseDur)
	if ratio == 1 {
		// Same timeframe
		out := make([]exchange.Candle, len(base))
		copy(out, base)
		return out, nil
	}

	var aggregated []exchange.Candle
	i := 0
	for i+ratio <= len(base) {
		window := base[i : i+ratio]

		// Check for gaps within window
		hasGap := false
		for j := 1; j < len(window); j++ {
			expectedTime := window[j-1].OpenTime.Add(baseDur)
			if !window[j].OpenTime.Equal(expectedTime) {
				hasGap = true
				break
			}
		}

		if hasGap {
			// Skip forward to the next index after the gap
			i++
			continue
		}

		openPrice := window[0].Open
		closePrice := window[len(window)-1].Close
		highPrice := window[0].High
		lowPrice := window[0].Low
		totalVolume := 0.0

		for _, c := range window {
			if c.High > highPrice {
				highPrice = c.High
			}
			if c.Low < lowPrice {
				lowPrice = c.Low
			}
			totalVolume += c.Volume
		}

		aggregated = append(aggregated, exchange.Candle{
			Symbol:    window[0].Symbol,
			Timeframe: targetTimeframe,
			OpenTime:  window[0].OpenTime,
			Open:      openPrice,
			High:      highPrice,
			Low:       lowPrice,
			Close:     closePrice,
			Volume:    math.Round(totalVolume*1e8) / 1e8,
		})

		i += ratio
	}

	return aggregated, nil
}
