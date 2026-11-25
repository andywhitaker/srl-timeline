package modals

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"

	"timeline/pkg/models"
)

// LiveRunningSentinel is the sentinel SHA representing the switch's live running configuration.
const LiveRunningSentinel = "__LIVE_RUNNING__"

// PickerSide identifies which existing endpoint (Left/A or Right/B) should receive the
// initial cursor focus when opening the unified 2-configuration compare picker.
type PickerSide int

const (
	// PickerSideLeft focuses the Left (A, older) configuration on open.
	PickerSideLeft PickerSide = iota
	// PickerSideRight focuses the Right (B, newer) configuration on open.
	PickerSideRight
)

const (
	commitPickerPreferredWidth = 86
	commitPickerMaxVisibleRows = 14
)

// CommitPickerModal provides an interactive 2-configuration multi-select picker for
// side-by-side comparison. Users toggle up to two configurations with Space (or mouse
// click), must unselect one (or press c to clear all) before choosing another when 2 are
// already checked, and press Enter to confirm. The older selected configuration is
// automatically assigned to Left (A) and the newer one (or live running config) to Right (B).
type CommitPickerModal struct {
	Side          PickerSide
	Commits       []models.TimelineCommit
	SelectedSHAs  []string // 0..2 canonical SHAs; when len==2, [0] is older (A) and [1] is newer (B)
	LeftSHA       string   // resolved older (A) SHA when len(SelectedSHAs)==2
	RightSHA      string   // resolved newer (B) SHA when len(SelectedSHAs)==2
	IncludeLive   bool     // true shows "● RUNNING CONFIG" as top option
	SelectedIndex int
	ScrollOffset  int
	Notice        string // transient inline hint (e.g. max-2 reached or need-2-to-confirm)
	Width         int
	Height        int
}

// NewCommitPickerModal constructs a unified 2-configuration CommitPickerModal with the
// currently compared leftSHA and rightSHA pre-checked (when present in the list) and the
// cursor positioned on the endpoint matching focusSide.
func NewCommitPickerModal(
	focusSide PickerSide,
	commits []models.TimelineCommit,
	leftSHA, rightSHA string,
	includeLive bool,
	width, height int,
) CommitPickerModal {
	copied := make([]models.TimelineCommit, len(commits))
	copy(copied, commits)

	m := CommitPickerModal{
		Side:        focusSide,
		Commits:     copied,
		IncludeLive: includeLive,
		Width:       width,
		Height:      height,
	}

	// Pre-select current A and B endpoints only if they exist in the visible entries so
	// the user can always see and unselect every checked item.
	if canonLeft, ok := m.canonicalizeSHA(leftSHA); ok {
		m.SelectedSHAs = append(m.SelectedSHAs, canonLeft)
	}
	if canonRight, ok := m.canonicalizeSHA(rightSHA); ok && !m.isSHASelected(canonRight) && len(m.SelectedSHAs) < 2 {
		m.SelectedSHAs = append(m.SelectedSHAs, canonRight)
	}
	m.normalizeSelectionOrder()

	// Position initial cursor on the requested endpoint for quick single-side replacement.
	targetSHA := leftSHA
	if focusSide == PickerSideRight {
		targetSHA = rightSHA
	}
	if idx := m.indexForSHA(targetSHA); idx >= 0 {
		m.SelectedIndex = idx
	}

	m.syncScroll()
	return m
}

func (m CommitPickerModal) totalEntries() int {
	total := len(m.Commits)
	if m.IncludeLive {
		total++
	}
	return total
}

func (m CommitPickerModal) isLiveIndex(idx int) bool {
	return m.IncludeLive && idx == 0
}

func (m CommitPickerModal) commitAtIndex(idx int) (models.TimelineCommit, int, bool) {
	if idx < 0 || idx >= m.totalEntries() {
		return models.TimelineCommit{}, -1, false
	}
	if m.IncludeLive {
		if idx == 0 {
			return models.TimelineCommit{}, -1, false
		}
		ci := idx - 1
		return m.Commits[ci], ci, true
	}
	return m.Commits[idx], idx, true
}

func (m CommitPickerModal) shaAtIndex(idx int) string {
	if idx < 0 || idx >= m.totalEntries() {
		return ""
	}
	if m.isLiveIndex(idx) {
		return LiveRunningSentinel
	}
	c, _, ok := m.commitAtIndex(idx)
	if !ok {
		return ""
	}
	if c.FullSHA != "" {
		return c.FullSHA
	}
	return c.CommitID
}

