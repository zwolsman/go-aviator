package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
)

type settingsSavedMsg dbpkg.Player
type settingsErrorMsg string

type settingsModel struct {
	root    *Model
	nameStr string
	saving  bool
	err     string
}

func newSettingsModel(root *Model, currentName string) settingsModel {
	return settingsModel{root: root, nameStr: currentName}
}

func (s settingsModel) update(msg tea.Msg) (settingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsErrorMsg:
		s.saving = false
		s.err = string(msg)

	case tea.KeyMsg:
		if s.saving {
			return s, nil
		}
		switch msg.String() {
		case "enter":
			name := strings.TrimSpace(s.nameStr)
			if name == "" {
				s.err = "display name cannot be empty"
				return s, nil
			}
			if len([]rune(name)) > 24 {
				s.err = "max 24 characters"
				return s, nil
			}
			s.saving = true
			s.err = ""
			return s, s.saveCmd(name)
		case "backspace":
			runes := []rune(s.nameStr)
			if len(runes) > 0 {
				s.nameStr = string(runes[:len(runes)-1])
			}
		default:
			ch := msg.String()
			if len([]rune(ch)) == 1 && len([]rune(s.nameStr)) < 24 {
				s.nameStr += ch
			}
		}
	}
	return s, nil
}

func (s settingsModel) saveCmd(name string) tea.Cmd {
	playerID := s.root.player.ID // stable; never changes during session
	return func() tea.Msg {
		p, err := s.root.queries.UpdateDisplayName(context.Background(), dbpkg.UpdateDisplayNameParams{
			DisplayName: name,
			ID:          playerID,
		})
		if err != nil {
			return settingsErrorMsg(err.Error())
		}
		return settingsSavedMsg(p)
	}
}

func (s settingsModel) view() string {
	var sb strings.Builder
	sb.WriteString(styleBold.Render("  Settings") + "\n\n")

	val := s.nameStr
	if val == "" {
		val = styleDim.Render("_")
	} else {
		val += styleInfo.Render("█")
	}
	sb.WriteString(styleInfo.Render("  Display name: ") + val + "\n\n")

	switch {
	case s.saving:
		sb.WriteString(styleDim.Render("  Saving..."))
	case s.err != "":
		sb.WriteString(styleDanger.Render("  "+s.err) + "\n")
		sb.WriteString(styleDim.Render("  [enter] save  [esc/q] cancel"))
	default:
		sb.WriteString(styleDim.Render("  [enter] save  [esc/q] cancel"))
	}

	return sb.String()
}
