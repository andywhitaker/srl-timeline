package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/blame"
	"timeline/pkg/filter"
	"timeline/pkg/models"
)

var polishANSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripPolishANSI(s string) string {
	return polishANSIRegex.ReplaceAllString(s, "")
}

// ============================================================================
// Fix 1: Path Diff ([2] Path) zero orphan badges & clean '/'-boundary wrapping
// ============================================================================

func TestWrapXPathSegmentsBoundariesAndFallback(t *testing.T) {
	// 1. Long ACL XPath wraps on '/' boundaries without mid-word breaks
	aclPath := "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/action/drop"
	lines64 := wrapXPathSegments(aclPath, 64)
	if len(lines64) != 2 {
		t.Fatalf("expected 2 wrapped lines at width 64, got %d: %#v", len(lines64), lines64)
	}
	if lines64[0] != "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]" || lines64[1] != "/action/drop" {
		t.Fatalf("unexpected / boundary split at width 64: %#v", lines64)
	}
	if strings.Join(lines64, "") != aclPath {
		t.Fatalf("rejoined path mismatch: got %q, want %q", strings.Join(lines64, ""), aclPath)
	}

	// 2. XPath with '/' inside [...] list keys keeps [name=ethernet-1/1] and [ip-prefix=10.10.1.2/30] intact
	ipPath := "/interface[name=ethernet-1/1]/subinterface[index=0]/ipv4/address[ip-prefix=10.10.1.2/30]"
	lines60 := wrapXPathSegments(ipPath, 60)
	if strings.Join(lines60, "") != ipPath {
		t.Fatalf("rejoined ipPath mismatch: got %q, want %q", strings.Join(lines60, ""), ipPath)
	}
	for i, l := range lines60 {
		if lipgloss.Width(l) > 60 {
			t.Fatalf("line %d exceeds maxW=60: len=%d %q", i, lipgloss.Width(l), l)
		}
		if strings.Contains(l, "10.10.1.2") && !strings.Contains(l, "10.10.1.2/30") {
			t.Fatalf("CIDR 10.10.1.2/30 was split mid-token across lines: %#v", lines60)
		}
		if strings.Contains(l, "ethernet-1") && !strings.Contains(l, "ethernet-1/1") {
			t.Fatalf("interface key ethernet-1/1 was split mid-token across lines: %#v", lines60)
		}
	}

	// 3. Narrow width (30 cols) sub-splits oversize segment on '[' cleanly before rune wrap
	lines30 := wrapXPathSegments(aclPath, 30)
	if strings.Join(lines30, "") != aclPath {
		t.Fatalf("rejoined aclPath at width 30 mismatch: %#v", lines30)
	}
	for i, l := range lines30 {
		if lipgloss.Width(l) > 30 {
			t.Fatalf("line %d exceeds maxW=30: %q", i, l)
		}
	}
	if lines30[0] == "" {
		t.Fatalf("first segment must never be empty: %#v", lines30)
	}

	// 4. Oversize single atomic token falls back to rune-width wrap
	mega := "/superlongsingleidentifierwithoutslashesorbrackets1234567890"
	megaLines := wrapXPathSegments(mega, 20)
	if len(megaLines) < 3 || strings.Join(megaLines, "") != mega {
		t.Fatalf("expected rune fallback wrap for mega token, got %#v", megaLines)
	}
	for i, l := range megaLines {
		if lipgloss.Width(l) > 20 {
			t.Fatalf("mega line %d exceeds maxW=20: %q", i, l)
		}
	}
}

func TestPathDiffNoOrphanBadgesAndHangingIndent(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	changes := []models.PathChange{
		{
			Path:     "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/action/drop",
			DiffType: models.DiffDeleted,
			OldValue: "true",
		},
		{
			Path:     "/interface[name=ethernet-1/1]/subinterface[index=0]/ipv4/address[ip-prefix=10.10.1.2/30]/primary",
			DiffType: models.DiffAdded,
			NewValue: "true",
		},
		{
			Path:     "/network-instance[name=default]/protocols/bgp/neighbor[peer-address=10.10.1.2]/admin-state",
			DiffType: models.DiffModified,
			OldValue: "disable",
			NewValue: "enable",
		},
	}

	for _, tc := range []struct {
		name   string
		innerW int
		innerH int
	}{
		{"80x24", 43, 19},
		{"120x35", 77, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dv := NewDiffViewModel(tc.innerW, tc.innerH)
			dv.DiffMode = "path"
			dv.IsFocused = true
			dv.SetDiff(models.SemanticDiffResult{
				HasChanges:    true,
				AddedCount:    1,
				ModifiedCount: 1,
				DeletedCount:  1,
				Changes:       changes,
			})

			plain := stripPolishANSI(dv.Viewport.View())
			lines := strings.Split(plain, "\n")

			badgeCount := 0
			contCount := 0
			for idx, line := range lines {
				// Locate content after gutter pipe "│ "
				body := line
				if pipePos := strings.Index(line, GlyphPipe+" "); pipePos >= 0 {
					body = line[pipePos+len(GlyphPipe)+1:]
				}

				for _, b := range []string{" DEL ", " ADD ", " MOD "} {
					if strings.HasPrefix(body, b) {
						badgeCount++
						afterBadge := strings.TrimSpace(body[len(b):])
						if afterBadge == "" || !strings.HasPrefix(afterBadge, "/") {
							t.Fatalf("[%s] line %d has orphaned %s badge without path start on same line: %q",
								tc.name, idx, strings.TrimSpace(b), line)
						}
					}
				}

				// Check continuation lines have 6-space hanging indent aligned with path start
				if strings.HasPrefix(strings.TrimLeft(line, " "), GlyphPipe+"       ") {
					contCount++
				}

				// Never split mid-word on "actio"/"n/drop" or "10"/".10.1.2"
				trimmed := strings.TrimSpace(body)
				if strings.HasSuffix(trimmed, "actio") || strings.HasPrefix(trimmed, "n/drop") ||
					strings.HasPrefix(trimmed, ".10.1.2") {
					t.Fatalf("[%s] line %d wrapped mid-word: %q", tc.name, idx, line)
				}
			}
			if badgeCount != 3 {
				t.Fatalf("[%s] expected 3 badge lines (DEL/ADD/MOD), found %d in:\n%s", tc.name, badgeCount, plain)
			}
			if contCount == 0 {
				t.Fatalf("[%s] expected hanging-indented continuation lines for long XPaths, got:\n%s", tc.name, plain)
			}
		})
	}
}

// ============================================================================
// Fix 2: Compact toolbar tier (< 54) & 100% ControlHitRanges alignment
// ============================================================================