func (m CommitPickerModal) indexForSHA(sha string) int {
	if sha == "" {
		return -1
	}
	if sha == LiveRunningSentinel {
		if m.IncludeLive {
			return 0
		}
		return -1
	}
	for i, c := range m.Commits {
		if commitMatchesSHA(c, sha) {
			if m.IncludeLive {
				return i + 1
			}
			return i
		}
	}
	return -1
}

func (m CommitPickerModal) canonicalizeSHA(sha string) (string, bool) {
	idx := m.indexForSHA(sha)
	if idx < 0 {
		return "", false
	}
	return m.shaAtIndex(idx), true
}

// SelectedSHA returns the SHA (or LiveRunningSentinel) of the currently focused cursor row,
// or an empty string if the list has no entries.
func (m CommitPickerModal) SelectedSHA() string {
	total := m.totalEntries()
	if total == 0 {
		return ""
	}
	idx := m.SelectedIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= total {
		idx = total - 1
	}
	return m.shaAtIndex(idx)
}

// ResolvedEndpoints returns (leftSHA, rightSHA) with the older configuration on Left (A)
// and the newer configuration (or LiveRunningSentinel) on Right (B) when exactly 2
// configurations are checked. Otherwise returns ("", "").
func (m CommitPickerModal) ResolvedEndpoints() (leftSHA, rightSHA string) {
	if len(m.SelectedSHAs) != 2 {
		return "", ""
	}
	return m.SelectedSHAs[0], m.SelectedSHAs[1]
}

// SelectionCount returns how many configurations are currently checked (0, 1, or 2).
func (m CommitPickerModal) SelectionCount() int {
	return len(m.SelectedSHAs)
}

func (m CommitPickerModal) isSHASelected(sha string) bool {
	if sha == "" {
		return false
	}
	for _, s := range m.SelectedSHAs {
		if s == sha {
			return true
		}
	}
	return false
}

// isOlderSHA reports whether a is strictly older than b using the same chronological rules
// as AppModel.isOlderCommit (with LiveRunningSentinel always strictly newest).
func (m CommitPickerModal) isOlderSHA(a, b string) bool {
	if a == b {
		return false
	}
	if a == LiveRunningSentinel {
		return false
	}
	if b == LiveRunningSentinel {
		return true
	}

	var ca, cb *models.TimelineCommit
	idxA, idxB := -1, -1
	for i := range m.Commits {
		if ca == nil && commitMatchesSHA(m.Commits[i], a) {
			ca = &m.Commits[i]
			idxA = i
		}
		if cb == nil && commitMatchesSHA(m.Commits[i], b) {
			cb = &m.Commits[i]
			idxB = i
		}
	}
	if ca != nil && cb != nil {
		if !ca.Timestamp.Equal(cb.Timestamp) {
			return ca.Timestamp.Before(cb.Timestamp)
		}
		// Commits are ordered newest-first, so a higher slice index is older.
		if idxA != idxB {
			return idxA > idxB
		}
	}
	return false
}

func (m *CommitPickerModal) normalizeSelectionOrder() {
	if len(m.SelectedSHAs) == 2 {
		if m.isOlderSHA(m.SelectedSHAs[1], m.SelectedSHAs[0]) {
			m.SelectedSHAs[0], m.SelectedSHAs[1] = m.SelectedSHAs[1], m.SelectedSHAs[0]
		}
		m.LeftSHA = m.SelectedSHAs[0]
		m.RightSHA = m.SelectedSHAs[1]
		return
	}
	m.LeftSHA = ""
	m.RightSHA = ""
}

// ToggleSelected toggles the checked state of the currently highlighted row.
// When 2 configurations are already checked, attempting to check a 3rd row is rejected
// with an inline notice until one is unchecked or all selections are cleared.
func (m *CommitPickerModal) ToggleSelected() {
	m.toggleIndex(m.SelectedIndex)
}

func (m *CommitPickerModal) toggleIndex(idx int) {
	sha := m.shaAtIndex(idx)
	if sha == "" {
		return
	}

	// If already selected, unselect it.
	for i, existing := range m.SelectedSHAs {
		if existing == sha {
			m.SelectedSHAs = append(m.SelectedSHAs[:i], m.SelectedSHAs[i+1:]...)
			m.Notice = ""
			m.normalizeSelectionOrder()
			return
		}
	}

	// Enforce max-2 strict rule: user must unselect one or clear all first.
	if len(m.SelectedSHAs) >= 2 {
		m.Notice = "2 already selected — press Space on a checked item to unselect, or c to clear all"
		return
	}

	m.SelectedSHAs = append(m.SelectedSHAs, sha)
	m.Notice = ""
	m.normalizeSelectionOrder()
}

