package modals

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/models"
)

// ExportModalModel handles configuration export options.
type ExportModalModel struct {
	Commit    models.TimelineCommit
	Format    string // "json", "cli", "startup"
	PathInput textinput.Model
	Width     int
	Height    int
}

// NewExportModal creates an export modal.
func NewExportModal(commit models.TimelineCommit, width, height int) ExportModalModel {
	ti := textinput.New()
	ti.Placeholder = "/tmp/exported_config.json"
	ti.Prompt = "Output File: "
	ti.Width = 45
	StyleTextInput(&ti, ColorSurface, ColorText, ColorTextDim, ColorActive)
	ti.Focus()

	return ExportModalModel{
		Commit:    commit,
		Format:    "json",
		PathInput: ti,
		Width:     width,
		Height:    height,
	}
}

// View renders the export modal dialog.
func (m ExportModalModel) View() string {
	StyleTextInput(&m.PathInput, ColorSurface, ColorText, ColorTextDim, ColorActive)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		Render("💾 EXPORT CONFIGURATION")

	shortSHA := m.Commit.FullSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	textStyle := lipgloss.NewStyle().Foreground(ColorText)
	details := textStyle.Render(fmt.Sprintf("Exporting revision: %s (%s)", shortSHA, m.Commit.Message))

	optJSON := textStyle.Render("  [1] JSON Startup Format (config.json)")
	if m.Format == "json" {
		optJSON = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("▶ [1] JSON Startup Format (config.json)")
	}

	optCLI := textStyle.Render("  [2] Flat CLI Script (set / ...)")
	if m.Format == "cli" {
		optCLI = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("▶ [2] Flat CLI Script (set / ...)")
	}

	optStartup := textStyle.Render("  [3] Save directly to switch /etc/opt/srlinux/config.json")
	if m.Format == "startup" {
		optStartup = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("▶ [3] Save directly to switch /etc/opt/srlinux/config.json")
	}

	pathView := m.PathInput.View()
	if m.Format == "startup" {
		pathView = lipgloss.NewStyle().Foreground(ColorMuted).Render("Target: /etc/opt/srlinux/config.json (automatic)")
	}

	footer := lipgloss.NewStyle().Foreground(ColorMuted).Render(
		"\n[1/2/3 or ↑/↓/Tab] Select Format  |  [Enter] Export  |  [Esc] Cancel",
	)

	content := fmt.Sprintf("%s\n\n%s\n\n%s\n%s\n%s\n\n%s\n%s", title, details, optJSON, optCLI, optStartup, pathView, footer)
	innerW := 66
	if m.Width > 0 && m.Width-6 < innerW {
		innerW = m.Width - 6
	}
	if innerW < 10 {
		innerW = 10
	}
	vPad := 1
	if m.Height > 0 && m.Height < 20 {
		vPad = 0
	}
	paintedContent := PaintSurface(content, innerW, 0, ColorSurface, ColorText)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		BorderBackground(ColorSurface).
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(vPad, 2).
		Render(paintedContent)

	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(ColorBackground),
		lipgloss.WithWhitespaceForeground(ColorTextDim),
	)
}

// Update processes export key events.
func (m ExportModalModel) Update(msg tea.Msg) (ExportModalModel, string, string, bool) {
	// Returns (model, format, outputPath, close)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, "", "", true
		case "up", "shift+tab":
			switch m.Format {
			case "startup":
				m.Format = "cli"
				m.PathInput.Placeholder = "/tmp/exported_config.cli"
				m.PathInput.Focus()
			case "cli":
				m.Format = "json"
				m.PathInput.Placeholder = "/tmp/exported_config.json"
				m.PathInput.Focus()
			default:
				m.Format = "startup"
				m.PathInput.Blur()
			}
			return m, "", "", false
		case "down", "tab":
			switch m.Format {
			case "json":
				m.Format = "cli"
				m.PathInput.Placeholder = "/tmp/exported_config.cli"
				m.PathInput.Focus()
			case "cli":
				m.Format = "startup"
				m.PathInput.Blur()
			default:
				m.Format = "json"
				m.PathInput.Placeholder = "/tmp/exported_config.json"
				m.PathInput.Focus()
			}
			return m, "", "", false
		case "1":
			if m.Format == "startup" || m.PathInput.Value() == "" {
				m.Format = "json"
				m.PathInput.Placeholder = "/tmp/exported_config.json"
				m.PathInput.Focus()
				return m, "", "", false
			}
		case "2":
			if m.Format == "startup" || m.PathInput.Value() == "" {
				m.Format = "cli"
				m.PathInput.Placeholder = "/tmp/exported_config.cli"
				m.PathInput.Focus()
				return m, "", "", false
			}
		case "3":
			if m.Format == "startup" || m.PathInput.Value() == "" {
				m.Format = "startup"
				m.PathInput.Blur()
				return m, "", "", false
			}
		case "enter":
			out := m.PathInput.Value()
			if out == "" && m.Format != "startup" {
				out = m.PathInput.Placeholder
			}
			return m, m.Format, out, true
		}
	}

	if m.Format != "startup" {
		var cmd tea.Cmd
		m.PathInput, cmd = m.PathInput.Update(msg)
		_ = cmd
	}

	return m, "", "", false
}