func TestToolbarCompactTierAndHitRangesAcrossWidths(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	// 25 lines so viewport has meaningful scroll indicator (e.g. "17/25" or "↑ 17/25")
	cliLines := make([]string, 25)
	for i := range cliLines {
		cliLines[i] = fmt.Sprintf("+ set / interface ethernet-1/%d admin-state enable", i+1)
	}
	blameEntries := make([]models.BlameEntry, 25)
	for i := range blameEntries {
		blameEntries[i] = models.BlameEntry{
			LineNumber: i + 1,
			CommitSHA:  "df3b5eca11111111",
			Author:     "root",
			Timestamp:  time.Now().Add(-10 * time.Minute),
			Content:    fmt.Sprintf("set / interface ethernet-1/%d admin-state enable", i+1),
		}
	}

	widths := []struct {
		termW    int
		innerW   int
		innerH   int
		compact  bool
		tightPad bool
	}{
		{40, 18, 15, true, true},
		{80, 43, 19, true, false},
		{120, 77, 30, false, false},
		{220, 167, 45, false, false},
	}

	verifyToolbarHitAlignment := func(t *testing.T, viewName string, toolbar string, chips []HitChip, expectedText map[string]string, innerW int) {
		t.Helper()
		plain := stripPolishANSI(toolbar)
		if gotW := lipgloss.Width(plain); gotW != innerW {
			t.Fatalf("%s (innerW=%d): toolbar printable width=%d, want %d: %q", viewName, innerW, gotW, innerW, plain)
		}
		plainRunes := []rune(plain)
		for _, chip := range chips {
			if chip.StartX < 0 || chip.EndX > len(plainRunes) || chip.StartX >= chip.EndX {
				t.Fatalf("%s (innerW=%d): invalid hit chip %+v for plain len %d (%q)", viewName, innerW, chip, len(plainRunes), plain)
			}
			slice := strings.TrimSpace(string(plainRunes[chip.StartX:chip.EndX]))
			want := expectedText[chip.Label]
			if chip.EndX < innerW {
				// Fully visible pill must match exact label text!
				if slice != want {
					t.Fatalf("%s (innerW=%d): chip %q slice [%d:%d] = %q, want %q (toolbar=%q)",
						viewName, innerW, chip.Label, chip.StartX, chip.EndX, slice, want, plain)
				}
			} else if want != "" && !strings.HasPrefix(want, slice) && !strings.HasPrefix(slice, want) {
				t.Fatalf("%s (innerW=%d): clipped chip %q slice [%d:%d] = %q does not match prefix of %q",
					viewName, innerW, chip.Label, chip.StartX, chip.EndX, slice, want)
			}
		}
	}

	for _, w := range widths {
		t.Run(fmt.Sprintf("term_%d_inner_%d", w.termW, w.innerW), func(t *testing.T) {
			// --- DiffView (both VsLive=false and VsLive=true) ---
			for _, vsLive := range []bool{false, true} {
				dv := NewDiffViewModel(w.innerW, w.innerH)
				dv.DiffMode = "cli"
				dv.VsLive = vsLive
				dv.IsFocused = true
				dv.SetDiff(models.SemanticDiffResult{
					HasChanges:   true,
					AddedCount:   25,
					CLIDiffLines: cliLines,
				})
				toolbar, chips := dv.buildToolbar(w.innerW)
				hitRanges := dv.ControlHitRanges()
				if len(hitRanges) != len(chips) {
					t.Fatalf("DiffView ControlHitRanges count mismatch: %d vs %d", len(hitRanges), len(chips))
				}
				for i := range chips {
					if hitRanges[i] != chips[i] {
						t.Fatalf("DiffView ControlHitRanges[%d] = %+v, want %+v", i, hitRanges[i], chips[i])
					}
				}

				expected := map[string]string{
					"unified": "[1] JSON",
					"path":    "[2] Path",
					"cli":     "[3] CLI",
					"split":   "[S] " + GlyphPaused + " Split",
					"live":    "[4] " + GlyphPaused + " vs Running",
				}
				if w.compact {
					expected = map[string]string{
						"unified": "1:JSON",
						"path":    "2:Path",
						"cli":     "3:CLI",
						"live":    "4:" + GlyphPaused + "Run",
					}
					if vsLive {
						expected["live"] = "4:" + GlyphLive + "Run"
					}
				} else if w.innerW < 72 {
					expected["split"] = "S:Split"
					expected["live"] = "[4] " + GlyphPaused + " Run"
					if vsLive {
						expected["live"] = "[4] " + GlyphLive + " Run"
					}
				} else if vsLive {
					expected["live"] = "[4] " + GlyphLive + " vs Running"
				}
				verifyToolbarHitAlignment(t, fmt.Sprintf("DiffView(vsLive=%v)", vsLive), toolbar, hitRanges, expected, w.innerW)

				// Visual vertical divider │ must separate formats (1-3) from compare target toggles
				// whenever all controls fit (innerW >= 40)
				if w.innerW >= tightToolbarPadThreshold {
					plain := stripPolishANSI(toolbar)
					if !strings.Contains(plain, " "+GlyphPipe+" ") {
						t.Fatalf("DiffView(innerW=%d, vsLive=%v) missing vertical divider %q between formats and toggle: %q",
							w.innerW, vsLive, " "+GlyphPipe+" ", plain)
					}
					// Key hint 4 must ALWAYS remain visible in BOTH off and on states!
					wantKeyHint := "[4]"
					if w.compact {
						wantKeyHint = "4:"
					}
					if !strings.Contains(plain, wantKeyHint) {
						t.Fatalf("DiffView(innerW=%d, vsLive=%v) dropped key hint %q: %q", w.innerW, vsLive, wantKeyHint, plain)
					}
					// Scroll progress indicator must stay visible across wide tiers too (innerW >= 43)
					if !strings.Contains(plain, "/") && !strings.Contains(plain, "%") {
						t.Fatalf("DiffView(innerW=%d, vsLive=%v) dropped scroll indicator: %q", w.innerW, vsLive, plain)
					}
				}

				// Specifically at 80x24 (innerW=43): ALL 4 pills + │ divider + scroll indicator must fit completely unclipped!
				if w.termW == 80 {
					plain := stripPolishANSI(toolbar)
					if len(hitRanges) != 4 {
						t.Fatalf("80x24 DiffView must expose all 4 toolbar chips, got %d: %+v", len(hitRanges), hitRanges)
					}
					for _, label := range []string{"unified", "path", "cli", "live"} {
						if !strings.Contains(plain, expected[label]) {
							t.Fatalf("80x24 DiffView toolbar clipped %q: %q", expected[label], plain)
						}
					}
					// Scroll progress indicator (e.g. "17/25" or "%") must be present
					if !strings.Contains(plain, "/") && !strings.Contains(plain, "%") {
						t.Fatalf("80x24 DiffView toolbar dropped scroll indicator: %q", plain)
					}
				}
			}

			// --- ConfigView ---
			cv := NewConfigViewModel(w.innerW, w.innerH)
			cv.IsFocused = true
			cv.SetConfig(map[string]interface{}{
				"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-01"}},
			}, "")
			cfgToolbar, _ := cv.buildToolbar(w.innerW)
			cfgExpected := map[string]string{"cli": "[1] CLI", "json": "[2] JSON"}
			if w.compact {
				cfgExpected = map[string]string{"cli": "1:CLI", "json": "2:JSON"}
			}
			verifyToolbarHitAlignment(t, "ConfigView", cfgToolbar, cv.ControlHitRanges(), cfgExpected, w.innerW)
			if w.termW == 80 {
				plain := stripPolishANSI(cfgToolbar)
				if !strings.Contains(plain, "1:CLI") || !strings.Contains(plain, "2:JSON") || !strings.Contains(plain, "/") {
					t.Fatalf("80x24 ConfigView toolbar missing compact pills or scroll indicator: %q", plain)
				}
			}

			// --- BlameView ---
			bv := NewBlameViewModel(w.innerW, w.innerH)
			bv.IsFocused = true
			bv.SetBlame(blameEntries, blame.ContributorStats{TotalLines: len(blameEntries)})
			blameToolbar, _ := bv.buildToolbar(w.innerW)
			blameExpected := map[string]string{"attribution": "[1] Attribution", "analytics": "[2] Analytics", "wrap": "[w] Wrap"}
			if w.compact {
				blameExpected = map[string]string{"attribution": "1:Attr", "analytics": "2:Stats", "wrap": "w:Wrap"}
			}
			verifyToolbarHitAlignment(t, "BlameView", blameToolbar, bv.ControlHitRanges(), blameExpected, w.innerW)
			if w.termW == 80 {
				plain := stripPolishANSI(blameToolbar)
				for _, want := range []string{"1:Attr", "2:Stats", "w:Wrap", "/"} {
					if !strings.Contains(plain, want) {
						t.Fatalf("80x24 BlameView toolbar clipped %q: %q", want, plain)
					}
				}
			}
		})
	}

	// Verify medium tier (54 <= innerW < 72): compact S:Split + "[4] ○ Run" / "[4] ● Run" + │ divider + scroll status + exact hit alignment
	for _, vsLive := range []bool{false, true} {
		dvMed := NewDiffViewModel(58, 24)
		dvMed.DiffMode = "cli"
		dvMed.VsLive = vsLive
		dvMed.IsFocused = true
		dvMed.SetDiff(models.SemanticDiffResult{HasChanges: true, AddedCount: 25, CLIDiffLines: cliLines})
		medToolbar, _ := dvMed.buildToolbar(58)
		medExpected := map[string]string{
			"unified": "[1] JSON",
			"path":    "[2] Path",
			"cli":     "[3] CLI",
			"split":   "S:Split",
			"live":    "[4] " + GlyphPaused + " Run",
		}
		if vsLive {
			medExpected["live"] = "[4] " + GlyphLive + " Run"
		}
		verifyToolbarHitAlignment(t, fmt.Sprintf("DiffView-medium(vsLive=%v)", vsLive), medToolbar, dvMed.ControlHitRanges(), medExpected, 58)
		medPlain := stripPolishANSI(medToolbar)
		if !strings.Contains(medPlain, " "+GlyphPipe+" ") {
			t.Fatalf("medium DiffView toolbar missing divider: %q", medPlain)
		}
		if !strings.Contains(medPlain, "/") && !strings.Contains(medPlain, "%") {
			t.Fatalf("medium DiffView toolbar dropped scroll indicator: %q", medPlain)
		}
	}

	// Verify 4-digit line count at 80x24 falls back cleanly to percentage ("0%") instead of dropping scroll status
	dvBig := NewDiffViewModel(43, 19)
	bigLines := make([]string, 1200)
	for i := range bigLines {
		bigLines[i] = fmt.Sprintf("+ set / system banner login-banner line-%d", i)
	}
	dvBig.SetDiff(models.SemanticDiffResult{HasChanges: true, AddedCount: 1200, CLIDiffLines: bigLines})
	bigToolbar, _ := dvBig.buildToolbar(43)
	bigPlain := stripPolishANSI(bigToolbar)
	if !strings.Contains(bigPlain, "%") && !strings.Contains(bigPlain, "/") {
		t.Fatalf("80x24 DiffView with 1200 lines dropped scroll indicator instead of falling back to %%: %q", bigPlain)
	}
}

