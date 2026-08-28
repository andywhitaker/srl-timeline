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
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#da3633")).
		Padding(0, 1).
		Render("⚠️ CONFIRM FULL CONFIGURATION RESTORE")

	warning := lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")).Render(
		"WARNING: This will replace the LIVE switch running configuration\n" +
			"with the snapshot from the selected revision!",
	)

	details := fmt.Sprintf(
		"Target Commit: %s\nAuthor:        %s\nDate:          %s\nSummary:       %s",
		m.Commit.FullSHA[:8],
		m.Commit.Author,
		m.Commit.FormattedTime(),
		m.Commit.Message,
	)

	prompt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render(
		"\nPress [Y] or [Enter] to Confirm Restore  |  Press [N] or [Esc] to Cancel",
	)

	content := fmt.Sprintf("%s\n\n%s\n\n%s\n%s", title, warning, details, prompt)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#da3633")).
		Background(lipgloss.Color("#161b22")).
		Padding(1, 2).
		Width(70).
		Render(content)

	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
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
