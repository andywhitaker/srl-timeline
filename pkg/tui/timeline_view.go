package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"
	"github.com/muesli/reflow/truncate"

	"timeline/pkg/models"
)

const timelineLinesPerCommit = 2

// TimelineViewModel handles the interactive visual timeline tree graph.
type TimelineViewModel struct {
	Commits        []models.TimelineCommit
	SelectedIndex  int
	BaseCommitSHA  string
	HeadSHA        string
	FilterQuery    string
	WindowNarrowed bool // true when time window < All history
	TotalMatching  int  // total path-filtered commits across All history, before window slicing
	LeftSHA        string
	RightSHA       string
	Viewport       viewport.Model
	Width          int
	Height         int
	IsFocused      bool

	selectedSHA                string
	lastRenderedFocused        bool
	lastRenderedBaseSHA        string
	lastRenderedFilter         string
	lastRenderedLeftSHA        string
	lastRenderedRightSHA       string
	lastRenderedWindowNarrowed bool
	lastRenderedTotalMatching  int
	renderedLines              []string
}

// NewTimelineViewModel creates a new visual timeline component.
func NewTimelineViewModel(width, height int) TimelineViewModel {
	vpW := timelineViewportWidth(width)
	vpH := timelineViewportHeight(height)
	vp := viewport.New(vpW, vpH)
	return TimelineViewModel{
		Commits:       []models.TimelineCommit{},
		SelectedIndex: 0,
		Viewport:      vp,
		Width:         width,
		Height:        height,
		IsFocused:     true,
	}
}

func timelineViewportWidth(width int) int {
	if width > 4 {
		return width - 1 // reserve 1 col for slim scrollbar
	}
	if width < 1 {
		return 1
	}
	return width
}

func timelineViewportHeight(height int) int {
	if height < 1 {
		return 1
	}
	return height
}

// LinesPerCommit returns the fixed visual height (in rows) of each timeline commit card.
func (m TimelineViewModel) LinesPerCommit() int {
	return timelineLinesPerCommit
}

// SetCommits updates the timeline commits list while preserving selection by FullSHA,
// rebuilds viewport content, and keeps the selected commit visible.
func (m *TimelineViewModel) SetCommits(commits []models.TimelineCommit) {
	prevSHA := m.selectedSHA
	if cur := m.SelectedCommit(); cur != nil && cur.FullSHA != "" {
		prevSHA = cur.FullSHA
	}

	m.Commits = commits
	if len(commits) == 0 {
		m.SelectedIndex = 0
		m.Viewport.SetYOffset(0)
		m.UpdateViewportContent()
		return
	}

	matched := false
	if prevSHA != "" {
		for i, c := range commits {
			if c.FullSHA == prevSHA {
				m.SelectedIndex = i
				matched = true
				break
			}
		}
	}
	if !matched {
		if m.SelectedIndex >= len(commits) {
			m.SelectedIndex = len(commits) - 1
		}
		if m.SelectedIndex < 0 {
			m.SelectedIndex = 0
		}
	}
	m.selectedSHA = commits[m.SelectedIndex].FullSHA

	m.UpdateViewportContent()
	m.ensureVisible()
}

// SelectIndex moves selection directly to idx (clamped), refreshes content, and scrolls into view.
func (m *TimelineViewModel) SelectIndex(idx int) {
	if len(m.Commits) == 0 {
		m.SelectedIndex = 0
		m.selectedSHA = ""
		m.Viewport.SetYOffset(0)
		m.UpdateViewportContent()
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.Commits) {
		idx = len(m.Commits) - 1
	}
	m.SelectedIndex = idx
	m.selectedSHA = m.Commits[idx].FullSHA
	m.UpdateViewportContent()
	m.ensureVisible()
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
		m.SelectIndex(m.SelectedIndex + 1)
	}
}

// Prev moves selection up.
func (m *TimelineViewModel) Prev() {
	if m.SelectedIndex > 0 {
		m.SelectIndex(m.SelectedIndex - 1)
	}
}

