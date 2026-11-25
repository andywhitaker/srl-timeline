package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/differ"
	"timeline/pkg/models"
)

func sampleSplitConfigs() (map[string]interface{}, map[string]interface{}) {
	var oldIfaces, newIfaces []interface{}
	for i := 1; i <= 16; i++ {
		name := fmt.Sprintf("ethernet-1/%02d", i)
		descOld := fmt.Sprintf("leaf-port-%02d", i)
		descNew := descOld
		if i == 2 {
			descNew = "leaf-port-02-updated-with-a-very-long-description-that-wraps-across-split-columns-cleanly"
		}
		oldIfaces = append(oldIfaces, map[string]interface{}{
			"name":        name,
			"admin-state": "enable",
			"description": descOld,
		})
		if i != 14 { // delete ethernet-1/14
			newIfaces = append(newIfaces, map[string]interface{}{
				"name":        name,
				"admin-state": "enable",
				"description": descNew,
			})
		}
	}
	// Add ethernet-1/17
	newIfaces = append(newIfaces, map[string]interface{}{
		"name":        "ethernet-1/17",
		"admin-state": "enable",
		"description": "new-fabric-uplink-with-long-description-to-exercise-multi-line-wrap-and-filler-alignment",
	})

	oldCfg := map[string]interface{}{
		"interface": oldIfaces,
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65001,
						"afi-safi": []interface{}{
							map[string]interface{}{
								"afi-safi-name": "ipv4-unicast",
								"admin-state":   "enable",
							},
						},
					},
				},
			},
		},
	}

	newCfg := map[string]interface{}{
		"interface": newIfaces,
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65002,
						"afi-safi": []interface{}{
							map[string]interface{}{
								"afi-safi-name": "ipv4-unicast",
								"admin-state":   "enable",
							},
							map[string]interface{}{
								"afi-safi-name": "ipv6-unicast",
								"admin-state":   "enable",
							},
						},
					},
				},
			},
		},
	}

	return oldCfg, newCfg
}

func TestDiffViewSplitActivationThresholdAndDefaults(t *testing.T) {
	dv := NewDiffViewModel(120, 30)
	if dv.SplitMode {
		t.Fatalf("expected SplitMode=false by default")
	}
	if !dv.FoldUnchanged {
		t.Fatalf("expected FoldUnchanged=true by default")
	}
	if dv.IsSplitActive() {
		t.Fatalf("expected IsSplitActive()=false when SplitMode=false")
	}
	if chips := dv.SplitEndpointHitRanges(); len(chips) != 0 {
		t.Fatalf("expected empty SplitEndpointHitRanges when split inactive, got %+v", chips)
	}

	// Below minimum width threshold (SplitMinDetailInnerWidth = 66): innerW=65 -> inactive
	dv.SplitMode = true
	dv.SetSize(SplitMinDetailInnerWidth-1, 30)
	if dv.IsSplitActive() {
		t.Fatalf("expected IsSplitActive()=false at innerW=%d (< %d)", dv.Width, SplitMinDetailInnerWidth)
	}
	if dv.HeaderRows() == 3 {
		t.Fatalf("expected non-split HeaderRows (< 3) when width below threshold, got %d", dv.HeaderRows())
	}

	// At exact minimum threshold: innerW=66 -> active
	dv.SetSize(SplitMinDetailInnerWidth, 30)
	if !dv.IsSplitActive() {
		t.Fatalf("expected IsSplitActive()=true at innerW=%d", dv.Width)
	}
	if dv.HeaderRows() != 3 {
		t.Fatalf("expected HeaderRows()=3 when split active, got %d", dv.HeaderRows())
	}
}

