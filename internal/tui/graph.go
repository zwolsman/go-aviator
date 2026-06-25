package tui

import (
	"fmt"
	"math"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/linechart"
	"github.com/charmbracelet/lipgloss"

	"github.com/zwolsman/go-aviator/internal/engine"
)

const (
	graphHeight    = 14 // total ntcharts canvas height (includes 2 rows for X-axis labels)
	minTicksWindow = 20 // minimum X window (~2 seconds) so early flight isn't degenerate
)

// plotWindow describes the coordinate ranges used for the log-space graph.
// X axis: tick index (0 = flight start). Y axis: ln(multiplier), so y=0 ↔ 1.00x.
// Because the engine curve is mult = e^(growthK·ticks), ln(mult) is linear in
// time — the line is naturally straight at a constant slope.
type plotWindow struct {
	tWindow  float64 // X range (tick count)
	lnWindow float64 // Y range (ln-mult)
}

// computeWindow derives the visible coordinate window for the current flight.
// Floors ensure the graph is non-degenerate at flight start.
func computeWindow(history []int, finalBP int) plotWindow {
	n := len(history)
	tMax := float64(n - 1)
	tWindow := math.Max(tMax, float64(minTicksWindow))

	mult := float64(finalBP) / 100.0
	if mult < 1.0 {
		mult = 1.0
	}
	lnWindow := math.Max(math.Log(mult), math.Log(1.5))
	return plotWindow{tWindow: tWindow, lnWindow: lnWindow}
}

// buildGraph renders the braille line chart for the given multiplier history.
// width and height are the total canvas dimensions (ntcharts allocates axis rows internally).
// Returns the rendered string from linechart.View().
func buildGraph(history []int, finalBP int, width, height int, st tuiStyles, renderer *lipgloss.Renderer) string {
	if width < 12 || height < 4 {
		return ""
	}

	lineCol := renderer.NewStyle().Foreground(lipgloss.Color("82"))  // green
	axisCol := renderer.NewStyle().Faint(true)
	labelCol := renderer.NewStyle().Faint(true)
	planeCol := renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("226"))

	n := len(history)
	if n == 0 {
		history = []int{100}
		n = 1
	}

	w := computeWindow(history, finalBP)

	xFmt := func(_ int, v float64) string {
		return fmt.Sprintf("%ds", int(math.Round(v/10.0)))
	}
	yFmt := func(_ int, v float64) string {
		return engine.FormatMult(int(math.Exp(v)*100)) + "x"
	}

	lc := linechart.New(
		width, height,
		0, w.tWindow,
		0, w.lnWindow,
		linechart.WithXYSteps(4, 2),
		linechart.WithStyles(axisCol, labelCol, lineCol),
		linechart.WithXLabelFormatter(xFmt),
		linechart.WithYLabelFormatter(yFmt),
	)

	lc.DrawXYAxisAndLabel()

	for i := 1; i < n; i++ {
		p1 := canvas.Float64Point{
			X: float64(i - 1),
			Y: math.Log(math.Max(float64(history[i-1])/100.0, 1.0)),
		}
		p2 := canvas.Float64Point{
			X: float64(i),
			Y: math.Log(math.Max(float64(history[i])/100.0, 1.0)),
		}
		lc.DrawBrailleLineWithStyle(p1, p2, lineCol)
	}

	// Plane glyph at the tip of the line.
	lastLn := math.Log(math.Max(float64(finalBP)/100.0, 1.0))
	lc.DrawRuneWithStyle(canvas.Float64Point{X: float64(n - 1), Y: lastLn}, '✈', planeCol)

	return lc.View()
}