// ClearSelections deselects all checked configurations.
func (m *CommitPickerModal) ClearSelections() {
	m.SelectedSHAs = nil
	m.LeftSHA = ""
	m.RightSHA = ""
	m.Notice = ""
}

func (m CommitPickerModal) shortLabelForSHA(sha string) string {
	if sha == LiveRunningSentinel {
		return "● RUNNING"
	}
	for _, c := range m.Commits {
		if commitMatchesSHA(c, sha) {
			id := strings.TrimSpace(c.CommitID)
			if id == "" {
				id = strings.TrimSpace(c.FullSHA)
			}
			if len(id) > 8 {
				id = id[:8]
			}
			if id != "" {
				return id
			}
		}
	}
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func commitMatchesSHA(c models.TimelineCommit, sha string) bool {
	if sha == "" || sha == LiveRunningSentinel {
		return false
	}
	if c.FullSHA != "" && c.FullSHA == sha {
		return true
	}
	if c.CommitID != "" && c.CommitID == sha {
		return true
	}
	return false
}

func (m CommitPickerModal) chromeMetrics() (compact, ultraCompact bool, headerLines, footerLines, emptyNoteLines int) {
	compact = m.Height > 0 && m.Height <= 24
	ultraCompact = m.Height > 0 && m.Height <= 10

	headerLines = 3 // header + live status/assignment strip + blank spacer
	if ultraCompact {
		headerLines = 1 // header only
	} else if compact {
		headerLines = 2 // header + live status/assignment strip
	}

	footerLines = 2 // blank spacer + footer
	if compact {
		footerLines = 1 // footer only
	}

	if len(m.Commits) == 0 && m.IncludeLive && !ultraCompact {
		emptyNoteLines = 1
	}
	return
}

func (m CommitPickerModal) visibleRows() int {
	total := m.totalEntries()
	if total == 0 {
		return 0
	}
	_, _, _, maxInnerH := modalBoxMetrics(m.Width, m.Height, commitPickerPreferredWidth)
	_, _, headerLines, footerLines, emptyNoteLines := m.chromeMetrics()
	baseOverhead := headerLines + footerLines + emptyNoteLines

	capRows := commitPickerMaxVisibleRows
	if maxInnerH <= 0 {
		if total < capRows {
			return total
		}
		return capRows
	}

	availNoScroll := maxInnerH - baseOverhead
	if availNoScroll < 1 {
		availNoScroll = 1
	}
	if total <= availNoScroll && total <= capRows {
		return total
	}

	// Reserve up to 2 lines for ↑/↓ scroll indicators so content never overflows maxInnerH.
	availWithScroll := maxInnerH - baseOverhead - 2
	if availWithScroll < 1 {
		availWithScroll = 1
	}
	if availWithScroll > capRows {
		availWithScroll = capRows
	}
	if availWithScroll > total {
		availWithScroll = total
	}
	return availWithScroll
}

func (m *CommitPickerModal) syncScroll() {
	total := m.totalEntries()
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
	if vis < 1 {
		vis = 1
	}
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

func (m *CommitPickerModal) selectIndex(idx int) {
	total := m.totalEntries()
	if total == 0 {
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= total {
		idx = total - 1
	}
	if idx != m.SelectedIndex {
		m.Notice = ""
	}
	m.SelectedIndex = idx
	m.syncScroll()
}

type pickerGeometry struct {
	innerW         int
	vPad           int
	hPad           int
	maxInnerH      int
	startIdx       int
	endIdx         int
	boxTopY        int
	boxBottomY     int
	boxLeftX       int
	boxRightX      int
	headerY        int
	innerLeftX     int
	listStartY     int
	compact        bool
	ultraCompact   bool
	emptyNoteLines int
}

func (m CommitPickerModal) listGeometry() pickerGeometry {
	innerW, vPad, hPad, maxInnerH := modalBoxMetrics(m.Width, m.Height, commitPickerPreferredWidth)
	compact, ultraCompact, headerLines, footerLines, emptyNoteLines := m.chromeMetrics()
	total := m.totalEntries()
	vis := m.visibleRows()

	scrollOffset := m.ScrollOffset
	if total > 0 && vis > 0 {
		if m.SelectedIndex < scrollOffset {
			scrollOffset = m.SelectedIndex
		}
		if m.SelectedIndex >= scrollOffset+vis {
			scrollOffset = m.SelectedIndex - vis + 1
		}
		maxOff := total - vis
		if maxOff < 0 {
			maxOff = 0
		}
		if scrollOffset > maxOff {
			scrollOffset = maxOff
		}
		if scrollOffset < 0 {
			scrollOffset = 0
		}
	} else {
		scrollOffset = 0
	}

	startIdx := scrollOffset
	endIdx := scrollOffset + vis
	if endIdx > total {
		endIdx = total
	}

	aboveLines := 0
	if startIdx > 0 {
		aboveLines = 1
	}
	belowLines := 0
	if endIdx < total {
		belowLines = 1
	}

	rowLines := endIdx - startIdx
	if total == 0 {
		rowLines = 1 // empty state message row
	}

	contentLines := headerLines + aboveLines + rowLines + emptyNoteLines + belowLines + footerLines
	if maxInnerH > 0 && contentLines > maxInnerH {
		contentLines = maxInnerH
	}

	boxH := contentLines + 2 + vPad*2
	boxW := innerW + 2 + hPad*2

	// Match lipgloss.Place(..., lipgloss.Center, lipgloss.Center, ...) exact integer rounding.
	boxTopY := 0
	if m.Height > boxH {
		gapH := m.Height - boxH
		splitH := int(math.Round(float64(gapH) * 0.5))
		boxTopY = gapH - splitH
	}
	boxBottomY := boxTopY + boxH - 1

	boxLeftX := 0
	if m.Width > boxW {
		gapW := m.Width - boxW
		splitW := int(math.Round(float64(gapW) * 0.5))
		boxLeftX = gapW - splitW
	}
	boxRightX := boxLeftX + boxW - 1

	headerY := boxTopY + 1 + vPad
	innerLeftX := boxLeftX + 1 + hPad
	listStartY := headerY + headerLines + aboveLines

	return pickerGeometry{
		innerW:         innerW,
		vPad:           vPad,
		hPad:           hPad,
		maxInnerH:      maxInnerH,
		startIdx:       startIdx,
		endIdx:         endIdx,
		boxTopY:        boxTopY,
		boxBottomY:     boxBottomY,
		boxLeftX:       boxLeftX,
		boxRightX:      boxRightX,
		headerY:        headerY,
		innerLeftX:     innerLeftX,
		listStartY:     listStartY,
		compact:        compact,
		ultraCompact:   ultraCompact,
		emptyNoteLines: emptyNoteLines,
	}
}

func (m CommitPickerModal) headerBadgeLayout(innerW int) (titlePill, statusBadge string, badgeStartX, badgeEndX int) {
	count := len(m.SelectedSHAs)

	titleText := "◆ Select 2 Configurations to Compare"
	if innerW < 62 {
		if innerW >= 42 {
			titleText = "◆ Compare 2 Configs (A↔B)"
		} else {
			titleText = "◆ Compare (A↔B)"
		}
	}

	badgeText := fmt.Sprintf("%d/2 selected", count)
	badgeBg := ColorBadgeBg
	badgeFg := ColorMuted
	switch count {
	case 1:
		badgeBg = ColorWarningBg
		badgeFg = ColorOnWarning
		if innerW < 42 {
			badgeText = "1/2"
		}
	case 2:
		badgeBg = ColorSuccessBg
		badgeFg = ColorOnAccent
		if innerW >= 54 {
			badgeText = "✓ 2/2 · Enter"
		} else {
			badgeText = "✓ 2/2"
		}
	default:
		if innerW < 42 {
			badgeText = "0/2"
		}
	}

	titlePill = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		Render(titleText)

	statusBadge = lipgloss.NewStyle().
		Foreground(badgeFg).
		Background(badgeBg).
		Bold(true).
		Padding(0, 1).
		Render(badgeText)

	if ansi.PrintableRuneWidth(titlePill)+1+ansi.PrintableRuneWidth(statusBadge) > innerW {
		// Drop pill padding on ultra-narrow screens to keep both cues intact.
		titlePill = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorOnAccent).
			Background(ColorPrimary).
			Render(truncateDisplay(titleText, maxInt(8, innerW-ansi.PrintableRuneWidth(badgeText)-1)))
		statusBadge = lipgloss.NewStyle().
			Foreground(badgeFg).
			Background(badgeBg).
			Bold(true).
			Render(badgeText)
	}

	tw := ansi.PrintableRuneWidth(titlePill)
	bw := ansi.PrintableRuneWidth(statusBadge)
	gapW := innerW - tw - bw
	if gapW >= 1 {
		badgeStartX = tw + gapW
		badgeEndX = badgeStartX + bw
	} else {
		badgeStartX, badgeEndX = -1, -1
	}
	return titlePill, statusBadge, badgeStartX, badgeEndX
}

// Update processes keyboard and mouse events for the CommitPickerModal.
// Returns (updatedModel, leftSHA, rightSHA, confirmed, close).
// When confirmed is true, leftSHA is the older configuration (A) and rightSHA is the
// newer configuration (B, or LiveRunningSentinel).
func (m CommitPickerModal) Update(msg tea.Msg) (CommitPickerModal, string, string, bool, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.syncScroll()
		return m, "", "", false, false

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.selectIndex(m.SelectedIndex - 1)
			return m, "", "", false, false

		case "down", "j":
			m.selectIndex(m.SelectedIndex + 1)
			return m, "", "", false, false

		case "pgup", "ctrl+u":
			step := m.visibleRows()
			if step < 1 {
				step = 4
			}
			m.selectIndex(m.SelectedIndex - step)
			return m, "", "", false, false

		case "pgdown", "ctrl+d":
			step := m.visibleRows()
			if step < 1 {
				step = 4
			}
			m.selectIndex(m.SelectedIndex + step)
			return m, "", "", false, false

		case "home", "g":
			m.selectIndex(0)
			return m, "", "", false, false

		case "end", "G":
			if total := m.totalEntries(); total > 0 {
				m.selectIndex(total - 1)
			}
			return m, "", "", false, false

		case " ", "space":
			m.ToggleSelected()
			return m, "", "", false, false

		case "c", "x", "backspace", "delete":
			m.ClearSelections()
			return m, "", "", false, false

		case "enter":
			if m.totalEntries() == 0 {
				return m, "", "", false, true
			}
			if leftSHA, rightSHA := m.ResolvedEndpoints(); leftSHA != "" && rightSHA != "" {
				return m, leftSHA, rightSHA, true, true
			}
			m.Notice = fmt.Sprintf("Select 2 configurations before pressing Enter (%d/2 selected)", len(m.SelectedSHAs))
			return m, "", "", false, false

		case "esc", "q":
			return m, "", "", false, true
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			m.selectIndex(m.SelectedIndex - 1)
			return m, "", "", false, false

		case tea.MouseWheelDown:
			m.selectIndex(m.SelectedIndex + 1)
			return m, "", "", false, false

		case tea.MouseLeft:
			g := m.listGeometry()
			// Click outside the floating modal box dismisses the picker.
			if msg.X < g.boxLeftX || msg.X > g.boxRightX || msg.Y < g.boxTopY || msg.Y > g.boxBottomY {
				return m, "", "", false, true
			}
			// Click on the right header confirm pill ("✓ 2/2 · Enter") when 2 are selected confirms immediately.
			if msg.Y == g.headerY && len(m.SelectedSHAs) == 2 {
				_, _, bStart, bEnd := m.headerBadgeLayout(g.innerW)
				relX := msg.X - g.innerLeftX
				if bStart >= 0 && relX >= bStart && relX < bEnd {
					leftSHA, rightSHA := m.ResolvedEndpoints()
					return m, leftSHA, rightSHA, true, true
				}
			}
			// Click on a visible commit/live row moves cursor there and toggles selection.
			if m.totalEntries() > 0 {
				row := msg.Y - g.listStartY
				if row >= 0 && g.startIdx+row < g.endIdx {
					targetIdx := g.startIdx + row
					m.selectIndex(targetIdx)
					m.toggleIndex(targetIdx)
					return m, "", "", false, false
				}
			}
		}
	}

	return m, "", "", false, false
}

