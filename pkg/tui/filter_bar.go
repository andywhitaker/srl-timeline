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

	inputW := width - 36
	if inputW < 12 {
		inputW = 12
	}
	ti.Width = inputW

	m := FilterBarModel{
		TextInput:  ti,
		MatchCount: 0,
		TotalCount: 0,
		Width:      width,
		IsFocused:  false,
	}
	m.RefreshStyles()
	return m
}

// RefreshStyles updates the text input styles to match the active theme and focus state.
func (m *FilterBarModel) RefreshStyles() {
	bgColor := ColorSurface
	if m.IsFocused {
		bgColor = ColorSurfaceFocus
	}
	StyleTextInput(&m.TextInput, bgColor, ColorText, ColorTextDim, ColorActive)
	m.TextInput.TextStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(bgColor)
	if m.IsFocused {
		m.TextInput.PromptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorOnAccent).
			Background(ColorPrimary).
			Padding(0, 1)
	} else {
		m.TextInput.PromptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorActive).
			Background(bgColor)
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
	m.RefreshStyles()
	return m.TextInput.Focus()
}

// Blur deactivates the input field.
func (m *FilterBarModel) Blur() {
	m.IsFocused = false
	m.RefreshStyles()
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
	m.RefreshStyles()

	bgColor := ColorSurface
	containerStyle := StyleFilterContainer
	if m.IsFocused {
		bgColor = ColorSurfaceFocus
		containerStyle = StyleFilterContainerFocused
	}

	var rightParts []string

	if m.Value() != "" {
		badgeStyle := lipgloss.NewStyle().
			Background(ColorSuccessBg).
			Foreground(ColorOnAccent).
			Bold(true).
			Padding(0, 1).
			MarginRight(1).
			MarginBackground(bgColor)
		rightParts = append(rightParts, badgeStyle.Render(fmt.Sprintf("Filtered (%d/%d)", m.MatchCount, m.TotalCount)))

		clearStyle := lipgloss.NewStyle().
			Background(ColorDangerBg).
			Foreground(ColorOnAccent).
			Bold(true).
			Padding(0, 1)
		rightParts = append(rightParts, clearStyle.Render("✖ Clear [Esc]"))
	} else {
		if m.Width >= 100 {
			if m.IsFocused {
				rightParts = append(rightParts, lipgloss.NewStyle().
					Foreground(ColorWarning).
					Background(bgColor).
					MarginRight(1).
					MarginBackground(bgColor).
					Render("[Enter/Esc to exit]"))
			} else {
				rightParts = append(rightParts, lipgloss.NewStyle().
					Foreground(ColorTextDim).
					Background(bgColor).
					MarginRight(1).
					MarginBackground(bgColor).
					Render("[Press '/' to filter]"))
			}
		}
		badgeStyle := lipgloss.NewStyle().
			Background(ColorBadgeBg).
			Foreground(ColorActive).
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

	innerW := m.Width - 2
	if innerW < 1 {
		innerW = 1
	}

	availSpace := innerW - leftLen - rightLen
	if availSpace < 0 {
		availSpace = 0
	}
	spacer := lipgloss.NewStyle().Background(bgColor).Foreground(ColorText).Render(strings.Repeat(" ", availSpace))

	barContent := lipgloss.JoinHorizontal(lipgloss.Center, leftContent, spacer, rightContent)
	paintedBar := PaintBarLine(barContent, innerW, bgColor, ColorText)

	return containerStyle.Render(paintedBar)
}
