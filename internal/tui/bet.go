package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zwolsman/go-aviator/internal/engine"
)

type betField int

const (
	fieldAmount betField = iota
	fieldAutoCashout
)

type betModel struct {
	root       *Model
	playerName string
	field      betField
	amountStr  string
	acStr      string // auto cashout; blank = manual
	maxBet     int64
	err        string

	placedThisRound bool // bet sent to engine for the current round
	queued          bool // form confirmed for next round (Enter pressed); still editable
}

func newBetModel(root *Model) betModel {
	return betModel{
		root:       root,
		playerName: root.player.DisplayName,
		maxBet:     root.player.Balance,
	}
}

// update handles navigation and editing keys.
// Enter is handled by game.go because it depends on the current game state.
func (b betModel) update(msg tea.Msg) (betModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return b, nil
	}

	switch key.String() {
	case "tab", "down":
		b.field = (b.field + 1) % 2
	case "up", "shift+tab":
		b.field = (b.field + 1) % 2
	case "esc":
		if b.queued {
			b.queued = false
			b.err = ""
		}
	case "backspace":
		switch b.field {
		case fieldAmount:
			if len(b.amountStr) > 0 {
				b.amountStr = b.amountStr[:len(b.amountStr)-1]
			}
		case fieldAutoCashout:
			if len(b.acStr) > 0 {
				b.acStr = b.acStr[:len(b.acStr)-1]
			}
		}
		b.err = ""
	default:
		ch := key.String()
		if len(ch) == 1 && ch[0] >= '0' && ch[0] <= '9' {
			switch b.field {
			case fieldAmount:
				b.amountStr += ch
			case fieldAutoCashout:
				b.acStr += ch
			}
			b.err = ""
		} else if ch == "." && b.field == fieldAutoCashout {
			if !strings.Contains(b.acStr, ".") {
				b.acStr += ch
				b.err = ""
			}
		}
	}
	return b, nil
}

// parseValues validates the current form state.
func (b betModel) parseValues() (amount int64, autoCashoutBP int, errMsg string) {
	amt, err := strconv.ParseInt(b.amountStr, 10, 64)
	if err != nil || amt <= 0 {
		return 0, 0, "Enter a valid amount"
	}
	if amt > b.maxBet {
		return 0, 0, fmt.Sprintf("Maximum bet is %d", b.maxBet)
	}
	if b.acStr != "" {
		f, err := strconv.ParseFloat(b.acStr, 64)
		if err != nil || f < 1.01 {
			return 0, 0, "Auto cashout must be 1.01 or higher"
		}
		autoCashoutBP = int(f * 100)
	}
	return amt, autoCashoutBP, ""
}

// submitNow validates and sends the bet to the engine immediately.
// Call only when the betting window is open and no bet has been placed yet this round.
func (b *betModel) submitNow() {
	amount, ac, errMsg := b.parseValues()
	if errMsg != "" {
		b.err = errMsg
		return
	}
	b.root.eng.PlaceBet(b.root.player.ID, b.playerName, amount, ac)
	b.placedThisRound = true
	b.queued = false
	b.err = ""
}

// queueForNextRound validates and confirms the current form as a queued bet.
// The form remains editable; the queued indicator updates live as the user types.
func (b *betModel) queueForNextRound() {
	_, _, errMsg := b.parseValues()
	if errMsg != "" {
		b.err = errMsg
		return
	}
	b.queued = true
	b.err = ""
}

// autoSubmitQueue fires the queued bet. Call on the StateBetting transition.
// Reads from the current form values so edits made after queuing are respected.
func (b *betModel) autoSubmitQueue() {
	if !b.queued {
		return
	}
	amount, ac, errMsg := b.parseValues()
	if errMsg != "" {
		b.err = errMsg
		b.queued = false
		return
	}
	b.root.eng.PlaceBet(b.root.player.ID, b.playerName, amount, ac)
	b.placedThisRound = true
	b.queued = false
	b.err = ""
}

// view renders the "Next Round" right-hand dock panel.
func (b betModel) view(_ int) string {
	var sb strings.Builder

	// Amount field
	amountCursor := "  "
	if b.field == fieldAmount {
		amountCursor = b.root.st.info.Render("▶ ")
	}
	amountVal := b.amountStr
	if amountVal == "" {
		amountVal = b.root.st.dim.Render("_")
	}
	maxHint := ""
	if b.maxBet > 0 {
		maxHint = b.root.st.dim.Render(fmt.Sprintf("  (max %d)", b.maxBet))
	}
	sb.WriteString(amountCursor + "Amount:       " + amountVal + maxHint + "\n")

	// Auto Cashout field
	acCursor := "  "
	if b.field == fieldAutoCashout {
		acCursor = b.root.st.info.Render("▶ ")
	}
	acVal := b.acStr
	if acVal == "" {
		acVal = b.root.st.dim.Render("_ (manual)")
	} else {
		if f, err := strconv.ParseFloat(b.acStr, 64); err == nil {
			acVal = b.acStr + "x" + b.root.st.dim.Render(
				fmt.Sprintf(" (%s)", engine.FormatMult(int(f*100))),
			)
		}
	}
	sb.WriteString(acCursor + "Auto Cashout: " + acVal + "\n")

	// Status line
	switch {
	case b.placedThisRound && b.queued:
		sb.WriteString(b.root.st.success.Render("  Bet placed this round") + "\n")
		nextStr := "  Next round: " + b.amountStr + " credits"
		if b.acStr != "" {
			nextStr += " @ " + b.acStr + "x"
		}
		sb.WriteString(b.root.st.info.Render(nextStr))
		sb.WriteString("\n" + b.root.st.dim.Render("  Esc to cancel, edit to adjust"))
	case b.placedThisRound:
		sb.WriteString(b.root.st.success.Render("  Bet placed — waiting for takeoff"))
		sb.WriteString("\n" + b.root.st.dim.Render("  Enter to queue next round"))
	case b.queued:
		queuedStr := "  Queued: " + b.amountStr + " credits"
		if b.acStr != "" {
			queuedStr += " @ " + b.acStr + "x"
		}
		sb.WriteString(b.root.st.success.Render(queuedStr))
		sb.WriteString("\n" + b.root.st.dim.Render("  Esc to cancel, edit to adjust"))
	default:
		sb.WriteString(b.root.st.dim.Render("  Tab to switch field, Enter to place bet"))
	}

	if b.err != "" {
		sb.WriteString("\n" + b.root.st.danger.Render("  " + b.err))
	}

	return sb.String()
}
