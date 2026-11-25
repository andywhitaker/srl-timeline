package modals

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"

	"timeline/pkg/models"
)

// assertModalFullBleed verifies exact width/height, SGR framing, zero unrecovered resets,
// and no wide emoji codepoints in rendered modal output.
func assertModalFullBleed(t *testing.T, label, block string, wantWidth, wantHeight int) {
	t.Helper()
	lines := strings.Split(block, "\n")
	if len(lines) != wantHeight {
		t.Fatalf("%s: expected %d lines, got %d\n%s", label, wantHeight, len(lines), block)
	}
	for rowIdx, line := range lines {
		gotW := ansi.PrintableRuneWidth(line)
		if gotW != wantWidth {
			t.Fatalf("%s line %d: expected printable width %d, got %d: %q", label, rowIdx, wantWidth, gotW, line)
		}
		if !strings.HasPrefix(line, "\x1b[") {
			t.Fatalf("%s line %d: does not start with SGR prefix: %q", label, rowIdx, line)
		}
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("%s line %d: does not end with SGR reset: %q", label, rowIdx, line)
		}

		body := strings.TrimSuffix(line, "\x1b[0m")
		for i := 0; i < len(body); i++ {
			if body[i] == '\x1b' && i+1 < len(body) && body[i+1] == '[' {
				j := i + 2
				for j < len(body) && ((body[j] >= '0' && body[j] <= '9') || body[j] == ';') {
					j++
				}
				if j < len(body) && body[j] == 'm' {
					params := body[i+2 : j]
					isReset := params == "" || params == "0" || params == "00" || strings.HasPrefix(params, "0;")
					if isReset {
						rest := body[j+1:]
						if rest != "" && !strings.HasPrefix(rest, "\x1b[") {
							t.Fatalf("%s line %d: unrecovered SGR reset at byte %d before %q in line %q",
								label, rowIdx, i, rest, line)
						}
					}
					i = j
				}
			}
		}

		// Verify no emoji runes exist in the modal
		for _, r := range line {
			if (r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x26A0 && r <= 0x26FF) || r == 0x274C || r == 0x2328 || r == 0xFE0F {
				t.Fatalf("%s line %d: unexpected emoji rune U+%04X (%q) in modal output", label, rowIdx, r, r)
			}
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7E) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func sampleCommit() models.TimelineCommit {
	return models.TimelineCommit{
		CommitID:  "a1b2c3d4",
		FullSHA:   "a1b2c3d4e5f60718293a",
		Timestamp: time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC),
		Author:    "netops",
		Message:   "Update BGP neighbor timers and interface descriptions",
	}
}

func TestThemeModalLivePreviewConfirmAndRevert(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")
	defer func() { _, _ = SetTheme("dark") }()

	tm := NewThemeModal(100, 30)
	if len(tm.Themes) != 11 {
		t.Fatalf("expected 11 built-in themes, got %d", len(tm.Themes))
	}
	if tm.InitialTheme != "dark" || tm.SelectedIndex != 0 {
		t.Fatalf("expected InitialTheme=dark and SelectedIndex=0, got %q idx=%d", tm.InitialTheme, tm.SelectedIndex)
	}

	// Render and check all 11 theme names, dark/light tags, markers, and swatches
	plain := stripANSI(tm.View())
	for _, th := range AvailableThemes() {
		if !strings.Contains(plain, th.DisplayName) {
			t.Fatalf("ThemeModal View missing theme DisplayName %q", th.DisplayName)
		}
	}
	if !strings.Contains(plain, "▸") || !strings.Contains(plain, "●") || !strings.Contains(plain, "██") {
		t.Fatalf("ThemeModal View missing expected marker/swatch glyphs:\n%s", plain)
	}
	if !strings.Contains(plain, "[dark ]") || !strings.Contains(plain, "[light]") {
		t.Fatalf("ThemeModal View missing dark/light tags:\n%s", plain)
	}

	// Down arrow immediately live-previews next theme
	var changed, confirmed, closed bool
	tm, changed, confirmed, closed = tm.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !changed || confirmed || closed {
		t.Fatalf("expected Down to live-preview (changed=true, confirmed=false, close=false), got %v/%v/%v", changed, confirmed, closed)
	}
	if CurrentTheme().Name != tm.Themes[1].Name {
		t.Fatalf("expected live preview theme %q, got %q", tm.Themes[1].Name, CurrentTheme().Name)
	}

	// PgDn moves forward and previews
	tm, changed, _, _ = tm.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if !changed || CurrentTheme().Name != tm.SelectedTheme().Name {
		t.Fatalf("expected PgDn to update live preview theme to %q, got %q", tm.SelectedTheme().Name, CurrentTheme().Name)
	}

	// MouseWheelUp moves back and previews
	prevIdx := tm.SelectedIndex
	tm, changed, _, _ = tm.Update(tea.MouseMsg{X: 50, Y: 15, Type: tea.MouseWheelUp})
	if !changed || tm.SelectedIndex != prevIdx-1 || CurrentTheme().Name != tm.SelectedTheme().Name {
		t.Fatalf("expected MouseWheelUp to decrement SelectedIndex and live preview")
	}

	// Esc reverts to InitialTheme ("dark")
	tm, changed, confirmed, closed = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !changed || confirmed || !closed {
		t.Fatalf("expected Esc after preview to revert (changed=true, confirmed=false, close=true), got %v/%v/%v", changed, confirmed, closed)
	}
	if CurrentTheme().Name != "dark" {
		t.Fatalf("expected Esc to revert active theme to 'dark', got %q", CurrentTheme().Name)
	}

	// Open again, move to dracula, test 'q' revert
	tm2 := NewThemeModal(100, 30)
	tm2, _, _, _ = tm2.Update(tea.KeyMsg{Type: tea.KeyDown})
	tm2, changed, confirmed, closed = tm2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !changed || confirmed || !closed || CurrentTheme().Name != "dark" {
		t.Fatalf("expected 'q' to revert to dark and close, got changed=%v confirmed=%v closed=%v theme=%q", changed, confirmed, closed, CurrentTheme().Name)
	}

	// Open again, move to a new theme, Enter confirms it
	tm3 := NewThemeModal(100, 30)
	tm3, _, _, _ = tm3.Update(tea.KeyMsg{Type: tea.KeyDown})
	tm3, _, _, _ = tm3.Update(tea.KeyMsg{Type: tea.KeyDown})
	wantConfirmed := tm3.SelectedTheme().Name
	tm3, _, confirmed, closed = tm3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !confirmed || !closed {
		t.Fatalf("expected Enter to confirm and close, got confirmed=%v closed=%v", confirmed, closed)
	}
	if CurrentTheme().Name != wantConfirmed {
		t.Fatalf("expected confirmed theme %q to remain active, got %q", wantConfirmed, CurrentTheme().Name)
	}
}

