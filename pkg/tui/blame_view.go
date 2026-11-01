package tui

import (
	"fmt"
	"sort"
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
	vpHeight := height - 2
	if vpHeight < 1 {
		vpHeight = 1
	}
	vp := viewport.New(width, vpHeight)
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
	m.Viewport.Width = width
	vpHeight := height - 2
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.Viewport.Height = vpHeight
	m.lastRendered = ""
	m.UpdateContent()
}

// UpdateContent builds the combined blame line table and metrics sidebar.
func (m *BlameViewModel) UpdateContent() {
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

	if len(m.Entries) == 0 {
		m.lastRendered = ""
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  No blame entries available.")
		m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n" + PaintLine(msg, wrapW, ColorPanel, ColorMuted))
		return
	}

	var sb strings.Builder

	// Header banner
	header := fmt.Sprintf("%-10s %-12s %-12s %s", "COMMIT", "AUTHOR", "DATE", "CONFIG STATEMENT")
	sb.WriteString(PaintBarLine(lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Render(header), wrapW, ColorSurface, ColorMuted))
	sb.WriteString("\n")
	sb.WriteString(PaintLine("", wrapW, ColorPanel, ColorText))
	sb.WriteString("\n")

	maxContentW := m.Width - 38
	if maxContentW < 20 {
		maxContentW = 20
	}

	for _, e := range m.Entries {
		shaStyled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render(fmt.Sprintf("%-10s", e.ShortSHA()))

		authorStyle := StyleAuthorOther
		if e.Author == "admin" || e.Author == "root" {
			authorStyle = StyleAuthorAdmin
		}
		authorStyled := authorStyle.Render(fmt.Sprintf("%-12s", e.Author))

		dateStr := e.Timestamp.Format("2006-01-02")
		dateStyled := lipgloss.NewStyle().Foreground(ColorTextDim).Render(fmt.Sprintf("%-12s", dateStr))

		cleanContent := strings.ReplaceAll(e.Content, "\n", " ")
		cleanContent = strings.ReplaceAll(cleanContent, "\r", " ")
		cleanContent = strings.ReplaceAll(cleanContent, "\\n", " ")
		contentRunes := []rune(cleanContent)
		if len(contentRunes) > maxContentW {
			cleanContent = string(contentRunes[:maxContentW-3]) + "..."
		}

		contentStyled := StyleCommitMessage.Render(cleanContent)

		lineStr := fmt.Sprintf("%s %s %s %s", shaStyled, authorStyled, dateStyled, contentStyled)
		sb.WriteString(PaintBarLine(lineStr, wrapW, ColorPanel, ColorText))
		sb.WriteString("\n")
	}

	// Contributor Metrics Section at bottom
	sb.WriteString(PaintLine("", wrapW, ColorPanel, ColorText) + "\n")
	sb.WriteString(PaintLine(lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Render("📊 CONTRIBUTOR METRICS & SUBSYSTEM BREAKDOWN"), wrapW, ColorPanel, ColorCyan) + "\n")
	sb.WriteString(PaintLine(StyleCommitMessage.Render(fmt.Sprintf("Total Lines: %d", m.Stats.TotalLines)), wrapW, ColorPanel, ColorText) + "\n")
	sb.WriteString(PaintLine("", wrapW, ColorPanel, ColorText) + "\n")

	sb.WriteString(PaintLine(lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("👤 Author Contributions:"), wrapW, ColorPanel, ColorActive) + "\n")
	var authorsList []string
	for author := range m.Stats.Authors {
		authorsList = append(authorsList, author)
	}
	sort.Strings(authorsList)
	for _, author := range authorsList {
		stat := m.Stats.Authors[author]
		barLen := int(stat.Percentage / 5.0)
		if barLen < 1 && stat.Count > 0 {
			barLen = 1
		}
		bar := lipgloss.NewStyle().Foreground(ColorSuccess).Render(strings.Repeat("█", barLen))
		row := StyleCommitMessage.Render(fmt.Sprintf("  • %-10s : %4d lines (%5.1f%%) ", author, stat.Count, stat.Percentage)) + bar
		sb.WriteString(PaintLine(row, wrapW, ColorPanel, ColorText) + "\n")
	}

	sb.WriteString(PaintLine("", wrapW, ColorPanel, ColorText) + "\n")
	sb.WriteString(PaintLine(lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render("📁 Subsystem Breakdown:"), wrapW, ColorPanel, ColorWarning) + "\n")
	var subList []string
	for sub := range m.Stats.Subsystems {
		subList = append(subList, sub)
	}
	sort.Strings(subList)
	for _, sub := range subList {
		authMap := m.Stats.Subsystems[sub]
		subTotal := 0
		for _, c := range authMap {
			subTotal += c
		}
		subHeader := StyleCommitMessage.Bold(true).Render(fmt.Sprintf("  [%s] - %d lines", strings.ToUpper(sub), subTotal))
		sb.WriteString(PaintLine(subHeader, wrapW, ColorPanel, ColorText) + "\n")
		var subAuthors []string
		for a := range authMap {
			subAuthors = append(subAuthors, a)
		}
		sort.Strings(subAuthors)
		for _, a := range subAuthors {
			subRow := StyleCommitMessage.Render(fmt.Sprintf("     └─ %-10s: %d", a, authMap[a]))
			sb.WriteString(PaintLine(subRow, wrapW, ColorPanel, ColorText) + "\n")
		}
	}

	sb.WriteString(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n")
	sb.WriteString(PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Render("─── End of Blame & Line Attribution Report ───"), wrapW, ColorPanel, ColorMuted))

	content := sb.String()
	if m.lastRendered == content {
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
	headerText := " 🕵️ CONFIGURATION BLAME "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 🕵️ CONFIGURATION BLAME [FOCUSED] "
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

	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	vpH := m.Viewport.Height
	if vpH < 1 {
		vpH = 1
	}

	header := PaintBarLine(StylePanelHeader.Render(headerText), innerW, ColorSurface, ColorText)
	controlsBar := PaintBarLine(" "+scrollBadge, innerW, ColorSurface, ColorText)
	vpView := PaintSurface(m.Viewport.View(), innerW, vpH, ColorPanel, ColorText)

	return borderStyle.
		Width(m.Width).
		Height(m.Height).
		Render(fmt.Sprintf("%s\n%s\n%s", header, controlsBar, vpView))
}

// Update processes viewport navigation.
func (m BlameViewModel) Update(msg tea.Msg) (BlameViewModel, tea.Cmd) {
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}
