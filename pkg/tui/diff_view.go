package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"timeline/pkg/differ"
	"timeline/pkg/filter"
	"timeline/pkg/models"
)

// SplitMinOuterWidth is the minimum total terminal width required to enable side-by-side split diff.
const SplitMinOuterWidth = 100

// SplitMinDetailInnerWidth is the minimum inner width of the DiffView pane required to
// render side-by-side split columns cleanly (allowing the left Commit List panel to remain
// visible alongside Split Diff at >=100 terminal columns).
const SplitMinDetailInnerWidth = 66

// DiffViewModel handles the syntax-highlighted semantic diff viewport.
type DiffViewModel struct {
	DiffResult             models.SemanticDiffResult
	DiffMode               string // "unified", "path", "cli"
	VsLive                 bool
	Zoomed                 bool
	SplitMode              bool // default false — single-pane by default
	FoldUnchanged          bool // default true — set true in NewDiffViewModel
	SideBySideResult       differ.SideBySideDiffResult
	LeftCommit             *models.TimelineCommit // older endpoint A
	RightCommit            *models.TimelineCommit // newer endpoint B; nil when VsLive is true
	Commit                 *models.TimelineCommit
	ParentSHA              string
	BaseCommit             *models.TimelineCommit
	FilterQuery            string
	DetailFilterBypass     bool
	ShownChangeCount       int
	TotalUnfilteredChanges int
	Viewport               viewport.Model
	Width                  int
	Height                 int
	IsFocused              bool
	renderedLines          []string

	emptyMsg string
}

func (m *DiffViewModel) setViewportLines(lines []string) {
	m.renderedLines = lines
	m.Viewport.SetContent(strings.Join(lines, "\n"))
}

// NewDiffViewModel creates a new diff viewer (defaulting to CLI diff mode).
func NewDiffViewModel(width, height int) DiffViewModel {
	m := DiffViewModel{
		DiffMode:      "cli",
		FoldUnchanged: true,
		Width:         width,
		Height:        height,
		IsFocused:     false,
	}
	m.Viewport = viewport.New(m.viewportWidth(), m.viewportHeight())
	return m
}

// IsSplitActive returns true when side-by-side split mode is enabled and the pane is wide enough.
func (m DiffViewModel) IsSplitActive() bool {
	return m.SplitMode && m.Width >= SplitMinDetailInnerWidth
}

func (m DiffViewModel) hasContextStrip() bool {
	return m.Height >= 16 && (m.Commit != nil || m.BaseCommit != nil)
}

func (m DiffViewModel) hasFilterBanner() bool {
	return strings.TrimSpace(m.FilterQuery) != "" && m.Height >= 10
}

// HeaderRows returns the number of inner rows above the scrollable viewport.
func (m DiffViewModel) HeaderRows() int {
	var rows int
	if m.IsSplitActive() {
		rows = 3 // toolbar + dual A/B header strip + thin ───┼─── divider
	} else {
		rows = 1 // toolbar
		if m.hasContextStrip() {
			rows++
		}
	}
	if m.hasFilterBanner() {
		rows++
	}
	return rows
}

// FilterBannerRow returns the 0-based inner row index of the filter scope banner,
// or -1 when no filter banner is displayed.
func (m DiffViewModel) FilterBannerRow() int {
	if !m.hasFilterBanner() {
		return -1
	}
	if m.IsSplitActive() {
		return 3
	}
	if m.hasContextStrip() {
		return 2
	}
	return 1
}

func (m DiffViewModel) effectiveShownChangeCount() int {
	if m.ShownChangeCount > 0 {
		return m.ShownChangeCount
	}
	if m.IsSplitActive() {
		sum := m.SideBySideResult.AddedCount + m.SideBySideResult.ModifiedCount + m.SideBySideResult.DeletedCount
		if sum > 0 {
			return sum
		}
		cnt := 0
		for _, r := range m.SideBySideResult.Rows {
			if r.Kind == differ.SideBySideAdded || r.Kind == differ.SideBySideDeleted || r.Kind == differ.SideBySideModified {
				cnt++
			}
		}
		return cnt
	}
	sum := m.DiffResult.AddedCount + m.DiffResult.ModifiedCount + m.DiffResult.DeletedCount
	if sum > 0 {
		return sum
	}
	if len(m.DiffResult.Changes) > 0 {
		return len(filterRedundantPathChanges(m.DiffResult.Changes))
	}
	return len(m.DiffResult.CLIDiffLines)
}

func (m DiffViewModel) buildFilterBanner(innerW int) (string, int, int) {
	if !m.hasFilterBanner() {
		return "", -1, -1
	}
	shown := m.effectiveShownChangeCount()
	total := m.TotalUnfilteredChanges
	if total < shown {
		total = shown
	}
	return buildDetailFilterBanner(m.FilterQuery, m.DetailFilterBypass, shown, total, "changes", "Full Commit", innerW)
}

// FilterBannerHitRange returns the clickable [startX, endX) range of the Full Commit / Re-scope
// toggle button on FilterBannerRow(), relative to inner pane X=0. Returns (-1, -1) if not shown.
func (m DiffViewModel) FilterBannerHitRange() (startX, endX int) {
	if !m.hasFilterBanner() {
		return -1, -1
	}
	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	_, startX, endX = m.buildFilterBanner(innerW)
	return startX, endX
}

func (m DiffViewModel) viewportWidth() int {
	if m.Width > 4 {
		return m.Width - 1
	}
	if m.Width < 1 {
		return 1
	}
	return m.Width
}

func (m DiffViewModel) viewportHeight() int {
	h := m.Height - m.HeaderRows()
	if h < 1 {
		h = 1
	}
	return h
}

// SetDiff updates the diff result and rebuilds viewport lines.
func (m *DiffViewModel) SetDiff(res models.SemanticDiffResult) {
	m.emptyMsg = ""
	m.DiffResult = res
	m.UpdateContent()
}

// SetSideBySideDiff updates the side-by-side diff result and rebuilds viewport content.
func (m *DiffViewModel) SetSideBySideDiff(res differ.SideBySideDiffResult) {
	m.emptyMsg = ""
	m.SideBySideResult = res
	m.UpdateContent()
}

// ResetScroll resets the viewport scroll position to the top.
func (m *DiffViewModel) ResetScroll() {
	m.Viewport.SetYOffset(0)
}

// Clear resets the diff view to an empty state with the given message.
func (m *DiffViewModel) Clear(message string) {
	m.DiffResult = models.SemanticDiffResult{}
	m.SideBySideResult = differ.SideBySideDiffResult{}
	m.LeftCommit = nil
	m.RightCommit = nil
	m.Commit = nil
	m.ParentSHA = ""
	m.BaseCommit = nil
	m.FilterQuery = ""
	m.DetailFilterBypass = false
	m.ShownChangeCount = 0
	m.TotalUnfilteredChanges = 0
	m.emptyMsg = strings.TrimSpace(message)
	m.Viewport.SetYOffset(0)
	m.UpdateContent()
}

// SetSize handles terminal resizing without needlessly re-wrapping content when width is unchanged.
func (m *DiffViewModel) SetSize(width, height int) {
	if m.Width == width && m.Height == height {
		return
	}
	prevW := m.Width
	prevSplit := m.IsSplitActive()
	prevHasCtx := m.hasContextStrip()
	prevHasBanner := m.hasFilterBanner()

	m.Width = width
	m.Height = height
	m.Viewport.Width = m.viewportWidth()
	m.Viewport.Height = m.viewportHeight()

	if prevW == width && prevSplit == m.IsSplitActive() && prevHasCtx == m.hasContextStrip() && prevHasBanner == m.hasFilterBanner() && m.Viewport.TotalLineCount() > 0 {
		return
	}
	m.UpdateContent()
}

// UpdateContent re-renders the diff content with Lip Gloss syntax colors and line numbers.
func (m *DiffViewModel) UpdateContent() {
	vpW := m.viewportWidth()
	vpH := m.viewportHeight()
	m.Viewport.Width = vpW
	m.Viewport.Height = vpH

	wrapW := vpW
	if wrapW < 4 {
		wrapW = 4
	}

	if m.emptyMsg != "" {
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  " + m.emptyMsg)
		m.setViewportLines([]string{
			PaintLine("", wrapW, ColorPanel, ColorMuted),
			PaintLine(msg, wrapW, ColorPanel, ColorMuted),
		})
		m.Viewport.SetYOffset(0)
		return
	}

	if m.IsSplitActive() {
		m.updateSplitContent(wrapW)
		return
	}

	if !m.DiffResult.HasChanges {
		var emptyText string
		commitID := ""
		if m.Commit != nil {
			commitID = strings.TrimSpace(m.Commit.CommitID)
		}
		fq := strings.TrimSpace(m.FilterQuery)
		switch {
		case m.VsLive:
			if commitID != "" {
				emptyText = fmt.Sprintf("  %s Revision %s matches the live running configuration", GlyphCheck, commitID)
			} else {
				emptyText = fmt.Sprintf("  %s Matches the live running configuration", GlyphCheck)
			}
		case m.BaseCommit != nil:
			baseID := strings.TrimSpace(m.BaseCommit.CommitID)
			if commitID != "" {
				emptyText = fmt.Sprintf("  %s No differences between BASE %s and %s", GlyphCheck, baseID, commitID)
			} else {
				emptyText = fmt.Sprintf("  %s No differences vs BASE %s", GlyphCheck, baseID)
			}
		default:
			emptyText = fmt.Sprintf("  %s No configuration changes vs previous commit", GlyphCheck)
		}
		if fq != "" && !m.DetailFilterBypass {
			emptyText += fmt.Sprintf(" (filter: '%s')", fq)
		}
		msg := lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true).
			Render(emptyText)
		m.setViewportLines([]string{
			PaintLine("", wrapW, ColorPanel, ColorText),
			PaintLine(msg, wrapW, ColorPanel, ColorSuccess),
		})
		m.Viewport.SetYOffset(0)
		return
	}

	highlightQuery := ""
	if m.DetailFilterBypass {
		highlightQuery = strings.TrimSpace(m.FilterQuery)
	}

	var outLines []string

	switch m.DiffMode {
	case "unified":
		if len(m.DiffResult.UnifiedDiffLines) <= 2 {
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render("  No JSON differences found for this filter. Check [2] Path or [3] CLI diff.")
			outLines = append(outLines, PaintLine(warn, wrapW, ColorPanel, ColorWarning))
		} else {
			styleHeader := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted)
			styleCtx := lipgloss.NewStyle().Foreground(ColorMuted)
			digits := gutterDigitsForCount(len(m.DiffResult.UnifiedDiffLines))

			for idx, l := range m.DiffResult.UnifiedDiffLines {
				lineNo := idx + 1
				var styled string
				var bg, fg lipgloss.Color
				if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
					styled = styleHeader.Background(ColorPanel).Render(l)
					bg, fg = ColorPanel, ColorMuted
				} else if strings.HasPrefix(l, "@@") {
					styled = StyleDiffHunkLine.Render(l)
					bg, fg = ColorDiffHunkBg, ColorActive
				} else if strings.HasPrefix(l, "+") {
					styled = StyleDiffAddLine.Render(l)
					bg, fg = ColorDiffAddBg, ColorSuccess
				} else if strings.HasPrefix(l, "-") {
					styled = StyleDiffDelLine.Render(l)
					bg, fg = ColorDiffDelBg, ColorDanger
				} else {
					styled = styleCtx.Background(ColorPanel).Render(l)
					bg, fg = ColorPanel, ColorMuted
				}
				matched := highlightQuery != "" && filter.LineMatchesQuery(l, highlightQuery)
				outLines = append(outLines, formatGutteredWrappedLinesMarked(lineNo, digits, styled, wrapW, bg, fg, matched)...)
			}
		}

	case "path":
		filtered := filterRedundantPathChanges(m.DiffResult.Changes)
		if len(filtered) == 0 {
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render("  No structured path changes found for this filter.")
			outLines = append(outLines, PaintLine(warn, wrapW, ColorPanel, ColorWarning))
		} else {
			digits := gutterDigitsForCount(len(filtered))
			for idx, c := range filtered {
				matched := highlightQuery != "" && pathChangeMatchesQuery(c, highlightQuery)
				outLines = append(outLines, formatPathChangeLinesMarked(idx+1, digits, c, wrapW, matched)...)
			}
		}

	default: // "cli"
		if len(m.DiffResult.CLIDiffLines) == 0 {
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render("  No CLI diff statements found for this filter.")
			outLines = append(outLines, PaintLine(warn, wrapW, ColorPanel, ColorWarning))
		} else {
			digits := gutterDigitsForCount(len(m.DiffResult.CLIDiffLines))
			for idx, l := range m.DiffResult.CLIDiffLines {
				lineNo := idx + 1
				bg := ColorPanel
				fg := ColorText
				if strings.HasPrefix(l, "+") {
					bg = ColorDiffAddBg
					fg = ColorSuccess
				} else if strings.HasPrefix(l, "-") {
					bg = ColorDiffDelBg
					fg = ColorDanger
				}
				styled := highlightSRLinuxCLI(l, bg)
				matched := highlightQuery != "" && filter.LineMatchesQuery(l, highlightQuery)
				outLines = append(outLines, formatGutteredWrappedLinesMarked(lineNo, digits, styled, wrapW, bg, fg, matched)...)
			}
		}
	}

	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorMuted),
		PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Background(ColorPanel).Render("─── End of Diff ───"), wrapW, ColorPanel, ColorMuted),
	)

	m.setViewportLines(outLines)
	m.Viewport.SetYOffset(m.Viewport.YOffset)
}

