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
	vpHeight := height - 3
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
	vpHeight := height - 3
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.Viewport.Height = vpHeight
	m.UpdateContent()
}

// UpdateContent re-renders the diff content with Lip Gloss syntax colors.
func (m *DiffViewModel) UpdateContent() {
	if !m.DiffResult.HasChanges {
		m.Viewport.SetContent(lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true).
			Padding(1, 2).
			Render("✓ No configuration changes for this revision / filter"))
		return
	}

	var sb strings.Builder
	wrapW := m.Viewport.Width
	if wrapW <= 0 {
		wrapW = m.Width
	}
	if wrapW < 20 {
		wrapW = 20
	}

	switch m.DiffMode {
	case "unified":
		if len(m.DiffResult.UnifiedDiffLines) <= 2 {
			sb.WriteString(lipgloss.NewStyle().Foreground(ColorWarning).Padding(1, 2).Render("No unified JSON differences found for this filter. Check [2] Path or [3] CLI diff."))
		} else {
			styleHeader := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Width(wrapW)
			styleHunk := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Width(wrapW)
			styleAdd := lipgloss.NewStyle().Foreground(ColorSuccess).Width(wrapW)
			styleDel := lipgloss.NewStyle().Foreground(ColorDanger).Width(wrapW)
			styleCtx := lipgloss.NewStyle().Foreground(ColorMuted).Width(wrapW)

			for _, l := range m.DiffResult.UnifiedDiffLines {
				if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
					sb.WriteString(styleHeader.Render(l))
				} else if strings.HasPrefix(l, "@@") {
					sb.WriteString(styleHunk.Render(l))
				} else if strings.HasPrefix(l, "+") {
					sb.WriteString(styleAdd.Render(l))
				} else if strings.HasPrefix(l, "-") {
					sb.WriteString(styleDel.Render(l))
				} else {
					sb.WriteString(styleCtx.Render(l))
				}
				sb.WriteString("\n")
			}
		}

	case "path":
		if len(m.DiffResult.Changes) == 0 {
			sb.WriteString(lipgloss.NewStyle().Foreground(ColorWarning).Padding(1, 2).Render("No structured path changes found for this filter."))
		} else {
			for _, c := range m.DiffResult.Changes {
				var badge string
				var pathStyle lipgloss.Style
				switch c.DiffType {
				case models.DiffAdded:
					badge = lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#ffffff")).Bold(true).Render(" ADD ")
					pathStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
				case models.DiffModified:
					badge = lipgloss.NewStyle().Background(ColorWarning).Foreground(lipgloss.Color("#ffffff")).Bold(true).Render(" MOD ")
					pathStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorText)
				case models.DiffDeleted:
					badge = lipgloss.NewStyle().Background(ColorDanger).Foreground(lipgloss.Color("#ffffff")).Bold(true).Render(" DEL ")
					pathStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorDanger)
				}
				pathWrapW := wrapW - 7
				if pathWrapW > 10 {
					pathStyle = pathStyle.Width(pathWrapW)
				}
				pathStr := pathStyle.Render(c.Path)
				lines := strings.Split(pathStr, "\n")
				for idx, pLine := range lines {
					if idx == 0 {
						sb.WriteString(fmt.Sprintf("%s %s\n", badge, pLine))
					} else {
						sb.WriteString(fmt.Sprintf("      %s\n", pLine))
					}
				}
			}
		}

	case "cli":
		if len(m.DiffResult.CLIDiffLines) == 0 {
			sb.WriteString(lipgloss.NewStyle().Foreground(ColorWarning).Padding(1, 2).Render("No CLI diff statements found for this filter."))
		} else {
			styleCLIAdd := lipgloss.NewStyle().Foreground(ColorSuccess).Width(wrapW)
			styleCLIDel := lipgloss.NewStyle().Foreground(ColorDanger).Width(wrapW)
			styleCLICtx := lipgloss.NewStyle().Foreground(ColorText).Width(wrapW)

			for _, l := range m.DiffResult.CLIDiffLines {
				if strings.HasPrefix(l, "+") {
					sb.WriteString(styleCLIAdd.Render(l))
				} else if strings.HasPrefix(l, "-") {
					sb.WriteString(styleCLIDel.Render(l))
				} else {
					sb.WriteString(styleCLICtx.Render(l))
				}
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("─── End of Diff ───") + "\n")

	content := sb.String()
	m.Viewport.SetContent(content)
}

// View renders the diff viewer box.
func (m DiffViewModel) View() string {
	btnActiveStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#1f6feb")).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true).
		Padding(0, 1).
		MarginRight(1)

	btnInactiveStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#21262d")).
		Foreground(lipgloss.Color("#8b949e")).
		Padding(0, 1).
		MarginRight(1)

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
			Background(lipgloss.Color("#d29922")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true).
			Padding(0, 1).
			MarginRight(1).
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
		scrollBadge = lipgloss.NewStyle().Background(lipgloss.Color("#21262d")).Foreground(lipgloss.Color("#58a6ff")).Bold(true).Padding(0, 1).Render(fmt.Sprintf("⬆ TOP (%d/%d)", botLine, totalLines))
	} else if m.Viewport.AtBottom() {
		scrollBadge = lipgloss.NewStyle().Background(lipgloss.Color("#238636")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1).Render(fmt.Sprintf("✓ BOT (%d/%d)", botLine, totalLines))
	} else {
		scrollBadge = lipgloss.NewStyle().Background(lipgloss.Color("#1f6feb")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1).Render(fmt.Sprintf("↕ %d%% (%d/%d)", pct, botLine, totalLines))
	}

	controls := lipgloss.JoinHorizontal(
		lipgloss.Center,
		btnUnified, btnPath, btnCLI, btnLive,
		" ",
		scrollBadge,
	)

	controlsBar := lipgloss.NewStyle().
		Background(lipgloss.Color("#161b22")).
		Padding(0, 1).
		Width(m.Width - 2).
		Render(controls)

	borderStyle := StyleInactiveBorder
	headerText := " 🔍 DIFF VIEW "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 🔍 DIFF VIEW [FOCUSED] "
	}

	header := StylePanelHeader.Render(headerText)

	return borderStyle.
		Width(m.Width).
		Height(m.Height).
		Render(fmt.Sprintf("%s\n%s\n%s", header, controlsBar, m.Viewport.View()))
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
