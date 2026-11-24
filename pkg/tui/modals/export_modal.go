package modals

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/models"
)

const (
	// ExportFocusFormat focuses the export format selector (1/2/3 or ↑/↓).
	ExportFocusFormat = 0
	// ExportFocusPath focuses the output path text input (when Format != "startup").
	ExportFocusPath = 1
	// ExportFocusExport focuses the [ Export ] action button.
	ExportFocusExport = 2
	// ExportFocusCancel focuses the [ Cancel ] action button.
	ExportFocusCancel = 3
)

// ExportModalModel handles configuration export options with separate focusable
// Format selector, Output Path input, and action buttons.
type ExportModalModel struct {
	Commit         models.TimelineCommit
	Format         string // "json", "cli", "startup"
	PathInput      textinput.Model
	FocusedSection int
	Width          int
	Height         int
}

// NewExportModal creates an export modal with Format selector focused by default.
func NewExportModal(commit models.TimelineCommit, width, height int) ExportModalModel {
	ti := textinput.New()
	ti.Placeholder = "/tmp/exported_config.json"
	ti.Prompt = "Output Path: "
	ti.Width = 44
	StyleTextInput(&ti, ColorSurface, ColorText, ColorTextDim, ColorActive)
	ti.Blur()

	return ExportModalModel{
		Commit:         commit,
		Format:         "json",
		PathInput:      ti,
		FocusedSection: ExportFocusFormat,
		Width:          width,
		Height:         height,
	}
}

// IsEditingPath reports whether keyboard input is currently directed to the Output Path field.
func (m ExportModalModel) IsEditingPath() bool {
	return m.FocusedSection == ExportFocusPath && m.Format != "startup"
}

func (m *ExportModalModel) setFormat(format string) {
	m.Format = format
	switch format {
	case "json":
		m.PathInput.Placeholder = "/tmp/exported_config.json"
	case "cli":
		m.PathInput.Placeholder = "/tmp/exported_config.cli"
	case "startup":
		m.PathInput.Blur()
		if m.FocusedSection == ExportFocusPath {
			m.FocusedSection = ExportFocusFormat
		}
	}
}

func (m *ExportModalModel) setFocus(section int) {
	if section == ExportFocusPath && m.Format == "startup" {
		section = ExportFocusExport
	}
	m.FocusedSection = section
	if m.FocusedSection == ExportFocusPath && m.Format != "startup" {
		m.PathInput.Focus()
	} else {
		m.PathInput.Blur()
	}
}

func (m *ExportModalModel) focusOrder() []int {
	if m.Format == "startup" {
		return []int{ExportFocusFormat, ExportFocusExport, ExportFocusCancel}
	}
	return []int{ExportFocusFormat, ExportFocusPath, ExportFocusExport, ExportFocusCancel}
}

func (m *ExportModalModel) cycleFocus(delta int) {
	order := m.focusOrder()
	pos := 0
	for i, s := range order {
		if s == m.FocusedSection {
			pos = i
			break
		}
	}
	next := (pos + delta + len(order)) % len(order)
	m.setFocus(order[next])
}

func (m ExportModalModel) resolvedOutputPath() string {
	if m.Format == "startup" {
		return ""
	}
	out := strings.TrimSpace(m.PathInput.Value())
	if out == "" {
		out = m.PathInput.Placeholder
	}
	return out
}

