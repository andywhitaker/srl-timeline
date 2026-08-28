package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"timeline/pkg/gitbackend"
	"timeline/pkg/models"
	"timeline/pkg/tui/modals"
)

func TestAppModelLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-tui-test"},
		},
	}
	_, _, err = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial baseline", nil)
	if err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	// Resize window
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	app := resModel.(AppModel)

	// Check view output
	viewStr := app.View()
	if !strings.Contains(viewStr, "TIMELINE") {
		t.Fatalf("expected TIMELINE in header")
	}
	if !strings.Contains(viewStr, "CONFIGURATION TIMELINE") {
		t.Fatalf("expected CONFIGURATION TIMELINE header")
	}
	if !strings.Contains(viewStr, "Diff View") {
		t.Fatalf("expected Diff View tab")
	}

	// Switch top tab to Config using 'c'
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabConfig {
		t.Fatalf("expected ActiveTab to be TabConfig after 'c'")
	}

	// Switch top tab to Blame using 'b'
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected ActiveTab to be TabBlame after 'b'")
	}

	// Switch top tab back to Diff using 'd'
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected ActiveTab to be TabDiff after 'd'")
	}

	// Switch sub-format to Path in Diff tab using 2
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "path" {
		t.Fatalf("expected DiffMode to be path after 2")
	}

	// Switch to Config tab and test sub-format 2 (JSON)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = resModel.(AppModel)
	if app.ConfigView.FormatMode != "json" {
		t.Fatalf("expected FormatMode to be json after 2 in Config tab")
	}

	// Switch focus to Detail Pane with Tab
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneDetail {
		t.Fatalf("expected FocusedPane to be PaneDetail")
	}

	// Test Filter activation with /
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneFilter || !app.FocusFilter {
		t.Fatalf("expected FocusedPane to be PaneFilter")
	}

	// Press Enter to close filter
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline {
		t.Fatalf("expected FocusedPane to be PaneTimeline after Enter")
	}

	// Open Help modal
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalHelp {
		t.Fatalf("expected ModalHelp to be active")
	}

	// Close Help modal
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalNone {
		t.Fatalf("expected ModalNone after esc")
	}
}

func TestLongLineWrapping(t *testing.T) {
	longStr := "set / system tls profile clab-profile certificate \"" + strings.Repeat("A", 1000) + "\""
	rendered := lipgloss.NewStyle().Width(80).Render(longStr)
	lines := strings.Split(rendered, "\n")
	t.Logf("Number of lines from lipgloss.Width(80): %d", len(lines))
	for i, l := range lines {
		t.Logf("line %d len: %d", i, len(l))
	}
}

func TestConfigAndBlameScroll(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_scroll_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-tui-test"},
			"tls": map[string]interface{}{
				"profile": []interface{}{
					map[string]interface{}{
						"name":        "clab-profile",
						"certificate": strings.Repeat("M", 2000),
						"key":         strings.Repeat("K", 500),
					},
				},
			},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Port 1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Port 2"},
			map[string]interface{}{"name": "ethernet-1/3", "description": "Port 3"},
			map[string]interface{}{"name": "ethernet-1/4", "description": "Port 4"},
			map[string]interface{}{"name": "ethernet-1/5", "description": "Port 5"},
		},
	}
	_, _, err = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial baseline", nil)
	if err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	// Switch to Config tab
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabConfig {
		t.Fatalf("expected TabConfig")
	}

	// Focus Detail Pane
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)

	initOffset := app.ConfigView.Viewport.YOffset
	t.Logf("Initial YOffset: %d", initOffset)

	// Scroll down with mouse wheel in detail pane (X=60, Y=10)
	for i := 0; i < 15; i++ {
		resModel, _ = app.Update(tea.MouseMsg{
			X:    70,
			Y:    10,
			Type: tea.MouseWheelDown,
		})
		app = resModel.(AppModel)
		t.Logf("Step %d: YOffset = %d (total lines: %d, height: %d)", i, app.ConfigView.Viewport.YOffset, app.ConfigView.Viewport.TotalLineCount(), app.ConfigView.Viewport.Height)
		_ = app.View() // Ensure View() renders without panicking or infinite loops
	}

	if app.ConfigView.Viewport.YOffset <= initOffset {
		t.Fatalf("expected YOffset to increase when scrolling down")
	}

	// Test Blame tab
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected TabBlame")
	}

	blameInitOffset := app.BlameView.Viewport.YOffset
	for i := 0; i < 15; i++ {
		resModel, _ = app.Update(tea.MouseMsg{
			X:    70,
			Y:    10,
			Type: tea.MouseWheelDown,
		})
		app = resModel.(AppModel)
		t.Logf("Blame Step %d: YOffset = %d", i, app.BlameView.Viewport.YOffset)
		_ = app.View()
	}

	if app.BlameView.Viewport.YOffset <= blameInitOffset {
		t.Fatalf("expected Blame YOffset to increase when scrolling down")
	}
}