func TestDiffViewSplitToolbarAndTopBorderBadge(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	oldCfg, newCfg := sampleSplitConfigs()
	sbs := differ.ComputeSideBySideDiff(oldCfg, newCfg, "cli", "", true, 3)

	dv := NewDiffViewModel(98, 30) // exact min split width
	dv.IsFocused = true
	dv.SplitMode = true
	dv.FoldUnchanged = true
	dv.SetSideBySideDiff(sbs)

	// Toolbar must include unified, path, cli, split, fold, live with exact hit ranges
	toolbar, chips := dv.buildToolbar(98)
	plainBar := stripPolishANSI(toolbar)
	if gotW := lipgloss.Width(plainBar); gotW != 98 {
		t.Fatalf("split toolbar printable width=%d, want 98: %q", gotW, plainBar)
	}

	wantLabels := map[string]string{
		"unified": "[1] JSON",
		"path":    "[2] Path",
		"cli":     "[3] CLI",
		"split":   "[S] " + GlyphLive + " Split",
		"fold":    "[E] Folded",
		"live":    "[4] " + GlyphPaused + " vs Running",
	}
	if len(chips) != len(wantLabels) {
		t.Fatalf("expected %d toolbar chips when split active, got %d: %+v (bar=%q)", len(wantLabels), len(chips), chips, plainBar)
	}
	runes := []rune(plainBar)
	for _, ch := range chips {
		slice := strings.TrimSpace(string(runes[ch.StartX:ch.EndX]))
		if slice != wantLabels[ch.Label] {
			t.Fatalf("chip %q [%d:%d] = %q, want %q (bar=%q)", ch.Label, ch.StartX, ch.EndX, slice, wantLabels[ch.Label], plainBar)
		}
	}

	// Toggle FoldUnchanged=false -> "[E] Expanded"
	dv.FoldUnchanged = false
	expandedBar, _ := dv.buildToolbar(98)
	if !strings.Contains(stripPolishANSI(expandedBar), "[E] Expanded") {
		t.Fatalf("expected '[E] Expanded' when FoldUnchanged=false, got: %q", stripPolishANSI(expandedBar))
	}

	// Top border shows [SPLIT A↔B] badge and does NOT shift DetailTabHitRanges
	rendered := dv.View()
	topBorder := stripPolishANSI(strings.Split(rendered, "\n")[0])
	if !strings.Contains(topBorder, "[SPLIT A↔B]") {
		t.Fatalf("expected top border to show [SPLIT A↔B] indicator, got: %q", topBorder)
	}
	tabsNoSplit := DetailTabHitRanges(100, false)
	_, tabsWithSplit := buildDetailTabLayoutWithSplit(int(TabDiff), true, false, true, 100)
	if len(tabsNoSplit) != len(tabsWithSplit) {
		t.Fatalf("tab chip count shifted with split badge: %d vs %d", len(tabsNoSplit), len(tabsWithSplit))
	}
	for i := range tabsNoSplit {
		if tabsNoSplit[i] != tabsWithSplit[i] {
			t.Fatalf("tab chip %d shifted with split badge: %+v vs %+v", i, tabsNoSplit[i], tabsWithSplit[i])
		}
	}
}