func TestRestoreModalSafeDefaultAndImpact(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	commit := sampleCommit()

	// 1. Default focus is Cancel — bare Enter MUST NOT confirm!
	rm := NewRestoreModal(commit, 100, 30)
	if rm.FocusedButton != RestoreButtonCancel {
		t.Fatalf("expected default FocusedButton=RestoreButtonCancel (0), got %d", rm.FocusedButton)
	}
	_, confirmed, closed := rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if confirmed || !closed {
		t.Fatalf("bare Enter on default Cancel focus MUST cancel (confirmed=false, close=true), got confirmed=%v close=%v", confirmed, closed)
	}

	// Space on Cancel also cancels
	_, confirmed, closed = rm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if confirmed || !closed {
		t.Fatalf("Space on default Cancel focus MUST cancel, got confirmed=%v close=%v", confirmed, closed)
	}

	// Arrow / Tab toggles to Confirm Restore, then Enter confirms
	rm, confirmed, closed = rm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if rm.FocusedButton != RestoreButtonConfirm || confirmed || closed {
		t.Fatalf("expected Right arrow to focus Confirm Restore without closing")
	}
	rm, confirmed, closed = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !confirmed || !closed || !rm.Confirmed {
		t.Fatalf("expected Enter on focused Confirm Restore to confirm, got confirmed=%v close=%v", confirmed, closed)
	}

	// 'y' confirms directly, 'n' and Esc cancel directly
	rmY := NewRestoreModal(commit, 100, 30)
	_, confirmed, closed = rmY.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !confirmed || !closed {
		t.Fatalf("expected 'y' to confirm restore")
	}
	rmN := NewRestoreModal(commit, 100, 30)
	_, confirmed, closed = rmN.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if confirmed || !closed {
		t.Fatalf("expected 'n' to cancel restore")
	}

	// 2. Impact diff summary + top 5 paths + overflow line
	impact := models.SemanticDiffResult{
		HasChanges:    true,
		AddedCount:    2,
		ModifiedCount: 3,
		DeletedCount:  2,
		Changes: []models.PathChange{
			{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffModified},
			{Path: "/interface[name=ethernet-1/2]/admin-state", DiffType: models.DiffAdded},
			{Path: "/network-instance[name=default]/protocols/bgp/autonomous-system", DiffType: models.DiffModified},
			{Path: "/system/name/host-name", DiffType: models.DiffModified},
			{Path: "/acl/ipv4-filter[name=edge]/entry[sequence-id=10]/action", DiffType: models.DiffAdded},
			{Path: "/system/ntp/server[address=10.0.0.1]", DiffType: models.DiffDeleted},
			{Path: "/system/ntp/server[address=10.0.0.2]", DiffType: models.DiffDeleted},
		},
	}
	rmImpact := NewRestoreModalWithImpact(commit, impact, 100, 30)
	plain := stripANSI(rmImpact.View())
	if !strings.Contains(plain, "+2 added · ~3 modified · -2 deleted") {
		t.Fatalf("expected impact counts summary in RestoreModal, got:\n%s", plain)
	}
	if !strings.Contains(plain, "/interface[name=ethernet-1/1]/description") ||
		!strings.Contains(plain, "/acl/ipv4-filter[name=edge]/entry[sequence-id=10]/action") {
		t.Fatalf("expected top 5 affected paths in RestoreModal, got:\n%s", plain)
	}
	if !strings.Contains(plain, "2 more affected paths") {
		t.Fatalf("expected overflow summary for >5 affected paths, got:\n%s", plain)
	}
	if !strings.Contains(plain, "[ Cancel ]") || !strings.Contains(plain, "[ Confirm Restore ]") {
		t.Fatalf("expected explicit [ Cancel ] and [ Confirm Restore ] buttons, got:\n%s", plain)
	}

	// 3. Empty impact diff message
	rmEmpty := NewRestoreModalWithImpact(commit, models.SemanticDiffResult{HasChanges: false}, 100, 30)
	emptyPlain := stripANSI(rmEmpty.View())
	if !strings.Contains(emptyPlain, "No differences vs running config") {
		t.Fatalf("expected 'No differences vs running config' for empty impact, got:\n%s", emptyPlain)
	}
}

