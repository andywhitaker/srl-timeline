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
	Commit      models.TimelineCommit
	Format      string // "json", "cli", "startup"
	PathInput   textinput.Model
	Width       int
	Height      int
}

// NewExportModal creates an export modal.
func NewExportModal(commit models.TimelineCommit, width, height int) ExportModalModel {
	ti := textinput.New()
	ti.Placeholder = "/tmp/exported_config.json"
	ti.Prompt = "Output File: "
	ti.Width = 45

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
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#1f6feb")).
		Padding(0, 1).
		Render("💾 EXPORT CONFIGURATION")

	details := fmt.Sprintf("Exporting revision: %s (%s)", m.Commit.FullSHA[:8], m.Commit.Message)

	optJSON := "[1] JSON Startup Format (config.json)"
	if m.Format == "json" {
		optJSON = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render("▶ [1] JSON Startup Format (config.json)")
	}

	optCLI := "[2] Flat CLI Script (set / ...)"
	if m.Format == "cli" {
		optCLI = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render("▶ [2] Flat CLI Script (set / ...)")
	}

	optStartup := "[3] Save directly to switch /etc/opt/srlinux/config.json"
	if m.Format == "startup" {
		optStartup = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3fb950")).Render("▶ [3] Save directly to switch /etc/opt/srlinux/config.json")
	}

	pathView := m.PathInput.View()
	if m.Format == "startup" {
		pathView = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render("Target: /etc/opt/srlinux/config.json (automatic)")
	}

	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(
		"\n[1/2/3] Select Format  |  [Enter] Export  |  [Esc] Cancel",
	)

	content := fmt.Sprintf("%s\n\n%s\n\n%s\n%s\n%s\n\n%s\n%s", title, details, optJSON, optCLI, optStartup, pathView, footer)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#1f6feb")).
		Background(lipgloss.Color("#161b22")).
		Padding(1, 2).
		Width(70).
		Render(content)

	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
}

// Update processes export key events.
func (m ExportModalModel) Update(msg tea.Msg) (ExportModalModel, string, string, bool) {
	// Returns (model, format, outputPath, close)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, "", "", true
		case "1":
			m.Format = "json"
			m.PathInput.Placeholder = "/tmp/exported_config.json"
		case "2":
			m.Format = "cli"
			m.PathInput.Placeholder = "/tmp/exported_config.cli"
		case "3":
			m.Format = "startup"
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
