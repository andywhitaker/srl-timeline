package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"timeline/pkg/models"
)

// DiffViewModel handles the syntax-highlighted semantic diff viewport.
type DiffViewModel struct {
	DiffResult models.SemanticDiffResult
	DiffMode   string // "unified", "path", "cli"
	VsLive     bool
	Zoomed     bool
	Commit     *models.TimelineCommit
	BaseCommit *models.TimelineCommit
	Viewport   viewport.Model
	Width      int
	Height     int
	IsFocused  bool

	emptyMsg string
}

// NewDiffViewModel creates a new diff viewer (defaulting to CLI diff mode).
func NewDiffViewModel(width, height int) DiffViewModel {
	m := DiffViewModel{
		DiffMode:  "cli",
		Width:     width,
		Height:    height,
		IsFocused: false,
	}
	m.Viewport = viewport.New(m.viewportWidth(), m.viewportHeight())
	return m
}

func (m DiffViewModel) hasContextStrip() bool {
	return m.Height >= 16 && (m.Commit != nil || m.BaseCommit != nil)
}

// HeaderRows returns the number of inner rows above the scrollable viewport.
func (m DiffViewModel) HeaderRows() int {
	rows := 1 // toolbar
	if m.hasContextStrip() {
		rows++
	}
	return rows
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

// ResetScroll resets the viewport scroll position to the top.
func (m *DiffViewModel) ResetScroll() {
	m.Viewport.SetYOffset(0)
}

// Clear resets the diff view to an empty state with the given message.
func (m *DiffViewModel) Clear(message string) {
	m.DiffResult = models.SemanticDiffResult{}
	m.Commit = nil
	m.BaseCommit = nil
	m.emptyMsg = strings.TrimSpace(message)
	m.Viewport.SetYOffset(0)
	m.UpdateContent()
}

// SetSize handles terminal resizing.
func (m *DiffViewModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.Viewport.Width = m.viewportWidth()
	m.Viewport.Height = m.viewportHeight()
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
		m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n" + PaintLine(msg, wrapW, ColorPanel, ColorMuted))
		m.Viewport.SetYOffset(0)
		return
	}

	if !m.DiffResult.HasChanges {
		msg := lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true).
			Render("  " + GlyphCheck + " No configuration changes for this revision / filter")
		m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorText) + "\n" + PaintLine(msg, wrapW, ColorPanel, ColorSuccess))
		m.Viewport.SetYOffset(0)
		return
	}

	var outLines []string

	switch m.DiffMode {
	case "unified":
		if len(m.DiffResult.UnifiedDiffLines) <= 2 {
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render("  No unified JSON differences found for this filter. Check [2] Path or [3] CLI diff.")
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
				outLines = append(outLines, formatGutteredWrappedLines(lineNo, digits, styled, wrapW, bg, fg)...)
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
				lineNo := idx + 1
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

				pathStyled := lipgloss.NewStyle().Bold(true).Foreground(pathFg).Background(ColorPanel).Render(c.Path)
				valSuffix := formatPathChangeValue(c, ColorPanel)
				entryStr := badge + lipgloss.NewStyle().Background(ColorPanel).Render(" ") + pathStyled
				if valSuffix != "" {
					entryStr += lipgloss.NewStyle().Background(ColorPanel).Render("  ") + valSuffix
				}
				outLines = append(outLines, formatGutteredWrappedLines(lineNo, digits, entryStr, wrapW, ColorPanel, pathFg)...)
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
				outLines = append(outLines, formatGutteredWrappedLines(lineNo, digits, styled, wrapW, bg, fg)...)
			}
		}
	}

	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorMuted),
		PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Background(ColorPanel).Render("─── End of Diff ───"), wrapW, ColorPanel, ColorMuted),
	)

	m.Viewport.SetContent(strings.Join(outLines, "\n"))
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
	label    string
	text     string
	active   bool
	warning  bool
	padH     int
	fixedPad bool
}

