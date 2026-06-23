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
}

func newGameModel(root *Model) gameModel {
	return gameModel{root: root, betEntry: newBetModel(root)}
}

func (g *gameModel) onSnapshot(snap engine.Snapshot) {
	prev := g.snap
	g.snap = snap

	// auto-show bet form when a new betting window opens
	if snap.State == engine.StateBetting && prev.State != engine.StateBetting {
		g.showBetForm = true
		g.betEntry = newBetModel(g.root)
		g.betEntry.amountStr = g.lastAmountStr
		g.betEntry.acStr = g.lastAcStr
		g.betEntry.playerName = g.lastDisplayName
		// maxBet is corrected by the playerRefreshedMsg that fires on state change
	}
	// hide form when flying
	if snap.State == engine.StateFlying {
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

func (g gameModel) planeView() string {
	multStr := engine.FormatMult(g.snap.CurrentMultBP)
	if g.snap.CurrentMultBP == 0 {
		multStr = "1.00"
	}

	height := planeHeight(g.snap.CurrentMultBP)

	var lines []string
	// draw the multiplier on the correct row
	for i := 12; i >= 0; i-- {
		if i == height {
			plane := "  ✈ " + g.root.st.mult.Render(multStr+"x")
			lines = append(lines, plane)
		} else {
			lines = append(lines, "")
		}
	}

	if g.snap.State == engine.StateCrashed {
		lines = append(lines, g.root.st.danger.Render("  💥 CRASHED at "+engine.FormatMult(g.snap.CrashMultBP)+"x"))
	} else if g.snap.State == engine.StateIdle {
		lines = append(lines, g.root.st.dim.Render("  Waiting..."))
	}

	// trajectory line
	trajectory := buildTrajectory(g.snap.CurrentMultBP)

	return lipgloss.JoinVertical(lipgloss.Left,
		strings.Join(lines, "\n"),
		trajectory,
	)
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

func buildTrajectory(multBP int) string {
	cols := 60
	pos := int(float64(multBP) / 100.0 * 5)
	if pos > cols {
		pos = cols
	}
	return "  " + strings.Repeat("─", pos)
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
