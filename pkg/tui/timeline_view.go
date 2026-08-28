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
	vp := viewport.New(width, height)
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
	m.Viewport.Width = width - 2
	m.Viewport.Height = height - 2
	m.UpdateViewportContent()
}

// UpdateViewportContent renders the visual tree graph into string content.
func (m *TimelineViewModel) UpdateViewportContent() {
	if len(m.Commits) == 0 {
		m.Viewport.SetContent(lipgloss.NewStyle().Foreground(ColorMuted).Padding(1, 2).Render("No commits recorded yet.\nConfiguration changes will appear here in realtime."))
		return
	}

	var sb strings.Builder
	total := len(m.Commits)

	for i, c := range m.Commits {
		isSelected := i == m.SelectedIndex

		// Determine graph glyphs
		var branchGlyph, connectorGlyph string
		isRoot := (i == total-1)
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
			cursor = "▶ "
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
			} else {
				statStyled = StyleDiffStatMod.Render(fmt.Sprintf("[%s]", c.DiffStat))
			}
		}

		headerLine := fmt.Sprintf("%s%s %s %s %s %s", cursor, nodeStyled, shaStyled, authorStyled, timeStyled, statStyled)

		// Format line 2: Connector + Message
		connectorStyled := StyleTimelineLine.Render(connectorGlyph)
		msgStr := c.Message
		maxMsgLen := m.Width - 12
		if maxMsgLen > 10 && len(msgStr) > maxMsgLen {
			msgStr = msgStr[:maxMsgLen] + "…"
		}
		msgStyled := lipgloss.NewStyle().Foreground(ColorText).Render(msgStr)

		messageLine := fmt.Sprintf("    %s %s", connectorStyled, msgStyled)

		// Format line 3: Connector spacing
		spacingLine := fmt.Sprintf("    %s", connectorStyled)

		// Combine lines
		rowBlock := fmt.Sprintf("%s\n%s\n%s", headerLine, messageLine, spacingLine)
		if isSelected {
			rowBlock = StyleTimelineSelectedRow.Width(m.Width - 4).Render(rowBlock)
		}

		sb.WriteString(rowBlock)
		sb.WriteString("\n")
	}

	m.Viewport.SetContent(sb.String())
}

// View renders the visual timeline component.
func (m TimelineViewModel) View() string {
	borderStyle := StyleInactiveBorder
	headerText := " 📜 CONFIGURATION TIMELINE (Press [Tab] or [←] to focus) "
	if m.IsFocused {
		borderStyle = StyleActiveBorder
		headerText = " 📜 CONFIGURATION TIMELINE [FOCUSED] - [↑/↓/j/k] Navigate | [Tab] Detail "
	}

	header := StylePanelHeader.Render(headerText)
	content := m.Viewport.View()

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
