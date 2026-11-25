package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"timeline/pkg/gitbackend"
	"timeline/pkg/models"
	"timeline/pkg/tui/modals"
)

var testANSIRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return testANSIRe.ReplaceAllString(s, "")
}

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

	// Check view output (border-embedded tabs and timeline header)
	viewStr := stripANSI(app.View())
	if !strings.Contains(viewStr, "TIMELINE") {
		t.Fatalf("expected TIMELINE in header")
	}
	if !strings.Contains(viewStr, "Diff") || !strings.Contains(viewStr, "Config") || !strings.Contains(viewStr, "Blame") {
		t.Fatalf("expected embedded Diff · Config · Blame tabs in detail border, got:\n%s", viewStr)
	}
	if app.DiffView.DiffMode != "cli" {
		t.Fatalf("expected default DiffMode to be 'cli', got %q", app.DiffView.DiffMode)
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

	// Test Blame tab (enable wrap so long TLS cert lines exceed viewport height and scroll)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected TabBlame")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	app = resModel.(AppModel)

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
	timelineW, detailW := computeSplitOuterWidths(120)
	detailStartX := timelineW + 1

	// 1. Click Filter Bar (Y=1, left input area)
	resModel, _ = app.Update(tea.MouseMsg{X: 20, Y: 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneFilter || !app.FocusFilter {
		t.Fatalf("expected FocusFilter to be true after clicking filter bar at Y=1")
	}

	tabRanges := DetailTabHitRanges(detailW, false)
	findChip := func(chips []HitChip, label string) HitChip {
		for _, c := range chips {
			if c.Label == label {
				return c
			}
		}
		t.Fatalf("missing hit chip %q in %+v", label, chips)
		return HitChip{}
	}

	// 2. Click Embedded Border Tab: Config (Y=2)
	cfgTab := findChip(tabRanges, "config")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + cfgTab.StartX + 1, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabConfig {
		t.Fatalf("expected ActiveTab to be TabConfig after clicking tab at Y=2, got %v", app.ActiveTab)
	}

	// 3. Click Embedded Border Tab: Blame (Y=2)
	blameTab := findChip(tabRanges, "blame")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + blameTab.StartX + 1, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected ActiveTab to be TabBlame after clicking tab at Y=2, got %v", app.ActiveTab)
	}

	// 4. Click Embedded Border Tab: Diff (Y=2)
	diffTab := findChip(tabRanges, "diff")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + diffTab.StartX + 1, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected ActiveTab to be TabDiff after clicking tab at Y=2, got %v", app.ActiveTab)
	}

	// 5. Click Sub-Option in Diff Toolbar: Path (Y=3)
	diffControls := app.DiffView.ControlHitRanges()
	pathChip := findChip(diffControls, "path")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + pathChip.StartX + 1, Y: 3, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "path" {
		t.Fatalf("expected DiffMode to be path after clicking sub-option at Y=3, got %v", app.DiffView.DiffMode)
	}

	// 6. Click Sub-Option in Diff Toolbar: vs Live (Y=3)
	liveChip := findChip(diffControls, "live")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + liveChip.StartX + 1, Y: 3, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if !app.VsLive {
		t.Fatalf("expected VsLive to be true after clicking vs Live at Y=3")
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
	cfgCLIViewStr := stripANSI(app.ConfigView.Viewport.View())
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
	cfgJSONViewStr := stripANSI(app.ConfigView.Viewport.View())
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

	// Switch to Blame Analytics sub-view ('2') where Subsystem Breakdown lives
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = resModel.(AppModel)
	if app.BlameView.ViewMode != "analytics" {
		t.Fatalf("expected BlameView.ViewMode=analytics after '2', got %q", app.BlameView.ViewMode)
	}

	// Focus Detail pane
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)

	// Scroll to bottom with 'G' (end)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	app = resModel.(AppModel)

	// Render view at bottom without errors
	v := stripANSI(app.View())
	if !strings.Contains(v, "Subsystem Breakdown") && !strings.Contains(v, "CONTRIBUTOR ANALYTICS") {
		t.Fatalf("expected Subsystem Breakdown / Analytics visible at bottom of Blame View")
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

	// 3. Test clearing via Mouse Click on [× Clear] badge (row Y=1)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i', 'n', 't'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)

	clearStart, clearEnd := app.FilterBar.ClearHitRange()
	if clearStart < 0 || clearEnd <= clearStart {
		t.Fatalf("expected valid ClearHitRange when filter is active, got [%d, %d)", clearStart, clearEnd)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: clearStart + 1, Y: 1, Type: tea.MouseLeft})
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

	// 4. Test mouse click hit-boxes remain stable (ZERO vertical jump) when inline footer toast is active
	app.setNotification("Test inline footer toast", lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent))
	timelineW, detailW := computeSplitOuterWidths(120)
	detailStartX := timelineW + 1
	diffTab := DetailTabHitRanges(detailW, false)[0]
	// Click embedded Top Tab Diff at Y = 2 (no vertical shift even with toast!)
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + diffTab.StartX + 1, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected click at Y=2 (with inline toast) to select TabDiff, got %v", app.ActiveTab)
	}
	// Click [1] Unified then [3] CLI sub-option in TabDiff toolbar at Y = 3
	controls := app.DiffView.ControlHitRanges()
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + controls[0].StartX + 1, Y: 3, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "unified" {
		t.Fatalf("expected click on Unified pill at Y=3 to select DiffMode=unified, got %q", app.DiffView.DiffMode)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + controls[2].StartX + 1, Y: 3, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "cli" {
		t.Fatalf("expected click on CLI pill at Y=3 (with inline toast) to select DiffMode=cli, got %q", app.DiffView.DiffMode)
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

	// 2. ExportModal focuses Format selector by default (1/2/3/up/down switch format cleanly),
	// and Tab moves focus to PathInput which accepts arbitrary characters including digits.
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
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus PathInput
	if !em.IsEditingPath() {
		t.Fatalf("expected ExportModal.IsEditingPath()=true after Tab")
	}
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
	if !strings.Contains(stripANSI(app.BlameView.Viewport.View()), "No blame entries available") {
		t.Fatalf("expected empty blame message in BlameView")
	}
	app.BlameView.SetBlame([]models.BlameEntry{
		{CommitSHA: "1234567", Author: "admin", Timestamp: time.Now().UTC(), Content: "set / system name host-name srl-filter-test — ok"},
	}, app.BlameView.Stats)
	blamePlain := stripANSI(app.BlameView.Viewport.View())
	if !strings.Contains(blamePlain, "1234567") || !strings.Contains(blamePlain, "set / system name host-name") {
		t.Fatalf("BlameView failed to restore content after empty entries, got:\n%s", blamePlain)
	}

	// 4. Test TabConfig sub-option mouse click on [2] JSON in toolbar (Y = 3)
	app.Notification = ""
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	timelineW, _ := computeSplitOuterWidths(120)
	detailStartX := timelineW + 1
	cfgControls := app.ConfigView.ControlHitRanges()
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + cfgControls[1].StartX + 1, Y: 3, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ConfigView.FormatMode != "json" {
		t.Fatalf("expected click on [2] JSON pill at Y=3 in TabConfig to select FormatMode=json, got %q", app.ConfigView.FormatMode)
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
		{40, 12},
		{80, 24},
		{120, 35},
		{220, 50},
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

			// 1. DiffView: unified, path, cli
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

			// 3. BlameView: attribution & analytics
			app.ActiveTab = TabBlame
			app.updateActiveTabContent()
			for _, bm := range []string{"attribution", "analytics"} {
				app.BlameView.ViewMode = bm
				app.BlameView.UpdateContent()
				assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/blame-%s", theme.Name, sz.w, sz.h, bm), app.View(), sz.w, sz.h)
			}

			// 4. All 6 Modals (incl. ThemeModal)
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

			app.ThemeModal = modals.NewThemeModal(sz.w, sz.h)
			app.ActiveModal = ModalTheme
			assertNoUnrecoveredReset(t, fmt.Sprintf("theme=%s/%dx%d/modal-theme", theme.Name, sz.w, sz.h), app.View(), sz.w, sz.h)

			// Restore HelpModal for 80x24 bottom-border clip check
			app.ActiveModal = ModalHelp

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

	// ModalExport: when !IsEditingPath() (Format selector focused), 'T' cycles theme;
	// after Tab (IsEditingPath() == true), 'T' types into PathInput without cycling theme.
	app.ExportModal = modals.NewExportModal(*selected, 80, 24)
	app.ActiveModal = ModalExport
	beforeExpTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name == beforeExpTheme {
		t.Fatalf("expected 'T' in ModalExport (!IsEditingPath) to cycle theme")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus PathInput
	app = resModel.(AppModel)
	beforeExpPathTheme := CurrentTheme().Name
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	app = resModel.(AppModel)
	if CurrentTheme().Name != beforeExpPathTheme || app.ExportModal.PathInput.Value() != "T" {
		t.Fatalf("expected 'T' in ModalExport (IsEditingPath) to type into PathInput, got val=%q theme=%q",
			app.ExportModal.PathInput.Value(), CurrentTheme().Name)
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




func TestDefaultCLIDiffAndRemoteGitKeybinding(t *testing.T) {
	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	cfg := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-cli-default"}},
	}
	if _, _, err := backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial", nil); err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	if app.DiffView.DiffMode != "cli" {
		t.Fatalf("expected default DiffMode == 'cli', got %q", app.DiffView.DiffMode)
	}

	// 'g' must open Remote Git modal even when PaneDetail is focused
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneDetail {
		t.Fatalf("expected PaneDetail focused after Tab")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalRemote {
		t.Fatalf("expected 'g' to open ModalRemote, got %v", app.ActiveModal)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)

	// 'b' must switch to TabBlame (not be consumed as page-up)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected 'b' to switch to TabBlame, got %v", app.ActiveTab)
	}
}

func TestEmptyFilterClearsDetailViewsAndShowsFilterEmptyTimeline(t *testing.T) {
	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	cfg := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-stale-check"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Uplink-Alpha"},
		},
	}
	if _, _, err := backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial", nil); err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	// Populate all 3 detail views first
	for _, k := range []rune{'d', 'c', 'b'} {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		app = resModel.(AppModel)
	}
	if !strings.Contains(stripANSI(app.ConfigView.Viewport.View()), "Uplink-Alpha") {
		t.Fatalf("expected Uplink-Alpha in ConfigView before empty filter")
	}

	// Type a non-matching filter
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	for _, r := range "zzzz_no_such_path" {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = resModel.(AppModel)
	}

	if len(app.TimelineView.Commits) != 0 {
		t.Fatalf("expected 0 commits for non-matching filter, got %d", len(app.TimelineView.Commits))
	}

	tlText := stripANSI(app.TimelineView.Viewport.View())
	if !strings.Contains(tlText, "No commits match filter") || !strings.Contains(tlText, "zzzz_no_such_path") {
		t.Fatalf("expected filter-specific empty Timeline message, got:\n%s", tlText)
	}

	// All 3 detail views must be explicitly cleared with no lingering commit data
	if app.DiffView.Commit != nil || app.ConfigView.Commit != nil || app.BlameView.Commit != nil {
		t.Fatalf("expected all 3 Detail views to have Commit == nil on empty filter")
	}
	for name, vp := range map[string]string{
		"DiffView":   stripANSI(app.DiffView.Viewport.View()),
		"ConfigView": stripANSI(app.ConfigView.Viewport.View()),
		"BlameView":  stripANSI(app.BlameView.Viewport.View()),
	} {
		if strings.Contains(vp, "Uplink-Alpha") || strings.Contains(vp, "srl-stale-check") {
			t.Fatalf("%s retained stale content after empty filter:\n%s", name, vp)
		}
		if !strings.Contains(vp, "No commits match filter") {
			t.Fatalf("%s missing empty-filter notice, got:\n%s", name, vp)
		}
	}

	// Esc clears filter and recovers commits + detail views
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if len(app.TimelineView.Commits) != 1 {
		t.Fatalf("expected commit list to recover after Esc, got %d", len(app.TimelineView.Commits))
	}
}