func isContainerValue(v interface{}) bool {
	if v == nil {
		return false
	}
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return true
	default:
		return false
	}
}

// filterRedundantPathChanges removes parent container PathChange entries when child leaf
// changes under c.Path+"/" are already present in changes.
func filterRedundantPathChanges(changes []models.PathChange) []models.PathChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]models.PathChange, 0, len(changes))
	for i, c := range changes {
		if isContainerValue(c.OldValue) || isContainerValue(c.NewValue) {
			prefix := c.Path + "/"
			hasChild := false
			for j, other := range changes {
				if i != j && strings.HasPrefix(other.Path, prefix) {
					hasChild = true
					break
				}
			}
			if hasChild {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func formatScalarValue(v interface{}, maxRunes int) string {
	if v == nil {
		return ""
	}
	var raw string
	switch val := v.(type) {
	case string:
		raw = val
	case bool:
		raw = strconv.FormatBool(val)
	case float64:
		raw = strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		raw = strconv.Itoa(val)
	case int64:
		raw = strconv.FormatInt(val, 10)
	case map[string]interface{}:
		if len(val) == 0 {
			return "{}"
		}
		return fmt.Sprintf("{%d keys}", len(val))
	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}
		// Render short leaf-lists inline if scalar
		parts := make([]string, 0, len(val))
		allScalar := true
		for _, item := range val {
			if isContainerValue(item) {
				allScalar = false
				break
			}
			parts = append(parts, formatScalarValue(item, 24))
		}
		if allScalar {
			raw = "[" + strings.Join(parts, ", ") + "]"
		} else {
			return fmt.Sprintf("[%d items]", len(val))
		}
	default:
		raw = fmt.Sprintf("%v", v)
	}

	raw = strings.ReplaceAll(raw, "\r\n", " ")
	raw = strings.ReplaceAll(raw, "\n", " ")
	raw = strings.ReplaceAll(raw, "\r", " ")
	raw = strings.TrimSpace(raw)

	runes := []rune(raw)
	if maxRunes > 1 && len(runes) > maxRunes {
		return string(runes[:maxRunes-1]) + "…"
	}
	return raw
}

func formatPathChangeValue(c models.PathChange, bg lipgloss.Color) string {
	const maxValRunes = 60
	switch c.DiffType {
	case models.DiffModified:
		oldStr := formatScalarValue(c.OldValue, maxValRunes)
		newStr := formatScalarValue(c.NewValue, maxValRunes)
		if oldStr == "" && newStr == "" {
			return ""
		}
		oldStyled := lipgloss.NewStyle().Foreground(ColorDanger).Background(bg).Render(oldStr)
		arrow := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" → ")
		newStyled := lipgloss.NewStyle().Foreground(ColorSuccess).Background(bg).Render(newStr)
		return oldStyled + arrow + newStyled
	case models.DiffAdded:
		if isContainerValue(c.NewValue) {
			return ""
		}
		newStr := formatScalarValue(c.NewValue, maxValRunes)
		if newStr == "" {
			return ""
		}
		return lipgloss.NewStyle().Foreground(ColorSuccess).Background(bg).Render("+ ") +
			lipgloss.NewStyle().Foreground(ColorSyntaxValue).Background(bg).Render(newStr)
	case models.DiffDeleted:
		if isContainerValue(c.OldValue) {
			return ""
		}
		oldStr := formatScalarValue(c.OldValue, maxValRunes)
		if oldStr == "" {
			return ""
		}
		return lipgloss.NewStyle().Foreground(ColorDanger).Background(bg).Render("- " + oldStr)
	default:
		return ""
	}
}

type toolbarPillSpec struct {
	label           string
	text            string
	active          bool
	warning         bool
	isToggle        bool
	separatorBefore bool
	padH            int
	fixedPad        bool
}

const wideToolbarThreshold = 62
const compactToolbarThreshold = 54
const tightToolbarPadThreshold = 40

func buildToolbarLine(specs []toolbarPillSpec, focused bool, rightStatus, rightFallback string, innerW int) (string, []HitChip) {
	if innerW < 1 {
		innerW = 1
	}

	var b strings.Builder
	sp := lipgloss.NewStyle().Background(ColorSurface).Render(" ")
	divSep := lipgloss.NewStyle().Background(ColorSurface).Foreground(ColorBorder).Render(" " + GlyphPipe + " ")
	divSepW := lipgloss.Width(divSep)
	b.WriteString(sp)
	cursorX := 1

	chips := make([]HitChip, 0, len(specs))
	for i, spec := range specs {
		if i > 0 {
			if spec.separatorBefore {
				b.WriteString(divSep)
				cursorX += divSepW
			} else {
				b.WriteString(sp)
				cursorX++
			}
		}
		padH := spec.padH
		if padH == 0 && !spec.fixedPad {
			padH = 1
		}
		st := lipgloss.NewStyle().Padding(0, padH)
		switch {
		case spec.warning && spec.active:
			st = st.Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true)
		case spec.isToggle && !spec.active:
			if focused {
				st = st.Background(ColorSurface).Foreground(ColorMuted)
			} else {
				st = st.Background(ColorSurface).Foreground(ColorTextDim)
			}
		case spec.active && focused:
			st = st.Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true)
		case spec.active && !focused:
			st = st.Background(ColorBadgeBg).Foreground(ColorText).Bold(true).Underline(true)
		case !focused:
			st = st.Background(ColorSurface).Foreground(ColorTextDim)
		default:
			st = st.Background(ColorBadgeBg).Foreground(ColorMuted)
		}

		rendered := st.Render(spec.text)
		w := lipgloss.Width(rendered)
		if cursorX < innerW {
			endX := cursorX + w
			if endX > innerW {
				endX = innerW
			}
			chips = append(chips, HitChip{
				Label:  spec.label,
				StartX: cursorX,
				EndX:   endX,
			})
		}
		b.WriteString(rendered)
		cursorX += w
	}

	chosenRight := ""
	if rightStatus != "" {
		if gap := innerW - cursorX - lipgloss.Width(rightStatus) - 1; gap >= 1 {
			chosenRight = rightStatus
		}
	}
	if chosenRight == "" && rightFallback != "" {
		if gap := innerW - cursorX - lipgloss.Width(rightFallback) - 1; gap >= 1 {
			chosenRight = rightFallback
		}
	}
	if chosenRight != "" {
		gap := innerW - cursorX - lipgloss.Width(chosenRight) - 1
		b.WriteString(lipgloss.NewStyle().Background(ColorSurface).Render(strings.Repeat(" ", gap)))
		b.WriteString(chosenRight)
	}

	return PaintBarLine(b.String(), innerW, ColorSurface, ColorText), chips
}

func (m DiffViewModel) buildToolbar(innerW int) (string, []HitChip) {
	compact := innerW < compactToolbarThreshold
	toggleGlyph := GlyphPaused // ○ when off
	if m.VsLive {
		toggleGlyph = GlyphLive // ● when on
	}
	splitGlyph := GlyphPaused // ○ when off
	if m.SplitMode {
		splitGlyph = GlyphLive // ● when on
	}

	var specs []toolbarPillSpec
	switch {
	case compact:
		padH := 1
		if innerW < tightToolbarPadThreshold {
			padH = 0
		}
		liveText := "4:" + toggleGlyph + "Run"
		specs = []toolbarPillSpec{
			{label: "unified", text: "1:JSON", active: m.DiffMode == "unified", padH: padH, fixedPad: true},
			{label: "path", text: "2:Path", active: m.DiffMode == "path", padH: padH, fixedPad: true},
			{label: "cli", text: "3:CLI", active: m.DiffMode == "cli" || m.DiffMode == "", padH: padH, fixedPad: true},
			{label: "live", text: liveText, active: m.VsLive, warning: true, isToggle: true, separatorBefore: true, padH: padH, fixedPad: true},
		}
	case innerW < 72:
		// Medium tier (e.g. 54..71): keep full format labels + compact S:Split + [4] ○ Run with scroll room
		liveText := "[4] " + toggleGlyph + " Run"
		specs = []toolbarPillSpec{
			{label: "unified", text: "[1] JSON", active: m.DiffMode == "unified", padH: 0, fixedPad: true},
			{label: "path", text: "[2] Path", active: m.DiffMode == "path", padH: 0, fixedPad: true},
			{label: "cli", text: "[3] CLI", active: m.DiffMode == "cli" || m.DiffMode == "", padH: 0, fixedPad: true},
			{label: "split", text: "S:Split", active: m.SplitMode, isToggle: true, separatorBefore: true, padH: 0, fixedPad: true},
			{label: "live", text: liveText, active: m.VsLive, warning: true, isToggle: true, padH: 0, fixedPad: true},
		}
	default:
		splitText := "[S] " + splitGlyph + " Split"
		splitPad := 1
		if innerW < 84 {
			splitPad = 0
		}
		liveText := "[4] " + toggleGlyph + " vs Running"
		specs = []toolbarPillSpec{
			{label: "unified", text: "[1] JSON", active: m.DiffMode == "unified", padH: 1},
			{label: "path", text: "[2] Path", active: m.DiffMode == "path", padH: 1},
			{label: "cli", text: "[3] CLI", active: m.DiffMode == "cli" || m.DiffMode == "", padH: 1},
			{label: "split", text: splitText, active: m.SplitMode, isToggle: true, separatorBefore: true, padH: splitPad, fixedPad: true},
		}
		if m.IsSplitActive() {
			foldText := "[E] Folded"
			if !m.FoldUnchanged {
				foldText = "[E] Expanded"
			}
			specs = append(specs, toolbarPillSpec{
				label:    "fold",
				text:     foldText,
				active:   !m.FoldUnchanged,
				isToggle: true,
				padH:     1,
				fixedPad: true,
			})
		}
		specs = append(specs, toolbarPillSpec{
			label:           "live",
			text:            liveText,
			active:          m.VsLive,
			warning:         true,
			isToggle:        true,
			separatorBefore: true,
			padH:            1,
			fixedPad:        true,
		})
	}

	scrollPrimary, scrollFallback := renderToolbarScrollStatuses(m.Viewport, compact)
	return buildToolbarLine(specs, m.IsFocused, scrollPrimary, scrollFallback, innerW)
}