func TestDiffViewSplitHeaderHitRangesAndStraightCenterDivider(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	oldCfg, newCfg := sampleSplitConfigs()
	leftCommit := &models.TimelineCommit{
		CommitID:  "a1b2c3d4",
		FullSHA:   "a1b2c3d400000000000000000000000000000000",
		Author:    "admin",
		Timestamp: time.Now().Add(-2 * time.Hour),
	}
	rightCommit := &models.TimelineCommit{
		CommitID:  "e5f60718",
		FullSHA:   "e5f6071800000000000000000000000000000000",
		Author:    "root",
		Timestamp: time.Now().Add(-15 * time.Minute),
	}

	for _, innerW := range []int{98, 99, 120, 141} {
		for _, vsLive := range []bool{false, true} {
			for _, mode := range []string{"cli", "unified", "path"} {
				t.Run(fmt.Sprintf("w%d_live%v_%s", innerW, vsLive, mode), func(t *testing.T) {
					sbs := differ.ComputeSideBySideDiff(oldCfg, newCfg, mode, "", true, 3)

					dv := NewDiffViewModel(innerW, 34)
					dv.IsFocused = true
					dv.SplitMode = true
					dv.DiffMode = mode
					dv.VsLive = vsLive
					dv.LeftCommit = leftCommit
					if !vsLive {
						dv.RightCommit = rightCommit
					}
					dv.SetSideBySideDiff(sbs)

					// Verify SplitEndpointHitRanges match rendered header strip pills
					hdr, chips := dv.buildSplitHeaderStrip(innerW)
					exportedChips := dv.SplitEndpointHitRanges()
					if len(chips) != 2 || len(exportedChips) != 2 {
						t.Fatalf("expected 2 endpoint hit chips, got %+v / %+v", chips, exportedChips)
					}
					if exportedChips[0].Label != "pick_left" || exportedChips[1].Label != "pick_right" {
						t.Fatalf("unexpected endpoint chip labels: %+v", exportedChips)
					}

					plainHdr := stripPolishANSI(hdr)
					if gotW := lipgloss.Width(plainHdr); gotW != innerW {
						t.Fatalf("split header width=%d, want %d", gotW, innerW)
					}
					hdrRunes := []rune(plainHdr)
					leftSlice := string(hdrRunes[exportedChips[0].StartX:exportedChips[0].EndX])
					rightSlice := string(hdrRunes[exportedChips[1].StartX:exportedChips[1].EndX])
					if !strings.HasPrefix(leftSlice, "[") || !strings.Contains(leftSlice, "a1b2c3d4") || !strings.HasSuffix(leftSlice, GlyphExpanded+"]") {
						t.Fatalf("pick_left chip slice unexpected: %q (hdr=%q)", leftSlice, plainHdr)
					}
					if vsLive {
						if !strings.Contains(rightSlice, "RUNNING CONFIG") || !strings.HasSuffix(rightSlice, GlyphExpanded+"]") {
							t.Fatalf("pick_right (live) chip slice unexpected: %q (hdr=%q)", rightSlice, plainHdr)
						}
					} else {
						if !strings.Contains(rightSlice, "e5f60718") || !strings.HasSuffix(rightSlice, GlyphExpanded+"]") {
							t.Fatalf("pick_right (commit) chip slice unexpected: %q (hdr=%q)", rightSlice, plainHdr)
						}
					}

					// Verify laser-straight center column across header, divider, and EVERY non-fold, non-footer viewport row!
					leftW, _ := splitColumnWidths(dv.viewportWidth())
					viewLines := strings.Split(stripPolishANSI(dv.View()), "\n")
					// Outer box rows: 0=top border, 1=toolbar, 2=split header, 3=divider, 4..N-2=viewport, N-1=bottom border
					// Inside outer box, left border '│' is at col 0, so inner X=leftW is at outer col leftW+1.
					centerCol := leftW + 1
					if r := []rune(viewLines[2])[centerCol]; string(r) != GlyphPipe {
						t.Fatalf("header center col %d = %q, want %q (line=%q)", centerCol, string(r), GlyphPipe, viewLines[2])
					}
					if r := []rune(viewLines[3])[centerCol]; r != '┼' {
						t.Fatalf("divider center col %d = %q, want '┼' (line=%q)", centerCol, string(r), viewLines[3])
					}
					for rowIdx := 4; rowIdx < len(viewLines)-1; rowIdx++ {
						line := viewLines[rowIdx]
						if strings.Contains(line, "unchanged lines") || strings.Contains(line, "End of Diff") || strings.TrimSpace(strings.Trim(line, "│█░ ")) == "" {
							continue
						}
						if r := []rune(line)[centerCol]; string(r) != GlyphPipe {
							t.Fatalf("viewport row %d center col %d = %q, want %q (line=%q)", rowIdx, centerCol, string(r), GlyphPipe, line)
						}
					}
				})
			}
		}
	}
}

func TestDiffViewSplitFoldBarAndFillerAndEmptyState(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	oldCfg, newCfg := sampleSplitConfigs()
	sbs := differ.ComputeSideBySideDiff(oldCfg, newCfg, "cli", "", true, 2)

	dv := NewDiffViewModel(120, 40)
	dv.SplitMode = true
	dv.FoldUnchanged = true
	dv.LeftCommit = &models.TimelineCommit{CommitID: "1111aaaa"}
	dv.RightCommit = &models.TimelineCommit{CommitID: "2222bbbb"}
	dv.SetSideBySideDiff(sbs)

	plainVP := stripPolishANSI(dv.Viewport.View())
	if !strings.Contains(plainVP, "unchanged lines") || !strings.Contains(plainVP, "press E to expand") {
		t.Fatalf("expected fold bar in folded split viewport, got:\n%s", plainVP)
	}
	// Shaded filler must appear for deleted/added rows
	if !strings.Contains(plainVP, "· "+GlyphScrollTrack) {
		t.Fatalf("expected shaded filler on added/deleted rows in split viewport, got:\n%s", plainVP)
	}

	// Empty state when identical & FoldUnchanged=true
	same := differ.ComputeSideBySideDiff(oldCfg, oldCfg, "cli", "", true, 3)
	dv.FilterQuery = "bgp"
	dv.SetSideBySideDiff(same)
	emptyVP := stripPolishANSI(dv.Viewport.View())
	if !strings.Contains(emptyVP, GlyphCheck+" No differences between A 1111aaaa and B 2222bbbb (filter: 'bgp')") {
		t.Fatalf("expected green no-differences empty state in split view, got:\n%s", emptyVP)
	}

	// Expanded identical view when FoldUnchanged=false shows equal rows side-by-side
	sameExpanded := differ.ComputeSideBySideDiff(oldCfg, oldCfg, "cli", "", false, 3)
	dv.FoldUnchanged = false
	dv.FilterQuery = ""
	dv.SetSideBySideDiff(sameExpanded)
	expVPTop := stripPolishANSI(dv.Viewport.View())
	if !strings.Contains(expVPTop, "ethernet-1/01") {
		t.Fatalf("expected expanded identical split view top to render equal CLI lines, got:\n%s", expVPTop)
	}
	dv.Viewport.GotoBottom()
	expVPBot := stripPolishANSI(dv.Viewport.View())
	if !strings.Contains(expVPBot, "ipv4-unicast") {
		t.Fatalf("expected expanded identical split view bottom to render BGP ipv4-unicast line, got:\n%s", expVPBot)
	}
}