func TestFullSHASelectionPreservationAndEnsureVisible(t *testing.T) {
	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	baseTime := time.Now().UTC().Add(-20 * time.Minute)
	for i := 1; i <= 16; i++ {
		cfg := map[string]interface{}{
			"system": map[string]interface{}{
				"name": map[string]interface{}{"host-name": fmt.Sprintf("srl-%02d", i)},
			},
		}
		if _, _, err := backend.RecordConfigChange(cfg, "admin", fmt.Sprintf("%d", i), "", baseTime.Add(time.Duration(i)*time.Minute), fmt.Sprintf("Commit %02d", i), nil); err != nil {
			t.Fatal(err)
		}
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 18})
	app := resModel.(AppModel)

	// Move down to commit index 10 (well past viewport height)
	for i := 0; i < 10; i++ {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
		app = resModel.(AppModel)
	}
	targetCommit := app.TimelineView.SelectedCommit()
	if targetCommit == nil {
		t.Fatalf("expected non-nil selected commit")
	}
	targetSHA := targetCommit.FullSHA

	// Simulate a new realtime commit arriving at HEAD
	newCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-17-realtime"},
		},
	}
	_, newSHA, err := backend.RecordConfigChange(newCfg, "netops", "17", "", time.Now().UTC(), "Realtime 17", nil)
	if err != nil {
		t.Fatal(err)
	}
	resModel, _ = app.Update(RealtimeCommitMsg{
		Commit: models.TimelineCommit{
			CommitID: newSHA[:8],
			FullSHA:  newSHA,
			Author:   "netops",
			Message:  "Realtime 17",
		},
	})
	app = resModel.(AppModel)

	// Selection must remain pinned to targetSHA (shifted from index 10 -> 11) and visible in viewport
	sel := app.TimelineView.SelectedCommit()
	if sel == nil || sel.FullSHA != targetSHA {
		t.Fatalf("expected selection preserved on %s after realtime commit, got %+v", targetSHA, sel)
	}
	if app.TimelineView.SelectedIndex != 11 {
		t.Fatalf("expected SelectedIndex shifted to 11, got %d", app.TimelineView.SelectedIndex)
	}
	selRowStart := app.TimelineView.SelectedIndex * app.TimelineView.LinesPerCommit()
	selRowEnd := selRowStart + app.TimelineView.LinesPerCommit() - 1
	if selRowStart < app.TimelineView.Viewport.YOffset || selRowEnd >= app.TimelineView.Viewport.YOffset+app.TimelineView.Viewport.Height {
		t.Fatalf("selected commit rows [%d..%d] not visible within viewport [%d..%d)",
			selRowStart, selRowEnd, app.TimelineView.Viewport.YOffset, app.TimelineView.Viewport.YOffset+app.TimelineView.Viewport.Height)
	}
}