// View renders the centered, full-bleed CommitPickerModal.
func (m CommitPickerModal) View() string {
	g := m.listGeometry()
	innerW := g.innerW
	total := m.totalEntries()

	borderAccent := ColorActive
	if len(m.SelectedSHAs) == 2 {
		borderAccent = ColorSuccess
	}

	titlePill, statusBadge, badgeStartX, _ := m.headerBadgeLayout(innerW)
	var headerRaw string
	if badgeStartX >= 0 {
		gapW := badgeStartX - ansi.PrintableRuneWidth(titlePill)
		spacer := lipgloss.NewStyle().Background(ColorSurface).Render(strings.Repeat(" ", gapW))
		headerRaw = titlePill + spacer + statusBadge
	} else {
		headerRaw = titlePill
	}
	headerLine := PaintBarLine(headerRaw, innerW, ColorSurface, ColorText)

	helperLine := m.renderStatusStrip(innerW)

	var rows []string
	if g.startIdx > 0 {
		rows = append(rows, PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSurface).Render(
				fmt.Sprintf("  ↑ %d more above", g.startIdx),
			),
			innerW,
			ColorSurface,
			ColorMuted,
		))
	}

	if total == 0 {
		emptyMsg := truncateDisplay("No commits available to compare.", maxInt(8, innerW-2))
		rows = append(rows, PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSurface).Render("  "+emptyMsg),
			innerW,
			ColorSurface,
			ColorMuted,
		))
	} else {
		for idx := g.startIdx; idx < g.endIdx; idx++ {
			isCursor := idx == m.SelectedIndex
			if m.isLiveIndex(idx) {
				rows = append(rows, m.renderLiveRow(innerW, isCursor))
			} else if c, ci, ok := m.commitAtIndex(idx); ok {
				rows = append(rows, m.renderCommitRow(c, ci, innerW, isCursor))
			}
		}
		if g.emptyNoteLines > 0 {
			note := truncateDisplay("No historical commits recorded yet.", maxInt(8, innerW-2))
			rows = append(rows, PaintBarLine(
				lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorSurface).Render("  "+note),
				innerW,
				ColorSurface,
				ColorTextDim,
			))
		}
	}

	if g.endIdx < total {
		rows = append(rows, PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSurface).Render(
				fmt.Sprintf("  ↓ %d more below", total-g.endIdx),
			),
			innerW,
			ColorSurface,
			ColorMuted,
		))
	}

	footer := renderModalFooter(
		[]string{"Space Toggle", "c Clear All", "Enter Compare", "Esc Cancel"},
		"Space Pick · c Clear · Enter OK · Esc",
		innerW,
		false,
	)

	var sections []string
	sections = append(sections, headerLine)
	if !g.ultraCompact {
		sections = append(sections, helperLine)
		if !g.compact {
			sections = append(sections, "")
		}
	}
	sections = append(sections, strings.Join(rows, "\n"))
	if !g.compact {
		sections = append(sections, "")
	}
	sections = append(sections, footer)

	content := strings.Join(sections, "\n")
	return renderCenteredModal(content, m.Width, m.Height, innerW, g.vPad, g.hPad, g.maxInnerH, borderAccent)
}