// View renders the export modal dialog.
func (m ExportModalModel) View() string {
	innerW, vPad, hPad, maxInnerH := modalBoxMetrics(m.Width, m.Height, 66)
	compact := m.Height > 0 && m.Height <= 24
	ultraCompact := m.Height > 0 && m.Height <= 14

	inputW := innerW - 18
	if inputW < 10 {
		inputW = 10
	}
	m.PathInput.Width = inputW
	StyleTextInput(&m.PathInput, ColorSurface, ColorText, ColorTextDim, ColorActive)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		Render("◆ EXPORT CONFIGURATION")

	shortSHA := m.Commit.FullSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	details := PaintBarLine(
		lipgloss.NewStyle().Foreground(ColorText).Render(
			fmt.Sprintf("Revision: %s (%s)", shortSHA, truncateDisplay(m.Commit.Message, innerW-22)),
		),
		innerW,
		ColorSurface,
		ColorText,
	)

	fmtHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted)
	fmtHeaderPrefix := "  "
	if m.FocusedSection == ExportFocusFormat {
		fmtHeaderStyle = fmtHeaderStyle.Foreground(ColorActive)
		fmtHeaderPrefix = "▸ "
	}
	fmtHeader := PaintBarLine(fmtHeaderStyle.Render(fmtHeaderPrefix+"Format (↑/↓ or 1/2/3):"), innerW, ColorSurface, ColorMuted)

	type optSpec struct {
		key   string
		num   string
		label string
	}
	opts := []optSpec{
		{"json", "1", "Startup JSON (config.json)"},
		{"cli", "2", "Flat CLI Script (set / ...)"},
		{"startup", "3", "Device Startup Config (/etc/opt/srlinux/config.json)"},
	}

	var optLines []string
	for _, opt := range opts {
		selected := m.Format == opt.key
		rowBg := ColorSurface
		if selected && m.FocusedSection == ExportFocusFormat {
			rowBg = ColorHighlight
		}
		bullet := lipgloss.NewStyle().Background(rowBg).Foreground(ColorTextDim).Render("  ○ ")
		textStyle := lipgloss.NewStyle().Background(rowBg).Foreground(ColorText)
		if selected {
			bulletColor := ColorActive
			if opt.key == "startup" {
				bulletColor = ColorSuccess
			}
			bullet = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(bulletColor).Render("  ● ")
			textStyle = textStyle.Bold(true).Foreground(bulletColor)
		}
		line := bullet + textStyle.Render(fmt.Sprintf("[%s] %s", opt.num, opt.label))
		optLines = append(optLines, PaintBarLine(line, innerW, rowBg, ColorText))
	}

	// Output Path section
	var pathBlock string
	if m.Format == "startup" {
		pathBlock = PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Render("  Target: /etc/opt/srlinux/config.json (automatic)"),
			innerW,
			ColorSurface,
			ColorMuted,
		)
	} else {
		pathBorderFg := ColorBorder
		if m.FocusedSection == ExportFocusPath {
			pathBorderFg = ColorActive
		}
		pathInnerW := innerW - 4
		if pathInnerW < 6 {
			pathInnerW = 6
		}
		pathRendered := PaintBarLine(m.PathInput.View(), pathInnerW, ColorSurface, ColorText)
		if ultraCompact {
			prefix := "  "
			if m.FocusedSection == ExportFocusPath {
				prefix = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("▸ ")
			}
			pathBlock = PaintBarLine(prefix+m.PathInput.View(), innerW, ColorSurface, ColorText)
		} else {
			pathBlock = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(pathBorderFg).
				BorderBackground(ColorSurface).
				Background(ColorSurface).
				Foreground(ColorText).
				Padding(0, 1).
				Render(pathRendered)
		}
	}

	// Action buttons
	exportStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusedSection == ExportFocusExport {
		exportStyle = exportStyle.Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnExport := exportStyle.Render("[ Export ]")

	cancelStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorMuted)
	if m.FocusedSection == ExportFocusCancel {
		cancelStyle = cancelStyle.Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnCancel := cancelStyle.Render("[ Cancel ]")

	btnGap := lipgloss.NewStyle().Background(ColorSurface).Render("  ")
	buttonsRow := PaintBarLine(
		lipgloss.JoinHorizontal(lipgloss.Center, btnExport, btnGap, btnCancel),
		innerW,
		ColorSurface,
		ColorText,
	)

	footerText := "[Tab] Switch Field  |  [1/2/3/↑/↓] Format  |  [Enter] Export  |  [Esc] Cancel"
	if innerW < 58 {
		footerText = "[Tab] Field | [1-3/↑↓] Fmt | [Enter] OK | [Esc] No"
	}
	footer := PaintBarLine(
		lipgloss.NewStyle().Foreground(ColorMuted).Render(footerText),
		innerW,
		ColorSurface,
		ColorMuted,
	)

	var sections []string
	sections = append(sections, PaintBarLine(title, innerW, ColorSurface, ColorText))
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, details)
	if !compact {
		sections = append(sections, "")
	}
	if !ultraCompact {
		sections = append(sections, fmtHeader)
	}
	sections = append(sections, strings.Join(optLines, "\n"))
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, pathBlock)
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, buttonsRow)
	sections = append(sections, footer)

	content := strings.Join(sections, "\n")
	return renderCenteredModal(content, m.Width, m.Height, innerW, vPad, hPad, maxInnerH, ColorPrimary)
}

// Update processes export modal key events.
// Returns (model, format, outputPath, close).
func (m ExportModalModel) Update(msg tea.Msg) (ExportModalModel, string, string, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "esc":
			return m, "", "", true
		case "tab":
			m.cycleFocus(1)
			return m, "", "", false
		case "shift+tab":
			m.cycleFocus(-1)
			return m, "", "", false
		}

		switch m.FocusedSection {
		case ExportFocusFormat:
			switch key {
			case "up", "k":
				switch m.Format {
				case "startup":
					m.setFormat("cli")
				case "cli":
					m.setFormat("json")
				default:
					m.setFormat("startup")
				}
				return m, "", "", false

			case "down", "j":
				switch m.Format {
				case "json":
					m.setFormat("cli")
				case "cli":
					m.setFormat("startup")
				default:
					m.setFormat("json")
				}
				return m, "", "", false

			case "1":
				m.setFormat("json")
				return m, "", "", false

			case "2":
				m.setFormat("cli")
				return m, "", "", false

			case "3":
				m.setFormat("startup")
				return m, "", "", false

			case "enter":
				return m, m.Format, m.resolvedOutputPath(), true
			}

		case ExportFocusPath:
			if key == "enter" {
				return m, m.Format, m.resolvedOutputPath(), true
			}
			var cmd tea.Cmd
			m.PathInput, cmd = m.PathInput.Update(msg)
			_ = cmd
			return m, "", "", false

		case ExportFocusExport:
			switch key {
			case "left", "right", "h", "l":
				m.setFocus(ExportFocusCancel)
				return m, "", "", false
			case "enter", " ", "space":
				return m, m.Format, m.resolvedOutputPath(), true
			}

		case ExportFocusCancel:
			switch key {
			case "left", "right", "h", "l":
				m.setFocus(ExportFocusExport)
				return m, "", "", false
			case "enter", " ", "space":
				return m, "", "", true
			}
		}
	}

	if m.IsEditingPath() {
		var cmd tea.Cmd
		m.PathInput, cmd = m.PathInput.Update(msg)
		_ = cmd
	}

	return m, "", "", false
}
