package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/muesli/ansi"

	"timeline/pkg/models"
)

func makeRibbonTestCommits(base time.Time) []models.TimelineCommit {
	// Newest-first (matching GitBackend.GetTimeline) spanning 10 days.
	return []models.TimelineCommit{
		{
			CommitID:     "c0000001",
			FullSHA:      "c0000001aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Timestamp:    base,
			Author:       "admin",
			Message:      "Enable BGP ipv4-unicast peer group",
			DiffStat:     "+18 / ~2 / -1",
			SummaryScope: "/network-instance/protocols/bgp/afi-safi/ipv4-unicast",
			IsCurrent:    true,
		},
		{
			CommitID:     "c0000002",
			FullSHA:      "c0000002bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Timestamp:    base.Add(-20 * time.Minute),
			Author:       "netops",
			Message:      "Restore baseline ipv6-unicast policy",
			DiffStat:     "+4 / ~10 / -2",
			SummaryScope: "/network-instance/protocols/bgp/afi-safi/ipv6-unicast",
			IsRestored:   true,
		},
		{
			CommitID:     "c0000003",
			FullSHA:      "c0000003cccccccccccccccccccccccccccccccc",
			Timestamp:    base.Add(-45 * time.Minute),
			Author:       "admin",
			Message:      "Remove stale ACL entry",
			DiffStat:     "-25",
			SummaryScope: "/acl/ipv4-filter",
		},
		{
			CommitID:     "c0000004",
			FullSHA:      "c0000004dddddddddddddddddddddddddddddddd",
			Timestamp:    base.Add(-6 * time.Hour),
			Author:       "admin",
			Message:      "Modify ethernet-1/1 MTU",
			DiffStat:     "~6",
			SummaryScope: "/interface/ethernet-1/1",
		},
		{
			CommitID:     "c0000005",
			FullSHA:      "c0000005eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			Timestamp:    base.Add(-3 * 24 * time.Hour),
			Author:       "netops",
			Message:      "Add uplink subinterface",
			DiffStat:     "+42 / ~3",
			SummaryScope: "/interface/ethernet-1/2",
		},
		{
			CommitID:     "c0000006",
			FullSHA:      "c0000006ffffffffffffffffffffffffffffffff",
			Timestamp:    base.Add(-10 * 24 * time.Hour),
			Author:       "system",
			Message:      "Initial startup configuration",
			DiffStat:     "+120",
			IsStartup:    true,
		},
	}
}