func (m CommitPickerModal) renderStatusStrip(innerW int) string {
	if m.Notice != "" {
		noticeText := truncateDisplay("◆ "+m.Notice, innerW)
		return PaintBarLine(
			lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Background(ColorSurface).Render(noticeText),
			innerW,
			ColorSurface,
			ColorWarning,
		)
	}

	sp := lipgloss.NewStyle().Background(ColorSurface).Render(" ")
	switch len(m.SelectedSHAs) {
	case 2:
		leftLbl := m.shortLabelForSHA(m.SelectedSHAs[0])
		rightLbl := m.shortLabelForSHA(m.SelectedSHAs[1])
		bA := renderEndpointBadge("A", ColorPrimary)
		bB := renderEndpointBadge("B", ColorSuccessBg)
		valStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(ColorSurface)
		dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSurface)

		raw := bA + sp + valStyle.Render(leftLbl) + dimStyle.Render(" (older)  ↔  ") + bB + sp + valStyle.Render(rightLbl) + dimStyle.Render(" (newer)")
		if ansi.PrintableRuneWidth(raw) > innerW {
			raw = bA + sp + valStyle.Render(leftLbl) + dimStyle.Render(" ↔ ") + bB + sp + valStyle.Render(rightLbl)
		}
		if ansi.PrintableRuneWidth(raw) > innerW {
			plain := truncateDisplay(fmt.Sprintf("[A] %s ↔ [B] %s", leftLbl, rightLbl), innerW)
			raw = valStyle.Render(plain)
		}
		return PaintBarLine(raw, innerW, ColorSurface, ColorText)

	case 1:
		lbl := m.shortLabelForSHA(m.SelectedSHAs[0])
		msg := fmt.Sprintf("Selected: %s · press Space on 1 more (older auto-assigned to A)", lbl)
		if innerW < 64 {
			msg = fmt.Sprintf("Selected: %s · pick 1 more (older → A)", lbl)
		}
		return PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorWarning).Background(ColorSurface).Render(truncateDisplay(msg, innerW)),
			innerW,
			ColorSurface,
			ColorWarning,
		)

	default:
		msg := "Press Space on 2 configurations to compare (older auto-assigned to A)"
		if innerW < 64 {
			msg = "Space on 2 configs (older → A, newer → B)"
		}
		return PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSurface).Render(truncateDisplay(msg, innerW)),
			innerW,
			ColorSurface,
			ColorMuted,
		)
	}
}