// ============================================================================
// Fix 3: Commit A↔B Compare & VsLive context strip stats
// ============================================================================

func TestContextStripCompareAndVsLiveStats(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	baseCommit := &models.TimelineCommit{
		CommitID:  "df3b5eca",
		FullSHA:   "df3b5eca00000000000000000000000000000000",
		Author:    "root",
		DiffStat:  "+3 / ~3 / -4",
		Timestamp: time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC),
	}
	targetCommit := &models.TimelineCommit{
		CommitID:  "97fa5693",
		FullSHA:   "97fa569300000000000000000000000000000000",
		Author:    "root",
		DiffStat:  "+9", // single-commit stat that must NOT appear during A↔B or VsLive compare!
		Timestamp: time.Date(2026, 11, 24, 16, 30, 0, 0, time.UTC),
	}

	// 1. Commit A↔B compare displays DiffResult totals (+4 ~2 -1), NOT Commit B's "+9"
	dv := NewDiffViewModel(77, 30)
	dv.BaseCommit = baseCommit
	dv.Commit = targetCommit
	dv.SetDiff(models.SemanticDiffResult{
		HasChanges:    true,
		AddedCount:    4,
		ModifiedCount: 2,
		DeletedCount:  1,
		CLIDiffLines:  []string{"+ set / system name host-name spine-01"},
	})
	viewLines := strings.Split(stripPolishANSI(dv.View()), "\n")
	// Row 0 = top border, Row 1 = toolbar, Row 2 = context strip
	ctxLine := viewLines[2]
	for _, want := range []string{"BASE", "df3b5eca", "97fa5693", "+4", "~2", "-1", "root"} {
		if !strings.Contains(ctxLine, want) {
			t.Fatalf("A↔B context strip missing %q: %q", want, ctxLine)
		}
	}
	if strings.Contains(ctxLine, "+9") {
		t.Fatalf("A↔B context strip leaked single-commit stat +9 instead of comparison totals: %q", ctxLine)
	}

	// 2. Fallback deduplication when Added/Modified/Deleted counts are 0 and only Changes is populated
	dvDedup := NewDiffViewModel(77, 30)
	dvDedup.BaseCommit = baseCommit
	dvDedup.Commit = targetCommit
	dvDedup.SetDiff(models.SemanticDiffResult{
		HasChanges: true,
		Changes: []models.PathChange{
			// Redundant parent container + 2 child leaves -> should count as +2, not +3
			{Path: "/acl/acl-filter[name=in]", DiffType: models.DiffAdded, NewValue: map[string]interface{}{"entry": []interface{}{}}},
			{Path: "/acl/acl-filter[name=in]/entry[sequence-id=10]/action", DiffType: models.DiffAdded, NewValue: "accept"},
			{Path: "/acl/acl-filter[name=in]/entry[sequence-id=20]/action", DiffType: models.DiffAdded, NewValue: "drop"},
			{Path: "/system/name/host-name", DiffType: models.DiffModified, OldValue: "a", NewValue: "b"},
		},
		CLIDiffLines: []string{"+ set / acl"},
	})
	dedupCtx := strings.Split(stripPolishANSI(dvDedup.View()), "\n")[2]
	if !strings.Contains(dedupCtx, "+2") || !strings.Contains(dedupCtx, "~1") || strings.Contains(dedupCtx, "+3") {
		t.Fatalf("A↔B deduplicated leaf totals expected +2 ~1 (not +3): %q", dedupCtx)
	}

	// 3. VsLive compare displays actual DiffResult totals (+2 -3) next to ● RUNNING CONFIG
	dvLive := NewDiffViewModel(77, 30)
	dvLive.Commit = targetCommit
	dvLive.VsLive = true
	dvLive.SetDiff(models.SemanticDiffResult{
		HasChanges:   true,
		AddedCount:   2,
		DeletedCount: 3,
		CLIDiffLines: []string{"+ set / system banner login-banner hi"},
	})
	liveCtx := strings.Split(stripPolishANSI(dvLive.View()), "\n")[2]
	for _, want := range []string{"97fa5693 →", GlyphLive + " RUNNING CONFIG", "+2", "-3", "root"} {
		if !strings.Contains(liveCtx, want) {
			t.Fatalf("VsLive context strip missing %q: %q", want, liveCtx)
		}
	}
	if strings.Contains(liveCtx, "+9") {
		t.Fatalf("VsLive context strip leaked single-commit stat +9: %q", liveCtx)
	}

	// 4. VsLive with identical config shows ✓ Matches Running in context strip AND context-aware empty diff message
	dvIdentical := NewDiffViewModel(77, 30)
	dvIdentical.Commit = targetCommit
	dvIdentical.VsLive = true
	dvIdentical.SetDiff(models.SemanticDiffResult{HasChanges: false})
	identCtx := strings.Split(stripPolishANSI(dvIdentical.View()), "\n")[2]
	if !strings.Contains(identCtx, GlyphCheck+" Matches Running") {
		t.Fatalf("VsLive identical context strip expected '%s Matches Running', got: %q", GlyphCheck, identCtx)
	}
	identBody := stripPolishANSI(dvIdentical.Viewport.View())
	if !strings.Contains(identBody, GlyphCheck+" Revision 97fa5693 matches the live running configuration") {
		t.Fatalf("VsLive empty diff body missing context-aware message, got:\n%s", identBody)
	}

	// 5. Default vs previous commit shows "Prev <parentSHA> → <commitID>"
	dvPrev := NewDiffViewModel(77, 30)
	dvPrev.Commit = targetCommit
	dvPrev.ParentSHA = baseCommit.CommitID
	dvPrev.SetDiff(models.SemanticDiffResult{HasChanges: false})
	prevCtx := strings.Split(stripPolishANSI(dvPrev.View()), "\n")[2]
	for _, want := range []string{"Prev df3b5eca → 97fa5693", "root", "2026-11-24 16:30", "+9"} {
		if !strings.Contains(prevCtx, want) {
			t.Fatalf("Default Prev context strip missing %q: %q", want, prevCtx)
		}
	}
	prevBody := stripPolishANSI(dvPrev.Viewport.View())
	if !strings.Contains(prevBody, GlyphCheck+" No configuration changes vs previous commit") {
		t.Fatalf("Default empty diff body missing previous-commit message, got:\n%s", prevBody)
	}

	// 6. Initial/root commit (ParentSHA == "") shows "Initial → <commitID>"
	dvInit := NewDiffViewModel(77, 30)
	dvInit.Commit = baseCommit
	dvInit.ParentSHA = ""
	dvInit.SetDiff(models.SemanticDiffResult{HasChanges: true, AddedCount: 3, CLIDiffLines: []string{"+ set / system"}})
	initCtx := strings.Split(stripPolishANSI(dvInit.View()), "\n")[2]
	for _, want := range []string{"Initial → df3b5eca", "root", "+3", "~3", "-4"} {
		if !strings.Contains(initCtx, want) {
			t.Fatalf("Initial commit context strip missing %q: %q", want, initCtx)
		}
	}

	// 7. Base compare identical shows ✓ Identical in strip and context-aware empty diff message (with filter note)
	dvBaseSame := NewDiffViewModel(77, 30)
	dvBaseSame.BaseCommit = baseCommit
	dvBaseSame.Commit = targetCommit
	dvBaseSame.FilterQuery = "bgp"
	dvBaseSame.SetDiff(models.SemanticDiffResult{HasChanges: false})
	baseSameCtx := strings.Split(stripPolishANSI(dvBaseSame.View()), "\n")[2]
	if !strings.Contains(baseSameCtx, GlyphCheck+" Identical") {
		t.Fatalf("BASE identical context strip expected '%s Identical', got: %q", GlyphCheck, baseSameCtx)
	}
	baseSameBody := stripPolishANSI(dvBaseSame.Viewport.View())
	if !strings.Contains(baseSameBody, "No differences between BASE df3b5eca and 97fa5693 (filter: 'bgp')") {
		t.Fatalf("BASE empty diff body missing filter-aware message, got:\n%s", baseSameBody)
	}
}

