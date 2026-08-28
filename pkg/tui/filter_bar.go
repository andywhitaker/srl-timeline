package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FilterBarModel manages the search and scoped filter input bar.
type FilterBarModel struct {
	TextInput  textinput.Model
	MatchCount int
	TotalCount int
	Width      int
	IsFocused  bool
}

// NewFilterBarModel creates a new FilterBar component.
func NewFilterBarModel(width int) FilterBarModel {
	ti := textinput.New()
	ti.Placeholder = "Type path or element (e.g. interface, bgp, acl, system)... [Press '/' to search]"
	ti.Prompt = " 🔍 FILTER: "
	ti.PromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff"))
	ti.TextStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff"))
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7d8590"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
	ti.Width = width - 35

	return FilterBarModel{
		TextInput:  ti,
		MatchCount: 0,
		TotalCount: 0,
		Width:      width,
		IsFocused:  false,
	}
}

// SetCounts updates the match count badge.
func (m *FilterBarModel) SetCounts(matching, total int) {
	m.MatchCount = matching
	m.TotalCount = total
}

// Value returns the current filter query string.
func (m FilterBarModel) Value() string {
	return m.TextInput.Value()
}

// Focus activates the input field.
func (m *FilterBarModel) Focus() tea.Cmd {
	m.IsFocused = true
	m.TextInput.PromptStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#1f6feb")).
		Padding(0, 1)
	return m.TextInput.Focus()
}

// Blur deactivates the input field.
func (m *FilterBarModel) Blur() {
	m.IsFocused = false
	m.TextInput.PromptStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#58a6ff"))
	m.TextInput.Blur()
}

// Clear resets the input field.
func (m *FilterBarModel) Clear() {
	m.TextInput.SetValue("")
}

// SetWidth handles terminal resizing.
func (m *FilterBarModel) SetWidth(width int) {
	m.Width = width
	m.TextInput.Width = width - 35
}

// Update processes Bubble Tea input messages.
func (m FilterBarModel) Update(msg tea.Msg) (FilterBarModel, tea.Cmd) {
	var cmd tea.Cmd
	m.TextInput, cmd = m.TextInput.Update(msg)
	return m, cmd
}

// View renders the search bar with badge and status.
func (m FilterBarModel) View() string {
	var badgeText string
	if m.Value() == "" {
		badgeText = fmt.Sprintf("All Commits (%d)", m.TotalCount)
	} else {
		badgeText = fmt.Sprintf("Filtered (%d/%d matching)", m.MatchCount, m.TotalCount)
	}

	badgeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#21262d")).
		Foreground(lipgloss.Color("#58a6ff")).
		Bold(true).
		Padding(0, 1)

	if m.Value() != "" {
		badgeStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#238636")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true).
			Padding(0, 1)
	}

	badge := badgeStyle.Render(badgeText)

	var hint string
	if m.IsFocused {
		hint = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d29922")).
			Bold(true).
			Render("[Enter to apply | Esc to clear]")
	} else {
		hint = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7d8590")).
			Render("[Press '/' to filter]")
	}

	inputView := m.TextInput.View()

	containerStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#161b22")).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottomForeground(lipgloss.Color("#30363d")).
		Padding(0, 1)

	if m.IsFocused {
		containerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#0d1b2a")).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottomForeground(lipgloss.Color("#58a6ff")).
			Padding(0, 1)
	}

	barContent := lipgloss.JoinHorizontal(
		lipgloss.Center,
		inputView,
		"  ",
		hint,
		"  ",
		badge,
	)

	return containerStyle.Width(m.Width).Render(barContent)
}