func TestDiffViewSplitFullBleedAcrossAll11Themes(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() {
		_, _ = SetTheme("dark")
	}()

	oldCfg, newCfg := sampleSplitConfigs()
	leftCommit := &models.TimelineCommit{
		CommitID:  "a1b2c3d4",
		Author:    "admin",
		Timestamp: time.Now().Add(-1 * time.Hour),
	}
	rightCommit := &models.TimelineCommit{
		CommitID:  "e5f60718",
		Author:    "root",
		Timestamp: time.Now().Add(-5 * time.Minute),
	}

	themes := AvailableThemes()
	if len(themes) != 11 {
		t.Fatalf("expected 11 themes, got %d", len(themes))
	}

	for _, th := range themes {
		_, ok := SetTheme(th.Name)
		if !ok {
			t.Fatalf("failed to set theme %q", th.Name)
		}

		for _, tc := range []struct {
			label   string
			innerW  int
			innerH  int
			mode    string
			vsLive  bool
			focused bool
			zoomed  bool
		}{
			{"80x24-fallback", 43, 19, "cli", false, true, false},
			{"100x30-min-cli", 98, 28, "cli", false, true, false},
			{"136x36-unified-live", 134, 34, "unified", true, true, true},
			{"141x32-path-unfocused", 139, 30, "path", false, false, false},
		} {
			sbs := differ.ComputeSideBySideDiff(oldCfg, newCfg, tc.mode, "", true, 2)
			dv := NewDiffViewModel(tc.innerW, tc.innerH)
			dv.SplitMode = true
			dv.DiffMode = tc.mode
			dv.VsLive = tc.vsLive
			dv.IsFocused = tc.focused
			dv.Zoomed = tc.zoomed
			dv.LeftCommit = leftCommit
			if !tc.vsLive {
				dv.RightCommit = rightCommit
			}
			dv.Commit = rightCommit
			dv.SetSideBySideDiff(sbs)
			if !dv.IsSplitActive() {
				// Populate single-pane fallback diff for narrow fallback check
				dv.SetDiff(differ.SemanticDiff(oldCfg, newCfg, ""))
			}
			assertNoUnrecoveredReset(t, th.Name+"-split-"+tc.label, dv.View(), tc.innerW+2, tc.innerH+2)
		}
	}
}

