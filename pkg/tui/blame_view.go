package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"timeline/pkg/blame"
	"timeline/pkg/filter"
	"timeline/pkg/models"
)

// BlameViewModel manages the configuration blame view and contributor analytics.
type BlameViewModel struct {
	Entries                []models.BlameEntry
	Stats                  blame.ContributorStats
	ViewMode               string // "attribution" (default), "analytics"
	WrapLines              bool
	SelectedLine           int
	FilterQuery            string
	DetailFilterBypass     bool
	TotalUnfilteredEntries int
	ShownEntryCount        int
	Zoomed                 bool
	Commit                 *models.TimelineCommit
	BaseCommit             *models.TimelineCommit
	lastRendered           string
	renderedLines          []string
	emptyMsg               string
	Viewport               viewport.Model
	Width                  int
	Height                 int
	IsFocused              bool

	lineRowStarts       []int
	lineRowEnds         []int
	lastRenderedFocused bool
	lastRenderedMode    string
	lastRenderedWrap    bool
	lastRenderedFilter  string
	lastRenderedBypass  bool
}

func (m *BlameViewModel) setViewportLines(lines []string) {
	m.renderedLines = lines
	m.Viewport.SetContent(strings.Join(lines, "\n"))
}

// NewBlameViewModel creates a new blame viewer (defaulting to Attribution mode).
func NewBlameViewModel(width, height int) BlameViewModel {
	m := BlameViewModel{
		Entries:   []models.BlameEntry{},
		ViewMode:  "attribution",
		Width:     width,
		Height:    height,
		IsFocused: false,
	}
	m.Viewport = viewport.New(m.viewportWidth(), m.viewportHeight())
	return m
}

func (m BlameViewModel) isAnalytics() bool {
	return m.ViewMode == "analytics"
}

func (m BlameViewModel) hasContextStrip() bool {
	return m.Height >= 16 && (m.Commit != nil || m.BaseCommit != nil)
}

func (m BlameViewModel) hasFilterBanner() bool {
	return strings.TrimSpace(m.FilterQuery) != "" && m.Height >= 10
}

func (m BlameViewModel) hasPinnedHeader() bool {
	return !m.isAnalytics() && m.emptyMsg == "" && len(m.Entries) > 0
}

// HeaderRows returns the number of inner rows above the scrollable viewport
// (toolbar + optional context strip + optional filter banner + optional pinned column header).
func (m BlameViewModel) HeaderRows() int {
	rows := 1 // toolbar
	if m.hasContextStrip() {
		rows++
	}
	if m.hasFilterBanner() {
		rows++
	}
	if m.hasPinnedHeader() {
		rows++
	}
	return rows
}

// FilterBannerRow returns the 0-based inner row index of the filter scope banner,
// or -1 when no filter banner is displayed.
func (m BlameViewModel) FilterBannerRow() int {
	if !m.hasFilterBanner() {
		return -1
	}
	if m.hasContextStrip() {
		return 2
	}
	return 1
}

func (m BlameViewModel) buildFilterBanner(innerW int) (string, int, int) {
	if !m.hasFilterBanner() {
		return "", -1, -1
	}
	shown := m.ShownEntryCount
	if shown <= 0 && len(m.Entries) > 0 {
		shown = len(m.Entries)
	}
	total := m.TotalUnfilteredEntries
	if total <= 0 && m.Stats.TotalLines > 0 {
		total = m.Stats.TotalLines
	}
	if total < shown {
		total = shown
	}
	return buildDetailFilterBanner(m.FilterQuery, m.DetailFilterBypass, shown, total, "lines", "Full Blame", innerW)
}

