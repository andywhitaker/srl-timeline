package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/models"
)

// TimelineViewModel handles the interactive visual timeline tree graph.
type TimelineViewModel struct {
	Commits       []models.TimelineCommit
	SelectedIndex int
	Viewport      viewport.Model
	Width         int
	Height        int
	IsFocused     bool
}

// NewTimelineViewModel creates a new visual timeline component.
func NewTimelineViewModel(width, height int) TimelineViewModel {
	vpHeight := height - 1
	if vpHeight < 1 {
		vpHeight = 1
	}
	vp := viewport.New(width, vpHeight)
	return TimelineViewModel{
		Commits:       []models.TimelineCommit{},
		SelectedIndex: 0,
		Viewport:      vp,
		Width:         width,
		Height:        height,
		IsFocused:     true,
	}
}

// SetCommits updates the timeline commits list and rebuilds viewport content.
func (m *TimelineViewModel) SetCommits(commits []models.TimelineCommit) {
	m.Commits = commits
	if m.SelectedIndex >= len(commits) {
		m.SelectedIndex = len(commits) - 1
	}
	if m.SelectedIndex < 0 {
		m.SelectedIndex = 0
	}
	m.UpdateViewportContent()
}

// SelectedCommit returns currently active commit, or nil.
func (m *TimelineViewModel) SelectedCommit() *models.TimelineCommit {
	if len(m.Commits) == 0 || m.SelectedIndex < 0 || m.SelectedIndex >= len(m.Commits) {
		return nil
	}
	return &m.Commits[m.SelectedIndex]
}

// Next moves selection down.
func (m *TimelineViewModel) Next() {
	if m.SelectedIndex < len(m.Commits)-1 {
		m.SelectedIndex++
		m.UpdateViewportContent()
		m.ensureVisible()
	}
}

// Prev moves selection up.
func (m *TimelineViewModel) Prev() {
	if m.SelectedIndex > 0 {
		m.SelectedIndex--
		m.UpdateViewportContent()
		m.ensureVisible()
	}
}

// GotoTop moves selection to the first commit.
func (m *TimelineViewModel) GotoTop() {
	if len(m.Commits) > 0 && m.SelectedIndex != 0 {
		m.SelectedIndex = 0
		m.UpdateViewportContent()
		m.ensureVisible()
	}
}

// GotoBottom moves selection to the last commit.
func (m *TimelineViewModel) GotoBottom() {
	if len(m.Commits) > 0 && m.SelectedIndex != len(m.Commits)-1 {
		m.SelectedIndex = len(m.Commits) - 1
		m.UpdateViewportContent()
		m.ensureVisible()
	}
}

// PageUp moves selection up by a page of commits.
func (m *TimelineViewModel) PageUp() {
	if len(m.Commits) == 0 {
		return
	}
	step := (m.Height - 1) / 3
	if step < 1 {
		step = 5
	}
	m.SelectedIndex -= step
	if m.SelectedIndex < 0 {
		m.SelectedIndex = 0
	}
	m.UpdateViewportContent()
	m.ensureVisible()
}

// PageDown moves selection down by a page of commits.
func (m *TimelineViewModel) PageDown() {
	if len(m.Commits) == 0 {
		return
	}
	step := (m.Height - 1) / 3
	if step < 1 {
		step = 5
	}
	m.SelectedIndex += step
	if m.SelectedIndex >= len(m.Commits) {
		m.SelectedIndex = len(m.Commits) - 1
	}
	m.UpdateViewportContent()
	m.ensureVisible()
}

func (m *TimelineViewModel) ensureVisible() {
	// Each commit occupies roughly 3 lines in the visual graph
	targetY := m.SelectedIndex * 3
	if targetY < m.Viewport.YOffset {
		m.Viewport.SetYOffset(targetY)
	} else if targetY >= m.Viewport.YOffset+m.Height-3 {
		m.Viewport.SetYOffset(targetY - m.Height + 4)
	}
}

// SetSize handles terminal resizing.
func (m *TimelineViewModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.Viewport.Width = width
	vpHeight := height - 1
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.Viewport.Height = vpHeight
	m.UpdateViewportContent()
}