func TestTimelineRibbonEmptySingleManyAndClustered(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	// 1. Zero commits
	m := NewTimelineRibbonModel(120)
	m.SetCommits(nil, 0, "")
	if m.OuterHeight() != 6 {
		t.Fatalf("expected OuterHeight 6, got %d", m.OuterHeight())
	}
	if len(m.WindowCommits) != 0 {
		t.Fatalf("expected 0 WindowCommits, got %d", len(m.WindowCommits))
	}
	out := m.View()
	assertNoUnrecoveredReset(t, "ribbon-0-commits", out, 120, 6)

	// 2. Single commit
	now := time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC)
	single := []models.TimelineCommit{
		{
			CommitID:  "1111aaaa",
			FullSHA:   "1111aaaa00000000000000000000000000000000",
			Timestamp: now,
			Author:    "admin",
			DiffStat:  "+10",
			IsCurrent: true,
			IsStartup: true,
		},
	}
	m.SetCommits(single, 1, "")
	m.SelectedSHA = single[0].FullSHA
	if len(m.WindowCommits) != 1 {
		t.Fatalf("expected 1 WindowCommit, got %d", len(m.WindowCommits))
	}
	if !m.FullMaxTime.After(m.FullMinTime) {
		t.Fatalf("expected padded time bounds for single commit, got %v..%v", m.FullMinTime, m.FullMaxTime)
	}
	outSingle := m.View()
	assertNoUnrecoveredReset(t, "ribbon-1-commit", outSingle, 120, 6)
	plainSingle := stripANSI(outSingle)
	if !strings.Contains(plainSingle, "HEAD") {
		t.Errorf("expected HEAD badge on single commit ribbon:\n%s", plainSingle)
	}

	// 3. Many commits with A/B split compare span
	commits := makeRibbonTestCommits(now)
	m.SetCommits(commits, len(commits), "")
	m.HeadSHA = commits[0].FullSHA
	m.SelectedSHA = commits[1].FullSHA
	m.SplitCompare = true
	m.LeftSHA = commits[4].FullSHA
	m.RightSHA = commits[0].FullSHA
	m.IsFocused = true

	outMany := m.View()
	assertNoUnrecoveredReset(t, "ribbon-many-commits", outMany, 120, 6)
	plainMany := stripANSI(outMany)
	if !strings.Contains(plainMany, "A") || !strings.Contains(plainMany, "B") {
		t.Errorf("expected A and B nodes in split compare view:\n%s", plainMany)
	}
	if !strings.Contains(plainMany, "━") {
		t.Errorf("expected highlighted span rail ━ in split compare view:\n%s", plainMany)
	}
	if !strings.Contains(plainMany, "◄") || !strings.Contains(plainMany, "►") {
		t.Errorf("expected overview scrubber handles ◄ and ►:\n%s", plainMany)
	}

	// 4. Clustered commits landing on the same column
	clustered := make([]models.TimelineCommit, 0, 12)
	for i := 0; i < 6; i++ {
		clustered = append(clustered, models.TimelineCommit{
			CommitID:  fmt.Sprintf("cl%06d", i),
			FullSHA:   fmt.Sprintf("cl%06d00000000000000000000000000000000", i),
			Timestamp: now.Add(-time.Duration(i) * time.Second), // all clustered at right edge
			DiffStat:  fmt.Sprintf("+%d", (i+1)*3),
			IsCurrent: i == 0,
		})
	}
	clustered = append(clustered, models.TimelineCommit{
		CommitID:  "old00001",
		FullSHA:   "old0000100000000000000000000000000000000",
		Timestamp: now.Add(-48 * time.Hour),
		DiffStat:  "+5",
		IsStartup: true,
	})

	mc := NewTimelineRibbonModel(120)
	mc.SetCommits(clustered, len(clustered), "")
	mc.SelectedSHA = clustered[0].FullSHA
	outCluster := mc.View()
	assertNoUnrecoveredReset(t, "ribbon-clustered", outCluster, 120, 6)
	plainCluster := stripANSI(outCluster)
	if !strings.Contains(plainCluster, "×6") {
		t.Errorf("expected ×6 cluster badge when 6 commits collapse into 1 column:\n%s", plainCluster)
	}

	// 5. Distinct A, B, and Selected commits collapsing into the SAME column must each render their own SHA.
	mc.SplitCompare = true
	mc.LeftSHA = clustered[4].FullSHA    // cl000004
	mc.RightSHA = clustered[2].FullSHA   // cl000002
	mc.SelectedSHA = clustered[1].FullSHA // cl000001
	outClusterAB := mc.View()
	assertNoUnrecoveredReset(t, "ribbon-clustered-ab-sel", outClusterAB, 120, 6)
	plainClusterAB := stripANSI(outClusterAB)
	if !strings.Contains(plainClusterAB, "A:cl000004") {
		t.Errorf("expected A:cl000004 badge in clustered column, got:\n%s", plainClusterAB)
	}
	if !strings.Contains(plainClusterAB, "B:cl000002") {
		t.Errorf("expected B:cl000002 badge in clustered column, got:\n%s", plainClusterAB)
	}
	if !strings.Contains(plainClusterAB, "cl000001") {
		t.Errorf("expected selected cl000001 badge in clustered column, got:\n%s", plainClusterAB)
	}
}