func buildToolbarLine(specs []toolbarPillSpec, focused bool, rightStatus string, innerW int) (string, []HitChip) {
	if innerW < 1 {
		innerW = 1
	}

	var b strings.Builder
	sp := lipgloss.NewStyle().Background(ColorSurface).Render(" ")
	b.WriteString(sp)
	cursorX := 1

	chips := make([]HitChip, 0, len(specs))
	for i, spec := range specs {
		if i > 0 {
			b.WriteString(sp)
			cursorX++
		}
		padH := spec.padH
		if padH == 0 && !spec.fixedPad {
			padH = 1
		}
		st := lipgloss.NewStyle().Padding(0, padH)
		switch {
		case spec.warning && spec.active:
			st = st.Background(ColorWarningBg).Foreground(ColorOnWarning)
			if focused {
				st = st.Bold(true)
			}
		case spec.active && focused:
			st = st.Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true)
		case spec.active && !focused:
			st = st.Background(ColorBadgeBg).Foreground(ColorTextDim).Bold(true)
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

	if rightStatus != "" {
		rightW := lipgloss.Width(rightStatus)
		gap := innerW - cursorX - rightW - 1
		if gap >= 1 {
			b.WriteString(lipgloss.NewStyle().Background(ColorSurface).Render(strings.Repeat(" ", gap)))
			b.WriteString(rightStatus)
		}
	}

	return PaintBarLine(b.String(), innerW, ColorSurface, ColorText), chips
}

func (m DiffViewModel) buildToolbar(innerW int) (string, []HitChip) {
	liveText := "[4] Live"
	livePad := 1
	if m.VsLive {
		liveText = GlyphLive + " Live"
		livePad = 2 // exact same total width (10 cols) as "[4] Live" with pad 1!
	}

	specs := []toolbarPillSpec{
		{label: "unified", text: "[1] Unified", active: m.DiffMode == "unified", padH: 1},
		{label: "path", text: "[2] Path", active: m.DiffMode == "path", padH: 1},
		{label: "cli", text: "[3] CLI", active: m.DiffMode == "cli" || m.DiffMode == "", padH: 1},
		{label: "live", text: liveText, active: m.VsLive, warning: true, padH: livePad, fixedPad: true},
	}

	scrollBadge := renderCompactScrollStatus(m.Viewport)
	return buildToolbarLine(specs, m.IsFocused, scrollBadge, innerW)
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

	if m.hasContextStrip() {
		sections = append(sections, renderCommitContextStrip(m.Commit, m.BaseCommit, m.VsLive, innerW))
	}

	vpH := innerH - len(sections)
	if vpH < 1 {
		vpH = 1
	}
	vpView := renderViewportWithScrollbar(m.Viewport, innerW, vpH, ColorPanel)
	sections = append(sections, vpView)

	body := strings.Join(sections, "\n")
	return renderDetailTitledBox(int(TabDiff), m.IsFocused, m.Zoomed, body, innerW, innerH)
}

// ============================================================================
// Shared Detail Pane Helpers (used by diff_view.go, config_view.go, blame_view.go)
// ============================================================================

type detailTabSpec struct {
	label string
	text  string
}

func buildDetailTabLayout(activeTab int, focused, zoomed bool, outerW int) (string, []HitChip) {
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
		activeStyle = lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorBadgeBg)
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
		}
	}

	return b.String(), chips
}

// renderDetailTitledBox renders the shared outer box for Diff, Config, and Blame views
// with embedded, aligned top-border tabs (`Diff · Config · Blame`).
func renderDetailTitledBox(activeTab int, focused, zoomed bool, body string, innerW, innerH int) string {
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
	title, _ := buildDetailTabLayout(activeTab, focused, zoomed, outerW)
	return RenderTitledBox(title, body, outerW, outerH, borderFg, ColorBackground, ColorBackground)
}

// DetailTabHitRanges returns 0-based horizontal hit ranges [StartX, EndX) relative to
// the Detail box left edge, matching the rendered top-border tabs ("diff", "config", "blame").
func DetailTabHitRanges(outerW int, zoomed bool) []HitChip {
	_, chips := buildDetailTabLayout(0, true, zoomed, outerW)
	return chips
}