func TestCherryPickModalScalarPreviewPrunedCountAndInteractions(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	longCert := strings.Repeat("MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A", 12)
	changes := []models.PathChange{
		{
			Path:     "/interface[name=ethernet-1/1]/description",
			DiffType: models.DiffModified,
			OldValue: "uplink-old",
			NewValue: "uplink-new",
		},
		{
			Path:     "/interface[name=ethernet-1/1]/admin-state",
			DiffType: models.DiffAdded,
			NewValue: "enable",
		},
		{
			Path:     "/interface[name=ethernet-1/1]/mtu",
			DiffType: models.DiffDeleted,
			OldValue: 1500,
		},
		{
			Path:     "/system/tls/server-profile[name=default]/certificate",
			DiffType: models.DiffModified,
			OldValue: "OLD_" + longCert,
			NewValue: "NEW_" + longCert,
		},
	}

	cm := NewCherryPickModal(sampleCommit(), changes, nil, 110, 32)

	// 1. Enter on collapsed container (/interface) MUST expand it rather than closing!
	if cm.VisibleNodes[0].Expanded {
		t.Fatalf("expected /interface root to start collapsed")
	}
	cm, paths, closed := cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if closed || len(paths) != 0 {
		t.Fatalf("Enter on collapsed container must expand, not close; got paths=%v closed=%v", paths, closed)
	}
	if !cm.RootNodes[0].Expanded {
		t.Fatalf("expected /interface to be expanded after Enter")
	}

	// Expand all to inspect scalar previews and pruned counts
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	viewExpanded := cm.View()
	assertModalFullBleed(t, "cherrypick-expanded-long-cert", viewExpanded, 110, 32)
	expectedTilde := lipgloss.NewStyle().Bold(true).Background(ColorWarningBg).Foreground(ColorOnWarning).Render("~")
	if !strings.Contains(viewExpanded, expectedTilde) {
		t.Fatalf("expected modified '~' indicator to use Background(ColorWarningBg).Foreground(ColorOnWarning)")
	}
	plainExpanded := stripANSI(viewExpanded)
	if !strings.Contains(plainExpanded, "uplink-old → uplink-new") {
		t.Fatalf("expected modified leaf scalar 'old → new' preview, got:\n%s", plainExpanded)
	}
	if strings.Contains(plainExpanded, "📄") || strings.Contains(plainExpanded, "📁") || strings.Contains(plainExpanded, "🍒") {
		t.Fatalf("unexpected legacy emoji icons in CherryPickModal:\n%s", plainExpanded)
	}

	// 2. Select /interface (parent + 4 descendants) -> pruned count MUST be 1 ("1 path selected"), NOT 5!
	cm.SelectedIndex = 0 // /interface
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if len(cm.SelectedPaths) <= 1 {
		t.Fatalf("expected parent selection to check descendants in SelectedPaths map, got %d", len(cm.SelectedPaths))
	}
	pruned := cm.PrunedSelectedPaths()
	if len(pruned) != 1 || pruned[0] != "/interface" {
		t.Fatalf("expected 1 pruned path [/interface], got %v", pruned)
	}
	plainOne := stripANSI(cm.View())
	if !strings.Contains(plainOne, "1 path selected") {
		t.Fatalf("expected singular '1 path selected' (pruned count), got:\n%s", plainOne)
	}
	if strings.Contains(plainOne, "1 items selected") {
		t.Fatalf("found ungrammatical '1 items selected' in CherryPickModal")
	}

	// Select /system too -> pruned count MUST be 2 ("2 paths selected")
	for i, n := range cm.VisibleNodes {
		if n.FullPath == "/system" {
			cm.SelectedIndex = i
			break
		}
	}
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	plainTwo := stripANSI(cm.View())
	if !strings.Contains(plainTwo, "2 paths selected") {
		t.Fatalf("expected plural '2 paths selected', got:\n%s", plainTwo)
	}

	// 3. Mouse left-click on a tree row selects and toggles it
	_, _, _, _, _, startIdx, _, listStartY, _, leftX, _ := cm.renderLayout()
	targetRow := 0 // /interface (currently checked)
	clickY := listStartY + (targetRow - startIdx)
	cm, _, _ = cm.Update(tea.MouseMsg{X: leftX + 10, Y: clickY, Type: tea.MouseLeft})
	if cm.SelectedIndex != targetRow {
		t.Fatalf("expected mouse click to move SelectedIndex to %d, got %d", targetRow, cm.SelectedIndex)
	}
	if cm.SelectedPaths["/interface"] {
		t.Fatalf("expected mouse click on checked /interface row to toggle it off")
	}

	// 4. Explicit 'A' shortcut applies selected paths immediately
	cm, applied, closed := cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if !closed || len(applied) != 1 || applied[0] != "/system" {
		t.Fatalf("expected 'A' to apply pruned selected path [/system], got applied=%v closed=%v", applied, closed)
	}
}

