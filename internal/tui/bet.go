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
	playerName string // kept in sync via playerRefreshedMsg; avoids stale root-pointer read
	field      betField
	amountStr  string
	acStr      string // auto-cashout string, empty = none
	maxBet     int64
	err        string
	submitted  bool
}

func newBetModel(root *Model) betModel {
	return betModel{
		root:       root,
		playerName: root.player.DisplayName,
		maxBet:     root.player.Balance,
	}
}

func (b betModel) update(msg tea.Msg) (betModel, tea.Cmd) {
	if b.submitted {
		return b, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return b, nil
	}

	switch key.String() {
	case "tab", "down":
		b.field = (b.field + 1) % 2
	case "up", "shift+tab":
		b.field = (b.field + 1) % 2
	case "enter":
		return b.submit()
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
	default:
		ch := key.String()
		if len(ch) == 1 && ch[0] >= '0' && ch[0] <= '9' {
			switch b.field {
			case fieldAmount:
				b.amountStr += ch
			case fieldAutoCashout:
				b.acStr += ch
			}
		} else if ch == "." && b.field == fieldAutoCashout {
			if !strings.Contains(b.acStr, ".") {
				b.acStr += ch
			}
		}
	}
	b.err = ""
	return b, nil
}

func (b betModel) submit() (betModel, tea.Cmd) {
	amount, err := strconv.ParseInt(b.amountStr, 10, 64)
	if err != nil || amount <= 0 {
		b.err = "Invalid amount"
		return b, nil
	}
	if amount > b.maxBet {
		b.err = fmt.Sprintf("Max bet is %d", b.maxBet)
		return b, nil
	}

	autoCashoutBP := 0
	if b.acStr != "" {
		f, err := strconv.ParseFloat(b.acStr, 64)
		if err != nil || f < 1.01 {
			b.err = "Auto-cashout must be >= 1.01"
			return b, nil
		}
		autoCashoutBP = int(f * 100)
	}

	b.submitted = true
	b.root.eng.PlaceBet(b.root.player.ID, b.playerName, amount, autoCashoutBP)
	return b, nil
}

func (b betModel) view() string {
	if b.submitted {
		return styleSuccess.Render("  Bet placed! Waiting for takeoff...")
	}

	var sb strings.Builder
	sb.WriteString(styleBold.Render("  Place your bet") + "\n\n")

	// amount field
	amountLabel := "  Amount: "
	if b.field == fieldAmount {
		amountLabel = styleInfo.Render("▶ Amount: ")
	}
	amountVal := b.amountStr
	if amountVal == "" {
		amountVal = styleDim.Render("_")
	}
	sb.WriteString(amountLabel + amountVal + fmt.Sprintf(styleDim.Render("  (max: %d)"), b.maxBet) + "\n")

	// auto-cashout field
	acLabel := "  Auto-cashout: "
	if b.field == fieldAutoCashout {
		acLabel = styleInfo.Render("▶ Auto-cashout: ")
	}
	acVal := b.acStr
	if acVal == "" {
		acVal = styleDim.Render("_  (blank = manual)")
	}
	sb.WriteString(acLabel + acVal + "\n")

	if b.err != "" {
		sb.WriteString(styleDanger.Render("  Error: "+b.err) + "\n")
	}

	// growth preview
	if b.acStr != "" {
		if f, err := strconv.ParseFloat(b.acStr, 64); err == nil {
			bp := int(f * 100)
			sb.WriteString(styleDim.Render(fmt.Sprintf("  Auto-cashout at %sx", engine.FormatMult(bp))) + "\n")
		}
	}

	sb.WriteString(styleDim.Render("  [tab] switch field  [enter] place bet  [space] cashout while flying"))
	return sb.String()
}