// renderCommitContextStrip renders a compact 1-row metadata summary strip below the Detail toolbar.
func renderCommitContextStrip(commit, baseCommit *models.TimelineCommit, vsLive bool, width int) string {
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
		if commit.Author != "" {
			appendPart(sep, lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(commit.Author))
		}
		if stat := formatDiffStatTokens(commit.DiffStat, bg); stat != "" {
			appendPart(sep, stat)
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

func renderCompactScrollStatus(vp viewport.Model) string {
	total := vp.TotalLineCount()
	if total <= 0 {
		return ""
	}
	bot := vp.YOffset + vp.Height
	if bot > total {
		bot = total
	}
	var text string
	var fg lipgloss.Color
	switch {
	case vp.AtTop():
		text = fmt.Sprintf("%s %d/%d", GlyphUp, bot, total)
		fg = ColorMuted
	case vp.AtBottom():
		text = fmt.Sprintf("%s %d/%d", GlyphCheck, bot, total)
		fg = ColorSuccess
	default:
		pct := int(vp.ScrollPercent() * 100)
		text = fmt.Sprintf("%d%% %d/%d", pct, bot, total)
		fg = ColorActive
	}
	return lipgloss.NewStyle().Foreground(fg).Background(ColorSurface).Render(text)
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

func gutterDigitsForCount(total int) int {
	if total < 100 {
		return 3
	}
	return len(strconv.Itoa(total))
}

// formatGutteredWrappedLines renders a single logical line with a dimmed line-number gutter
// and wraps its content cleanly onto continuation rows aligned after the gutter.
func formatGutteredWrappedLines(lineNo, digits int, styledContent string, totalW int, bg, defaultFg lipgloss.Color) []string {
	if totalW <= digits+8 {
		// Too narrow for a separate gutter; paint content directly.
		wrapped := PaintLine(styledContent, totalW, bg, defaultFg)
		return strings.Split(wrapped, "\n")
	}

	gutterW := digits + 2 // e.g. "  1 │" -> digits + space + pipe + space? Keep compact: "%*d " + "│" + " " = digits+3, or "%*d " = digits+1
	usePipe := totalW >= 36
	if usePipe {
		gutterW = digits + 3 // "%*d │ "
	} else {
		gutterW = digits + 2 // "%*d  "
	}
	contentW := totalW - gutterW
	if contentW < 4 {
		contentW = 4
	}

	wrappedContent := PaintLine(styledContent, contentW, bg, defaultFg)
	cLines := strings.Split(wrappedContent, "\n")

	numStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg)
	pipeStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(bg)
	sp := lipgloss.NewStyle().Background(bg).Render(" ")

	var firstGutter, contGutter string
	if usePipe {
		firstGutter = numStyle.Render(fmt.Sprintf("%*d", digits, lineNo)) + sp + pipeStyle.Render(GlyphPipe) + sp
		contGutter = numStyle.Render(strings.Repeat(" ", digits)) + sp + pipeStyle.Render(GlyphPipe) + sp
	} else {
		firstGutter = numStyle.Render(fmt.Sprintf("%*d", digits, lineNo)) + lipgloss.NewStyle().Background(bg).Render("  ")
		contGutter = lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gutterW))
	}

	out := make([]string, 0, len(cLines))
	for i, cl := range cLines {
		if i == 0 {
			out = append(out, PaintBarLine(firstGutter+cl, totalW, bg, defaultFg))
		} else {
			out = append(out, PaintBarLine(contGutter+cl, totalW, bg, defaultFg))
		}
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
	rest := line

	// 1. Optional leading diff sign (+ / -)
	if strings.HasPrefix(rest, "+") {
		signStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Background(bg)
		b.WriteString(signStyle.Render("+"))
		rest = rest[1:]
		if strings.HasPrefix(rest, " ") {
			b.WriteString(lipgloss.NewStyle().Background(bg).Render(" "))
			rest = rest[1:]
		}
	} else if strings.HasPrefix(rest, "-") {
		signStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorDanger).Background(bg)
		b.WriteString(signStyle.Render("-"))
		rest = rest[1:]
		if strings.HasPrefix(rest, " ") {
			b.WriteString(lipgloss.NewStyle().Background(bg).Render(" "))
			rest = rest[1:]
		}
	}

	// Preserve any extra leading spaces
	trimmedLeft := strings.TrimLeft(rest, " ")
	if padLen := len(rest) - len(trimmedLeft); padLen > 0 {
		b.WriteString(lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", padLen)))
		rest = trimmedLeft
	}

	// 2. Dim `set /` or `delete /` command prefix
	cmdStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg)
	hasCmdPrefix := false
	for _, prefix := range []string{"set / ", "delete / ", "set /", "delete /"} {
		if strings.HasPrefix(rest, prefix) {
			b.WriteString(cmdStyle.Render(prefix))
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

	prevWord := ""
	for idx, tok := range tokens {
		if tok.isSpace {
			b.WriteString(lipgloss.NewStyle().Background(bg).Render(tok.text))
			continue
		}

		// Subsystem token
		if idx == subsystemIdx && (hasCmdPrefix || isKnownCLISubsystem(tok.text)) {
			subFg := cliSubsystemColor(tok.text)
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(subFg).Background(bg).Render(tok.text))
			prevWord = strings.ToLower(tok.text)
			continue
		}

		// Quoted string or explicit leaf value
		if tok.quoted {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorSyntaxValue).Background(bg).Render(tok.text))
			prevWord = ""
			continue
		}

		// Emphasize list keys immediately following a list-parent keyword
		if cliListKeyParents[prevWord] && !isCLIStructuralKeyword(tok.text) {
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Background(bg).Render(tok.text))
			prevWord = ""
			continue
		}

		// Final token on a 3+ token statement is typically the leaf value
		if idx == lastArgIdx {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorSyntaxValue).Background(bg).Render(tok.text))
			prevWord = strings.ToLower(tok.text)
			continue
		}

		// Intermediate container / leaf name
		b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Background(bg).Render(tok.text))
		prevWord = strings.ToLower(tok.text)
	}

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
