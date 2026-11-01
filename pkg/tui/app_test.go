package tui

import (
	"fmt"
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

func TestExactScreenHeightFit(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_height_test_*")
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
	_, _, _ = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial baseline", nil)

	model := NewAppModel(backend, nil, "")
	for _, w := range []int{80, 100, 120} {
		for _, h := range []int{24, 30, 36, 40} {
			resModel, _ := model.Update(tea.WindowSizeMsg{Width: w, Height: h})
			app := resModel.(AppModel)

			// 1. TabDiff (unfiltered)
			v := app.View()
			lines := strings.Split(v, "\n")
			if len(lines) != h {
				for i, l := range lines {
					t.Logf("L%d: %s", i, l)
				}
				t.Fatalf("expected exactly %d lines in TabDiff (unfiltered, width %d), got %d", h, w, len(lines))
			}

			// 2. TabDiff (filtered)
			resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			app = resModel.(AppModel)
			resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e', 't', 'h'}})
			app = resModel.(AppModel)
			v = app.View()
			lines = strings.Split(v, "\n")
			if len(lines) != h {
				t.Fatalf("expected exactly %d lines in TabDiff (filtered, width %d), got %d", h, w, len(lines))
			}

			// 3. TabConfig
			resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
			app = resModel.(AppModel)
			resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
			app = resModel.(AppModel)
			v = app.View()
			lines = strings.Split(v, "\n")
			if len(lines) != h {
				t.Fatalf("expected exactly %d lines in TabConfig (width %d), got %d", h, w, len(lines))
			}

			// 4. TabBlame
			resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
			app = resModel.(AppModel)
			v = app.View()
			lines = strings.Split(v, "\n")
			if len(lines) != h {
				t.Fatalf("expected exactly %d lines in TabBlame (width %d), got %d", h, w, len(lines))
			}
		}
	}
}

func TestMouseClickHitBoxes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_mouse_test_*")
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
	_, _, _ = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial baseline", nil)

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)
	timelineW := (120 * 38) / 100

	// 1. Click Filter Bar (Y=2)
	resModel, _ = app.Update(tea.MouseMsg{X: 20, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneFilter || !app.FocusFilter {
		t.Fatalf("expected FocusFilter to be true after clicking filter bar at Y=2")
	}

	// 2. Click Top Tab: Full Config [c] (Y=4, X = timelineW + 30)
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 30, Y: 4, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabConfig {
		t.Fatalf("expected ActiveTab to be TabConfig after clicking tab at Y=4, got %v", app.ActiveTab)
	}

	// 3. Click Top Tab: Blame View [b] (Y=4, X = timelineW + 55)
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 55, Y: 4, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected ActiveTab to be TabBlame after clicking tab at Y=4, got %v", app.ActiveTab)
	}

	// 4. Click Top Tab: Diff View [d] (Y=4, X = timelineW + 10)
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 10, Y: 4, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected ActiveTab to be TabDiff after clicking tab at Y=4, got %v", app.ActiveTab)
	}

	// 5. Click Sub-Option in Diff: Path Changes (Y=6, X = timelineW + 20)
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 20, Y: 6, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "path" {
		t.Fatalf("expected DiffMode to be path after clicking sub-option at Y=6, got %v", app.DiffView.DiffMode)
	}

	// 6. Click Sub-Option in Diff: vs Live (Y=6, X = timelineW + 45)
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 45, Y: 6, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if !app.VsLive {
		t.Fatalf("expected VsLive to be true after clicking vs Live at Y=6")
	}
}