func TestExportModalFocusSeparationAndDigitPaths(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	em := NewExportModal(sampleCommit(), 100, 30)
	if em.FocusedSection != ExportFocusFormat {
		t.Fatalf("expected initial focus on ExportFocusFormat, got %d", em.FocusedSection)
	}
	if em.IsEditingPath() {
		t.Fatalf("expected PathInput NOT focused initially")
	}

	// Format keys work cleanly while Format is focused
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if em.Format != "cli" {
		t.Fatalf("expected Format='cli' after pressing '2' on Format selector, got %q", em.Format)
	}
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyUp})
	if em.Format != "json" {
		t.Fatalf("expected Format='json' after Up arrow, got %q", em.Format)
	}

	// Tab switches focus to Output Path input
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyTab})
	if em.FocusedSection != ExportFocusPath || !em.IsEditingPath() {
		t.Fatalf("expected Tab to move focus to ExportFocusPath, got %d", em.FocusedSection)
	}

	// Typing a path starting with '1', '2', '3' on an EMPTY input MUST NOT switch format!
	digitPath := "123_srl_backup.json"
	for _, r := range digitPath {
		em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if em.Format != "json" {
		t.Fatalf("typing digits in PathInput must not change Format; got %q", em.Format)
	}
	if em.PathInput.Value() != digitPath {
		t.Fatalf("expected PathInput value %q, got %q", digitPath, em.PathInput.Value())
	}

	// Tab to Export button, Right arrow to Cancel button, Space cancels
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyTab})
	if em.FocusedSection != ExportFocusExport {
		t.Fatalf("expected Tab from Path to Export button, got %d", em.FocusedSection)
	}
	em, _, _, _ = em.Update(tea.KeyMsg{Type: tea.KeyRight})
	if em.FocusedSection != ExportFocusCancel {
		t.Fatalf("expected Right arrow to move to Cancel button, got %d", em.FocusedSection)
	}
	_, format, outPath, closed := em.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !closed || format != "" || outPath != "" {
		t.Fatalf("expected Space on Cancel to close with empty format/path, got fmt=%q path=%q closed=%v", format, outPath, closed)
	}

	// When Format == "startup", Tab skips PathInput and goes straight to Export button
	em2 := NewExportModal(sampleCommit(), 100, 30)
	em2, _, _, _ = em2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if em2.Format != "startup" {
		t.Fatalf("expected Format='startup', got %q", em2.Format)
	}
	em2, _, _, _ = em2.Update(tea.KeyMsg{Type: tea.KeyTab})
	if em2.FocusedSection != ExportFocusExport {
		t.Fatalf("expected Tab on startup format to skip PathInput and focus Export button, got %d", em2.FocusedSection)
	}
	_, format, outPath, closed = em2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !closed || format != "startup" || outPath != "" {
		t.Fatalf("expected Enter on Export for startup to return startup/'', got fmt=%q path=%q closed=%v", format, outPath, closed)
	}
}

