package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dbpkg "github.com/zwolsman/go-aviator/internal/db"
)

type settingsSavedMsg dbpkg.Player
type settingsErrorMsg string

type settingsField int

const (
	settingsFieldName   settingsField = iota
	settingsFieldHidden
)

type settingsModel struct {
	root        *Model
	field       settingsField
	nameStr     string
	hidden      bool
	saving      bool
	savingName  bool // true when the pending save is for the name (closes settings on success)
	err         string
}

func newSettingsModel(root *Model, currentName string, currentHidden bool) settingsModel {
	return settingsModel{root: root, nameStr: currentName, hidden: currentHidden}
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
		case "tab", "down":
			s.field = (s.field + 1) % 2
			s.err = ""
		case "up", "shift+tab":
			if s.field == 0 {
				s.field = settingsFieldHidden
			} else {
				s.field--
			}
			s.err = ""

		case "enter":
			if s.field == settingsFieldName {
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
				s.savingName = true
				s.err = ""
				return s, s.saveNameCmd(name)
			}
			// hidden field: toggle and save immediately, stay in settings
			s.hidden = !s.hidden
			s.saving = true
			s.savingName = false
			s.err = ""
			return s, s.saveHiddenCmd(s.hidden)

		case " ":
			if s.field == settingsFieldHidden {
				s.hidden = !s.hidden
				s.saving = true
				s.savingName = false
				s.err = ""
				return s, s.saveHiddenCmd(s.hidden)
			}

		case "backspace":
			if s.field == settingsFieldName {
				runes := []rune(s.nameStr)
				if len(runes) > 0 {
					s.nameStr = string(runes[:len(runes)-1])
				}
			}

		default:
			if s.field == settingsFieldName {
				ch := msg.String()
				if len([]rune(ch)) == 1 && len([]rune(s.nameStr)) < 24 {
					s.nameStr += ch
				}
			}
		}
	}
	return s, nil
}

func (s settingsModel) saveNameCmd(name string) tea.Cmd {
	playerID := s.root.player.ID
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

func (s settingsModel) saveHiddenCmd(hidden bool) tea.Cmd {
	playerID := s.root.player.ID
	return func() tea.Msg {
		p, err := s.root.queries.UpdateHidden(context.Background(), dbpkg.UpdateHiddenParams{
			Hidden: hidden,
			ID:     playerID,
		})
		if err != nil {
			return settingsErrorMsg(err.Error())
		}
		return settingsSavedMsg(p)
	}
}

func (s settingsModel) view() string {
	var sb strings.Builder
	sb.WriteString(s.root.st.bold.Render("  Settings") + "\n\n")

	// Display name field
	nameCursor := "  "
	if s.field == settingsFieldName {
		nameCursor = s.root.st.info.Render("▶ ")
	}
	val := s.nameStr
	if val == "" {
		val = s.root.st.dim.Render("_")
	} else {
		val += s.root.st.info.Render("█")
	}
	sb.WriteString(nameCursor + s.root.st.info.Render("Display name: ") + val + "\n\n")

	// Hidden field
	hiddenCursor := "  "
	if s.field == settingsFieldHidden {
		hiddenCursor = s.root.st.info.Render("▶ ")
	}
	var hiddenVal string
	if s.hidden {
		hiddenVal = s.root.st.warning.Render("Hidden") + s.root.st.dim.Render(`  (others see "(hidden)" instead of your name)`)
	} else {
		hiddenVal = s.root.st.success.Render("Public") + s.root.st.dim.Render("  (your name is visible to others)")
	}
	sb.WriteString(hiddenCursor + s.root.st.info.Render("Profile:      ") + hiddenVal + "\n\n")

	switch {
	case s.saving:
		sb.WriteString(s.root.st.dim.Render("  Saving..."))
	case s.err != "":
		sb.WriteString(s.root.st.danger.Render("  "+s.err) + "\n")
		sb.WriteString(s.root.st.dim.Render("  [tab] switch field  [enter/space] save/toggle  [esc/q] cancel"))
	default:
		sb.WriteString(s.root.st.dim.Render("  [tab] switch field  [enter/space] save/toggle  [esc/q] cancel"))
	}

	return sb.String()
}