func TestTUIFilteringEthernet(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_filter_eth_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Old Port 1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Port 2"},
		},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "New Port 1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Port 2"},
		},
	}
	_, _, _ = backend.RecordConfigChange(cfg1, "admin", "1", "", time.Now().UTC(), "Initial", nil)
	_, _, _ = backend.RecordConfigChange(cfg2, "admin", "2", "", time.Now().UTC(), "Update Port 1", nil)

	// Create app with filter "ethernet-1/1"
	model := NewAppModel(backend, nil, "ethernet-1/1")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	// 1. Verify Diff View (Unified) contains ethernet-1/1 diff
	diffViewStr := app.DiffView.Viewport.View()
	t.Logf("diffViewStr:\n%s", diffViewStr)
	if !strings.Contains(diffViewStr, "New Port 1") {
		t.Fatalf("expected Diff View to contain 'New Port 1' for ethernet-1/1 filter, got:\n%s", diffViewStr)
	}

	// 2. Switch to Path Diff (Key 2)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = resModel.(AppModel)
	pathViewStr := app.DiffView.Viewport.View()
	t.Logf("pathViewStr:\n%s", pathViewStr)
	if !strings.Contains(pathViewStr, "ethernet-1/1") {
		t.Fatalf("expected Path View to contain ethernet-1/1, got:\n%s", pathViewStr)
	}

	// 3. Switch to Full Config (Key c) - CLI mode
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	cfgCLIViewStr := app.ConfigView.Viewport.View()
	t.Logf("cfgCLIViewStr:\n%s", cfgCLIViewStr)
	if !strings.Contains(cfgCLIViewStr, "set / interface ethernet-1/1") {
		t.Fatalf("expected Config CLI View to contain 'set / interface ethernet-1/1', got:\n%s", cfgCLIViewStr)
	}
	if strings.Contains(cfgCLIViewStr, "ethernet-1/2") {
		t.Fatalf("did not expect ethernet-1/2 in filtered Config CLI View")
	}

	// 4. Switch to JSON mode (Key 2) in Full Config
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = resModel.(AppModel)
	cfgJSONViewStr := app.ConfigView.Viewport.View()
	t.Logf("cfgJSONViewStr:\n%s", cfgJSONViewStr)
	if !strings.Contains(cfgJSONViewStr, "ethernet-1/1") {
		t.Fatalf("expected Config JSON View to contain ethernet-1/1, got:\n%s", cfgJSONViewStr)
	}
	if strings.Contains(cfgJSONViewStr, "ethernet-1/2") {
		t.Fatalf("did not expect ethernet-1/2 in filtered Config JSON View")
	}
}

func TestBlameViewScrollToBottom(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_blame_scroll_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	// Create config with multiple interfaces, systems, and a 3000-char TLS certificate
	cfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl"},
			"tls": map[string]interface{}{
				"server-profile": map[string]interface{}{
					"clab-profile": map[string]interface{}{
						"certificate": "-----BEGIN CERTIFICATE-----\n" + strings.Repeat("MIID9jCCAt6gAwIBAgICBnowDQYJKoZIhvcNAQELBQAwWDELMAkGA1UEBhMCVVMx", 50) + "\n-----END CERTIFICATE-----",
						"key":         "$aes1$" + strings.Repeat("ATQEFCVvnvpwAG8=$mw6fBitjVHKPNzOKblIPLppe//FjkaT9d/hPSicQk9bvTFET", 40),
					},
				},
			},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Port 1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Port 2"},
			map[string]interface{}{"name": "ethernet-1/3", "description": "Port 3"},
		},
	}
	_, _, _ = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial", nil)

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	// Switch to Blame tab
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected ActiveTab to be TabBlame")
	}

	// Focus Detail pane
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)

	// Scroll to bottom with 'G' (end)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	app = resModel.(AppModel)

	// Render view at bottom without errors
	v := app.View()
	if !strings.Contains(v, "Subsystem Breakdown") && !strings.Contains(v, "CONTRIBUTOR METRICS") {
		t.Fatalf("expected Subsystem Breakdown / Metrics visible at bottom of Blame View")
	}
}

func TestFilterClearingMechanisms(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_filter_clear_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl"}},
	}
	_, _, _ = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial", nil)

	// 1. Test clearing via Esc when focused in filter
	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	// Focus filter and type query
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b', 'g', 'p'}})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != "bgp" {
		t.Fatalf("expected filter value 'bgp', got %s", app.FilterBar.Value())
	}

	// Press Esc while focused -> clears filter
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != "" {
		t.Fatalf("expected empty filter after Esc, got %s", app.FilterBar.Value())
	}

	// 2. Test clearing via Esc when unfocused
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 'y', 's'}})
	app = resModel.(AppModel)
	// Press Enter to unfocus with query
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.FocusFilter || app.FilterBar.Value() != "sys" {
		t.Fatalf("expected unfocused filter with 'sys'")
	}
	// Press Esc while unfocused -> clears filter
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != "" {
		t.Fatalf("expected empty filter after unfocused Esc")
	}

	// 3. Test clearing via Mouse Click on [✖ Clear] badge
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i', 'n', 't'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)

	// Click on clear badge (X=115, Y=2)
	resModel, _ = app.Update(tea.MouseMsg{X: 115, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != "" {
		t.Fatalf("expected empty filter after mouse click on clear button")
	}
}

func TestScreenWidthNotExceeded(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tui_width_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-width-test"},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "This is a very long configuration statement intended to test wrapping behavior across lines",
			},
		},
	}
	_, _, _ = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial baseline", nil)

	model := NewAppModel(backend, nil, "")

	for _, w := range []int{80, 100, 120, 140} {
		for _, h := range []int{24, 30, 40} {
			resModel, _ := model.Update(tea.WindowSizeMsg{Width: w, Height: h})
			app := resModel.(AppModel)

			for _, tabKey := range []rune{'d', 'c', 'b'} {
				resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tabKey}})
				app = resModel.(AppModel)

				viewStr := app.View()
				lines := strings.Split(viewStr, "\n")
				for lineIdx, line := range lines {
					lineW := lipgloss.Width(line)
					if lineW > w {
						t.Fatalf("Width %d, Height %d, Tab %c: line %d exceeded screen width (line width %d):\n%s",
							w, h, tabKey, lineIdx, lineW, line)
					}
				}
			}
		}
	}
}

