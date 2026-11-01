package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/filter"
	"timeline/pkg/normalizer"
)

// ConfigViewModel manages the snapshot configuration viewer.
type ConfigViewModel struct {
	ConfigData   map[string]interface{}
	FormatMode   string // "cli", "json"
	FilterPath   string
	lastRendered string
	Viewport     viewport.Model
	Width        int
	Height       int
	IsFocused    bool
}

// NewConfigViewModel creates a new configuration viewer.
func NewConfigViewModel(width, height int) ConfigViewModel {
	vpHeight := height - 2
	if vpHeight < 1 {
		vpHeight = 1
	}
	vp := viewport.New(width, vpHeight)
	return ConfigViewModel{
		ConfigData: make(map[string]interface{}),
		FormatMode: "cli",
		Viewport:   vp,
		Width:      width,
		Height:     height,
		IsFocused:  false,
	}
}

// SetConfig updates the config data and renders viewport.
func (m *ConfigViewModel) SetConfig(cfg map[string]interface{}, filterPath string) {
	m.ConfigData = cfg
	m.FilterPath = filterPath
	m.UpdateContent()
}

// SetSize handles terminal resizing.
func (m *ConfigViewModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.Viewport.Width = width
	vpHeight := height - 2
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.Viewport.Height = vpHeight
	m.lastRendered = ""
	m.UpdateContent()
}

// UpdateContent syntax highlights and loads the configuration into the viewport.
func (m *ConfigViewModel) UpdateContent() {
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

	if len(m.ConfigData) == 0 {
		m.lastRendered = ""
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  No configuration data available for this revision.")
		m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n" + PaintLine(msg, wrapW, ColorPanel, ColorMuted))
		return
	}

	var sb strings.Builder

	if m.FormatMode == "cli" {
		lines := normalizer.JSONToFlatCLI(m.ConfigData, "")
		if m.FilterPath != "" {
			lines = filter.FilterCLILines(lines, m.FilterPath)
		}
		if len(lines) == 0 {
			m.lastRendered = ""
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("  No CLI configuration statements match filter '%s'", m.FilterPath))
			m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorWarning) + "\n" + PaintLine(warn, wrapW, ColorPanel, ColorWarning))
			return
		}

		for _, l := range lines {
			if strings.HasPrefix(l, "set / interface") {
				sb.WriteString(PaintLine(StyleConfigInterface.Render(l), wrapW, ColorPanel, ColorSyntaxInterface))
			} else if strings.HasPrefix(l, "set / network-instance") {
				sb.WriteString(PaintLine(StyleConfigNetwork.Render(l), wrapW, ColorPanel, ColorSyntaxNetwork))
			} else if strings.HasPrefix(l, "set / system") {
				sb.WriteString(PaintLine(StyleConfigSystem.Render(l), wrapW, ColorPanel, ColorSyntaxSystem))
			} else if strings.HasPrefix(l, "set / acl") {
				sb.WriteString(PaintLine(StyleConfigACL.Render(l), wrapW, ColorPanel, ColorSyntaxACL))
			} else {
				sb.WriteString(PaintLine(StyleConfigDefault.Render(l), wrapW, ColorPanel, ColorText))
			}
			sb.WriteString("\n")
		}
	} else {
		data := m.ConfigData
		if m.FilterPath != "" {
			data = filter.FilterConfigSubtree(data, m.FilterPath)
		}
		if len(data) == 0 {
			m.lastRendered = ""
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("  No JSON configuration subtrees match filter '%s'", m.FilterPath))
			m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorWarning) + "\n" + PaintLine(warn, wrapW, ColorPanel, ColorWarning))
			return
		}
		jsonStr, _ := normalizer.CanonicalJSONString(data, 2)
		lines := strings.Split(jsonStr, "\n")

		for _, l := range lines {
			if strings.Contains(l, ":") {
				parts := strings.SplitN(l, ":", 2)
				styledLine := StyleConfigKey.Render(parts[0]) + StyleConfigPunctuation.Render(":") + StyleConfigValue.Render(parts[1])
				sb.WriteString(PaintLine(styledLine, wrapW, ColorPanel, ColorTextPrimary))
			} else {
				sb.WriteString(PaintLine(StyleConfigDefault.Render(l), wrapW, ColorPanel, ColorTextPrimary))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n")
	sb.WriteString(PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("─── End of Configuration (All statements loaded) ───"), wrapW, ColorPanel, ColorMuted))

	content := sb.String()

	if content == m.lastRendered {
		return
	}
	m.lastRendered = content
	prevY := m.Viewport.YOffset
	m.Viewport.SetContent(content)
	if prevY > 0 {
		m.Viewport.SetYOffset(prevY)
	}
}

// View renders the configuration panel with format switchers and scroll indicator.
func (m ConfigViewModel) View() string {
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

	btnCLI := btnInactiveStyle.Render("[1] Flat CLI")
	if m.FormatMode == "cli" {
		btnCLI = btnActiveStyle.Render("▶ [1] Flat CLI")
	}

	btnJSON := btnInactiveStyle.Render("[2] Hierarchical JSON")
	if m.FormatMode == "json" {
		btnJSON = btnActiveStyle.Render("▶ [2] Hierarchical JSON")
	}

	totalLines := m.Viewport.TotalLineCount()
	topLine := m.Viewport.YOffset + 1
	botLine := m.Viewport.YOffset + m.Viewport.Height
	if botLine > totalLines {
		botLine = totalLines
	}
	if totalLines == 0 {
		topLine = 0
	}
	pct := int(m.Viewport.ScrollPercent() * 100)
	var scrollBadge string
	if m.Viewport.AtTop() {
		scrollBadge = lipgloss.NewStyle().Background(ColorBadgeBg).Foreground(ColorActive).Bold(true).Padding(0, 1).Render(fmt.Sprintf("⬆ TOP (Line 1-%d/%d)", botLine, totalLines))
	} else if m.Viewport.AtBottom() {
		scrollBadge = lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1).Render(fmt.Sprintf("✓ BOTTOM (100%% | Line %d-%d/%d)", topLine, botLine, totalLines))
	} else {
		scrollBadge = lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1).Render(fmt.Sprintf("↕ %d%% (Line %d-%d/%d)", pct, topLine, botLine, totalLines))
	}
	controls := lipgloss.JoinHorizontal(
		lipgloss.Center,
		btnCLI, btnJSON,
		"  ",
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
	headerText := " 📄 FULL CONFIGURATION "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 📄 FULL CONFIGURATION [FOCUSED] "
	}

	header := PaintBarLine(StylePanelHeader.Render(headerText), innerW, ColorSurface, ColorText)
	vpView := PaintSurface(m.Viewport.View(), innerW, vpH, ColorPanel, ColorText)

	return borderStyle.
		Width(m.Width).
		Height(m.Height).
		Render(fmt.Sprintf("%s\n%s\n%s", header, controlsBar, vpView))
}

// Update handles format toggle key events and viewport scrolling.
func (m ConfigViewModel) Update(msg tea.Msg) (ConfigViewModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "1":
			m.FormatMode = "cli"
			m.UpdateContent()
		case "2":
			m.FormatMode = "json"
			m.UpdateContent()
		}
	}
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}