// ============================================================================
// Fix 4: Timeline HEAD card stat visibility & unfocused Detail active clarity
// ============================================================================

func TestTimelineHeadCardStatVisibilityAt120x35(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	// Unit-test compactRelativeTime directly
	relCases := map[string]string{
		"just now": "now",
		"14s ago":  "14s",
		"27m ago":  "27m",
		"2h ago":   "2h",
		"5d ago":   "5d",
		"3mo ago":  "3mo",
		"1y ago":   "1y",
	}
	for in, want := range relCases {
		if got := compactRelativeTime(in); got != want {
			t.Fatalf("compactRelativeTime(%q) = %q, want %q", in, got, want)
		}
	}

	// Exact 120x35 timeline inner dimensions: timelineOuterW=40 -> timelineInnerW=38
	tl := NewTimelineViewModel(38, 30)
	tl.IsFocused = true
	tl.SetCommits([]models.TimelineCommit{
		{
			CommitID:     "df3b5eca",
			FullSHA:      "df3b5eca11111111111111111111111111111111",
			Timestamp:    time.Now().Add(-27 * time.Minute),
			Author:       "root",
			Message:      "Updated uplink ACL and BGP peers",
			DiffStat:     "+3 / ~3 / -4",
			IsCurrent:    true,
			SummaryScope: "acl, network-instance",
		},
		{
			CommitID:     "97fa5693",
			FullSHA:      "97fa569322222222222222222222222222222222",
			Timestamp:    time.Now().Add(-2 * time.Hour),
			Author:       "admin",
			Message:      "Initial baseline",
			DiffStat:     "+9",
			IsStartup:    true,
			SummaryScope: "system, interface",
		},
	})

	plainLines := strings.Split(stripPolishANSI(tl.Viewport.View()), "\n")
	if len(plainLines) < 2 {
		t.Fatalf("expected >=2 viewport lines, got %d", len(plainLines))
	}
	headLine := plainLines[0]
	for _, want := range []string{"df3b5eca", "HEAD", "root", "27m", "+3", "~3", "-4"} {
		if !strings.Contains(headLine, want) {
			t.Fatalf("120x35 Timeline HEAD card line 1 missing %q: %q", want, headLine)
		}
	}
	if strings.Contains(headLine, " ago") {
		t.Fatalf("Timeline HEAD card should strip redundant ' ago' suffix: %q", headLine)
	}
}

