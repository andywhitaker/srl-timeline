package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/models"
)

// DiffViewModel handles the syntax-highlighted semantic diff viewport.
type DiffViewModel struct {
	DiffResult models.SemanticDiffResult
	DiffMode   string // "unified", "path", "cli"
	VsLive     bool
	Viewport   viewport.Model
	Width      int
	Height     int
	IsFocused  bool
}

// NewDiffViewModel creates a new diff viewer.
func NewDiffViewModel(width, height int) DiffViewModel {
	vpHeight := height - 2
	if vpHeight < 1 {
		vpHeight = 1
	}
	vp := viewport.New(width, vpHeight)
	return DiffViewModel{
		DiffMode:  "unified",
		Viewport:  vp,
		Width:     width,
		Height:    height,
		IsFocused: false,
	}
}

// SetDiff updates the diff result and rebuilds viewport lines.
func (m *DiffViewModel) SetDiff(res models.SemanticDiffResult) {
	m.DiffResult = res
	m.UpdateContent()
}

// SetSize handles terminal resizing.
func (m *DiffViewModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.Viewport.Width = width
	vpHeight := height - 2
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.Viewport.Height = vpHeight
	m.UpdateContent()
}

// UpdateContent re-renders the diff content with Lip Gloss syntax colors.
func (m *DiffViewModel) UpdateContent() {
	wrapW := m.Viewport.Width
	if wrapW <= 0 {
		wrapW = m.Width
	}
	if wrapW <= 0 {
		wrapW = 20
	}
	if wrapW < 4 {
		wrapW = 4
	}

	if !m.DiffResult.HasChanges {
		msg := lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true).
			Render("  ✓ No configuration changes for this revision / filter")
		m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorText) + "\n" + PaintLine(msg, wrapW, ColorPanel, ColorSuccess))
		m.Viewport.SetYOffset(0)
		return
	}

	var sb strings.Builder

	switch m.DiffMode {
	case "unified":
		if len(m.DiffResult.UnifiedDiffLines) <= 2 {
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render("  No unified JSON differences found for this filter. Check [2] Path or [3] CLI diff.")
			sb.WriteString(PaintLine(warn, wrapW, ColorPanel, ColorWarning))
			sb.WriteString("\n")
		} else {
			styleHeader := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted)
			styleCtx := lipgloss.NewStyle().Foreground(ColorMuted)

			for _, l := range m.DiffResult.UnifiedDiffLines {
				if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
					sb.WriteString(PaintLine(styleHeader.Render(l), wrapW, ColorPanel, ColorMuted))
				} else if strings.HasPrefix(l, "@@") {
					sb.WriteString(PaintLine(StyleDiffHunkLine.Render(l), wrapW, ColorDiffHunkBg, ColorActive))
				} else if strings.HasPrefix(l, "+") {
					sb.WriteString(PaintLine(StyleDiffAddLine.Render(l), wrapW, ColorDiffAddBg, ColorSuccess))
				} else if strings.HasPrefix(l, "-") {
					sb.WriteString(PaintLine(StyleDiffDelLine.Render(l), wrapW, ColorDiffDelBg, ColorDanger))
				} else {
					sb.WriteString(PaintLine(styleCtx.Render(l), wrapW, ColorPanel, ColorMuted))
				}
				sb.WriteString("\n")
			}
		}

	case "path":
		if len(m.DiffResult.Changes) == 0 {
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render("  No structured path changes found for this filter.")
			sb.WriteString(PaintLine(warn, wrapW, ColorPanel, ColorWarning))
			sb.WriteString("\n")
		} else {
			for _, c := range m.DiffResult.Changes {
				var badge string
				var pathFg lipgloss.Color
				switch c.DiffType {
				case models.DiffAdded:
					badge = lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Render(" ADD ")
					pathFg = ColorSuccess
				case models.DiffModified:
					badge = lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Render(" MOD ")
					pathFg = ColorText
				case models.DiffDeleted:
					badge = lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Render(" DEL ")
					pathFg = ColorDanger
				}
				pathWrapW := wrapW - 6
				if pathWrapW < 4 {
					pathWrapW = 4
				}
				pathStyle := lipgloss.NewStyle().Bold(true).Foreground(pathFg)
				wrappedPath := PaintLine(pathStyle.Render(c.Path), pathWrapW, ColorPanel, pathFg)
				lines := strings.Split(wrappedPath, "\n")
				for idx, pLine := range lines {
					if idx == 0 {
						sb.WriteString(PaintBarLine(fmt.Sprintf("%s %s", badge, pLine), wrapW, ColorPanel, pathFg))
					} else {
						sb.WriteString(PaintBarLine(fmt.Sprintf("      %s", pLine), wrapW, ColorPanel, pathFg))
					}
					sb.WriteString("\n")
				}
			}
		}

	case "cli":
		if len(m.DiffResult.CLIDiffLines) == 0 {
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render("  No CLI diff statements found for this filter.")
			sb.WriteString(PaintLine(warn, wrapW, ColorPanel, ColorWarning))
			sb.WriteString("\n")
		} else {
			styleCLICtx := lipgloss.NewStyle().Foreground(ColorText)

			for _, l := range m.DiffResult.CLIDiffLines {
				if strings.HasPrefix(l, "+") {
					sb.WriteString(PaintLine(StyleDiffAddLine.Render(l), wrapW, ColorDiffAddBg, ColorSuccess))
				} else if strings.HasPrefix(l, "-") {
					sb.WriteString(PaintLine(StyleDiffDelLine.Render(l), wrapW, ColorDiffDelBg, ColorDanger))
				} else {
					sb.WriteString(PaintLine(styleCLICtx.Render(l), wrapW, ColorPanel, ColorText))
				}
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n")
	sb.WriteString(PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("─── End of Diff ───"), wrapW, ColorPanel, ColorMuted))

	content := sb.String()
	m.Viewport.SetContent(content)
	m.Viewport.SetYOffset(m.Viewport.YOffset)
}