func TestTimelineRibbonPresetsAndWindowManipulation(t *testing.T) {
	now := time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC)
	commits := makeRibbonTestCommits(now)

	m := NewTimelineRibbonModel(120)
	m.SetCommits(commits, len(commits), "")

	// Default preset is "all"
	if m.ActivePreset != "all" {
		t.Fatalf("expected default preset 'all', got %q", m.ActivePreset)
	}
	if len(m.WindowCommits) != 6 {
		t.Fatalf("expected 6 commits under 'all', got %d", len(m.WindowCommits))
	}

	// Preset "7d" excludes the 10-day-old startup commit
	m.SetPreset("7d")
	if m.ActivePreset != "7d" || len(m.WindowCommits) != 5 {
		t.Fatalf("expected 5 commits under '7d', got %d (preset=%q)", len(m.WindowCommits), m.ActivePreset)
	}

	// Preset "24h" includes 0m, -20m, -45m, -6h (4 commits)
	m.SetPreset("24h")
	if m.ActivePreset != "24h" || len(m.WindowCommits) != 4 {
		t.Fatalf("expected 4 commits under '24h', got %d", len(m.WindowCommits))
	}

	// Preset "1h" includes 0m, -20m, -45m (3 commits)
	m.SetPreset("1h")
	if m.ActivePreset != "1h" || len(m.WindowCommits) != 3 {
		t.Fatalf("expected 3 commits under '1h', got %d", len(m.WindowCommits))
	}

	// FitToMatches adds 3% padding and includes all filtered commits
	m.FitToMatches()
	if m.ActivePreset != "fit" || len(m.WindowCommits) != 6 {
		t.Fatalf("expected 6 commits under FitToMatches, got %d (preset=%q)", len(m.WindowCommits), m.ActivePreset)
	}
	if !m.WindowStart.Before(m.FullMinTime) || !m.WindowEnd.After(m.FullMaxTime) {
		t.Errorf("expected FitToMatches to add padding around FullMinTime..FullMaxTime")
	}

	// ResetWindow returns to "all"
	m.ResetWindow()
	if m.ActivePreset != "all" || !m.WindowStart.Equal(m.FullMinTime) || !m.WindowEnd.Equal(m.FullMaxTime) {
		t.Fatalf("ResetWindow did not restore full bounds")
	}

	// NudgeStart positive moves start right (narrowing window)
	origStart := m.WindowStart
	m.NudgeStart(0.25)
	if !m.WindowStart.After(origStart) || m.ActivePreset != "" {
		t.Fatalf("NudgeStart(+0.25) should move WindowStart right and switch to custom preset")
	}

	// NudgeEnd negative moves end left (narrowing window)
	origEnd := m.WindowEnd
	m.NudgeEnd(-0.25)
	if !m.WindowEnd.Before(origEnd) {
		t.Fatalf("NudgeEnd(-0.25) should move WindowEnd left")
	}

	// PanWindow preserves window span
	spanBeforePan := m.WindowEnd.Sub(m.WindowStart)
	startBeforePan := m.WindowStart
	m.PanWindow(-0.2)
	if !m.WindowStart.Before(startBeforePan) {
		t.Fatalf("PanWindow(-0.2) should shift earlier")
	}
	if diff := m.WindowEnd.Sub(m.WindowStart) - spanBeforePan; diff < -time.Second || diff > time.Second {
		t.Fatalf("PanWindow did not preserve span: before=%v after=%v", spanBeforePan, m.WindowEnd.Sub(m.WindowStart))
	}

	// ZoomAround factor < 1 zooms in, > 1 zooms out
	center := m.WindowStart.Add(m.WindowEnd.Sub(m.WindowStart) / 2)
	spanBeforeZoom := m.WindowEnd.Sub(m.WindowStart)
	m.ZoomAround(center, 0.5)
	spanZoomedIn := m.WindowEnd.Sub(m.WindowStart)
	if spanZoomedIn >= spanBeforeZoom {
		t.Fatalf("ZoomAround(0.5) should narrow span: before=%v after=%v", spanBeforeZoom, spanZoomedIn)
	}
	m.ZoomAround(center, 2.0)
	spanZoomedOut := m.WindowEnd.Sub(m.WindowStart)
	if spanZoomedOut <= spanZoomedIn {
		t.Fatalf("ZoomAround(2.0) should widen span: in=%v out=%v", spanZoomedIn, spanZoomedOut)
	}

	// Preserving custom window across SetCommits
	customStart, customEnd := m.WindowStart, m.WindowEnd
	m.SetCommits(commits, len(commits), "")
	if m.ActivePreset != "" || !m.WindowStart.Equal(customStart) || !m.WindowEnd.Equal(customEnd) {
		t.Fatalf("expected custom window to be preserved on SetCommits")
	}

	// Chronological left/right navigation within WindowCommits
	m.ResetWindow()
	m.SelectedSHA = commits[0].FullSHA // newest (rightmost)
	prevSHA := m.SelectPrevInWindow()  // left -> older -> commits[1]
	if prevSHA != commits[1].FullSHA {
		t.Fatalf("SelectPrevInWindow expected %s, got %s", commits[1].FullSHA, prevSHA)
	}
	nextSHA := m.SelectNextInWindow() // right -> newer -> commits[0]
	if nextSHA != commits[0].FullSHA {
		t.Fatalf("SelectNextInWindow expected %s, got %s", commits[0].FullSHA, nextSHA)
	}
}

