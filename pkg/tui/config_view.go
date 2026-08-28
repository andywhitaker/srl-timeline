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
	vpHeight := height - 3
	if vpHeight < 1 {
		vpHeight = 1
	}
	vp := viewport.New(width-4, vpHeight)
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
	m.Viewport.Width = width - 4
	vpHeight := height - 3
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.Viewport.Height = vpHeight
	m.lastRendered = ""
	m.UpdateContent()
}

// UpdateContent syntax highlights and loads the configuration into the viewport.
func (m *ConfigViewModel) UpdateContent() {
	if len(m.ConfigData) == 0 {
		m.Viewport.SetContent(lipgloss.NewStyle().Foreground(ColorMuted).Padding(1, 2).Render("No configuration data available for this revision."))
		return
	}

	var sb strings.Builder

	if m.FormatMode == "cli" {
		lines := normalizer.JSONToFlatCLI(m.ConfigData, "")
		if m.FilterPath != "" {
			lines = filter.FilterCLILines(lines, m.FilterPath)
		}
		if len(lines) == 0 {
			m.Viewport.SetContent(lipgloss.NewStyle().Foreground(ColorWarning).Padding(1, 2).Render(fmt.Sprintf("No CLI configuration statements match filter '%s'", m.FilterPath)))
			return
		}
		for _, l := range lines {
			if strings.HasPrefix(l, "set / interface") {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#79c0ff")).Render(l))
			} else if strings.HasPrefix(l, "set / network-instance") {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#d2a8ff")).Render(l))
			} else if strings.HasPrefix(l, "set / system") {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#56d364")).Render(l))
			} else if strings.HasPrefix(l, "set / acl") {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#ffa657")).Render(l))
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#e6edf3")).Render(l))
			}
			sb.WriteString("\n")
		}
	} else {
		data := m.ConfigData
		if m.FilterPath != "" {
			data = filter.FilterConfigSubtree(data, m.FilterPath)
		}
		if len(data) == 0 {
			m.Viewport.SetContent(lipgloss.NewStyle().Foreground(ColorWarning).Padding(1, 2).Render(fmt.Sprintf("No JSON configuration subtrees match filter '%s'", m.FilterPath)))
			return
		}
		jsonStr, _ := normalizer.CanonicalJSONString(data, 2)
		lines := strings.Split(jsonStr, "\n")
		for _, l := range lines {
			if strings.Contains(l, ":") {
				parts := strings.SplitN(l, ":", 2)
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#79c0ff")).Render(parts[0]))
				sb.WriteString(":")
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#a5d6ff")).Render(parts[1]))
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#e6edf3")).Render(l))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("─── End of Configuration (All statements loaded) ───") + "\n")

	content := sb.String()
	if m.Viewport.Width > 0 {
		content = lipgloss.NewStyle().Width(m.Viewport.Width).Render(content)
	}

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
		scrollBadge = lipgloss.NewStyle().Background(lipgloss.Color("#21262d")).Foreground(lipgloss.Color("#58a6ff")).Bold(true).Padding(0, 1).Render(fmt.Sprintf("⬆ TOP (Line 1-%d/%d)", botLine, totalLines))
	} else if m.Viewport.AtBottom() {
		scrollBadge = lipgloss.NewStyle().Background(lipgloss.Color("#238636")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1).Render(fmt.Sprintf("✓ BOTTOM (100%% | Line %d-%d/%d)", topLine, botLine, totalLines))
	} else {
		scrollBadge = lipgloss.NewStyle().Background(lipgloss.Color("#1f6feb")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1).Render(fmt.Sprintf("↕ %d%% (Line %d-%d/%d)", pct, topLine, botLine, totalLines))
	}
	controls := lipgloss.JoinHorizontal(
		lipgloss.Center,
		btnCLI, btnJSON,
		"  ",
		scrollBadge,
	)

	controlsBar := lipgloss.NewStyle().
		Background(lipgloss.Color("#161b22")).
		Padding(0, 1).
		Width(m.Width - 4).
		Render(controls)

	borderStyle := StyleInactiveBorder
	headerText := " 📄 FULL CONFIGURATION "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 📄 FULL CONFIGURATION [FOCUSED] "
	}

	header := StylePanelHeader.Render(headerText)

	return borderStyle.
		Width(m.Width).
		Height(m.Height).
		Render(fmt.Sprintf("%s\n%s\n%s", header, controlsBar, m.Viewport.View()))
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
		case "2", "j":
			m.FormatMode = "json"
			m.UpdateContent()
		}
	}
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}
