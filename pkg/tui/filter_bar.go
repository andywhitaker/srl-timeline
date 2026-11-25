package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HitChip represents the half-open horizontal column range [StartX, EndX)
// of a clickable quick-filter chip on the filter bar.
type HitChip struct {
	Label  string
	StartX int
	EndX   int
}

var defaultQuickFilterChips = []string{"interface", "bgp", "ospf", "acl", "system"}

// FilterBarModel manages the search and scoped filter input bar.
type FilterBarModel struct {
	TextInput    textinput.Model
	MatchCount   int
	TotalCount   int
	Width        int
	IsFocused    bool
	DetailBypass bool
}

// NewFilterBarModel creates a new FilterBar component.
func NewFilterBarModel(width int) FilterBarModel {
	ti := textinput.New()
	ti.Placeholder = "Filter path (e.g. interface, bgp, ospf)..."
	ti.Prompt = "/ "

	inputW := width - 24
	if inputW < 10 {
		inputW = 10
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
			Foreground(ColorActive).
			Background(bgColor)
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
	inputW := width - 24
	if inputW < 10 {
		inputW = 10
	}
	m.TextInput.Width = inputW
}

// Update processes Bubble Tea input messages.
func (m FilterBarModel) Update(msg tea.Msg) (FilterBarModel, tea.Cmd) {
	var cmd tea.Cmd
	m.TextInput, cmd = m.TextInput.Update(msg)
	return m, cmd
}

type filterBarLayout struct {
	line        string
	chips       []HitChip
	scopeStartX int
	scopeEndX   int
	clearStartX int
	clearEndX   int
}

func (m FilterBarModel) computeLayout() filterBarLayout {
	if m.Width <= 0 {
		return filterBarLayout{scopeStartX: -1, scopeEndX: -1, clearStartX: -1, clearEndX: -1}
	}

	m.RefreshStyles()

	bgColor := ColorSurface
	if m.IsFocused {
		bgColor = ColorSurfaceFocus
	}

	if m.Width < 4 {
		return filterBarLayout{
			line:        PaintBarLine("/", m.Width, bgColor, ColorActive),
			scopeStartX: -1,
			scopeEndX:   -1,
			clearStartX: -1,
			clearEndX:   -1,
		}
	}

	innerW := m.Width - 2 // 1-col left and right padding

	// Build right cluster: [[scope pill] ] [match count badge] [ [× Clear]]
	hasFilter := m.Value() != ""
	gapStr := lipgloss.NewStyle().Background(bgColor).Render(" ")

	var scopePillStr string
	scopePillW := 0
	showScope := false
	if strings.TrimSpace(m.Value()) != "" && innerW >= 54 {
		var scopeLabel string
		var scopeStyle lipgloss.Style
		if m.DetailBypass {
			if innerW >= 70 {
				scopeLabel = "Scope: TL only"
			} else {
				scopeLabel = "TL only"
			}
			scopeStyle = lipgloss.NewStyle().
				Background(ColorPrimary).
				Foreground(ColorOnAccent).
				Bold(true).
				Padding(0, 1)
		} else {
			if innerW >= 70 {
				scopeLabel = "Scope: TL+Detail"
			} else {
				scopeLabel = "TL+Detail"
			}
			scopeStyle = lipgloss.NewStyle().
				Background(ColorBadgeBg).
				Foreground(ColorWarning).
				Padding(0, 1)
		}
		scopePillStr = scopeStyle.Render(scopeLabel)
		scopePillW = lipgloss.Width(scopePillStr)
		showScope = true
	}

	var countBadgeStr string
	showCount := false
	if m.TotalCount > 0 {
		countText := fmt.Sprintf("%d/%d", m.MatchCount, m.TotalCount)
		var countStyle lipgloss.Style
		if hasFilter {
			if m.MatchCount == 0 {
				countStyle = lipgloss.NewStyle().
					Background(ColorWarningBg).
					Foreground(ColorOnWarning).
					Bold(true).
					Padding(0, 1)
			} else {
				countStyle = lipgloss.NewStyle().
					Background(ColorSuccessBg).
					Foreground(ColorOnAccent).
					Bold(true).
					Padding(0, 1)
			}
		} else {
			countStyle = lipgloss.NewStyle().
				Background(ColorBadgeBg).
				Foreground(ColorMuted).
				Padding(0, 1)
		}
		countBadgeStr = countStyle.Render(countText)
		showCount = true
	}

	var clearBtnStr string
	clearBtnW := 0
	showClear := false
	if hasFilter {
		clearBtnStr = lipgloss.NewStyle().
			Background(ColorDangerBg).
			Foreground(ColorOnAccent).
			Bold(true).
			Render("[× Clear]")
		clearBtnW = lipgloss.Width(clearBtnStr)
		showClear = true
	}

	buildRight := func(incScope, incCount, incClear bool) (string, int) {
		var parts []string
		if incScope {
			parts = append(parts, scopePillStr)
		}
		if incCount {
			if len(parts) > 0 {
				parts = append(parts, gapStr)
			}
			parts = append(parts, countBadgeStr)
		}
		if incClear {
			if len(parts) > 0 {
				parts = append(parts, gapStr)
			}
			parts = append(parts, clearBtnStr)
		}
		s := strings.Join(parts, "")
		return s, lipgloss.Width(s)
	}

	rightContent, rightW := buildRight(showScope, showCount, showClear)

	// Guard against narrow widths where right cluster alone crowds out the prompt.
	if showScope && rightW > innerW-10 {
		showScope = false
		rightContent, rightW = buildRight(false, showCount, showClear)
	}
	if rightW > innerW-4 {
		if showClear && clearBtnW <= innerW-4 {
			showScope = false
			showCount = false
			rightContent = clearBtnStr
			rightW = clearBtnW
		} else {
			showScope = false
			showCount = false
			showClear = false
			rightContent = ""
			rightW = 0
		}
	}

	// Build optional quick-filter chips on wide terminals when filter is empty and unfocused.
	type renderedChip struct {
		label    string
		rendered string
		width    int
	}
	var chips []renderedChip
	chipsBlock := ""
	chipsW := 0
	chipsRightGapW := 0

	if m.Width >= 115 && !hasFilter && !m.IsFocused {
		chipStyle := lipgloss.NewStyle().
			Background(ColorBadgeBg).
			Foreground(ColorMuted).
			Padding(0, 1)

		var chipPieces []string
		totalChipsW := 0
		for i, lbl := range defaultQuickFilterChips {
			rc := chipStyle.Render(lbl)
			w := lipgloss.Width(rc)
			chips = append(chips, renderedChip{label: lbl, rendered: rc, width: w})
			if i > 0 {
				chipPieces = append(chipPieces, gapStr)
				totalChipsW++
			}
			chipPieces = append(chipPieces, rc)
			totalChipsW += w
		}

		gapToRight := 0
		if rightW > 0 {
			gapToRight = 2
		}
		// Ensure plenty of room for the "/ Filter path..." input on the left.
		const minLeftWithChips = 46
		if minLeftWithChips+2+totalChipsW+gapToRight+rightW <= innerW {
			chipsBlock = strings.Join(chipPieces, "")
			chipsW = totalChipsW
			chipsRightGapW = gapToRight
		} else {
			chips = nil
		}
	}

	// Budget left textinput width.
	rightSideTotalW := chipsW + chipsRightGapW + rightW
	leftToRightGap := 0
	if rightSideTotalW > 0 {
		leftToRightGap = 1
	}
	maxLeftW := innerW - rightSideTotalW - leftToRightGap
	if maxLeftW < 4 {
		maxLeftW = 4
	}

	promptW := lipgloss.Width(m.TextInput.Prompt)
	availInput := maxLeftW - promptW - 1
	if availInput < 4 {
		availInput = 4
	}
	m.TextInput.Width = availInput

	leftContent := m.TextInput.View()
	leftW := lipgloss.Width(leftContent)
	if leftW > maxLeftW {
		leftContent = PaintBarLine(leftContent, maxLeftW, bgColor, ColorText)
		leftW = maxLeftW
	}

	spacerW := innerW - leftW - rightSideTotalW
	if spacerW < 0 {
		spacerW = 0
	}
	spacer := lipgloss.NewStyle().Background(bgColor).Render(strings.Repeat(" ", spacerW))
	pad := lipgloss.NewStyle().Background(bgColor).Render(" ")

	var middle strings.Builder
	middle.WriteString(pad)
	middle.WriteString(leftContent)
	middle.WriteString(spacer)

	var hitChips []HitChip
	if len(chips) > 0 {
		cursorX := 1 + leftW + spacerW
		for i, c := range chips {
			if i > 0 {
				cursorX++
			}
			hitChips = append(hitChips, HitChip{
				Label:  c.label,
				StartX: cursorX,
				EndX:   cursorX + c.width,
			})
			cursorX += c.width
		}
		middle.WriteString(chipsBlock)
		if chipsRightGapW > 0 {
			middle.WriteString(lipgloss.NewStyle().Background(bgColor).Render(strings.Repeat(" ", chipsRightGapW)))
		}
	}
	middle.WriteString(rightContent)
	middle.WriteString(pad)

	fullLine := PaintBarLine(middle.String(), m.Width, bgColor, ColorText)

	rightStartX := 1 + leftW + spacerW + chipsW + chipsRightGapW
	scopeStartX, scopeEndX := -1, -1
	if showScope {
		scopeStartX = rightStartX
		scopeEndX = scopeStartX + scopePillW
		if scopeStartX < 0 || scopeEndX > m.Width-1 {
			scopeStartX = -1
			scopeEndX = -1
		}
	}

	clearStartX, clearEndX := -1, -1
	if showClear {
		// Clear button is the rightmost element inside innerW.
		clearEndX = rightStartX + rightW
		if clearEndX > m.Width-1 {
			clearEndX = m.Width - 1
		}
		clearStartX = clearEndX - clearBtnW
		if clearStartX < 0 {
			clearStartX = -1
			clearEndX = -1
		}
	}

	return filterBarLayout{
		line:        fullLine,
		chips:       hitChips,
		scopeStartX: scopeStartX,
		scopeEndX:   scopeEndX,
		clearStartX: clearStartX,
		clearEndX:   clearEndX,
	}
}

// QuickChipHitRanges returns the exact rendered horizontal column ranges [StartX, EndX)
// of visible quick-filter chips, or nil when chips are not displayed.
func (m FilterBarModel) QuickChipHitRanges() []HitChip {
	return m.computeLayout().chips
}

// ScopeHitRange returns the exact rendered horizontal column range [startX, endX)
// of the clickable scope pill ("Scope: TL+Detail" / "Scope: TL only"),
// or (-1, -1) when no filter is active or the terminal is too narrow.
func (m FilterBarModel) ScopeHitRange() (startX, endX int) {
	l := m.computeLayout()
	return l.scopeStartX, l.scopeEndX
}

// ClearHitRange returns the exact rendered horizontal column range [startX, endX)
// of the explicit [× Clear] button, or (-1, -1) when the filter is empty or hidden.
func (m FilterBarModel) ClearHitRange() (startX, endX int) {
	l := m.computeLayout()
	return l.clearStartX, l.clearEndX
}

// View renders the 1-row borderless search and filter bar.
func (m FilterBarModel) View() string {
	return m.computeLayout().line
}
