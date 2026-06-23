package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
	"github.com/zwolsman/go-aviator/internal/engine"
)

type inspectViewMode int

const (
	inspectList   inspectViewMode = iota
	inspectDetail                 // drill-down from list, or direct from [i]
)

type inspectModel struct {
	root         *Model
	rounds       []dbpkg.ListRecentRoundsWithStatsRow
	cursor       int
	loading      bool
	bets         []dbpkg.GetBetsForRoundRow
	betsLoading  bool
	betsForID    int64
	viewMode     inspectViewMode
	directDetail bool // opened via [i]: q/esc exits immediately, no list shown
	shouldExit   bool
}

// roundsLoadedMsg carries the history list (with stats).
type roundsLoadedMsg []dbpkg.ListRecentRoundsWithStatsRow

// betsLoadedMsg carries per-round bet results, tagged with the round they belong to.
type betsLoadedMsg struct {
	forRound int64
	bets     []dbpkg.GetBetsForRoundRow
}

// newInspectModel opens the history list view (via [h]).
func newInspectModel(root *Model) inspectModel {
	return inspectModel{root: root, loading: true, viewMode: inspectList}
}

// newInspectModelDirect opens the detail view for the most recent round (via [i]).
func newInspectModelDirect(root *Model) inspectModel {
	return inspectModel{root: root, loading: true, viewMode: inspectList, directDetail: true}
}

func (m inspectModel) Init() tea.Cmd {
	return m.loadRounds()
}

func (m inspectModel) loadRounds() tea.Cmd {
	return func() tea.Msg {
		rows, err := m.root.queries.ListRecentRoundsWithStats(context.Background(), 20)
		if err != nil {
			return roundsLoadedMsg(nil)
		}
		return roundsLoadedMsg(rows)
	}
}

func (m inspectModel) loadBetsCmd() tea.Cmd {
	if len(m.rounds) == 0 {
		return nil
	}
	roundID := m.rounds[m.cursor].ID
	return func() tea.Msg {
		bets, err := m.root.queries.GetBetsForRound(context.Background(), roundID)
		if err != nil {
			return betsLoadedMsg{forRound: roundID}
		}
		return betsLoadedMsg{forRound: roundID, bets: bets}
	}
}

func (m inspectModel) update(msg tea.Msg) (inspectModel, tea.Cmd) {
	switch msg := msg.(type) {
	case roundsLoadedMsg:
		m.loading = false
		m.rounds = []dbpkg.ListRecentRoundsWithStatsRow(msg)
		if len(m.rounds) == 0 {
			return m, nil
		}
		if m.directDetail {
			// jump straight to detail for the most recent round
			m.viewMode = inspectDetail
			m.betsLoading = true
			m.betsForID = m.rounds[0].ID
			return m, m.loadBetsCmd()
		}

	case betsLoadedMsg:
		if len(m.rounds) > 0 && msg.forRound == m.rounds[m.cursor].ID {
			m.betsLoading = false
			m.bets = msg.bets
		}

	case tea.KeyMsg:
		switch m.viewMode {
		case inspectList:
			switch msg.String() {
			case "q", "esc":
				m.shouldExit = true
			case "up", "k":
				if !m.loading && m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if !m.loading && m.cursor < len(m.rounds)-1 {
					m.cursor++
				}
			case "enter":
				if !m.loading && len(m.rounds) > 0 {
					m.viewMode = inspectDetail
					m.bets = nil
					m.betsLoading = true
					m.betsForID = m.rounds[m.cursor].ID
					return m, m.loadBetsCmd()
				}
			}

		case inspectDetail:
			switch msg.String() {
			case "q", "esc":
				if m.directDetail {
					m.shouldExit = true
				} else {
					// back to list
					m.viewMode = inspectList
					m.bets = nil
				}
			}
		}
	}
	return m, nil
}

func (m inspectModel) view() string {
	switch m.viewMode {
	case inspectList:
		return m.listView()
	case inspectDetail:
		return m.detailView()
	}
	return ""
}

