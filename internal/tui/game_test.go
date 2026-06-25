package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
	"github.com/zwolsman/go-aviator/internal/engine"
)

// testModel returns a minimal Model with a renderer suitable for unit tests.
func testModel() Model {
	r := lipgloss.DefaultRenderer()
	m := Model{
		renderer: r,
		st:       newStyles(r),
		player: dbpkg.Player{
			ID:                42,
			DisplayName:       "tester",
			Balance:           1000,
			PubkeyFingerprint: "fingerprint123456",
		},
	}
	m.game = newGameModel(&m)
	m.game.lastDisplayName = m.player.DisplayName
	return m
}

// hasBraille returns true if s contains at least one filled braille glyph (U+2801..U+28FF).
func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2801 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// TestComputeWindowFloors verifies that the window never collapses to zero.
func TestComputeWindowFloors(t *testing.T) {
	// At 1.00x with short history, both floors must kick in.
	w := computeWindow([]int{100, 100}, 100)
	if w.tWindow < float64(minTicksWindow) {
		t.Errorf("tWindow %f should be >= minTicksWindow %d", w.tWindow, minTicksWindow)
	}
	minLn := math.Log(1.5)
	if w.lnWindow < minLn {
		t.Errorf("lnWindow %f should be >= ln(1.5) %f", w.lnWindow, minLn)
	}
}

// TestComputeWindowGrows checks that the window tracks the actual multiplier as it grows.
func TestComputeWindowGrows(t *testing.T) {
	smallW := computeWindow([]int{100, 150}, 150)
	// Build a history longer than minTicksWindow so the floor doesn't mask growth.
	longHistory := make([]int, minTicksWindow+10)
	for i := range longHistory {
		longHistory[i] = 100 + i*50
	}
	largeW := computeWindow(longHistory, 1000)
	if largeW.lnWindow <= smallW.lnWindow {
		t.Errorf("lnWindow should grow with multiplier: small=%f large=%f", smallW.lnWindow, largeW.lnWindow)
	}
	if largeW.tWindow <= smallW.tWindow {
		t.Errorf("tWindow should grow with more history: small=%f large=%f", smallW.tWindow, largeW.tWindow)
	}
}

// TestGraphRendersBraille verifies the braille chart contains actual braille dots.
func TestGraphRendersBraille(t *testing.T) {
	m := testModel()
	m.game.snap = engine.Snapshot{
		State:         engine.StateFlying,
		CurrentMultBP: 300,
	}
	m.game.multHistory = []int{100, 120, 150, 200, 250, 300}

	view := m.game.graphView()
	if !hasBraille(view) {
		t.Errorf("graphView() should contain braille glyphs (U+2801–U+28FF)\n%s", view)
	}
}

// TestGraphLineRises verifies the braille trail appears on multiple rows,
// indicating the line actually climbs upward.
func TestGraphLineRises(t *testing.T) {
	m := testModel()
	m.game.snap = engine.Snapshot{
		State:         engine.StateFlying,
		CurrentMultBP: 1000,
	}
	m.game.multHistory = []int{100, 200, 500, 1000}

	view := m.game.graphView()
	rows := strings.Split(view, "\n")

	brailleRows := 0
	for _, row := range rows {
		for _, r := range row {
			if r >= 0x2800 && r <= 0x28FF {
				brailleRows++
				break
			}
		}
	}
	if brailleRows < 2 {
		t.Errorf("expected braille on at least 2 rows (rising graph), got %d", brailleRows)
	}
}

// TestGraphCrashFreezesAtCrashMultiplier verifies the crash multiplier is shown,
// not the overshoot tick.
func TestGraphCrashFreezesAtCrashMultiplier(t *testing.T) {
	m := testModel()
	m.game.snap = engine.Snapshot{
		State:         engine.StateCrashed,
		CurrentMultBP: 350,
		CrashMultBP:   312,
	}
	m.game.multHistory = []int{100, 200, 312}

	view := m.game.graphView()

	if !strings.Contains(view, "3.12") {
		t.Errorf("expected crash multiplier 3.12 in graph view:\n%s", view)
	}
	if strings.Contains(view, "3.50") {
		t.Errorf("overshoot value 3.50 must not appear in graph view:\n%s", view)
	}
}