func TestDiffViewLineWrappingColors(t *testing.T) {
	diffRes := models.SemanticDiffResult{
		HasChanges: true,
		UnifiedDiffLines: []string{
			"--- previous_config",
			"+++ current_config",
			"@@ -1,5 +1,5 @@",
			"+ set / interface ethernet-1/1 description \"A very long description that is expected to wrap onto multiple lines in a narrow viewport\"",
			"- set / interface ethernet-1/1 description \"Old long description that is also expected to wrap onto multiple lines in a narrow viewport\"",
		},
		CLIDiffLines: []string{
			"+ set / interface ethernet-1/1 description \"A very long description that is expected to wrap onto multiple lines in a narrow viewport\"",
			"- set / interface ethernet-1/1 description \"Old long description that is also expected to wrap onto multiple lines in a narrow viewport\"",
		},
	}

	dv := NewDiffViewModel(40, 20)
	dv.SetDiff(diffRes)

	// Unified mode
	dv.DiffMode = "unified"
	dv.UpdateContent()
	content := dv.Viewport.View()
	lines := strings.Split(content, "\n")

	// Verify that lines containing the addition text have green ANSI codes (\x1b[)
	for _, l := range lines {
		if strings.Contains(l, "A very long") || strings.Contains(l, "multiple lines") {
			if !strings.Contains(l, "\x1b[") {
				t.Fatalf("expected wrapped line portion to retain ANSI escape codes, got: %q", l)
			}
		}
	}

	// CLI mode
	dv.DiffMode = "cli"
	dv.UpdateContent()
	content = dv.Viewport.View()
	lines = strings.Split(content, "\n")

	for _, l := range lines {
		if strings.Contains(l, "A very long") || strings.Contains(l, "multiple lines") {
			if !strings.Contains(l, "\x1b[") {
				t.Fatalf("expected wrapped CLI line portion to retain ANSI escape codes, got: %q", l)
			}
		}
	}
}

func TestCherryPickModalDeselectChildUnchecksParent(t *testing.T) {
	changes := []models.PathChange{
		{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffModified, NewValue: "Port 1"},
		{Path: "/interface[name=ethernet-1/2]/description", DiffType: models.DiffModified, NewValue: "Port 2"},
	}
	commit := models.TimelineCommit{CommitID: "abc12345", Message: "Test"}
	cm := modals.NewCherryPickModal(commit, changes, nil, 120, 40)

	// Expand all and select all
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	// Move cursor down to [name=ethernet-1/2] and toggle it off
	for i := 0; i < len(cm.VisibleNodes); i++ {
		if cm.VisibleNodes[cm.SelectedIndex].FullPath == "/interface[name=ethernet-1/2]" {
			break
		}
		cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	paths := cm.GetSelectedPaths()
	for _, p := range paths {
		if p == "/interface" || strings.Contains(p, "ethernet-1/2") {
			t.Fatalf("unchecking child must deselect parent /interface and ethernet-1/2, got selected paths: %v", paths)
		}
	}
}

func TestCherryPickModalCompositeListExpansion(t *testing.T) {
	changes := []models.PathChange{
		{
			Path:     "/interface",
			DiffType: models.DiffAdded,
			NewValue: []interface{}{
				map[string]interface{}{
					"name":        "ethernet-1/1",
					"admin-state": "enable",
				},
			},
		},
	}
	commit := models.TimelineCommit{CommitID: "abc12345", Message: "Added interface list"}
	cm := modals.NewCherryPickModal(commit, changes, nil, 120, 40)
	if len(cm.RootNodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(cm.RootNodes))
	}
	if cm.RootNodes[0].IsLeaf || len(cm.RootNodes[0].Children) == 0 {
		t.Fatalf("expected /interface root node to be expandable with children, got IsLeaf=%v Children=%d", cm.RootNodes[0].IsLeaf, len(cm.RootNodes[0].Children))
	}
}

