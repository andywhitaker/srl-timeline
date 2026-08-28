package modals

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpModalModel displays the keyboard navigation cheat sheet.
type HelpModalModel struct {
	Width  int
	Height int
}

// NewHelpModal creates a help modal.
func NewHelpModal(width, height int) HelpModalModel {
	return HelpModalModel{Width: width, Height: height}
}

// View renders the help dialog.
func (m HelpModalModel) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#1f6feb")).
		Padding(0, 1).
		Render("⌨️ KEYBOARD SHORTCUTS & NAVIGATION")

	shortcuts := [][]string{
		{"/", "Focus search / path filter bar (typing goes here)"},
		{"Enter", "Apply search filter & return to timeline"},
		{"Esc", "Clear filter / Cancel search / Close modal"},
		{"Tab", "Toggle focus between [Timeline] and [Detail View]"},
		{"d", "Switch to Diff View"},
		{"c", "Switch to Full Config View"},
		{"b", "Switch to Blame View"},
		{"[ / ] / t", "Cycle Top Tabs (Diff -> Config -> Blame)"},
		{"1 / 2 / 3", "Select Sub-Formats (Unified / Paths / Flat CLI / JSON)"},
		{"4 / v", "Toggle Diff vs Live Running mode"},
		{"j / k / ↑ / ↓", "Navigate commits (in Timeline) or Scroll (in Detail)"},
		{"r", "Restore full configuration from selected commit"},
		{"p", "Cherry-Pick restore specific subtrees"},
		{"R / F5", "Live reload / refresh timeline from disk"},
		{"e", "Export configuration to file or startup config"},
		{"g", "Remote Git settings & synchronization"},
		{"?", "Show this Help modal"},
		{"q", "Quit application"},
	}

	var sb strings.Builder
	for _, sc := range shortcuts {
		k := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render(fmt.Sprintf("%-12s", sc[0]))
		d := lipgloss.NewStyle().Foreground(lipgloss.Color("#e6edf3")).Render(sc[1])
		sb.WriteString(fmt.Sprintf("%s : %s\n", k, d))
	}

	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(
		"\nPress [Esc], [Enter], or [?] to close",
	)

	content := fmt.Sprintf("%s\n\n%s%s", title, sb.String(), footer)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#58a6ff")).
		Background(lipgloss.Color("#161b22")).
		Padding(1, 2).
		Width(65).
		Render(content)

	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
}

// Update processes help modal close events.
func (m HelpModalModel) Update(msg tea.Msg) (HelpModalModel, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "enter", "?", "q":
			return m, true
		}
	}
	return m, false
}