// UpdateViewportContent renders the visual tree graph into string content.
func (m *TimelineViewModel) UpdateViewportContent() {
	rowW := m.Width
	if rowW < 1 {
		rowW = 1
	}

	if len(m.Commits) == 0 {
		emptyMsg := "  No commits recorded yet.\n  Configuration changes will appear here in realtime."
		m.Viewport.SetContent(PaintSurface(emptyMsg, rowW, 2, ColorBackground, ColorMuted))
		return
	}

	var rows []string
	total := len(m.Commits)

	for i, c := range m.Commits {
		isSelected := i == m.SelectedIndex
		rowBg := ColorBackground
		rowFg := ColorText
		if isSelected {
			rowBg = ColorHighlight
		}

		// Determine graph glyphs
		var branchGlyph, connectorGlyph string
		isRoot := (i == total - 1)
		isHead := (i == 0)

		if isHead {
			if total == 1 {
				branchGlyph = "●"
				connectorGlyph = " "
			} else {
				branchGlyph = "●"
				connectorGlyph = "│"
			}
		} else if isRoot {
			branchGlyph = "└─○"
			connectorGlyph = "  "
		} else {
			if c.IsRestored {
				branchGlyph = "├─◆"
			} else {
				branchGlyph = "├─○"
			}
			connectorGlyph = "│ │"
		}

		// Cursor indicator
		cursor := "  "
		if isSelected {
			cursor = StyleSelectedRow.Render("▶ ")
		}

		// Author badge styling
		authorStyle := StyleAuthorOther
		if c.Author == "admin" || c.Author == "root" {
			authorStyle = StyleAuthorAdmin
		}

		// Format line 1: Node + SHA + Author + Time + Stat badge
		var nodeStyled string
		if isSelected {
			nodeStyled = StyleTimelineNodeActive.Render(branchGlyph)
		} else if c.IsRestored {
			nodeStyled = StyleTimelineNodeMilestone.Render(branchGlyph)
		} else {
			nodeStyled = StyleTimelineNodeNormal.Render(branchGlyph)
		}

		shaStyled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render(c.CommitID)
		authorStyled := authorStyle.Render(fmt.Sprintf("[%s]", c.Author))
		timeStyled := lipgloss.NewStyle().Foreground(ColorTextDim).Render(c.RelativeTime())

		var statStyled string
		if c.DiffStat != "" && c.DiffStat != "-" {
			if strings.HasPrefix(c.DiffStat, "+") {
				statStyled = StyleDiffStatAdd.Render(fmt.Sprintf("[%s]", c.DiffStat))
			} else if strings.HasPrefix(c.DiffStat, "-") {
				statStyled = StyleDiffStatDel.Render(fmt.Sprintf("[%s]", c.DiffStat))
			} else {
				statStyled = StyleDiffStatMod.Render(fmt.Sprintf("[%s]", c.DiffStat))
			}
		}

		headerLine := fmt.Sprintf("%s%s %s %s %s %s", cursor, nodeStyled, shaStyled, authorStyled, timeStyled, statStyled)
		if m.Width > 0 && lipgloss.Width(headerLine) > m.Width {
			headerLine = fmt.Sprintf("%s%s %s %s %s", cursor, nodeStyled, shaStyled, authorStyled, timeStyled)
			if lipgloss.Width(headerLine) > m.Width {
				headerLine = fmt.Sprintf("%s%s %s %s", cursor, nodeStyled, shaStyled, authorStyled)
			}
		}

		// Format line 2: Connector + Message
		connectorStyled := StyleTimelineLine.Render(connectorGlyph)
		msgStr := c.Message
		maxMsgLen := m.Width - 10
		msgRunes := []rune(msgStr)
		if maxMsgLen > 5 && len(msgRunes) > maxMsgLen {
			msgStr = string(msgRunes[:maxMsgLen]) + "…"
		}
		msgStyled := StyleCommitMessage.Bold(isSelected).Render(msgStr)

		messageLine := fmt.Sprintf("    %s %s", connectorStyled, msgStyled)

		// Format line 3: Connector spacing
		spacingLine := fmt.Sprintf("    %s", connectorStyled)

		line1 := PaintBarLine(headerLine, rowW, rowBg, rowFg)
		line2 := PaintBarLine(messageLine, rowW, rowBg, rowFg)
		line3 := PaintBarLine(spacingLine, rowW, rowBg, rowFg)

		rows = append(rows, fmt.Sprintf("%s\n%s\n%s", line1, line2, line3))
	}

	m.Viewport.SetContent(strings.Join(rows, "\n"))
}

// View renders the visual timeline component.
func (m TimelineViewModel) View() string {
	borderStyle := StyleInactiveBorder
	headerText := " 📜 TIMELINE "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 📜 TIMELINE [FOCUSED] "
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
	content := PaintSurface(m.Viewport.View(), innerW, vpH, ColorBackground, ColorText)

	box := borderStyle.
		Width(m.Width).
		Height(m.Height).
		Render(fmt.Sprintf("%s\n%s", header, content))

	return box
}

// Update processes Bubble Tea messages.
func (m TimelineViewModel) Update(msg tea.Msg) (TimelineViewModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			m.Next()
		case "k", "up":
			m.Prev()
		}
	case tea.MouseMsg:
		if msg.Type == tea.MouseWheelUp {
			m.Prev()
		} else if msg.Type == tea.MouseWheelDown {
			m.Next()
		}
	}
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}