func (m CommitPickerModal) selectionRole(sha string) (checked bool, role string) {
	if sha == "" {
		return false, ""
	}
	if len(m.SelectedSHAs) == 2 {
		if m.SelectedSHAs[0] == sha {
			return true, "A"
		}
		if m.SelectedSHAs[1] == sha {
			return true, "B"
		}
		return false, ""
	}
	if len(m.SelectedSHAs) == 1 && m.SelectedSHAs[0] == sha {
		return true, "1"
	}
	return false, ""
}

func renderCheckbox(checked bool, role string, bg lipgloss.Color) string {
	if !checked {
		return lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render("[ ]")
	}
	fg := ColorActive
	switch role {
	case "B":
		fg = ColorSuccess
	case "1":
		fg = ColorWarning
	}
	return lipgloss.NewStyle().Bold(true).Foreground(fg).Background(bg).Render("[✓]")
}

func (m CommitPickerModal) renderLiveRow(innerW int, isCursor bool) string {
	checked, role := m.selectionRole(LiveRunningSentinel)

	rowBg := ColorSurface
	if isCursor {
		rowBg = ColorHighlight
	} else if checked {
		rowBg = ColorSurfaceFocus
	}

	sp := lipgloss.NewStyle().Background(rowBg).Render(" ")
	accentBar := lipgloss.NewStyle().Background(rowBg).Render(" ")
	if isCursor {
		accentBar = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(rowBg).Render("▎")
	} else if checked {
		barFg := ColorActive
		if role == "B" {
			barFg = ColorSuccess
		} else if role == "1" {
			barFg = ColorWarning
		}
		accentBar = lipgloss.NewStyle().Foreground(barFg).Background(rowBg).Render("▎")
	}

	checkBox := renderCheckbox(checked, role, rowBg)

	badgeStyle := lipgloss.NewStyle().
		Bold(true).
		Background(ColorWarningBg).
		Foreground(ColorOnWarning)
	if innerW >= 38 {
		badgeStyle = badgeStyle.Padding(0, 1)
	}
	liveBadge := badgeStyle.Render("● RUNNING CONFIG")

	row := accentBar + checkBox + sp + liveBadge
	usedW := ansi.PrintableRuneWidth(row)

	if checked {
		var roleBadge string
		switch role {
		case "A":
			roleBadge = renderEndpointBadge("A", ColorPrimary)
		case "B":
			roleBadge = renderEndpointBadge("B", ColorSuccessBg)
		case "1":
			roleBadge = renderEndpointBadge("1/2", ColorWarningBg)
		}
		if roleBadge != "" {
			if bw := ansi.PrintableRuneWidth(roleBadge); usedW+1+bw <= innerW {
				row += sp + roleBadge
				usedW += 1 + bw
			}
		}
	}

	rem := innerW - usedW - 1
	if rem >= 4 {
		descStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(rowBg)
		if isCursor || checked {
			descStyle = lipgloss.NewStyle().Bold(isCursor).Foreground(ColorText).Background(rowBg)
		}
		row += sp + descStyle.Render(truncateDisplay("Live switch state", rem))
	}

	return PaintBarLine(row, innerW, rowBg, ColorText)
}