func TestCherryPickModalTreeAndScroll(t *testing.T) {
	changes := []models.PathChange{
		{
			Path:     "/interface[name=ethernet-1/1]/description",
			DiffType: models.DiffModified,
			OldValue: "Port 1 Old",
			NewValue: "Port 1 New",
		},
		{
			Path:     "/interface[name=ethernet-1/1]/admin-state",
			DiffType: models.DiffAdded,
			NewValue: "enable",
		},
		{
			Path:     "/interface[name=ethernet-1/1]/subinterface[index=0]/admin-state",
			DiffType: models.DiffAdded,
			NewValue: "enable",
		},
		{
			Path:     "/interface[name=ethernet-1/2]/description",
			DiffType: models.DiffModified,
			OldValue: "Port 2 Old",
			NewValue: "Port 2 New",
		},
		{
			Path:     "/network-instance[name=default]/protocols/bgp/neighbor[peer-address=10.0.0.1]/admin-state",
			DiffType: models.DiffModified,
			NewValue: "enable",
		},
		{
			Path:     "/system/information/location",
			DiffType: models.DiffModified,
			NewValue: "Lab Rack 1",
		},
	}

	commit := models.TimelineCommit{
		CommitID: "abc12345",
		FullSHA:  "abc1234567890",
		Author:   "admin",
		Message:  "Multi-subsystem update",
	}

	modal := modals.NewCherryPickModal(commit, changes, nil, 100, 30)

	// Verify high-level root nodes
	if len(modal.RootNodes) != 3 {
		t.Fatalf("expected 3 high-level root nodes (/interface, /network-instance, /system), got %d", len(modal.RootNodes))
	}
	if len(modal.VisibleNodes) != 3 {
		t.Fatalf("expected 3 visible nodes at high level, got %d", len(modal.VisibleNodes))
	}

	// Verify view rendering
	viewStr := modal.View()
	t.Logf("viewStr:\n%s", viewStr)
	if !strings.Contains(viewStr, "/interface") || !strings.Contains(viewStr, "/system") {
		t.Fatalf("expected root nodes in view output")
	}

	// Expand /interface (first node)
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRight})
	if len(modal.VisibleNodes) <= 3 {
		t.Fatalf("expected more visible nodes after expanding /interface, got %d", len(modal.VisibleNodes))
	}

	// Move down to ethernet-1/1 and expand it
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyDown})
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRight})

	// Move down to leaf description and verify selection
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyDown})
	modal, selectedPaths, confirmed := modal.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !confirmed || len(selectedPaths) != 1 || !strings.Contains(selectedPaths[0], "description") {
		t.Fatalf("expected single cursor selection of description, got %v (confirmed=%v)", selectedPaths, confirmed)
	}

	// Test multi-select checkbox toggle with Space and x
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !modal.SelectedPaths[modal.VisibleNodes[modal.SelectedIndex].FullPath] {
		t.Fatalf("expected node to be selected after space")
	}

	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyDown})
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	multiPaths := modal.GetSelectedPaths()
	if len(multiPaths) < 2 {
		t.Fatalf("expected at least 2 selected paths, got %v", multiPaths)
	}

	// Test Expand All
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	totalExpanded := len(modal.VisibleNodes)
	t.Logf("Total nodes expanded: %d", totalExpanded)
	if totalExpanded < 10 {
		t.Fatalf("expected >= 10 visible nodes when all expanded, got %d", totalExpanded)
	}

	// Test Select All ('a')
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) // Deselects when some were selected
	modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) // Selects all
	if len(modal.SelectedPaths) < 10 {
		t.Fatalf("expected >= 10 selected paths after select all, got %d", len(modal.SelectedPaths))
	}

	// Test scrolling down through the expanded tree
	for i := 0; i < 8; i++ {
		modal, _, _ = modal.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	// Render view during scroll with checkboxes
	scrolledView := modal.View()
	t.Logf("scrolledView:\n%s", scrolledView)
	if !strings.Contains(scrolledView, "CHERRY-PICK") || !strings.Contains(scrolledView, "[✓]") {
		t.Fatalf("expected CHERRY-PICK header and checkboxes in view")
	}
}