// FilterBannerHitRange returns the clickable [startX, endX) range of the Full Blame / Re-scope
// toggle button on FilterBannerRow(), relative to inner pane X=0. Returns (-1, -1) if not shown.
func (m BlameViewModel) FilterBannerHitRange() (startX, endX int) {
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

func (m BlameViewModel) viewportWidth() int {
	if m.Width > 4 {
		return m.Width - 1
	}
	if m.Width < 1 {
		return 1
	}
	return m.Width
}

func (m BlameViewModel) viewportHeight() int {
	h := m.Height - m.HeaderRows()
	if h < 1 {
		h = 1
	}
	return h
}

// SetBlame updates entries and contributor statistics.
func (m *BlameViewModel) SetBlame(entries []models.BlameEntry, stats blame.ContributorStats) {
	m.emptyMsg = ""
	m.Entries = entries
	m.Stats = stats
	if len(entries) == 0 {
		m.SelectedLine = 0
	} else if m.SelectedLine >= len(entries) {
		m.SelectedLine = len(entries) - 1
	} else if m.SelectedLine < 0 {
		m.SelectedLine = 0
	}
	m.lastRendered = ""
	m.UpdateContent()
}

// ResetScroll resets the viewport scroll position and line selection to the top.
func (m *BlameViewModel) ResetScroll() {
	m.SelectedLine = 0
	m.Viewport.SetYOffset(0)
	m.lastRendered = ""
	m.UpdateContent()
}

// Clear resets the blame view to an empty state with the given message.
func (m *BlameViewModel) Clear(message string) {
	m.Entries = nil
	m.Stats = blame.ContributorStats{}
	m.Commit = nil
	m.BaseCommit = nil
	m.FilterQuery = ""
	m.DetailFilterBypass = false
	m.TotalUnfilteredEntries = 0
	m.ShownEntryCount = 0
	m.SelectedLine = 0
	m.lineRowStarts = nil
	m.lineRowEnds = nil
	m.renderedLines = nil
	m.lastRendered = ""
	m.emptyMsg = strings.TrimSpace(message)
	m.Viewport.SetYOffset(0)
	m.UpdateContent()
}

// SelectNext moves the blame line cursor down by one entry in attribution mode.
func (m *BlameViewModel) SelectNext() {
	if m.isAnalytics() || len(m.Entries) == 0 {
		return
	}
	if m.SelectedLine < len(m.Entries)-1 {
		m.SelectedLine++
		m.lastRendered = ""
		m.UpdateContent()
		m.ensureSelectedVisible()
	}
}

// SelectPrev moves the blame line cursor up by one entry in attribution mode.
func (m *BlameViewModel) SelectPrev() {
	if m.isAnalytics() || len(m.Entries) == 0 {
		return
	}
	if m.SelectedLine > 0 {
		m.SelectedLine--
		m.lastRendered = ""
		m.UpdateContent()
		m.ensureSelectedVisible()
	}
}

// SelectedEntry returns the currently selected BlameEntry, or nil if unavailable.
func (m *BlameViewModel) SelectedEntry() *models.BlameEntry {
	if m.isAnalytics() || len(m.Entries) == 0 || m.SelectedLine < 0 || m.SelectedLine >= len(m.Entries) {
		return nil
	}
	return &m.Entries[m.SelectedLine]
}

// EntryAtRow maps a 0-based visible row inside the Blame viewport to its BlameEntry
// (accounting for Viewport.YOffset and wrapped lines), or returns nil if out of bounds
// or in Analytics mode.
func (m *BlameViewModel) EntryAtRow(row int) *models.BlameEntry {
	if m.isAnalytics() || row < 0 || len(m.Entries) == 0 {
		return nil
	}
	absRow := row + m.Viewport.YOffset
	if len(m.lineRowStarts) == len(m.Entries) && len(m.lineRowEnds) == len(m.Entries) {
		for i := range m.Entries {
			if absRow >= m.lineRowStarts[i] && absRow < m.lineRowEnds[i] {
				return &m.Entries[i]
			}
		}
		return nil
	}
	if absRow >= 0 && absRow < len(m.Entries) {
		return &m.Entries[absRow]
	}
	return nil
}

func (m *BlameViewModel) ensureSelectedVisible() {
	if m.isAnalytics() || len(m.Entries) == 0 || m.SelectedLine < 0 || m.SelectedLine >= len(m.lineRowStarts) {
		return
	}
	vpH := m.Viewport.Height
	if vpH < 1 {
		vpH = m.viewportHeight()
	}
	rowStart := m.lineRowStarts[m.SelectedLine]
	rowEnd := m.lineRowEnds[m.SelectedLine] - 1
	if rowEnd < rowStart {
		rowEnd = rowStart
	}

	if rowStart < m.Viewport.YOffset {
		m.Viewport.SetYOffset(rowStart)
	} else if rowEnd >= m.Viewport.YOffset+vpH {
		nextOff := rowEnd - vpH + 1
		if nextOff < 0 {
			nextOff = 0
		}
		m.Viewport.SetYOffset(nextOff)
	}
}

// SetSize handles terminal resizing without needlessly re-rendering when width is unchanged.
func (m *BlameViewModel) SetSize(width, height int) {
	if m.Width == width && m.Height == height {
		return
	}
	prevW := m.Width
	prevHasCtx := m.hasContextStrip()
	prevHasBanner := m.hasFilterBanner()
	m.Width = width
	m.Height = height
	m.Viewport.Width = m.viewportWidth()
	m.Viewport.Height = m.viewportHeight()
	if prevW == width && prevHasCtx == m.hasContextStrip() && prevHasBanner == m.hasFilterBanner() && m.lastRendered != "" {
		return
	}
	m.lastRendered = ""
	m.UpdateContent()
}

type blameColSpec struct {
	gutterDigits int
	gutterW      int
	usePipe      bool
	shaW         int
	authorW      int
	dateW        int
	metaW        int
	contentW     int
}

func computeBlameColumns(vpW, totalEntries int) blameColSpec {
	var spec blameColSpec
	if vpW >= 34 {
		spec.gutterDigits = gutterDigitsForCount(totalEntries)
		spec.usePipe = vpW >= 48
		if spec.usePipe {
			spec.gutterW = spec.gutterDigits + 3 // "%*d │ "
		} else {
			spec.gutterW = spec.gutterDigits + 2 // "%*d  "
		}
	}

	avail := vpW - spec.gutterW
	switch {
	case avail >= 64:
		spec.shaW = 8
		spec.authorW = 12
		spec.dateW = 10 // YYYY-MM-DD
		spec.metaW = spec.shaW + 1 + spec.authorW + 1 + spec.dateW + 1
	case avail >= 44:
		spec.shaW = 7
		spec.authorW = 10
		spec.dateW = 10
		spec.metaW = spec.shaW + 1 + spec.authorW + 1 + spec.dateW + 1
	case avail >= 26:
		spec.shaW = 7
		spec.authorW = 8
		spec.dateW = 0
		spec.metaW = spec.shaW + 1 + spec.authorW + 1
	default:
		spec.shaW = 6
		spec.authorW = 0
		spec.dateW = 0
		spec.metaW = spec.shaW + 1
	}

	spec.contentW = avail - spec.metaW
	if spec.contentW < 4 {
		spec.contentW = 4
	}
	return spec
}

func (m BlameViewModel) renderPinnedHeader(innerW int) string {
	vpW := m.viewportWidth()
	cols := computeBlameColumns(vpW, len(m.Entries))

	bg := ColorSurface
	hdrStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Background(bg)
	sp := lipgloss.NewStyle().Background(bg).Render(" ")

	var b strings.Builder
	if cols.gutterW > 0 {
		if cols.usePipe {
			b.WriteString(hdrStyle.Render(fmt.Sprintf("%*s", cols.gutterDigits, "#")))
			b.WriteString(sp)
			b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Background(bg).Render(GlyphPipe))
			b.WriteString(sp)
		} else {
			b.WriteString(lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", cols.gutterW)))
		}
	}

	b.WriteString(hdrStyle.Render(fmt.Sprintf("%-*s", cols.shaW, truncateStringEllipsis("COMMIT", cols.shaW))))
	b.WriteString(sp)
	if cols.authorW > 0 {
		b.WriteString(hdrStyle.Render(fmt.Sprintf("%-*s", cols.authorW, truncateStringEllipsis("AUTHOR", cols.authorW))))
		b.WriteString(sp)
	}
	if cols.dateW > 0 {
		b.WriteString(hdrStyle.Render(fmt.Sprintf("%-*s", cols.dateW, truncateStringEllipsis("DATE", cols.dateW))))
		b.WriteString(sp)
	}
	b.WriteString(hdrStyle.Render(truncateStringEllipsis("CONFIG STATEMENT", cols.contentW)))

	return PaintBarLine(b.String(), innerW, bg, ColorMuted)
}

// UpdateContent builds either the line-by-line Attribution table or the Contributor Analytics report.
func (m *BlameViewModel) UpdateContent() {
	vpW := m.viewportWidth()
	vpH := m.viewportHeight()
	m.Viewport.Width = vpW
	m.Viewport.Height = vpH

	wrapW := vpW
	if wrapW < 4 {
		wrapW = 4
	}

	m.lastRenderedFocused = m.IsFocused
	m.lastRenderedMode = m.ViewMode
	m.lastRenderedWrap = m.WrapLines
	m.lastRenderedFilter = m.FilterQuery
	m.lastRenderedBypass = m.DetailFilterBypass

	if m.emptyMsg != "" {
		m.lastRendered = ""
		m.lineRowStarts = nil
		m.lineRowEnds = nil
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  " + m.emptyMsg)
		m.setViewportLines([]string{
			PaintLine("", wrapW, ColorPanel, ColorMuted),
			PaintLine(msg, wrapW, ColorPanel, ColorMuted),
		})
		m.Viewport.SetYOffset(0)
		return
	}

	if len(m.Entries) == 0 && (!m.isAnalytics() || m.Stats.TotalLines == 0 || (strings.TrimSpace(m.FilterQuery) != "" && !m.DetailFilterBypass)) {
		m.lastRendered = ""
		m.lineRowStarts = nil
		m.lineRowEnds = nil
		var msgText string
		if q := strings.TrimSpace(m.FilterQuery); q != "" && !m.DetailFilterBypass {
			msgText = fmt.Sprintf("  No blame entries match filter '%s'.", q)
		} else {
			msgText = "  No blame entries available."
		}
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render(msgText)
		m.setViewportLines([]string{
			PaintLine("", wrapW, ColorPanel, ColorMuted),
			PaintLine(msg, wrapW, ColorPanel, ColorMuted),
		})
		m.Viewport.SetYOffset(0)
		return
	}

	if m.isAnalytics() {
		m.renderAnalyticsContent(wrapW)
		return
	}

	m.renderAttributionContent(wrapW)
}

func (m *BlameViewModel) renderAttributionContent(wrapW int) {
	cols := computeBlameColumns(wrapW, len(m.Entries))
	outLines := make([]string, 0, len(m.Entries)+2)
	starts := make([]int, len(m.Entries))
	ends := make([]int, len(m.Entries))
	highlightMatches := m.DetailFilterBypass && strings.TrimSpace(m.FilterQuery) != ""

	for i, e := range m.Entries {
		isSelected := i == m.SelectedLine
		rowBg := ColorPanel
		if isSelected {
			if m.IsFocused {
				rowBg = ColorHighlight
			} else {
				rowBg = ColorSurface
			}
		}

		isMatch := highlightMatches && filter.LineMatchesQuery(e.Content, m.FilterQuery)
		sp := lipgloss.NewStyle().Background(rowBg).Render(" ")
		numStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(rowBg)
		pipeStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(rowBg)
		if isMatch {
			numStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Background(rowBg)
			pipeStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Background(rowBg)
		}

		var prefix strings.Builder
		if cols.gutterW > 0 {
			if cols.usePipe {
				glyph := GlyphPipe
				if isMatch {
					glyph = GlyphCollapsed
				}
				prefix.WriteString(numStyle.Render(fmt.Sprintf("%*d", cols.gutterDigits, i+1)))
				prefix.WriteString(sp)
				prefix.WriteString(pipeStyle.Render(glyph))
				prefix.WriteString(sp)
			} else {
				prefix.WriteString(numStyle.Render(fmt.Sprintf("%*d", cols.gutterDigits, i+1)))
				if isMatch {
					prefix.WriteString(pipeStyle.Render(GlyphCollapsed))
					prefix.WriteString(sp)
				} else {
					prefix.WriteString(lipgloss.NewStyle().Background(rowBg).Render("  "))
				}
			}
		}

		// Commit SHA
		shortSHA := e.ShortSHA()
		if len(shortSHA) > cols.shaW {
			shortSHA = shortSHA[:cols.shaW]
		}
		shaStyled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(rowBg).
			Render(fmt.Sprintf("%-*s", cols.shaW, shortSHA))
		prefix.WriteString(shaStyled)
		prefix.WriteString(sp)

		// Author (always safely truncated with … when > 12 or > cols.authorW)
		if cols.authorW > 0 {
			authorText := strings.TrimSpace(e.Author)
			if authorText == "" {
				authorText = "unknown"
			}
			maxAuthor := cols.authorW
			if maxAuthor > 12 {
				maxAuthor = 12
			}
			if lipgloss.Width(authorText) > maxAuthor {
				authorText = truncate.StringWithTail(authorText, uint(maxAuthor), "…")
			}
			authorStyle := StyleAuthorOther.Background(rowBg)
			if e.Author == "admin" || e.Author == "root" {
				authorStyle = StyleAuthorAdmin.Background(rowBg)
			}
			padAuthor := cols.authorW - lipgloss.Width(authorText)
			if padAuthor < 0 {
				padAuthor = 0
			}
			prefix.WriteString(authorStyle.Render(authorText + strings.Repeat(" ", padAuthor)))
			prefix.WriteString(sp)
		}

		// Date
		if cols.dateW > 0 {
			dateStr := ""
			if !e.Timestamp.IsZero() {
				dateStr = e.Timestamp.Format("2006-01-02")
			}
			if len(dateStr) > cols.dateW {
				dateStr = dateStr[:cols.dateW]
			}
			prefix.WriteString(lipgloss.NewStyle().Foreground(ColorTextDim).Background(rowBg).
				Render(fmt.Sprintf("%-*s", cols.dateW, dateStr)))
			prefix.WriteString(sp)
		}

		cleanContent := strings.ReplaceAll(e.Content, "\r\n", " ")
		cleanContent = strings.ReplaceAll(cleanContent, "\n", " ")
		cleanContent = strings.ReplaceAll(cleanContent, "\r", " ")
		cleanContent = strings.ReplaceAll(cleanContent, "\\n", " ")

		starts[i] = len(outLines)

		if !m.WrapLines {
			if lipgloss.Width(cleanContent) > cols.contentW {
				cleanContent = truncate.StringWithTail(cleanContent, uint(cols.contentW), "…")
			}
			styledContent := styleBlameStatement(cleanContent, rowBg)
			outLines = append(outLines, PaintBarLine(prefix.String()+styledContent, wrapW, rowBg, ColorText))
		} else {
			styledContent := styleBlameStatement(cleanContent, rowBg)
			wrapped := PaintLine(styledContent, cols.contentW, rowBg, ColorText)
			wLines := strings.Split(wrapped, "\n")

			contPrefixW := cols.gutterW + cols.metaW
			var contPrefix string
			if cols.gutterW > 0 && cols.usePipe {
				contPrefix = numStyle.Render(strings.Repeat(" ", cols.gutterDigits)) +
					sp + pipeStyle.Render(GlyphPipe) +
					lipgloss.NewStyle().Background(rowBg).Render(strings.Repeat(" ", cols.metaW+1))
			} else {
				contPrefix = lipgloss.NewStyle().Background(rowBg).Render(strings.Repeat(" ", contPrefixW))
			}

			for wi, wl := range wLines {
				if wi == 0 {
					outLines = append(outLines, PaintBarLine(prefix.String()+wl, wrapW, rowBg, ColorText))
				} else {
					outLines = append(outLines, PaintBarLine(contPrefix+wl, wrapW, rowBg, ColorText))
				}
			}
		}

		ends[i] = len(outLines)
	}

	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorMuted),
		PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Background(ColorPanel).Render("─── End of Blame Attribution ───"), wrapW, ColorPanel, ColorMuted),
	)

	m.lineRowStarts = starts
	m.lineRowEnds = ends

	content := strings.Join(outLines, "\n")
	if m.lastRendered == content {
		m.renderedLines = outLines
		return
	}
	m.lastRendered = content
	m.renderedLines = outLines
	prevY := m.Viewport.YOffset
	m.Viewport.SetContent(content)
	if prevY > 0 {
		m.Viewport.SetYOffset(prevY)
	}
}