func TestNotifySeqGuardAndZeroVerticalJump(t *testing.T) {
	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	cfg := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-toast"}},
	}
	if _, _, err := backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial", nil); err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	// Capture baseline layout geometry before toast
	viewBefore := app.View()
	linesBefore := strings.Split(viewBefore, "\n")
	tlHeightBefore := app.TimelineView.Height
	diffHeightBefore := app.DiffView.Height

	// Set Toast #1 (warning)
	seq1 := app.setNotification("Warning one", lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true))
	if app.NotifyStyle.GetBackground() != ColorWarningBg || app.NotifyStyle.GetForeground() != ColorOnWarning {
		t.Fatalf("expected warning toast to use ColorWarningBg/ColorOnWarning")
	}

	// Verify ZERO vertical jump when toast is active
	viewWithToast := app.View()
	linesWithToast := strings.Split(viewWithToast, "\n")
	if len(linesWithToast) != 30 || len(linesBefore) != 30 {
		t.Fatalf("expected exact 30 lines before (%d) and with toast (%d)", len(linesBefore), len(linesWithToast))
	}
	if app.TimelineView.Height != tlHeightBefore || app.DiffView.Height != diffHeightBefore {
		t.Fatalf("pane heights jumped when toast appeared: tl %d->%d, diff %d->%d",
			tlHeightBefore, app.TimelineView.Height, diffHeightBefore, app.DiffView.Height)
	}
	// Rows 0..28 (Header, Filter, Split Panes) must remain identical; only Row 29 (Footer) displays the toast
	for row := 0; row < 29; row++ {
		if linesBefore[row] != linesWithToast[row] {
			t.Fatalf("vertical jump detected at row %d when inline toast appeared", row)
		}
	}
	if !strings.Contains(stripANSI(linesWithToast[29]), "Warning one") {
		t.Fatalf("expected inline toast on footer row 29, got: %q", stripANSI(linesWithToast[29]))
	}

	// Set Toast #2 before Toast #1 timer fires
	seq2 := app.setNotification("Newer toast two", lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true))
	if seq2 <= seq1 {
		t.Fatalf("expected notifySeq to increment monotonically: seq1=%d seq2=%d", seq1, seq2)
	}

	// Stale timer for seq1 must NOT clear Toast #2
	resModel, _ = app.Update(clearNotificationMsg{seq: seq1})
	app = resModel.(AppModel)
	if app.Notification != "Newer toast two" {
		t.Fatalf("stale clearNotificationMsg(seq=%d) wiped newer toast; got %q", seq1, app.Notification)
	}

	// Matching timer for seq2 clears Toast #2 cleanly
	resModel, _ = app.Update(clearNotificationMsg{seq: seq2})
	app = resModel.(AppModel)
	if app.Notification != "" {
		t.Fatalf("expected matching clearNotificationMsg(seq=%d) to clear toast, got %q", seq2, app.Notification)
	}
}