func TestRemoteModalArrowSpaceAndNarrowWidth(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	cfg := models.RemoteRepoConfig{
		URL:        "git@github.com:org/srl.git",
		AutoPush:   false,
		SyncStatus: "Up to date",
		PublicKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey netops@srl",
	}
	rm := NewRemoteModal(cfg, 68, 24)

	// Tab to AutoPush, Space toggles it
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if rm.FocusedIndex != RemoteFocusAutoPush {
		t.Fatalf("expected focus on AutoPush (1), got %d", rm.FocusedIndex)
	}
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !rm.AutoPush {
		t.Fatalf("expected Space to toggle AutoPush true")
	}

	// Tab to Copy Key (2), verify focused Copy Key uses ColorWarningBg / ColorOnWarning
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if rm.FocusedIndex != RemoteFocusCopyKey {
		t.Fatalf("expected focus on Copy Key (2), got %d", rm.FocusedIndex)
	}
	expectedCopyToken := lipgloss.NewStyle().
		Background(ColorWarningBg).
		Foreground(ColorOnWarning).
		Bold(true).
		Render("Copy Key")
	if !strings.Contains(rm.View(), expectedCopyToken) {
		t.Fatalf("expected focused Copy Key button to use Background(ColorWarningBg).Foreground(ColorOnWarning)")
	}

	// Right/Left arrows navigate across action buttons 2..5
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if rm.FocusedIndex != RemoteFocusPushNow {
		t.Fatalf("expected Right from Copy Key to Push Now (3), got %d", rm.FocusedIndex)
	}
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if rm.FocusedIndex != RemoteFocusSave {
		t.Fatalf("expected Right from Push Now to Save (4), got %d", rm.FocusedIndex)
	}
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if rm.FocusedIndex != RemoteFocusCancel {
		t.Fatalf("expected Right from Save to Cancel (5), got %d", rm.FocusedIndex)
	}
	rm, _, _, _ = rm.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if rm.FocusedIndex != RemoteFocusSave {
		t.Fatalf("expected Left from Cancel to Save (4), got %d", rm.FocusedIndex)
	}

	// Space on Save triggers save & close
	savedModel, save, pushNow, closed := rm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !save || pushNow || !closed || !savedModel.Config.AutoPush {
		t.Fatalf("expected Space on Save to return save=true, pushNow=false, close=true; got %v/%v/%v", save, pushNow, closed)
	}

	// Space on Push Now triggers save=true, pushNow=true
	rm.setFocus(RemoteFocusPushNow)
	_, save, pushNow, closed = rm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !save || !pushNow || closed {
		t.Fatalf("expected Space on Push Now to return save=true, pushNow=true, close=false; got %v/%v/%v", save, pushNow, closed)
	}

	// Narrow terminal < 72 cols renders all 4 button labels cleanly with zero clipping
	for _, sz := range []struct{ w, h int }{{68, 24}, {56, 20}, {40, 12}} {
		rmN := NewRemoteModal(cfg, sz.w, sz.h)
		out := rmN.View()
		assertModalFullBleed(t, fmt.Sprintf("remote-%dx%d", sz.w, sz.h), out, sz.w, sz.h)
		plain := stripANSI(out)
		for _, lbl := range []string{"Copy Key", "Push Now", "Save", "Cancel"} {
			if !strings.Contains(plain, lbl) {
				t.Fatalf("remote-%dx%d missing button %q:\n%s", sz.w, sz.h, lbl, plain)
			}
		}
	}
}

func TestHelpModalCategoriesScrollAndKeybindings(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	hm := NewHelpModal(120, 35)
	plain := stripANSI(hm.View())

	// Must have all 3 categories and user-chosen keybindings
	mustContain := []string{
		"Navigation & Panes",
		"Views & Compare",
		"Actions & Modals",
		"Remote Git Sync",
		"Ctrl+t",
		"Zoom",
		"Base Commit",
		"4 / v",
		"Toggle vs Running Config",
		"Shift+Tab",
		"Cherry-Pick",
		"Press [Esc], [Enter], or [?] to close",
	}
	for _, s := range mustContain {
		if !strings.Contains(plain, s) {
			t.Fatalf("HelpModal missing expected text %q:\n%s", s, plain)
		}
	}
	// Must NOT advertise Vim g/G jump bindings
	if strings.Contains(plain, "Home / g") || strings.Contains(plain, "End / G") {
		t.Fatalf("HelpModal must not include Vim g/G jump bindings:\n%s", plain)
	}

	// Short terminal (40x12) must scroll vertically via arrows, PgDn/PgUp, and mouse wheel
	hmShort := NewHelpModal(40, 12)
	if hmShort.maxScrollOffset() <= 0 {
		t.Fatalf("expected HelpModal at 40x12 to be vertically scrollable")
	}
	hmShort, closed := hmShort.Update(tea.KeyMsg{Type: tea.KeyDown})
	if closed || hmShort.ScrollOffset != 1 {
		t.Fatalf("expected Down to scroll offset to 1, got %d (closed=%v)", hmShort.ScrollOffset, closed)
	}
	hmShort, _ = hmShort.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if hmShort.ScrollOffset <= 1 {
		t.Fatalf("expected PgDown to advance ScrollOffset past 1, got %d", hmShort.ScrollOffset)
	}
	afterPgDn := hmShort.ScrollOffset
	hmShort, _ = hmShort.Update(tea.MouseMsg{X: 20, Y: 6, Type: tea.MouseWheelUp})
	if hmShort.ScrollOffset != afterPgDn-1 {
		t.Fatalf("expected MouseWheelUp to decrement ScrollOffset to %d, got %d", afterPgDn-1, hmShort.ScrollOffset)
	}
	hmShort, _ = hmShort.Update(tea.MouseMsg{X: 20, Y: 6, Type: tea.MouseWheelDown})
	if hmShort.ScrollOffset != afterPgDn {
		t.Fatalf("expected MouseWheelDown to increment ScrollOffset back to %d, got %d", afterPgDn, hmShort.ScrollOffset)
	}

	// Esc closes
	_, closed = hmShort.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !closed {
		t.Fatalf("expected Esc to close HelpModal")
	}
}