func TestDiffViewSplitFilterBannerAndCyanMatchingMarkers(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() {
		_, _ = SetTheme("dark")
	}()
	_, _ = SetTheme("dark")

	oldCfg, newCfg := sampleSplitConfigs()
	leftCommit := &models.TimelineCommit{
		CommitID:  "a1b2c3d4",
		Author:    "admin",
		Timestamp: time.Now().Add(-1 * time.Hour),
	}
	rightCommit := &models.TimelineCommit{
		CommitID:  "e5f60718",
		Author:    "root",
		Timestamp: time.Now().Add(-5 * time.Minute),
	}

	// 1. Zero extra rows when FilterQuery == ""
	dv := NewDiffViewModel(120, 34)
	dv.SplitMode = true
	dv.IsFocused = true
	dv.LeftCommit = leftCommit
	dv.RightCommit = rightCommit
	dv.SetSideBySideDiff(differ.ComputeSideBySideDiff(oldCfg, newCfg, "cli", "", true, 2))
	if dv.HeaderRows() != 3 {
		t.Fatalf("expected HeaderRows()=3 without filter, got %d", dv.HeaderRows())
	}
	if dv.FilterBannerRow() != -1 {
		t.Fatalf("expected FilterBannerRow()=-1 without filter, got %d", dv.FilterBannerRow())
	}
	if sx, ex := dv.FilterBannerHitRange(); sx != -1 || ex != -1 {
		t.Fatalf("expected FilterBannerHitRange()=(-1,-1) without filter, got (%d,%d)", sx, ex)
	}

	// 2. Scoped split diff (DetailFilterBypass=false)
	sbsScoped := differ.ComputeSideBySideDiff(oldCfg, newCfg, "cli", "bgp", true, 2)
	dv.FilterQuery = "bgp"
	dv.DetailFilterBypass = false
	dv.TotalUnfilteredChanges = 12
	dv.SetSideBySideDiff(sbsScoped)
	if dv.HeaderRows() != 4 {
		t.Fatalf("expected HeaderRows()=4 with filter banner in split mode, got %d", dv.HeaderRows())
	}
	if dv.FilterBannerRow() != 3 {
		t.Fatalf("expected FilterBannerRow()=3 in split mode, got %d", dv.FilterBannerRow())
	}
	sx, ex := dv.FilterBannerHitRange()
	if sx < 0 || ex <= sx {
		t.Fatalf("expected valid FilterBannerHitRange in scoped split mode, got (%d,%d)", sx, ex)
	}
	viewLines := strings.Split(stripPolishANSI(dv.View()), "\n")
	// Outer row 0 = top border, inner row 3 = outer row 4
	bannerOuter := viewLines[1+dv.FilterBannerRow()]
	bannerInner := []rune(bannerOuter)[1 : len([]rune(bannerOuter))-1]
	if gotW := lipgloss.Width(string(bannerInner)); gotW != 120 {
		t.Fatalf("split scoped banner inner width=%d, want 120", gotW)
	}
	pillSlice := string(bannerInner[sx:ex])
	if pillSlice != `[\ Show Full Commit]` {
		t.Fatalf("scoped split banner pill slice [%d:%d]=%q, want %q (banner=%q)", sx, ex, pillSlice, `[\ Show Full Commit]`, string(bannerInner))
	}
	for _, want := range []string{GlyphMilestone + " FILTERED", "'bgp'", "of 12 changes", "hidden)"} {
		if !strings.Contains(string(bannerInner), want) {
			t.Fatalf("scoped split banner missing %q: %q", want, string(bannerInner))
		}
	}

	// 3. Detached / Full split diff (DetailFilterBypass=true) across CLI, unified JSON, and Path modes:
	//    verifies ● FULL banner, Re-scope pill hit range, straight center │ divider, and cyan ▸ gutter markers!
	for _, mode := range []string{"cli", "unified", "path"} {
		t.Run("bypass_split_"+mode, func(t *testing.T) {
			fold := mode != "unified" // Keep "bgp": { key visible in pretty-printed JSON split diff
			sbsFull := differ.ComputeSideBySideDiff(oldCfg, newCfg, mode, "", fold, 2)
			totalChanges := sbsFull.AddedCount + sbsFull.ModifiedCount + sbsFull.DeletedCount
			dvFull := NewDiffViewModel(134, 34)
			dvFull.SplitMode = true
			dvFull.FoldUnchanged = fold
			dvFull.DiffMode = mode
			dvFull.IsFocused = true
			dvFull.LeftCommit = leftCommit
			dvFull.RightCommit = rightCommit
			dvFull.FilterQuery = "bgp"
			dvFull.DetailFilterBypass = true
			dvFull.TotalUnfilteredChanges = totalChanges
			dvFull.SetSideBySideDiff(sbsFull)

			bsx, bex := dvFull.FilterBannerHitRange()
			if bsx < 0 || bex <= bsx {
				t.Fatalf("[%s] expected valid Full banner hit range, got (%d,%d)", mode, bsx, bex)
			}
			fullLines := strings.Split(stripPolishANSI(dvFull.View()), "\n")
			bInner := []rune(fullLines[1+dvFull.FilterBannerRow()])
			bInner = bInner[1 : len(bInner)-1]
			if gotPill := string(bInner[bsx:bex]); gotPill != `[\ Re-scope 'bgp']` {
				t.Fatalf("[%s] full split pill slice [%d:%d]=%q, want %q", mode, bsx, bex, gotPill, `[\ Re-scope 'bgp']`)
			}
			for _, want := range []string{GlyphLive + " FULL", fmt.Sprintf("all %d changes", totalChanges), "Timeline filtered by 'bgp'"} {
				if !strings.Contains(string(bInner), want) {
					t.Fatalf("[%s] full split banner missing %q: %q", mode, want, string(bInner))
				}
			}

			// Verify laser-straight center │ divider at leftW+1 across header, divider, and viewport rows
			leftW, _ := splitColumnWidths(dvFull.viewportWidth())
			centerCol := leftW + 1
			if r := []rune(fullLines[2])[centerCol]; string(r) != GlyphPipe {
				t.Fatalf("[%s] split header center col %d = %q, want %q", mode, centerCol, string(r), GlyphPipe)
			}
			if r := []rune(fullLines[3])[centerCol]; r != '┼' {
				t.Fatalf("[%s] split divider center col %d = %q, want '┼'", mode, centerCol, string(r))
			}
			// Viewport starts at outer row 1 + HeaderRows() = 5
			vpStart := 1 + dvFull.HeaderRows()
			for rowIdx := vpStart; rowIdx < len(fullLines)-1; rowIdx++ {
				line := fullLines[rowIdx]
				if strings.Contains(line, "unchanged lines") || strings.Contains(line, "End of Diff") || strings.TrimSpace(strings.Trim(line, "│█░ ")) == "" {
					continue
				}
				if r := []rune(line)[centerCol]; string(r) != GlyphPipe {
					t.Fatalf("[%s] bypass split viewport row %d center col %d = %q, want %q (line=%q)", mode, rowIdx, centerCol, string(r), GlyphPipe, line)
				}
			}

			// Verify ▸ matching-line marker appears on BGP lines and NEVER on non-matching ethernet-1/02 lines
			plainAll := strings.Split(stripPolishANSI(strings.Join(dvFull.renderedLines, "\n")), "\n")
			foundMatchMarker := false
			foundNonMatchUnmarked := false
			for _, l := range plainAll {
				if strings.Contains(l, "bgp") {
					if strings.Contains(l, GlyphCollapsed) {
						foundMatchMarker = true
					}
				}
				if strings.Contains(l, "ethernet-1/02") {
					if strings.Contains(l, GlyphCollapsed) {
						t.Fatalf("[%s] non-matching ethernet-1/02 line unexpectedly got %q marker: %q", mode, GlyphCollapsed, l)
					}
					foundNonMatchUnmarked = true
				}
			}
			if !foundMatchMarker {
				t.Fatalf("[%s] expected %q cyan marker on matching bgp split line(s)", mode, GlyphCollapsed)
			}
			if !foundNonMatchUnmarked && mode != "unified" {
				t.Fatalf("[%s] expected non-matching ethernet-1/02 line without marker", mode)
			}
		})
	}

	// 4. Full-bleed ANSI safety across all 11 themes in both Scoped & Full split modes at 100x30 and 136x36
	sbsFull := differ.ComputeSideBySideDiff(oldCfg, newCfg, "cli", "", true, 2)
	for _, th := range AvailableThemes() {
		_, _ = SetTheme(th.Name)
		for _, bypass := range []bool{false, true} {
			for _, sz := range [][2]int{{98, 28}, {134, 34}} {
				dvT := NewDiffViewModel(sz[0], sz[1])
				dvT.SplitMode = true
				dvT.IsFocused = true
				dvT.LeftCommit = leftCommit
				dvT.RightCommit = rightCommit
				dvT.FilterQuery = "bgp"
				dvT.DetailFilterBypass = bypass
				dvT.TotalUnfilteredChanges = 12
				dvT.SetSideBySideDiff(sbsFull)
				label := fmt.Sprintf("%s-split-banner-bypass%v-%dx%d", th.Name, bypass, sz[0], sz[1])
				assertNoUnrecoveredReset(t, label, dvT.View(), sz[0]+2, sz[1]+2)
			}
		}
	}
}
