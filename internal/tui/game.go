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
	root          *Model
	snap          engine.Snapshot
	betEntry      betModel
	recentCrashes []int  // crashBP of last N settled rounds, newest first
	lastAmountStr string // remembered across rounds
	lastAcStr     string
	lastDisplayName string
	multHistory   []int // CurrentMultBP sampled each flying tick; reset at new round
}

func newGameModel(root *Model) gameModel {
	return gameModel{root: root, betEntry: newBetModel(root)}
}

func (g *gameModel) onSnapshot(snap engine.Snapshot) {
	prev := g.snap
	g.snap = snap

	if snap.State == engine.StateBetting && prev.State != engine.StateBetting {
		g.multHistory = nil
		// Refresh per-round balance and name regardless of queue state.
		g.betEntry.maxBet = g.root.player.Balance
		g.betEntry.playerName = g.lastDisplayName
		// Auto-submit queued bet; otherwise open the form for a fresh bet.
		if g.betEntry.queued {
			g.betEntry.autoSubmitQueue()
		} else {
			g.betEntry.placedThisRound = false
		}
	}

	if snap.State == engine.StateFlying {
		g.multHistory = append(g.multHistory, snap.CurrentMultBP)
	}
}

func (g gameModel) update(msg tea.Msg) (gameModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case " ":
			if g.snap.State == engine.StateFlying {
				g.root.eng.CashOut(g.root.player.ID)
			}
			return g, nil
		case "enter":
			// During the open betting window and not yet placed: submit immediately.
			// In all other states (flying, crashed, idle, or already placed): queue.
			if g.snap.State == engine.StateBetting && !g.betEntry.placedThisRound {
				g.betEntry.submitNow()
			} else {
				g.betEntry.queueForNextRound()
			}
			return g, nil
		}
		// Delegate navigation and editing keys to the form (always active).
		var cmd tea.Cmd
		g.betEntry, cmd = g.betEntry.update(msg)
		if g.betEntry.amountStr != "" {
			g.lastAmountStr = g.betEntry.amountStr
		}
		if g.betEntry.acStr != "" {
			g.lastAcStr = g.betEntry.acStr
		}
		return g, cmd
	}
	return g, nil
}

func (g gameModel) view() string {
	var sb strings.Builder

	if bar := g.historyBar(); bar != "" {
		sb.WriteString(bar)
		sb.WriteString("\n")
	}

	sb.WriteString(g.graphView())
	sb.WriteString("\n")

	sb.WriteString(g.dockView())

	if g.snap.CommitHash != "" {
		short := g.snap.CommitHash
		if len(short) > 16 {
			short = short[:16] + "..."
		}
		sb.WriteString("\n" + g.root.st.dim.Render(fmt.Sprintf("  Commit: %s", short)))
	}

	return sb.String()
}

// contentWidth returns the usable content width, clamped to a sensible range.
func (g gameModel) contentWidth() int {
	w := g.root.width
	if w == 0 {
		w = 80
	}
	w -= 2 // small terminal margin
	if w < 50 {
		w = 50
	}
	return w
}

// graphView builds and returns the braille chart for the current flight.
func (g gameModel) graphView() string {
	displayBP := g.snap.CurrentMultBP
	if g.snap.State == engine.StateCrashed || g.snap.State == engine.StateSettling {
		displayBP = g.snap.CrashMultBP
	}
	if displayBP == 0 {
		displayBP = 100
	}

	history := g.multHistory
	if len(history) == 0 && g.snap.State == engine.StateFlying {
		history = []int{100, displayBP}
	}

	chart := buildGraph(history, displayBP, g.contentWidth(), graphHeight, g.root.st, g.root.renderer)

	if g.snap.State == engine.StateCrashed || g.snap.State == engine.StateSettling {
		chart += "\n" + g.root.st.danger.Render(
			fmt.Sprintf("  Crashed at %sx", engine.FormatMult(g.snap.CrashMultBP)),
		)
	}
	return chart
}

// cashoutValues returns the current total return and profit for this player's active bet.
func (g gameModel) cashoutValues() (total, profit int64) {
	displayBP := g.snap.CurrentMultBP
	if g.snap.State == engine.StateCrashed || g.snap.State == engine.StateSettling {
		displayBP = g.snap.CrashMultBP
	}
	for _, p := range g.snap.Participants {
		if p.PlayerID == g.root.player.ID {
			total = p.Amount * int64(displayBP) / 100
			profit = total - p.Amount
			return
		}
	}
	return 0, 0
}

// ownParticipant finds this player's participant view in the current snapshot.
func (g gameModel) ownParticipant() (engine.ParticipantView, bool) {
	for _, p := range g.snap.Participants {
		if p.PlayerID == g.root.player.ID {
			return p, true
		}
	}
	return engine.ParticipantView{}, false
}