// ControlHitRanges returns clickable toolbar pill hit ranges relative to the inner pane X=0.
func (m DiffViewModel) ControlHitRanges() []HitChip {
	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	_, chips := m.buildToolbar(innerW)
	return chips
}

// SplitEndpointHitRanges returns clickable HitChip ranges ("pick_left" and "pick_right")
// relative to the inner pane X=0 on the split column header row. Returns nil when !IsSplitActive().
func (m DiffViewModel) SplitEndpointHitRanges() []HitChip {
	if !m.IsSplitActive() {
		return nil
	}
	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	_, chips := m.buildSplitHeaderStrip(innerW)
	return chips
}

// View renders the diff viewer inside the shared border-titled detail box.
func (m DiffViewModel) View() string {
	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	innerH := m.Height
	if innerH < 1 {
		innerH = 1
	}

	controlsBar, _ := m.buildToolbar(innerW)
	var sections []string
	sections = append(sections, controlsBar)

	if m.IsSplitActive() {
		splitHeader, _ := m.buildSplitHeaderStrip(innerW)
		leftW, _ := splitColumnWidths(m.viewportWidth())
		sections = append(sections, splitHeader, renderSplitDividerRow(leftW, innerW))
	} else if m.hasContextStrip() {
		sections = append(sections, renderDiffContextStrip(m.Commit, m.BaseCommit, m.ParentSHA, m.VsLive, &m.DiffResult, innerW))
	}
	if m.hasFilterBanner() {
		bannerLine, _, _ := m.buildFilterBanner(innerW)
		sections = append(sections, bannerLine)
	}

	vpH := innerH - len(sections)
	if vpH < 1 {
		vpH = 1
	}
	vpView := renderLinesWithScrollbar(m.renderedLines, m.Viewport, innerW, vpH, ColorPanel)
	sections = append(sections, vpView)

	body := strings.Join(sections, "\n")
	return renderDetailTitledBoxWithSplit(int(TabDiff), m.IsFocused, m.Zoomed, m.IsSplitActive(), body, innerW, innerH)
}

// ============================================================================
// Side-by-Side Split-Pane Diff Helpers
// ============================================================================

func splitColumnWidths(contentW int) (leftW, rightW int) {
	if contentW < 3 {
		return 1, 1
	}
	avail := contentW - 1 // 1 column reserved for center │ divider
	leftW = avail / 2
	rightW = avail - leftW
	return leftW, rightW
}

func splitGutterDigits(maxLine int) int {
	if maxLine < 100 {
		return 3
	}
	return len(strconv.Itoa(maxLine))
}

func formatSplitEndpointLabel(commit *models.TimelineCommit, fallbackSHA, defaultLabel string) string {
	if commit != nil {
		id := strings.TrimSpace(commit.CommitID)
		if id == "" && len(commit.FullSHA) >= 8 {
			id = commit.FullSHA[:8]
		}
		if id == "" {
			id = defaultLabel
		}
		if !commit.Timestamp.IsZero() {
			rel := compactRelativeTime(commit.RelativeTime())
			if rel != "" {
				return id + " · " + rel
			}
			return id + " · " + commit.Timestamp.Format("15:04")
		}
		return id
	}
	if sha := strings.TrimSpace(fallbackSHA); sha != "" {
		if len(sha) > 8 {
			sha = sha[:8]
		}
		return sha
	}
	return defaultLabel
}

func formatSideBySideStatTokens(res differ.SideBySideDiffResult, bg lipgloss.Color) string {
	var parts []string
	if res.AddedCount > 0 {
		parts = append(parts, fmt.Sprintf("+%d", res.AddedCount))
	}
	if res.ModifiedCount > 0 {
		parts = append(parts, fmt.Sprintf("~%d", res.ModifiedCount))
	}
	if res.DeletedCount > 0 {
		parts = append(parts, fmt.Sprintf("-%d", res.DeletedCount))
	}
	if len(parts) > 0 {
		return formatDiffStatTokens(strings.Join(parts, " "), bg)
	}
	if !res.HasChanges {
		return lipgloss.NewStyle().Foreground(ColorSuccess).Background(bg).Render(GlyphCheck + " Identical")
	}
	return ""
}

func (m DiffViewModel) buildSplitHeaderStrip(innerW int) (string, []HitChip) {
	if innerW < 3 {
		return PaintBarLine("", innerW, ColorBackground, ColorText), nil
	}

	leftW, _ := splitColumnWidths(m.viewportWidth())
	if leftW < 1 {
		leftW = 1
	}
	if leftW >= innerW-1 {
		leftW = innerW / 2
	}
	rightHeaderW := innerW - leftW - 1
	if rightHeaderW < 1 {
		rightHeaderW = 1
	}

	bg := ColorBackground
	sp := lipgloss.NewStyle().Background(bg).Render(" ")
	chips := make([]HitChip, 0, 2)

	// --- Left half: A (OLDER) + [<sha> · <time> ▾] ---
	leftBadge := lipgloss.NewStyle().
		Background(ColorPrimary).
		Foreground(ColorOnAccent).
		Bold(true).
		Render(" A (OLDER) ")
	leftBadgeW := lipgloss.Width(leftBadge)

	leftCommit := m.LeftCommit
	if leftCommit == nil {
		leftCommit = m.BaseCommit
	}
	leftLabel := formatSplitEndpointLabel(leftCommit, m.ParentSHA, "Initial")
	leftPillStartX := 1 + leftBadgeW + 1
	availLeftPillW := leftW - leftPillStartX - 1
	if availLeftPillW < 6 {
		availLeftPillW = 6
	}
	// Format: "[<label> ▾]" => 4 cols overhead ("[", " ", "▾", "]")
	leftLabelMax := availLeftPillW - 4
	if leftLabelMax < 2 {
		leftLabelMax = 2
	}
	leftLabelTrim := truncateStringEllipsis(leftLabel, leftLabelMax)
	leftPill := lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorActive).
		Bold(true).
		Render("[" + leftLabelTrim + " " + GlyphExpanded + "]")
	leftPillW := lipgloss.Width(leftPill)

	if leftPillStartX < leftW {
		endX := leftPillStartX + leftPillW
		if endX > leftW {
			endX = leftW
		}
		chips = append(chips, HitChip{
			Label:  "pick_left",
			StartX: leftPillStartX,
			EndX:   endX,
		})
	}

	leftRaw := sp + leftBadge + sp + leftPill
	if leftCommit != nil && leftCommit.Author != "" {
		used := leftPillStartX + leftPillW
		sep := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" · ")
		authorSeg := lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(leftCommit.Author)
		if used+lipgloss.Width(sep)+lipgloss.Width(authorSeg)+1 <= leftW {
			leftRaw += sep + authorSeg
		}
	}
	leftPainted := PaintBarLine(leftRaw, leftW, bg, ColorText)

	// --- Center divider at X = leftW ---
	centerPipe := lipgloss.NewStyle().Foreground(ColorBorder).Background(bg).Render(GlyphPipe)

	// --- Right half: B (NEWER) + [<sha> · <time> ▾] or [● RUNNING CONFIG ▾] ---
	var rightBadge string
	if m.VsLive {
		rightBadge = lipgloss.NewStyle().
			Background(ColorWarningBg).
			Foreground(ColorOnWarning).
			Bold(true).
			Render(" B (NEWER) ")
	} else {
		rightBadge = lipgloss.NewStyle().
			Background(ColorSuccessBg).
			Foreground(ColorOnAccent).
			Bold(true).
			Render(" B (NEWER) ")
	}
	rightBadgeW := lipgloss.Width(rightBadge)

	rightPillRelStart := 1 + rightBadgeW + 1
	availRightPillW := rightHeaderW - rightPillRelStart - 1
	if availRightPillW < 6 {
		availRightPillW = 6
	}

	var rightPill string
	rightCommit := m.RightCommit
	if rightCommit == nil && !m.VsLive {
		rightCommit = m.Commit
	}
	if m.VsLive {
		liveInner := GlyphLive + " RUNNING CONFIG"
		if lipgloss.Width(liveInner)+4 > availRightPillW {
			liveInner = truncateStringEllipsis(liveInner, availRightPillW-4)
		}
		rightPill = lipgloss.NewStyle().
			Background(ColorWarningBg).
			Foreground(ColorOnWarning).
			Bold(true).
			Render("[" + liveInner + " " + GlyphExpanded + "]")
	} else {
		rightLabel := formatSplitEndpointLabel(rightCommit, "", "Current")
		rightLabelMax := availRightPillW - 4
		if rightLabelMax < 2 {
			rightLabelMax = 2
		}
		rightLabelTrim := truncateStringEllipsis(rightLabel, rightLabelMax)
		rightPill = lipgloss.NewStyle().
			Background(ColorBadgeBg).
			Foreground(ColorActive).
			Bold(true).
			Render("[" + rightLabelTrim + " " + GlyphExpanded + "]")
	}
	rightPillW := lipgloss.Width(rightPill)

	absRightStart := leftW + 1 + rightPillRelStart
	if absRightStart < innerW {
		absRightEnd := absRightStart + rightPillW
		if absRightEnd > innerW {
			absRightEnd = innerW
		}
		chips = append(chips, HitChip{
			Label:  "pick_right",
			StartX: absRightStart,
			EndX:   absRightEnd,
		})
	}

	rightRaw := sp + rightBadge + sp + rightPill
	if stat := formatSideBySideStatTokens(m.SideBySideResult, bg); stat != "" {
		used := rightPillRelStart + rightPillW
		sep := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" · ")
		if used+lipgloss.Width(sep)+lipgloss.Width(stat)+1 <= rightHeaderW {
			rightRaw += sep + stat
		}
	}
	rightPainted := PaintBarLine(rightRaw, rightHeaderW, bg, ColorText)

	return leftPainted + centerPipe + rightPainted, chips
}

func renderSplitDividerRow(leftW, innerW int) string {
	bg := ColorPanel
	if leftW < 0 {
		leftW = 0
	}
	if leftW >= innerW {
		rule := strings.Repeat("─", innerW)
		return PaintBarLine(lipgloss.NewStyle().Foreground(ColorBorder).Background(bg).Render(rule), innerW, bg, ColorBorder)
	}
	rightW := innerW - leftW - 1
	if rightW < 0 {
		rightW = 0
	}
	rule := strings.Repeat("─", leftW) + "┼" + strings.Repeat("─", rightW)
	return PaintBarLine(lipgloss.NewStyle().Foreground(ColorBorder).Background(bg).Render(rule), innerW, bg, ColorBorder)
}

