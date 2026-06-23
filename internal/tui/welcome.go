package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/zwolsman/go-aviator/internal/auth"
)

func (m Model) welcomeView() string {
	r := m.renderer
	name := m.player.DisplayName
	if name == "" {
		name = m.player.PubkeyFingerprint[:12] + "..."
	}

	greeting := r.NewStyle().Bold(true).Foreground(lipgloss.Color("33")).Render("Welcome, ") +
		r.NewStyle().Bold(true).Italic(true).Foreground(lipgloss.Color("33")).Render(name) +
		r.NewStyle().Bold(true).Foreground(lipgloss.Color("33")).Render("!")

	body := strings.Join([]string{
		greeting,
		"",
		r.NewStyle().Render(fmt.Sprintf("You've received %s credits to get you started.", r.NewStyle().Bold(true).Foreground(lipgloss.Color("226")).Render(fmt.Sprintf("%d", auth.StartingBalance)))),
		r.NewStyle().Foreground(lipgloss.Color("82")).Render(fmt.Sprintf("✦  Earn %d credits every day just for connecting.", auth.DailyCreditGrant)),
		"",
		m.st.dim.Render("✦  Press") + " " + m.st.bold.Render("[s]") + " " + m.st.dim.Render("to open Settings and set your display name."),
		"",
		"",
		m.st.dim.Render("Press") + " " + m.st.bold.Render("enter") + " " + m.st.dim.Render("or") + " " + m.st.bold.Render("space") + " " + m.st.dim.Render("to start playing."),
	}, "\n")

	box := r.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("33")).
		Padding(1, 3).
		Render(body)

	if m.width == 0 || m.height == 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
