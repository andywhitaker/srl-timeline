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
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
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
		{"T", "Cycle color theme (Dark, Cobalt2, Monokai, Solarized, ...)"},
		{"?", "Show this Help modal"},
		{"q", "Quit application"},
	}

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorActive)
	sepStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	descStyle := lipgloss.NewStyle().Foreground(ColorText)

	innerW := 66
	if m.Width > 0 && m.Width-6 < innerW {
		innerW = m.Width - 6
	}
	if innerW < 10 {
		innerW = 10
	}

	var sb strings.Builder
	for _, sc := range shortcuts {
		k := keyStyle.Render(fmt.Sprintf("%-13s", sc[0]))
		sep := sepStyle.Render(" : ")
		d := descStyle.Render(sc[1])
		sb.WriteString(PaintBarLine(fmt.Sprintf("%s%s%s", k, sep, d), innerW, ColorSurface, ColorText) + "\n")
	}

	compact := m.Height > 0 && m.Height < 28
	footerText := "Press [Esc], [Enter], or [?] to close"
	if !compact {
		footerText = "\n" + footerText
	}
	footer := lipgloss.NewStyle().Foreground(ColorMuted).Render(footerText)

	var content string
	vPad := 1
	if compact {
		vPad = 0
		content = fmt.Sprintf("%s\n%s%s", title, sb.String(), footer)
	} else {
		content = fmt.Sprintf("%s\n\n%s%s", title, sb.String(), footer)
	}
	maxInnerH := 0
	if m.Height > 2+vPad*2 {
		lines := strings.Split(content, "\n")
		if len(lines) > m.Height-(2+vPad*2) {
			maxInnerH = m.Height - (2 + vPad*2)
		}
	}
	paintedContent := PaintSurface(content, innerW, maxInnerH, ColorSurface, ColorText)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorActive).
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