func (m *DiffViewModel) updateSplitContent(wrapW int) {
	res := m.SideBySideResult

	if (!res.HasChanges && m.FoldUnchanged) || (!res.HasChanges && len(res.Rows) == 0) {
		msg := m.renderSplitEmptyMessage()
		styled := lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true).
			Background(ColorPanel).
			Render(msg)
		m.setViewportLines([]string{
			PaintLine("", wrapW, ColorPanel, ColorText),
			PaintLine(styled, wrapW, ColorPanel, ColorSuccess),
		})
		m.Viewport.SetYOffset(0)
		return
	}

	leftW, rightW := splitColumnWidths(wrapW)
	maxLeft, maxRight := 0, 0
	for _, r := range res.Rows {
		if r.LeftLineNo > maxLeft {
			maxLeft = r.LeftLineNo
		}
		if r.RightLineNo > maxRight {
			maxRight = r.RightLineNo
		}
	}
	leftDigits := splitGutterDigits(maxLeft)
	rightDigits := splitGutterDigits(maxRight)

	mode := strings.ToLower(strings.TrimSpace(res.Mode))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(m.DiffMode))
	}
	if mode == "" {
		mode = "cli"
	}

	centerPipe := lipgloss.NewStyle().Foreground(ColorBorder).Background(ColorPanel).Render(GlyphPipe)
	leftFiller := renderSplitFillerLine(leftW)
	rightFiller := renderSplitFillerLine(rightW)
	leftBlankPanel := PaintBarLine("", leftW, ColorPanel, ColorText)
	rightBlankPanel := PaintBarLine("", rightW, ColorPanel, ColorText)
	leftBlankDel := PaintBarLine("", leftW, ColorDiffDelBg, ColorDanger)
	rightBlankAdd := PaintBarLine("", rightW, ColorDiffAddBg, ColorSuccess)
	outLines := make([]string, 0, len(res.Rows)+2)
	highlightQuery := ""
	if m.DetailFilterBypass {
		highlightQuery = strings.TrimSpace(m.FilterQuery)
	}

	for _, row := range res.Rows {
		if row.Kind == differ.SideBySideFold {
			outLines = append(outLines, renderSplitFoldBar(row, wrapW))
			continue
		}
		outLines = append(outLines, renderSplitDiffRowCached(
			row, mode, highlightQuery, leftW, rightW, leftDigits, rightDigits, centerPipe,
			leftFiller, rightFiller, leftBlankPanel, rightBlankPanel, leftBlankDel, rightBlankAdd,
		)...)
	}

	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorMuted),
		PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Background(ColorPanel).Render("─── End of Diff ───"), wrapW, ColorPanel, ColorMuted),
	)

	m.setViewportLines(outLines)
	m.Viewport.SetYOffset(m.Viewport.YOffset)
}

func (m DiffViewModel) renderSplitEmptyMessage() string {
	leftCommit := m.LeftCommit
	if leftCommit == nil {
		leftCommit = m.BaseCommit
	}
	leftID := "Initial"
	if leftCommit != nil && strings.TrimSpace(leftCommit.CommitID) != "" {
		leftID = strings.TrimSpace(leftCommit.CommitID)
	} else if p := strings.TrimSpace(m.ParentSHA); p != "" {
		leftID = p
	}

	rightID := "Current"
	if m.VsLive {
		rightID = "RUNNING CONFIG"
	} else {
		rc := m.RightCommit
		if rc == nil {
			rc = m.Commit
		}
		if rc != nil && strings.TrimSpace(rc.CommitID) != "" {
			rightID = strings.TrimSpace(rc.CommitID)
		}
	}

	msg := fmt.Sprintf("  %s No differences between A %s and B %s", GlyphCheck, leftID, rightID)
	if fq := strings.TrimSpace(m.FilterQuery); fq != "" && !m.DetailFilterBypass {
		msg += fmt.Sprintf(" (filter: '%s')", fq)
	}
	return msg
}

func renderSplitFoldBar(row differ.SideBySideRow, wrapW int) string {
	var label string
	if strings.TrimSpace(row.FoldHint) != "" {
		label = fmt.Sprintf("··· %d unchanged lines (%s) — press E to expand ···", row.FoldCount, strings.TrimSpace(row.FoldHint))
	} else {
		label = fmt.Sprintf("··· %d unchanged lines — press E to expand ···", row.FoldCount)
	}
	if wrapW > 2 && lipgloss.Width(label) > wrapW-2 {
		label = truncateStringEllipsis(label, wrapW-2)
	}
	leftPad := (wrapW - lipgloss.Width(label)) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	styled := lipgloss.NewStyle().
		Foreground(ColorActive).
		Background(ColorDiffHunkBg).
		Italic(true).
		Render(strings.Repeat(" ", leftPad) + label)
	return PaintBarLine(styled, wrapW, ColorDiffHunkBg, ColorActive)
}

func renderSplitDiffRowCached(
	row differ.SideBySideRow,
	mode string,
	highlightQuery string,
	leftW, rightW, leftDigits, rightDigits int,
	centerPipe, leftFiller, rightFiller, leftBlankPanel, rightBlankPanel, leftBlankDel, rightBlankAdd string,
) []string {
	var leftLines, rightLines []string
	var leftPad, rightPad string

	switch row.Kind {
	case differ.SideBySideEqual:
		match := highlightQuery != "" && filter.LineMatchesQuery(row.LeftText, highlightQuery)
		leftStyled := highlightSplitLine(row.LeftText, mode, row.Kind, true, ColorPanel)
		rightStyled := highlightSplitLine(row.RightText, mode, row.Kind, false, ColorPanel)
		leftLines = renderSplitHalfCell(row.LeftLineNo, leftDigits, " ", ColorTextDim, leftStyled, leftW, ColorPanel, ColorText, match)
		rightLines = renderSplitHalfCell(row.RightLineNo, rightDigits, " ", ColorTextDim, rightStyled, rightW, ColorPanel, ColorText, match)
		leftPad, rightPad = leftBlankPanel, rightBlankPanel

	case differ.SideBySideDeleted:
		match := highlightQuery != "" && filter.LineMatchesQuery(row.LeftText, highlightQuery)
		leftStyled := highlightSplitLine(row.LeftText, mode, row.Kind, true, ColorDiffDelBg)
		leftLines = renderSplitHalfCell(row.LeftLineNo, leftDigits, "-", ColorDanger, leftStyled, leftW, ColorDiffDelBg, ColorDanger, match)
		rightLines = make([]string, len(leftLines))
		for i := range rightLines {
			rightLines[i] = rightFiller
		}
		leftPad, rightPad = leftBlankDel, rightFiller

	case differ.SideBySideAdded:
		match := highlightQuery != "" && filter.LineMatchesQuery(row.RightText, highlightQuery)
		rightStyled := highlightSplitLine(row.RightText, mode, row.Kind, false, ColorDiffAddBg)
		rightLines = renderSplitHalfCell(row.RightLineNo, rightDigits, "+", ColorSuccess, rightStyled, rightW, ColorDiffAddBg, ColorSuccess, match)
		leftLines = make([]string, len(rightLines))
		for i := range leftLines {
			leftLines[i] = leftFiller
		}
		leftPad, rightPad = leftFiller, rightBlankAdd

	case differ.SideBySideModified:
		leftMatch := highlightQuery != "" && filter.LineMatchesQuery(row.LeftText, highlightQuery)
		rightMatch := highlightQuery != "" && filter.LineMatchesQuery(row.RightText, highlightQuery)
		leftStyled := highlightSplitLine(row.LeftText, mode, row.Kind, true, ColorDiffDelBg)
		rightStyled := highlightSplitLine(row.RightText, mode, row.Kind, false, ColorDiffAddBg)
		leftLines = renderSplitHalfCell(row.LeftLineNo, leftDigits, "-", ColorDanger, leftStyled, leftW, ColorDiffDelBg, ColorDanger, leftMatch)
		rightLines = renderSplitHalfCell(row.RightLineNo, rightDigits, "+", ColorSuccess, rightStyled, rightW, ColorDiffAddBg, ColorSuccess, rightMatch)
		leftPad, rightPad = leftBlankDel, rightBlankAdd

	default:
		leftLines = []string{leftBlankPanel}
		rightLines = []string{rightBlankPanel}
		leftPad, rightPad = leftBlankPanel, rightBlankPanel
	}

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}
	if maxLines == 0 {
		maxLines = 1
	}

	for len(leftLines) < maxLines {
		leftLines = append(leftLines, leftPad)
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, rightPad)
	}

	out := make([]string, maxLines)
	for i := 0; i < maxLines; i++ {
		out[i] = leftLines[i] + centerPipe + rightLines[i]
	}
	return out
}

func renderSplitHalfCell(lineNo, digits int, sign string, signFg lipgloss.Color, styledContent string, colW int, bg, defaultFg lipgloss.Color, highlightMatch bool) []string {
	gutterW := digits + 3 // "%*d <sign> " (or "%*d <sign>▸" on filter match)
	if colW <= gutterW+4 {
		wrapped := PaintLine(styledContent, colW, bg, defaultFg)
		lines := strings.Split(wrapped, "\n")
		for i, l := range lines {
			lines[i] = PaintBarLine(l, colW, bg, defaultFg)
		}
		return lines
	}

	contentW := colW - gutterW
	wrapped := PaintLine(styledContent, contentW, bg, defaultFg)
	cLines := strings.Split(wrapped, "\n")

	numStr := strings.Repeat(" ", digits)
	if lineNo > 0 {
		numStr = fmt.Sprintf("%*d", digits, lineNo)
	}
	numSGR := styleSGR(bg, ColorTextDim, false, false, false)
	if highlightMatch {
		numSGR = styleSGR(bg, ColorCyan, true, false, false)
	}
	signSGR := styleSGR(bg, signFg, sign != " ", false, false)
	sp := wrapSGR(styleSGR(bg, "", false, false, false), " ")
	trail := sp
	if highlightMatch {
		trail = wrapSGR(styleSGR(bg, ColorCyan, true, false, false), GlyphCollapsed)
	}

	firstGutter := wrapSGR(numSGR, numStr) + sp + wrapSGR(signSGR, sign) + trail
	contGutter := wrapSGR(styleSGR(bg, "", false, false, false), strings.Repeat(" ", gutterW))

	out := make([]string, len(cLines))
	for i, cl := range cLines {
		gutter := contGutter
		if i == 0 {
			gutter = firstGutter
		}
		if strings.HasPrefix(gutter, "\x1b[") && strings.HasSuffix(gutter, "\x1b[0m") &&
			strings.HasPrefix(cl, "\x1b[") && strings.HasSuffix(cl, "\x1b[0m") {
			out[i] = gutter + cl
		} else {
			out[i] = PaintBarLine(gutter+cl, colW, bg, defaultFg)
		}
	}
	return out
}

func renderSplitFillerLine(colW int) string {
	bg := ColorBackground
	fg := ColorBadgeBg
	if colW <= 0 {
		return ""
	}
	var pattern string
	if colW >= 6 {
		pattern = " · " + strings.Repeat(GlyphScrollTrack, colW-4) + " "
	} else {
		pattern = strings.Repeat(GlyphScrollTrack, colW)
	}
	styled := lipgloss.NewStyle().Foreground(fg).Background(bg).Render(pattern)
	return PaintBarLine(styled, colW, bg, fg)
}

func highlightSplitLine(text, mode string, kind differ.SideBySideRowKind, isLeft bool, bg lipgloss.Color) string {
	switch mode {
	case "unified":
		return highlightJSONLine(text, bg)
	case "path":
		return highlightSplitPathLine(text, kind, isLeft, bg)
	default:
		return highlightSRLinuxCLI(text, bg)
	}
}

func highlightSplitPathLine(text string, kind differ.SideBySideRowKind, isLeft bool, bg lipgloss.Color) string {
	var pathFg lipgloss.Color
	switch kind {
	case differ.SideBySideAdded:
		pathFg = ColorSuccess
	case differ.SideBySideDeleted:
		pathFg = ColorDanger
	case differ.SideBySideModified:
		if isLeft {
			pathFg = ColorDanger
		} else {
			pathFg = ColorSuccess
		}
	default:
		pathFg = ColorText
	}
	pathStyle := lipgloss.NewStyle().Bold(true).Foreground(pathFg).Background(bg)
	if idx := strings.Index(text, " = "); idx != -1 {
		eqStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg)
		valStyle := lipgloss.NewStyle().Foreground(ColorSyntaxValue).Background(bg)
		return pathStyle.Render(text[:idx]) + eqStyle.Render(" = ") + valStyle.Render(text[idx+3:])
	}
	return pathStyle.Render(text)
}

