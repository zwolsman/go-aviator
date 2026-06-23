package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zwolsman/go-aviator/internal/engine"
)

type gameModel struct {
	root            *Model
	snap            engine.Snapshot
	betEntry        betModel
	showBetForm     bool
	recentCrashes   []int  // crashBP of last N settled rounds, newest first
	lastAmountStr   string // remembered from previous round
	lastAcStr       string
	lastDisplayName string // kept in sync; safe alternative to root.player.DisplayName
	multHistory     []int  // CurrentMultBP sampled each flying tick; reset at new round
}

func newGameModel(root *Model) gameModel {
	return gameModel{root: root, betEntry: newBetModel(root)}
}

func (g *gameModel) onSnapshot(snap engine.Snapshot) {
	prev := g.snap
	g.snap = snap

	// auto-show bet form when a new betting window opens
	if snap.State == engine.StateBetting && prev.State != engine.StateBetting {
		g.multHistory = nil // clear graph for new round
		g.showBetForm = true
		g.betEntry = newBetModel(g.root)
		g.betEntry.amountStr = g.lastAmountStr
		g.betEntry.acStr = g.lastAcStr
		g.betEntry.playerName = g.lastDisplayName
		// maxBet is corrected by the playerRefreshedMsg that fires on state change
	}
	// record multiplier each flying tick; hide form
	if snap.State == engine.StateFlying {
		g.multHistory = append(g.multHistory, snap.CurrentMultBP)
		g.showBetForm = false
	}
}

func (g gameModel) update(msg tea.Msg) (gameModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case " ":
			// manual cashout
			if g.snap.State == engine.StateFlying {
				g.root.eng.CashOut(g.root.player.ID)
			}
			return g, nil
		}
		if g.showBetForm {
			wasSubmitted := g.betEntry.submitted
			var cmd tea.Cmd
			g.betEntry, cmd = g.betEntry.update(msg)
			if !wasSubmitted && g.betEntry.submitted {
				g.lastAmountStr = g.betEntry.amountStr
				g.lastAcStr = g.betEntry.acStr
			}
			return g, cmd
		}
	}
	return g, nil
}

func (g gameModel) view() string {
	var sb strings.Builder

	// plane + multiplier
	sb.WriteString(g.planeView())
	sb.WriteString("\n")

	// recent round history bar
	if bar := g.historyBar(); bar != "" {
		sb.WriteString(bar)
		sb.WriteString("\n")
	}

	// participants panel
	sb.WriteString(g.participantsView())
	sb.WriteString("\n")

	// bet form or status
	switch g.snap.State {
	case engine.StateBetting:
		if g.showBetForm {
			sb.WriteString(g.betEntry.view())
		} else {
			sb.WriteString(g.root.st.dim.Render("Bet submitted. Waiting for takeoff..."))
		}
		remaining := time.Until(g.snap.BettingEndsAt)
		if remaining < 0 {
			remaining = 0
		}
		sb.WriteString("\n" + g.root.st.info.Render(fmt.Sprintf("  Takeoff in: %.1fs", remaining.Seconds())))
	case engine.StateFlying:
		if g.betEntry.submitted {
			if g.betEntry.acStr != "" {
				sb.WriteString(g.root.st.info.Render(fmt.Sprintf("  Auto-cashout: %sx", g.betEntry.acStr)) +
					g.root.st.dim.Render("  — SPACE to cash out early"))
			} else {
				sb.WriteString(g.root.st.warning.Render("  Press SPACE to cash out!"))
			}
		} else {
			sb.WriteString(g.root.st.dim.Render("  Spectating — place a bet in the next round"))
		}
	case engine.StateCrashed:
		sb.WriteString(g.root.st.danger.Render(fmt.Sprintf("  Crashed at %sx! Press [i] to inspect.", engine.FormatMult(g.snap.CrashMultBP))))
	case engine.StateSettling:
		sb.WriteString(g.root.st.info.Render("  Settling round..."))
	case engine.StateIdle:
		sb.WriteString(g.root.st.dim.Render("  Waiting for players..."))
	}

	// commit hash footer
	if g.snap.CommitHash != "" {
		short := g.snap.CommitHash
		if len(short) > 16 {
			short = short[:16] + "..."
		}
		sb.WriteString("\n" + g.root.st.dim.Render(fmt.Sprintf("  Commit: %s", short)))
	}

	return sb.String()
}