func TestTimelineRibbonWidthsExpandedAndCollapsed(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	now := time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC)
	commits := makeRibbonTestCommits(now)
	widths := []int{136, 120, 80, 40}

	for _, w := range widths {
		m := NewTimelineRibbonModel(w)
		m.SetCommits(commits, len(commits), "")
		m.HeadSHA = commits[0].FullSHA
		m.SelectedSHA = commits[1].FullSHA
		m.SplitCompare = true
		m.LeftSHA = commits[4].FullSHA
		m.RightSHA = commits[0].FullSHA
		m.IsFocused = true

		if m.OuterHeight() != 6 {
			t.Fatalf("width %d expanded: expected OuterHeight 6, got %d", w, m.OuterHeight())
		}
		expandedView := m.View()
		assertNoUnrecoveredReset(t, fmt.Sprintf("ribbon-expanded-w%d", w), expandedView, w, 6)

		m.ToggleCollapsed()
		if m.OuterHeight() != 1 {
			t.Fatalf("width %d collapsed: expected OuterHeight 1, got %d", w, m.OuterHeight())
		}
		collapsedView := m.View()
		assertNoUnrecoveredReset(t, fmt.Sprintf("ribbon-collapsed-w%d", w), collapsedView, w, 1)
		if !strings.Contains(stripANSI(collapsedView), "TIMELINE") {
			t.Errorf("width %d collapsed: expected TIMELINE label, got %q", w, stripANSI(collapsedView))
		}
	}

	// Ultra-narrow (< 20 cols) gracefully degrades to 1 row without panic
	narrow := NewTimelineRibbonModel(16)
	narrow.SetCommits(commits, len(commits), "")
	if narrow.OuterHeight() != 1 {
		t.Fatalf("expected OuterHeight 1 for width 16, got %d", narrow.OuterHeight())
	}
	assertNoUnrecoveredReset(t, "ribbon-w16", narrow.View(), 16, 1)
}

