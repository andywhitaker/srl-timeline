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
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	app = resModel.(AppModel)
	if app.ActiveTab != TabConfig {
		t.Fatalf("expected TabConfig")
	}

	// Focus Detail Pane
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)

	initOffset := app.ConfigView.Viewport.YOffset
	t.Logf("Initial YOffset: %d", initOffset)

	// Scroll down with mouse wheel in detail pane (X=70, Y=12)
	for i := 0; i < 15; i++ {
		resModel, _ = app.Update(tea.MouseMsg{
			X:    70,
			Y:    12,
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
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
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
			Y:    12,
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

	mainStartY := 2 + app.TimelineRibbon.OuterHeight()

	// 2. Click Embedded Border Tab: Config (Y=mainStartY)
	cfgTab := findChip(tabRanges, "config")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + cfgTab.StartX + 1, Y: mainStartY, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabConfig {
		t.Fatalf("expected ActiveTab to be TabConfig after clicking tab at Y=%d, got %v", mainStartY, app.ActiveTab)
	}

	// 3. Click Embedded Border Tab: Blame (Y=mainStartY)
	blameTab := findChip(tabRanges, "blame")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + blameTab.StartX + 1, Y: mainStartY, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabBlame {
		t.Fatalf("expected ActiveTab to be TabBlame after clicking tab at Y=%d, got %v", mainStartY, app.ActiveTab)
	}

	// 4. Click Embedded Border Tab: Diff (Y=mainStartY)
	diffTab := findChip(tabRanges, "diff")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + diffTab.StartX + 1, Y: mainStartY, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected ActiveTab to be TabDiff after clicking tab at Y=%d, got %v", mainStartY, app.ActiveTab)
	}

	// 5. Click Sub-Option in Diff Toolbar: Path (Y=mainStartY+1)
	diffControls := app.DiffView.ControlHitRanges()
	pathChip := findChip(diffControls, "path")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + pathChip.StartX + 1, Y: mainStartY + 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "path" {
		t.Fatalf("expected DiffMode to be path after clicking sub-option at Y=%d, got %v", mainStartY+1, app.DiffView.DiffMode)
	}

	// 6. Click Sub-Option in Diff Toolbar: vs Live (Y=mainStartY+1)
	liveChip := findChip(diffControls, "live")
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + liveChip.StartX + 1, Y: mainStartY + 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if !app.VsLive {
		t.Fatalf("expected VsLive to be true after clicking vs Live at Y=%d", mainStartY+1)
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
	mainStartY := 2 + app.TimelineRibbon.OuterHeight()
	diffTab := DetailTabHitRanges(detailW, false)[0]
	// Click embedded Top Tab Diff at Y = mainStartY (no vertical shift even with toast!)
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + diffTab.StartX + 1, Y: mainStartY, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveTab != TabDiff {
		t.Fatalf("expected click at Y=%d (with inline toast) to select TabDiff, got %v", mainStartY, app.ActiveTab)
	}
	// Click [1] Unified then [3] CLI sub-option in TabDiff toolbar at Y = mainStartY + 1
	controls := app.DiffView.ControlHitRanges()
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + controls[0].StartX + 1, Y: mainStartY + 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "unified" {
		t.Fatalf("expected click on Unified pill at Y=%d to select DiffMode=unified, got %q", mainStartY+1, app.DiffView.DiffMode)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + controls[2].StartX + 1, Y: mainStartY + 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.DiffMode != "cli" {
		t.Fatalf("expected click on CLI pill at Y=%d (with inline toast) to select DiffMode=cli, got %q", mainStartY+1, app.DiffView.DiffMode)
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

	// 4. Test TabConfig sub-option mouse click on [2] JSON in toolbar (Y = mainStartY + 1)
	app.Notification = ""
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	timelineW, _ := computeSplitOuterWidths(120)
	detailStartX := timelineW + 1
	mainStartY := 2 + app.TimelineRibbon.OuterHeight()
	cfgControls := app.ConfigView.ControlHitRanges()
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + cfgControls[1].StartX + 1, Y: mainStartY + 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ConfigView.FormatMode != "json" {
		t.Fatalf("expected click on [2] JSON pill at Y=%d in TabConfig to select FormatMode=json, got %q", mainStartY+1, app.ConfigView.FormatMode)
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
	mainStartY := 2 + app.TimelineRibbon.OuterHeight()
	resModel, _ = app.Update(tea.MouseMsg{X: 10, Y: mainStartY + 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.TimelineView.SelectedIndex != 0 {
		t.Fatalf("expected click on Timeline row Y=%d to select commit 0, got %d", mainStartY+1, app.TimelineView.SelectedIndex)
	}

	// 8. Blame Attribution row click selects entry via EntryAtRow
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	vpStartY := mainStartY + 1 + app.BlameView.HeaderRows()
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
	if app.DiffView.Width != 114 { // 120 - 3 collapsed ▶ rail - 1 gap - 2 borders
		t.Fatalf("expected zoomed DiffView inner width 114, got %d", app.DiffView.Width)
	}
	zoomedView := stripANSI(app.View())
	if strings.Contains(zoomedView, "TIMELINE 1/3") || !strings.Contains(zoomedView, "[ZOOM]") || !strings.Contains(zoomedView, "▶") {
		t.Fatalf("expected zoomed view to hide left commit list TIMELINE 1/3 and show [ZOOM] badge + ▶ rail")
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

	// Shift+Tab cycles focus backwards across visible panes: Timeline -> Ribbon -> Detail -> Timeline
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneRibbon {
		t.Fatalf("expected Shift+Tab from Timeline to cycle to PaneRibbon, got %v", app.FocusedPane)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneDetail {
		t.Fatalf("expected second Shift+Tab to cycle to PaneDetail, got %v", app.FocusedPane)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline {
		t.Fatalf("expected third Shift+Tab to cycle back to PaneTimeline, got %v", app.FocusedPane)
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

func TestDiffTargetUXParentLookupToggleAndFooter(t *testing.T) {
	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	t0 := time.Now().UTC().Add(-5 * time.Minute)
	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-1"}},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-2"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Uplink"},
		},
	}
	cfg3 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-3"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Uplink"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Edge-Special"},
		},
	}
	_, sha1, err := backend.RecordConfigChange(cfg1, "alice", "1", "", t0, "Initial", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha2, err := backend.RecordConfigChange(cfg2, "bob", "2", "", t0.Add(time.Minute), "Add eth1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha3, err := backend.RecordConfigChange(cfg3, "admin", "3", "", t0.Add(2*time.Minute), "Add eth2", nil)
	if err != nil {
		t.Fatal(err)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	app := resModel.(AppModel)

	// 1. Default selection (HEAD = sha3) displays Prev <sha2> → <sha3> and OFF toggle "[4] ○ vs Running"
	if app.DiffView.ParentSHA != sha2[:8] {
		t.Fatalf("expected ParentSHA=%s for HEAD commit, got %q", sha2[:8], app.DiffView.ParentSHA)
	}
	viewOff := stripANSI(app.View())
	if !strings.Contains(viewOff, "Prev "+sha2[:8]+" → "+sha3[:8]) {
		t.Fatalf("expected Prev %s → %s in DiffView context strip, got:\n%s", sha2[:8], sha3[:8], viewOff)
	}
	if !strings.Contains(viewOff, GlyphPipe+" [4] "+GlyphPaused+" vs Running") && !strings.Contains(viewOff, GlyphPipe+"  [4] "+GlyphPaused+" vs Running") {
		t.Fatalf("expected separated OFF toggle '[4] %s vs Running' in toolbar, got:\n%s", GlyphPaused, viewOff)
	}

	// 2. Filtering to "ethernet-1/2" leaves only sha3 in filtered timeline, yet ParentSHA MUST still be sha2!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	for _, r := range "ethernet-1/2" {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = resModel.(AppModel)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if len(app.TimelineView.Commits) != 1 || app.DiffView.ParentSHA != sha2[:8] {
		t.Fatalf("filtered commit lost true parent SHA: len=%d parent=%q want=%q", len(app.TimelineView.Commits), app.DiffView.ParentSHA, sha2[:8])
	}
	// Clear filter
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)

	// 3. Initial/root commit (sha1, index 2) has ParentSHA == "" and renders "Initial → <sha1>"
	app.TimelineView.SelectIndex(2)
	app.updateActiveTabContent()
	if app.DiffView.ParentSHA != "" {
		t.Fatalf("expected empty ParentSHA for initial commit, got %q", app.DiffView.ParentSHA)
	}
	viewInit := stripANSI(app.View())
	if !strings.Contains(viewInit, "Initial → "+sha1[:8]) {
		t.Fatalf("expected 'Initial → %s' in DiffView context strip, got:\n%s", sha1[:8], viewInit)
	}

	// 4. Select HEAD (sha3), focus Detail, verify footer shows "4/v vs Running"
	app.TimelineView.SelectIndex(0)
	app.updateActiveTabContent()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = resModel.(AppModel)
	app.Notification = ""
	footerOff := stripANSI(strings.Split(app.View(), "\n")[34])
	if !strings.Contains(footerOff, "4/v vs Running") {
		t.Fatalf("expected Detail/Diff footer to show '4/v vs Running', got: %q", footerOff)
	}

	// 5. Press '4' -> VsLive ON: warning toast, "[4] ● vs Running" visible, "● RUNNING CONFIG" + "✓ Matches Running",
	//    context-aware empty message, and footer (once toast clears) shows "4/v vs Prev"
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	app = resModel.(AppModel)
	if !app.VsLive {
		t.Fatalf("expected VsLive=true after pressing '4'")
	}
	wantOnToast := fmt.Sprintf("Comparing %s → Live Running Config (press 4 to return to vs Prev)", sha3[:8])
	if app.Notification != wantOnToast {
		t.Fatalf("unexpected VsLive ON toast: got %q, want %q", app.Notification, wantOnToast)
	}
	if app.NotifyStyle.GetBackground() != ColorWarningBg {
		t.Fatalf("expected VsLive ON toast background ColorWarningBg, got %v", app.NotifyStyle.GetBackground())
	}
	viewOn := stripANSI(app.View())
	if !strings.Contains(viewOn, "[4] "+GlyphLive+" vs Running") {
		t.Fatalf("expected '[4] %s vs Running' visible when VsLive=true, got:\n%s", GlyphLive, viewOn)
	}
	if !strings.Contains(viewOn, sha3[:8]+" →  "+GlyphLive+" RUNNING CONFIG ") {
		t.Fatalf("expected '%s →  %s RUNNING CONFIG ' in context strip, got:\n%s", sha3[:8], GlyphLive, viewOn)
	}
	if !strings.Contains(viewOn, "Revision "+sha3[:8]+" matches the live running configuration") {
		t.Fatalf("expected context-aware empty diff message for HEAD vs Running, got:\n%s", viewOn)
	}
	app.Notification = ""
	footerOn := stripANSI(strings.Split(app.View(), "\n")[34])
	if !strings.Contains(footerOn, "4/v vs Prev") {
		t.Fatalf("expected Detail/Diff footer with VsLive=true to show '4/v vs Prev', got: %q", footerOn)
	}

	// 6. Mouse click on [4] chip toggles VsLive back OFF with primary toast!
	timelineW, _ := computeSplitOuterWidths(120)
	detailStartX := timelineW + 1
	var liveChip HitChip
	for _, c := range app.DiffView.ControlHitRanges() {
		if c.Label == "live" {
			liveChip = c
			break
		}
	}
	mainStartY := 2 + app.TimelineRibbon.OuterHeight()
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + liveChip.StartX + 2, Y: mainStartY + 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.VsLive {
		t.Fatalf("expected mouse click on [4] toggle chip to disable VsLive")
	}
	wantOffToast := "Comparing vs Previous Commit (press 4 to compare vs Running Config)"
	if app.Notification != wantOffToast {
		t.Fatalf("unexpected VsLive OFF toast: got %q, want %q", app.Notification, wantOffToast)
	}
	if app.NotifyStyle.GetBackground() != ColorPrimary {
		t.Fatalf("expected VsLive OFF toast background ColorPrimary, got %v", app.NotifyStyle.GetBackground())
	}
}

func TestVisualTimelineRibbonIntegrationZoomPanAndFilterSync(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	baseTime := time.Now().UTC().Truncate(time.Second)

	cfgs := []struct {
		ts   time.Time
		msg  string
		data map[string]interface{}
	}{
		{
			ts:  baseTime.Add(-10 * 24 * time.Hour),
			msg: "Initial baseline",
			data: map[string]interface{}{
				"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-rib-1"}},
				"network-instance": []interface{}{
					map[string]interface{}{
						"name": "default",
						"protocols": map[string]interface{}{
							"bgp": map[string]interface{}{
								"afi-safi": []interface{}{
									map[string]interface{}{"afi-safi-name": "ipv4-unicast", "admin-state": "enable"},
								},
							},
						},
					},
				},
			},
		},
		{
			ts:  baseTime.Add(-3 * 24 * time.Hour),
			msg: "Add ethernet-1/1",
			data: map[string]interface{}{
				"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-rib-2"}},
				"interface": []interface{}{
					map[string]interface{}{"name": "ethernet-1/1", "description": "Uplink-1"},
				},
			},
		},
		{
			ts:  baseTime.Add(-6 * time.Hour),
			msg: "Add ipv6-unicast peer",
			data: map[string]interface{}{
				"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-rib-3"}},
				"interface": []interface{}{
					map[string]interface{}{"name": "ethernet-1/1", "description": "Uplink-1"},
				},
				"network-instance": []interface{}{
					map[string]interface{}{
						"name": "default",
						"protocols": map[string]interface{}{
							"bgp": map[string]interface{}{
								"afi-safi": []interface{}{
									map[string]interface{}{"afi-safi-name": "ipv4-unicast", "admin-state": "enable"},
									map[string]interface{}{"afi-safi-name": "ipv6-unicast", "admin-state": "enable"},
								},
							},
						},
					},
				},
			},
		},
		{
			ts:  baseTime.Add(-15 * time.Minute),
			msg: "Update ethernet-1/1 description",
			data: map[string]interface{}{
				"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-rib-4"}},
				"interface": []interface{}{
					map[string]interface{}{"name": "ethernet-1/1", "description": "Core-Link-1"},
				},
			},
		},
	}

	var shas []string
	for i, c := range cfgs {
		_, sha, err := backend.RecordConfigChange(c.data, "admin", fmt.Sprintf("%d", i+1), "", c.ts, c.msg, nil)
		if err != nil {
			t.Fatal(err)
		}
		shas = append(shas, sha)
	}

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 136, Height: 36})
	app := resModel.(AppModel)

	if app.TimelineRibbon.OuterHeight() != 6 || len(app.TimelineView.Commits) != 4 {
		t.Fatalf("expected 6-row ribbon & 4 commits, got h=%d n=%d", app.TimelineRibbon.OuterHeight(), len(app.TimelineView.Commits))
	}
	assertNoUnrecoveredReset(t, "ribbon-int-136x36", app.View(), 136, 36)

	// 1. Click preset [1h] on ribbon row 0 (Y=2) -> narrows window to only the -15m commit
	var chip1h, chipAll HitChip
	for _, ch := range app.TimelineRibbon.TopChipHitRanges() {
		if ch.Label == "preset:1h" {
			chip1h = ch
		}
		if ch.Label == "preset:all" {
			chipAll = ch
		}
	}
	resModel, _ = app.Update(tea.MouseMsg{X: chip1h.StartX + 1, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.TimelineRibbon.ActivePreset != "1h" || len(app.TimelineView.Commits) != 1 || !app.TimelineView.WindowNarrowed {
		t.Fatalf("expected preset 1h to narrow to 1 commit, got preset=%q n=%d narrowed=%v",
			app.TimelineRibbon.ActivePreset, len(app.TimelineView.Commits), app.TimelineView.WindowNarrowed)
	}
	if !strings.Contains(stripANSI(app.View()), "IN WINDOW 1/1") {
		t.Fatalf("expected IN WINDOW 1/1 title when ribbon window is narrowed")
	}

	// Click [All] -> restores all 4 commits
	resModel, _ = app.Update(tea.MouseMsg{X: chipAll.StartX + 1, Y: 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.TimelineRibbon.ActivePreset != "all" || len(app.TimelineView.Commits) != 4 {
		t.Fatalf("expected preset All to restore 4 commits, got preset=%q n=%d", app.TimelineRibbon.ActivePreset, len(app.TimelineView.Commits))
	}

	// 2. Keyboard window edge nudges [, ], pan <, zoom +, reset 0, fit f
	origStart := app.TimelineRibbon.WindowStart
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	app = resModel.(AppModel)
	if !app.TimelineRibbon.WindowStart.After(origStart) {
		t.Fatalf("expected '[' to nudge WindowStart right")
	}
	origEnd := app.TimelineRibbon.WindowEnd
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	app = resModel.(AppModel)
	if !app.TimelineRibbon.WindowEnd.Before(origEnd) {
		t.Fatalf("expected ']' to nudge WindowEnd left")
	}
	spanBeforeZoom := app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	app = resModel.(AppModel)
	if app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart) >= spanBeforeZoom {
		t.Fatalf("expected '+' to zoom in")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	app = resModel.(AppModel)
	if app.TimelineRibbon.ActivePreset != "all" || len(app.TimelineView.Commits) != 4 {
		t.Fatalf("expected '0' to reset window to All")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	app = resModel.(AppModel)
	if app.TimelineRibbon.ActivePreset != "fit" {
		t.Fatalf("expected 'f' to FitToMatches, got preset=%q", app.TimelineRibbon.ActivePreset)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	app = resModel.(AppModel)

	// 3. Toggle ribbon collapse via 'w'
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	app = resModel.(AppModel)
	if !app.TimelineRibbon.Collapsed || app.TimelineRibbon.OuterHeight() != 1 {
		t.Fatalf("expected 'w' to collapse ribbon to 1 row")
	}
	assertNoUnrecoveredReset(t, "ribbon-collapsed-136x36", app.View(), 136, 36)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	app = resModel.(AppModel)
	if app.TimelineRibbon.Collapsed || app.TimelineRibbon.OuterHeight() != 6 {
		t.Fatalf("expected second 'w' to expand ribbon back to 6 rows")
	}

	// 4. Focus PaneRibbon and step commits with ← / → and enter workspace with Enter
	app.TimelineView.SelectIndex(0)
	app.syncCompareMarkers()
	app.setFocusedPane(PaneRibbon)
	if app.TimelineView.SelectedIndex != 0 {
		t.Fatalf("expected newest commit 0 selected initially")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app = resModel.(AppModel)
	if app.TimelineView.SelectedIndex != 1 {
		t.Fatalf("expected ← in PaneRibbon to move to older commit 1, got %d", app.TimelineView.SelectedIndex)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRight})
	app = resModel.(AppModel)
	if app.TimelineView.SelectedIndex != 0 {
		t.Fatalf("expected → in PaneRibbon to move back to newer commit 0, got %d", app.TimelineView.SelectedIndex)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline {
		t.Fatalf("expected Enter in PaneRibbon to enter PaneTimeline, got %v", app.FocusedPane)
	}

	// 5. Mouse wheel zoom + scrubber drag (MouseLeft -> MouseMotion -> MouseRelease)
	spanAll := app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart)
	resModel, _ = app.Update(tea.MouseMsg{X: 60, Y: 3, Type: tea.MouseWheelUp})
	app = resModel.(AppModel)
	if app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart) >= spanAll {
		t.Fatalf("expected MouseWheelUp over ribbon to zoom in")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	app = resModel.(AppModel)

	layout := app.TimelineRibbon.computeLayout()
	origDragStart := app.TimelineRibbon.WindowStart
	resModel, _ = app.Update(tea.MouseMsg{X: layout.leftHandleX, Y: 2 + 4, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.TimelineRibbon.DragMode != "left" {
		t.Fatalf("expected left handle drag start, got %q", app.TimelineRibbon.DragMode)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: layout.leftHandleX + 20, Y: 2 + 4, Type: tea.MouseMotion})
	app = resModel.(AppModel)
	if !app.TimelineRibbon.WindowStart.After(origDragStart) {
		t.Fatalf("expected MouseMotion on left handle to advance WindowStart")
	}
	resModel, _ = app.Update(tea.MouseMsg{X: layout.leftHandleX + 20, Y: 2 + 4, Type: tea.MouseRelease})
	app = resModel.(AppModel)
	if app.TimelineRibbon.DragMode != "" {
		t.Fatalf("expected MouseRelease to clear DragMode")
	}

	// 6. Shift+Click on oldest commit node pins Left A and enters Split Diff
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.MouseMsg{X: 1, Y: 4, Type: tea.MouseLeft, Shift: true})
	app = resModel.(AppModel)
	if app.LeftCommitSHA != shas[0] || !app.SplitDiff {
		t.Fatalf("expected Shift+Click on oldest node to pin Left A=%s and enable SplitDiff, got left=%q split=%v",
			shas[0], app.LeftCommitSHA, app.SplitDiff)
	}
}

func TestSideBySideSplitDiffPickerAndChronologicalNormalization(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	t0 := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)

	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-split-1"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Old-Link"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Shared-2"},
			map[string]interface{}{"name": "ethernet-1/3", "description": "Shared-3"},
			map[string]interface{}{"name": "ethernet-1/4", "description": "Shared-4"},
			map[string]interface{}{"name": "ethernet-1/5", "description": "Shared-5"},
			map[string]interface{}{"name": "ethernet-1/6", "description": "Shared-6"},
			map[string]interface{}{"name": "ethernet-1/7", "description": "Shared-7"},
		},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-split-2"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Mid-Link"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Shared-2"},
			map[string]interface{}{"name": "ethernet-1/3", "description": "Shared-3"},
			map[string]interface{}{"name": "ethernet-1/4", "description": "Shared-4"},
			map[string]interface{}{"name": "ethernet-1/5", "description": "Shared-5"},
			map[string]interface{}{"name": "ethernet-1/6", "description": "Shared-6"},
			map[string]interface{}{"name": "ethernet-1/7", "description": "Shared-7"},
		},
	}
	cfg3 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-split-3"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "New-Link"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Shared-2"},
			map[string]interface{}{"name": "ethernet-1/3", "description": "Shared-3"},
			map[string]interface{}{"name": "ethernet-1/4", "description": "Shared-4"},
			map[string]interface{}{"name": "ethernet-1/5", "description": "Shared-5"},
			map[string]interface{}{"name": "ethernet-1/6", "description": "Shared-6"},
			map[string]interface{}{"name": "ethernet-1/7", "description": "Shared-7"},
		},
	}

	_, sha1, err := backend.RecordConfigChange(cfg1, "alice", "1", "", t0, "Commit 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha2, err := backend.RecordConfigChange(cfg2, "bob", "2", "", t0.Add(time.Hour), "Commit 2", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha3, err := backend.RecordConfigChange(cfg3, "admin", "3", "", t0.Add(2*time.Hour), "Commit 3", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = sha2

	model := NewAppModel(backend, nil, "")

	// 1. Width < 100 guard at 80x24: 'S' must stay single-pane and emit warning toast + ribbon auto-collapsed
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app := resModel.(AppModel)
	if !app.TimelineRibbon.Collapsed {
		t.Fatalf("expected ribbon auto-collapsed at Height=24")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	if app.SplitDiff || app.DiffView.IsSplitActive() {
		t.Fatalf("expected SplitDiff to stay false at 80 cols")
	}
	if !strings.Contains(app.Notification, "≥100 cols") {
		t.Fatalf("expected ≥100 cols warning toast, got %q", app.Notification)
	}
	assertNoUnrecoveredReset(t, "split-guard-80x24", app.View(), 80, 24)

	// 2. Resize to 136x36 and press 'S' -> Side-by-Side Split Diff activates while KEEPING left commit list open!
	resModel, _ = app.Update(tea.WindowSizeMsg{Width: 136, Height: 36})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	if !app.SplitDiff || !app.DiffView.IsSplitActive() || app.hideTimelineList() {
		t.Fatalf("expected active SplitDiff at 136x36 with left panel still open, got split=%v active=%v hideTL=%v",
			app.SplitDiff, app.DiffView.IsSplitActive(), app.hideTimelineList())
	}
	splitView := app.View()
	assertNoUnrecoveredReset(t, "split-active-136x36", splitView, 136, 36)
	plainSplit := stripANSI(splitView)
	if !strings.Contains(plainSplit, "[SPLIT A↔B]") || !strings.Contains(plainSplit, "A (OLDER)") || !strings.Contains(plainSplit, "B (NEWER)") || !strings.Contains(plainSplit, "[◀]") {
		t.Fatalf("expected split header badges and left-panel [◀] button in 136x36 split view:\n%s", plainSplit)
	}

	// 3. 'E' toggles FoldUnchanged (Folded ↔ Full Config) and 1/2/3 switch split diff modes cleanly
	if !app.FoldUnchanged {
		t.Fatalf("expected FoldUnchanged=true by default")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	app = resModel.(AppModel)
	if app.FoldUnchanged || app.DiffView.FoldUnchanged {
		t.Fatalf("expected 'E' to disable FoldUnchanged")
	}
	for _, tc := range []struct {
		key  rune
		mode string
	}{
		{'1', "unified"},
		{'2', "path"},
		{'3', "cli"},
	} {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		app = resModel.(AppModel)
		if app.DiffView.DiffMode != tc.mode || app.DiffView.SideBySideResult.Mode != tc.mode {
			t.Fatalf("key %q in split mode: expected DiffMode/SideBySideResult.Mode=%q, got %q/%q",
				tc.key, tc.mode, app.DiffView.DiffMode, app.DiffView.SideBySideResult.Mode)
		}
		assertNoUnrecoveredReset(t, "split-mode-"+tc.mode, app.View(), 136, 36)
	}

	// 4. Open unified 2-config CommitPickerModal via 'A':
	//    Clear selections ('c'), Space-select NEWEST (sha3) first then OLDER (sha1), verify
	//    strict max-2 enforcement on a 3rd Space, and confirm with Enter -> older sha1 is
	//    auto-assigned Left (A) and newer sha3 is Right (B)!
	app.LeftCommitSHA = sha1
	app.RightCommitSHA = sha2
	app.normalizeSplitEndpoints()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalCommitPicker {
		t.Fatalf("expected ModalCommitPicker after 'A', got modal=%v", app.ActiveModal)
	}
	assertNoUnrecoveredReset(t, "commit-picker-136x36", app.View(), 136, 36)
	// Press 'c' to clear all pre-selections, then check index 1 (sha3 newest) and index 3 (sha1 oldest) via Space.
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	if app.CommitPickerModal.SelectionCount() != 0 {
		t.Fatalf("expected 'c' to clear picker selections, got %d", app.CommitPickerModal.SelectionCount())
	}
	app.CommitPickerModal.SelectedIndex = 1 // sha3 (newest)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	// Enter with only 1 selected must keep modal open.
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalCommitPicker {
		t.Fatalf("expected ModalCommitPicker to stay open on Enter when only 1/2 selected")
	}
	app.CommitPickerModal.SelectedIndex = 3 // sha1 (oldest)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	// Trying to Space-select a 3rd config (index 2 = sha2) must be rejected while 2 are selected.
	app.CommitPickerModal.SelectedIndex = 2
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	if app.CommitPickerModal.SelectionCount() != 2 || !strings.Contains(app.CommitPickerModal.Notice, "2 already selected") {
		t.Fatalf("expected strict max-2 rejection on 3rd Space, got count=%d notice=%q",
			app.CommitPickerModal.SelectionCount(), app.CommitPickerModal.Notice)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalNone {
		t.Fatalf("expected ModalCommitPicker to close on Enter with 2/2 selected")
	}
	if app.LeftCommitSHA != sha1 || app.RightCommitSHA != sha3 {
		t.Fatalf("expected chronological assignment Left=%s (older) Right=%s (newer), got Left=%s Right=%s",
			sha1, sha3, app.LeftCommitSHA, app.RightCommitSHA)
	}

	// 5. Click dual header [B ▾] chip to open CommitPicker (cursor starts on B = sha3),
	//    Space to uncheck sha3, move to index 0 (● RUNNING CONFIG), Space to check it, Enter -> VsLive=true!
	mainStartY := 2 + app.TimelineRibbon.OuterHeight()
	leftW, _, sepW := app.computeWorkspaceOuterWidths()
	detailStartX := leftW + sepW
	var pickRight HitChip
	for _, ch := range app.DiffView.SplitEndpointHitRanges() {
		if ch.Label == "pick_right" {
			pickRight = ch
			break
		}
	}
	if pickRight.Label != "pick_right" {
		t.Fatalf("expected pick_right hit range on active split header")
	}
	resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 1 + pickRight.StartX + 1, Y: mainStartY + 2, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalCommitPicker || app.CommitPickerModal.SelectedIndex != 1 {
		t.Fatalf("expected clicking [B ▾] to open ModalCommitPicker focused on B (idx 1 = sha3), got modal=%v idx=%d",
			app.ActiveModal, app.CommitPickerModal.SelectedIndex)
	}
	// Space unchecks sha3 (B), keeping sha1 checked.
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	app.CommitPickerModal.SelectedIndex = 0 // LiveRunningSentinel
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if !app.VsLive || app.LeftCommitSHA != sha1 || app.RightCommitSHA != "" {
		t.Fatalf("expected ● RUNNING CONFIG selection to set VsLive=true with Left=%s, got vsLive=%v left=%q right=%q",
			sha1, app.VsLive, app.LeftCommitSHA, app.RightCommitSHA)
	}
	if !strings.Contains(stripANSI(app.View()), "RUNNING CONFIG") {
		t.Fatalf("expected RUNNING CONFIG badge in split header")
	}

	// 6. Collapsible Left Sidebar rail ([◀] / ▶) works independently of Split Diff,
	//    and never strands focus on hidden PaneTimeline when collapsed!
	cStart, cEnd := app.TimelineView.CollapseHitRange()
	if cStart < 0 || cEnd <= cStart {
		t.Fatalf("expected valid [◀] collapse hit range, got [%d, %d)", cStart, cEnd)
	}
	// Focus Left Panel, then click [◀] -> collapses to 3-col ▶ rail, keeps Split Diff active, redirects focus to PaneDetail!
	app.setFocusedPane(PaneTimeline)
	resModel, _ = app.Update(tea.MouseMsg{X: cStart + 1, Y: mainStartY, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if !app.Zoomed || !app.hideTimelineList() || !app.SplitDiff || app.FocusedPane != PaneDetail {
		t.Fatalf("expected clicking [◀] to collapse left panel into ▶ rail while keeping SplitDiff and redirecting focus, got zoomed=%v split=%v focus=%v",
			app.Zoomed, app.SplitDiff, app.FocusedPane)
	}
	collapsedOut := app.View()
	assertNoUnrecoveredReset(t, "split-collapsed-rail-136x36", collapsedOut, 136, 36)
	if !strings.Contains(stripANSI(collapsedOut), "▶") {
		t.Fatalf("expected collapsed ▶ sidebar rail to be rendered:\n%s", stripANSI(collapsedOut))
	}
	// '/' then Esc while collapsed must return focus to PaneDetail (never stranded on hidden PaneTimeline).
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneDetail {
		t.Fatalf("expected Esc from FilterBar while collapsed to return focus to PaneDetail, got %v", app.FocusedPane)
	}
	// Click anywhere on the collapsed ▶ rail (X=1) -> expands Left Panel while keeping Split Diff active!
	resModel, _ = app.Update(tea.MouseMsg{X: 1, Y: mainStartY + 3, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.Zoomed || app.hideTimelineList() || !app.SplitDiff {
		t.Fatalf("expected clicking ▶ rail to reopen Left Panel while preserving SplitDiff, got zoomed=%v split=%v", app.Zoomed, app.SplitDiff)
	}

	// 7. Plain-clicking ANY commit in Left Commit List OR Visual Timeline Ribbon exits Split Diff back to single commit view!
	lpc := app.TimelineView.LinesPerCommit()
	if lpc < 1 {
		lpc = 2
	}
	// Plain click row 1 (sha2) in Left Commit List -> exits SplitDiff and shows sha2 in single-commit view!
	resModel, _ = app.Update(tea.MouseMsg{X: 8, Y: mainStartY + 1 + 1*lpc, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.SplitDiff || app.VsLive || app.selectedCommitSHA() != sha2 {
		t.Fatalf("expected plain click in Left Commit List to exit SplitDiff to sha2 (%s), got split=%v vsLive=%v sel=%s",
			sha2, app.SplitDiff, app.VsLive, app.selectedCommitSHA())
	}
	// Re-enter SplitDiff via 'S', then plain-click oldest commit dot on Visual Timeline Ribbon -> exits SplitDiff to sha1!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	if !app.SplitDiff {
		t.Fatalf("expected 'S' to re-enter SplitDiff")
	}
	resModel, _ = app.Update(tea.MouseMsg{X: 1, Y: 4, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.SplitDiff || app.selectedCommitSHA() != sha1 {
		t.Fatalf("expected plain click on Visual Timeline Ribbon dot to exit SplitDiff to sha1 (%s), got split=%v sel=%s",
			sha1, app.SplitDiff, app.selectedCommitSHA())
	}
	// Re-enter SplitDiff and verify resize 80x24 -> 136x36 preserves clean rendering & left panel visibility
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app = resModel.(AppModel)
	app.setFocusedPane(PaneTimeline)
	resModel, _ = app.Update(tea.WindowSizeMsg{Width: 136, Height: 36})
	app = resModel.(AppModel)
	if app.FocusedPane != PaneTimeline || !app.DiffView.IsSplitActive() || app.hideTimelineList() {
		t.Fatalf("expected resize back to >=100 cols in Split Diff to keep Left Panel open and focused, got focus=%v splitActive=%v hideTL=%v",
			app.FocusedPane, app.DiffView.IsSplitActive(), app.hideTimelineList())
	}

	// 8. Exact WxH & full-bleed SGR across 136x36, 120x35, 80x24, 40x12 (both open & collapsed ▶ rail)
	for _, collapsed := range []bool{false, true} {
		app.Zoomed = collapsed
		for _, sz := range []struct{ w, h int }{
			{136, 36},
			{120, 35},
			{80, 24},
			{40, 12},
		} {
			resModel, _ = app.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			app = resModel.(AppModel)
			assertNoUnrecoveredReset(t, fmt.Sprintf("split-collapsed=%v-%dx%d", collapsed, sz.w, sz.h), app.View(), sz.w, sz.h)
		}
	}
	app.Zoomed = false
}

func TestDecoupledDetailFilterScopeAndAutoRescope(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")
	defer func() { _, _ = SetTheme("dark") }()

	backend := gitbackend.NewGitBackend(filepath.Join(t.TempDir(), "repo"))
	t0 := time.Now().UTC().Add(-10 * time.Minute)

	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf-01"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable", "description": "uplink-1"},
		},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf-01"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable", "description": "uplink-core"},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65001,
						"afi-safi": []interface{}{
							map[string]interface{}{"afi-safi-name": "ipv4-unicast", "admin-state": "enable"},
							map[string]interface{}{"afi-safi-name": "ipv6-unicast", "admin-state": "disable"},
						},
					},
				},
			},
		},
	}
	cfg3 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf-02"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable", "description": "uplink-core"},
			map[string]interface{}{"name": "ethernet-1/2", "admin-state": "enable", "description": "edge-peer"},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65002,
						"afi-safi": []interface{}{
							map[string]interface{}{"afi-safi-name": "ipv4-unicast", "admin-state": "enable"},
							map[string]interface{}{"afi-safi-name": "ipv6-unicast", "admin-state": "enable"},
						},
					},
				},
			},
		},
	}

	_, sha1, err := backend.RecordConfigChange(cfg1, "alice", "1", "", t0, "Baseline", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha2, err := backend.RecordConfigChange(cfg2, "bob", "2", "", t0.Add(2*time.Minute), "Enable BGP", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha3, err := backend.RecordConfigChange(cfg3, "admin", "3", "", t0.Add(4*time.Minute), "Update BGP and hostname", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = sha1

	model := NewAppModel(backend, nil, "")
	resModel, _ := model.Update(tea.WindowSizeMsg{Width: 136, Height: 36})
	app := resModel.(AppModel)

	// 1. Unfiltered state: zero filter banner, no scope pill, '\' is a no-op
	if app.DiffView.FilterBannerRow() != -1 {
		t.Fatalf("expected no DiffView filter banner when unfiltered, got row=%d", app.DiffView.FilterBannerRow())
	}
	if app.DiffView.ShownChangeCount != 0 || app.DiffView.TotalUnfilteredChanges != 0 {
		t.Fatalf("expected 0 diff counts when unfiltered, got shown=%d total=%d",
			app.DiffView.ShownChangeCount, app.DiffView.TotalUnfilteredChanges)
	}
	if sX, eX := app.FilterBar.ScopeHitRange(); sX != -1 || eX != -1 {
		t.Fatalf("expected ScopeHitRange=(-1,-1) when unfiltered, got [%d,%d)", sX, eX)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	app = resModel.(AppModel)
	if app.DetailFilterBypass {
		t.Fatalf("expected '\\' to be a no-op when filter is empty")
	}

	// 2. Apply '/' filter "bgp": Timeline & Ribbon filter to 2/3 commits; Detail shows scoped subset banner
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	for _, r := range "bgp" {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = resModel.(AppModel)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)

	if len(app.TimelineView.Commits) != 2 || len(app.TimelineRibbon.WindowCommits) != 2 {
		t.Fatalf("expected Timeline and Ribbon filtered to 2 bgp commits, got tl=%d ribbon=%d",
			len(app.TimelineView.Commits), len(app.TimelineRibbon.WindowCommits))
	}
	if app.DetailFilterBypass {
		t.Fatalf("expected DetailFilterBypass=false initially after applying filter")
	}
	if app.DiffView.FilterBannerRow() < 0 {
		t.Fatalf("expected DiffView filter banner row >= 0 when filter active")
	}
	if app.DiffView.ShownChangeCount <= 0 || app.DiffView.TotalUnfilteredChanges <= app.DiffView.ShownChangeCount {
		t.Fatalf("expected 0 < ShownChangeCount < TotalUnfilteredChanges for scoped bgp diff on sha3, got shown=%d total=%d",
			app.DiffView.ShownChangeCount, app.DiffView.TotalUnfilteredChanges)
	}
	scopedShown := app.DiffView.ShownChangeCount
	scopedTotal := app.DiffView.TotalUnfilteredChanges

	scopedScreen := stripANSI(app.View())
	if !strings.Contains(scopedScreen, "Scope: TL+Detail") {
		t.Fatalf("expected FilterBar 'Scope: TL+Detail' pill when scoped:\n%s", scopedScreen)
	}
	if !strings.Contains(scopedScreen, "FILTERED") {
		t.Fatalf("expected Detail 'FILTERED' banner when scoped:\n%s", scopedScreen)
	}
	if !strings.Contains(scopedScreen, "\\ Full Commit") {
		t.Fatalf("expected Footer '\\ Full Commit' shortcut when scoped:\n%s", scopedScreen)
	}
	if strings.Contains(stripANSI(app.DiffView.Viewport.View()), "leaf-02") {
		t.Fatalf("scoped bgp diff should hide non-bgp change 'leaf-02'")
	}

	// Verify Config and Blame scoped banners and Blame row click alignment via HeaderRows()
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	if app.ConfigView.FilterBannerRow() < 0 || app.ConfigView.ShownLineCount <= 0 ||
		app.ConfigView.TotalUnfilteredLines <= app.ConfigView.ShownLineCount {
		t.Fatalf("expected scoped ConfigView banner with 0 < shown < total, got row=%d shown=%d total=%d",
			app.ConfigView.FilterBannerRow(), app.ConfigView.ShownLineCount, app.ConfigView.TotalUnfilteredLines)
	}

	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	if app.BlameView.FilterBannerRow() < 0 || app.BlameView.ShownEntryCount <= 0 ||
		app.BlameView.TotalUnfilteredEntries <= app.BlameView.ShownEntryCount {
		t.Fatalf("expected scoped BlameView banner with 0 < shown < total, got row=%d shown=%d total=%d",
			app.BlameView.FilterBannerRow(), app.BlameView.ShownEntryCount, app.BlameView.TotalUnfilteredEntries)
	}
	// Click second Blame attribution row (row 1) below header rows to confirm HeaderRows() alignment
	if len(app.BlameView.Entries) >= 2 {
		tlOuterW, _ := computeSplitOuterWidths(app.Width)
		detailStartX := tlOuterW + 1
		mainStartY := app.mainStartY()
		blameClickY := mainStartY + 1 + app.BlameView.HeaderRows() + 1
		resModel, _ = app.Update(tea.MouseMsg{X: detailStartX + 10, Y: blameClickY, Type: tea.MouseLeft})
		app = resModel.(AppModel)
		if app.BlameView.SelectedLine != 1 {
			t.Fatalf("expected Blame row click below filter banner to select entry 1, got %d", app.BlameView.SelectedLine)
		}
	}

	// Return to Diff tab
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	app = resModel.(AppModel)

	// 3. Toggle via '\' key -> Full Commit while Timeline & Ribbon stay filtered + cyan ▸ match markers
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass || !app.FilterBar.DetailBypass {
		t.Fatalf("expected DetailFilterBypass=true after '\\'")
	}
	if len(app.TimelineView.Commits) != 2 || len(app.TimelineRibbon.WindowCommits) != 2 {
		t.Fatalf("expected Timeline and Ribbon to remain filtered (2 commits) when DetailFilterBypass=true, got tl=%d ribbon=%d",
			len(app.TimelineView.Commits), len(app.TimelineRibbon.WindowCommits))
	}
	if app.DiffView.ShownChangeCount != scopedTotal || app.DiffView.TotalUnfilteredChanges != scopedTotal {
		t.Fatalf("expected Full Diff counts %d/%d, got %d/%d",
			scopedTotal, scopedTotal, app.DiffView.ShownChangeCount, app.DiffView.TotalUnfilteredChanges)
	}
	fullScreen := stripANSI(app.View())
	if !strings.Contains(fullScreen, "Scope: TL only") {
		t.Fatalf("expected FilterBar 'Scope: TL only' pill when bypassed:\n%s", fullScreen)
	}
	if !strings.Contains(fullScreen, "FULL") {
		t.Fatalf("expected Detail 'FULL' banner when bypassed:\n%s", fullScreen)
	}
	if !strings.Contains(fullScreen, "\\ Scope Filter") {
		t.Fatalf("expected Footer '\\ Scope Filter' shortcut when bypassed:\n%s", fullScreen)
	}
	fullDiffPlain := stripANSI(app.DiffView.Viewport.View())
	if !strings.Contains(fullDiffPlain, "leaf-02") || !strings.Contains(fullDiffPlain, "ethernet-1/2") {
		t.Fatalf("expected Full Commit diff to reveal non-bgp changes 'leaf-02' and 'ethernet-1/2', got:\n%s", fullDiffPlain)
	}
	if !strings.Contains(fullDiffPlain, GlyphCollapsed) {
		t.Fatalf("expected cyan '%s' matching-line gutter marker in Full Commit diff, got:\n%s", GlyphCollapsed, fullDiffPlain)
	}

	// 4. Toggle via FilterBar Scope pill click (must NOT focus PaneFilter)
	scopeStart, scopeEnd := app.FilterBar.ScopeHitRange()
	if scopeStart < 0 || scopeEnd <= scopeStart {
		t.Fatalf("expected valid FilterBar ScopeHitRange, got [%d,%d)", scopeStart, scopeEnd)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: scopeStart + 1, Y: 1, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DetailFilterBypass || app.FocusedPane == PaneFilter {
		t.Fatalf("expected FilterBar Scope pill click to toggle bypass=false without focusing filter, got bypass=%v pane=%v",
			app.DetailFilterBypass, app.FocusedPane)
	}
	if app.DiffView.ShownChangeCount != scopedShown {
		t.Fatalf("expected re-scoped Diff ShownChangeCount=%d, got %d", scopedShown, app.DiffView.ShownChangeCount)
	}

	// 5. Toggle via Detail banner pill click
	tlOuterW, _ := computeSplitOuterWidths(app.Width)
	detailStartX := tlOuterW + 1
	mainStartY := app.mainStartY()
	bannerRow := app.DiffView.FilterBannerRow()
	bStart, bEnd := app.DiffView.FilterBannerHitRange()
	if bannerRow < 0 || bStart < 0 || bEnd <= bStart {
		t.Fatalf("expected valid Detail FilterBannerRow/HitRange, got row=%d [%d,%d)", bannerRow, bStart, bEnd)
	}
	resModel, _ = app.Update(tea.MouseMsg{
		X:    detailStartX + 1 + bStart + 1,
		Y:    mainStartY + 1 + bannerRow,
		Type: tea.MouseLeft,
	})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass {
		t.Fatalf("expected Detail banner pill click to toggle DetailFilterBypass=true")
	}

	// 6. Preserve DetailFilterBypass across formats (1/2/3), Split (S), Fold (E), Zoom (z), and tabs (c/b/d)
	for _, key := range []rune{'1', '2', '3'} {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		app = resModel.(AppModel)
		if !app.DetailFilterBypass {
			t.Fatalf("expected format key %q to preserve DetailFilterBypass=true", string(key))
		}
	}
	// Split Diff ('S') preserves DetailFilterBypass and keeps consistent counts + cyan ▸ + clickable split banner
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	if !app.DiffView.IsSplitActive() || !app.DetailFilterBypass {
		t.Fatalf("expected 'S' to activate Split Diff preserving DetailFilterBypass=true, got split=%v bypass=%v",
			app.DiffView.IsSplitActive(), app.DetailFilterBypass)
	}
	if app.DiffView.ShownChangeCount != scopedTotal || app.DiffView.TotalUnfilteredChanges != scopedTotal {
		t.Fatalf("expected consistent Split Diff counts %d/%d, got %d/%d",
			scopedTotal, scopedTotal, app.DiffView.ShownChangeCount, app.DiffView.TotalUnfilteredChanges)
	}
	if !strings.Contains(stripANSI(app.DiffView.Viewport.View()), GlyphCollapsed) {
		t.Fatalf("expected cyan '%s' matching-line marker in Split Diff Full Commit mode", GlyphCollapsed)
	}
	// Toggle Fold ('E') preserves DetailFilterBypass
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass {
		t.Fatalf("expected 'E' fold toggle to preserve DetailFilterBypass=true")
	}
	// Click banner pill in Split Diff (using live workspace detailStartX, bannerRow == 3) twice -> false then true
	splitLeftW, _, splitSepW := app.computeWorkspaceOuterWidths()
	splitDetailStartX := splitLeftW + splitSepW
	splitBannerRow := app.DiffView.FilterBannerRow()
	sbStart, sbEnd := app.DiffView.FilterBannerHitRange()
	if splitBannerRow != 3 || sbStart < 0 || sbEnd <= sbStart {
		t.Fatalf("expected Split Diff FilterBannerRow=3 and valid hit range, got row=%d [%d,%d)", splitBannerRow, sbStart, sbEnd)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: splitDetailStartX + 1 + sbStart + 1, Y: app.mainStartY() + 1 + splitBannerRow, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DetailFilterBypass || app.DiffView.ShownChangeCount != scopedShown {
		t.Fatalf("expected Split Diff banner click to re-scope (bypass=false, shown=%d), got bypass=%v shown=%d",
			scopedShown, app.DetailFilterBypass, app.DiffView.ShownChangeCount)
	}
	sbStart, _ = app.DiffView.FilterBannerHitRange()
	resModel, _ = app.Update(tea.MouseMsg{X: splitDetailStartX + 1 + sbStart + 1, Y: app.mainStartY() + 1 + splitBannerRow, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass {
		t.Fatalf("expected second Split Diff banner click to re-enable Full Commit")
	}
	// Exit Split ('S') preserves DetailFilterBypass
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	if app.DiffView.IsSplitActive() || !app.DetailFilterBypass {
		t.Fatalf("expected exiting Split Diff to preserve DetailFilterBypass=true")
	}
	// Zoom ('z' / 'z') preserves DetailFilterBypass
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	app = resModel.(AppModel)
	if !app.Zoomed || !app.DetailFilterBypass {
		t.Fatalf("expected Zoom ON to preserve DetailFilterBypass=true")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	app = resModel.(AppModel)
	if app.Zoomed || !app.DetailFilterBypass {
		t.Fatalf("expected Zoom OFF to preserve DetailFilterBypass=true")
	}
	// Tabs ('c' -> Config full + cyan ▸, 'b' -> Blame full + cyan ▸, 'd' -> Diff) preserve DetailFilterBypass
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass || app.ConfigView.ShownLineCount != app.ConfigView.TotalUnfilteredLines ||
		!strings.Contains(stripANSI(app.ConfigView.Viewport.View()), GlyphCollapsed) {
		t.Fatalf("expected Config tab to preserve DetailFilterBypass=true with full lines and cyan '%s'", GlyphCollapsed)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass || app.BlameView.ShownEntryCount != app.BlameView.TotalUnfilteredEntries ||
		!strings.Contains(stripANSI(app.BlameView.Viewport.View()), GlyphCollapsed) {
		t.Fatalf("expected Blame tab to preserve DetailFilterBypass=true with full entries and cyan '%s'", GlyphCollapsed)
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass {
		t.Fatalf("expected returning to Diff tab to preserve DetailFilterBypass=true")
	}

	// 7. Auto-rescope (DetailFilterBypass -> false) on configuration or filter changes
	// 7a. Selecting a different commit in Timeline ('j')
	app.setFocusedPane(PaneTimeline)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	app = resModel.(AppModel)
	if app.selectedCommitSHA() != sha2 {
		t.Fatalf("expected 'j' to select sha2 (%s), got %s", sha2, app.selectedCommitSHA())
	}
	if app.DetailFilterBypass {
		t.Fatalf("expected commit change to auto-rescope DetailFilterBypass=false")
	}

	// 7b. Pinning Base commit ('m')
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass {
		t.Fatalf("expected '\\' to enable DetailFilterBypass")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	app = resModel.(AppModel)
	if app.DetailFilterBypass {
		t.Fatalf("expected 'm' base toggle to auto-rescope DetailFilterBypass=false")
	}
	// Unpin base
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	app = resModel.(AppModel)

	// 7c. Toggling vs Running Config ('4')
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	app = resModel.(AppModel)
	if app.DetailFilterBypass {
		t.Fatalf("expected '4' live toggle to auto-rescope DetailFilterBypass=false")
	}
	// Exit VsLive
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	app = resModel.(AppModel)

	// 7d. Changing A/B endpoints via unified 2-config CommitPickerModal ('A')
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app = resModel.(AppModel)
	if app.ActiveModal != ModalCommitPicker {
		t.Fatalf("expected ModalCommitPicker after 'A'")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	app.CommitPickerModal.SelectedIndex = 1 // newest filtered commit (sha3)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	app.CommitPickerModal.SelectedIndex = 2 // older filtered commit
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = resModel.(AppModel)
	if app.DetailFilterBypass {
		t.Fatalf("expected CommitPickerModal selection to auto-rescope DetailFilterBypass=false")
	}
	// Return to single-pane on sha3
	if app.SplitDiff {
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
		app = resModel.(AppModel)
	}
	app.selectCommitBySHA(sha3)
	app.updateActiveTabContent()

	// 7e. Changing filter query via '/'
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	app = resModel.(AppModel)
	if !app.DetailFilterBypass {
		t.Fatalf("expected DetailFilterBypass=true before filter edit")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	app = resModel.(AppModel)
	if app.DetailFilterBypass {
		t.Fatalf("expected editing filter query to auto-rescope DetailFilterBypass=false")
	}
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = resModel.(AppModel)

	// 7f. Whitespace-only filter does not show Scope pill
	app.FilterBar.TextInput.SetValue("   ")
	if sX, eX := app.FilterBar.ScopeHitRange(); sX != -1 || eX != -1 {
		t.Fatalf("expected whitespace-only filter to hide Scope pill, got [%d,%d)", sX, eX)
	}

	// 7g. Scalar value filter ("leaf-02" / "65002") yields identical ShownChangeCount in Single-Pane and Split Diff
	for _, valQuery := range []string{"leaf-02", "65002"} {
		app.FilterBar.TextInput.SetValue(valQuery)
		app.setDetailFilterBypass(false)
		app.SplitDiff = false
		app.LeftCommitSHA = ""
		app.RightCommitSHA = ""
		app.leftCommit = nil
		app.rightCommit = nil
		app.ActiveTab = TabDiff
		app.refreshTimeline()
		singleShown := app.DiffView.ShownChangeCount
		singleTotal := app.DiffView.TotalUnfilteredChanges
		if singleShown != 1 || singleTotal <= singleShown {
			t.Fatalf("scalar value filter %q in Single-Pane Diff expected shown=1 and total>1, got shown=%d total=%d",
				valQuery, singleShown, singleTotal)
		}
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
		app = resModel.(AppModel)
		if app.DiffView.ShownChangeCount != singleShown || app.DiffView.TotalUnfilteredChanges != singleTotal {
			t.Fatalf("scalar value filter %q Split Diff counts (%d/%d) mismatch Single-Pane (%d/%d)",
				valQuery, app.DiffView.ShownChangeCount, app.DiffView.TotalUnfilteredChanges, singleShown, singleTotal)
		}
		resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
		app = resModel.(AppModel)
	}

	// 8. Exact WxH & full-bleed SGR invariants across 136x36, 120x35, 80x24, 40x12 (Scoped & Full)
	app.FilterBar.TextInput.SetValue("bgp")
	app.refreshTimeline()
	for _, bypass := range []bool{false, true} {
		app.setDetailFilterBypass(bypass)
		app.updateActiveTabContent()
		for _, sz := range []struct{ w, h int }{
			{136, 36},
			{120, 35},
			{80, 24},
			{40, 12},
		} {
			resModel, _ = app.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			app = resModel.(AppModel)
			assertNoUnrecoveredReset(t, fmt.Sprintf("decoupled-filter-bypass=%v-%dx%d", bypass, sz.w, sz.h), app.View(), sz.w, sz.h)
		}
	}
}

func TestAppTimelineRibbonSGRBrushDragAndDoubleClickZoom(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	repoDir := t.TempDir()
	backend := gitbackend.NewGitBackend(repoDir)

	now := time.Now().UTC().Truncate(time.Second)
	// Record 4 commits spread across a 12h gap with a tight recent cluster.
	type seedCommit struct {
		host string
		ts   time.Time
	}
	seeds := []seedCommit{
		{"leaf-01", now.Add(-12 * time.Hour)},
		{"leaf-02", now.Add(-6 * time.Minute)},
		{"leaf-03", now.Add(-3 * time.Minute)},
		{"leaf-04", now},
	}
	for i, s := range seeds {
		cfg := map[string]interface{}{
			"system": map[string]interface{}{"name": map[string]interface{}{"host-name": s.host}},
		}
		if _, _, err := backend.RecordConfigChange(cfg, "admin", fmt.Sprintf("%d", i+1), "", s.ts, fmt.Sprintf("commit %d", i+1), nil); err != nil {
			t.Fatalf("RecordConfigChange %d: %v", i, err)
		}
	}

	app := NewAppModel(backend, nil, "")
	resModel, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	app = resModel.(AppModel)
	if len(app.allCommits) != 4 {
		t.Fatalf("expected 4 commits, got %d", len(app.allCommits))
	}

	clk := now
	app.TimelineRibbon.nowFunc = func() time.Time { return clk }
	origSpan := app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart)

	// 1. SGR Bubble Tea v0.25 click-and-drag brush on timeline plot row (Y=3 -> relY=1):
	//    Note: in Bubble Tea v0.25 SGR mode, drag motion carries Action=MouseActionMotion AND Type=MouseLeft!
	resModel, _ = app.Update(tea.MouseMsg{X: 70, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.MouseMsg{X: 118, Y: 3, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.TimelineRibbon.DragMode != "brush" || !app.TimelineRibbon.brushDragged {
		t.Fatalf("expected active SGR brush drag, got mode=%q dragged=%v", app.TimelineRibbon.DragMode, app.TimelineRibbon.brushDragged)
	}
	assertNoUnrecoveredReset(t, "app-sgr-brush-live", app.View(), 120, 35)
	resModel, _ = app.Update(tea.MouseMsg{X: 118, Y: 3, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)
	if app.TimelineRibbon.DragMode != "" {
		t.Fatalf("expected DragMode cleared on release, got %q", app.TimelineRibbon.DragMode)
	}
	if app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart) >= origSpan {
		t.Fatalf("expected SGR brush drag to narrow window")
	}
	if len(app.TimelineView.Commits) != 3 {
		t.Fatalf("expected TimelineView synced to 3 recent commits after brush drag, got %d", len(app.TimelineView.Commits))
	}

	// 2. SGR overview scrubber handle drag (Action=MouseActionMotion, Type=MouseLeft) moves left edge cleanly.
	app.TimelineRibbon.ResetWindow()
	app.syncWindowCommits()
	layout := app.TimelineRibbon.computeLayout()
	resModel, _ = app.Update(tea.MouseMsg{X: layout.leftHandleX, Y: 6, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.MouseMsg{X: layout.leftHandleX + 20, Y: 6, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.MouseMsg{X: layout.leftHandleX + 20, Y: 6, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)
	if !app.TimelineRibbon.WindowStart.After(app.TimelineRibbon.FullMinTime) {
		t.Fatalf("expected SGR overview left-handle drag to move WindowStart right")
	}

	// 3. Double-click on plot row progressively narrows the window around the clicked cluster!
	app.TimelineRibbon.ResetWindow()
	app.syncWindowCommits()
	spanBefore := app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart)
	clk = clk.Add(1 * time.Second)
	resModel, _ = app.Update(tea.MouseMsg{X: 117, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.MouseMsg{X: 117, Y: 4, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)
	clk = clk.Add(150 * time.Millisecond)
	resModel, _ = app.Update(tea.MouseMsg{X: 117, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.MouseMsg{X: 117, Y: 4, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)

	spanAfter := app.TimelineRibbon.WindowEnd.Sub(app.TimelineRibbon.WindowStart)
	if spanAfter >= spanBefore {
		t.Fatalf("expected double-click on ribbon to narrow window: before=%v after=%v", spanBefore, spanAfter)
	}
	if len(app.TimelineView.Commits) != 3 {
		t.Fatalf("expected double-click zoom on recent cluster to keep 3 recent commits in TimelineView, got %d", len(app.TimelineView.Commits))
	}

	// 4. Shift+Click in the Left Commit List & Visual Timeline Ribbon automatically opens Split Diff
	//    between the currently selected commit and the Shift+Clicked commit (older->A, newer->B)!
	app.TimelineRibbon.ResetWindow()
	app.syncWindowCommits()
	// allCommits order (newest-first): [0]=leaf-04 (newest), [1]=leaf-03, [2]=leaf-02, [3]=leaf-01 (oldest)
	cNewest := app.TimelineView.Commits[0].FullSHA
	cMidNew := app.TimelineView.Commits[1].FullSHA
	cMidOld := app.TimelineView.Commits[2].FullSHA
	cOldest := app.TimelineView.Commits[3].FullSHA

	// 4a. Select older commit (idx 2 = cMidOld), then Shift+Click newer commit (idx 0 = cNewest) in Left Commit List.
	app.TimelineView.SelectIndex(2)
	app.syncCompareMarkers()
	app.updateActiveTabContent()
	mainStartY := app.mainStartY()
	lpc := app.TimelineView.LinesPerCommit()
	if lpc < 1 {
		lpc = 2
	}
	clickYRow0 := mainStartY + 1 + 0*lpc
	resModel, _ = app.Update(tea.MouseMsg{X: 8, Y: clickYRow0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft, Shift: true})
	app = resModel.(AppModel)
	if !app.SplitDiff || app.ActiveTab != TabDiff || app.FocusedPane != PaneDetail {
		t.Fatalf("expected Shift+Click in Left Commit List to enter SplitDiff on TabDiff/PaneDetail, got split=%v tab=%v focus=%v",
			app.SplitDiff, app.ActiveTab, app.FocusedPane)
	}
	if app.LeftCommitSHA != cMidOld || app.RightCommitSHA != cNewest {
		t.Fatalf("expected Shift+Click (selected older -> shift-click newer) A=%s B=%s, got A=%s B=%s",
			cMidOld, cNewest, app.LeftCommitSHA, app.RightCommitSHA)
	}

	// 4b. Exit split view ('S'), select newer commit (idx 1 = cMidNew), then Shift+Click oldest (idx 3 = cOldest):
	//     must compare cOldest (A) <-> cMidNew (B), NOT stale cNewest!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	app.TimelineView.SelectIndex(1)
	app.syncCompareMarkers()
	app.updateActiveTabContent()
	clickYRow3 := mainStartY + 1 + 3*lpc
	resModel, _ = app.Update(tea.MouseMsg{X: 8, Y: clickYRow3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft, Shift: true})
	app = resModel.(AppModel)
	if !app.SplitDiff || app.LeftCommitSHA != cOldest || app.RightCommitSHA != cMidNew {
		t.Fatalf("expected Shift+Click (selected newer -> shift-click older) A=%s B=%s, got split=%v A=%s B=%s",
			cOldest, cMidNew, app.SplitDiff, app.LeftCommitSHA, app.RightCommitSHA)
	}

	// 4c. Shift+Clicking the SAME selected commit does NOT enter empty A==B split view.
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	app.TimelineView.SelectIndex(1)
	clickYRow1 := mainStartY + 1 + 1*lpc
	resModel, _ = app.Update(tea.MouseMsg{X: 8, Y: clickYRow1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft, Shift: true})
	app = resModel.(AppModel)
	if app.SplitDiff {
		t.Fatalf("expected Shift+Click on already-selected commit NOT to open empty A==B split diff")
	}

	// 4d. Visual Timeline Ribbon: select oldest commit (idx 3 = cOldest), Shift+Click newest dot on right edge of ribbon!
	app.TimelineView.SelectIndex(3)
	app.syncCompareMarkers()
	app.updateActiveTabContent()
	resModel, _ = app.Update(tea.MouseMsg{X: 118, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft, Shift: true})
	app = resModel.(AppModel)
	if !app.SplitDiff || app.LeftCommitSHA != cOldest || app.RightCommitSHA == cOldest || app.RightCommitSHA == "" {
		t.Fatalf("expected Shift+Click on right ribbon cluster while oldest selected to open SplitDiff A=%s B=<recent>, got split=%v A=%s B=%s",
			cOldest, app.SplitDiff, app.LeftCommitSHA, app.RightCommitSHA)
	}

	// 4e. Ctrl+Click, Alt+Click, and Right-Click aliases work identically in Left Commit List & Ribbon!
	for _, tc := range []struct {
		name string
		msg  tea.MouseMsg
	}{
		{
			name: "ctrl+click-list",
			msg:  tea.MouseMsg{X: 8, Y: clickYRow0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft, Ctrl: true},
		},
		{
			name: "alt+click-list",
			msg:  tea.MouseMsg{X: 8, Y: clickYRow0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft, Alt: true},
		},
		{
			name: "right-click-list",
			msg:  tea.MouseMsg{X: 8, Y: clickYRow0, Action: tea.MouseActionPress, Button: tea.MouseButtonRight, Type: tea.MouseRight},
		},
	} {
		// Exit split view, select cMidOld (idx 2), then trigger compare alias on row 0 (cNewest).
		if app.SplitDiff {
			resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
			app = resModel.(AppModel)
		}
		app.TimelineView.SelectIndex(2)
		app.syncCompareMarkers()
		app.updateActiveTabContent()

		resModel, _ = app.Update(tc.msg)
		app = resModel.(AppModel)
		if !app.SplitDiff || app.LeftCommitSHA != cMidOld || app.RightCommitSHA != cNewest {
			t.Fatalf("%s: expected SplitDiff A=%s B=%s, got split=%v A=%s B=%s",
				tc.name, cMidOld, cNewest, app.SplitDiff, app.LeftCommitSHA, app.RightCommitSHA)
		}
	}

	// 4f. Ctrl+Click on Visual Timeline Ribbon dot works identically.
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	app.TimelineView.SelectIndex(3) // cOldest
	app.syncCompareMarkers()
	app.updateActiveTabContent()
	resModel, _ = app.Update(tea.MouseMsg{X: 118, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft, Ctrl: true})
	app = resModel.(AppModel)
	if !app.SplitDiff || app.LeftCommitSHA != cOldest || app.RightCommitSHA == cOldest || app.RightCommitSHA == "" {
		t.Fatalf("ctrl+click-ribbon: expected SplitDiff A=%s B=<recent>, got split=%v A=%s B=%s",
			cOldest, app.SplitDiff, app.LeftCommitSHA, app.RightCommitSHA)
	}

	// 5. Kernel inotify HEAD watcher delivers live commits to AppModel in sub-second time (far faster than 5s fallback).
	app.headWatch.start(backend.RepoDir, backend, app.headEventChan)
	defer app.headWatch.stop()

	newCfg := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "leaf-live-05"}},
	}
	t0 := time.Now()
	_, newSHA, err := backend.RecordConfigChange(newCfg, "admin", "5", "", time.Now().UTC(), "live instant commit", nil)
	if err != nil || newSHA == "" {
		t.Fatalf("RecordConfigChange live commit failed: sha=%q err=%v", newSHA, err)
	}
	select {
	case gotSHA := <-app.headEventChan:
		if gotSHA != newSHA {
			t.Fatalf("expected headWatch event SHA %q, got %q", newSHA, gotSHA)
		}
		if time.Since(t0) > 500*time.Millisecond {
			t.Fatalf("expected instant (<500ms) inotify HEAD event, took %v", time.Since(t0))
		}
		// Feeding headWatchMsg into Update refreshes TimelineView immediately with the new commit!
		resModel, _ = app.Update(headWatchMsg{sha: gotSHA})
		app = resModel.(AppModel)
		if app.lastHeadSHA != newSHA || len(app.allCommits) != 5 {
			t.Fatalf("expected headWatchMsg to refresh AppModel to 5 commits with head=%s, got head=%s count=%d",
				newSHA, app.lastHeadSHA, len(app.allCommits))
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for inotify HEAD watch event")
	}
}

func TestScrollbarClickJumpAndSGRDragAcrossConfigDiffBlameAndTimeline(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	repoDir := t.TempDir()
	backend := gitbackend.NewGitBackend(repoDir)
	now := time.Now().UTC().Truncate(time.Second)

	// Build two large configs (~80 interfaces -> >160 CLI lines) + 20 commits so every pane has a live scrollbar!
	makeLargeCfg := func(prefix string, count int) map[string]interface{} {
		ifaces := make([]interface{}, 0, count)
		for i := 1; i <= count; i++ {
			ifaces = append(ifaces, map[string]interface{}{
				"name":        fmt.Sprintf("ethernet-1/%d", i),
				"admin-state": "enable",
				"description": fmt.Sprintf("%s-port-%02d", prefix, i),
			})
		}
		return map[string]interface{}{
			"interface": ifaces,
			"system":    map[string]interface{}{"name": map[string]interface{}{"host-name": prefix}},
		}
	}

	for i := 0; i < 18; i++ {
		cfg := makeLargeCfg(fmt.Sprintf("leaf-%02d", i+1), 45)
		if _, _, err := backend.RecordConfigChange(cfg, "admin", fmt.Sprintf("%d", i+1), "", now.Add(-time.Duration(20-i)*time.Minute), fmt.Sprintf("bulk commit %d", i+1), nil); err != nil {
			t.Fatalf("RecordConfigChange %d: %v", i, err)
		}
	}

	app := NewAppModel(backend, nil, "")
	resModel, _ := app.Update(tea.WindowSizeMsg{Width: 136, Height: 30})
	app = resModel.(AppModel)

	mainStartY := app.mainStartY()
	leftOuterW, detailOuterW, sepW := app.computeWorkspaceOuterWidths()
	detailStartX := leftOuterW + sepW
	detailSbX := detailStartX + detailOuterW - 2
	timelineSbX := leftOuterW - 2

	// 1. DiffView (Single-Pane): click bottom of scrollbar track -> jumps to maxOffset, then click top -> 0!
	vp, totalLines, trackStartY, trackH, _ := app.scrollbarTargetMetrics("detail", mainStartY)
	if totalLines <= trackH {
		t.Fatalf("expected scrollable DiffView, got totalLines=%d trackH=%d", totalLines, trackH)
	}
	maxOff := totalLines - trackH
	// Click bottom cell of scrollbar track
	resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: trackStartY + trackH - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.Viewport.YOffset != maxOff {
		t.Fatalf("DiffView click bottom of scrollbar: expected YOffset=%d, got %d", maxOff, app.DiffView.Viewport.YOffset)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: trackStartY + trackH - 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)
	if app.scrollDragTarget != "" {
		t.Fatalf("expected scrollDragTarget cleared on release, got %q", app.scrollDragTarget)
	}
	// Click middle of scrollbar track -> jumps near midpoint!
	midY := trackStartY + trackH/2
	resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: midY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.Viewport.YOffset <= 0 || app.DiffView.Viewport.YOffset >= maxOff {
		t.Fatalf("DiffView click mid scrollbar: expected 0 < YOffset < %d, got %d", maxOff, app.DiffView.Viewport.YOffset)
	}
	// Drag thumb back to top while cursor drifts far left (X=20) -> must keep scrubbing smoothly!
	resModel, _ = app.Update(tea.MouseMsg{X: 20, Y: trackStartY, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.DiffView.Viewport.YOffset != 0 {
		t.Fatalf("DiffView drag scrollbar to top with horizontal drift: expected YOffset=0, got %d", app.DiffView.Viewport.YOffset)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: 20, Y: trackStartY, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)

	// 2. ConfigView: grab thumb at top (zero jump on initial press), SGR drag down -> advances YOffset!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = resModel.(AppModel)
	vp, totalLines, trackStartY, trackH, _ = app.scrollbarTargetMetrics("detail", mainStartY)
	if totalLines <= trackH || vp.YOffset != 0 {
		t.Fatalf("expected scrollable ConfigView at YOffset=0, got total=%d trackH=%d yOff=%d", totalLines, trackH, vp.YOffset)
	}
	// Grab on thumb top cell
	resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: trackStartY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ConfigView.Viewport.YOffset != 0 || app.scrollDragTarget != "detail" {
		t.Fatalf("ConfigView grab thumb should preserve YOffset=0 and arm drag, got yOff=%d target=%q", app.ConfigView.Viewport.YOffset, app.scrollDragTarget)
	}
	// Drag 6 rows downward
	resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: trackStartY + 6, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.ConfigView.Viewport.YOffset <= 0 {
		t.Fatalf("ConfigView drag scrollbar down should advance YOffset > 0, got %d", app.ConfigView.Viewport.YOffset)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: trackStartY + 6, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)
	assertNoUnrecoveredReset(t, "config-scrollbar-dragged", app.View(), 136, 30)

	// 3. Split-Pane Diff: click bottom of scrollbar track -> jumps cleanly!
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	app = resModel.(AppModel)
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)
	leftOuterW, detailOuterW, sepW = app.computeWorkspaceOuterWidths()
	detailSbX = leftOuterW + sepW + detailOuterW - 2
	_, totalLines, trackStartY, trackH, _ = app.scrollbarTargetMetrics("detail", mainStartY)
	if totalLines > trackH {
		resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: trackStartY + trackH - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
		app = resModel.(AppModel)
		if app.DiffView.Viewport.YOffset != totalLines-trackH {
			t.Fatalf("Split Diff click bottom scrollbar: expected %d, got %d", totalLines-trackH, app.DiffView.Viewport.YOffset)
		}
		resModel, _ = app.Update(tea.MouseMsg{X: detailSbX, Y: trackStartY + trackH - 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
		app = resModel.(AppModel)
	}
	// Exit split diff for TimelineView scrollbar check
	resModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	app = resModel.(AppModel)

	// 4. TimelineView (Left Panel): clicking scrollbar scrolls viewport WITHOUT changing selected commit!
	leftOuterW, _, _ = app.computeWorkspaceOuterWidths()
	timelineSbX = leftOuterW - 2
	selBefore := app.selectedCommitSHA()
	_, tlTotal, tlStartY, tlH, _ := app.scrollbarTargetMetrics("timeline", mainStartY)
	if tlTotal <= tlH {
		t.Fatalf("expected scrollable TimelineView with 18 commits, got total=%d trackH=%d", tlTotal, tlH)
	}
	resModel, _ = app.Update(tea.MouseMsg{X: timelineSbX, Y: tlStartY + tlH - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Type: tea.MouseLeft})
	app = resModel.(AppModel)
	if app.TimelineView.Viewport.YOffset != tlTotal-tlH {
		t.Fatalf("TimelineView click bottom scrollbar: expected YOffset=%d, got %d", tlTotal-tlH, app.TimelineView.Viewport.YOffset)
	}
	if app.selectedCommitSHA() != selBefore {
		t.Fatalf("TimelineView scrollbar click must NOT change selected commit: before=%s after=%s", selBefore, app.selectedCommitSHA())
	}
	resModel, _ = app.Update(tea.MouseMsg{X: timelineSbX, Y: tlStartY + tlH - 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Type: tea.MouseRelease})
	app = resModel.(AppModel)
	assertNoUnrecoveredReset(t, "timeline-scrollbar-dragged", app.View(), 136, 30)
}
