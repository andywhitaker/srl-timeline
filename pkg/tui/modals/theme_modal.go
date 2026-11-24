package modals

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"
	"github.com/muesli/reflow/truncate"
)

// truncateDisplay cleans newlines/tabs and truncates s to fit within maxW visual columns.
func truncateDisplay(s string, maxW int) string {
	clean := strings.ReplaceAll(s, "\r", "")
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.ReplaceAll(clean, "\t", " ")
	clean = strings.TrimSpace(clean)
	if maxW <= 0 || ansi.PrintableRuneWidth(clean) <= maxW {
		return clean
	}
	if maxW == 1 {
		return "…"
	}
	return truncate.StringWithTail(clean, uint(maxW), "…")
}

// modalBoxMetrics computes consistent inner width, padding, and max inner height for a modal.
func modalBoxMetrics(width, height, preferredInnerW int) (innerW, vPad, hPad, maxInnerH int) {
	vPad = 1
	if height > 0 && height <= 24 {
		vPad = 0
	}
	hPad = 2
	if width > 0 && width <= 46 {
		hPad = 1
	}

	innerW = preferredInnerW
	if width > 0 {
		maxW := width - 2 - hPad*2
		if maxW < 8 {
			maxW = width - 2
			hPad = 0
		}
		if maxW < 4 {
			maxW = 4
		}
		if innerW > maxW {
			innerW = maxW
		}
	}
	if innerW < 8 {
		innerW = 8
	}

	if height > 0 {
		maxInnerH = height - 2 - vPad*2
		if maxInnerH < 1 {
			vPad = 0
			maxInnerH = height - 2
		}
		if maxInnerH < 1 {
			maxInnerH = 1
		}
	}
	return innerW, vPad, hPad, maxInnerH
}

// renderCenteredModal wraps content in a full-bleed rounded modal box centered over ColorBackground.
func renderCenteredModal(content string, width, height, innerW, vPad, hPad, maxInnerH int, borderColor lipgloss.Color) string {
	clampH := 0
	if maxInnerH > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > maxInnerH {
			clampH = maxInnerH
		}
	}
	paintedContent := PaintSurface(content, innerW, clampH, ColorSurface, ColorText)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		BorderBackground(ColorSurface).
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(vPad, hPad).
		Render(paintedContent)

	if width <= 0 || height <= 0 {
		return box
	}

	placed := lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(ColorBackground),
		lipgloss.WithWhitespaceForeground(ColorTextDim),
	)
	return PaintSurface(placed, width, height, ColorBackground, ColorText)
}

// ThemeModal provides an interactive theme picker with live preview and revert-on-cancel.
type ThemeModal struct {
	Themes        []Theme
	InitialTheme  string
	SelectedIndex int
	ScrollOffset  int
	Width         int
	Height        int
}

// NewThemeModal creates an interactive theme picker initialized to the currently active theme.
func NewThemeModal(width, height int) ThemeModal {
	themes := AvailableThemes()
	cur := CurrentTheme()
	selIdx := 0
	for i, t := range themes {
		if t.Name == cur.Name {
			selIdx = i
			break
		}
	}
	m := ThemeModal{
		Themes:        themes,
		InitialTheme:  cur.Name,
		SelectedIndex: selIdx,
		Width:         width,
		Height:        height,
	}
	m.syncScroll()
	return m
}

// SelectedTheme returns the currently highlighted Theme.
func (m ThemeModal) SelectedTheme() Theme {
	if len(m.Themes) == 0 {
		return CurrentTheme()
	}
	idx := m.SelectedIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.Themes) {
		idx = len(m.Themes) - 1
	}
	return m.Themes[idx]
}

func (m ThemeModal) visibleRows() int {
	_, _, _, maxInnerH := modalBoxMetrics(m.Width, m.Height, 62)
	if maxInnerH <= 0 {
		return len(m.Themes)
	}
	// Overhead: title (1), blank/subtitle (2 on tall, 1 on compact), footer (2 on tall, 1 on compact), scroll hints (up to 2)
	overhead := 6
	if m.Height > 0 && m.Height <= 24 {
		overhead = 4
	}
	if m.Height > 0 && m.Height <= 14 {
		overhead = 3
	}
	rows := maxInnerH - overhead
	if rows < 3 {
		rows = 3
	}
	if rows > len(m.Themes) && len(m.Themes) > 0 {
		rows = len(m.Themes)
	}
	return rows
}

