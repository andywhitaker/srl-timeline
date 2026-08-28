package tui

import (
	"fmt"
	"strings"

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
	ti.Placeholder = "path (e.g. interface, bgp)"
	ti.Prompt = " 🔍 FILTER: "
	ti.PromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff"))
	ti.TextStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff"))
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7d8590"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))

	inputW := width - 36
	if inputW < 12 {
		inputW = 12
	}
	ti.Width = inputW

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
	inputW := width - 42
	if inputW < 12 {
		inputW = 12
	}
	m.TextInput.Width = inputW
}

// Update processes Bubble Tea input messages.
func (m FilterBarModel) Update(msg tea.Msg) (FilterBarModel, tea.Cmd) {
	var cmd tea.Cmd
	m.TextInput, cmd = m.TextInput.Update(msg)
	return m, cmd
}

// View renders the search bar with badge and status.
func (m FilterBarModel) View() string {
	var rightParts []string

	if m.Value() != "" {
		badgeStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#238636")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true).
			Padding(0, 1)
		rightParts = append(rightParts, badgeStyle.Render(fmt.Sprintf("Filtered (%d/%d)", m.MatchCount, m.TotalCount)))

		clearStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#da3633")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true).
			Padding(0, 1)
		rightParts = append(rightParts, clearStyle.Render("✖ Clear [Esc]"))
	} else {
		if m.Width >= 100 {
			if m.IsFocused {
				rightParts = append(rightParts, lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922")).Render("[Enter/Esc to exit]"))
			} else {
				rightParts = append(rightParts, lipgloss.NewStyle().Foreground(lipgloss.Color("#7d8590")).Render("[Press '/' to filter]"))
			}
		}
		badgeStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#21262d")).
			Foreground(lipgloss.Color("#58a6ff")).
			Bold(true).
			Padding(0, 1)
		rightParts = append(rightParts, badgeStyle.Render(fmt.Sprintf("All (%d)", m.TotalCount)))
	}

	rightContent := lipgloss.JoinHorizontal(lipgloss.Center, rightParts...)
	rightLen := lipgloss.Width(rightContent)

	promptLen := 14
	availInput := m.Width - rightLen - promptLen - 6
	if availInput < 10 {
		availInput = 10
	}
	m.TextInput.Width = availInput

	leftContent := m.TextInput.View()
	leftLen := lipgloss.Width(leftContent)

	availSpace := (m.Width - 2) - leftLen - rightLen
	if availSpace < 0 {
		availSpace = 0
	}
	spacer := strings.Repeat(" ", availSpace)

	barContent := lipgloss.JoinHorizontal(lipgloss.Center, leftContent, spacer, rightContent)

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

	return containerStyle.Width(m.Width).Render(barContent)
}