func TestTimelineRibbonMouseHitTestingAndDrag(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	now := time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC)
	commits := makeRibbonTestCommits(now)

	m := NewTimelineRibbonModel(120)
	m.SetCommits(commits, len(commits), "")
	m.HeadSHA = commits[0].FullSHA
	m.SelectedSHA = commits[0].FullSHA

	// 1. Row 0 preset chips & collapse toggle
	chips := m.TopChipHitRanges()
	expectedActions := []string{"preset:1h", "preset:24h", "preset:7d", "preset:all", "preset:fit", "toggle_collapse"}
	if len(chips) != len(expectedActions) {
		t.Fatalf("expected %d top chips at width 120, got %d: %+v", len(expectedActions), len(chips), chips)
	}
	for i, wantAction := range expectedActions {
		if chips[i].Label != wantAction {
			t.Fatalf("chip %d: expected %q, got %q", i, wantAction, chips[i].Label)
		}
		act, payload := m.HandleClick(chips[i].StartX, 0, false)
		if act != wantAction || payload != "" {
			t.Fatalf("click on chip %q at x=%d: expected (%q, \"\"), got (%q, %q)", wantAction, chips[i].StartX, wantAction, act, payload)
		}
	}

	// 2. Rows 1..2 commit node click & Shift-click pin_left (plus +/-1 col tolerance)
	layout := m.computeLayout()
	if len(layout.colToCommit) == 0 {
		t.Fatalf("expected non-empty colToCommit mapping")
	}
	// Find column of oldest commit (commits[5], at left edge col 0)
	oldestCol := -1
	for col, idx := range layout.colToCommit {
		if idx == 5 {
			oldestCol = col
			break
		}
	}
	if oldestCol < 0 {
		t.Fatalf("could not find oldest commit column in colToCommit: %+v", layout.colToCommit)
	}
	// Click exact column on rail (row 2)
	act, sha := m.HandleClick(1+oldestCol, 2, false)
	if act != "select_commit" || sha != commits[5].FullSHA {
		t.Fatalf("click commit node: expected (select_commit, %s), got (%q, %q)", commits[5].FullSHA, act, sha)
	}
	// Shift-click within +1 col tolerance on histogram (row 1)
	act, sha = m.HandleClick(1+oldestCol+1, 1, true)
	if act != "pin_left" || sha != commits[5].FullSHA {
		t.Fatalf("shift-click commit node +1 col: expected (pin_left, %s), got (%q, %q)", commits[5].FullSHA, act, sha)
	}

	// 3. Row 4 overview scrubber: left handle drag, right handle drag, pan drag, and outside jump
	act, _ = m.HandleClick(layout.leftHandleX, 4, false)
	if act != "drag_start:left" || m.DragMode != "left" {
		t.Fatalf("expected drag_start:left, got (%q, DragMode=%q)", act, m.DragMode)
	}
	origStart := m.WindowStart
	changed := m.HandleDrag(layout.leftHandleX + 15)
	if !changed || !m.WindowStart.After(origStart) {
		t.Fatalf("HandleDrag(left) failed to move WindowStart right")
	}
	m.EndDrag()
	if m.DragMode != "" {
		t.Fatalf("EndDrag did not clear DragMode")
	}

	layout = m.computeLayout()
	act, _ = m.HandleClick(layout.rightHandleX, 4, false)
	if act != "drag_start:right" || m.DragMode != "right" {
		t.Fatalf("expected drag_start:right, got (%q, DragMode=%q)", act, m.DragMode)
	}
	origEnd := m.WindowEnd
	changed = m.HandleDrag(layout.rightHandleX - 15)
	if !changed || !m.WindowEnd.Before(origEnd) {
		t.Fatalf("HandleDrag(right) failed to move WindowEnd left")
	}
	m.EndDrag()

	// Pan inside narrowed window
	layout = m.computeLayout()
	midX := (layout.leftHandleX + layout.rightHandleX) / 2
	act, _ = m.HandleClick(midX, 4, false)
	if act != "drag_start:pan" || m.DragMode != "pan" {
		t.Fatalf("expected drag_start:pan at x=%d (handles %d..%d), got (%q, DragMode=%q)",
			midX, layout.leftHandleX, layout.rightHandleX, act, m.DragMode)
	}
	panOrigStart := m.WindowStart
	changed = m.HandleDrag(midX - 5)
	if !changed || !m.WindowStart.Before(panOrigStart) {
		t.Fatalf("HandleDrag(pan) failed to shift window left")
	}
	m.EndDrag()

	// Click outside active window on right side of overview track to jump/center
	layout = m.computeLayout()
	jumpX := layout.trackStartX + layout.trackW - 1
	if jumpX > layout.rightHandleX {
		beforeJumpEnd := m.WindowEnd
		act, _ = m.HandleClick(jumpX, 4, false)
		if act != "window_changed" {
			t.Fatalf("expected window_changed on outside track click, got %q", act)
		}
		if !m.WindowEnd.After(beforeJumpEnd) {
			t.Fatalf("expected outside click on right to jump window later")
		}
	}

	// 4. Collapsed bar click toggles expand
	m.ToggleCollapsed()
	act, _ = m.HandleClick(5, 0, false)
	if act != "toggle_collapse" {
		t.Fatalf("expected toggle_collapse on collapsed bar click, got %q", act)
	}
}

