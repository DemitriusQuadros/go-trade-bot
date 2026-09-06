package components

import (
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// BuildSparkline renders closes as a termui widgets.SparklineGroup.
// termui/v3.1.0's Sparkline widget renders using 8-level block characters.
func BuildSparkline(title string, closes []float64, lineColor ui.Color) *widgets.SparklineGroup {
	data := closes
	if len(data) == 0 {
		data = []float64{0, 0}
	} else if len(data) == 1 {
		data = []float64{data[0], data[0]}
	}

	sl := widgets.NewSparkline()
	sl.Data = data
	sl.LineColor = lineColor
	sl.TitleStyle.Fg = ui.ColorWhite

	slg := widgets.NewSparklineGroup(sl)
	slg.Title = title
	return slg
}

// ComputeEMA20 computes exponential moving average with period N=20
// (EMA_t = Close_t * k + EMA_{t-1} * (1-k), k = 2/(N+1)).
func ComputeEMA20(closes []float64) []float64 {
	n := len(closes)
	if n == 0 {
		return nil
	}
	period := 20
	k := 2.0 / float64(period+1)

	ema := make([]float64, n)

	if n <= period {
		sum := 0.0
		for _, v := range closes {
			sum += v
		}
		avg := sum / float64(n)
		for i := range ema {
			ema[i] = avg
		}
		return ema
	}

	// First value is SMA of first 'period' elements
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += closes[i]
		ema[i] = sum / float64(i+1)
	}

	for i := period; i < n; i++ {
		ema[i] = closes[i]*k + ema[i-1]*(1-k)
	}

	return ema
}
