package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/filter"
	"timeline/pkg/models"
	"timeline/pkg/normalizer"
)

// ConfigViewModel manages the snapshot configuration viewer.
type ConfigViewModel struct {
	ConfigData           map[string]interface{}
	FormatMode           string // "cli", "json"
	FilterPath           string
	FilterQuery          string
	DetailFilterBypass   bool
	ShownLineCount       int
	TotalUnfilteredLines int
	Zoomed               bool
	Commit               *models.TimelineCommit
	BaseCommit           *models.TimelineCommit
	lastRendered         string
	renderedLines        []string
	emptyMsg             string
	Viewport             viewport.Model
	Width                int
	Height               int
	IsFocused            bool
}

func (m *ConfigViewModel) setViewportLines(lines []string) {
	m.renderedLines = lines
	m.Viewport.SetContent(strings.Join(lines, "\n"))
}

// NewConfigViewModel creates a new configuration viewer.
func NewConfigViewModel(width, height int) ConfigViewModel {
	m := ConfigViewModel{
		ConfigData: make(map[string]interface{}),
		FormatMode: "cli",
		Width:      width,
		Height:     height,
		IsFocused:  false,
	}
	m.Viewport = viewport.New(m.viewportWidth(), m.viewportHeight())
	return m
}

func (m ConfigViewModel) effectiveFilterQuery() string {
	if !m.DetailFilterBypass {
		if q := strings.TrimSpace(m.FilterPath); q != "" {
			return q
		}
		return strings.TrimSpace(m.FilterQuery)
	}
	if q := strings.TrimSpace(m.FilterQuery); q != "" {
		return q
	}
	return strings.TrimSpace(m.FilterPath)
}

func (m ConfigViewModel) hasContextStrip() bool {
	return m.Height >= 16 && (m.Commit != nil || m.BaseCommit != nil)
}

func (m ConfigViewModel) hasFilterBanner() bool {
	return m.effectiveFilterQuery() != "" && m.Height >= 10
}

// HeaderRows returns the number of inner rows above the scrollable viewport.
func (m ConfigViewModel) HeaderRows() int {
	rows := 1 // toolbar
	if m.hasContextStrip() {
		rows++
	}
	if m.hasFilterBanner() {
		rows++
	}
	return rows
}

// FilterBannerRow returns the 0-based inner row index of the filter scope banner,
// or -1 when no filter banner is displayed.
func (m ConfigViewModel) FilterBannerRow() int {
	if !m.hasFilterBanner() {
		return -1
	}
	if m.hasContextStrip() {
		return 2
	}
	return 1
}

func (m ConfigViewModel) buildFilterBanner(innerW int) (string, int, int) {
	q := m.effectiveFilterQuery()
	if q == "" || !m.hasFilterBanner() {
		return "", -1, -1
	}
	shown := m.ShownLineCount
	total := m.TotalUnfilteredLines
	if total < shown {
		total = shown
	}
	return buildDetailFilterBanner(q, m.DetailFilterBypass, shown, total, "lines", "Full Config", innerW)
}

