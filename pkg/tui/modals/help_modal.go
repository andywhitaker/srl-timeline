package modals

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"
)

type helpItem struct {
	key  string
	desc string
}

type helpCategory struct {
	title string
	items []helpItem
}

var helpCategories = []helpCategory{
	{
		title: "Navigation & Panes",
		items: []helpItem{
			{"↑/↓ / j/k", "Move selection / scroll detail"},
			{"PgUp / PgDn", "Page scroll active pane"},
			{"Home / End", "Jump to top / bottom"},
			{"Tab/Shift+Tab", "Switch panes"},
			{"← / →", "Focus Timeline / Detail pane"},
			{"Enter", "Inspect Detail from Timeline"},
			{"Esc", "Back to Timeline / clear filter"},
			{"/", "Focus search & path filter"},
			{"z", "Zoom / unzoom Detail pane"},
		},
	},
	{
		title: "Views & Compare",
		items: []helpItem{
			{"d / c / b", "Diff / Config / Blame tab"},
			{"[ / ]", "Cycle detail tabs"},
			{"1 – 4", "Diff modes / Config / Blame sub-views"},
			{"v", "Toggle Diff vs Live Running"},
			{"m", "Pin/unpin Base Commit (A↔B Compare)"},
			{"w", "Toggle line wrap (Config / Blame)"},
		},
	},
	{
		title: "Actions & Modals",
		items: []helpItem{
			{"r", "Full configuration Restore"},
			{"p", "Cherry-Pick subtree restore"},
			{"e", "Export config (JSON / CLI / Startup)"},
			{"g", "Remote Git Sync settings"},
			{"R / F5", "Refresh timeline from disk"},
			{"T / Ctrl+t", "Cycle Theme / Theme Picker"},
			{"? / q", "Show Help / Quit"},
		},
	},
}

// HelpModalModel displays the categorized, vertically scrollable keyboard shortcut reference.
type HelpModalModel struct {
	Width        int
	Height       int
	ScrollOffset int
}

// NewHelpModal creates a help modal.
func NewHelpModal(width, height int) HelpModalModel {
	return HelpModalModel{Width: width, Height: height}
}

func renderHelpCategoryLines(cat helpCategory, colW int) []string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorActive)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorCyan)
	sepStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	descStyle := lipgloss.NewStyle().Foreground(ColorText)

	keyW := 13
	if colW < 36 {
		keyW = 11
	}
	if colW < 28 {
		keyW = 9
	}

	var out []string
	out = append(out, PaintBarLine(headerStyle.Render("◆ "+cat.title), colW, ColorSurface, ColorActive))
	for _, item := range cat.items {
		kText := truncateDisplay(item.key, keyW)
		if pad := keyW - ansi.PrintableRuneWidth(kText); pad > 0 {
			kText += strings.Repeat(" ", pad)
		}
		descMax := colW - keyW - 3
		if descMax < 4 {
			descMax = 4
		}
		line := keyStyle.Render(kText) + sepStyle.Render(" : ") + descStyle.Render(truncateDisplay(item.desc, descMax))
		out = append(out, PaintBarLine(line, colW, ColorSurface, ColorText))
	}
	return out
}

func (m HelpModalModel) layoutMetrics() (innerW, vPad, hPad, maxInnerH, bodyViewportH int, bodyLines []string) {
	innerW, vPad, hPad, maxInnerH = modalBoxMetrics(m.Width, m.Height, 92)
	compact := m.Height > 0 && m.Height <= 24

	if innerW >= 70 {
		// Clean 2-column layout: Left = Navigation & Panes; Right = Views & Compare + Actions & Modals
		divW := 3 // " │ "
		leftW := (innerW - divW) / 2
		rightW := innerW - divW - leftW

		leftLines := renderHelpCategoryLines(helpCategories[0], leftW)

		rightLines := renderHelpCategoryLines(helpCategories[1], rightW)
		rightLines = append(rightLines, PaintBarLine("", rightW, ColorSurface, ColorText))
		rightLines = append(rightLines, renderHelpCategoryLines(helpCategories[2], rightW)...)

		maxRows := len(leftLines)
		if len(rightLines) > maxRows {
			maxRows = len(rightLines)
		}

		blankLeft := PaintBarLine("", leftW, ColorSurface, ColorText)
		blankRight := PaintBarLine("", rightW, ColorSurface, ColorText)
		divider := lipgloss.NewStyle().Background(ColorSurface).Foreground(ColorBorder).Render(" │ ")

		bodyLines = make([]string, 0, maxRows)
		for i := 0; i < maxRows; i++ {
			l := blankLeft
			if i < len(leftLines) {
				l = leftLines[i]
			}
			r := blankRight
			if i < len(rightLines) {
				r = rightLines[i]
			}
			bodyLines = append(bodyLines, PaintBarLine(l+divider+r, innerW, ColorSurface, ColorText))
		}
	} else {
		// Single-column stacked layout for narrow terminals (e.g. 40x12)
		for idx, cat := range helpCategories {
			if idx > 0 {
				bodyLines = append(bodyLines, PaintBarLine("", innerW, ColorSurface, ColorText))
			}
			bodyLines = append(bodyLines, renderHelpCategoryLines(cat, innerW)...)
		}
	}

	// Title (1) + optional blank (0 or 1) + footer (1 or 2)
	chromeLines := 4
	if compact {
		chromeLines = 2
	}
	if maxInnerH > 0 {
		bodyViewportH = maxInnerH - chromeLines
		if bodyViewportH < 1 {
			bodyViewportH = 1
		}
		if bodyViewportH > len(bodyLines) {
			bodyViewportH = len(bodyLines)
		}
	} else {
		bodyViewportH = len(bodyLines)
	}
	return innerW, vPad, hPad, maxInnerH, bodyViewportH, bodyLines
}

