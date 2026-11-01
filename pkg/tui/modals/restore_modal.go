package modals

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/models"
)

// RestoreModalModel handles full configuration restore confirmation.
type RestoreModalModel struct {
	Commit    models.TimelineCommit
	Confirmed bool
	Width     int
	Height    int
}

// NewRestoreModal creates a new restore confirmation modal.
func NewRestoreModal(commit models.TimelineCommit, width, height int) RestoreModalModel {
	return RestoreModalModel{
		Commit: commit,
		Width:  width,
		Height: height,
	}
}

// View renders the restore confirmation dialog.
func (m RestoreModalModel) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorDangerBg).
		Padding(0, 1).
		Render("⚠️ CONFIRM FULL CONFIGURATION RESTORE")

	warning := lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render(
		"WARNING: This will replace the LIVE switch running configuration\n" +
			"with the snapshot from the selected revision!",
	)

	shortSHA := m.Commit.FullSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	lblStyle := lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	valStyle := lipgloss.NewStyle().Foreground(ColorText)
	shaStyle := lipgloss.NewStyle().Foreground(ColorActive).Bold(true)

	details := fmt.Sprintf(
		"%s %s\n%s %s\n%s %s\n%s %s",
		lblStyle.Render("Target Commit:"), shaStyle.Render(shortSHA),
		lblStyle.Render("Author:       "), valStyle.Render(m.Commit.Author),
		lblStyle.Render("Date:         "), valStyle.Render(m.Commit.FormattedTime()),
		lblStyle.Render("Summary:      "), valStyle.Render(m.Commit.Message),
	)

	prompt := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render(
		"\nPress [Y] or [Enter] to Confirm Restore  |  Press [N] or [Esc] to Cancel",
	)

	content := fmt.Sprintf("%s\n\n%s\n\n%s\n%s", title, warning, details, prompt)
	innerW := 66
	if m.Width > 0 && m.Width-6 < innerW {
		innerW = m.Width - 6
	}
	if innerW < 10 {
		innerW = 10
	}
	vPad := 1
	if m.Height > 0 && m.Height < 20 {
		vPad = 0
	}
	paintedContent := PaintSurface(content, innerW, 0, ColorSurface, ColorText)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorDangerBg).
		BorderBackground(ColorSurface).
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(vPad, 2).
		Render(paintedContent)

	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(ColorBackground),
		lipgloss.WithWhitespaceForeground(ColorTextDim),
	)
}

// Update processes key events.
func (m RestoreModalModel) Update(msg tea.Msg) (RestoreModalModel, bool, bool) {
	// Returns (model, confirmed, close)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			return m, true, true
		case "n", "N", "esc":
			return m, false, true
		}
	}
	return m, false, false
}
