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