const (
	graphCols = 60
	graphRows = 12
)

// trajectoryLen maps a multiplier (basis points) to how many grid columns the
// trail occupies. 0 at 1x, grows linearly, capped at graphCols.
func trajectoryLen(multBP int) int {
	if multBP <= 100 {
		return 0
	}
	pos := int(float64(multBP-100) / 100.0 * 5)
	if pos > graphCols {
		pos = graphCols
	}
	return pos
}

func (g gameModel) planeView() string {
	// freeze at actual crash multiplier, not the overshoot tick
	displayBP := g.snap.CurrentMultBP
	if g.snap.State == engine.StateCrashed || g.snap.State == engine.StateSettling {
		displayBP = g.snap.CrashMultBP
	}

	multStr := engine.FormatMult(displayBP)
	if displayBP == 0 {
		multStr = "1.00"
	}

	// trailCols is how many columns the historical path occupies.
	// The plane sits immediately to the right of those columns.
	// At 1x trailCols=0 → plane is at the left edge ("on the ground").
	trailCols := trajectoryLen(displayBP)

	// Build 2D character grid: grid[row][col], row 0 = top of screen.
	var grid [graphRows + 1][graphCols]rune
	for r := range grid {
		for c := range grid[r] {
			grid[r][c] = ' '
		}
	}

	history := g.multHistory
	n := len(history)
	if n > 0 && trailCols > 0 {
		prevRow := -1
		for col := 0; col < trailCols; col++ {
			histIdx := 0
			if n > 1 && trailCols > 1 {
				histIdx = col * (n - 1) / (trailCols - 1)
				if histIdx >= n {
					histIdx = n - 1
				}
			}
			bp := history[histIdx]
			h := planeHeight(bp)
			gridRow := graphRows - h // 0=top, graphRows=bottom
			if gridRow < 0 {
				gridRow = 0
			}
			if gridRow > graphRows {
				gridRow = graphRows
			}

			grid[gridRow][col] = '─'

			// fill vertical gap when the curve rises between columns
			if prevRow >= 0 && prevRow != gridRow {
				lo, hi := gridRow, prevRow
				if lo > hi {
					lo, hi = hi, lo
				}
				for r := lo + 1; r < hi; r++ {
					grid[r][col] = '│'
				}
			}
			prevRow = gridRow
		}
	}

	// Plane row is determined by the current multiplier height.
	planeRow := graphRows - planeHeight(displayBP)
	if planeRow < 0 {
		planeRow = 0
	}
	if planeRow > graphRows {
		planeRow = graphRows
	}

	var lines []string
	for r := 0; r <= graphRows; r++ {
		line := "  " + g.root.st.dim.Render(string(grid[r][:trailCols]))
		if r == planeRow {
			line += "✈ " + g.root.st.mult.Render(multStr+"x")
		}
		lines = append(lines, line)
	}

	if g.snap.State == engine.StateCrashed {
		lines = append(lines, g.root.st.danger.Render("  💥 CRASHED at "+engine.FormatMult(g.snap.CrashMultBP)+"x"))
	} else if g.snap.State == engine.StateIdle {
		lines = append(lines, g.root.st.dim.Render("  Waiting..."))
	}

	return strings.Join(lines, "\n")
}

func planeHeight(multBP int) int {
	if multBP <= 100 {
		return 0
	}
	// logarithmic scaling: height = log2(mult/100) * 3, max 12
	mult := float64(multBP) / 100.0
	h := int(logApprox(mult) * 4)
	if h > 12 {
		h = 12
	}
	return h
}

func logApprox(x float64) float64 {
	// simple integer log approximation
	if x <= 1 {
		return 0
	}
	n := 0.0
	for x > 1 {
		x /= 2
		n++
	}
	return n
}