// ============================================================================
// Shared Detail Pane Helpers (used by diff_view.go, config_view.go, blame_view.go)
// ============================================================================

type detailTabSpec struct {
	label string
	text  string
}

func buildDetailTabLayout(activeTab int, focused, zoomed bool, outerW int) (string, []HitChip) {
	return buildDetailTabLayoutWithSplit(activeTab, focused, zoomed, false, outerW)
}

func buildDetailTabLayoutWithSplit(activeTab int, focused, zoomed, splitActive bool, outerW int) (string, []HitChip) {
	if outerW < 7 {
		return "", nil
	}
	maxTitleW := outerW - 6 // ╭─ (2) + space (1) + title + space (1) + ─ (1+) + ╮ (1)

	compact := outerW <= 52
	var tabs []detailTabSpec
	var sepText string
	if compact {
		tabs = []detailTabSpec{
			{label: "diff", text: "d:Diff"},
			{label: "config", text: "c:Cfg"},
			{label: "blame", text: "b:Blm"},
		}
		sepText = " "
	} else {
		tabs = []detailTabSpec{
			{label: "diff", text: "Diff"},
			{label: "config", text: "Config"},
			{label: "blame", text: "Blame"},
		}
		sepText = " · "
	}

	titleBg := ColorBackground
	var activeStyle lipgloss.Style
	if focused {
		activeStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorPrimary)
	} else {
		activeStyle = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(ColorText).Background(ColorBadgeBg)
	}
	inactiveFg := ColorMuted
	if !focused {
		inactiveFg = ColorTextDim
	}
	inactiveStyle := lipgloss.NewStyle().Foreground(inactiveFg).Background(titleBg)
	sepStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(titleBg)

	var b strings.Builder
	// RenderTitledBox places titleStr starting at col 3 (0:╭, 1:─, 2:space, 3:titleStr).
	const baseOffsetX = 3
	maxEndX := baseOffsetX + maxTitleW
	cursorX := baseOffsetX

	chips := make([]HitChip, 0, len(tabs))
	for i, tab := range tabs {
		if i > 0 {
			sepRendered := sepStyle.Render(sepText)
			b.WriteString(sepRendered)
			cursorX += lipgloss.Width(sepRendered)
		}
		var rendered string
		if i == activeTab {
			rendered = activeStyle.Render(tab.text)
		} else {
			rendered = inactiveStyle.Render(tab.text)
		}
		w := lipgloss.Width(rendered)
		if cursorX < maxEndX {
			endX := cursorX + w
			if endX > maxEndX {
				endX = maxEndX
			}
			chips = append(chips, HitChip{
				Label:  tab.label,
				StartX: cursorX,
				EndX:   endX,
			})
		}
		b.WriteString(rendered)
		cursorX += w
	}

	if zoomed {
		zoomBadge := lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Background(titleBg).Render("[ZOOM]")
		sp := lipgloss.NewStyle().Background(titleBg).Render(" ")
		if cursorX-baseOffsetX+1+lipgloss.Width(zoomBadge) <= maxTitleW {
			b.WriteString(sp)
			b.WriteString(zoomBadge)
			cursorX += 1 + lipgloss.Width(zoomBadge)
		}
	}

	if splitActive {
		splitBadge := lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Background(titleBg).Render("[SPLIT A↔B]")
		sp := lipgloss.NewStyle().Background(titleBg).Render(" ")
		if cursorX-baseOffsetX+1+lipgloss.Width(splitBadge) <= maxTitleW {
			b.WriteString(sp)
			b.WriteString(splitBadge)
		}
	}

	return b.String(), chips
}

// renderDetailTitledBox renders the shared outer box for Diff, Config, and Blame views
// with embedded, aligned top-border tabs (`Diff · Config · Blame`).
func renderDetailTitledBox(activeTab int, focused, zoomed bool, body string, innerW, innerH int) string {
	return renderDetailTitledBoxWithSplit(activeTab, focused, zoomed, false, body, innerW, innerH)
}

// renderDetailTitledBoxWithSplit renders the shared detail box with an optional cyan [SPLIT A↔B] indicator
// appended after the tabs without shifting any tab hit ranges.
func renderDetailTitledBoxWithSplit(activeTab int, focused, zoomed, splitActive bool, body string, innerW, innerH int) string {
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	outerW := innerW + 2
	outerH := innerH + 2

	borderFg := ColorBorder
	if focused {
		borderFg = ColorActive
	}
	title, _ := buildDetailTabLayoutWithSplit(activeTab, focused, zoomed, splitActive, outerW)
	return RenderTitledBox(title, body, outerW, outerH, borderFg, ColorBackground, ColorBackground)
}

// DetailTabHitRanges returns 0-based horizontal hit ranges [StartX, EndX) relative to
// the Detail box left edge, matching the rendered top-border tabs ("diff", "config", "blame").
func DetailTabHitRanges(outerW int, zoomed bool) []HitChip {
	_, chips := buildDetailTabLayout(0, true, zoomed, outerW)
	return chips
}

// formatComparisonStatTokens computes and styles actual +A ~M -D leaf totals from diffRes
// for A↔B comparison and VsLive modes.
func formatComparisonStatTokens(diffRes models.SemanticDiffResult, bg lipgloss.Color) string {
	return formatComparisonStatTokensWithLabel(diffRes, bg, GlyphCheck+" Identical")
}

func formatComparisonStatTokensWithLabel(diffRes models.SemanticDiffResult, bg lipgloss.Color, identicalLabel string) string {
	added, modified, deleted := diffRes.AddedCount, diffRes.ModifiedCount, diffRes.DeletedCount
	if added == 0 && modified == 0 && deleted == 0 && len(diffRes.Changes) > 0 {
		for _, c := range filterRedundantPathChanges(diffRes.Changes) {
			switch c.DiffType {
			case models.DiffAdded:
				added++
			case models.DiffModified:
				modified++
			case models.DiffDeleted:
				deleted++
			}
		}
	}
	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("~%d", modified))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("-%d", deleted))
	}
	if len(parts) > 0 {
		return formatDiffStatTokens(strings.Join(parts, " "), bg)
	}
	if !diffRes.HasChanges && identicalLabel != "" {
		return lipgloss.NewStyle().Foreground(ColorSuccess).Background(bg).Render(identicalLabel)
	}
	return ""
}

// renderDiffContextStrip renders the explicit comparison-endpoint context strip for DiffView,
// showing whether the user is comparing Prev -> Commit (or Initial -> Commit),
// Commit -> ● RUNNING CONFIG, or BASE -> Commit.
func renderDiffContextStrip(commit, baseCommit *models.TimelineCommit, parentSHA string, vsLive bool, diffRes *models.SemanticDiffResult, width int) string {
	if width < 1 {
		return ""
	}
	bg := ColorBackground
	sp := lipgloss.NewStyle().Background(bg).Render(" ")
	sep := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" · ")
	arrow := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" → ")

	var parts []string
	usedW := 1 // leading space

	appendPart := func(sepStr, seg string) bool {
		if seg == "" {
			return false
		}
		need := lipgloss.Width(sepStr) + lipgloss.Width(seg)
		if len(parts) > 0 && usedW+need > width-1 {
			return false
		}
		if len(parts) > 0 && sepStr != "" {
			parts = append(parts, sepStr)
			usedW += lipgloss.Width(sepStr)
		}
		parts = append(parts, seg)
		usedW += lipgloss.Width(seg)
		return true
	}

	shaStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(bg)

	switch {
	case vsLive && commit != nil:
		liveBadge := lipgloss.NewStyle().
			Background(ColorWarningBg).
			Foreground(ColorOnWarning).
			Bold(true).
			Render(" " + GlyphLive + " RUNNING CONFIG ")
		appendPart("", shaStyle.Render(commit.CommitID)+arrow+liveBadge)
		if diffRes != nil {
			if stat := formatComparisonStatTokensWithLabel(*diffRes, bg, GlyphCheck+" Matches Running"); stat != "" {
				if !appendPart(sep, stat) && !diffRes.HasChanges {
					shortMatch := lipgloss.NewStyle().Foreground(ColorSuccess).Background(bg).Render(GlyphCheck + " Matches")
					appendPart(sep, shortMatch)
				}
			}
		}
		if commit.Author != "" {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(commit.Author))
		}
		if !commit.Timestamp.IsZero() {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(commit.Timestamp.Format("2006-01-02 15:04")))
		}
	case baseCommit != nil && commit != nil:
		baseBadge := lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true).
			Render(" BASE ")
		appendPart("", baseBadge+sp+shaStyle.Render(baseCommit.CommitID)+arrow+shaStyle.Render(commit.CommitID))
		if diffRes != nil {
			if stat := formatComparisonStatTokensWithLabel(*diffRes, bg, GlyphCheck+" Identical"); stat != "" {
				appendPart(sep, stat)
			}
		}
		if commit.Author != "" {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(commit.Author))
		}
	case commit != nil:
		if pSHA := strings.TrimSpace(parentSHA); pSHA != "" {
			prevLabel := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render("Prev ")
			parentStyled := lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(pSHA)
			appendPart("", prevLabel+parentStyled+arrow+shaStyle.Render(commit.CommitID))
		} else {
			initLabel := lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render("Initial")
			appendPart("", initLabel+arrow+shaStyle.Render(commit.CommitID))
		}
		if commit.Author != "" {
			authorSt := StyleAuthorOther.Background(bg)
			if commit.Author == "admin" || commit.Author == "root" {
				authorSt = StyleAuthorAdmin.Background(bg)
			}
			appendPart(sep, authorSt.Render(commit.Author))
		}
		if !commit.Timestamp.IsZero() {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(commit.Timestamp.Format("2006-01-02 15:04")))
		}
		stat := formatDiffStatTokens(commit.DiffStat, bg)
		if stat == "" && diffRes != nil && diffRes.HasChanges {
			stat = formatComparisonStatTokensWithLabel(*diffRes, bg, "")
		}
		if stat != "" {
			appendPart(sep, stat)
		}
	case baseCommit != nil:
		baseBadge := lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true).
			Render(" BASE ")
		appendPart("", baseBadge+sp+shaStyle.Render(baseCommit.CommitID))
	}

	return PaintBarLine(sp+strings.Join(parts, ""), width, bg, ColorText)
}