func TestAllModalsFullBleedInvariantsAcrossAll11ThemesAndSizes(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() { _, _ = SetTheme("dark") }()

	commit := sampleCommit()
	impact := models.SemanticDiffResult{
		HasChanges:    true,
		AddedCount:    1,
		ModifiedCount: 2,
		DeletedCount:  1,
		Changes: []models.PathChange{
			{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffModified, OldValue: "old", NewValue: "new"},
			{Path: "/interface[name=ethernet-1/2]/admin-state", DiffType: models.DiffAdded, NewValue: "enable"},
			{Path: "/system/name/host-name", DiffType: models.DiffModified, OldValue: "srl1", NewValue: "srl2"},
			{Path: "/system/banner/login-banner", DiffType: models.DiffDeleted, OldValue: "Welcome"},
		},
	}
	remoteCfg := models.RemoteRepoConfig{
		URL:        "git@github.com:example/srl-configs.git",
		Branch:     "main",
		AutoPush:   true,
		SyncStatus: "Up to date",
		PublicKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@srl",
	}

	sizes := []struct{ w, h int }{
		{80, 24},
		{120, 35},
		{40, 12},
	}

	themes := AvailableThemes()
	if len(themes) != 11 {
		t.Fatalf("expected 11 built-in themes, got %d", len(themes))
	}

	for _, th := range themes {
		_, ok := SetTheme(th.Name)
		if !ok {
			t.Fatalf("SetTheme(%q) failed", th.Name)
		}

		for _, sz := range sizes {
			prefix := fmt.Sprintf("theme=%s/%dx%d", th.Name, sz.w, sz.h)

			// 1. ThemeModal
			tm := NewThemeModal(sz.w, sz.h)
			tmOut := tm.View()
			assertModalFullBleed(t, prefix+"/theme", tmOut, sz.w, sz.h)
			if !strings.Contains(tmOut, "╰") {
				t.Fatalf("%s/theme clipped bottom border ╰", prefix)
			}

			// 2. RestoreModal (with impact)
			rm := NewRestoreModalWithImpact(commit, impact, sz.w, sz.h)
			rmOut := rm.View()
			assertModalFullBleed(t, prefix+"/restore", rmOut, sz.w, sz.h)
			if !strings.Contains(rmOut, "╰") {
				t.Fatalf("%s/restore clipped bottom border ╰", prefix)
			}

			// 3. CherryPickModal (expanded with checked items)
			cm := NewCherryPickModal(commit, impact.Changes, nil, sz.w, sz.h)
			cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
			cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			cmOut := cm.View()
			assertModalFullBleed(t, prefix+"/cherrypick", cmOut, sz.w, sz.h)
			if !strings.Contains(cmOut, "╰") {
				t.Fatalf("%s/cherrypick clipped bottom border ╰", prefix)
			}

			// 4. ExportModal
			em := NewExportModal(commit, sz.w, sz.h)
			emOut := em.View()
			assertModalFullBleed(t, prefix+"/export", emOut, sz.w, sz.h)
			if !strings.Contains(emOut, "╰") {
				t.Fatalf("%s/export clipped bottom border ╰", prefix)
			}

			// 5. RemoteModal (focused Copy Key to test ColorWarningBg/ColorOnWarning too)
			rem := NewRemoteModal(remoteCfg, sz.w, sz.h)
			rem.setFocus(RemoteFocusCopyKey)
			remOut := rem.View()
			assertModalFullBleed(t, prefix+"/remote", remOut, sz.w, sz.h)
			if !strings.Contains(remOut, "╰") {
				t.Fatalf("%s/remote clipped bottom border ╰", prefix)
			}

			// 6. HelpModal
			hm := NewHelpModal(sz.w, sz.h)
			hmOut := hm.View()
			assertModalFullBleed(t, prefix+"/help", hmOut, sz.w, sz.h)
			if !strings.Contains(hmOut, "╰") {
				t.Fatalf("%s/help clipped bottom border ╰", prefix)
			}
		}
	}
}

func TestModalFootersNeverClipMidWord(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	commit := sampleCommit()
	changes := []models.PathChange{
		{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffModified, OldValue: "old", NewValue: "new"},
		{Path: "/system/name/host-name", DiffType: models.DiffModified, OldValue: "srl1", NewValue: "srl2"},
	}
	impact := models.SemanticDiffResult{
		HasChanges:    true,
		ModifiedCount: 2,
		Changes:       changes,
	}
	remoteCfg := models.RemoteRepoConfig{
		URL:        "git@github.com:org/srl.git",
		AutoPush:   true,
		SyncStatus: "Up to date",
		PublicKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey netops@srl",
	}

	assertHintsIntact := func(t *testing.T, label, rendered string, w, h int, hints []string) {
		t.Helper()
		assertModalFullBleed(t, label, rendered, w, h)
		plain := stripANSI(rendered)
		for _, hint := range hints {
			if !strings.Contains(plain, hint) {
				t.Fatalf("%s: footer hint %q missing or clipped in rendered modal:\n%s", label, hint, plain)
			}
		}
	}

	// Standard 120x35 and compact 80x24 sizes plus narrow 52x24 wrapping check
	for _, sz := range []struct{ w, h int }{{120, 35}, {80, 24}, {52, 24}} {
		tag := fmt.Sprintf("%dx%d", sz.w, sz.h)

		rm := NewRestoreModalWithImpact(commit, impact, sz.w, sz.h)
		assertHintsIntact(t, "restore-"+tag, rm.View(), sz.w, sz.h,
			[]string{"←/→/Tab Switch", "Enter Select", "y Confirm", "n/Esc Cancel"})

		em := NewExportModal(commit, sz.w, sz.h)
		assertHintsIntact(t, "export-"+tag, em.View(), sz.w, sz.h,
			[]string{"Tab Field", "1-3/↑↓ Format", "Enter Export", "Esc Cancel"})

		cm := NewCherryPickModal(commit, changes, nil, sz.w, sz.h)
		cpHints := []string{"Space/x Sel", "a All", "Enter Expand", "A Apply", "Tab Focus", "Esc Cancel"}
		if sz.w >= 120 {
			cpHints = []string{"Space/x Select", "a All", "Enter Expand/Apply", "A Apply", "Tab Focus", "Esc Cancel"}
		}
		assertHintsIntact(t, "cherrypick-"+tag, cm.View(), sz.w, sz.h, cpHints)

		rem := NewRemoteModal(remoteCfg, sz.w, sz.h)
		assertHintsIntact(t, "remote-"+tag, rem.View(), sz.w, sz.h,
			[]string{"Tab/↑↓←→ Move", "Space/Enter Select", "c Copy Key", "Esc Close"})
	}

	// Ultra-compact 40x12 uses concise single-line fallbacks without mid-word clipping
	assertHintsIntact(t, "restore-40x12", NewRestoreModalWithImpact(commit, impact, 40, 12).View(), 40, 12,
		[]string{"←/→ Pick", "Enter OK", "y/Esc"})
	assertHintsIntact(t, "export-40x12", NewExportModal(commit, 40, 12).View(), 40, 12,
		[]string{"Tab Field", "Enter OK", "Esc Cancel"})
	assertHintsIntact(t, "cherrypick-40x12", NewCherryPickModal(commit, changes, nil, 40, 12).View(), 40, 12,
		[]string{"Space Sel", "Enter OK", "Esc"})
	assertHintsIntact(t, "remote-40x12", NewRemoteModal(remoteCfg, 40, 12).View(), 40, 12,
		[]string{"Tab/←→ Move", "Enter OK", "Esc Close"})
}

