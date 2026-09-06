package components

import (
	"fmt"
	"image"
	"math"

	ui "github.com/gizak/termui/v3"
)

// rowLabelWidth/colCellWidth are fixed layout constants for HeatmapGrid — a
// simple monospace grid is sufficient for the 2D parameter sweeps this
// component renders (PRD's own example is a modest 6x5 grid).
const (
	rowLabelWidth = 22
	colCellWidth  = 9
)

// HeatmapGrid is a custom ui.Drawable that renders a 2D parameter grid as a
// color-coded table using direct ui.Buffer cell writes, since
// widgets.Table's RowStyles (termui/v3.1.0) only supports per-row styling,
// not true per-(row,col) background coloring.
type HeatmapGrid struct {
	ui.Block
	xAxisLabel   string
	xAxisValues  []float64
	yAxisLabel   string
	yAxisValues  []float64
	cellValues   [][]float64
	bestY, bestX int
	invert       bool
}

// BuildHeatmapGrid renders a 2D parameter grid as a color-coded table using
// direct ui.Buffer cell writes (bypassing widgets.Table's row-only styling
// limitation). cellValues[y][x] holds the rank-metric value for
// (yAxisValues[y], xAxisValues[x]); bestY/bestX mark the single
// best-performing cell for the distinct highlight border.
//
// invert flips the color gradient direction for metrics where a LOWER value
// is better (e.g. Max Drawdown Pct) so the greenest cell is always "best,"
// never simply "highest" — this parameter is an addition beyond the spec's
// original sketch signature, required to satisfy the documented
// color-inversion acceptance criterion without the caller having to
// pre-transform its raw metric values (which would break the displayed cell
// text).
func BuildHeatmapGrid(
	xAxisLabel string, xAxisValues []float64,
	yAxisLabel string, yAxisValues []float64,
	cellValues [][]float64,
	bestY, bestX int,
	invert bool,
) ui.Drawable {
	h := &HeatmapGrid{
		Block:       *ui.NewBlock(),
		xAxisLabel:  xAxisLabel,
		xAxisValues: xAxisValues,
		yAxisLabel:  yAxisLabel,
		yAxisValues: yAxisValues,
		cellValues:  cellValues,
		bestY:       bestY,
		bestX:       bestX,
		invert:      invert,
	}
	h.Border = false
	return h
}

// heatmapColor buckets a normalized [0,1] value (this cell's value scaled
// against the observed min/max across the whole grid, with inversion already
// applied by the caller) into a 5-step red→yellow→green gradient. termui/v3
// exposes ui.Color as an int mapped onto the terminal's xterm color table
// (0-255), so xterm 256-color codes are used here for a genuine 5-shade
// gradient rather than cycling through only the 3 named 8-color anchors.
func heatmapColor(normalized float64) ui.Color {
	if normalized < 0 {
		normalized = 0
	} else if normalized > 1 {
		normalized = 1
	}
	switch {
	case normalized < 0.2:
		return ui.Color(196) // red — worst
	case normalized < 0.4:
		return ui.Color(208) // orange
	case normalized < 0.6:
		return ui.Color(226) // yellow — mid
	case normalized < 0.8:
		return ui.Color(190) // yellow-green
	default:
		return ui.Color(46) // green — best
	}
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

// Draw implements ui.Drawable.
func (h *HeatmapGrid) Draw(buf *ui.Buffer) {
	h.Block.Draw(buf)

	if len(h.yAxisValues) == 0 || len(h.xAxisValues) == 0 {
		return
	}

	x0, y0 := h.Inner.Min.X, h.Inner.Min.Y
	maxX, maxY := h.Inner.Max.X, h.Inner.Max.Y

	minV, maxV := math.Inf(1), math.Inf(-1)
	for _, row := range h.cellValues {
		for _, v := range row {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
	}
	spread := maxV - minV

	// Header row: x-axis values.
	for xi, xv := range h.xAxisValues {
		cx := x0 + rowLabelWidth + xi*colCellWidth
		if cx+colCellWidth > maxX {
			break
		}
		label := fmt.Sprintf("%s=%g", h.xAxisLabel, xv)
		if xi > 0 {
			label = fmt.Sprintf("=%g", xv)
		}
		buf.SetString(fmt.Sprintf("%*s", colCellWidth, truncate(label, colCellWidth)),
			ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold), image.Pt(cx, y0))
	}

	for yi, yv := range h.yAxisValues {
		ry := y0 + 1 + yi
		if ry >= maxY {
			break
		}

		rowLabel := fmt.Sprintf("%s=%g", h.yAxisLabel, yv)
		buf.SetString(fmt.Sprintf("%-*s", rowLabelWidth, truncate(rowLabel, rowLabelWidth)),
			ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold), image.Pt(x0, ry))

		for xi := range h.xAxisValues {
			cx := x0 + rowLabelWidth + xi*colCellWidth
			if cx+colCellWidth > maxX {
				break
			}
			if yi >= len(h.cellValues) || xi >= len(h.cellValues[yi]) {
				continue
			}

			v := h.cellValues[yi][xi]
			normalized := 0.5
			if spread > 0 {
				normalized = (v - minV) / spread
			}
			if h.invert {
				normalized = 1 - normalized
			}
			bg := heatmapColor(normalized)

			isBest := yi == h.bestY && xi == h.bestX
			text := fmt.Sprintf("%.2f", v)
			style := ui.NewStyle(ui.ColorBlack, bg)
			if isBest {
				// Best-cell marker: color alone is not sufficient (WCAG
				// "color is not the only means of conveying information"),
				// so the single best cell also gets a distinct
				// bracket/box-drawing style, bold, matching the ASCII
				// wireframe's "┏1.24┓" treatment.
				text = "[" + text + "]"
				style = ui.NewStyle(ui.ColorWhite, bg, ui.ModifierBold)
			}
			buf.SetString(fmt.Sprintf("%*s", colCellWidth, text), style, image.Pt(cx, ry))
		}
	}
}