// GotoTop moves selection to the first commit.
func (m *TimelineViewModel) GotoTop() {
	if len(m.Commits) > 0 && m.SelectedIndex != 0 {
		m.SelectIndex(0)
	}
}

// GotoBottom moves selection to the last commit.
func (m *TimelineViewModel) GotoBottom() {
	if len(m.Commits) > 0 && m.SelectedIndex != len(m.Commits)-1 {
		m.SelectIndex(len(m.Commits) - 1)
	}
}

// PageUp moves selection up by a page of commits.
func (m *TimelineViewModel) PageUp() {
	if len(m.Commits) == 0 {
		return
	}
	step := m.Viewport.Height / timelineLinesPerCommit
	if step < 1 {
		step = 1
	}
	m.SelectIndex(m.SelectedIndex - step)
}

// PageDown moves selection down by a page of commits.
func (m *TimelineViewModel) PageDown() {
	if len(m.Commits) == 0 {
		return
	}
	step := m.Viewport.Height / timelineLinesPerCommit
	if step < 1 {
		step = 1
	}
	m.SelectIndex(m.SelectedIndex + step)
}

func (m *TimelineViewModel) ensureVisible() {
	if len(m.Commits) == 0 {
		m.Viewport.SetYOffset(0)
		return
	}
	vpH := m.Viewport.Height
	if vpH < 1 {
		vpH = timelineViewportHeight(m.Height)
	}
	targetY := m.SelectedIndex * timelineLinesPerCommit
	targetEnd := targetY + timelineLinesPerCommit - 1

	if targetY < m.Viewport.YOffset {
		m.Viewport.SetYOffset(targetY)
	} else if targetEnd >= m.Viewport.YOffset+vpH {
		nextOff := targetEnd - vpH + 1
		if nextOff < 0 {
			nextOff = 0
		}
		m.Viewport.SetYOffset(nextOff)
	}
}

// SetSize handles terminal resizing without needlessly re-rendering commit cards when width is unchanged.
func (m *TimelineViewModel) SetSize(width, height int) {
	if m.Width == width && m.Height == height {
		return
	}
	prevW := m.Width
	m.Width = width
	m.Height = height
	m.Viewport.Width = timelineViewportWidth(width)
	m.Viewport.Height = timelineViewportHeight(height)
	if prevW != width || m.Viewport.TotalLineCount() == 0 {
		m.UpdateViewportContent()
	}
	m.ensureVisible()
}