// View renders the diff viewer box.
func (m DiffViewModel) View() string {
	btnActiveStyle := lipgloss.NewStyle().
		Background(ColorPrimary).
		Foreground(ColorOnAccent).
		Bold(true).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorSurface)

	btnInactiveStyle := lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorMuted).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorSurface)

	btnUnified := btnInactiveStyle.Render("[1] Diff")
	if m.DiffMode == "unified" {
		btnUnified = btnActiveStyle.Render("▶ Diff")
	}

	btnPath := btnInactiveStyle.Render("[2] Path")
	if m.DiffMode == "path" {
		btnPath = btnActiveStyle.Render("▶ Path")
	}

	btnCLI := btnInactiveStyle.Render("[3] CLI")
	if m.DiffMode == "cli" {
		btnCLI = btnActiveStyle.Render("▶ CLI")
	}

	btnLive := btnInactiveStyle.Render("[4] Live")
	if m.VsLive {
		btnLive = lipgloss.NewStyle().
			Background(ColorWarning).
			Foreground(ColorOnAccent).
			Bold(true).
			Padding(0, 1).
			MarginRight(1).
			MarginBackground(ColorSurface).
			Render("● Live")
	}

	// Calculate scroll progress badge
	botLine := m.Viewport.YOffset + m.Viewport.Height
	totalLines := m.Viewport.TotalLineCount()
	if botLine > totalLines {
		botLine = totalLines
	}
	pct := int(m.Viewport.ScrollPercent() * 100)
	var scrollBadge string
	if m.Viewport.AtTop() {
		scrollBadge = lipgloss.NewStyle().Background(ColorBadgeBg).Foreground(ColorActive).Bold(true).Padding(0, 1).Render(fmt.Sprintf("⬆ TOP (%d/%d)", botLine, totalLines))
	} else if m.Viewport.AtBottom() {
		scrollBadge = lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1).Render(fmt.Sprintf("✓ BOT (%d/%d)", botLine, totalLines))
	} else {
		scrollBadge = lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1).Render(fmt.Sprintf("↕ %d%% (%d/%d)", pct, botLine, totalLines))
	}

	controls := lipgloss.JoinHorizontal(
		lipgloss.Center,
		btnUnified, btnPath, btnCLI, btnLive,
		" ",
		scrollBadge,
	)

	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	vpH := m.Viewport.Height
	if vpH < 1 {
		vpH = 1
	}

	controlsBar := PaintBarLine(" "+controls, innerW, ColorSurface, ColorText)

	borderStyle := StyleInactiveBorder
	headerText := " 🔍 DIFF VIEW "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 🔍 DIFF VIEW [FOCUSED] "
	}

	header := PaintBarLine(StylePanelHeader.Render(headerText), innerW, ColorSurface, ColorText)
	vpView := PaintSurface(m.Viewport.View(), innerW, vpH, ColorPanel, ColorText)

	return borderStyle.
		Width(m.Width).
		Height(m.Height).
		Render(fmt.Sprintf("%s\n%s\n%s", header, controlsBar, vpView))
}

// Update processes viewport navigation and sub-option selection.
func (m DiffViewModel) Update(msg tea.Msg) (DiffViewModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "1", "u":
			m.DiffMode = "unified"
			m.UpdateContent()
		case "2":
			m.DiffMode = "path"
			m.UpdateContent()
		case "3":
			m.DiffMode = "cli"
			m.UpdateContent()
		}
	}
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}
