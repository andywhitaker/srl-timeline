package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/blame"
	"timeline/pkg/models"
)

// BlameViewModel manages the configuration blame view and contributor analytics.
type BlameViewModel struct {
	Entries      []models.BlameEntry
	Stats        blame.ContributorStats
	lastRendered string
	Viewport     viewport.Model
	Width        int
	Height       int
	IsFocused    bool
}

// NewBlameViewModel creates a new blame viewer.
func NewBlameViewModel(width, height int) BlameViewModel {
	vp := viewport.New(width, height)
	return BlameViewModel{
		Entries:   []models.BlameEntry{},
		Viewport:  vp,
		Width:     width,
		Height:    height,
		IsFocused: false,
	}
}

// SetBlame updates entries and contributor statistics.
func (m *BlameViewModel) SetBlame(entries []models.BlameEntry, stats blame.ContributorStats) {
	m.Entries = entries
	m.Stats = stats
	m.UpdateContent()
}

// SetSize handles terminal resizing.
func (m *BlameViewModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.Viewport.Width = width - 4
	m.Viewport.Height = height - 5
	m.lastRendered = ""
	m.UpdateContent()
}

// UpdateContent builds the combined blame line table and metrics sidebar.
func (m *BlameViewModel) UpdateContent() {
	if len(m.Entries) == 0 {
		m.Viewport.SetContent(lipgloss.NewStyle().Foreground(ColorMuted).Padding(1, 2).Render("No blame entries available."))
		return
	}

	var sb strings.Builder

	// Header banner
	header := fmt.Sprintf("%-10s %-12s %-12s %s\n", "COMMIT", "AUTHOR", "DATE", "CONFIG STATEMENT")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Background(ColorSurface).Render(header))
	sb.WriteString("\n")

	for _, e := range m.Entries {
		shaStyled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render(fmt.Sprintf("%-10s", e.ShortSHA()))

		authorStyle := StyleAuthorOther
		if e.Author == "admin" || e.Author == "root" {
			authorStyle = StyleAuthorAdmin
		}
		authorStyled := authorStyle.Render(fmt.Sprintf("%-12s", e.Author))

		dateStr := e.Timestamp.Format("2006-01-02")
		dateStyled := lipgloss.NewStyle().Foreground(ColorTextDim).Render(fmt.Sprintf("%-12s", dateStr))

		contentStyled := lipgloss.NewStyle().Foreground(ColorText).Render(e.Content)

		lineStr := fmt.Sprintf("%s %s %s %s\n", shaStyled, authorStyled, dateStyled, contentStyled)
		sb.WriteString(lineStr)
	}

	// Contributor Metrics Section at bottom
	sb.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Render("📊 CONTRIBUTOR METRICS & SUBSYSTEM BREAKDOWN") + "\n")
	sb.WriteString(fmt.Sprintf("Total Lines: %d\n\n", m.Stats.TotalLines))

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("👤 Author Contributions:\n"))
	for author, stat := range m.Stats.Authors {
		barLen := int(stat.Percentage / 5.0)
		if barLen < 1 && stat.Count > 0 {
			barLen = 1
		}
		bar := lipgloss.NewStyle().Foreground(ColorSuccess).Render(strings.Repeat("█", barLen))
		sb.WriteString(fmt.Sprintf("  • %-10s : %4d lines (%5.1f%%) %s\n", author, stat.Count, stat.Percentage, bar))
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render("📁 Subsystem Breakdown:\n"))
	for sub, authMap := range m.Stats.Subsystems {
		subTotal := 0
		for _, c := range authMap {
			subTotal += c
		}
		sb.WriteString(fmt.Sprintf("  [%s] - %d lines\n", strings.ToUpper(sub), subTotal))
		for a, c := range authMap {
			sb.WriteString(fmt.Sprintf("     └─ %-10s: %d\n", a, c))
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("─── End of Blame & Line Attribution Report ───") + "\n")

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

// View renders the blame panel with live scroll indicator.
func (m BlameViewModel) View() string {
	borderStyle := StyleInactiveBorder
	headerText := " 🕵️ CONFIGURATION BLAME (Press [Tab] or [→] to focus scroll) "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 🕵️ CONFIGURATION BLAME [FOCUSED] - Use [↑/↓/PgUp/PgDn] to scroll "
	}
	header := StylePanelHeader.Render(headerText)

	// Calculate scroll progress badge
	topLine := m.Viewport.YOffset + 1
	botLine := m.Viewport.YOffset + m.Viewport.Height
	totalLines := m.Viewport.TotalLineCount()
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

	controlsBar := lipgloss.NewStyle().
		Background(lipgloss.Color("#161b22")).
		Padding(0, 1).
		Width(m.Width - 4).
		Render(scrollBadge)

	return borderStyle.
		Width(m.Width).
		Height(m.Height).
		Render(fmt.Sprintf("%s\n%s\n\n%s", header, controlsBar, m.Viewport.View()))
}

// Update processes viewport navigation.
func (m BlameViewModel) Update(msg tea.Msg) (BlameViewModel, tea.Cmd) {
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}