// renderCommitContextStrip renders a compact 1-row metadata summary strip below the Detail toolbar
// for ConfigView and BlameView (showing snapshot metadata, plus BASE badge if pinned).
func renderCommitContextStrip(commit, baseCommit *models.TimelineCommit, vsLive bool, diffRes *models.SemanticDiffResult, width int) string {
	if width < 1 {
		return ""
	}
	bg := ColorBackground
	sp := lipgloss.NewStyle().Background(bg).Render(" ")
	sep := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" · ")
	arrow := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" → ")

	var parts []string
	usedW := 1 // leading space

	appendPart := func(sepStr, seg string) {
		if seg == "" {
			return
		}
		need := lipgloss.Width(sepStr) + lipgloss.Width(seg)
		if len(parts) > 0 && usedW+need > width-1 {
			return
		}
		if len(parts) > 0 && sepStr != "" {
			parts = append(parts, sepStr)
			usedW += lipgloss.Width(sepStr)
		}
		parts = append(parts, seg)
		usedW += lipgloss.Width(seg)
	}

	shaStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(bg)

	switch {
	case vsLive && commit != nil:
		liveBadge := lipgloss.NewStyle().
			Background(ColorWarningBg).
			Foreground(ColorOnWarning).
			Bold(true).
			Render(" " + GlyphLive + " LIVE ")
		appendPart("", shaStyle.Render(commit.CommitID)+arrow+liveBadge)
		if diffRes != nil {
			if stat := formatComparisonStatTokens(*diffRes, bg); stat != "" {
				appendPart(sep, stat)
			}
		}
		if commit.Author != "" {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(commit.Author))
		}
		if !commit.Timestamp.IsZero() {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(commit.Timestamp.Format("2006-01-02 15:04")))
		}
	case baseCommit != nil && commit != nil:
		baseBadge := lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true).
			Render(" BASE ")
		appendPart("", baseBadge+sp+shaStyle.Render(baseCommit.CommitID)+arrow+shaStyle.Render(commit.CommitID))
		if diffRes != nil {
			if stat := formatComparisonStatTokens(*diffRes, bg); stat != "" {
				appendPart(sep, stat)
			}
		}
		if commit.Author != "" {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(commit.Author))
		}
	case commit != nil:
		appendPart("", shaStyle.Render(commit.CommitID))
		if commit.Author != "" {
			authorSt := StyleAuthorOther.Background(bg)
			if commit.Author == "admin" || commit.Author == "root" {
				authorSt = StyleAuthorAdmin.Background(bg)
			}
			appendPart(sep, authorSt.Render(commit.Author))
		}
		if !commit.Timestamp.IsZero() {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(commit.Timestamp.Format("2006-01-02 15:04")))
		}
		if stat := formatDiffStatTokens(commit.DiffStat, bg); stat != "" {
			appendPart(sep, stat)
		}
	case baseCommit != nil:
		baseBadge := lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true).
			Render(" BASE ")
		appendPart("", baseBadge+sp+shaStyle.Render(baseCommit.CommitID))
	}

	return PaintBarLine(sp+strings.Join(parts, ""), width, bg, ColorText)
}

func renderToolbarScrollStatuses(vp viewport.Model, compact bool) (string, string) {
	total := vp.TotalLineCount()
	if total <= 0 {
		return "", ""
	}
	bot := vp.YOffset + vp.Height
	if bot > total {
		bot = total
	}
	pct := int(vp.ScrollPercent() * 100)
	var fg lipgloss.Color
	switch {
	case vp.AtTop():
		fg = ColorMuted
	case vp.AtBottom():
		fg = ColorSuccess
	default:
		fg = ColorActive
	}
	st := lipgloss.NewStyle().Foreground(fg).Background(ColorSurface)
	shortRatio := st.Render(fmt.Sprintf("%d/%d", bot, total))
	if compact {
		pctOnly := st.Render(fmt.Sprintf("%d%%", pct))
		return shortRatio, pctOnly
	}

	var fullText string
	switch {
	case vp.AtTop():
		fullText = fmt.Sprintf("%s %d/%d", GlyphUp, bot, total)
	case vp.AtBottom():
		fullText = fmt.Sprintf("%s %d/%d", GlyphCheck, bot, total)
	default:
		fullText = fmt.Sprintf("%d%% %d/%d", pct, bot, total)
	}
	return st.Render(fullText), shortRatio
}

func renderCompactScrollStatus(vp viewport.Model) string {
	primary, _ := renderToolbarScrollStatuses(vp, false)
	return primary
}

// renderLinesWithScrollbar slices pre-painted viewport lines directly at vp.YOffset
// (bypassing bubbles/viewport.View()'s per-frame lipgloss style re-render) and attaches
// a slim 1-col scrollbar on the rightmost column when innerW > 4.
func renderLinesWithScrollbar(lines []string, vp viewport.Model, innerW, vpH int, bg lipgloss.Color) string {
	if len(lines) == 0 {
		return renderViewportWithScrollbar(vp, innerW, vpH, bg)
	}
	if innerW < 1 {
		innerW = 1
	}
	if vpH < 1 {
		vpH = 1
	}

	contentW := innerW
	withScrollbar := innerW > 4
	if withScrollbar {
		contentW = innerW - 1
	}

	total := len(lines)
	yOff := vp.YOffset
	if yOff < 0 {
		yOff = 0
	}
	maxOff := total - vpH
	if maxOff < 0 {
		maxOff = 0
	}
	if yOff > maxOff {
		yOff = maxOff
	}

	var sbCells []string
	if withScrollbar {
		sbCells = RenderScrollbar(vpH, total, yOff, bg)
	}
	var blankLine string

	combined := make([]string, vpH)
	for i := 0; i < vpH; i++ {
		idx := yOff + i
		var left string
		if idx < total {
			left = PaintBarLine(lines[idx], contentW, bg, ColorText)
		} else {
			if blankLine == "" {
				blankLine = PaintLine("", contentW, bg, ColorText)
			}
			left = blankLine
		}
		if withScrollbar {
			combined[i] = left + sbCells[i]
		} else {
			combined[i] = left
		}
	}
	return strings.Join(combined, "\n")
}

// renderViewportWithScrollbar renders vp at exact innerW x vpH, placing a slim 1-col
// scrollbar on the rightmost column when innerW > 4.
func renderViewportWithScrollbar(vp viewport.Model, innerW, vpH int, bg lipgloss.Color) string {
	if innerW < 1 {
		innerW = 1
	}
	if vpH < 1 {
		vpH = 1
	}
	vp.Height = vpH
	if innerW <= 4 {
		vp.Width = innerW
		return PaintSurface(vp.View(), innerW, vpH, bg, ColorText)
	}

	contentW := innerW - 1
	vp.Width = contentW
	painted := PaintSurface(vp.View(), contentW, vpH, bg, ColorText)
	vpLines := strings.Split(painted, "\n")
	sbCells := RenderScrollbar(vpH, vp.TotalLineCount(), vp.YOffset, bg)

	combined := make([]string, vpH)
	for i := 0; i < vpH; i++ {
		var left string
		if i < len(vpLines) {
			left = vpLines[i]
		} else {
			left = PaintLine("", contentW, bg, ColorText)
		}
		combined[i] = left + sbCells[i]
	}
	return strings.Join(combined, "\n")
}

// formatDiffStatTokens color-codes multi-part DiffStat strings (e.g. "+3 / ~1 / -2").
func formatDiffStatTokens(diffStat string, bg lipgloss.Color) string {
	s := strings.TrimSpace(diffStat)
	if s == "" || s == "-" || strings.EqualFold(s, "no changes") || strings.EqualFold(s, "identical") {
		return ""
	}
	// Replace "/" separators with spaces so we can style each +N, ~N, -N token independently.
	cleaned := strings.ReplaceAll(s, "/", " ")
	fields := strings.Fields(cleaned)
	if len(fields) == 0 {
		return ""
	}
	var styled []string
	sp := lipgloss.NewStyle().Background(bg).Render(" ")
	for _, tok := range fields {
		switch {
		case strings.HasPrefix(tok, "+"):
			styled = append(styled, StyleDiffStatAdd.Background(bg).Render(tok))
		case strings.HasPrefix(tok, "~"):
			styled = append(styled, StyleDiffStatMod.Background(bg).Render(tok))
		case strings.HasPrefix(tok, "-"):
			styled = append(styled, StyleDiffStatDel.Background(bg).Render(tok))
		default:
			styled = append(styled, lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(tok))
		}
	}
	return strings.Join(styled, sp)
}

// buildDetailFilterBanner builds the 1-row subset / full-view scope banner shared by Diff, Config, and Blame views.
// Returns an empty line and (-1, -1) when filterQuery is empty or innerW < 1.
func buildDetailFilterBanner(filterQuery string, detailBypass bool, shown, total int, unit string, fullLabel string, innerW int) (line string, toggleStartX, toggleEndX int) {
	q := strings.TrimSpace(filterQuery)
	if q == "" || innerW < 1 {
		return "", -1, -1
	}
	if shown < 0 {
		shown = 0
	}
	if total < 0 {
		total = 0
	}
	if strings.TrimSpace(unit) == "" {
		unit = "lines"
	}
	if strings.TrimSpace(fullLabel) == "" {
		fullLabel = "Full"
	}

	var barBg lipgloss.Color
	var badgeRendered string
	var pillFullText, pillCompactText string
	var pillStyle lipgloss.Style

	if !detailBypass {
		barBg = ColorDiffHunkBg
		badgeText := " " + GlyphMilestone + " FILTERED "
		if innerW < 54 {
			badgeText = " FILTERED "
		}
		badgeRendered = lipgloss.NewStyle().
			Background(ColorWarningBg).
			Foreground(ColorOnWarning).
			Bold(true).
			Render(badgeText)
		pillStyle = lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true)
		pillFullText = fmt.Sprintf(`[\ Show %s]`, fullLabel)
		pillCompactText = `[\ Full]`
	} else {
		barBg = ColorSurface
		badgeRendered = lipgloss.NewStyle().
			Background(ColorSuccessBg).
			Foreground(ColorOnAccent).
			Bold(true).
			Render(" " + GlyphLive + " FULL ")
		pillStyle = lipgloss.NewStyle().
			Background(ColorBadgeBg).
			Foreground(ColorActive).
			Bold(true)
		pillQ := truncateStringEllipsis(q, 14)
		pillFullText = fmt.Sprintf(`[\ Re-scope '%s']`, pillQ)
		pillCompactText = `[\ Scope]`
	}

	sp := lipgloss.NewStyle().Background(barBg).Render(" ")
	badgeW := lipgloss.Width(badgeRendered)
	leftPrefixW := 1 + badgeW + 1 // leading space + badge + space

	var pillText string
	pillFullW := lipgloss.Width(pillFullText)
	pillCompactW := lipgloss.Width(pillCompactText)
	switch {
	case innerW >= leftPrefixW+16+1+pillFullW+1:
		pillText = pillFullText
	case innerW >= leftPrefixW+4+1+pillCompactW+1:
		pillText = pillCompactText
	default:
		pillText = ""
	}

	var pillRendered string
	var availMidW int
	if pillText != "" {
		pillRendered = pillStyle.Render(pillText)
		pillW := lipgloss.Width(pillRendered)
		toggleStartX = innerW - pillW - 1
		toggleEndX = toggleStartX + pillW
		availMidW = toggleStartX - leftPrefixW - 1
	} else {
		toggleStartX, toggleEndX = -1, -1
		availMidW = innerW - leftPrefixW - 1
	}
	if availMidW < 0 {
		availMidW = 0
	}

	midStyled := buildDetailFilterBannerMid(q, detailBypass, shown, total, unit, barBg, availMidW)
	midW := lipgloss.Width(midStyled)
	if midW > availMidW {
		midStyled = ""
		midW = 0
	}

	var b strings.Builder
	b.WriteString(sp)
	b.WriteString(badgeRendered)
	if midW > 0 {
		b.WriteString(sp)
		b.WriteString(midStyled)
	}
	usedW := 1 + badgeW
	if midW > 0 {
		usedW += 1 + midW
	}
	if pillRendered != "" {
		gap := toggleStartX - usedW
		if gap < 1 {
			gap = 1
		}
		b.WriteString(lipgloss.NewStyle().Background(barBg).Render(strings.Repeat(" ", gap)))
		b.WriteString(pillRendered)
		b.WriteString(sp)
	}

	return PaintBarLine(b.String(), innerW, barBg, ColorText), toggleStartX, toggleEndX
}