// UpdateViewportContent renders the compact 2-line commit cards into viewport content.
func (m *TimelineViewModel) UpdateViewportContent() {
	vpW := timelineViewportWidth(m.Width)
	vpH := timelineViewportHeight(m.Height)
	m.Viewport.Width = vpW
	m.Viewport.Height = vpH

	m.lastRenderedFocused = m.IsFocused
	m.lastRenderedBaseSHA = m.BaseCommitSHA
	m.lastRenderedFilter = m.FilterQuery
	m.lastRenderedLeftSHA = m.LeftSHA
	m.lastRenderedRightSHA = m.RightSHA
	m.lastRenderedWindowNarrowed = m.WindowNarrowed
	m.lastRenderedTotalMatching = m.TotalMatching

	if len(m.Commits) == 0 {
		var emptyMsg string
		if m.WindowNarrowed && m.TotalMatching > 0 {
			emptyMsg = "No commits in current time window.\nPress 0 (All) or f (Fit) to expand."
		} else if q := strings.TrimSpace(m.FilterQuery); q != "" {
			emptyMsg = fmt.Sprintf("No commits match filter '%s'\nPress Esc to clear filter.", q)
		} else {
			emptyMsg = "No commits recorded yet.\nConfiguration changes will appear here in realtime."
		}
		var padded []string
		padded = append(padded, PaintLine("", vpW, ColorBackground, ColorMuted))
		for _, line := range strings.Split(emptyMsg, "\n") {
			padded = append(padded, PaintLine(" "+line, vpW, ColorBackground, ColorMuted))
		}
		m.renderedLines = padded
		m.Viewport.SetContent(strings.Join(padded, "\n"))
		m.Viewport.SetYOffset(0)
		return
	}

	rows := make([]string, 0, len(m.Commits)*timelineLinesPerCommit)
	total := len(m.Commits)
	unfiltered := strings.TrimSpace(m.FilterQuery) == ""

	for i, c := range m.Commits {
		isSelected := i == m.SelectedIndex
		isHead := false
		if m.HeadSHA != "" {
			isHead = c.FullSHA == m.HeadSHA
		} else {
			isHead = c.IsCurrent || (unfiltered && i == 0)
		}
		isRoot := c.IsStartup || (unfiltered && i == total-1)
		isBase := m.BaseCommitSHA != "" && c.FullSHA == m.BaseCommitSHA
		isLeft := m.LeftSHA != "" && c.FullSHA != "" && c.FullSHA == m.LeftSHA
		isRight := m.RightSHA != "" && c.FullSHA != "" && c.FullSHA == m.RightSHA

		rowBg := ColorBackground
		rowFg := ColorText
		var accentBar string
		if isSelected {
			if m.IsFocused {
				rowBg = ColorHighlight
				accentBar = lipgloss.NewStyle().Foreground(ColorActive).Background(rowBg).Render(GlyphBar)
			} else {
				rowBg = ColorSurface
				accentBar = lipgloss.NewStyle().Foreground(ColorMuted).Background(rowBg).Render(GlyphBar)
			}
		} else {
			accentBar = lipgloss.NewStyle().Background(rowBg).Render(" ")
		}

		// Node glyph: check c.IsRestored BEFORE isHead/isRoot so Restore/Cherry-Pick
		// commits ALWAYS display ◆ (GlyphMilestone), even at HEAD or selected.
		var nodeGlyph string
		nodeStyle := lipgloss.NewStyle().Background(rowBg)
		switch {
		case c.IsRestored:
			nodeGlyph = GlyphMilestone
			nodeStyle = nodeStyle.Bold(true).Foreground(ColorWarning)
		case isSelected:
			nodeGlyph = GlyphSelected
			if m.IsFocused {
				nodeStyle = nodeStyle.Bold(true).Foreground(ColorActive)
			} else {
				nodeStyle = nodeStyle.Foreground(ColorMuted)
			}
		case isHead:
			nodeGlyph = GlyphLive
			nodeStyle = nodeStyle.Bold(true).Foreground(ColorSuccess)
		case isRoot:
			nodeGlyph = GlyphEmpty
			nodeStyle = nodeStyle.Foreground(ColorMuted)
		default:
			nodeGlyph = GlyphPaused
			nodeStyle = nodeStyle.Foreground(ColorMuted)
		}
		nodeStyled := nodeStyle.Render(nodeGlyph)

		// Commit ID
		shaStyled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(rowBg).Render(c.CommitID)

		// Micro-badges
		var badges []string
		if isHead {
			badges = append(badges, lipgloss.NewStyle().
				Background(ColorSuccessBg).
				Foreground(ColorOnAccent).
				Bold(true).
				Render("HEAD"))
		}
		if isBase {
			badges = append(badges, lipgloss.NewStyle().
				Background(ColorPrimary).
				Foreground(ColorOnAccent).
				Bold(true).
				Render("BASE"))
		}
		if isLeft && !isBase {
			badges = append(badges, lipgloss.NewStyle().
				Background(ColorPrimary).
				Foreground(ColorOnAccent).
				Bold(true).
				Render("A"))
		}
		if isRight {
			badges = append(badges, lipgloss.NewStyle().
				Background(ColorSuccessBg).
				Foreground(ColorOnAccent).
				Bold(true).
				Render("B"))
		}
		if c.IsRestored {
			badges = append(badges, lipgloss.NewStyle().
				Background(ColorWarningBg).
				Foreground(ColorOnWarning).
				Bold(true).
				Render("RSTR"))
		}
		if isRoot {
			badges = append(badges, lipgloss.NewStyle().
				Background(ColorBadgeBg).
				Foreground(ColorMuted).
				Render("INIT"))
		}

		// Author
		authorName := strings.TrimSpace(c.Author)
		if authorName == "" {
			authorName = "unknown"
		}
		if len([]rune(authorName)) > 12 {
			authorName = truncate.StringWithTail(authorName, 12, "…")
		}
		authorStyle := StyleAuthorOther.Background(rowBg)
		if authorName == "admin" || authorName == "root" {
			authorStyle = StyleAuthorAdmin.Background(rowBg)
		}
		authorStyled := authorStyle.Render(authorName)

		// Compact relative time ("27m" instead of "27m ago") so HEAD cards comfortably fit diff stats
		relTime := compactRelativeTime(c.RelativeTime())
		timeStyled := lipgloss.NewStyle().Foreground(ColorTextDim).Background(rowBg).Render(relTime)

		// Color-coded multi-part DiffStat tokens (+N green, ~N warning, -N danger)
		statStyled := formatDiffStatTokens(c.DiffStat, rowBg)

		// Progressive assembly of Line 1 so we fit cleanly into vpW without clipping essential fields.
		sp := lipgloss.NewStyle().Background(rowBg).Render(" ")
		line1Str := accentBar + nodeStyled + sp + shaStyled
		usedW := lipgloss.Width(line1Str)

		for _, b := range badges {
			bw := lipgloss.Width(b)
			if usedW+1+bw <= vpW {
				line1Str += sp + b
				usedW += 1 + bw
			}
		}

		if aw := lipgloss.Width(authorStyled); usedW+1+aw <= vpW {
			line1Str += sp + authorStyled
			usedW += 1 + aw
		}

		if tw := lipgloss.Width(timeStyled); usedW+1+tw <= vpW {
			line1Str += sp + timeStyled
			usedW += 1 + tw
		}

		if statStyled != "" {
			if sw := lipgloss.Width(statStyled); usedW+1+sw <= vpW {
				line1Str += sp + statStyled
			}
		}

		// Line 2: <accentBar><rail> <subtitle>
		// Rail is 1-col │ directly under <nodeGlyph>, or space on root / final row.
		railGlyph := GlyphPipe
		if isRoot || i == total-1 {
			railGlyph = " "
		}
		railStyled := lipgloss.NewStyle().Foreground(ColorBorder).Background(rowBg).Render(railGlyph)

		subtitle := commitSubtitle(c)
		maxSubW := vpW - 3 // accentBar (1) + rail (1) + space (1)
		if maxSubW < 4 {
			maxSubW = 4
		}
		if lipgloss.Width(subtitle) > maxSubW {
			subtitle = truncate.StringWithTail(subtitle, uint(maxSubW), "…")
		}

		subFg := ColorMuted
		subStyle := lipgloss.NewStyle().Foreground(subFg).Background(rowBg)
		if isSelected {
			subFg = ColorText
			subStyle = lipgloss.NewStyle().Foreground(subFg).Background(rowBg).Bold(m.IsFocused)
		}
		line2Str := accentBar + railStyled + sp + subStyle.Render(subtitle)

		rows = append(rows,
			PaintBarLine(line1Str, vpW, rowBg, rowFg),
			PaintBarLine(line2Str, vpW, rowBg, subFg),
		)
	}

	m.renderedLines = rows
	m.Viewport.SetContent(strings.Join(rows, "\n"))
}

