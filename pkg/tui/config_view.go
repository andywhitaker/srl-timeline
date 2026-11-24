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
	ConfigData   map[string]interface{}
	FormatMode   string // "cli", "json"
	FilterPath   string
	Zoomed       bool
	Commit       *models.TimelineCommit
	BaseCommit   *models.TimelineCommit
	lastRendered string
	emptyMsg     string
	Viewport     viewport.Model
	Width        int
	Height       int
	IsFocused    bool
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

func (m ConfigViewModel) hasContextStrip() bool {
	return m.Height >= 16 && (m.Commit != nil || m.BaseCommit != nil)
}

// HeaderRows returns the number of inner rows above the scrollable viewport.
func (m ConfigViewModel) HeaderRows() int {
	rows := 1 // toolbar
	if m.hasContextStrip() {
		rows++
	}
	return rows
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
	m.Commit = nil
	m.BaseCommit = nil
	m.lastRendered = ""
	m.emptyMsg = strings.TrimSpace(message)
	m.Viewport.SetYOffset(0)
	m.UpdateContent()
}

// SetSize handles terminal resizing.
func (m *ConfigViewModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.Viewport.Width = m.viewportWidth()
	m.Viewport.Height = m.viewportHeight()
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
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  " + m.emptyMsg)
		m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n" + PaintLine(msg, wrapW, ColorPanel, ColorMuted))
		m.Viewport.SetYOffset(0)
		return
	}

	if len(m.ConfigData) == 0 {
		m.lastRendered = ""
		msg := lipgloss.NewStyle().Foreground(ColorMuted).Render("  No configuration data available for this revision.")
		m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorMuted) + "\n" + PaintLine(msg, wrapW, ColorPanel, ColorMuted))
		return
	}

	var outLines []string

	if m.FormatMode != "json" {
		lines := normalizer.JSONToFlatCLI(m.ConfigData, "")
		if m.FilterPath != "" {
			lines = filter.FilterCLILines(lines, m.FilterPath)
		}
		if len(lines) == 0 {
			m.lastRendered = ""
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("  No CLI configuration statements match filter '%s'", m.FilterPath))
			m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorWarning) + "\n" + PaintLine(warn, wrapW, ColorPanel, ColorWarning))
			return
		}

		digits := gutterDigitsForCount(len(lines))
		for idx, l := range lines {
			styled := highlightSRLinuxCLI(l, ColorPanel)
			outLines = append(outLines, formatGutteredWrappedLines(idx+1, digits, styled, wrapW, ColorPanel, ColorText)...)
		}
	} else {
		data := m.ConfigData
		if m.FilterPath != "" {
			data = filter.FilterConfigSubtree(data, m.FilterPath)
		}
		if len(data) == 0 {
			m.lastRendered = ""
			warn := lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("  No JSON configuration subtrees match filter '%s'", m.FilterPath))
			m.Viewport.SetContent(PaintLine("", wrapW, ColorPanel, ColorWarning) + "\n" + PaintLine(warn, wrapW, ColorPanel, ColorWarning))
			return
		}
		jsonStr, _ := normalizer.CanonicalJSONString(data, 2)
		lines := strings.Split(jsonStr, "\n")

		digits := gutterDigitsForCount(len(lines))
		for idx, l := range lines {
			styled := highlightJSONLine(l, ColorPanel)
			outLines = append(outLines, formatGutteredWrappedLines(idx+1, digits, styled, wrapW, ColorPanel, ColorTextPrimary)...)
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
	prevY := m.Viewport.YOffset
	m.Viewport.SetContent(content)
	if prevY > 0 {
		m.Viewport.SetYOffset(prevY)
	}
}

// highlightJSONLine performs quote-aware JSON syntax highlighting so IPv6 addresses,
// MAC addresses, timestamps, booleans, numbers, and null values render accurately.
func highlightJSONLine(line string, bg lipgloss.Color) string {
	var b strings.Builder
	keyStyle := StyleConfigKey.Background(bg)
	strStyle := StyleConfigValue.Background(bg)
	punctStyle := StyleConfigPunctuation.Background(bg)
	numStyle := lipgloss.NewStyle().Foreground(ColorSyntaxACL).Background(bg)
	boolStyle := lipgloss.NewStyle().Foreground(ColorPurple).Bold(true).Background(bg)
	nullStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true).Background(bg)
	spaceStyle := lipgloss.NewStyle().Background(bg)
	defaultStyle := StyleConfigDefault.Background(bg)

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
			b.WriteString(spaceStyle.Render(line[start:i]))
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
				b.WriteString(keyStyle.Render(lit))
			} else {
				b.WriteString(strStyle.Render(lit))
			}
			continue
		}

		// Structural punctuation
		if ch == '{' || ch == '}' || ch == '[' || ch == ']' || ch == ':' || ch == ',' {
			b.WriteString(punctStyle.Render(line[i : i+1]))
			i++
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
			b.WriteString(numStyle.Render(line[start:i]))
			continue
		}

		// Keywords: true, false, null
		if strings.HasPrefix(line[i:], "true") && isJSONDelimAfter(line, i+4) {
			b.WriteString(boolStyle.Render("true"))
			i += 4
			continue
		}
		if strings.HasPrefix(line[i:], "false") && isJSONDelimAfter(line, i+5) {
			b.WriteString(boolStyle.Render("false"))
			i += 5
			continue
		}
		if strings.HasPrefix(line[i:], "null") && isJSONDelimAfter(line, i+4) {
			b.WriteString(nullStyle.Render("null"))
			i += 4
			continue
		}

		// Fallback single character
		b.WriteString(defaultStyle.Render(line[i : i+1]))
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
	specs := []toolbarPillSpec{
		{label: "cli", text: "[1] CLI", active: m.FormatMode != "json", padH: 1},
		{label: "json", text: "[2] JSON", active: m.FormatMode == "json", padH: 1},
	}
	scrollBadge := renderCompactScrollStatus(m.Viewport)
	return buildToolbarLine(specs, m.IsFocused, scrollBadge, innerW)
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
		sections = append(sections, renderCommitContextStrip(m.Commit, m.BaseCommit, false, innerW))
	}

	vpH := innerH - len(sections)
	if vpH < 1 {
		vpH = 1
	}
	vpView := renderViewportWithScrollbar(m.Viewport, innerW, vpH, ColorPanel)
	sections = append(sections, vpView)

	body := strings.Join(sections, "\n")
	return renderDetailTitledBox(int(TabConfig), m.IsFocused, m.Zoomed, body, innerW, innerH)
}