func TestUnfocusedDetailActiveTabAndPillDistinguishable(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	// 1. Top-border Detail tabs when unfocused: active tab must differ distinctly from inactive tabs
	//    (bold + underline + ColorText on ColorBadgeBg) while inactive stays dim without bold/underline,
	//    and must also differ from focused active (ColorPrimary).
	unfocusedTitle, unfocusChips := buildDetailTabLayout(int(TabDiff), false, false, 79)
	focusedTitle, focusChips := buildDetailTabLayout(int(TabDiff), true, false, 79)

	if len(unfocusChips) != len(focusChips) {
		t.Fatalf("focused vs unfocused tab chip counts differ: %d vs %d", len(focusChips), len(unfocusChips))
	}
	for i := range focusChips {
		if unfocusChips[i] != focusChips[i] {
			t.Fatalf("focused vs unfocused tab hit range[%d] shifted: %+v vs %+v", i, unfocusChips[i], focusChips[i])
		}
	}

	wantActiveUnfocused := lipgloss.NewStyle().Bold(true).Underline(true).Foreground(ColorText).Background(ColorBadgeBg).Render("Diff")
	wantInactiveUnfocused := lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorBackground).Render("Config")
	if !strings.Contains(unfocusedTitle, wantActiveUnfocused) {
		t.Fatalf("unfocused active tab should use bold+underline ColorText on ColorBadgeBg;\nwant substring: %q\ngot title:      %q",
			wantActiveUnfocused, unfocusedTitle)
	}
	if !strings.Contains(unfocusedTitle, wantInactiveUnfocused) {
		t.Fatalf("unfocused inactive tab should remain dim ColorTextDim;\nwant substring: %q\ngot title:      %q",
			wantInactiveUnfocused, unfocusedTitle)
	}
	if unfocusedTitle == focusedTitle {
		t.Fatalf("unfocused and focused Detail tab bars must be visually distinct")
	}

	// 2. Sub-mode toolbar pills when unfocused across Diff, Config, Blame:
	//    active pill uses bold+underline ColorText on ColorBadgeBg; inactive pills remain flat ColorTextDim on ColorSurface.
	//    Note: PaintBarLine re-injects base surface SGR after every inner \x1b[0m reset, so normalize
	//    expected pill snippets through PaintLine(..., 0, ColorSurface, ColorText) for exact comparison.
	normPill := func(styled string) string {
		p := PaintLine(styled, 0, ColorSurface, ColorText)
		return strings.TrimSuffix(p, "\x1b[0m")
	}

	dv := NewDiffViewModel(77, 30)
	dv.DiffMode = "cli"
	dv.IsFocused = false
	diffBarUnfocused, _ := dv.buildToolbar(77)

	wantActivePill := normPill(lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText).Bold(true).Underline(true).Render("[3] CLI"))
	wantInactivePill := normPill(lipgloss.NewStyle().Padding(0, 1).Background(ColorSurface).Foreground(ColorTextDim).Render("[1] JSON"))
	if !strings.Contains(diffBarUnfocused, wantActivePill) {
		t.Fatalf("unfocused DiffView active pill [3] CLI missing high-contrast active style;\nwant: %q\ngot:  %q",
			wantActivePill, diffBarUnfocused)
	}
	if !strings.Contains(diffBarUnfocused, wantInactivePill) {
		t.Fatalf("unfocused DiffView inactive pill [1] JSON missing dim style;\nwant: %q\ngot:  %q",
			wantInactivePill, diffBarUnfocused)
	}

	cv := NewConfigViewModel(77, 30)
	cv.FormatMode = "json"
	cv.IsFocused = false
	cfgBarUnfocused, _ := cv.buildToolbar(77)
	wantCfgActive := normPill(lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText).Bold(true).Underline(true).Render("[2] JSON"))
	if !strings.Contains(cfgBarUnfocused, wantCfgActive) {
		t.Fatalf("unfocused ConfigView active pill [2] JSON missing high-contrast active style")
	}

	bv := NewBlameViewModel(77, 30)
	bv.ViewMode = "analytics"
	bv.IsFocused = false
	blameBarUnfocused, _ := bv.buildToolbar(77)
	wantBlameActive := normPill(lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText).Bold(true).Underline(true).Render("[2] Analytics"))
	if !strings.Contains(blameBarUnfocused, wantBlameActive) {
		t.Fatalf("unfocused BlameView active pill [2] Analytics missing high-contrast active style")
	}
}

// ============================================================================
// 11-Theme ANSI Full-Bleed Safety across all polished views & sizes
// ============================================================================

func TestPolishedViewsFullBleedAcrossAll11Themes(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() {
		_, _ = SetTheme("dark")
	}()

	themes := AvailableThemes()
	if len(themes) != 11 {
		t.Fatalf("expected 11 built-in themes, got %d", len(themes))
	}

	baseCommit := &models.TimelineCommit{
		CommitID:  "df3b5eca",
		FullSHA:   "df3b5eca11111111111111111111111111111111",
		Timestamp: time.Now().Add(-27 * time.Minute),
		Author:    "root",
		Message:   "ACL and BGP policy update",
		DiffStat:  "+3 / ~3 / -4",
		IsCurrent: true,
	}
	targetCommit := &models.TimelineCommit{
		CommitID:  "97fa5693",
		FullSHA:   "97fa569322222222222222222222222222222222",
		Timestamp: time.Now().Add(-2 * time.Hour),
		Author:    "admin",
		Message:   "Initial baseline",
		DiffStat:  "+9",
		IsStartup: true,
	}
	pathDiff := models.SemanticDiffResult{
		HasChanges:    true,
		AddedCount:    2,
		ModifiedCount: 1,
		DeletedCount:  1,
		Changes: []models.PathChange{
			{Path: "/acl/acl-filter[name=uplink-in][type=ipv4]/entry[sequence-id=20]/action/drop", DiffType: models.DiffDeleted, OldValue: "true"},
			{Path: "/interface[name=ethernet-1/1]/subinterface[index=0]/ipv4/address[ip-prefix=10.10.1.2/30]", DiffType: models.DiffAdded, NewValue: "primary"},
			{Path: "/system/name/host-name", DiffType: models.DiffModified, OldValue: "leaf-1", NewValue: "leaf-2"},
		},
	}

	for _, th := range themes {
		_, ok := SetTheme(th.Name)
		if !ok {
			t.Fatalf("failed to set theme %q", th.Name)
		}

		// DiffView across all compare modes & sizes (40x12, 80x24, 120x35, 220x50)
		for _, sz := range []struct {
			label     string
			w, h      int
			focused   bool
			vsLive    bool
			useBase   bool
			parentSHA string
		}{
			{"40x12-prev-focused", 18, 7, true, false, false, baseCommit.CommitID},
			{"80x24-path-ab-unfocused", 43, 19, false, false, true, ""},
			{"80x24-prev-focused", 43, 19, true, false, false, baseCommit.CommitID},
			{"120x35-path-live-focused", 77, 30, true, true, false, ""},
			{"220x50-initial-focused", 167, 45, true, false, false, ""},
		} {
			dv := NewDiffViewModel(sz.w, sz.h)
			dv.DiffMode = "path"
			dv.IsFocused = sz.focused
			dv.VsLive = sz.vsLive
			dv.Commit = targetCommit
			dv.ParentSHA = sz.parentSHA
			if sz.useBase {
				dv.BaseCommit = baseCommit
			}
			dv.SetDiff(pathDiff)
			assertNoUnrecoveredReset(t, th.Name+"-diff-"+sz.label, dv.View(), sz.w+2, sz.h+2)
		}

		// ConfigView compact unfocused at 80x24
		cv := NewConfigViewModel(43, 19)
		cv.IsFocused = false
		cv.Commit = baseCommit
		cv.SetConfig(map[string]interface{}{"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-01"}}}, "")
		assertNoUnrecoveredReset(t, th.Name+"-config-80x24", cv.View(), 45, 21)

		// BlameView compact unfocused at 80x24
		bv := NewBlameViewModel(43, 19)
		bv.IsFocused = false
		bv.Commit = baseCommit
		bv.SetBlame([]models.BlameEntry{
			{LineNumber: 1, CommitSHA: baseCommit.FullSHA, Author: "root", Timestamp: baseCommit.Timestamp, Content: "set / system name host-name srl-01"},
		}, blame.ContributorStats{TotalLines: 1})
		assertNoUnrecoveredReset(t, th.Name+"-blame-80x24", bv.View(), 45, 21)

		// TimelineView at 120x35 inner width (38x30)
		tl := NewTimelineViewModel(38, 30)
		tl.IsFocused = true
		tl.SetCommits([]models.TimelineCommit{*baseCommit, *targetCommit})
		assertNoUnrecoveredReset(t, th.Name+"-timeline-120x35", tl.View(), 40, 32)
	}
}