func styleBlameStatement(stmt string, bg lipgloss.Color) string {
	trimmed := strings.TrimSpace(stmt)
	if strings.HasPrefix(trimmed, "set /") || strings.HasPrefix(trimmed, "delete /") {
		return highlightSRLinuxCLI(stmt, bg)
	}
	return lipgloss.NewStyle().Foreground(ColorText).Background(bg).Render(stmt)
}

func (m *BlameViewModel) renderAnalyticsContent(wrapW int) {
	m.lineRowStarts = nil
	m.lineRowEnds = nil

	filterQ := strings.TrimSpace(m.FilterQuery)
	scopedFilter := filterQ != "" && !m.DetailFilterBypass
	stats := m.Stats
	if scopedFilter || (stats.TotalLines == 0 && len(m.Entries) > 0) {
		stats = computeStatsFromEntries(m.Entries)
	}

	var outLines []string

	// Title header (with filtered annotation when scoped FilterQuery is active)
	title := "CONTRIBUTOR ANALYTICS & SUBSYSTEM BREAKDOWN"
	if scopedFilter {
		title = fmt.Sprintf("CONTRIBUTOR ANALYTICS (filtered: %s)", filterQ)
	}
	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorText),
		PaintLine(" "+lipgloss.NewStyle().Bold(true).Foreground(ColorCyan).Background(ColorPanel).Render(title), wrapW, ColorPanel, ColorCyan),
		PaintLine(" "+lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorPanel).Render(fmt.Sprintf("Total Lines: %d", stats.TotalLines)), wrapW, ColorPanel, ColorMuted),
		PaintLine("", wrapW, ColorPanel, ColorText),
	)

	// Author Contributions sorted by count descending, then name ascending
	outLines = append(outLines,
		PaintLine(" "+lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorPanel).Render("Author Contributions"), wrapW, ColorPanel, ColorActive),
	)

	type authorRow struct {
		name string
		stat blame.AuthorStat
	}
	authors := make([]authorRow, 0, len(stats.Authors))
	for name, st := range stats.Authors {
		authors = append(authors, authorRow{name: name, stat: st})
	}
	sort.Slice(authors, func(i, j int) bool {
		if authors[i].stat.Count != authors[j].stat.Count {
			return authors[i].stat.Count > authors[j].stat.Count
		}
		return authors[i].name < authors[j].name
	})

	maxBarW := wrapW - 38
	if maxBarW > 20 {
		maxBarW = 20
	}
	if maxBarW < 4 {
		maxBarW = 4
	}

	for _, a := range authors {
		displayAuthor := a.name
		if lipgloss.Width(displayAuthor) > 12 {
			displayAuthor = truncate.StringWithTail(displayAuthor, 12, "…")
		}
		barLen := int(math.Round((a.stat.Percentage / 100.0) * float64(maxBarW)))
		if barLen < 1 && a.stat.Count > 0 {
			barLen = 1
		}
		if barLen > maxBarW {
			barLen = maxBarW
		}
		authorSt := StyleAuthorOther.Background(ColorPanel)
		if a.name == "admin" || a.name == "root" {
			authorSt = StyleAuthorAdmin.Background(ColorPanel)
		}
		padAuthor := 12 - lipgloss.Width(displayAuthor)
		if padAuthor < 0 {
			padAuthor = 0
		}
		bullet := lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorPanel).Render("  " + GlyphLive + " ")
		metrics := lipgloss.NewStyle().Foreground(ColorText).Background(ColorPanel).
			Render(fmt.Sprintf(" %4d lines (%5.1f%%) ", a.stat.Count, a.stat.Percentage))
		bar := lipgloss.NewStyle().Foreground(ColorSuccess).Background(ColorPanel).Render(strings.Repeat(GlyphScrollThumb, barLen))

		row := bullet + authorSt.Render(displayAuthor+strings.Repeat(" ", padAuthor)) + metrics + bar
		outLines = append(outLines, PaintLine(row, wrapW, ColorPanel, ColorText))
	}

	// Subsystem Breakdown sorted by total lines descending, then subsystem name
	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorText),
		PaintLine(" "+lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Background(ColorPanel).Render("Subsystem Breakdown"), wrapW, ColorPanel, ColorWarning),
	)

	type subRow struct {
		name    string
		total   int
		authors map[string]int
	}
	var subs []subRow
	for sub, authMap := range stats.Subsystems {
		subTotal := 0
		for _, c := range authMap {
			subTotal += c
		}
		if subTotal == 0 {
			continue
		}
		subs = append(subs, subRow{name: sub, total: subTotal, authors: authMap})
	}
	sort.Slice(subs, func(i, j int) bool {
		if subs[i].total != subs[j].total {
			return subs[i].total > subs[j].total
		}
		return subs[i].name < subs[j].name
	})

	for _, sr := range subs {
		subHdr := lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(ColorPanel).
			Render(fmt.Sprintf("  [%s] - %d lines", strings.ToUpper(sr.name), sr.total))
		outLines = append(outLines, PaintLine(subHdr, wrapW, ColorPanel, ColorText))

		subAuthors := make([]string, 0, len(sr.authors))
		for a := range sr.authors {
			subAuthors = append(subAuthors, a)
		}
		sort.Slice(subAuthors, func(i, j int) bool {
			if sr.authors[subAuthors[i]] != sr.authors[subAuthors[j]] {
				return sr.authors[subAuthors[i]] > sr.authors[subAuthors[j]]
			}
			return subAuthors[i] < subAuthors[j]
		})
		for _, a := range subAuthors {
			displayAuthor := a
			if lipgloss.Width(displayAuthor) > 12 {
				displayAuthor = truncate.StringWithTail(displayAuthor, 12, "…")
			}
			subLine := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorPanel).
				Render(fmt.Sprintf("     └─ %-12s : %d", displayAuthor, sr.authors[a]))
			outLines = append(outLines, PaintLine(subLine, wrapW, ColorPanel, ColorMuted))
		}
	}

	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorMuted),
		PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Background(ColorPanel).Render("─── End of Contributor Analytics ───"), wrapW, ColorPanel, ColorMuted),
	)

	content := strings.Join(outLines, "\n")
	if m.lastRendered == content {
		m.renderedLines = outLines
		return
	}
	m.lastRendered = content
	m.renderedLines = outLines
	prevY := m.Viewport.YOffset
	m.Viewport.SetContent(content)
	if prevY > 0 {
		m.Viewport.SetYOffset(prevY)
	}
}