func (m HelpModalModel) maxScrollOffset() int {
	_, _, _, _, viewportH, bodyLines := m.layoutMetrics()
	maxOff := len(bodyLines) - viewportH
	if maxOff < 0 {
		return 0
	}
	return maxOff
}

func (m *HelpModalModel) clampScroll() {
	maxOff := m.maxScrollOffset()
	if m.ScrollOffset < 0 {
		m.ScrollOffset = 0
	}
	if m.ScrollOffset > maxOff {
		m.ScrollOffset = maxOff
	}
}

// View renders the categorized, scrollable help dialog.
func (m HelpModalModel) View() string {
	innerW, vPad, hPad, maxInnerH, viewportH, bodyLines := m.layoutMetrics()
	compact := m.Height > 0 && m.Height <= 24

	scrollOffset := m.ScrollOffset
	maxOff := len(bodyLines) - viewportH
	if maxOff < 0 {
		maxOff = 0
	}
	if scrollOffset > maxOff {
		scrollOffset = maxOff
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		Render("◆ KEYBOARD SHORTCUTS & NAVIGATION")

	var headerLine string
	if maxOff > 0 {
		scrollBadge := lipgloss.NewStyle().
			Background(ColorBadgeBg).
			Foreground(ColorActive).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("↑/↓ %d-%d/%d", scrollOffset+1, scrollOffset+viewportH, len(bodyLines)))
		spacer := lipgloss.NewStyle().Background(ColorSurface).Render("  ")
		headerLine = PaintBarLine(lipgloss.JoinHorizontal(lipgloss.Center, title, spacer, scrollBadge), innerW, ColorSurface, ColorText)
	} else {
		headerLine = PaintBarLine(title, innerW, ColorSurface, ColorText)
	}

	endIdx := scrollOffset + viewportH
	if endIdx > len(bodyLines) {
		endIdx = len(bodyLines)
	}
	visibleBody := strings.Join(bodyLines[scrollOffset:endIdx], "\n")

	footerText := "Press [Esc], [Enter], or [?] to close"
	if maxOff > 0 && innerW >= 54 {
		footerText = "[↑/↓/PgUp/PgDn] Scroll  |  Press [Esc], [Enter], or [?] to close"
	} else if innerW < 38 {
		footerText = "[↑/↓] Scroll | [Esc/?] Close"
	}
	footer := PaintBarLine(
		lipgloss.NewStyle().Foreground(ColorMuted).Render(footerText),
		innerW,
		ColorSurface,
		ColorMuted,
	)

	var sections []string
	sections = append(sections, headerLine)
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, visibleBody)
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, footer)

	content := strings.Join(sections, "\n")
	return renderCenteredModal(content, m.Width, m.Height, innerW, vPad, hPad, maxInnerH, ColorActive)
}

// Update processes help modal scroll and close events.
func (m HelpModalModel) Update(msg tea.Msg) (HelpModalModel, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "enter", "?", "q":
			return m, true
		case "up", "k":
			m.ScrollOffset--
			m.clampScroll()
		case "down", "j":
			m.ScrollOffset++
			m.clampScroll()
		case "pgup", "ctrl+u":
			_, _, _, _, vh, _ := m.layoutMetrics()
			if vh < 1 {
				vh = 4
			}
			m.ScrollOffset -= vh
			m.clampScroll()
		case "pgdown", "ctrl+d", " ", "space":
			_, _, _, _, vh, _ := m.layoutMetrics()
			if vh < 1 {
				vh = 4
			}
			m.ScrollOffset += vh
			m.clampScroll()
		case "home":
			m.ScrollOffset = 0
		case "end":
			m.ScrollOffset = m.maxScrollOffset()
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			m.ScrollOffset--
			m.clampScroll()
		case tea.MouseWheelDown:
			m.ScrollOffset++
			m.clampScroll()
		}
	}
	return m, false
}