func TestMouseHitRegionsAndStrictWheelRouting(t *testing.T) {
	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	var ifaces []interface{}
	for i := 1; i <= 25; i++ {
		ifaces = append(ifaces, map[string]interface{}{
			"name":        fmt.Sprintf("ethernet-1/%d", i),
			"description": fmt.Sprintf("Uplink port %d", i),
		})
	}
	cfg1 := map[string]interface{}{
		"system":    map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-mouse-1"}},
		"interface": ifaces,
	}
	cfg2 := map[string]interface{}{
		"system":    map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-mouse-2"}},
		"interface": ifaces,
	}
	t0 := time.Now().UTC().Add(-2 * time.Minute)
	if _, _, err := backend.RecordConfigChange(cfg1, "admin", "1", "", t0, "First commit", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.RecordConfigChange(cfg2, "netops", "2", "", t0.Add(time.Minute), "Second commit", nil); err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 130, Height: 24})
	app := resModel.(AppModel)
	timelineW, _ := computeSplitOuterWidths(130)
	detailStartX := timelineW + 1

	// 1. Header click outside Theme pill does NOT change focus or open modal
	resModel, _ = app.Update(tea.MouseMsg{X: 4, Y: 0, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline || app.ActiveModal != ModalNone {
		t.Fatalf("unexpected focus/modal change on non-theme header click: pane=%v modal=%v", app.FocusedPane, app.ActiveModal)
	}

	// 2. Header Theme pill click opens ModalTheme
	tStart, tEnd := app.HeaderBar.ThemeHitRange()
	if tStart < 0 || tEnd <= tStart {
		t.Fatalf("expected visible ThemeHitRange at 130 cols, got [%d, %d)", tStart, tEnd)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: tStart + 1, Y: 0, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalTheme {
		t.Fatalf("expected clicking Theme pill to open ModalTheme, got %v", app.ActiveModal)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)

	// 3. Footer click NEVER leaks into Detail or Timeline
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 10, Y: 23, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline {
		t.Fatalf("expected footer click not to shift focus to Detail, got %v", app.FocusedPane)
	}

	// 4. Quick-filter chip click applies filter and focuses Timeline
	chips := app.FilterBar.QuickChipHitRanges()
	if len(chips) == 0 {
		t.Fatalf("expected visible QuickChipHitRanges at 130 cols when filter is empty")
	}
	resModel, _ = app.Update(tea.MouseMsg{X: chips[0].StartX + 1, Y: 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != chips[0].Label || app.FocusedPane != PaneTimeline {
		t.Fatalf("expected quick chip %q to apply filter and focus Timeline, got val=%q pane=%v",
			chips[0].Label, app.FilterBar.Value(), app.FocusedPane)
	}

	// 5. Clicking filter match-count badge (just left of [× Clear]) must NOT clear the filter
	cStart, _ := app.FilterBar.ClearHitRange()
	if cStart < 6 {
		t.Fatalf("expected ClearHitRange to leave room for count badge, got start=%d", cStart)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: cStart - 3, Y: 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != chips[0].Label {
		t.Fatalf("clicking filter count badge must NOT clear filter; got %q", app.FilterBar.Value())
	}

	// Click explicit [× Clear] button clears filter
	resModel, _ = app.Update(tea.MouseMsg{X: cStart + 1, Y: 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != "" {
		t.Fatalf("expected Clear button click to clear filter, got %q", app.FilterBar.Value())
	}

	// 6. Strict wheel routing by cursor X:
	// Switch to Config tab and focus Detail, then wheel over Timeline (X < detailStartX) -> must move Timeline selection!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneDetail {
		t.Fatalf("expected PaneDetail focused")
	}
	cfgOffsetBefore := app.ConfigView.Viewport.YOffset
	resModel, _ = app.Update(tea.MouseMsg{X: 5, Y: 6, Type: tea.MouseWheelDown})
	app = resModel.(AppModel)
	if app.TimelineView.SelectedIndex != 1 {
		t.Fatalf("expected wheel over Timeline (while Detail focused) to advance Timeline to 1, got %d", app.TimelineView.SelectedIndex)
	}
	if app.ConfigView.Viewport.YOffset != cfgOffsetBefore {
		t.Fatalf("expected wheel over Timeline not to scroll Detail viewport")
	}

	// Now focus Timeline, then wheel over Detail (X >= detailStartX) -> must scroll Detail without moving Timeline!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline {
		t.Fatalf("expected PaneTimeline focused")
	}
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 10, Y: 8, Type: tea.MouseWheelDown})
	app = resModel.(AppModel)
	if app.TimelineView.SelectedIndex != 1 {
		t.Fatalf("expected wheel over Detail not to change Timeline SelectedIndex")
	}
	if app.ConfigView.Viewport.YOffset == 0 {
		t.Fatalf("expected wheel over Detail (while Timeline focused) to scroll Detail viewport")
	}

	// 7. Timeline row click selects commit using LinesPerCommit() (2 rows/commit)
	// Commit 0 is at Y=3..4, Commit 1 is at Y=5..6
	resModel, _ = app.Update(tea.MouseMsg{X: 10, Y: 3, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.TimelineView.SelectedIndex != 0 {
		t.Fatalf("expected click on Timeline row Y=3 to select commit 0, got %d", app.TimelineView.SelectedIndex)
	}

	// 8. Blame Attribution row click selects entry via EntryAtRow
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	vpStartY := 3 + app.BlameView.HeaderRows()
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 10, Y: vpStartY + 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.BlameView.SelectedLine != 2 {
		t.Fatalf("expected clicking Blame Attribution row 2 (Y=%d) to set SelectedLine=2, got %d", vpStartY+2, app.BlameView.SelectedLine)
	}
}

func TestKeyboardFlowZoomCompareThemeModalAndBlameJump(t *testing.T) {
	_, _ = SetTheme("dark")
	defer func() {
		_, _ = SetTheme("dark")
	}()

	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	t0 := time.Now().UTC().Add(-5 * time.Minute)
	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-v1"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Port-V1"},
		},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-v2"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Port-V1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Port-V2"},
		},
	}
	cfg3 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-v3"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Port-V1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Port-V3"},
		},
	}
	_, sha1, _ := backend.RecordConfigChange(cfg1, "alice", "1", "", t0, "Commit 1", nil)
	_, sha2, _ := backend.RecordConfigChange(cfg2, "bob", "2", "", t0.Add(time.Minute), "Commit 2", nil)
	_, sha3, _ := backend.RecordConfigChange(cfg3, "admin", "3", "", t0.Add(2*time.Minute), "Commit 3", nil)
	_ = sha2

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app := resModel.(AppModel)

	// 1. Enter in Timeline -> focuses Detail
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneDetail {
		t.Fatalf("expected Enter in Timeline to focus Detail, got %v", app.FocusedPane)
	}

	// 2. z toggles Pane Zoom & Esc hierarchy: unzoom -> Detail->Timeline -> clear filter
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	app = resModel.(AppModel)
	if !app.Zoomed || !app.DiffView.Zoomed || !app.ConfigView.Zoomed || !app.BlameView.Zoomed {
		t.Fatalf("expected Zoomed=true synced across AppModel and all 3 Detail views")
	}
	if app.DiffView.Width != 118 { // 120 - 2 borders
		t.Fatalf("expected zoomed DiffView inner width 118, got %d", app.DiffView.Width)
	}
	zoomedView := stripANSI(app.View())
	if strings.Contains(zoomedView, "TIMELINE") || !strings.Contains(zoomedView, "[ZOOM]") {
		t.Fatalf("expected zoomed view to hide TIMELINE and show [ZOOM] badge")
	}
	// First Esc unzooms (staying in PaneDetail)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.Zoomed || app.FocusedPane != PaneDetail {
		t.Fatalf("expected first Esc to unzoom while staying in PaneDetail, got zoomed=%v pane=%v", app.Zoomed, app.FocusedPane)
	}
	// Second Esc returns to PaneTimeline
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline {
		t.Fatalf("expected second Esc to return focus to PaneTimeline, got %v", app.FocusedPane)
	}

	// Shift+Tab cycles focus between Timeline and Detail
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneDetail {
		t.Fatalf("expected Shift+Tab to cycle to PaneDetail")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline {
		t.Fatalf("expected second Shift+Tab to cycle back to PaneTimeline")
	}

	// 3. PaneFilter: Tab and Down exit keeping query; Ctrl+c clears non-empty filter, quits when empty
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	for _, r := range "ethernet-1/2" {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = resModel.(AppModel)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline || app.FilterBar.Value() != "ethernet-1/2" {
		t.Fatalf("expected Tab in PaneFilter to exit to Timeline keeping query, got pane=%v val=%q", app.FocusedPane, app.FilterBar.Value())
	}
	// Re-enter filter and exit with Down
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline || app.FilterBar.Value() != "ethernet-1/2" {
		t.Fatalf("expected Down in PaneFilter to exit to Timeline keeping query")
	}
	// Re-enter filter: Ctrl+c clears non-empty filter without quitting
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	app = resModel.(AppModel)
	if app.FilterBar.Value() != "" {
		t.Fatalf("expected Ctrl+c in PaneFilter with query to clear filter")
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Fatalf("expected Ctrl+c on non-empty filter NOT to quit")
		}
	}
	// Ctrl+c on empty filter quits
	_, quitCmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quitCmd == nil {
		t.Fatalf("expected Ctrl+c on empty filter to return tea.Quit")
	}
	if _, isQuit := quitCmd().(tea.QuitMsg); !isQuit {
		t.Fatalf("expected QuitMsg from Ctrl+c on empty filter")
	}
	// Exit filter cleanly for next checks
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)

	// 4. Commit A <-> B Compare ('m')
	// Select oldest commit (sha1, index 2) and pin as base
	app.TimelineView.SelectIndex(2)
	app.updateActiveTabContent()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	app = resModel.(AppModel)
	if app.BaseCommitSHA != sha1 || app.TimelineView.BaseCommitSHA != sha1 {
		t.Fatalf("expected BaseCommitSHA=%s after 'm', got %q", sha1, app.BaseCommitSHA)
	}
	// Move to newest commit (sha3, index 0): diff should compare sha1 -> sha3 (contains srl-v1 -> srl-v3)
	app.TimelineView.SelectIndex(0)
	app.updateActiveTabContent()
	diffPlain := stripANSI(app.DiffView.Viewport.View())
	if !strings.Contains(diffPlain, "srl-v1") || !strings.Contains(diffPlain, "srl-v3") {
		t.Fatalf("expected A<->B diff (sha1->sha3) to show srl-v1 and srl-v3, got:\n%s", diffPlain)
	}
	if app.DiffView.BaseCommit == nil || app.DiffView.BaseCommit.FullSHA != sha1 {
		t.Fatalf("expected DiffView.BaseCommit pinned to sha1")
	}
	// Toggling 'v' (VsLive) unpins BaseCommit cleanly
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	app = resModel.(AppModel)
	if !app.VsLive || app.BaseCommitSHA != "" {
		t.Fatalf("expected 'v' to enable VsLive and unpin BaseCommit, got vsLive=%v base=%q", app.VsLive, app.BaseCommitSHA)
	}
	// Turn VsLive off
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	app = resModel.(AppModel)

	// 5. Instant visual mode toggles produce NO noisy toast
	app.Notification = ""
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "path" || app.Notification != "" {
		t.Fatalf("expected '2' in TabDiff to switch to path with no toast, got mode=%q notif=%q", app.DiffView.DiffMode, app.Notification)
	}

	// 6. Ctrl+t ThemeModal live preview + Esc revert + Enter confirm
	_, _ = SetTheme("dark")
	app.RefreshTheme()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalTheme {
		t.Fatalf("expected Ctrl+t to open ModalTheme, got %v", app.ActiveModal)
	}
	// Move down -> live preview changes active theme
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = resModel.(AppModel)
	previewed := CurrentTheme()
	if previewed.Name == "dark" {
		t.Fatalf("expected Down in ModalTheme to live-preview next theme")
	}
	// Esc -> reverts to initial theme ("dark")
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalNone || CurrentTheme().Name != "dark" {
		t.Fatalf("expected Esc in ModalTheme to close and revert to dark, got modal=%v theme=%q", app.ActiveModal, CurrentTheme().Name)
	}
	// Open again, move down, Enter -> confirms and shows concise toast
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = resModel.(AppModel)
	chosen := CurrentTheme()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalNone || CurrentTheme().Name != chosen.Name {
		t.Fatalf("expected Enter in ModalTheme to confirm %q", chosen.Name)
	}
	if app.Notification != fmt.Sprintf("Theme: %s", chosen.DisplayName) {
		t.Fatalf("expected toast 'Theme: %s', got %q", chosen.DisplayName, app.Notification)
	}

	// 7. Blame Attribution navigation + Enter jump-to-commit + filter-aware Analytics
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus Detail
	app = resModel.(AppModel)
	if app.BlameView.ViewMode != "attribution" {
		t.Fatalf("expected default BlameView.ViewMode=attribution")
	}
	// Find an entry attributed to sha1 (e.g. ethernet-1/1 description Port-V1)
	foundEntryIdx := -1
	for i, e := range app.BlameView.Entries {
		if strings.HasPrefix(sha1, e.CommitSHA) || strings.HasPrefix(e.CommitSHA, sha1[:8]) {
			foundEntryIdx = i
			break
		}
	}
	if foundEntryIdx < 0 {
		t.Fatalf("expected at least one Blame entry attributed to commit 1 (%s)", sha1[:8])
	}
	for app.BlameView.SelectedLine < foundEntryIdx {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
		app = resModel.(AppModel)
	}
	if app.BlameView.SelectedLine != foundEntryIdx {
		t.Fatalf("expected Down in Blame Attribution to advance SelectedLine to %d, got %d", foundEntryIdx, app.BlameView.SelectedLine)
	}
	// Press Enter -> jumps Timeline selection to sha1 and switches to TabDiff!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected Enter on Blame entry to switch to TabDiff, got %v", app.ActiveTab)
	}
	if sel := app.TimelineView.SelectedCommit(); sel == nil || sel.FullSHA != sha1 {
		t.Fatalf("expected Enter on Blame entry to jump Timeline to %s, got %+v", sha1, sel)
	}

	// Filter-aware Blame Analytics: filter for "ethernet-1/2" so only sha2/sha3 match,
	// then check Analytics header includes "(filtered: ethernet-1/2)" and Enter on sha1 entry shows toast!
	app.TimelineView.SelectIndex(0) // back to sha3
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	for _, r := range "ethernet-1/2" {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = resModel.(AppModel)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}}) // Analytics
	app = resModel.(AppModel)
	analyticsPlain := stripANSI(app.BlameView.Viewport.View())
	if !strings.Contains(analyticsPlain, "(filtered: ethernet-1/2)") {
		t.Fatalf("expected filter-aware Analytics header '(filtered: ethernet-1/2)', got:\n%s", analyticsPlain)
	}

	// 8. 'r' Restore opens ModalRestore with live impact diff populated
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) // clear filter
	app = resModel.(AppModel)
	app.TimelineView.SelectIndex(2) // select oldest commit sha1 (differs from latest sha3)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalRestore || !app.RestoreModal.HasImpact || !app.RestoreModal.Impact.HasChanges {
		t.Fatalf("expected 'r' to open ModalRestore with populated impact diff, got modal=%v hasImpact=%v hasChanges=%v",
			app.ActiveModal, app.RestoreModal.HasImpact, app.RestoreModal.Impact.HasChanges)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)

	// 9. Async summaryScopesMsg updates Timeline commits cleanly
	app.TimelineView.Commits[0].SummaryScope = ""
	resModel, _ = app.Update(summaryScopesMsg{
		scopes: map[string]string{sha3: "interface ethernet-1/2, system"},
	})
	app = resModel.(AppModel)
	if app.TimelineView.Commits[0].SummaryScope != "interface ethernet-1/2, system" {
		t.Fatalf("expected summaryScopesMsg to update commit SummaryScope, got %q", app.TimelineView.Commits[0].SummaryScope)
	}
}