func TestTimelineRibbonAll11Themes(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() {
		_, _ = SetTheme("dark")
	}()

	themes := AvailableThemes()
	if len(themes) != 11 {
		t.Fatalf("expected 11 built-in themes, got %d", len(themes))
	}

	now := time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC)
	commits := makeRibbonTestCommits(now)

	for _, th := range themes {
		_, ok := SetTheme(th.Name)
		if !ok {
			t.Fatalf("failed to activate theme %q", th.Name)
		}

		m := NewTimelineRibbonModel(100)
		m.SetCommits(commits, len(commits), "")
		m.HeadSHA = commits[0].FullSHA
		m.SelectedSHA = commits[1].FullSHA
		m.SplitCompare = true
		m.LeftSHA = commits[4].FullSHA
		m.RightSHA = commits[0].FullSHA
		m.IsFocused = true

		expanded := m.View()
		assertNoUnrecoveredReset(t, "theme-"+th.Name+"-expanded", expanded, 100, 6)
		for idx, line := range strings.Split(expanded, "\n") {
			if gotW := ansi.PrintableRuneWidth(line); gotW != 100 {
				t.Fatalf("theme %s line %d: width=%d want=100", th.Name, idx, gotW)
			}
		}

		m.ToggleCollapsed()
		collapsed := m.View()
		assertNoUnrecoveredReset(t, "theme-"+th.Name+"-collapsed", collapsed, 100, 1)
	}
}