func computeStatsFromEntries(entries []models.BlameEntry) blame.ContributorStats {
	totalLines := len(entries)
	authorsCount := make(map[string]int)
	subsystems := map[string]map[string]int{
		"acl":              make(map[string]int),
		"interfaces":       make(map[string]int),
		"network-instance": make(map[string]int),
		"system":           make(map[string]int),
		"other":            make(map[string]int),
	}

	for _, entry := range entries {
		author := entry.Author
		if author == "" {
			author = "unknown"
		}
		authorsCount[author]++

		sub := classifyBlameSubsystem(entry.Content)
		subsystems[sub][author]++
	}

	authors := make(map[string]blame.AuthorStat, len(authorsCount))
	for author, count := range authorsCount {
		pct := 0.0
		if totalLines > 0 {
			pct = math.Round((float64(count)/float64(totalLines))*1000.0) / 10.0
		}
		authors[author] = blame.AuthorStat{
			Count:      count,
			Percentage: pct,
		}
	}

	return blame.ContributorStats{
		TotalLines: totalLines,
		Authors:    authors,
		Subsystems: subsystems,
	}
}

func classifyBlameSubsystem(content string) string {
	trimmed := strings.ToLower(strings.TrimSpace(content))
	if strings.HasPrefix(trimmed, "set / system ") || trimmed == "set / system" || strings.HasPrefix(trimmed, "/system") {
		return "system"
	}
	if strings.HasPrefix(trimmed, "set / network-instance ") || trimmed == "set / network-instance" || strings.HasPrefix(trimmed, "/network-instance") {
		return "network-instance"
	}
	if strings.HasPrefix(trimmed, "set / interface ") || trimmed == "set / interface" || strings.HasPrefix(trimmed, "/interface") {
		return "interfaces"
	}
	if strings.HasPrefix(trimmed, "set / acl ") || trimmed == "set / acl" || strings.HasPrefix(trimmed, "/acl") {
		return "acl"
	}
	if strings.Contains(trimmed, " network-instance") || strings.Contains(trimmed, "/network-instance") {
		return "network-instance"
	}
	if strings.Contains(trimmed, " system") || strings.Contains(trimmed, "/system") {
		return "system"
	}
	if strings.Contains(trimmed, " interface") || strings.Contains(trimmed, "/interface") {
		return "interfaces"
	}
	if strings.Contains(trimmed, " acl ") || strings.Contains(trimmed, "/acl") {
		return "acl"
	}
	return "other"
}

