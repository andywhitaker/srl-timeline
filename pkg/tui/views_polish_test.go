package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/blame"
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
					"unified": "[1] Unified",
					"path":    "[2] Path",
					"cli":     "[3] CLI",
					"live":    "[4] Live",
				}
				if w.compact {
					expected = map[string]string{
						"unified": "1:Uni",
						"path":    "2:Path",
						"cli":     "3:CLI",
						"live":    "4:Live",
					}
				}
				if vsLive {
					expected["live"] = GlyphLive + " Live"
				}
				verifyToolbarHitAlignment(t, fmt.Sprintf("DiffView(vsLive=%v)", vsLive), toolbar, hitRanges, expected, w.innerW)

				// Specifically at 80x24 (innerW=43): ALL 4 pills + scroll indicator must fit completely unclipped!
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
					// Scroll progress indicator (e.g. "18/27" or "%") must be present
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

	// 3. VsLive compare displays actual DiffResult totals (+2 -3) next to ● LIVE
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
	for _, want := range []string{"97fa5693", GlyphLive + " LIVE", "+2", "-3", "root"} {
		if !strings.Contains(liveCtx, want) {
			t.Fatalf("VsLive context strip missing %q: %q", want, liveCtx)
		}
	}
	if strings.Contains(liveCtx, "+9") {
		t.Fatalf("VsLive context strip leaked single-commit stat +9: %q", liveCtx)
	}

	// 4. VsLive with identical config shows ✓ Identical
	dvIdentical := NewDiffViewModel(77, 30)
	dvIdentical.Commit = targetCommit
	dvIdentical.VsLive = true
	dvIdentical.SetDiff(models.SemanticDiffResult{HasChanges: false})
	identCtx := strings.Split(stripPolishANSI(dvIdentical.View()), "\n")[2]
	if !strings.Contains(identCtx, GlyphCheck+" Identical") {
		t.Fatalf("VsLive identical context strip expected '%s Identical', got: %q", GlyphCheck, identCtx)
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
	wantInactivePill := normPill(lipgloss.NewStyle().Padding(0, 1).Background(ColorSurface).Foreground(ColorTextDim).Render("[1] Unified"))
	if !strings.Contains(diffBarUnfocused, wantActivePill) {
		t.Fatalf("unfocused DiffView active pill [3] CLI missing high-contrast active style;\nwant: %q\ngot:  %q",
			wantActivePill, diffBarUnfocused)
	}
	if !strings.Contains(diffBarUnfocused, wantInactivePill) {
		t.Fatalf("unfocused DiffView inactive pill [1] Unified missing dim style;\nwant: %q\ngot:  %q",
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

		// DiffView Path mode A↔B compare at 80x24 (compact toolbar, unfocused) and 120x35 (full toolbar, focused)
		for _, sz := range []struct {
			label   string
			w, h    int
			focused bool
			vsLive  bool
		}{
			{"80x24-path-ab-unfocused", 43, 19, false, false},
			{"120x35-path-live-focused", 77, 30, true, true},
		} {
			dv := NewDiffViewModel(sz.w, sz.h)
			dv.DiffMode = "path"
			dv.IsFocused = sz.focused
			dv.VsLive = sz.vsLive
			dv.Commit = targetCommit
			if !sz.vsLive {
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