func buildDetailFilterBannerMid(q string, detailBypass bool, shown, total int, unit string, barBg lipgloss.Color, availW int) string {
	if availW <= 0 {
		return ""
	}
	textSt := lipgloss.NewStyle().Foreground(ColorText).Background(barBg)
	mutedSt := lipgloss.NewStyle().Foreground(ColorMuted).Background(barBg)
	warnSt := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Background(barBg)
	queryFg := ColorActive
	if detailBypass {
		queryFg = ColorCyan
	}
	querySt := lipgloss.NewStyle().Foreground(queryFg).Bold(true).Background(barBg)

	if !detailBypass {
		hidden := total - shown
		if hidden < 0 {
			hidden = 0
		}

		if total > shown {
			fullQ := fmt.Sprintf("'%s'", q)
			sep := " · "
			countFull := fmt.Sprintf("Showing %d of %d %s ", shown, total, unit)
			hiddenFull := fmt.Sprintf("(%d hidden)", hidden)
			if lipgloss.Width(fullQ+sep+countFull+hiddenFull) <= availW {
				return querySt.Render(fullQ) + mutedSt.Render(sep) + textSt.Render(countFull) + warnSt.Render(hiddenFull)
			}

			countCompact := fmt.Sprintf("%d/%d ", shown, total)
			if lipgloss.Width(fullQ+sep+countCompact+hiddenFull) <= availW {
				return querySt.Render(fullQ) + mutedSt.Render(sep) + textSt.Render(countCompact) + warnSt.Render(hiddenFull)
			}

			countShort := fmt.Sprintf("%d/%d", shown, total)
			if lipgloss.Width(fullQ+sep+countShort) <= availW {
				return querySt.Render(fullQ) + mutedSt.Render(sep) + textSt.Render(countShort)
			}

			maxQ := availW - lipgloss.Width(sep+countShort) - 2
			if maxQ >= 3 {
				tq := fmt.Sprintf("'%s'", truncateStringEllipsis(q, maxQ))
				return querySt.Render(tq) + mutedSt.Render(sep) + textSt.Render(countShort)
			}
			return textSt.Render(truncateStringEllipsis(countShort, availW))
		}

		fullQ := fmt.Sprintf("'%s'", q)
		sep := " · "
		countFull := fmt.Sprintf("Showing %d %s", shown, unit)
		if lipgloss.Width(fullQ+sep+countFull) <= availW {
			return querySt.Render(fullQ) + mutedSt.Render(sep) + textSt.Render(countFull)
		}
		countShort := fmt.Sprintf("%d %s", shown, unit)
		if lipgloss.Width(fullQ+sep+countShort) <= availW {
			return querySt.Render(fullQ) + mutedSt.Render(sep) + textSt.Render(countShort)
		}
		maxQ := availW - lipgloss.Width(sep+countShort) - 2
		if maxQ >= 3 {
			tq := fmt.Sprintf("'%s'", truncateStringEllipsis(q, maxQ))
			return querySt.Render(tq) + mutedSt.Render(sep) + textSt.Render(countShort)
		}
		return textSt.Render(truncateStringEllipsis(countShort, availW))
	}

	// Detached / Full view banner
	displayTotal := total
	if displayTotal == 0 {
		displayTotal = shown
	}
	fullQ := fmt.Sprintf("'%s'", q)
	sep := " · "
	if displayTotal > 0 {
		prefix := fmt.Sprintf("all %d %s", displayTotal, unit)
		midLbl := "Timeline filtered by "
		if lipgloss.Width(prefix+sep+midLbl+fullQ) <= availW {
			return textSt.Render(prefix) + mutedSt.Render(sep+midLbl) + querySt.Render(fullQ)
		}
		midShort := "Filter: "
		if lipgloss.Width(prefix+sep+midShort+fullQ) <= availW {
			return textSt.Render(prefix) + mutedSt.Render(sep+midShort) + querySt.Render(fullQ)
		}
		maxQ := availW - lipgloss.Width(prefix+sep) - 2
		if maxQ >= 3 {
			tq := fmt.Sprintf("'%s'", truncateStringEllipsis(q, maxQ))
			return textSt.Render(prefix) + mutedSt.Render(sep) + querySt.Render(tq)
		}
		return textSt.Render(truncateStringEllipsis(prefix, availW))
	}
	midLbl := "Timeline filtered by "
	if lipgloss.Width(midLbl+fullQ) <= availW {
		return mutedSt.Render(midLbl) + querySt.Render(fullQ)
	}
	maxQ := availW - 2
	if maxQ >= 3 {
		return querySt.Render(fmt.Sprintf("'%s'", truncateStringEllipsis(q, maxQ)))
	}
	return mutedSt.Render(truncateStringEllipsis("Full", availW))
}

func pathChangeMatchesQuery(c models.PathChange, query string) bool {
	if filter.IsPathMatching(c.Path, query) {
		return true
	}
	if c.OldValue != nil && filter.LineMatchesQuery(fmt.Sprintf("%v", c.OldValue), query) {
		return true
	}
	if c.NewValue != nil && filter.LineMatchesQuery(fmt.Sprintf("%v", c.NewValue), query) {
		return true
	}
	return false
}

func gutterDigitsForCount(total int) int {
	if total < 100 {
		return 3
	}
	return len(strconv.Itoa(total))
}

// formatGutteredWrappedLines renders a single logical line with a dimmed line-number gutter
// and wraps its content cleanly onto continuation rows aligned after the gutter.
func formatGutteredWrappedLines(lineNo, digits int, styledContent string, totalW int, bg, defaultFg lipgloss.Color) []string {
	return formatGutteredWrappedLinesMarked(lineNo, digits, styledContent, totalW, bg, defaultFg, false)
}

// formatGutteredWrappedLinesMarked renders a single logical line with a line-number gutter,
// optionally emphasizing the gutter with bold cyan line number + ▸ marker when highlightMatch is true.
func formatGutteredWrappedLinesMarked(lineNo, digits int, styledContent string, totalW int, bg, defaultFg lipgloss.Color, highlightMatch bool) []string {
	if totalW <= digits+8 {
		// Too narrow for a separate gutter; paint content directly.
		wrapped := PaintLine(styledContent, totalW, bg, defaultFg)
		return strings.Split(wrapped, "\n")
	}

	gutterW := digits + 2
	usePipe := totalW >= 36
	if usePipe {
		gutterW = digits + 3 // "%*d │ " (or "%*d ▸ " on match)
	} else {
		gutterW = digits + 2 // "%*d  " (or "%*d▸ " on match)
	}
	contentW := totalW - gutterW
	if contentW < 4 {
		contentW = 4
	}

	wrappedContent := PaintLine(styledContent, contentW, bg, defaultFg)
	cLines := strings.Split(wrappedContent, "\n")

	numSGR := styleSGR(bg, ColorTextDim, false, false, false)
	pipeSGR := styleSGR(bg, ColorBorder, false, false, false)
	firstGlyph := GlyphPipe
	if highlightMatch {
		numSGR = styleSGR(bg, ColorCyan, true, false, false)
		pipeSGR = styleSGR(bg, ColorCyan, true, false, false)
		firstGlyph = GlyphCollapsed
	}
	bgSGR := styleSGR(bg, "", false, false, false)
	sp := wrapSGR(bgSGR, " ")

	var firstGutter, contGutter string
	if usePipe {
		firstGutter = wrapSGR(numSGR, fmt.Sprintf("%*d", digits, lineNo)) + sp + wrapSGR(pipeSGR, firstGlyph) + sp
		contGutter = wrapSGR(numSGR, strings.Repeat(" ", digits)) + sp + wrapSGR(pipeSGR, GlyphPipe) + sp
	} else {
		if highlightMatch {
			firstGutter = wrapSGR(numSGR, fmt.Sprintf("%*d", digits, lineNo)) + wrapSGR(pipeSGR, GlyphCollapsed) + sp
		} else {
			firstGutter = wrapSGR(numSGR, fmt.Sprintf("%*d", digits, lineNo)) + wrapSGR(bgSGR, "  ")
		}
		contGutter = wrapSGR(bgSGR, strings.Repeat(" ", gutterW))
	}

	out := make([]string, 0, len(cLines))
	for i, cl := range cLines {
		gutter := contGutter
		if i == 0 {
			gutter = firstGutter
		}
		if strings.HasPrefix(gutter, "\x1b[") && strings.HasSuffix(gutter, "\x1b[0m") &&
			strings.HasPrefix(cl, "\x1b[") && strings.HasSuffix(cl, "\x1b[0m") {
			out = append(out, gutter+cl)
		} else {
			out = append(out, PaintBarLine(gutter+cl, totalW, bg, defaultFg))
		}
	}
	return out
}

// splitTopLevelXPathSegments splits an XPath on '/' boundaries outside [...] brackets,
// keeping the leading '/' attached to each segment so wrapped continuation lines read cleanly.
func splitTopLevelXPathSegments(path string) []string {
	if path == "" {
		return nil
	}
	var segs []string
	segStart := 0
	bracketDepth := 0
	for i, r := range path {
		switch r {
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '/':
			if bracketDepth == 0 && i > segStart {
				segs = append(segs, path[segStart:i])
				segStart = i
			}
		}
	}
	if segStart < len(path) {
		segs = append(segs, path[segStart:])
	}
	return segs
}

// splitBracketPredicates splits an oversize XPath segment on '[' boundaries
// (e.g. "/acl-filter[name=uplink-in][type=ipv4]" -> ["/acl-filter", "[name=uplink-in]", "[type=ipv4]"]).
func splitBracketPredicates(seg string) []string {
	if seg == "" {
		return nil
	}
	var parts []string
	partStart := 0
	for i, r := range seg {
		if r == '[' && i > partStart {
			parts = append(parts, seg[partStart:i])
			partStart = i
		}
	}
	if partStart < len(seg) {
		parts = append(parts, seg[partStart:])
	}
	return parts
}