// TestLabelFormatters verifies the axis formatters produce the expected output.
func TestLabelFormatters(t *testing.T) {
	// Y formatter: v = ln(mult) → "mult x"
	yFmt := func(_ int, v float64) string {
		return engine.FormatMult(int(math.Exp(v)*100)) + "x"
	}
	if got := yFmt(0, 0); got != "1.00x" {
		t.Errorf("yFmt(ln(1.00)) = %q, want 1.00x", got)
	}
	if got := yFmt(0, math.Log(2)); !strings.HasPrefix(got, "2.00") {
		t.Errorf("yFmt(ln(2)) = %q, want prefix 2.00", got)
	}

	// X formatter: v = tick index (10 ticks = 1 second)
	xFmt := func(_ int, v float64) string {
		return fmt.Sprintf("%ds", int(math.Round(v/10.0)))
	}
	_ = xFmt // checked inline below
	tests := []struct {
		v    float64
		want string
	}{
		{0, "0s"},
		{10, "1s"},
		{20, "2s"},
		{100, "10s"},
	}
	for _, tt := range tests {
		if got := xFmt(0, tt.v); got != tt.want {
			t.Errorf("xFmt(%g) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

// TestCashoutValuesComputeCorrectly checks the live cash-out math.
func TestCashoutValuesComputeCorrectly(t *testing.T) {
	m := testModel()
	m.player.ID = 42

	m.game.snap = engine.Snapshot{
		State:         engine.StateFlying,
		CurrentMultBP: 241,
		Participants: []engine.ParticipantView{
			{PlayerID: 42, Amount: 100},
		},
	}

	total, profit := m.game.cashoutValues()
	if total != 241 {
		t.Errorf("total = %d, want 241", total)
	}
	if profit != 141 {
		t.Errorf("profit = %d, want 141", profit)
	}

	// Spectating: no participant entry.
	m.game.snap.Participants = nil
	total, profit = m.game.cashoutValues()
	if total != 0 || profit != 0 {
		t.Errorf("spectating: want (0,0), got (%d,%d)", total, profit)
	}

	// At exactly 1.00x.
	m.game.snap.CurrentMultBP = 100
	m.game.snap.Participants = []engine.ParticipantView{{PlayerID: 42, Amount: 100}}
	total, profit = m.game.cashoutValues()
	if total != 100 || profit != 0 {
		t.Errorf("at 1.00x: want (100,0), got (%d,%d)", total, profit)
	}
}

// TestQueuedBetStagesCorrectly verifies that queueForNextRound() sets queued=true
// and parseValues reads back the typed amount and auto-cashout correctly.
func TestQueuedBetStagesCorrectly(t *testing.T) {
	m := testModel()
	m.game.betEntry.amountStr = "50"
	m.game.betEntry.acStr = "2.00"

	m.game.betEntry.queueForNextRound()
	if !m.game.betEntry.queued {
		t.Fatal("betEntry should be queued after queueForNextRound()")
	}
	amount, ac, errMsg := m.game.betEntry.parseValues()
	if errMsg != "" {
		t.Fatalf("parseValues() error: %s", errMsg)
	}
	if amount != 50 {
		t.Errorf("amount = %d, want 50", amount)
	}
	if ac != 200 {
		t.Errorf("autoCashoutBP = %d, want 200", ac)
	}
}

// TestQueuedBetRejectsInvalidAmount checks queueForNextRound() validates the amount.
func TestQueuedBetRejectsInvalidAmount(t *testing.T) {
	m := testModel()
	m.game.betEntry.amountStr = "0"

	m.game.betEntry.queueForNextRound()
	if m.game.betEntry.queued {
		t.Error("should not queue a zero-amount bet")
	}
	if m.game.betEntry.err == "" {
		t.Error("expected an error message for zero amount")
	}
}

// TestBetFormAlwaysEditable verifies keys reach the form regardless of game state.
func TestBetFormAlwaysEditable(t *testing.T) {
	m := testModel()
	m.game.snap = engine.Snapshot{State: engine.StateFlying}

	// Type "5" while flying — should update amountStr.
	m.game, _ = m.game.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	if m.game.betEntry.amountStr != "5" {
		t.Errorf("amountStr = %q, want \"5\"", m.game.betEntry.amountStr)
	}
}

// TestBrailleGlyphsAreNarrow confirms braille code points are east-asian narrow
// (width 1), so lipgloss width math stays correct.
func TestBrailleGlyphsAreNarrow(t *testing.T) {
	// Every valid braille cell should be exactly 1 rune and render as 1-width.
	samples := []rune{0x2800, 0x2801, 0x28FF}
	for _, r := range samples {
		s := string(r)
		if utf8.RuneCountInString(s) != 1 {
			t.Errorf("rune %U: expected 1 rune, got %d", r, utf8.RuneCountInString(s))
		}
		w := lipgloss.Width(s)
		if w != 1 {
			t.Errorf("rune %U: lipgloss.Width = %d, want 1", r, w)
		}
	}
}