// compactRelativeTime strips the redundant " ago" suffix (and shortens "just now" to "now")
// so Timeline HEAD cards fit badges, author, timestamp, and full diff stats at 120x35.
func compactRelativeTime(rel string) string {
	s := strings.TrimSpace(rel)
	if s == "just now" {
		return "now"
	}
	return strings.TrimSuffix(s, " ago")
}

// commitSubtitle prefers a non-generic user message, else c.SummaryScope, else c.Message.
func commitSubtitle(c models.TimelineCommit) string {
	cleanMsg := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(c.Message, "\r", ""), "\n", " "))
	scope := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(c.SummaryScope, "\r", ""), "\n", " "))

	if cleanMsg != "" && !isGenericCommitMessage(cleanMsg) {
		// If daemon/gitbackend appended a user comment after " — ", show the user comment cleanly.
		if strings.HasPrefix(strings.ToLower(cleanMsg), "config commit by ") {
			if idx := strings.Index(cleanMsg, " — "); idx >= 0 {
				if comment := strings.TrimSpace(cleanMsg[idx+len(" — "):]); comment != "" {
					return comment
				}
			}
		}
		return cleanMsg
	}
	if scope != "" {
		return scope
	}
	if cleanMsg != "" {
		return cleanMsg
	}
	return "Configuration update"
}