func (m inspectModel) listView() string {
	if m.loading {
		return styleDim.Render("  Loading history...")
	}
	if len(m.rounds) == 0 {
		return styleDim.Render("  No completed rounds yet.")
	}

	var sb strings.Builder
	sb.WriteString(styleDim.Render("  [↑/↓] navigate   [enter] inspect   [q/esc] back") + "\n\n")

	for i, r := range m.rounds {
		prefix := "  "
		if i == m.cursor {
			prefix = styleInfo.Render("▶ ")
		}
		multStyle := crashMultStyle(int(r.CrashMultiplier))
		line := fmt.Sprintf("%sGAME-%-5d  %s",
			prefix,
			r.ID,
			multStyle.Render(engine.FormatMult(int(r.CrashMultiplier))+"x"),
		)
		if r.TotalPayout > 0 {
			line += styleDim.Render(fmt.Sprintf("   paid out: %d", r.TotalPayout))
		}
		sb.WriteString(line + "\n")
	}

	return sb.String()
}

func (m inspectModel) detailView() string {
	if m.loading {
		return styleDim.Render("  Loading history...")
	}
	if len(m.rounds) == 0 {
		return styleDim.Render("  No completed rounds yet.")
	}

	var nav string
	if m.directDetail {
		nav = styleDim.Render("  [q/esc] back")
	} else {
		nav = styleDim.Render("  [q/esc] back to list")
	}

	if m.betsLoading {
		return nav + "\n\n" + styleDim.Render("  Loading round details...")
	}

	round := m.rounds[m.cursor]
	var sb strings.Builder
	sb.WriteString(nav + "\n\n")

	multStyle := crashMultStyle(int(round.CrashMultiplier))
	sb.WriteString(styleBold.Render(fmt.Sprintf("  GAME-%d", round.ID)) +
		"  crashed at " + multStyle.Render(engine.FormatMult(int(round.CrashMultiplier))+"x") + "\n\n")

	// winners only
	var winners []dbpkg.GetBetsForRoundRow
	for _, b := range m.bets {
		if b.CashedOutAtMultiplier.Valid {
			winners = append(winners, b)
		}
	}
	sort.Slice(winners, func(i, j int) bool {
		return winners[i].CashedOutAtMultiplier.Int32 > winners[j].CashedOutAtMultiplier.Int32
	})

	if round.TotalPot > 0 {
		sb.WriteString(fmt.Sprintf("  Total pot:    %s credits\n", styleBold.Render(fmt.Sprintf("%d", round.TotalPot))))
	}
	if round.TotalPayout > 0 {
		sb.WriteString(fmt.Sprintf("  Total payout: %s credits\n", styleSuccess.Render(fmt.Sprintf("%d", round.TotalPayout))))
	}

	if len(winners) > 0 {
		sb.WriteString("\n")
		sb.WriteString(styleBold.Render("  PLAYER              BET        CASHED OUT") + "\n")
		sb.WriteString(styleDim.Render("  "+strings.Repeat("─", 52)) + "\n")
		for _, b := range winners {
			name := b.DisplayName
			if len(name) > 18 {
				name = name[:18]
			}
			sb.WriteString(fmt.Sprintf("  %-18s  %-9d  %s\n",
				name,
				b.Amount,
				styleSuccess.Render(fmt.Sprintf("✓ @%sx  +%d",
					engine.FormatMult(int(b.CashedOutAtMultiplier.Int32)),
					b.Payout.Int64)),
			))
		}
	} else if len(m.bets) > 0 {
		sb.WriteString(styleDim.Render("  No winners this round.") + "\n")
	} else {
		sb.WriteString(styleDim.Render("  No bets placed this round.") + "\n")
	}

	// provably fair
	preimage, computed := engine.Commit(round.ID, int(round.CrashMultiplier), round.Salt)
	ok := computed == round.CommitHash
	sb.WriteString("\n" + styleBold.Render("  Provably Fair") + "\n")
	sb.WriteString(styleDim.Render("  "+strings.Repeat("─", 60)) + "\n")
	sb.WriteString(fmt.Sprintf("  Preimage : %s\n", preimage))
	sb.WriteString(fmt.Sprintf("  Commit   : %s\n", round.CommitHash))
	if ok {
		sb.WriteString("  " + styleSuccess.Render("✓ VERIFIED"))
	} else {
		sb.WriteString("  " + styleDanger.Render("✗ MISMATCH"))
	}
	sb.WriteString("\n" + styleDim.Render("  Verify: echo -n '"+preimage+"' | sha256sum"))

	return sb.String()
}