func (m CommitPickerModal) renderCommitRow(c models.TimelineCommit, commitIdx, innerW int, isCursor bool) string {
	sha := c.FullSHA
	if sha == "" {
		sha = c.CommitID
	}
	checked, role := m.selectionRole(sha)

	rowBg := ColorSurface
	if isCursor {
		rowBg = ColorHighlight
	} else if checked {
		rowBg = ColorSurfaceFocus
	}
	sp := lipgloss.NewStyle().Background(rowBg).Render(" ")

	accentBar := lipgloss.NewStyle().Background(rowBg).Render(" ")
	if isCursor {
		accentBar = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(rowBg).Render("▎")
	} else if checked {
		barFg := ColorActive
		if role == "B" {
			barFg = ColorSuccess
		} else if role == "1" {
			barFg = ColorWarning
		}
		accentBar = lipgloss.NewStyle().Foreground(barFg).Background(rowBg).Render("▎")
	}

	checkBox := renderCheckbox(checked, role, rowBg)

	isHead := c.IsCurrent || commitIdx == 0
	isRoot := c.IsStartup || (len(m.Commits) > 1 && commitIdx == len(m.Commits)-1)

	nodeGlyph := "○"
	nodeStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(rowBg)
	switch {
	case c.IsRestored:
		nodeGlyph = "◆"
		nodeStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Background(rowBg)
	case isHead:
		nodeGlyph = "●"
		nodeStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Background(rowBg)
	case isRoot:
		nodeGlyph = "◌"
		nodeStyle = lipgloss.NewStyle().Foreground(ColorMuted).Background(rowBg)
	}
	nodeStyled := nodeStyle.Render(nodeGlyph)

	shortID := strings.TrimSpace(c.CommitID)
	if shortID == "" {
		shortID = strings.TrimSpace(c.FullSHA)
	}
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	if shortID == "" {
		shortID = "--------"
	}
	shaStyled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(rowBg).Render(shortID)

	row := accentBar + checkBox + sp + nodeStyled + sp + shaStyled
	usedW := ansi.PrintableRuneWidth(row)

	// Live endpoint role badge ([A] older, [B] newer, or [1/2] pending 2nd pick).
	if checked {
		var roleBadge string
		switch role {
		case "A":
			roleBadge = renderEndpointBadge("A", ColorPrimary)
		case "B":
			roleBadge = renderEndpointBadge("B", ColorSuccessBg)
		case "1":
			roleBadge = renderEndpointBadge("1/2", ColorWarningBg)
		}
		if roleBadge != "" {
			if bw := ansi.PrintableRuneWidth(roleBadge); usedW+1+bw <= innerW {
				row += sp + roleBadge
				usedW += 1 + bw
			}
		}
	}

	// Reserve minimum subtitle columns on narrow viewports so commit descriptions remain readable.
	minSubReserve := 14
	if innerW < 46 {
		minSubReserve = 10
	}

	// Author (included when comfortable space remains for subtitle).
	authorName := strings.TrimSpace(c.Author)
	if authorName == "" {
		authorName = "unknown"
	}
	authorName = truncateDisplay(authorName, 10)
	authorStyle := lipgloss.NewStyle().Foreground(ColorPurple).Background(rowBg)
	if authorName == "admin" || authorName == "root" {
		authorStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Background(rowBg)
	}
	authorStyled := authorStyle.Render(authorName)
	if innerW >= 50 {
		if aw := ansi.PrintableRuneWidth(authorStyled); usedW+1+aw+minSubReserve <= innerW {
			row += sp + authorStyled
			usedW += 1 + aw
		}
	}

	// Compact relative time.
	if !c.Timestamp.IsZero() {
		rel := pickerCompactRelativeTime(c.RelativeTime())
		if rel != "" {
			timeStyled := lipgloss.NewStyle().Foreground(ColorTextDim).Background(rowBg).Render(rel)
			if tw := ansi.PrintableRuneWidth(timeStyled); usedW+1+tw+minSubReserve <= innerW {
				row += sp + timeStyled
				usedW += 1 + tw
			}
		}
	}

	// Color-coded DiffStat tokens (+N / ~N / -N).
	if statStyled := pickerFormatDiffStat(c.DiffStat, rowBg); statStyled != "" && innerW >= 56 {
		if sw := ansi.PrintableRuneWidth(statStyled); usedW+1+sw+minSubReserve <= innerW {
			row += sp + statStyled
			usedW += 1 + sw
		}
	}

	// Truncated subtitle fills remaining columns.
	rem := innerW - usedW - 1
	if rem >= 4 {
		subText := truncateDisplay(cleanCommitSummary(c), rem)
		subStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(rowBg)
		if isCursor {
			subStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(rowBg)
		} else if checked {
			subStyle = lipgloss.NewStyle().Foreground(ColorText).Background(rowBg)
		}
		row += sp + subStyle.Render(subText)
	}

	return PaintBarLine(row, innerW, rowBg, ColorText)
}