func TestTUIBugFixes(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	var ifList []interface{}
	for i := 1; i <= 30; i++ {
		ifList = append(ifList, map[string]interface{}{
			"name":        "ethernet-1/" + string(rune('0'+i%10)),
			"description": strings.Repeat("PortDescription", 6),
			"admin-state": "enable",
		})
	}
	cfg1 := map[string]interface{}{
		"system":    map[string]interface{}{"name": map[string]interface{}{"host-name": "srl1"}},
		"interface": ifList,
	}
	cfg2 := map[string]interface{}{
		"system":    map[string]interface{}{"name": map[string]interface{}{"host-name": "srl2"}},
		"interface": ifList,
	}
	_, _, _ = backend.RecordConfigChange(cfg1, "admin", "1", "", time.Now().UTC(), "Commit 1", nil)
	_, _, _ = backend.RecordConfigChange(cfg2, "admin", "2", "", time.Now().UTC(), "Commit 2", nil)

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app := resModel.(AppModel)

	// 1. Verify resizeLayout updates modal dimensions
	if app.HelpModal.Width != 120 || app.HelpModal.Height != 24 {
		t.Fatalf("expected HelpModal dimensions 120x24, got %dx%d", app.HelpModal.Width, app.HelpModal.Height)
	}

	// 2. Switch to TabConfig ('c'), keep focus in PaneTimeline, press 'j' -> navigates commit instead of switching to JSON mode
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	if app.ConfigView.FormatMode != "cli" {
		t.Fatalf("expected FormatMode=cli initially")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	app = resModel.(AppModel)
	if app.ConfigView.FormatMode != "cli" {
		t.Fatalf("expected 'j' not to hijack FormatMode to json")
	}
	if app.TimelineView.SelectedIndex != 1 {
		t.Fatalf("expected 'j' in PaneTimeline to advance SelectedIndex to 1, got %d", app.TimelineView.SelectedIndex)
	}

	// 3. Focus PaneDetail (Tab), press 'j' and Space (' ') -> scrolls viewport down in TabConfig
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	app = resModel.(AppModel)
	if app.ConfigView.Viewport.YOffset == 0 {
		t.Fatalf("expected 'j' in PaneDetail TabConfig to scroll down, got YOffset=0")
	}
	offsetAfterJ := app.ConfigView.Viewport.YOffset
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	if app.ConfigView.Viewport.YOffset <= offsetAfterJ {
		t.Fatalf("expected Space key in PaneDetail to page down, got %d <= %d", app.ConfigView.Viewport.YOffset, offsetAfterJ)
	}

	// 4. Test mouse click hit-boxes when Notification banner is active (shifts Y down by 1)
	app.Notification = "Test notification banner"
	timelineW := (120 * 38) / 100
	// Click Top Tab Diff View at Y = 5 (4 + notifExtra)
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 5, Y: 5, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected click at Y=5 (with notification banner) to select TabDiff, got %v", app.ActiveTab)
	}
	// Click [3] CLI sub-option in TabDiff at Y = 7 (6 + notifExtra), X = timelineW + 30
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 30, Y: 7, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "cli" {
		t.Fatalf("expected click at X=timelineW+30, Y=7 (with notification banner) to select DiffMode=cli, got %q", app.DiffView.DiffMode)
	}

	// 5. Test CherryPickModal mouse wheel scrolling via AppModel.Update
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalCherryPick {
		t.Fatalf("expected ModalCherryPick to open on 'p'")
	}
	resModel, _ = app.Update(tea.MouseMsg{X: 50, Y: 10, Type: tea.MouseWheelDown})
	app = resModel.(AppModel)
	if app.CherryPickModal.SelectedIndex == 0 && len(app.CherryPickModal.VisibleNodes) > 1 {
		t.Fatalf("expected MouseWheelDown in ModalCherryPick to advance SelectedIndex")
	}

	// 6. Test RemoteModal spacebar (' ') toggle on AutoPush
	rm := modals.NewRemoteModal(models.RemoteRepoConfig{}, 120, 30)
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus index 1 (AutoPush)
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !rm.AutoPush {
		t.Fatalf("expected Space key (' ') to toggle AutoPush in RemoteModal")
	}
}

