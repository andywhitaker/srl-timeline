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
func (m FooterBarModel) View(filterActive bool) string {
	var shortcuts []struct {
		key  string
		desc string
	}

	if filterActive {
		shortcuts = append(shortcuts, struct {
			key  string
			desc string
		}{"Esc", "Clear"})
	}

	if m.Width >= 115 {
		shortcuts = append(shortcuts, []struct {
			key  string
			desc string
		}{
			{"/", "Filter"},
			{"Tab", "Pane"},
			{"d/c/b", "View"},
			{"1-3", "Format"},
			{"4/v", "Live"},
			{"r", "Restore"},
			{"p", "Cherry"},
			{"e", "Export"},
			{"g", "Git"},
			{"?", "Help"},
			{"q", "Quit"},
		}...)
	} else {
		shortcuts = append(shortcuts, []struct {
			key  string
			desc string
		}{
			{"/", "Filter"},
			{"Tab", "Pane"},
			{"d/c/b", "View"},
			{"r", "Restore"},
			{"?", "Help"},
			{"q", "Quit"},
		}...)
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