func (m *ThemeModal) syncScroll() {
	total := len(m.Themes)
	if total == 0 {
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		return
	}
	if m.SelectedIndex < 0 {
		m.SelectedIndex = 0
	}
	if m.SelectedIndex >= total {
		m.SelectedIndex = total - 1
	}
	vis := m.visibleRows()
	if m.SelectedIndex < m.ScrollOffset {
		m.ScrollOffset = m.SelectedIndex
	}
	if m.SelectedIndex >= m.ScrollOffset+vis {
		m.ScrollOffset = m.SelectedIndex - vis + 1
	}
	maxOff := total - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if m.ScrollOffset > maxOff {
		m.ScrollOffset = maxOff
	}
	if m.ScrollOffset < 0 {
		m.ScrollOffset = 0
	}
}

func (m *ThemeModal) selectIndex(idx int) bool {
	if len(m.Themes) == 0 {
		return false
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.Themes) {
		idx = len(m.Themes) - 1
	}
	if idx == m.SelectedIndex {
		return false
	}
	m.SelectedIndex = idx
	m.syncScroll()
	SetTheme(m.Themes[m.SelectedIndex].Name)
	return true
}

func (m ThemeModal) listGeometry() (innerW, vPad, hPad, maxInnerH, startIdx, endIdx, listStartY, boxLeftX, boxRightX int) {
	innerW, vPad, hPad, maxInnerH = modalBoxMetrics(m.Width, m.Height, 62)
	vis := m.visibleRows()
	total := len(m.Themes)

	scrollOffset := m.ScrollOffset
	if m.SelectedIndex < scrollOffset {
		scrollOffset = m.SelectedIndex
	}
	if m.SelectedIndex >= scrollOffset+vis {
		scrollOffset = m.SelectedIndex - vis + 1
	}
	if scrollOffset > total-vis {
		scrollOffset = total - vis
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}

	startIdx = scrollOffset
	endIdx = scrollOffset + vis
	if endIdx > total {
		endIdx = total
	}

	// Count rendered lines to compute vertical centering offset
	compact := m.Height > 0 && m.Height <= 24
	ultraCompact := m.Height > 0 && m.Height <= 14
	headerLines := 3 // title + blank + subtitle
	if ultraCompact {
		headerLines = 1 // title only
	} else if compact {
		headerLines = 2 // title + subtitle
	}
	aboveLines := 0
	if startIdx > 0 {
		aboveLines = 1
	}
	belowLines := 0
	if endIdx < total {
		belowLines = 1
	}
	footerLines := 2 // blank + footer
	if compact {
		footerLines = 1
	}

	contentLines := headerLines + aboveLines + (endIdx - startIdx) + belowLines + footerLines
	if maxInnerH > 0 && contentLines > maxInnerH {
		contentLines = maxInnerH
	}
	boxH := contentLines + 2 + vPad*2
	boxW := innerW + 2 + hPad*2
	topY := 0
	if m.Height > boxH {
		topY = (m.Height - boxH) / 2
	}
	boxLeftX = 0
	if m.Width > boxW {
		boxLeftX = (m.Width - boxW) / 2
	}
	boxRightX = boxLeftX + boxW - 1
	listStartY = topY + 1 + vPad + headerLines + aboveLines
	return
}

// Update handles keyboard and mouse events for live-preview theme selection.
// Returns (updatedModel, changed, confirmed, close).
func (m ThemeModal) Update(msg tea.Msg) (ThemeModal, bool, bool, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			changed := m.selectIndex(m.SelectedIndex - 1)
			return m, changed, false, false

		case "down", "j":
			changed := m.selectIndex(m.SelectedIndex + 1)
			return m, changed, false, false

		case "pgup", "ctrl+u":
			step := m.visibleRows()
			if step < 1 {
				step = 4
			}
			changed := m.selectIndex(m.SelectedIndex - step)
			return m, changed, false, false

		case "pgdown", "ctrl+d":
			step := m.visibleRows()
			if step < 1 {
				step = 4
			}
			changed := m.selectIndex(m.SelectedIndex + step)
			return m, changed, false, false

		case "home":
			changed := m.selectIndex(0)
			return m, changed, false, false

		case "end":
			changed := m.selectIndex(len(m.Themes) - 1)
			return m, changed, false, false

		case "enter":
			if len(m.Themes) > 0 {
				sel := m.SelectedTheme()
				prev := CurrentTheme().Name
				SetTheme(sel.Name)
				return m, prev != sel.Name, true, true
			}
			return m, false, true, true

		case "esc", "q":
			prev := CurrentTheme().Name
			SetTheme(m.InitialTheme)
			return m, prev != m.InitialTheme, false, true
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			changed := m.selectIndex(m.SelectedIndex - 1)
			return m, changed, false, false

		case tea.MouseWheelDown:
			changed := m.selectIndex(m.SelectedIndex + 1)
			return m, changed, false, false

		case tea.MouseLeft:
			_, _, _, _, startIdx, endIdx, listStartY, leftX, rightX := m.listGeometry()
			if msg.X >= leftX && msg.X <= rightX {
				row := msg.Y - listStartY
				if row >= 0 && startIdx+row < endIdx {
					changed := m.selectIndex(startIdx + row)
					return m, changed, false, false
				}
			}
		}
	}
	return m, false, false, false
}