func TestTimelineRibbonBrushDragAndOverviewBrush(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() { _, _ = SetTheme("dark") }()
	_, _ = SetTheme("dark")

	now := time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC)
	commits := makeRibbonTestCommits(now) // spans 10 days (now-10d .. now)

	m := NewTimelineRibbonModel(120)
	m.SetCommits(commits, len(commits), "")
	origStart, origEnd := m.WindowStart, m.WindowEnd

	// 1. Click & release without dragging (<2 cols) must NOT alter window.
	act, _ := m.HandleClick(40, 1, false)
	if act != "drag_start:brush" || m.DragMode != "brush" {
		t.Fatalf("expected brush drag start on empty plot area, got act=%q mode=%q", act, m.DragMode)
	}
	m.HandleDrag(41) // 1 col jitter (<2)
	if committed := m.EndDrag(); committed {
		t.Fatalf("expected <2 col jitter release NOT to change window")
	}
	if !m.WindowStart.Equal(origStart) || !m.WindowEnd.Equal(origEnd) {
		t.Fatalf("window should remain unchanged after non-drag click")
	}

	// 2. Click at x=80 and drag right to x=118 across recent commits (plot row 2).
	clk := now
	m.nowFunc = func() time.Time { return clk }
	m.HandleClick(80, 2, false)
	m.HandleDrag(118)
	if !m.brushDragged {
		t.Fatalf("expected brushDragged=true after dragging 38 cols")
	}
	// Live brush overlay & axis readout must render full-bleed across widths & themes.
	brushView := m.View()
	assertNoUnrecoveredReset(t, "ribbon-brush-active-120", brushView, 120, 6)
	if !strings.Contains(stripANSI(brushView), "Drag window:") {
		t.Fatalf("expected live 'Drag window:' readout in axis row during brush:\n%s", stripANSI(brushView))
	}
	// Release commits the brushed window!
	if committed := m.EndDrag(); !committed {
		t.Fatalf("expected EndDrag() to commit brushed window")
	}
	if m.ActivePreset != "" || !m.WindowStart.After(origStart) {
		t.Fatalf("expected brushed window to narrow start from %v, got %v..%v (preset=%q)",
			origStart, m.WindowStart, m.WindowEnd, m.ActivePreset)
	}
	// Oldest 10-day commit should now be excluded from WindowCommits.
	if len(m.WindowCommits) >= len(commits) {
		t.Fatalf("expected brushed recent window to exclude oldest commit, got %d/%d", len(m.WindowCommits), len(commits))
	}

	// 3. Reverse (right-to-left) brush drag on axis row (relY=3) also works cleanly.
	m.ResetWindow()
	clk = clk.Add(1 * time.Second)
	m.HandleClick(90, 3, false)
	m.HandleDrag(30)
	if committed := m.EndDrag(); !committed {
		t.Fatalf("expected right-to-left brush drag to commit")
	}
	if !m.WindowStart.After(origStart) || !m.WindowEnd.Before(origEnd) {
		t.Fatalf("expected right-to-left brush [30..90] to narrow both ends, got %v..%v (full %v..%v)",
			m.WindowStart, m.WindowEnd, origStart, origEnd)
	}

	// 4. Overview scrubber outside-window sweep (overview_brush) defines a new window live.
	layout := m.computeLayout()
	// Start outside active window on far left of overview track and sweep to mid-left.
	startX := layout.trackStartX
	if startX < layout.leftHandleX {
		m.HandleClick(startX, 4, false)
		if m.DragMode != "overview_brush" {
			t.Fatalf("expected overview_brush on outside-window click, got %q", m.DragMode)
		}
		if changed := m.HandleDrag(startX + 18); !changed {
			t.Fatalf("expected overview_brush drag to update window live")
		}
		m.EndDrag()
		if !m.WindowStart.Equal(m.FullMinTime) {
			t.Fatalf("expected overview_brush starting at trackStartX to start at FullMinTime, got %v vs %v", m.WindowStart, m.FullMinTime)
		}
	}

	// 5. Full-bleed SGR check during active brush across all 11 themes and narrow width 40.
	m.ResetWindow()
	m.SetWidth(40)
	m.HandleClick(8, 1, false)
	m.HandleDrag(32)
	for _, th := range AvailableThemes() {
		_, _ = SetTheme(th.Name)
		assertNoUnrecoveredReset(t, "brush-w40-"+th.Name, m.View(), 40, 6)
	}
	m.EndDrag()
}

