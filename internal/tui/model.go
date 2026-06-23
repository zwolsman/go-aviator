package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
	"github.com/zwolsman/go-aviator/internal/engine"
)

// screen identifies which view is active.
type screen int

const (
	screenWelcome screen = iota
	screenGame
	screenInspect
	screenSettings
)

// SnapshotMsg wraps an engine snapshot for delivery as a tea.Msg.
type SnapshotMsg engine.Snapshot

// playerRefreshedMsg carries a freshly-loaded player record.
type playerRefreshedMsg dbpkg.Player

// recentCrashesMsg carries crash multipliers (basis points) for recent settled rounds.
type recentCrashesMsg []int

// tuiStyles holds all Lip Gloss styles for a session. Created from the
// per-session renderer so colors work correctly inside containers.
type tuiStyles struct {
	header  lipgloss.Style
	dim     lipgloss.Style
	success lipgloss.Style
	danger  lipgloss.Style
	warning lipgloss.Style
	info    lipgloss.Style
	bold    lipgloss.Style
	mult    lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) tuiStyles {
	return tuiStyles{
		header:  r.NewStyle().Bold(true).Foreground(lipgloss.Color("33")),
		dim:     r.NewStyle().Faint(true),
		success: r.NewStyle().Foreground(lipgloss.Color("82")),
		danger:  r.NewStyle().Foreground(lipgloss.Color("196")),
		warning: r.NewStyle().Foreground(lipgloss.Color("214")),
		info:    r.NewStyle().Foreground(lipgloss.Color("39")),
		bold:    r.NewStyle().Bold(true),
		mult:    r.NewStyle().Bold(true).Foreground(lipgloss.Color("226")),
	}
}

// Model is the root Bubble Tea model.
type Model struct {
	player  dbpkg.Player
	queries *dbpkg.Queries
	eng     *engine.Engine
	snapCh  <-chan engine.Snapshot
	unsubFn func()

	width  int
	height int
	screen screen

	renderer *lipgloss.Renderer
	st       tuiStyles

	game     gameModel
	inspect  inspectModel
	settings settingsModel
	lastSnap engine.Snapshot
	err      string
}

// New creates the root TUI model for a connected player.
func New(
	player dbpkg.Player,
	queries *dbpkg.Queries,
	eng *engine.Engine,
	snapCh <-chan engine.Snapshot,
	unsubFn func(),
	renderer *lipgloss.Renderer,
	isNew bool,
) Model {
	startScreen := screenGame
	if isNew {
		startScreen = screenWelcome
	}
	m := Model{
		player:   player,
		queries:  queries,
		eng:      eng,
		snapCh:   snapCh,
		unsubFn:  unsubFn,
		screen:   startScreen,
		renderer: renderer,
		st:       newStyles(renderer),
	}
	m.game = newGameModel(&m)
	m.game.lastDisplayName = player.DisplayName
	m.inspect = newInspectModel(&m)
	return m
}