func TestRestoreModalImpactDeduplicationSmartXPathAndCleanSummary(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	// Simulate realistic diff output: 10 leaf changes (+4 added, ~3 modified, -3 deleted)
	// interleaved with 10 parent container rows (20 raw PathChanges total), matching #15.
	impact := models.SemanticDiffResult{
		HasChanges:    true,
		AddedCount:    4,
		ModifiedCount: 3,
		DeletedCount:  3,
		Changes: []models.PathChange{
			// Added ACL entry with 5 parent container rows + 4 leaves
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]", DiffType: models.DiffAdded, NewValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/action", DiffType: models.DiffAdded, NewValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/action/drop", DiffType: models.DiffAdded, NewValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/description", DiffType: models.DiffAdded, NewValue: "drop bogons"},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/match", DiffType: models.DiffAdded, NewValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/match/ipv4", DiffType: models.DiffAdded, NewValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/match/ipv4/protocol", DiffType: models.DiffAdded, NewValue: "tcp"},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/match/ipv4/source-ip", DiffType: models.DiffAdded, NewValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/match/ipv4/source-ip/prefix", DiffType: models.DiffAdded, NewValue: "10.0.0.0/8"},
			// 3 modified leaves
			{Path: "/interface[name=ethernet-1/1]/subinterface[index=0]/ipv4/address[ip-prefix=10.10.1.1/30]/primary", DiffType: models.DiffModified, OldValue: false, NewValue: true},
			{Path: "/system/name/host-name", DiffType: models.DiffModified, OldValue: "leaf1", NewValue: "leaf2"},
			{Path: "/network-instance[name=default]/protocols/bgp/autonomous-system", DiffType: models.DiffModified, OldValue: 65001, NewValue: 65002},
			// Deleted container with 5 parent rows + 3 leaves
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]", DiffType: models.DiffDeleted, OldValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]/action", DiffType: models.DiffDeleted, OldValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]/action/accept", DiffType: models.DiffDeleted, OldValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]/match", DiffType: models.DiffDeleted, OldValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]/match/ipv4", DiffType: models.DiffDeleted, OldValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]/match/ipv4/destination-ip", DiffType: models.DiffDeleted, OldValue: map[string]interface{}{}},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]/match/ipv4/destination-ip/prefix", DiffType: models.DiffDeleted, OldValue: "192.0.2.0/24"},
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=30]/description", DiffType: models.DiffDeleted, OldValue: "old rule"},
		},
	}

	leaves := deduplicatedLeafChanges(impact.Changes)
	if len(leaves) != 10 {
		t.Fatalf("expected 10 deduplicated leaf paths, got %d: %+v", len(leaves), leaves)
	}

	// Verify fallback counting when AddedCount/ModifiedCount/DeletedCount are unset
	zeroCountImpact := models.SemanticDiffResult{HasChanges: true, Changes: impact.Changes}
	rmZero := NewRestoreModalWithImpact(sampleCommit(), zeroCountImpact, 120, 35)
	a, m, d := rmZero.impactCounts(deduplicatedLeafChanges(zeroCountImpact.Changes))
	if a != 4 || m != 3 || d != 3 {
		t.Fatalf("expected fallback leaf counts +4/~3/-3, got +%d/~%d/-%d", a, m, d)
	}

	// Commit with raw boilerplate + human comment
	commitWithComment := models.TimelineCommit{
		CommitID:     "df3b5eca",
		FullSHA:      "df3b5eca1234567890ab",
		Timestamp:    time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC),
		Author:       "root",
		Message:      "Config commit by root (SRL commit #8) [+6 / ~1] — Protect uplink ACL",
		SummaryScope: "acl uplink-in, interface ethernet-1/1",
	}

	for _, sz := range []struct{ w, h int }{{120, 35}, {80, 24}} {
		rm := NewRestoreModalWithImpact(commitWithComment, impact, sz.w, sz.h)
		out := rm.View()
		assertModalFullBleed(t, fmt.Sprintf("restore-dedup-%dx%d", sz.w, sz.h), out, sz.w, sz.h)
		plain := stripANSI(out)

		// Totals and remainder count MUST match leaf total (5 shown + 5 remaining = 10)
		if !strings.Contains(plain, "+4 added · ~3 modified · -3 deleted") {
			t.Fatalf("%dx%d: missing impact totals:\n%s", sz.w, sz.h, plain)
		}
		if !strings.Contains(plain, "and 5 more affected paths") {
			t.Fatalf("%dx%d: expected 'and 5 more affected paths' (matching 10 leaves), got:\n%s", sz.w, sz.h, plain)
		}
		if strings.Contains(plain, "15 more affected paths") {
			t.Fatalf("%dx%d: parent container rows leaked into remainder count:\n%s", sz.w, sz.h, plain)
		}

		// Smart XPath truncation preserves root subsystem AND unique trailing leaf/entry segments
		wantPaths := []string{
			"/acl/.../entry[sequence-id=20]/action/drop",
			"/acl/.../entry[sequence-id=20]/description",
			"/acl/.../entry[sequence-id=20]/match/ipv4/protocol",
			"/acl/.../entry[sequence-id=20]/match/ipv4/source-ip/prefix",
			"/interface/.../ipv4/address[ip-prefix=10.10.1.1/30]/primary",
		}
		for _, wp := range wantPaths {
			if !strings.Contains(plain, wp) {
				t.Fatalf("%dx%d: expected smart-truncated unique leaf path %q, got:\n%s", sz.w, sz.h, wp, plain)
			}
		}

		// Summary strips raw boilerplate prefix
		if !strings.Contains(plain, "Protect uplink ACL") {
			t.Fatalf("%dx%d: expected clean comment in Summary line:\n%s", sz.w, sz.h, plain)
		}
		if strings.Contains(plain, "Config commit by root") {
			t.Fatalf("%dx%d: raw boilerplate prefix leaked into Summary line:\n%s", sz.w, sz.h, plain)
		}
	}

	// Commit with generic boilerplate message (no comment) prefers SummaryScope
	commitScopeOnly := models.TimelineCommit{
		CommitID:     "23c07f24",
		FullSHA:      "23c07f241234567890ab",
		Timestamp:    time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
		Author:       "root",
		Message:      "Config commit by root (SRL commit #1) [+12]",
		SummaryScope: "acl cpm, system, network-instance",
	}
	plainScope := stripANSI(NewRestoreModal(commitScopeOnly, 120, 35).View())
	if !strings.Contains(plainScope, "acl cpm, system, network-instance") {
		t.Fatalf("expected SummaryScope in Summary line when commit has no user comment, got:\n%s", plainScope)
	}
	if strings.Contains(plainScope, "Config commit by root") {
		t.Fatalf("expected raw boilerplate stripped when SummaryScope is present, got:\n%s", plainScope)
	}

	// Generic baseline message also prefers SummaryScope
	commitBaseline := models.TimelineCommit{
		CommitID:     "11112222",
		FullSHA:      "11112222333344445555",
		Timestamp:    time.Date(2026, 5, 10, 13, 0, 0, 0, time.UTC),
		Author:       "root",
		Message:      "Initial baseline configuration",
		SummaryScope: "interface ethernet-1/1, system",
	}
	if got := cleanCommitSummary(commitBaseline); got != "interface ethernet-1/1, system" {
		t.Fatalf("expected SummaryScope for baseline commit, got %q", got)
	}

	// Boilerplate with no comment and no SummaryScope still strips "Config commit by <user>"
	commitNoScope := models.TimelineCommit{
		Author:  "root",
		Message: "Config commit by root (SRL commit #4) [+2]",
	}
	if got := cleanCommitSummary(commitNoScope); got != "(SRL commit #4) [+2]" {
		t.Fatalf("expected boilerplate prefix stripped, got %q", got)
	}

	// Verify truncateXPath preserves width bounds even at very narrow widths
	deep := "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/action/drop"
	for _, maxW := range []int{64, 48, 32, 20, 10} {
		got := truncateXPath(deep, maxW)
		if w := ansi.PrintableRuneWidth(got); w > maxW {
			t.Fatalf("truncateXPath(maxW=%d) returned width %d: %q", maxW, w, got)
		}
		if maxW >= 20 && (!strings.HasPrefix(got, "/acl/") || !strings.HasSuffix(got, "drop")) {
			t.Fatalf("truncateXPath(maxW=%d) did not preserve root and trailing leaf: %q", maxW, got)
		}
	}
}