func TestTimelineRibbonDoubleClickProgressiveClusterZoom(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	now := time.Date(2026, 4, 15, 18, 0, 0, 0, time.UTC)
	// Big 20-hour gap: 1 old commit at T-20h, plus a tight 10-minute cluster of 5 commits near T-5m.
	commits := []models.TimelineCommit{
		{CommitID: "cl000001", FullSHA: "cl00000100000000000000000000000000000000", Timestamp: now, DiffStat: "+4", IsCurrent: true},
		{CommitID: "cl000002", FullSHA: "cl00000200000000000000000000000000000000", Timestamp: now.Add(-2 * time.Minute), DiffStat: "+6"},
		{CommitID: "cl000003", FullSHA: "cl00000300000000000000000000000000000000", Timestamp: now.Add(-5 * time.Minute), DiffStat: "~3"},
		{CommitID: "cl000004", FullSHA: "cl00000400000000000000000000000000000000", Timestamp: now.Add(-8 * time.Minute), DiffStat: "+9"},
		{CommitID: "cl000005", FullSHA: "cl00000500000000000000000000000000000000", Timestamp: now.Add(-10 * time.Minute), DiffStat: "-2"},
		{CommitID: "old00001", FullSHA: "old000010000000000000000000000000000000", Timestamp: now.Add(-20 * time.Hour), DiffStat: "+80", IsStartup: true},
	}

	m := NewTimelineRibbonModel(120)
	clk := now
	m.nowFunc = func() time.Time { return clk }
	m.SetCommits(commits, len(commits), "")

	// Clicks spaced > 450ms apart must NOT trigger double-click zoom.
	span0 := m.WindowEnd.Sub(m.WindowStart)
	m.HandleClick(117, 2, false)
	m.EndDrag()
	clk = clk.Add(700 * time.Millisecond)
	act, _ := m.HandleClick(117, 2, false)
	m.EndDrag()
	if act == "window_changed" || m.WindowEnd.Sub(m.WindowStart) != span0 {
		t.Fatalf("slow clicks (>450ms) should not trigger double-click zoom")
	}

	// Consecutive double-clicks near the right-hand cluster progressively narrow the window
	// closer and closer across the 20h gap, framing the 10m cluster by step 3 and zooming
	// straight into the clicked commit on step 4!
	prevSpan := span0
	for step := 1; step <= 4; step++ {
		clk = clk.Add(1 * time.Second)
		// Find current column of cluster midpoint commit (cl000003, T-5m).
		frac := float64(commits[2].Timestamp.Sub(m.WindowStart)) / float64(m.WindowEnd.Sub(m.WindowStart))
		clickX := 1 + posToCol(frac, m.Width-2)

		m.HandleClick(clickX, 2, false)
		m.EndDrag()
		clk = clk.Add(150 * time.Millisecond)
		act, _ = m.HandleClick(clickX, 2, false)
		if act != "window_changed" {
			t.Fatalf("step %d: expected double-click to return window_changed, got %q", step, act)
		}
		newSpan := m.WindowEnd.Sub(m.WindowStart)
		if newSpan >= prevSpan {
			t.Fatalf("step %d: expected double-click to narrow window, prev=%v new=%v", step, prevSpan, newSpan)
		}
		if step <= 3 && len(m.WindowCommits) < 5 {
			t.Fatalf("step %d: expected all 5 cluster commits to stay in view while narrowing onto cluster, got %d commits (window %v..%v)",
				step, len(m.WindowCommits), m.WindowStart, m.WindowEnd)
		}
		if step == 4 {
			if len(m.WindowCommits) < 1 || m.WindowCommits[0].CommitID != "cl000003" {
				t.Fatalf("step 4: expected deep zoom centered on cl000003, got %+v (window %v..%v)",
					m.WindowCommits, m.WindowStart, m.WindowEnd)
			}
		}
		prevSpan = newSpan
	}
	if prevSpan > 10*time.Minute {
		t.Fatalf("expected 4 double-clicks across 20h gap to zoom below 10m around cl000003, got %v", prevSpan)
	}

	// Double-click on overview track zooms into that section of full history.
	m.ResetWindow()
	layout := m.computeLayout()
	ovX := layout.trackStartX + layout.trackW - 2
	clk = clk.Add(1 * time.Second)
	m.HandleClick(ovX, 4, false)
	m.EndDrag()
	clk = clk.Add(150 * time.Millisecond)
	act, _ = m.HandleClick(ovX, 4, false)
	if act != "window_changed" || m.WindowEnd.Sub(m.WindowStart) >= 20*time.Hour {
		t.Fatalf("expected double-click on overview track to zoom in, got act=%q span=%v", act, m.WindowEnd.Sub(m.WindowStart))
	}
}
