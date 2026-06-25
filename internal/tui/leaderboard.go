package tui

import (
	"context"
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
)

const leaderboardPageSize = 10

type leaderboardLoadedMsg struct {
	rows     []dbpkg.ListLeaderboardRow
	standing dbpkg.GetPlayerStandingRow
	hasStand bool // false when player has no bets
	page     int
}

type leaderboardModel struct {
	root     *Model
	rows     []dbpkg.ListLeaderboardRow
	standing dbpkg.GetPlayerStandingRow
	hasStand bool
	page     int
	cursor   int
	loading  bool
}

func newLeaderboardModel(root *Model) leaderboardModel {
	return leaderboardModel{root: root, loading: true}
}

func (l leaderboardModel) Init() tea.Cmd {
	return l.loadCmd(0)
}

func (l leaderboardModel) loadCmd(page int) tea.Cmd {
	playerID := l.root.player.ID
	offset := int32(page * leaderboardPageSize)
	return func() tea.Msg {
		rows, err := l.root.queries.ListLeaderboard(context.Background(), dbpkg.ListLeaderboardParams{
			Limit:  leaderboardPageSize,
			Offset: offset,
		})
		if err != nil {
			rows = nil
		}

		standing, err := l.root.queries.GetPlayerStanding(context.Background(), playerID)
		hasStand := err == nil

		return leaderboardLoadedMsg{
			rows:     rows,
			standing: standing,
			hasStand: hasStand,
			page:     page,
		}
	}
}

func (l leaderboardModel) update(msg tea.Msg) (leaderboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case leaderboardLoadedMsg:
		l.rows = msg.rows
		l.standing = msg.standing
		l.hasStand = msg.hasStand
		l.page = msg.page
		l.cursor = 0
		l.loading = false

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if l.cursor < len(l.rows)-1 {
				l.cursor++
			}
		case "k", "up":
			if l.cursor > 0 {
				l.cursor--
			}
		case "right", "n":
			if len(l.rows) == leaderboardPageSize {
				l.loading = true
				return l, l.loadCmd(l.page + 1)
			}
		case "left", "p":
			if l.page > 0 {
				l.loading = true
				return l, l.loadCmd(l.page - 1)
			}
		}
	}
	return l, nil
}

func (l leaderboardModel) view() string {
	var sb strings.Builder
	sb.WriteString(l.root.st.bold.Render("  Leaderboard") + "\n\n")

	if l.loading {
		sb.WriteString(l.root.st.dim.Render("  Loading..."))
		return sb.String()
	}

	if len(l.rows) == 0 && l.page == 0 {
		sb.WriteString(l.root.st.dim.Render("  No players ranked yet. Place a bet to appear here."))
		return sb.String()
	}

	// Header row
	sb.WriteString(l.root.st.bold.Render(fmt.Sprintf("  %-5s  %-18s  %-8s  %s", "RANK", "PLAYER", "GAMES", "POINTS")) + "\n")
	sb.WriteString(l.root.st.dim.Render("  "+strings.Repeat("─", 52)) + "\n")

	viewerID := l.root.player.ID
	myRank := l.standing.Rank
	myOnPage := false

	for i, row := range l.rows {
		isMe := row.PlayerID == viewerID
		if isMe {
			myOnPage = true
		}

		name := maskName(viewerID, row.PlayerID, row.DisplayName, row.Hidden, l.root.st)
		if len([]rune(name)) > 18 {
			name = string([]rune(name)[:18])
		}

		cursor := "  "
		if i == l.cursor {
			cursor = l.root.st.info.Render("▶ ")
		}

		rankStr := fmt.Sprintf("#%-4d", row.Rank)
		line := fmt.Sprintf("%s%-5s  %-18s  %-8d  %d",
			cursor, rankStr, name, row.Games, row.Balance)

		if isMe {
			pct := percentile(myRank, l.standing.Total)
			pctStr := l.root.st.dim.Render(fmt.Sprintf(" (top %d%%)", pct))
			line += pctStr
			sb.WriteString(l.root.st.bold.Render(line) + "\n")
		} else {
			sb.WriteString(line + "\n")
		}
	}

	// Pinned own row when not visible on this page
	if l.hasStand && !myOnPage {
		sb.WriteString(l.root.st.dim.Render("  "+strings.Repeat("─", 52)) + "\n")

		name := l.root.player.DisplayName
		if name == "" {
			name = l.root.player.PubkeyFingerprint[:12] + "..."
		}
		if len([]rune(name)) > 18 {
			name = string([]rune(name)[:18])
		}
		pct := percentile(myRank, l.standing.Total)
		pctStr := l.root.st.dim.Render(fmt.Sprintf(" (top %d%%)", pct))
		rankStr := fmt.Sprintf("#%-4d", myRank)
		line := fmt.Sprintf("  %-5s  %-18s  %-8d  %d",
			rankStr, name, l.standing.Games, l.standing.Balance)
		sb.WriteString(l.root.st.bold.Render(line) + pctStr + "\n")
	} else if l.hasStand && len(l.rows) == 0 {
		// page has no rows but player has a standing (shouldn't normally happen)
		sb.WriteString(l.root.st.dim.Render("  No entries on this page.") + "\n")
	}

	// Pagination hint
	pageInfo := fmt.Sprintf("  Page %d", l.page+1)
	if len(l.rows) == leaderboardPageSize {
		pageInfo += "+"
	}
	nav := ""
	if l.page > 0 {
		nav += "[←/p] prev  "
	}
	if len(l.rows) == leaderboardPageSize {
		nav += "[→/n] next  "
	}
	nav += "[esc/q] back"
	sb.WriteString("\n" + l.root.st.dim.Render(pageInfo+"  "+nav))

	return sb.String()
}

// percentile returns ceil(rank/total*100), clamped to [1,100].
func percentile(rank, total int64) int {
	if total == 0 {
		return 100
	}
	pct := int(math.Ceil(float64(rank) / float64(total) * 100))
	if pct < 1 {
		pct = 1
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}
