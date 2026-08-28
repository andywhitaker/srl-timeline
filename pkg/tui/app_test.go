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