func TestResizeDoesNotRequeryGitAndPreservesScrollOffset(t *testing.T) {
	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	var ifaces []interface{}
	for i := 1; i <= 40; i++ {
		ifaces = append(ifaces, map[string]interface{}{
			"name":        fmt.Sprintf("ethernet-1/%d", i),
			"description": fmt.Sprintf("Port description number %d", i),
		})
	}
	cfg1 := map[string]interface{}{
		"system":    map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-resize-1"}},
		"interface": ifaces,
	}
	if _, _, err := backend.RecordConfigChange(cfg1, "admin", "1", "", time.Now().UTC().Add(-time.Minute), "Commit 1", nil); err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	app := resModel.(AppModel)

	if len(app.TimelineView.Commits) != 1 {
		t.Fatalf("expected 1 commit initially, got %d", len(app.TimelineView.Commits))
	}

	// Switch to Config tab and scroll down
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)
	for i := 0; i < 6; i++ {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
		app = resModel.(AppModel)
	}
	savedOffset := app.ConfigView.Viewport.YOffset
	if savedOffset == 0 {
		t.Fatalf("expected non-zero ConfigView YOffset before resize")
	}

	// Add a second commit directly to GitBackend behind the TUI's back
	cfg2 := map[string]interface{}{
		"system":    map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-resize-2"}},
		"interface": ifaces,
	}
	if _, _, err := backend.RecordConfigChange(cfg2, "admin", "2", "", time.Now().UTC(), "Commit 2", nil); err != nil {
		t.Fatal(err)
	}

	// Send WindowSizeMsg: resizeLayout must NOT re-query Git or reset ConfigView scroll offset!
	resModel, _ = app.Update(tea.WindowSizeMsg{Width: 140, Height: 26})
	app = resModel.(AppModel)

	if len(app.TimelineView.Commits) != 1 {
		t.Fatalf("resizeLayout must NOT re-query Git (expected 1 commit still loaded, got %d)", len(app.TimelineView.Commits))
	}
	if app.ConfigView.Viewport.YOffset != savedOffset {
		t.Fatalf("expected resizeLayout to preserve ConfigView YOffset=%d, got %d", savedOffset, app.ConfigView.Viewport.YOffset)
	}

	// Verify computeSplitOuterWidths cap and narrow fallbacks
	cases := []struct {
		total  int
		wantTL int
		wantDT int
	}{
		{220, 50, 169},
		{120, 40, 79},
		{80, 34, 45},
		{40, 19, 20},
	}
	for _, tc := range cases {
		tl, dt := computeSplitOuterWidths(tc.total)
		if tl != tc.wantTL || dt != tc.wantDT || tl+1+dt != tc.total {
			t.Fatalf("computeSplitOuterWidths(%d) = (%d, %d), want (%d, %d)", tc.total, tl, dt, tc.wantTL, tc.wantDT)
		}
	}
}