func wrapRunesByWidth(s string, maxW int) []string {
	if maxW < 1 {
		maxW = 1
	}
	if lipgloss.Width(s) <= maxW {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	curW := 0
	for _, r := range s {
		rs := string(r)
		rw := lipgloss.Width(rs)
		if curW > 0 && curW+rw > maxW {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteRune(r)
		curW += rw
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// wrapXPathSegments wraps an XPath into lines of at most maxW columns, breaking cleanly
// on top-level '/' segment boundaries (and '[' predicate boundaries for oversize segments),
// falling back to rune-width wrap only when a single atomic token exceeds maxW.
func wrapXPathSegments(path string, maxW int) []string {
	if maxW < 1 {
		maxW = 1
	}
	if path == "" || lipgloss.Width(path) <= maxW {
		return []string{path}
	}

	segments := splitTopLevelXPathSegments(path)
	var lines []string
	cur := ""
	curW := 0

	appendAtomic := func(tok string) {
		tw := lipgloss.Width(tok)
		if cur == "" {
			if tw <= maxW {
				cur = tok
				curW = tw
				return
			}
			chunks := wrapRunesByWidth(tok, maxW)
			lines = append(lines, chunks[:len(chunks)-1]...)
			cur = chunks[len(chunks)-1]
			curW = lipgloss.Width(cur)
			return
		}
		if curW+tw <= maxW {
			cur += tok
			curW += tw
			return
		}
		lines = append(lines, cur)
		if tw <= maxW {
			cur = tok
			curW = tw
			return
		}
		chunks := wrapRunesByWidth(tok, maxW)
		lines = append(lines, chunks[:len(chunks)-1]...)
		cur = chunks[len(chunks)-1]
		curW = lipgloss.Width(cur)
	}

	for _, seg := range segments {
		sw := lipgloss.Width(seg)
		if cur != "" && curW+sw <= maxW {
			cur += seg
			curW += sw
			continue
		}
		if sw <= maxW {
			if cur != "" {
				lines = append(lines, cur)
			}
			cur = seg
			curW = sw
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
			curW = 0
		}
		for _, part := range splitBracketPredicates(seg) {
			appendAtomic(part)
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// formatPathChangeLines renders a single structured PathChange with a line-number gutter,
// keeping the ADD/MOD/DEL badge and the first XPath segment on the same line (never orphaned),
// wrapping continuation segments on '/' boundaries with a clean hanging indent aligned to the path start.
func formatPathChangeLines(lineNo, digits int, c models.PathChange, totalW int) []string {
	return formatPathChangeLinesMarked(lineNo, digits, c, totalW, false)
}

func formatPathChangeLinesMarked(lineNo, digits int, c models.PathChange, totalW int, highlightMatch bool) []string {
	var badge string
	var pathFg lipgloss.Color
	switch c.DiffType {
	case models.DiffAdded:
		badge = lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Render(" ADD ")
		pathFg = ColorSuccess
	case models.DiffModified:
		badge = lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Render(" MOD ")
		pathFg = ColorText
	case models.DiffDeleted:
		badge = lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Render(" DEL ")
		pathFg = ColorDanger
	default:
		badge = lipgloss.NewStyle().Background(ColorBadgeBg).Foreground(ColorMuted).Render(" CHG ")
		pathFg = ColorText
	}

	bg := ColorPanel
	sp := lipgloss.NewStyle().Background(bg).Render(" ")
	badgePrefix := badge + sp
	badgeBlockW := lipgloss.Width(badgePrefix) // 6 cols (" DEL  ")

	var firstGutter, contGutter string
	contentW := totalW
	if totalW > digits+8 {
		usePipe := totalW >= 36
		gutterW := digits + 2
		if usePipe {
			gutterW = digits + 3
		}
		contentW = totalW - gutterW
		if contentW < 4 {
			contentW = 4
		}
		numStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg)
		pipeStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(bg)
		firstGlyph := GlyphPipe
		if highlightMatch {
			numStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Background(bg)
			pipeStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Background(bg)
			firstGlyph = GlyphCollapsed
		}
		if usePipe {
			firstGutter = numStyle.Render(fmt.Sprintf("%*d", digits, lineNo)) + sp + pipeStyle.Render(firstGlyph) + sp
			contGutter = numStyle.Render(strings.Repeat(" ", digits)) + sp + pipeStyle.Render(GlyphPipe) + sp
		} else {
			if highlightMatch {
				firstGutter = numStyle.Render(fmt.Sprintf("%*d", digits, lineNo)) + pipeStyle.Render(GlyphCollapsed) + sp
			} else {
				firstGutter = numStyle.Render(fmt.Sprintf("%*d", digits, lineNo)) + lipgloss.NewStyle().Background(bg).Render("  ")
			}
			contGutter = lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gutterW))
		}
	}

	availPathW := contentW - badgeBlockW
	hangIndent := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", badgeBlockW))
	if availPathW < 4 {
		availPathW = 4
		hangIndent = ""
	}

	pathStyle := lipgloss.NewStyle().Bold(true).Foreground(pathFg).Background(bg)
	pathLines := wrapXPathSegments(c.Path, availPathW)
	valSuffix := formatPathChangeValue(c, bg)

	// Decide whether valSuffix fits cleanly on the final path line or wraps onto continuation line(s).
	appendValInline := false
	var extraValLines []string
	if valSuffix != "" {
		lastPathW := lipgloss.Width(pathLines[len(pathLines)-1])
		valW := lipgloss.Width(valSuffix)
		if lastPathW+2+valW <= availPathW {
			appendValInline = true
		} else {
			wrappedVal := PaintLine(valSuffix, availPathW, bg, pathFg)
			extraValLines = strings.Split(wrappedVal, "\n")
		}
	}

	out := make([]string, 0, len(pathLines)+len(extraValLines))
	for i, pl := range pathLines {
		styledSeg := pathStyle.Render(pl)
		if i == len(pathLines)-1 && appendValInline {
			styledSeg += lipgloss.NewStyle().Background(bg).Render("  ") + valSuffix
		}
		if i == 0 {
			out = append(out, PaintBarLine(firstGutter+badgePrefix+styledSeg, totalW, bg, pathFg))
		} else {
			out = append(out, PaintBarLine(contGutter+hangIndent+styledSeg, totalW, bg, pathFg))
		}
	}
	for _, vl := range extraValLines {
		out = append(out, PaintBarLine(contGutter+hangIndent+vl, totalW, bg, pathFg))
	}
	return out
}

// cliListKeyParents lists SR Linux CLI keywords whose immediately following token is a list key.
var cliListKeyParents = map[string]bool{
	"interface":         true,
	"subinterface":      true,
	"network-instance":  true,
	"neighbor":          true,
	"group":             true,
	"peer-group":        true,
	"policy":            true,
	"statement":         true,
	"prefix-set":        true,
	"community-set":     true,
	"as-path-set":       true,
	"entry":             true,
	"ipv4-filter":       true,
	"ipv6-filter":       true,
	"cpm-filter":        true,
	"server-profile":    true,
	"client-profile":    true,
	"profile":           true,
	"user":              true,
	"area":              true,
	"instance":          true,
	"tunnel-interface":  true,
	"vxlan-interface":   true,
	"bgp-instance":      true,
	"address":           true,
	"prefix":            true,
	"next-hop-group":    true,
	"route":             true,
	"vlan":              true,
	"vrf":               true,
	"lag":               true,
	"member":            true,
	"ntp-server":        true,
	"server":            true,
	"syslog":            true,
	"remote-server":     true,
	"snmp":              true,
	"community":         true,
	"keychain":          true,
	"key":               true,
	"mac-vrf":           true,
	"ip-vrf":            true,
}

type cliToken struct {
	text    string
	isSpace bool
	quoted  bool
}

func tokenizeCLIArgs(s string) []cliToken {
	var tokens []cliToken
	i := 0
	n := len(s)
	for i < n {
		if s[i] == ' ' || s[i] == '\t' {
			start := i
			for i < n && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			tokens = append(tokens, cliToken{text: s[start:i], isSpace: true})
			continue
		}
		if s[i] == '"' {
			start := i
			i++
			for i < n {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == '"' {
					i++
					break
				}
				i++
			}
			tokens = append(tokens, cliToken{text: s[start:i], quoted: true})
			continue
		}
		start := i
		for i < n && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		tokens = append(tokens, cliToken{text: s[start:i]})
	}
	return tokens
}

// highlightSRLinuxCLI tokenizes and syntax-highlights an SR Linux flat CLI statement
// (with optional leading '+'/'-' diff sign).
func highlightSRLinuxCLI(line string, bg lipgloss.Color) string {
	var b strings.Builder
	b.Grow(len(line) + 96)
	rest := line
	bgOnlySGR := styleSGR(bg, "", false, false, false)

	// 1. Optional leading diff sign (+ / -)
	if strings.HasPrefix(rest, "+") {
		b.WriteString(wrapSGR(styleSGR(bg, ColorSuccess, true, false, false), "+"))
		rest = rest[1:]
		if strings.HasPrefix(rest, " ") {
			b.WriteString(wrapSGR(bgOnlySGR, " "))
			rest = rest[1:]
		}
	} else if strings.HasPrefix(rest, "-") {
		b.WriteString(wrapSGR(styleSGR(bg, ColorDanger, true, false, false), "-"))
		rest = rest[1:]
		if strings.HasPrefix(rest, " ") {
			b.WriteString(wrapSGR(bgOnlySGR, " "))
			rest = rest[1:]
		}
	}

	// Preserve any extra leading spaces
	trimmedLeft := strings.TrimLeft(rest, " ")
	if padLen := len(rest) - len(trimmedLeft); padLen > 0 {
		b.WriteString(wrapSGR(bgOnlySGR, rest[:padLen]))
		rest = trimmedLeft
	}

	// 2. Dim `set /` or `delete /` command prefix
	cmdSGR := styleSGR(bg, ColorTextDim, false, false, false)
	hasCmdPrefix := false
	for _, prefix := range []string{"set / ", "delete / ", "set /", "delete /"} {
		if strings.HasPrefix(rest, prefix) {
			b.WriteString(wrapSGR(cmdSGR, prefix))
			rest = rest[len(prefix):]
			hasCmdPrefix = true
			break
		}
	}

	tokens := tokenizeCLIArgs(rest)
	if len(tokens) == 0 {
		return b.String()
	}

	// Identify non-space token indices so we know subsystem (0) and leaf value (last)
	nonSpaceIdxs := make([]int, 0, len(tokens))
	for idx, tok := range tokens {
		if !tok.isSpace {
			nonSpaceIdxs = append(nonSpaceIdxs, idx)
		}
	}

	subsystemIdx := -1
	lastArgIdx := -1
	if len(nonSpaceIdxs) > 0 {
		subsystemIdx = nonSpaceIdxs[0]
		if len(nonSpaceIdxs) >= 3 {
			lastArgIdx = nonSpaceIdxs[len(nonSpaceIdxs)-1]
		}
	}

	// Use cached SGR prefixes and coalesce adjacent plain/space runs.
	valSGR := styleSGR(bg, ColorSyntaxValue, false, false, false)
	keySGR := styleSGR(bg, ColorCyan, true, false, false)
	textSGR := styleSGR(bg, ColorText, false, false, false)

	var plainRun strings.Builder
	flushPlain := func() {
		if plainRun.Len() > 0 {
			b.WriteString(wrapSGR(textSGR, plainRun.String()))
			plainRun.Reset()
		}
	}

	prevWord := ""
	for idx, tok := range tokens {
		if tok.isSpace {
			plainRun.WriteString(tok.text)
			continue
		}

		// Subsystem token
		if idx == subsystemIdx && (hasCmdPrefix || isKnownCLISubsystem(tok.text)) {
			flushPlain()
			subFg := cliSubsystemColor(tok.text)
			b.WriteString(wrapSGR(styleSGR(bg, subFg, true, false, false), tok.text))
			prevWord = strings.ToLower(tok.text)
			continue
		}

		// Quoted string or explicit leaf value
		if tok.quoted {
			flushPlain()
			b.WriteString(wrapSGR(valSGR, tok.text))
			prevWord = ""
			continue
		}

		// Emphasize list keys immediately following a list-parent keyword
		if cliListKeyParents[prevWord] && !isCLIStructuralKeyword(tok.text) {
			flushPlain()
			b.WriteString(wrapSGR(keySGR, tok.text))
			prevWord = ""
			continue
		}

		// Final token on a 3+ token statement is typically the leaf value
		if idx == lastArgIdx {
			flushPlain()
			b.WriteString(wrapSGR(valSGR, tok.text))
			prevWord = strings.ToLower(tok.text)
			continue
		}

		// Intermediate container / leaf name (coalesced with adjacent spaces)
		plainRun.WriteString(tok.text)
		prevWord = strings.ToLower(tok.text)
	}
	flushPlain()

	return b.String()
}

func isKnownCLISubsystem(word string) bool {
	switch strings.ToLower(word) {
	case "interface", "network-instance", "system", "acl", "routing-policy", "platform", "bfd", "tunnel-interface", "qos":
		return true
	default:
		return false
	}
}

func cliSubsystemColor(word string) lipgloss.Color {
	switch strings.ToLower(word) {
	case "interface", "tunnel-interface":
		return ColorSyntaxInterface
	case "network-instance", "routing-policy":
		return ColorSyntaxNetwork
	case "system", "platform":
		return ColorSyntaxSystem
	case "acl", "qos":
		return ColorSyntaxACL
	default:
		return ColorActive
	}
}

func isCLIStructuralKeyword(word string) bool {
	switch strings.ToLower(word) {
	case "admin-state", "description", "protocols", "bgp", "ospf", "isis", "static-routes",
		"ipv4", "ipv6", "unicast", "evpn", "vxlan", "ethernet", "vlan-tagging", "mtu",
		"match", "action", "accept", "drop", "reject", "default-action":
		return true
	default:
		return false
	}
}

// TruncateRunes truncates s to at most maxRunes printable columns with an ellipsis.
func truncateStringEllipsis(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return truncate.StringWithTail(s, uint(maxWidth), "…")
}