func isGenericCommitMessage(msg string) bool {
	m := strings.TrimSpace(msg)
	if m == "" {
		return true
	}
	lower := strings.ToLower(m)
	if strings.HasPrefix(lower, "config commit by ") {
		if idx := strings.Index(m, " — "); idx >= 0 {
			if strings.TrimSpace(m[idx+len(" — "):]) != "" {
				return false
			}
		}
		return true
	}
	switch lower {
	case "configuration change",
		"config change",
		"auto-captured configuration change",
		"initial baseline configuration":
		return true
	}
	return false
}

func (m TimelineViewModel) titleText() string {
	titleText := "TIMELINE"
	if m.WindowNarrowed {
		if len(m.Commits) > 0 {
			sel := m.SelectedIndex + 1
			if sel < 1 {
				sel = 1
			}
			if sel > len(m.Commits) {
				sel = len(m.Commits)
			}
			titleText = fmt.Sprintf("IN WINDOW %d/%d", sel, len(m.Commits))
		} else {
			titleText = "IN WINDOW 0/0"
		}
	} else if len(m.Commits) > 0 {
		sel := m.SelectedIndex + 1
		if sel < 1 {
			sel = 1
		}
		if sel > len(m.Commits) {
			sel = len(m.Commits)
		}
		titleText = fmt.Sprintf("TIMELINE %d/%d", sel, len(m.Commits))
	}
	return titleText
}

func (m TimelineViewModel) buildTopBorder(outerW int, borderFg lipgloss.Color) (topRow string, collapseStartX, collapseEndX int) {
	b := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(borderFg).Background(ColorBackground)
	if outerW <= 1 {
		return PaintBarLine(b.TopLeft, outerW, ColorBackground, borderFg), -1, -1
	}

	titleStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorBackground)
	if m.IsFocused {
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorBackground)
	}

	titleText := m.titleText()
	btnText := "[◀]"
	if outerW < 20 {
		btnText = "◀"
	}
	btnW := ansi.PrintableRuneWidth(btnText)
	btnStyled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorBadgeBg).Render(btnText)
	sp := lipgloss.NewStyle().Background(ColorBackground).Render(" ")

	// Layout: ╭─ (2) + " " (1) + title + " " (1) + fill (>=1) + " " (1) + btn (btnW) + " " (1) + ─╮ (2)
	// Fixed overhead = 8 + btnW, plus at least 1 fill char => 9 + btnW.
	if outerW >= 12+btnW {
		maxTitleW := outerW - 9 - btnW
		if maxTitleW < 2 {
			maxTitleW = 2
		}
		displayTitle := titleText
		if ansi.PrintableRuneWidth(displayTitle) > maxTitleW {
			displayTitle = truncate.String(displayTitle, uint(maxTitleW))
		}
		tw := ansi.PrintableRuneWidth(displayTitle)
		fillW := outerW - 8 - tw - btnW
		if fillW < 1 {
			fillW = 1
		}
		collapseStartX = 5 + tw + fillW
		collapseEndX = collapseStartX + btnW
		raw := borderStyle.Render(b.TopLeft+b.Top) +
			sp + titleStyle.Render(displayTitle) + sp +
			borderStyle.Render(strings.Repeat(b.Top, fillW)) +
			sp + btnStyled + sp +
			borderStyle.Render(b.Top+b.TopRight)
		return PaintBarLine(raw, outerW, ColorBackground, borderFg), collapseStartX, collapseEndX
	}

	// Ultra-narrow fallback without button.
	return PaintBarLine(b.TopLeft+strings.Repeat(b.Top, outerW-2)+b.TopRight, outerW, ColorBackground, borderFg), -1, -1
}