func (m BlameViewModel) buildToolbar(innerW int) (string, []HitChip) {
	compact := innerW < compactToolbarThreshold
	var specs []toolbarPillSpec
	if compact {
		padH := 1
		if innerW < tightToolbarPadThreshold {
			padH = 0
		}
		specs = []toolbarPillSpec{
			{label: "attribution", text: "1:Attr", active: !m.isAnalytics(), padH: padH, fixedPad: true},
			{label: "analytics", text: "2:Stats", active: m.isAnalytics(), padH: padH, fixedPad: true},
			{label: "wrap", text: "w:Wrap", active: m.WrapLines, padH: padH, fixedPad: true},
		}
	} else {
		specs = []toolbarPillSpec{
			{label: "attribution", text: "[1] Attribution", active: !m.isAnalytics(), padH: 1},
			{label: "analytics", text: "[2] Analytics", active: m.isAnalytics(), padH: 1},
			{label: "wrap", text: "[w] Wrap", active: m.WrapLines, padH: 1},
		}
	}
	scrollPrimary, scrollFallback := renderToolbarScrollStatuses(m.Viewport, compact)
	return buildToolbarLine(specs, m.IsFocused, scrollPrimary, scrollFallback, innerW)
}

// ControlHitRanges returns clickable toolbar pill hit ranges relative to the inner pane X=0.
func (m BlameViewModel) ControlHitRanges() []HitChip {
	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	_, chips := m.buildToolbar(innerW)
	return chips
}

// View renders the blame panel inside the shared border-titled detail box.
func (m BlameViewModel) View() string {
	if m.IsFocused != m.lastRenderedFocused ||
		m.ViewMode != m.lastRenderedMode ||
		m.WrapLines != m.lastRenderedWrap ||
		m.FilterQuery != m.lastRenderedFilter ||
		m.DetailFilterBypass != m.lastRenderedBypass {
		m.lastRendered = ""
		m.UpdateContent()
	}

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
		sections = append(sections, renderCommitContextStrip(m.Commit, m.BaseCommit, false, nil, innerW))
	}
	if m.hasFilterBanner() {
		bannerLine, _, _ := m.buildFilterBanner(innerW)
		sections = append(sections, bannerLine)
	}
	if m.hasPinnedHeader() {
		sections = append(sections, m.renderPinnedHeader(innerW))
	}

	vpH := innerH - len(sections)
	if vpH < 1 {
		vpH = 1
	}
	vpView := renderLinesWithScrollbar(m.renderedLines, m.Viewport, innerW, vpH, ColorPanel)
	sections = append(sections, vpView)

	body := strings.Join(sections, "\n")
	return renderDetailTitledBox(int(TabBlame), m.IsFocused, m.Zoomed, body, innerW, innerH)
}