// FilterBannerHitRange returns the clickable [startX, endX) range of the Full Config / Re-scope
// toggle button on FilterBannerRow(), relative to inner pane X=0. Returns (-1, -1) if not shown.
func (m ConfigViewModel) FilterBannerHitRange() (startX, endX int) {
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

func (m ConfigViewModel) viewportWidth() int {
	if m.Width > 4 {
		return m.Width - 1
	}
	if m.Width < 1 {
		return 1
	}
	return m.Width
}

func (m ConfigViewModel) viewportHeight() int {
	h := m.Height - m.HeaderRows()
	if h < 1 {
		h = 1
	}
	return h
}

// SetConfig updates the config data and renders viewport.
func (m *ConfigViewModel) SetConfig(cfg map[string]interface{}, filterPath string) {
	m.emptyMsg = ""
	m.ConfigData = cfg
	m.FilterPath = filterPath
	if !m.DetailFilterBypass {
		m.FilterQuery = filterPath
	} else if strings.TrimSpace(filterPath) != "" {
		m.FilterQuery = filterPath
	}
	m.lastRendered = ""
	m.UpdateContent()
}

// SetConfigScoped updates the config data with an explicit timeline filter query and detail bypass mode.
func (m *ConfigViewModel) SetConfigScoped(cfg map[string]interface{}, filterQuery string, bypass bool) {
	m.emptyMsg = ""
	m.ConfigData = cfg
	m.DetailFilterBypass = bypass
	m.FilterQuery = filterQuery
	if bypass {
		m.FilterPath = ""
	} else {
		m.FilterPath = filterQuery
	}
	m.lastRendered = ""
	m.UpdateContent()
}

// ResetScroll resets the viewport scroll position to the top.
func (m *ConfigViewModel) ResetScroll() {
	m.Viewport.SetYOffset(0)
}

// Clear resets the config view to an empty state with the given message.
func (m *ConfigViewModel) Clear(message string) {
	m.ConfigData = nil
	m.FilterPath = ""
	m.FilterQuery = ""
	m.DetailFilterBypass = false
	m.ShownLineCount = 0
	m.TotalUnfilteredLines = 0
	m.Commit = nil
	m.BaseCommit = nil
	m.lastRendered = ""
	m.emptyMsg = strings.TrimSpace(message)
	m.Viewport.SetYOffset(0)
	m.UpdateContent()
}

// SetSize handles terminal resizing without needlessly re-rendering when width is unchanged.
func (m *ConfigViewModel) SetSize(width, height int) {
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

// UpdateContent syntax highlights and loads the configuration into the viewport.
func (m *ConfigViewModel) UpdateContent() {
	vpW := m.viewportWidth()
	vpH := m.viewportHeight()
	m.Viewport.Width = vpW
	m.Viewport.Height = vpH

	wrapW := vpW
	if wrapW < 4 {
		wrapW = 4
	}

	if m.emptyMsg != "" {
		m.lastRendered = ""
		m.ShownLineCount = 0
		m.TotalUnfilteredLines = 0
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  " + m.emptyMsg)
		m.setViewportLines([]string{
			PaintLine("", wrapW, ColorPanel, ColorMuted),
			PaintLine(msg, wrapW, ColorPanel, ColorMuted),
		})
		m.Viewport.SetYOffset(0)
		return
	}

	if len(m.ConfigData) == 0 {
		m.lastRendered = ""
		m.ShownLineCount = 0
		m.TotalUnfilteredLines = 0
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  No configuration data available for this revision.")
		m.setViewportLines([]string{
			PaintLine("", wrapW, ColorPanel, ColorMuted),
			PaintLine(msg, wrapW, ColorPanel, ColorMuted),
		})
		return
	}

	activeQ := m.effectiveFilterQuery()
	highlightMatches := m.DetailFilterBypass && activeQ != ""
	var outLines []string

	if m.FormatMode != "json" {
		allLines := normalizer.JSONToFlatCLI(m.ConfigData, "")
		m.TotalUnfilteredLines = len(allLines)
		lines := allLines
		if !m.DetailFilterBypass && activeQ != "" {
			lines = filter.FilterCLILines(allLines, activeQ)
		}
		m.ShownLineCount = len(lines)
		if len(lines) == 0 {
			m.lastRendered = ""
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("  No CLI configuration statements match filter '%s'", activeQ))
			m.setViewportLines([]string{
				PaintLine("", wrapW, ColorPanel, ColorWarning),
				PaintLine(warn, wrapW, ColorPanel, ColorWarning),
			})
			return
		}

		digits := gutterDigitsForCount(len(lines))
		for idx, l := range lines {
			styled := highlightSRLinuxCLI(l, ColorPanel)
			matched := highlightMatches && filter.LineMatchesQuery(l, activeQ)
			outLines = append(outLines, formatGutteredWrappedLinesMarked(idx+1, digits, styled, wrapW, ColorPanel, ColorText, matched)...)
		}
	} else {
		fullJSONStr, _ := normalizer.CanonicalJSONString(m.ConfigData, 2)
		totalJSONLines := 0
		if fullJSONStr != "" {
			totalJSONLines = strings.Count(fullJSONStr, "\n") + 1
		}
		m.TotalUnfilteredLines = totalJSONLines

		var lines []string
		if !m.DetailFilterBypass && activeQ != "" {
			data := filter.FilterConfigSubtree(m.ConfigData, activeQ)
			if len(data) == 0 {
				m.ShownLineCount = 0
				m.lastRendered = ""
				warn := lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("  No JSON configuration subtrees match filter '%s'", activeQ))
				m.setViewportLines([]string{
					PaintLine("", wrapW, ColorPanel, ColorWarning),
					PaintLine(warn, wrapW, ColorPanel, ColorWarning),
				})
				return
			}
			jsonStr, _ := normalizer.CanonicalJSONString(data, 2)
			lines = strings.Split(jsonStr, "\n")
		} else {
			lines = strings.Split(fullJSONStr, "\n")
		}
		m.ShownLineCount = len(lines)

		digits := gutterDigitsForCount(len(lines))
		for idx, l := range lines {
			styled := highlightJSONLine(l, ColorPanel)
			matched := highlightMatches && filter.LineMatchesQuery(l, activeQ)
			outLines = append(outLines, formatGutteredWrappedLinesMarked(idx+1, digits, styled, wrapW, ColorPanel, ColorTextPrimary, matched)...)
		}
	}

	outLines = append(outLines,
		PaintLine("", wrapW, ColorPanel, ColorMuted),
		PaintLine(lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Background(ColorPanel).Render("─── End of Configuration ───"), wrapW, ColorPanel, ColorMuted),
	)

	content := strings.Join(outLines, "\n")
	if content == m.lastRendered {
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

// highlightJSONLine performs quote-aware JSON syntax highlighting using cached SGR prefixes
// so IPv6 addresses, MAC addresses, timestamps, booleans, numbers, and null values render accurately and fast.
func highlightJSONLine(line string, bg lipgloss.Color) string {
	var b strings.Builder
	b.Grow(len(line) + 64)
	keySGR := styleSGR(bg, ColorSyntaxInterface, true, false, false)
	strSGR := styleSGR(bg, ColorSyntaxValue, false, false, false)
	punctSGR := styleSGR(bg, ColorMuted, false, false, false)
	numSGR := styleSGR(bg, ColorSyntaxACL, false, false, false)
	boolSGR := styleSGR(bg, ColorPurple, true, false, false)
	nullSGR := styleSGR(bg, ColorMuted, false, true, false)
	spaceSGR := styleSGR(bg, "", false, false, false)
	defaultSGR := styleSGR(bg, ColorText, false, false, false)

	n := len(line)
	i := 0
	for i < n {
		ch := line[i]

		// Whitespace
		if ch == ' ' || ch == '\t' {
			start := i
			for i < n && (line[i] == ' ' || line[i] == '\t') {
				i++
			}
			b.WriteString(wrapSGR(spaceSGR, line[start:i]))
			continue
		}

		// Quoted string: check if followed by ':' (JSON key) or not (JSON string value)
		if ch == '"' {
			start := i
			i++
			for i < n {
				if line[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
			lit := line[start:i]
			// Look ahead past spaces/tabs for ':'
			look := i
			for look < n && (line[look] == ' ' || line[look] == '\t') {
				look++
			}
			if look < n && line[look] == ':' {
				b.WriteString(wrapSGR(keySGR, lit))
			} else {
				b.WriteString(wrapSGR(strSGR, lit))
			}
			continue
		}

		// Structural punctuation (coalesce consecutive punctuation like `},` or `": `)
		if ch == '{' || ch == '}' || ch == '[' || ch == ']' || ch == ':' || ch == ',' {
			start := i
			i++
			for i < n {
				c := line[i]
				if c == '{' || c == '}' || c == '[' || c == ']' || c == ':' || c == ',' {
					i++
					continue
				}
				break
			}
			b.WriteString(wrapSGR(punctSGR, line[start:i]))
			continue
		}

		// Numbers: -?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?
		if ch == '-' || (ch >= '0' && ch <= '9') {
			start := i
			if ch == '-' {
				i++
			}
			for i < n && ((line[i] >= '0' && line[i] <= '9') || line[i] == '.' || line[i] == 'e' || line[i] == 'E' || line[i] == '+' || line[i] == '-') {
				i++
			}
			b.WriteString(wrapSGR(numSGR, line[start:i]))
			continue
		}

		// Keywords: true, false, null
		if strings.HasPrefix(line[i:], "true") && isJSONDelimAfter(line, i+4) {
			b.WriteString(wrapSGR(boolSGR, "true"))
			i += 4
			continue
		}
		if strings.HasPrefix(line[i:], "false") && isJSONDelimAfter(line, i+5) {
			b.WriteString(wrapSGR(boolSGR, "false"))
			i += 5
			continue
		}
		if strings.HasPrefix(line[i:], "null") && isJSONDelimAfter(line, i+4) {
			b.WriteString(wrapSGR(nullSGR, "null"))
			i += 4
			continue
		}

		// Fallback single character
		b.WriteString(wrapSGR(defaultSGR, line[i:i+1]))
		i++
	}

	return b.String()
}

func isJSONDelimAfter(s string, pos int) bool {
	if pos >= len(s) {
		return true
	}
	switch s[pos] {
	case ' ', '\t', ',', '}', ']', ':':
		return true
	default:
		return false
	}
}

func (m ConfigViewModel) buildToolbar(innerW int) (string, []HitChip) {
	compact := innerW < compactToolbarThreshold
	var specs []toolbarPillSpec
	if compact {
		padH := 1
		if innerW < tightToolbarPadThreshold {
			padH = 0
		}
		specs = []toolbarPillSpec{
			{label: "cli", text: "1:CLI", active: m.FormatMode != "json", padH: padH, fixedPad: true},
			{label: "json", text: "2:JSON", active: m.FormatMode == "json", padH: padH, fixedPad: true},
		}
	} else {
		specs = []toolbarPillSpec{
			{label: "cli", text: "[1] CLI", active: m.FormatMode != "json", padH: 1},
			{label: "json", text: "[2] JSON", active: m.FormatMode == "json", padH: 1},
		}
	}
	scrollPrimary, scrollFallback := renderToolbarScrollStatuses(m.Viewport, compact)
	return buildToolbarLine(specs, m.IsFocused, scrollPrimary, scrollFallback, innerW)
}

// ControlHitRanges returns clickable toolbar pill hit ranges relative to the inner pane X=0.
func (m ConfigViewModel) ControlHitRanges() []HitChip {
	innerW := m.Width
	if innerW < 1 {
		innerW = 1
	}
	_, chips := m.buildToolbar(innerW)
	return chips
}

// View renders the configuration panel inside the shared border-titled detail box.
func (m ConfigViewModel) View() string {
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

	vpH := innerH - len(sections)
	if vpH < 1 {
		vpH = 1
	}
	vpView := renderLinesWithScrollbar(m.renderedLines, m.Viewport, innerW, vpH, ColorPanel)
	sections = append(sections, vpView)

	body := strings.Join(sections, "\n")
	return renderDetailTitledBox(int(TabConfig), m.IsFocused, m.Zoomed, body, innerW, innerH)
}