// CollapseHitRange returns the clickable [startX, endX) column range (in outer pane coordinates)
// of the `[◀]` collapse button on the top border of the Left Panel, or (-1, -1) if hidden.
func (m TimelineViewModel) CollapseHitRange() (startX, endX int) {
	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	borderFg := ColorBorder
	if m.IsFocused {
		borderFg = ColorActive
	}
	_, startX, endX = m.buildTopBorder(innerW+2, borderFg)
	return startX, endX
}

// RenderCollapsedTimelineRail renders the slim 3-column collapsed sidebar rail (`▶`) shown
// on the far left of the workspace when the Left Commit List is minimized, giving a clear
// visual indicator and clickable expand target.
func RenderCollapsedTimelineRail(height int) string {
	if height <= 0 {
		return ""
	}
	b := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(ColorBackground)
	arrowStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorSurfaceFocus)
	emptyCell := lipgloss.NewStyle().Background(ColorSurface).Render(" ")
	leftBar := borderStyle.Render(b.Left)
	rightBar := borderStyle.Render(b.Right)

	topRow := PaintBarLine(b.TopLeft+b.Top+b.TopRight, 3, ColorBackground, ColorBorder)
	if height == 1 {
		return topRow
	}

	rows := make([]string, 0, height)
	rows = append(rows, topRow)

	innerH := height - 2
	midRow := innerH / 2
	for i := 0; i < innerH; i++ {
		cell := emptyCell
		if i == 0 || i == midRow {
			cell = arrowStyle.Render("▶")
		}
		rows = append(rows, PaintBarLine(leftBar+cell+rightBar, 3, ColorBackground, ColorBorder))
	}

	bottomRow := PaintBarLine(b.BottomLeft+b.Bottom+b.BottomRight, 3, ColorBackground, ColorBorder)
	rows = append(rows, bottomRow)
	return strings.Join(rows, "\n")
}

// View renders the visual timeline component inside a border-titled box with a slim scrollbar
// and a clickable `[◀]` collapse button on the top border.
func (m TimelineViewModel) View() string {
	if m.IsFocused != m.lastRenderedFocused ||
		m.BaseCommitSHA != m.lastRenderedBaseSHA ||
		m.FilterQuery != m.lastRenderedFilter ||
		m.LeftSHA != m.lastRenderedLeftSHA ||
		m.RightSHA != m.lastRenderedRightSHA ||
		m.WindowNarrowed != m.lastRenderedWindowNarrowed ||
		m.TotalMatching != m.lastRenderedTotalMatching {
		prevY := m.Viewport.YOffset
		m.UpdateViewportContent()
		m.Viewport.SetYOffset(prevY)
	}

	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	innerH := m.Height
	if innerH < 1 {
		innerH = 1
	}
	outerW := innerW + 2
	outerH := innerH + 2

	borderFg := ColorBorder
	if m.IsFocused {
		borderFg = ColorActive
	}

	topRow, _, _ := m.buildTopBorder(outerW, borderFg)
	b := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(borderFg).Background(ColorBackground)
	leftBar := borderStyle.Render(b.Left)
	rightBar := borderStyle.Render(b.Right)

	body := renderLinesWithScrollbar(m.renderedLines, m.Viewport, innerW, innerH, ColorBackground)
	paintedBody := PaintSurface(body, innerW, innerH, ColorBackground, ColorText)

	rows := make([]string, 0, outerH)
	rows = append(rows, topRow)
	for _, line := range strings.Split(paintedBody, "\n") {
		rows = append(rows, leftBar+line+rightBar)
	}
	bottomRow := PaintBarLine(b.BottomLeft+strings.Repeat(b.Bottom, innerW)+b.BottomRight, outerW, ColorBackground, borderFg)
	rows = append(rows, bottomRow)

	return strings.Join(rows, "\n")
}