// historyBar renders a horizontal row of recent crash multipliers, newest first.
func (g gameModel) historyBar() string {
	if len(g.recentCrashes) == 0 {
		return ""
	}
	var parts []string
	for _, bp := range g.recentCrashes {
		label := engine.FormatMult(bp) + "x"
		parts = append(parts, crashMultStyle(g.root.renderer, bp).Render(label))
	}
	return g.root.st.dim.Render("  History: ") + strings.Join(parts, g.root.st.dim.Render("  "))
}

// crashMultStyle returns a lipgloss style for a crash multiplier in basis points.
//   < 1.50x  → dim gray   (bust)
//   1.50–1.99x → cyan     (small return)
//   2.00–2.99x → green    (solid double)
//   3.00–4.99x → yellow   (strong)
//   5.00–9.99x → orange   (great)
//   10.00x+    → gold bold (legendary)
func crashMultStyle(r *lipgloss.Renderer, bp int) lipgloss.Style {
	switch {
	case bp >= 1000:
		return r.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	case bp >= 500:
		return r.NewStyle().Foreground(lipgloss.Color("214"))
	case bp >= 300:
		return r.NewStyle().Foreground(lipgloss.Color("226"))
	case bp >= 200:
		return r.NewStyle().Foreground(lipgloss.Color("82"))
	case bp >= 150:
		return r.NewStyle().Foreground(lipgloss.Color("87"))
	default:
		return r.NewStyle().Faint(true)
	}
}

const participantDisplayMax = 15

func (g gameModel) participantsView() string {
	parts := g.snap.Participants
	total := len(parts)

	// during betting: just show a headcount
	if g.snap.State == engine.StateBetting {
		if total == 0 {
			return g.root.st.dim.Render("  No bets placed yet.\n")
		}
		return g.root.st.dim.Render(fmt.Sprintf("  %d player%s in the lobby.\n", total, pluralS(total)))
	}

	if total == 0 {
		return g.root.st.dim.Render("  No bets this round.\n")
	}

	// count still-in players
	stillIn := 0
	for _, p := range parts {
		if !p.CashedOut {
			stillIn++
		}
	}

	// sort: cashed-out first by cashout multiplier descending (latest cashout on top),
	// then still-in players, then crashed/lost at the end
	sorted := make([]engine.ParticipantView, total)
	copy(sorted, parts)
	sort.SliceStable(sorted, func(i, j int) bool {
		ci, cj := sorted[i].CashedOut, sorted[j].CashedOut
		if ci != cj {
			return ci // cashed-out before still-in/lost
		}
		if ci {
			return sorted[i].CashoutMult > sorted[j].CashoutMult // highest mult first
		}
		return false
	})

	crashed := g.snap.State == engine.StateCrashed || g.snap.State == engine.StateSettling

	var rows []string
	rows = append(rows, g.root.st.bold.Render("  PLAYER               BET        STATUS"))
	rows = append(rows, g.root.st.dim.Render("  "+strings.Repeat("─", 50)))

	shown := sorted
	truncated := 0
	if len(sorted) > participantDisplayMax {
		shown = sorted[:participantDisplayMax]
		truncated = len(sorted) - participantDisplayMax
	}

	for _, p := range shown {
		name := p.DisplayName
		if len(name) > 18 {
			name = name[:18]
		}
		var statusCol string
		if p.CashedOut {
			statusCol = g.root.st.success.Render(fmt.Sprintf("✓ @%sx  +%d", engine.FormatMult(p.CashoutMult), p.Payout))
		} else if crashed {
			statusCol = g.root.st.danger.Render("✗ Lost")
		} else {
			statusCol = g.root.st.warning.Render("✈ In flight")
		}
		rows = append(rows, fmt.Sprintf("  %-18s  %-9d  %s", name, p.Amount, statusCol))
	}

	if truncated > 0 {
		rows = append(rows, g.root.st.dim.Render(fmt.Sprintf("  ...and %d more", truncated)))
	}

	// still-in summary (only meaningful while flying)
	if !crashed && stillIn > 0 {
		rows = append(rows, g.root.st.warning.Render(fmt.Sprintf("  %d player%s still in flight", stillIn, pluralS(stillIn))))
	}

	return strings.Join(rows, "\n") + "\n"
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