// waitForSnapshot is a command that blocks until a snapshot arrives.
func waitForSnapshot(ch <-chan engine.Snapshot) tea.Cmd {
	return func() tea.Msg {
		return SnapshotMsg(<-ch)
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForSnapshot(m.snapCh), m.loadRecentCrashesCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.unsubFn()
			return m, tea.Quit
		}

		if m.screen == screenWelcome {
			if msg.String() == "s" || msg.String() == "S" {
				m.screen = screenSettings
				m.settings = newSettingsModel(&m, m.player.DisplayName)
			} else {
				m.screen = screenGame
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "esc":
			switch m.screen {
			case screenSettings:
				m.screen = screenGame
				return m, nil
			case screenGame:
				if msg.String() == "esc" {
					return m, nil // esc does nothing on game screen
				}
				m.unsubFn()
				return m, tea.Quit
			case screenInspect:
				// delegate to inspect — it decides whether to pop detail or exit
				var cmd tea.Cmd
				m.inspect, cmd = m.inspect.update(msg)
				if m.inspect.shouldExit {
					m.screen = screenGame
					m.inspect.shouldExit = false
				}
				return m, cmd
			}

		case "h", "H":
			if m.screen == screenGame {
				m.screen = screenInspect
				m.inspect = newInspectModel(&m)
				return m, m.inspect.Init()
			}

		case "i", "I":
			if m.screen == screenGame &&
				(m.lastSnap.State == engine.StateCrashed || m.lastSnap.State == engine.StateSettling) {
				m.screen = screenInspect
				m.inspect = newInspectModelDirect(&m)
				return m, m.inspect.Init()
			}

		case "s", "S":
			if m.screen == screenGame {
				m.screen = screenSettings
				m.settings = newSettingsModel(&m, m.player.DisplayName)
				return m, nil
			}
		}

		// delegate key to the active screen
		switch m.screen {
		case screenGame:
			var cmd tea.Cmd
			m.game, cmd = m.game.update(msg)
			return m, cmd
		case screenInspect:
			var cmd tea.Cmd
			m.inspect, cmd = m.inspect.update(msg)
			if m.inspect.shouldExit {
				m.screen = screenGame
				m.inspect.shouldExit = false
			}
			return m, cmd
		case screenSettings:
			var cmd tea.Cmd
			m.settings, cmd = m.settings.update(msg)
			return m, cmd
		}

	case SnapshotMsg:
		snap := engine.Snapshot(msg)
		prevState := m.lastSnap.State
		m.lastSnap = snap
		m.game.onSnapshot(snap)
		cmds := []tea.Cmd{waitForSnapshot(m.snapCh)}
		if snap.State != prevState {
			cmds = append(cmds, m.refreshPlayerCmd())
			if snap.State == engine.StateBetting {
				cmds = append(cmds, m.loadRecentCrashesCmd())
			}
		}
		return m, tea.Batch(cmds...)

	case playerRefreshedMsg:
		m.player = dbpkg.Player(msg)
		m.game.betEntry.maxBet = m.player.Balance
		m.game.betEntry.playerName = m.player.DisplayName
		m.game.lastDisplayName = m.player.DisplayName
		return m, nil

	case recentCrashesMsg:
		m.game.recentCrashes = []int(msg)
		return m, nil

	case settingsSavedMsg:
		m.player = dbpkg.Player(msg)
		m.game.lastDisplayName = m.player.DisplayName
		m.game.betEntry.playerName = m.player.DisplayName
		m.screen = screenGame
		return m, nil

	case settingsErrorMsg:
		if m.screen == screenSettings {
			var cmd tea.Cmd
			m.settings, cmd = m.settings.update(msg)
			return m, cmd
		}

	case roundsLoadedMsg:
		if m.screen == screenInspect {
			var cmd tea.Cmd
			m.inspect, cmd = m.inspect.update(msg)
			return m, cmd
		}

	case betsLoadedMsg:
		if m.screen == screenInspect {
			var cmd tea.Cmd
			m.inspect, cmd = m.inspect.update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m Model) View() string {
	header := m.headerView()
	if m.screen == screenWelcome {
		return m.welcomeView()
	}

	var body string
	switch m.screen {
	case screenGame:
		body = m.game.view()
	case screenInspect:
		body = m.inspect.view()
	case screenSettings:
		body = m.settings.view()
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, body)
	if m.width == 0 || m.height == 0 {
		return content
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) headerView() string {
	name := m.player.DisplayName
	if name == "" {
		name = m.player.PubkeyFingerprint[:12] + "..."
	}
	bal := fmt.Sprintf("Balance: %d credits", m.player.Balance)
	roundInfo := ""
	if m.lastSnap.RoundID > 0 {
		roundInfo = fmt.Sprintf("  Round: GAME-%d", m.lastSnap.RoundID)
	}
	var hint string
	switch m.screen {
	case screenInspect:
		hint = m.st.dim.Render("  [q/esc] back")
	case screenSettings:
		hint = m.st.dim.Render("  [esc/q] cancel")
	default:
		hint = m.st.dim.Render("  [q] quit  [h] history  [s] settings")
	}
	return m.st.header.Render(fmt.Sprintf("✈ Aviator  %s  %s%s%s", name, bal, roundInfo, hint))
}

// refreshPlayerCmd returns a command that fetches the current player record from DB.
func (m Model) refreshPlayerCmd() tea.Cmd {
	fp := m.player.PubkeyFingerprint
	return func() tea.Msg {
		p, err := m.queries.GetPlayerByFingerprint(context.Background(), fp)
		if err != nil {
			return nil
		}
		return playerRefreshedMsg(p)
	}
}

// loadRecentCrashesCmd fetches crash multipliers for the last 10 settled rounds.
func (m Model) loadRecentCrashesCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := m.queries.ListRecentRounds(context.Background(), 10)
		if err != nil || len(rows) == 0 {
			return recentCrashesMsg(nil)
		}
		crashes := make([]int, len(rows))
		for i, r := range rows {
			crashes[i] = int(r.CrashMultiplier)
		}
		return recentCrashesMsg(crashes)
	}
}