// dockView renders the twin-panel bottom dock.
func (g gameModel) dockView() string {
	cw := g.contentWidth()
	halfW := cw/2 - 1
	if halfW < 20 {
		halfW = 20
	}

	leftContent := g.leftPanelContent()
	rightContent := g.betEntry.view(halfW)

	border := lipgloss.RoundedBorder()
	leftPanel := g.root.renderer.NewStyle().
		Border(border).
		Width(halfW).
		Render(leftContent)
	rightPanel := g.root.renderer.NewStyle().
		Border(border).
		Width(halfW).
		Render(rightContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, " ", rightPanel)
}

// leftPanelContent returns the content for the left dock panel, state-dependent.
func (g gameModel) leftPanelContent() string {
	var sb strings.Builder

	switch g.snap.State {
	case engine.StateFlying:
		p, hasBet := g.ownParticipant()
		switch {
		case hasBet && p.CashedOut:
			sb.WriteString(g.root.st.success.Render(
				fmt.Sprintf("  Cashed out at %sx", engine.FormatMult(p.CashoutMult)),
			) + "\n")
			sb.WriteString(g.root.st.success.Render(
				fmt.Sprintf("  Profit  +%d credits", p.Payout-p.Amount),
			))
		case hasBet:
			total, profit := g.cashoutValues()
			sb.WriteString(g.root.st.warning.Render("  Press SPACE to cash out") + "\n")
			sb.WriteString(g.root.st.mult.Render(
				fmt.Sprintf("  %d credits  (+%d)", total, profit),
			))
		default:
			sb.WriteString(g.root.st.dim.Render("  Spectating this round"))
		}

	case engine.StateBetting:
		remaining := time.Until(g.snap.BettingEndsAt)
		if remaining < 0 {
			remaining = 0
		}
		if g.betEntry.placedThisRound {
			sb.WriteString(g.root.st.success.Render("  Bet placed") + "\n")
			sb.WriteString(g.root.st.dim.Render(
				fmt.Sprintf("  Takeoff in %.1fs", remaining.Seconds()),
			))
		} else {
			sb.WriteString(g.root.st.info.Render("  Betting window open") + "\n")
			sb.WriteString(g.root.st.dim.Render(
				fmt.Sprintf("  Takeoff in %.1fs", remaining.Seconds()),
			))
		}

	case engine.StateCrashed, engine.StateSettling:
		p, hasBet := g.ownParticipant()
		if hasBet {
			if p.CashedOut {
				sb.WriteString(g.root.st.success.Render("  Won this round") + "\n")
				sb.WriteString(g.root.st.success.Render(
					fmt.Sprintf("  +%d credits", p.Payout-p.Amount),
				))
			} else {
				sb.WriteString(g.root.st.danger.Render("  Lost this round") + "\n")
				sb.WriteString(g.root.st.danger.Render(
					fmt.Sprintf("  -%d credits", p.Amount),
				))
			}
		} else {
			sb.WriteString(g.root.st.dim.Render("  Spectated this round"))
		}
		sb.WriteString("\n" + g.root.st.dim.Render("  [i] to inspect result"))

	default:
		sb.WriteString(g.root.st.dim.Render("  Waiting for players..."))
	}

	return sb.String()
}

// historyBar renders a compact row of recent crash multipliers.
func (g gameModel) historyBar() string {
	if len(g.recentCrashes) == 0 {
		return ""
	}
	var parts []string
	for _, bp := range g.recentCrashes {
		label := engine.FormatMult(bp) + "x"
		parts = append(parts, crashMultStyle(g.root.renderer, bp).Render(label))
	}
	return g.root.st.dim.Render("  History  ") + strings.Join(parts, g.root.st.dim.Render("  "))
}

// crashMultStyle returns a colour for a crash multiplier pill.
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

	if g.snap.State == engine.StateBetting {
		if total == 0 {
			return g.root.st.dim.Render("  No bets placed yet.\n")
		}
		return g.root.st.dim.Render(fmt.Sprintf("  %d player%s in the lobby.\n", total, pluralS(total)))
	}

	if total == 0 {
		return g.root.st.dim.Render("  No bets this round.\n")
	}

	stillIn := 0
	for _, p := range parts {
		if !p.CashedOut {
			stillIn++
		}
	}

	sorted := make([]engine.ParticipantView, total)
	copy(sorted, parts)
	sort.SliceStable(sorted, func(i, j int) bool {
		ci, cj := sorted[i].CashedOut, sorted[j].CashedOut
		if ci != cj {
			return ci
		}
		if ci {
			return sorted[i].CashoutMult > sorted[j].CashoutMult
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
		name := maskName(g.root.player.ID, p.PlayerID, p.DisplayName, p.Hidden, g.root.st)
		if len([]rune(name)) > 18 {
			name = string([]rune(name)[:18])
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

// maskName returns "(hidden)" for another player who has hidden their profile.
func maskName(viewerID, subjectID int64, name string, hidden bool, st tuiStyles) string {
	if hidden && subjectID != viewerID {
		return st.dim.Render("(hidden)")
	}
	return name
}