// ============================================================================
// Decoupled Detail Filter Scope Banner & Matching-Line Cyan Gutter Markers
// ============================================================================

func TestFilterLineMatchesQuerySemantics(t *testing.T) {
	cases := []struct {
		line  string
		query string
		want  bool
	}{
		// Empty / header / pure punctuation never match
		{"", "bgp", false},
		{"set / interface ethernet-1/1 admin-state enable", "", false},
		{"--- a/config.json", "config", false},
		{"+++ b/config.json", "config", false},
		{"@@ -1,4 +1,5 @@", "1", false},
		{"  },", "bgp", false},
		{"  {", "bgp", false},
		// CLI & XPath matching with numeric-suffix safety
		{"+ set / interface ethernet-1/1 description uplink", "ethernet-1/1", true},
		{"+ set / interface ethernet-1/10 description downlink", "ethernet-1/1", false},
		{"+ set / interface ethernet-1/1 admin-state enable", "/interface[name=ethernet-1/1]", true},
		{"+ set / interface ethernet-1/10 admin-state enable", "/interface[name=ethernet-1/1]", false},
		{"set / network-instance default protocols bgp autonomous-system 65001", "bgp", true},
		// JSON key/value lines (including YANG module prefixes & IPv6 preservation)
		{`  "bgp": {`, "bgp", true},
		{`  "srl_nokia-network-instance:bgp": {`, "bgp", true},
		{`+ "name": "ethernet-1/1",`, "ethernet-1/1", true},
		{`+ "name": "ethernet-1/10",`, "ethernet-1/1", false},
		{`  "name": "ethernet-1/1",`, "/interface[name=ethernet-1/1]", true},
		{`  "name": "ethernet-1/10",`, "/interface[name=ethernet-1/1]", false},
		{`  "ip-prefix": "2001:db8::1/64",`, "2001:db8::1", true},
		{`  "autonomous-system": 65002,`, "65002", true},
	}

	for _, tc := range cases {
		if got := filter.LineMatchesQuery(tc.line, tc.query); got != tc.want {
			t.Errorf("LineMatchesQuery(%q, %q) = %v, want %v", tc.line, tc.query, got, tc.want)
		}
	}
	if !filter.JSONLineMatchesQuery(`  "srl_nokia-network-instance:bgp": {`, "bgp") {
		t.Fatalf("JSONLineMatchesQuery failed to match namespaced JSON key")
	}
	if filter.JSONLineMatchesQuery(`  },`, "bgp") {
		t.Fatalf("JSONLineMatchesQuery unexpectedly matched pure punctuation")
	}
	for _, hdr := range []struct{ line, q string }{
		{"--- a/config.json", "config"},
		{"+++ b/config.json", "config"},
		{"@@ -1,4 +1,5 @@", "1"},
	} {
		if filter.JSONLineMatchesQuery(hdr.line, hdr.q) {
			t.Fatalf("JSONLineMatchesQuery(%q, %q) unexpectedly matched diff header", hdr.line, hdr.q)
		}
	}
}