func renderEndpointBadge(label string, bg lipgloss.Color) string {
	fg := ColorOnAccent
	if bg == ColorWarningBg {
		fg = ColorOnWarning
	}
	return lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(fg).
		Render("[" + label + "]")
}

func pickerCompactRelativeTime(rel string) string {
	s := strings.TrimSpace(rel)
	if s == "just now" {
		return "now"
	}
	return strings.TrimSuffix(s, " ago")
}

func pickerFormatDiffStat(diffStat string, bg lipgloss.Color) string {
	s := strings.TrimSpace(diffStat)
	if s == "" || s == "-" || strings.EqualFold(s, "no changes") || strings.EqualFold(s, "identical") {
		return ""
	}
	cleaned := strings.ReplaceAll(s, "/", " ")
	fields := strings.Fields(cleaned)
	if len(fields) == 0 {
		return ""
	}
	sp := lipgloss.NewStyle().Background(bg).Render(" ")
	styled := make([]string, 0, len(fields))
	for _, tok := range fields {
		switch {
		case strings.HasPrefix(tok, "+"):
			styled = append(styled, lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Background(bg).Render(tok))
		case strings.HasPrefix(tok, "~"):
			styled = append(styled, lipgloss.NewStyle().Foreground(ColorWarning).Background(bg).Render(tok))
		case strings.HasPrefix(tok, "-"):
			styled = append(styled, lipgloss.NewStyle().Foreground(ColorDanger).Background(bg).Render(tok))
		default:
			styled = append(styled, lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(tok))
		}
	}
	return strings.Join(styled, sp)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