// View renders the interactive theme picker modal.
func (m ThemeModal) View() string {
	innerW, vPad, hPad, maxInnerH, startIdx, endIdx, _, _, _ := m.listGeometry()
	total := len(m.Themes)
	compact := m.Height > 0 && m.Height <= 24
	ultraCompact := m.Height > 0 && m.Height <= 14

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		Render("◆ SELECT COLOR THEME")

	cur := m.SelectedTheme()
	counter := lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorActive).
		Bold(true).
		Padding(0, 1).
		Render(fmt.Sprintf("%d/%d", m.SelectedIndex+1, total))

	spacer := lipgloss.NewStyle().Background(ColorSurface).Render("  ")
	headerLine := PaintBarLine(lipgloss.JoinHorizontal(lipgloss.Center, title, spacer, counter), innerW, ColorSurface, ColorText)

	subtitle := PaintBarLine(
		lipgloss.NewStyle().Foreground(ColorMuted).Render(
			fmt.Sprintf("Live preview: %s (%s) · initial: %s", cur.DisplayName, cur.Name, m.InitialTheme),
		),
		innerW,
		ColorSurface,
		ColorMuted,
	)

	var rows []string
	if startIdx > 0 {
		rows = append(rows, PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("  ↑ %d more above", startIdx)),
			innerW,
			ColorSurface,
			ColorMuted,
		))
	}

	activeName := CurrentTheme().Name
	for i := startIdx; i < endIdx; i++ {
		t := m.Themes[i]
		isSelected := i == m.SelectedIndex
		isActive := t.Name == activeName

		rowBg := ColorSurface
		if isSelected {
			rowBg = ColorHighlight
		}

		cursor := lipgloss.NewStyle().Background(rowBg).Foreground(ColorMuted).Render("  ")
		if isSelected {
			cursor = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorActive).Render("▸ ")
		}

		marker := lipgloss.NewStyle().Background(rowBg).Foreground(ColorTextDim).Render("○ ")
		if isActive {
			marker = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorSuccess).Render("● ")
		}

		nameW := 18
		if innerW < 46 {
			nameW = 14
		}
		displayName := truncateDisplay(t.DisplayName, nameW)
		if pad := nameW - ansi.PrintableRuneWidth(displayName); pad > 0 {
			displayName += strings.Repeat(" ", pad)
		}
		nameStyle := lipgloss.NewStyle().Background(rowBg).Foreground(ColorText)
		if isSelected {
			nameStyle = nameStyle.Bold(true).Foreground(ColorActive)
		}
		namePart := nameStyle.Render(displayName)

		modeLabel := "dark "
		if !t.IsDark {
			modeLabel = "light"
		}
		tagStyle := lipgloss.NewStyle().Background(rowBg).Foreground(ColorMuted)
		modeTag := tagStyle.Render(" [" + modeLabel + "] ")

		swatches := renderThemeSwatches(t, rowBg, innerW >= 50)
		rowContent := cursor + marker + namePart + modeTag + swatches
		rows = append(rows, PaintBarLine(rowContent, innerW, rowBg, ColorText))
	}

	if endIdx < total {
		rows = append(rows, PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("  ↓ %d more below", total-endIdx)),
			innerW,
			ColorSurface,
			ColorMuted,
		))
	}

	footerText := "[↑/↓/PgUp/PgDn] Preview  |  [Enter] Confirm  |  [Esc/q] Revert"
	if innerW < 54 {
		footerText = "[↑/↓] Preview | [Enter] OK | [Esc] Revert"
	}
	footer := PaintBarLine(
		lipgloss.NewStyle().Foreground(ColorMuted).Render(footerText),
		innerW,
		ColorSurface,
		ColorMuted,
	)

	var sections []string
	sections = append(sections, headerLine)
	if !ultraCompact {
		if !compact {
			sections = append(sections, "")
		}
		sections = append(sections, subtitle)
	}
	sections = append(sections, strings.Join(rows, "\n"))
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, footer)

	content := strings.Join(sections, "\n")
	return renderCenteredModal(content, m.Width, m.Height, innerW, vPad, hPad, maxInnerH, ColorActive)
}

func renderThemeSwatches(t Theme, rowBg lipgloss.Color, spaced bool) string {
	colors := []lipgloss.Color{t.Background, t.Active, t.Success, t.Warning, t.Danger}
	sep := ""
	if spaced {
		sep = lipgloss.NewStyle().Background(rowBg).Render(" ")
	}
	parts := make([]string, 0, len(colors))
	for _, c := range colors {
		parts = append(parts, lipgloss.NewStyle().Foreground(c).Background(rowBg).Render("██"))
	}
	return strings.Join(parts, sep)
}