func TestDetailFilterBannerHitRangesAndZeroOverheadAcrossViews(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	commit := &models.TimelineCommit{
		CommitID:  "df3b5eca",
		FullSHA:   "df3b5eca11111111111111111111111111111111",
		Author:    "root",
		Timestamp: time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC),
	}
	sampleCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable", "description": "uplink"},
			map[string]interface{}{"name": "ethernet-1/10", "admin-state": "enable", "description": "downlink"},
		},
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf-01"},
		},
	}
	sampleBlame := []models.BlameEntry{
		{LineNumber: 1, CommitSHA: commit.FullSHA, Author: "root", Timestamp: commit.Timestamp, Content: "set / interface ethernet-1/1 admin-state enable"},
		{LineNumber: 2, CommitSHA: commit.FullSHA, Author: "admin", Timestamp: commit.Timestamp, Content: "set / interface ethernet-1/10 admin-state enable"},
		{LineNumber: 3, CommitSHA: commit.FullSHA, Author: "root", Timestamp: commit.Timestamp, Content: "set / network-instance default protocols bgp autonomous-system 65001"},
	}

	assertBannerPill := func(t *testing.T, label, renderedView string, innerW, bannerRow, sx, ex int, wantPill string, wantSubstrings []string) {
		t.Helper()
		lines := strings.Split(stripPolishANSI(renderedView), "\n")
		outerRow := 1 + bannerRow // row 0 is top box border
		if outerRow < 1 || outerRow >= len(lines)-1 {
			t.Fatalf("[%s] outerRow=%d out of bounds (len=%d)", label, outerRow, len(lines))
		}
		rowRunes := []rune(lines[outerRow])
		innerRunes := rowRunes[1 : len(rowRunes)-1]
		innerPlain := string(innerRunes)
		if gotW := lipgloss.Width(innerPlain); gotW != innerW {
			t.Fatalf("[%s] banner inner width=%d, want %d (%q)", label, gotW, innerW, innerPlain)
		}
		if sx < 0 || ex <= sx || ex > len(innerRunes) {
			t.Fatalf("[%s] invalid hit range [%d:%d] for inner len %d (%q)", label, sx, ex, len(innerRunes), innerPlain)
		}
		if got := string(innerRunes[sx:ex]); got != wantPill {
			t.Fatalf("[%s] pill slice [%d:%d] = %q, want %q (banner=%q)", label, sx, ex, got, wantPill, innerPlain)
		}
		for _, sub := range wantSubstrings {
			if !strings.Contains(innerPlain, sub) {
				t.Fatalf("[%s] banner missing %q: %q", label, sub, innerPlain)
			}
		}
	}

	// --- 1. DiffView: zero overhead when unfiltered, scoped & full banners at wide (93) and narrow (43) ---
	dv := NewDiffViewModel(93, 31)
	dv.Commit = commit
	dv.SetDiff(models.SemanticDiffResult{
		HasChanges:   true,
		AddedCount:   2,
		CLIDiffLines: []string{"+ set / interface ethernet-1/1 admin-state enable", "+ set / interface ethernet-1/1 description uplink"},
	})
	if dv.HeaderRows() != 2 || dv.FilterBannerRow() != -1 {
		t.Fatalf("DiffView unfiltered expected HeaderRows=2, FilterBannerRow=-1; got %d, %d", dv.HeaderRows(), dv.FilterBannerRow())
	}
	if sx, ex := dv.FilterBannerHitRange(); sx != -1 || ex != -1 {
		t.Fatalf("DiffView unfiltered expected hit range (-1,-1), got (%d,%d)", sx, ex)
	}

	dv.FilterQuery = "ethernet-1/1"
	dv.TotalUnfilteredChanges = 7
	dv.DetailFilterBypass = false
	dv.UpdateContent()
	if dv.HeaderRows() != 3 || dv.FilterBannerRow() != 2 {
		t.Fatalf("DiffView filtered expected HeaderRows=3, FilterBannerRow=2; got %d, %d", dv.HeaderRows(), dv.FilterBannerRow())
	}
	sx, ex := dv.FilterBannerHitRange()
	assertBannerPill(t, "DiffView-wide-scoped", dv.View(), 93, dv.FilterBannerRow(), sx, ex,
		`[\ Show Full Commit]`,
		[]string{GlyphMilestone + " FILTERED", "'ethernet-1/1'", "Showing 2 of 7 changes", "(5 hidden)"})

	dv.DetailFilterBypass = true
	dv.UpdateContent()
	sx, ex = dv.FilterBannerHitRange()
	assertBannerPill(t, "DiffView-wide-full", dv.View(), 93, dv.FilterBannerRow(), sx, ex,
		`[\ Re-scope 'ethernet-1/1']`,
		[]string{GlyphLive + " FULL", "all 7 changes", "Timeline filtered by 'ethernet-1/1'"})

	// Narrow 80x24 (innerW=43) compact pills
	dvNarrow := NewDiffViewModel(43, 19)
	dvNarrow.Commit = commit
	dvNarrow.FilterQuery = "bgp"
	dvNarrow.TotalUnfilteredChanges = 9
	dvNarrow.SetDiff(models.SemanticDiffResult{HasChanges: true, AddedCount: 2, CLIDiffLines: []string{"+ set / bgp a", "+ set / bgp b"}})
	sx, ex = dvNarrow.FilterBannerHitRange()
	assertBannerPill(t, "DiffView-narrow-scoped", dvNarrow.View(), 43, dvNarrow.FilterBannerRow(), sx, ex,
		`[\ Full]`, []string{"FILTERED", "2/9"})

	dvNarrow.DetailFilterBypass = true
	dvNarrow.UpdateContent()
	sx, ex = dvNarrow.FilterBannerHitRange()
	assertBannerPill(t, "DiffView-narrow-full", dvNarrow.View(), 43, dvNarrow.FilterBannerRow(), sx, ex,
		`[\ Scope]`, []string{GlyphLive + " FULL"})

	// --- 2. ConfigView: auto ShownLineCount / TotalUnfilteredLines in CLI & JSON + exact banners ---
	cv := NewConfigViewModel(93, 31)
	cv.Commit = commit
	cv.SetConfig(sampleCfg, "")
	if cv.HeaderRows() != 2 || cv.FilterBannerRow() != -1 {
		t.Fatalf("ConfigView unfiltered expected HeaderRows=2, FilterBannerRow=-1; got %d, %d", cv.HeaderRows(), cv.FilterBannerRow())
	}

	cv.SetConfig(sampleCfg, "ethernet-1/1")
	if cv.TotalUnfilteredLines <= cv.ShownLineCount || cv.ShownLineCount == 0 {
		t.Fatalf("ConfigView CLI scoped expected 0 < Shown (%d) < Total (%d)", cv.ShownLineCount, cv.TotalUnfilteredLines)
	}
	if cv.HeaderRows() != 3 || cv.FilterBannerRow() != 2 {
		t.Fatalf("ConfigView filtered expected HeaderRows=3, FilterBannerRow=2; got %d, %d", cv.HeaderRows(), cv.FilterBannerRow())
	}
	sx, ex = cv.FilterBannerHitRange()
	assertBannerPill(t, "ConfigView-CLI-scoped", cv.View(), 93, cv.FilterBannerRow(), sx, ex,
		`[\ Show Full Config]`,
		[]string{GlyphMilestone + " FILTERED", "'ethernet-1/1'", fmt.Sprintf("Showing %d of %d lines", cv.ShownLineCount, cv.TotalUnfilteredLines), "hidden)"})

	// Switch to Full Config (bypass)
	cv.SetConfigScoped(sampleCfg, "ethernet-1/1", true)
	if cv.ShownLineCount != cv.TotalUnfilteredLines {
		t.Fatalf("ConfigView CLI bypass expected Shown (%d) == Total (%d)", cv.ShownLineCount, cv.TotalUnfilteredLines)
	}
	sx, ex = cv.FilterBannerHitRange()
	assertBannerPill(t, "ConfigView-CLI-full", cv.View(), 93, cv.FilterBannerRow(), sx, ex,
		`[\ Re-scope 'ethernet-1/1']`,
		[]string{GlyphLive + " FULL", fmt.Sprintf("all %d lines", cv.TotalUnfilteredLines), "Timeline filtered by 'ethernet-1/1'"})

	// JSON mode scoped count check
	cv.FormatMode = "json"
	cv.SetConfigScoped(sampleCfg, "ethernet-1/1", false)
	if cv.TotalUnfilteredLines <= cv.ShownLineCount || cv.ShownLineCount == 0 {
		t.Fatalf("ConfigView JSON scoped expected 0 < Shown (%d) < Total (%d)", cv.ShownLineCount, cv.TotalUnfilteredLines)
	}

	// --- 3. BlameView: zero overhead when unfiltered, banner above pinned header when filtered ---
	bv := NewBlameViewModel(93, 31)
	bv.Commit = commit
	bv.SetBlame(sampleBlame, blame.ContributorStats{TotalLines: 3})
	if bv.HeaderRows() != 3 || bv.FilterBannerRow() != -1 {
		t.Fatalf("BlameView unfiltered expected HeaderRows=3 (toolbar+ctx+pinned), FilterBannerRow=-1; got %d, %d", bv.HeaderRows(), bv.FilterBannerRow())
	}

	bv.FilterQuery = "bgp"
	bv.DetailFilterBypass = false
	bv.TotalUnfilteredEntries = 3
	bv.SetBlame(sampleBlame[2:], blame.ContributorStats{TotalLines: 3})
	if bv.HeaderRows() != 4 || bv.FilterBannerRow() != 2 {
		t.Fatalf("BlameView filtered expected HeaderRows=4, FilterBannerRow=2; got %d, %d", bv.HeaderRows(), bv.FilterBannerRow())
	}
	sx, ex = bv.FilterBannerHitRange()
	assertBannerPill(t, "BlameView-scoped", bv.View(), 93, bv.FilterBannerRow(), sx, ex,
		`[\ Show Full Blame]`,
		[]string{GlyphMilestone + " FILTERED", "'bgp'", "Showing 1 of 3 lines", "(2 hidden)"})

	bv.DetailFilterBypass = true
	bv.SetBlame(sampleBlame, blame.ContributorStats{TotalLines: 3})
	sx, ex = bv.FilterBannerHitRange()
	assertBannerPill(t, "BlameView-full", bv.View(), 93, bv.FilterBannerRow(), sx, ex,
		`[\ Re-scope 'bgp']`,
		[]string{GlyphLive + " FULL", "all 3 lines", "Timeline filtered by 'bgp'"})
}