func TestReviewerModalInputsAndFilterRecovery(t *testing.T) {
	// 1. CherryPickModal CustomInput must accept all characters without swallowing 'a', 'e', 'f', 'h', 'j', 'k', 'l', 'x', etc.
	changes := []models.PathChange{
		{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffModified, NewValue: "Port 1"},
	}
	commit := models.TimelineCommit{CommitID: "abc12345", Message: "Test"}
	cm := modals.NewCherryPickModal(commit, changes, nil, 120, 40)
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyTab}) // switch to UseCustom
	if !cm.UseCustom {
		t.Fatalf("expected UseCustom=true after Tab")
	}
	targetXPath := "/interface[name=ethernet-1/1]/subinterface[index=0]"
	for _, r := range targetXPath {
		cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if cm.CustomInput.Value() != targetXPath {
		t.Fatalf("CherryPickModal CustomInput swallowed characters: got %q, want %q", cm.CustomInput.Value(), targetXPath)
	}
	selected := cm.GetSelectedPaths()
	if len(selected) != 1 || selected[0] != targetXPath {
		t.Fatalf("expected GetSelectedPaths() = [%q], got %v", targetXPath, selected)
	}

	// 2. ExportModal PathInput must be focused on creation and accept typed paths, while 1/2/3/up/down/tab switch format cleanly
	em := modals.NewExportModal(commit, 120, 40)
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if em.Format != "cli" || em.PathInput.Value() != "" {
		t.Fatalf("expected Format=cli and empty PathInput after '2', got fmt=%q val=%q", em.Format, em.PathInput.Value())
	}
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyDown})
	if em.Format != "startup" {
		t.Fatalf("expected Format=startup after Down arrow, got %q", em.Format)
	}
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	targetFile := "/tmp/export_123.json"
	for _, r := range targetFile {
		em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if em.PathInput.Value() != targetFile {
		t.Fatalf("ExportModal PathInput failed to capture typed path: got %q, want %q", em.PathInput.Value(), targetFile)
	}

	// 3. ConfigView and BlameView must recover full content after clearing a non-matching filter
	tempDir := t.TempDir()
	backend := gitbackend.NewGitBackend(filepath.Join(tempDir, "repo"))
	cfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-filter-test"},
		},
	}
	_, _, err := backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Commit — with em-dash", nil)
	if err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app := resModel.(AppModel)

	// Verify 80-col timeline row does not wrap past 3 lines and preserves valid UTF-8 for em-dash
	tvLines := strings.Split(app.TimelineView.Viewport.View(), "\n")
	nonEmpty := 0
	for _, l := range tvLines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 3 {
		t.Fatalf("expected single commit in 80-col TimelineView to occupy at most 3 non-empty lines, got %d:\n%s", nonEmpty, app.TimelineView.Viewport.View())
	}

	resModel, _ = app.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app = resModel.(AppModel)

	// Switch to TabConfig and apply non-matching filter, then clear it
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	app.ConfigView.FilterPath = "/nonexistent_xyz"
	app.ConfigView.UpdateContent()
	if !strings.Contains(app.ConfigView.Viewport.View(), "No CLI configuration statements match") {
		t.Fatalf("expected no-match filter message in ConfigView, got:\n%s", app.ConfigView.Viewport.View())
	}
	app.ConfigView.FilterPath = ""
	app.ConfigView.UpdateContent()
	if !strings.Contains(app.ConfigView.Viewport.View(), "srl-filter-test") {
		t.Fatalf("ConfigView failed to restore content after clearing non-matching filter, got:\n%s", app.ConfigView.Viewport.View())
	}

	// Same for BlameView (empty entries -> populated entries recovery)
	app.BlameView.SetBlame(nil, app.BlameView.Stats)
	if !strings.Contains(app.BlameView.Viewport.View(), "No blame entries available") {
		t.Fatalf("expected empty blame message in BlameView")
	}
	app.BlameView.SetBlame([]models.BlameEntry{
		{CommitSHA: "1234567", Author: "admin", Timestamp: time.Now().UTC(), Content: "set / system name host-name srl-filter-test — ok"},
	}, app.BlameView.Stats)
	if !strings.Contains(app.BlameView.Viewport.View(), "1234567") || !strings.Contains(app.BlameView.Viewport.View(), "set / system name host-name") {
		t.Fatalf("BlameView failed to restore content after empty entries, got:\n%s", app.BlameView.Viewport.View())
	}

	// 4. Test TabConfig sub-option mouse click on [2] JSON at X = timelineW + 21, Y = 6
	app.Notification = ""
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	timelineW := (120 * 38) / 100
	resModel, _ = app.Update(tea.MouseMsg{X: timelineW + 21, Y: 6, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ConfigView.FormatMode != "json" {
		t.Fatalf("expected click at X=timelineW+21, Y=6 in TabConfig to select FormatMode=json, got %q", app.ConfigView.FormatMode)
	}

	// 5. Test in-flight restore/cherry-pick guard (isRestoring)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalRestore {
		t.Fatalf("expected ModalRestore to open on 'r'")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	app = resModel.(AppModel)
	if !app.isRestoring {
		t.Fatalf("expected isRestoring=true while restore command is in flight")
	}
	// Attempting to open Restore or CherryPick while isRestoring is true must be blocked
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalNone {
		t.Fatalf("expected ModalCherryPick to be blocked while isRestoring=true")
	}
	// Receiving NotificationMsg clears isRestoring
	resModel, _ = app.Update(NotificationMsg{Message: "Restored", Severity: "success"})
	app = resModel.(AppModel)
	if app.isRestoring {
		t.Fatalf("expected isRestoring=false after NotificationMsg completion")
	}
}

func TestFullBleedSurfaceAndMultiThemeInvariants(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")
	defer func() {
		_, _ = SetTheme("dark")
	}()

	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-theme-test"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Old Port"},
		},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-theme-test"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "New Port"},
		},
	}
	if _, _, err := backend.RecordConfigChange(cfg1, "admin", "1", "", time.Now().UTC().Add(-time.Minute), "Initial commit", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.RecordConfigChange(cfg2, "netops", "2", "", time.Now().UTC(), "Update interface description", nil); err != nil {
		t.Fatal(err)
	}
	model := NewAppModel(backend, nil, "")

	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	app := resModel.(AppModel)

	// Verify live 'T' key cycles themes and updates notification banner
	initialTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	nextTheme := CurrentTheme().Name
	if nextTheme == initialTheme {
		t.Fatalf("expected 'T' key to advance theme from %q, still %q", initialTheme, nextTheme)
	}
	if !strings.Contains(app.Notification, nextTheme) {
		t.Fatalf("expected notification banner to mention new theme %q, got %q", nextTheme, app.Notification)
	}

	// Verify multiline notifications are clamped to 1 line and preserve exact screen height
	resModel, _ = app.Update(NotificationMsg{
		Message:  "Multiline error:\nline two of error\r\nline three of error",
		Severity: "error",
	})
	app = resModel.(AppModel)
	assertNoUnrecoveredReset(t, "multiline-notification-120x35", app.View(), 120, 35)

	// Clear notification
	app.setNotification("", app.NotifyStyle)

	sizes := []struct{ w, h int }{
		{80, 24},
		{120, 35},
	}

	for _, theme := range AvailableThemes() {
		_, ok := SetTheme(theme.Name)
		if !ok {
			t.Fatalf("SetTheme(%q) failed", theme.Name)
		}
		app.RefreshTheme()

		for _, sz := range sizes {
			resModel, _ = app.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			app = resModel.(AppModel)

			// 1. DiffView: unified, path, cli, vsLive
			app.ActiveModal = ModalNone
			app.ActiveTab = TabDiff
			for _, mode := range []string{"unified", "path", "cli"} {
				app.DiffView.DiffMode = mode
				app.DiffView.UpdateContent()
				assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/diff-%s", theme.Name, sz.w, sz.h, mode), app.View(), sz.w, sz.h)
			}

			// 2. ConfigView: cli, json
			app.ActiveTab = TabConfig
			app.updateActiveTabContent()
			for _, fmtMode := range []string{"cli", "json"} {
				app.ConfigView.FormatMode = fmtMode
				app.ConfigView.UpdateContent()
				assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/config-%s", theme.Name, sz.w, sz.h, fmtMode), app.View(), sz.w, sz.h)
			}

			// 3. BlameView
			app.ActiveTab = TabBlame
			app.updateActiveTabContent()
			assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/blame", theme.Name, sz.w, sz.h), app.View(), sz.w, sz.h)

			// 4. All 5 Modals
			selected := app.TimelineView.SelectedCommit()
			if selected == nil {
				t.Fatalf("expected non-nil selected commit")
			}

			app.RestoreModal = modals.NewRestoreModal(*selected, sz.w, sz.h)
			app.ActiveModal = ModalRestore
			assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/modal-restore", theme.Name, sz.w, sz.h), app.View(), sz.w, sz.h)

			app.CherryPickModal = modals.NewCherryPickModal(*selected, app.DiffView.DiffResult.Changes, map[string]interface{}{
				"interface": []interface{}{
					map[string]interface{}{"name": "ethernet-1/1", "description": "test"},
				},
			}, sz.w, sz.h)
			app.ActiveModal = ModalCherryPick
			assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/modal-cherrypick", theme.Name, sz.w, sz.h), app.View(), sz.w, sz.h)

			app.ExportModal = modals.NewExportModal(*selected, sz.w, sz.h)
			app.ActiveModal = ModalExport
			assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/modal-export", theme.Name, sz.w, sz.h), app.View(), sz.w, sz.h)

			app.RemoteModal = modals.NewRemoteModal(models.RemoteRepoConfig{
				URL:        "git@github.com:example/srl-configs.git",
				Branch:     "main",
				AutoPush:   true,
				SyncStatus: "Up to date",
				PublicKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@srl",
			}, sz.w, sz.h)
			app.ActiveModal = ModalRemote
			assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/modal-remote", theme.Name, sz.w, sz.h), app.View(), sz.w, sz.h)

			app.HelpModal = modals.NewHelpModal(sz.w, sz.h)
			app.ActiveModal = ModalHelp
			assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/modal-help", theme.Name, sz.w, sz.h), app.View(), sz.w, sz.h)

			// At 80x24, verify HelpModal and CherryPickModal (with many nodes) do not vertically clip their bottom border ╰
			if sz.w == 80 && sz.h == 24 {
				helpView := app.View()
				if !strings.Contains(helpView, "╰") || !strings.Contains(helpView, "Press [Esc], [Enter], or [?] to close") {
					t.Fatalf("theme=%s/80x24/modal-help clipped bottom border or footer:\n%s", theme.Name, helpView)
				}

				manyChanges := []models.PathChange{
					{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffModified, NewValue: "P1"},
					{Path: "/interface[name=ethernet-1/2]/description", DiffType: models.DiffModified, NewValue: "P2"},
					{Path: "/interface[name=ethernet-1/3]/description", DiffType: models.DiffModified, NewValue: "P3"},
					{Path: "/interface[name=ethernet-1/4]/description", DiffType: models.DiffModified, NewValue: "P4"},
					{Path: "/interface[name=ethernet-1/5]/description", DiffType: models.DiffModified, NewValue: "P5"},
					{Path: "/system/name/host-name", DiffType: models.DiffModified, NewValue: "srl-test"},
					{Path: "/network-instance[name=default]/protocols/bgp/admin-state", DiffType: models.DiffAdded, NewValue: "enable"},
				}
				cm := modals.NewCherryPickModal(*selected, manyChanges, nil, 80, 24)
				cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}) // expand all
				app.CherryPickModal = cm
				app.ActiveModal = ModalCherryPick
				cpView := app.View()
				assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/80x24/modal-cherrypick-expanded", theme.Name), cpView, 80, 24)
				if !strings.Contains(cpView, "╰") {
					t.Fatalf("theme=%s/80x24/modal-cherrypick-expanded clipped bottom border ╰:\n%s", theme.Name, cpView)
				}
			}

			app.ActiveModal = ModalNone
		}
	}

	// Verify extreme narrow/short terminal (40x12) does not overflow split widths or corrupt ANSI resets
	tlW, dtW := computeSplitOuterWidths(40)
	if tlW+1+dtW != 40 {
		t.Fatalf("computeSplitOuterWidths(40) = (%d, %d), expected tlW+1+dtW == 40", tlW, dtW)
	}
	resModel, _ = app.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	app = resModel.(AppModel)
	for _, tab := range []ActiveTab{TabDiff, TabConfig, TabBlame} {
		app.ActiveModal = ModalNone
		app.ActiveTab = tab
		app.updateActiveTabContent()
		assertNoUnrecoveredReset(t, fmt.Sprintf("narrow-40x12/tab-%d", tab), app.View(), 40, 12)
	}
	selected := app.TimelineView.SelectedCommit()
	app.HelpModal = modals.NewHelpModal(40, 12)
	app.ActiveModal = ModalHelp
	assertNoUnrecoveredReset(t, "narrow-40x12/modal-help", app.View(), 40, 12)

	// Verify live 'T' theme cycling works inside ModalHelp, ModalRestore, ModalCherryPick (!UseCustom),
	// ModalExport (Format=="startup"), and ModalRemote (FocusedIndex!=0), and does NOT intercept 'T' when textinput is focused.
	_, _ = SetTheme("dracula")
	app.RefreshTheme()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name == "dracula" {
		t.Fatalf("expected 'T' inside ModalHelp to cycle theme away from dracula")
	}
	cycledTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalNone || CurrentTheme().Name != cycledTheme {
		t.Fatalf("expected ModalNone and theme %q preserved after Esc, got modal=%v theme=%q", cycledTheme, app.ActiveModal, CurrentTheme().Name)
	}

	// ModalRestore: 'T' cycles theme without confirming or closing modal
	app.RestoreModal = modals.NewRestoreModal(*selected, 80, 24)
	app.ActiveModal = ModalRestore
	beforeRestTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalRestore || CurrentTheme().Name == beforeRestTheme {
		t.Fatalf("expected 'T' inside ModalRestore to cycle theme without closing modal, got modal=%v theme=%q",
			app.ActiveModal, CurrentTheme().Name)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)

	// ModalCherryPick: when !UseCustom, 'T' cycles theme; when UseCustom, 'T' types into CustomInput
	app.CherryPickModal = modals.NewCherryPickModal(*selected, app.DiffView.DiffResult.Changes, nil, 80, 24)
	app.ActiveModal = ModalCherryPick
	beforeCPTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name == beforeCPTheme {
		t.Fatalf("expected 'T' in ModalCherryPick (!UseCustom) to cycle theme")
	}
	app.CherryPickModal.UseCustom = true
	app.CherryPickModal.CustomInput.Focus()
	beforeCPCustomTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name != beforeCPCustomTheme || app.CherryPickModal.CustomInput.Value() != "T" {
		t.Fatalf("expected 'T' in ModalCherryPick (UseCustom) to type into CustomInput without changing theme, got val=%q theme=%q",
			app.CherryPickModal.CustomInput.Value(), CurrentTheme().Name)
	}

	// ModalExport: when Format == "json" (PathInput focused), 'T' types into PathInput; when Format == "startup", 'T' cycles theme
	app.ExportModal = modals.NewExportModal(*selected, 80, 24)
	app.ActiveModal = ModalExport
	beforeExpTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name != beforeExpTheme || app.ExportModal.PathInput.Value() != "T" {
		t.Fatalf("expected 'T' in ModalExport (Format=json) to type into PathInput, got val=%q", app.ExportModal.PathInput.Value())
	}
	app.ExportModal.Format = "startup"
	app.ExportModal.PathInput.Blur()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name == beforeExpTheme {
		t.Fatalf("expected 'T' in ModalExport (Format=startup) to cycle theme")
	}

	// ModalRemote: when FocusedIndex == 0 (URLInput focused), 'T' types into URLInput; when FocusedIndex != 0, 'T' cycles theme
	app.RemoteModal = modals.NewRemoteModal(models.RemoteRepoConfig{}, 80, 24)
	app.ActiveModal = ModalRemote
	beforeRemTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name != beforeRemTheme || app.RemoteModal.URLInput.Value() != "T" {
		t.Fatalf("expected 'T' in ModalRemote (FocusedIndex=0) to type into URLInput, got val=%q", app.RemoteModal.URLInput.Value())
	}
	app.RemoteModal.FocusedIndex = 1
	app.RemoteModal.URLInput.Blur()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name == beforeRemTheme {
		t.Fatalf("expected 'T' in ModalRemote (FocusedIndex=1) to cycle theme")
	}
	app.ActiveModal = ModalNone

	// Verify RefreshTheme clears ConfigView.lastRendered and BlameView.lastRendered across ascii <-> truecolor profiles
	app.ActiveTab = TabConfig
	app.updateActiveTabContent()
	if app.ConfigView.lastRendered == "" {
		t.Fatalf("expected non-empty ConfigView.lastRendered after updateActiveTabContent")
	}
	app.ActiveTab = TabBlame
	app.updateActiveTabContent()
	if app.BlameView.lastRendered == "" {
		t.Fatalf("expected non-empty BlameView.lastRendered after updateActiveTabContent")
	}
	_ = SetColorProfile("ascii")
	app.RefreshTheme()
	if strings.Contains(app.ConfigView.Viewport.View(), "\x1b[") || strings.Contains(app.BlameView.Viewport.View(), "\x1b[") {
		t.Fatalf("expected no ANSI SGR sequences in ConfigView/BlameView after switching to ascii profile and calling RefreshTheme()")
	}
	_ = SetColorProfile("truecolor")
	app.RefreshTheme()
	if !strings.Contains(app.ConfigView.Viewport.View(), "\x1b[") || !strings.Contains(app.BlameView.Viewport.View(), "\x1b[") {
		t.Fatalf("expected ANSI SGR sequences restored in ConfigView/BlameView after switching back to truecolor and calling RefreshTheme()")
	}

	// Verify success/error NotificationMsg uses high-contrast ColorSuccessBg / ColorDangerBg and updates on RefreshTheme()
	_, _ = SetTheme("dracula")
	app.RefreshTheme()
	resModel, _ = app.Update(NotificationMsg{Message: "Saved", Severity: "success"})
	app = resModel.(AppModel)
	if app.NotifyStyle.GetBackground() != ColorSuccessBg {
		t.Fatalf("expected success notification background %v (ColorSuccessBg), got %v", ColorSuccessBg, app.NotifyStyle.GetBackground())
	}
	_, _ = SetTheme("nord")
	app.RefreshTheme()
	if app.NotifyStyle.GetBackground() != ColorSuccessBg {
		t.Fatalf("expected success notification background to update to nord ColorSuccessBg (%v) after RefreshTheme(), got %v",
			ColorSuccessBg, app.NotifyStyle.GetBackground())
	}
	resModel, _ = app.Update(NotificationMsg{Message: "Failed", Severity: "error"})
	app = resModel.(AppModel)
	if app.NotifyStyle.GetBackground() != ColorDangerBg {
		t.Fatalf("expected error notification background %v (ColorDangerBg), got %v", ColorDangerBg, app.NotifyStyle.GetBackground())
	}
}



