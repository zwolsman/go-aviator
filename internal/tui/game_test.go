package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/zwolsman/go-aviator/internal/engine"
)

// testModel returns a minimal Model with a renderer suitable for unit tests.
func testModel() Model {
	r := lipgloss.DefaultRenderer()
	m := Model{
		renderer: r,
		st:       newStyles(r),
	}
	m.game = newGameModel(&m)
	return m
}

func TestPlaneViewHasTrail(t *testing.T) {
	tests := []struct {
		name      string
		multBP    int
		wantTrail bool
	}{
		{"at 1x no trail yet", 100, false},
		{"at 1.5x short trail", 150, true},
		{"at 2x trail", 200, true},
		{"at 5x longer trail", 500, true},
		{"at 10x long trail", 1000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel()
			m.game.snap = engine.Snapshot{
				State:         engine.StateFlying,
				CurrentMultBP: tt.multBP,
			}
			// simulate flight history from 1x up to current multiplier
			if tt.multBP > 100 {
				m.game.multHistory = []int{100, tt.multBP}
			}

			view := m.game.planeView()

			// Find the line containing the plane glyph.
			planeLine := ""
			for _, line := range strings.Split(view, "\n") {
				if strings.Contains(line, "✈") {
					planeLine = line
					break
				}
			}
			if planeLine == "" {
				t.Fatal("no plane glyph found in planeView output")
			}

			hasTrail := strings.Contains(planeLine, "─")
			if hasTrail != tt.wantTrail {
				t.Errorf("multBP=%d: wantTrail=%v but planeLine=%q", tt.multBP, tt.wantTrail, planeLine)
			}

			// Trail must appear before the plane on the same line.
			if tt.wantTrail {
				trailIdx := strings.Index(planeLine, "─")
				planeIdx := strings.Index(planeLine, "✈")
				if trailIdx >= planeIdx {
					t.Errorf("trail (col %d) should be left of plane (col %d)", trailIdx, planeIdx)
				}
			}
		})
	}
}

// TestPlaneViewGraphRises verifies that the trail spans multiple screen rows,
// meaning the graph actually curves upward rather than staying flat.
func TestPlaneViewGraphRises(t *testing.T) {
	m := testModel()
	m.game.snap = engine.Snapshot{
		State:         engine.StateFlying,
		CurrentMultBP: 1000, // 10x
	}
	// history spanning 1x → 2x → 5x → 10x so the curve visibly rises
	m.game.multHistory = []int{100, 200, 500, 1000}

	view := m.game.planeView()
	rows := strings.Split(view, "\n")

	// Count how many distinct rows contain graph characters.
	trailRowCount := 0
	lowestTrailRow := -1 // highest index = lowest on screen = where 1x trail starts
	planeRow := -1
	for i, row := range rows {
		if strings.ContainsAny(row, "─│") {
			trailRowCount++
			lowestTrailRow = i
		}
		if strings.Contains(row, "✈") {
			planeRow = i
		}
	}

	if trailRowCount <= 1 {
		t.Errorf("expected trail on multiple rows (rising graph), got trail on %d row(s)", trailRowCount)
	}
	if planeRow == -1 {
		t.Fatal("no plane glyph found in graph")
	}
	// The 1x start of the trail is at the bottom; the plane (high mult) is above it.
	if lowestTrailRow <= planeRow {
		t.Errorf("graph does not rise: trail bottom at row %d should be below plane at row %d", lowestTrailRow, planeRow)
	}
}

func TestPlaneViewCrashFreezesAtCrashMultiplier(t *testing.T) {
	m := testModel()
	m.game.snap = engine.Snapshot{
		State:         engine.StateCrashed,
		CurrentMultBP: 350, // overshoot tick
		CrashMultBP:   312, // actual crash point
	}

	view := m.game.planeView()

	if !strings.Contains(view, "3.12") {
		t.Errorf("expected crash multiplier 3.12x in view, got:\n%s", view)
	}
	if strings.Contains(view, "3.50") {
		t.Errorf("overshoot value 3.50x must not appear in view, got:\n%s", view)
	}
}