func TestDetailViewsCyanMatchingLineGutterMarkers(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	// 1. DiffView single-pane CLI, Path, Unified JSON modes
	dv := NewDiffViewModel(93, 31)
	dv.FilterQuery = "ethernet-1/1"
	dv.DetailFilterBypass = true
	diffRes := models.SemanticDiffResult{
		HasChanges: true,
		AddedCount: 3,
		CLIDiffLines: []string{
			"+ set / interface ethernet-1/1 description uplink",
			"+ set / interface ethernet-1/10 description downlink",
			"+ set / system name host-name leaf-01",
		},
		Changes: []models.PathChange{
			{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffAdded, NewValue: "uplink"},
			{Path: "/interface[name=ethernet-1/10]/description", DiffType: models.DiffAdded, NewValue: "downlink"},
			{Path: "/system/name/host-name", DiffType: models.DiffAdded, NewValue: "leaf-01"},
		},
		UnifiedDiffLines: []string{
			"--- a/config.json",
			"+++ b/config.json",
			"@@ -1,2 +1,4 @@",
			`+   "name": "ethernet-1/1",`,
			`+   "name": "ethernet-1/10",`,
			`    "host-name": "leaf-01"`,
		},
	}

	for _, mode := range []string{"cli", "path", "unified"} {
		dv.DiffMode = mode
		dv.SetDiff(diffRes)
		plainLines := strings.Split(stripPolishANSI(strings.Join(dv.renderedLines, "\n")), "\n")
		var matchedHit, nonMatchedHit bool
		for _, l := range plainLines {
			if strings.Contains(l, "ethernet-1/1") && !strings.Contains(l, "ethernet-1/10") {
				if strings.Contains(l, GlyphCollapsed) {
					matchedHit = true
				}
			}
			if strings.Contains(l, "ethernet-1/10") || strings.Contains(l, "host-name") || strings.Contains(l, "@@") || strings.Contains(l, "--- a/") {
				if strings.Contains(l, GlyphCollapsed) {
					t.Fatalf("DiffView(%s) non-matching line unexpectedly got %q marker: %q", mode, GlyphCollapsed, l)
				}
				nonMatchedHit = true
			}
		}
		if !matchedHit || !nonMatchedHit {
			t.Fatalf("DiffView(%s) expected matching marker (%v) and unmarked non-match (%v) in:\n%s",
				mode, matchedHit, nonMatchedHit, strings.Join(plainLines, "\n"))
		}
	}

	// 2. ConfigView CLI & JSON modes
	cfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "uplink"},
			map[string]interface{}{"name": "ethernet-1/10", "description": "downlink"},
		},
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "leaf-01"}},
	}
	for _, fmtMode := range []string{"cli", "json"} {
		cv := NewConfigViewModel(93, 31)
		cv.FormatMode = fmtMode
		cv.SetConfigScoped(cfg, "ethernet-1/1", true)
		plainLines := strings.Split(stripPolishANSI(strings.Join(cv.renderedLines, "\n")), "\n")
		var matchedHit, nonMatchedHit bool
		for _, l := range plainLines {
			if strings.Contains(l, "ethernet-1/1") && !strings.Contains(l, "ethernet-1/10") {
				if strings.Contains(l, GlyphCollapsed) {
					matchedHit = true
				}
			}
			if strings.Contains(l, "ethernet-1/10") || strings.Contains(l, "leaf-01") {
				if strings.Contains(l, GlyphCollapsed) {
					t.Fatalf("ConfigView(%s) non-matching line unexpectedly got %q marker: %q", fmtMode, GlyphCollapsed, l)
				}
				nonMatchedHit = true
			}
		}
		if !matchedHit || !nonMatchedHit {
			t.Fatalf("ConfigView(%s) expected matching marker (%v) and unmarked non-match (%v) in:\n%s",
				fmtMode, matchedHit, nonMatchedHit, strings.Join(plainLines, "\n"))
		}
	}

	// 3. BlameView Attribution mode + dirty check on DetailFilterBypass flip
	bv := NewBlameViewModel(93, 31)
	bv.FilterQuery = "bgp"
	bv.DetailFilterBypass = false
	bv.SetBlame([]models.BlameEntry{
		{LineNumber: 1, CommitSHA: "df3b5eca11111111", Author: "root", Timestamp: time.Now(), Content: "set / interface ethernet-1/1 admin-state enable"},
		{LineNumber: 2, CommitSHA: "df3b5eca11111111", Author: "admin", Timestamp: time.Now(), Content: "set / network-instance default protocols bgp autonomous-system 65001"},
	}, blame.ContributorStats{TotalLines: 2})
	// Flip DetailFilterBypass directly without calling UpdateContent() — View() dirty check must re-render!
	bv.DetailFilterBypass = true
	rendered := stripPolishANSI(bv.View())
	var bgpMarked, ifaceUnmarked bool
	for _, l := range strings.Split(rendered, "\n") {
		if strings.Contains(l, "protocols bgp") && strings.Contains(l, GlyphCollapsed) {
			bgpMarked = true
		}
		if strings.Contains(l, "ethernet-1/1") {
			if strings.Contains(l, GlyphCollapsed) {
				t.Fatalf("BlameView non-matching line unexpectedly got %q marker: %q", GlyphCollapsed, l)
			}
			ifaceUnmarked = true
		}
	}
	if !bgpMarked || !ifaceUnmarked {
		t.Fatalf("BlameView bypass View() expected bgpMarked=%v ifaceUnmarked=%v in:\n%s", bgpMarked, ifaceUnmarked, rendered)
	}
}

func TestDetailFilterBannersFullBleedAcrossAll11Themes(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() {
		_, _ = SetTheme("dark")
	}()

	commit := &models.TimelineCommit{
		CommitID:  "df3b5eca",
		FullSHA:   "df3b5eca11111111111111111111111111111111",
		Author:    "root",
		Timestamp: time.Now().Add(-20 * time.Minute),
	}
	cfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable"},
			map[string]interface{}{"name": "ethernet-1/10", "admin-state": "enable"},
		},
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "leaf-01"}},
	}
	diffRes := models.SemanticDiffResult{
		HasChanges: true,
		AddedCount: 2,
		CLIDiffLines: []string{
			"+ set / interface ethernet-1/1 admin-state enable",
			"+ set / system name host-name leaf-01",
		},
	}
	blameEntries := []models.BlameEntry{
		{LineNumber: 1, CommitSHA: commit.FullSHA, Author: "root", Timestamp: commit.Timestamp, Content: "set / interface ethernet-1/1 admin-state enable"},
		{LineNumber: 2, CommitSHA: commit.FullSHA, Author: "admin", Timestamp: commit.Timestamp, Content: "set / system name host-name leaf-01"},
	}

	for _, th := range AvailableThemes() {
		_, ok := SetTheme(th.Name)
		if !ok {
			t.Fatalf("failed to set theme %q", th.Name)
		}
		for _, bypass := range []bool{false, true} {
			for _, sz := range [][2]int{{43, 19}, {93, 31}} {
				w, h := sz[0], sz[1]

				dv := NewDiffViewModel(w, h)
				dv.IsFocused = true
				dv.Commit = commit
				dv.FilterQuery = "ethernet-1/1"
				dv.DetailFilterBypass = bypass
				dv.TotalUnfilteredChanges = 5
				dv.SetDiff(diffRes)
				assertNoUnrecoveredReset(t, fmt.Sprintf("%s-diff-banner-bypass%v-%dx%d", th.Name, bypass, w, h), dv.View(), w+2, h+2)

				cv := NewConfigViewModel(w, h)
				cv.IsFocused = true
				cv.Commit = commit
				cv.SetConfigScoped(cfg, "ethernet-1/1", bypass)
				assertNoUnrecoveredReset(t, fmt.Sprintf("%s-config-banner-bypass%v-%dx%d", th.Name, bypass, w, h), cv.View(), w+2, h+2)

				bv := NewBlameViewModel(w, h)
				bv.IsFocused = true
				bv.Commit = commit
				bv.FilterQuery = "ethernet-1/1"
				bv.DetailFilterBypass = bypass
				bv.TotalUnfilteredEntries = 5
				bv.SetBlame(blameEntries, blame.ContributorStats{TotalLines: 5})
				assertNoUnrecoveredReset(t, fmt.Sprintf("%s-blame-banner-bypass%v-%dx%d", th.Name, bypass, w, h), bv.View(), w+2, h+2)
			}
		}
	}
}
