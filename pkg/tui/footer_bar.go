package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// FooterBarModel renders keybinding shortcuts at the bottom.
type FooterBarModel struct {
	Width int
}

// NewFooterBarModel creates a footer bar.
func NewFooterBarModel(width int) FooterBarModel {
	return FooterBarModel{Width: width}
}

// View renders the bottom keyboard shortcuts bar.
func (m FooterBarModel) View() string {
	shortcuts := []struct {
		key  string
		desc string
	}{
		{"/", "Filter"},
		{"Tab", "Pane"},
		{"d / c / b", "Tabs"},
		{"1 / 2 / 3", "Formats"},
		{"4 / v", "vs Live"},
		{"r", "Restore"},
		{"p", "Cherry-Pick"},
		{"e", "Export"},
		{"g", "Remote Git"},
		{"?", "Help"},
		{"q", "Quit"},
	}

	var parts []string
	for _, sc := range shortcuts {
		k := StyleKeybindKey.Render(sc.key)
		d := StyleKeybindDesc.Render(fmt.Sprintf(" %s", sc.desc))
		parts = append(parts, k+d)
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Center, parts...)
	return StyleFooterContainer.Width(m.Width).Render(bar)
}
