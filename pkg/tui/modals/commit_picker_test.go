package modals

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"timeline/pkg/models"
)

func samplePickerCommits(n int) []models.TimelineCommit {
	now := time.Now().UTC()
	commits := make([]models.TimelineCommit, 0, n)
	for i := 0; i < n; i++ {
		short := fmt.Sprintf("c%07x", 0x1000+i)
		full := fmt.Sprintf("%s%032x", short, i+1)
		author := "netops"
		if i%2 == 0 {
			author = "admin"
		}
		c := models.TimelineCommit{
			CommitID:     short,
			FullSHA:      full,
			Timestamp:    now.Add(-time.Duration(i*15+2) * time.Minute),
			Author:       author,
			Message:      fmt.Sprintf("Update BGP peer policy and interface ethernet-1/%d description", i+1),
			SummaryScope: fmt.Sprintf("interface ethernet-1/%d, network-instance default", i+1),
			DiffStat:     fmt.Sprintf("+%d / ~%d / -%d", i+1, (i%3)+1, i%2),
			IsCurrent:    i == 0,
			IsRestored:   i == 2,
			IsStartup:    i == n-1,
		}
		commits = append(commits, c)
	}
	return commits
}

func TestCommitPickerInitializationAndPreselection(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	commits := samplePickerCommits(20)
	leftSHA := commits[12].FullSHA // older
	rightSHA := commits[1].FullSHA // newer

	// 1. Focus Left with IncludeLive=true: both A & B pre-checked, cursor on A (offset +1 for live row).
	mLeft := NewCommitPickerModal(PickerSideLeft, commits, leftSHA, rightSHA, true, 136, 36)
	if mLeft.SelectedIndex != 13 {
		t.Fatalf("expected SelectedIndex=13 (commits[12] + 1 for live row), got %d", mLeft.SelectedIndex)
	}
	if got := mLeft.SelectedSHA(); got != leftSHA {
		t.Fatalf("expected cursor SelectedSHA=%q, got %q", leftSHA, got)
	}
	gotA, gotB := mLeft.ResolvedEndpoints()
	if gotA != leftSHA || gotB != rightSHA {
		t.Fatalf("expected pre-selected A=%q B=%q, got A=%q B=%q", leftSHA, rightSHA, gotA, gotB)
	}
	// Ensure pre-selected cursor item deep in the list is scrolled into view.
	g := mLeft.listGeometry()
	if mLeft.SelectedIndex < g.startIdx || mLeft.SelectedIndex >= g.endIdx {
		t.Fatalf("expected pre-selected index %d within visible window [%d, %d)", mLeft.SelectedIndex, g.startIdx, g.endIdx)
	}

	// 2. Passed in reverse order (newer as leftSHA, older as rightSHA) -> auto-normalized older=A, newer=B!
	mReversed := NewCommitPickerModal(PickerSideRight, commits, rightSHA, leftSHA, false, 136, 36)
	gotA, gotB = mReversed.ResolvedEndpoints()
	if gotA != leftSHA || gotB != rightSHA {
		t.Fatalf("expected chronological normalization A=%q (older) B=%q (newer), got A=%q B=%q", leftSHA, rightSHA, gotA, gotB)
	}

	// 3. Focus Right on LiveRunningSentinel.
	mLive := NewCommitPickerModal(PickerSideRight, commits, leftSHA, LiveRunningSentinel, true, 136, 36)
	if mLive.SelectedIndex != 0 {
		t.Fatalf("expected SelectedIndex=0 for LiveRunningSentinel, got %d", mLive.SelectedIndex)
	}
	gotA, gotB = mLive.ResolvedEndpoints()
	if gotA != leftSHA || gotB != LiveRunningSentinel {
		t.Fatalf("expected A=%q B=LiveRunningSentinel, got A=%q B=%q", leftSHA, gotA, gotB)
	}

	// 4. Short CommitID matching fallback & deduplication when leftSHA == rightSHA.
	mShort := NewCommitPickerModal(PickerSideLeft, commits, commits[4].CommitID, commits[4].FullSHA, false, 136, 36)
	if mShort.SelectedIndex != 4 {
		t.Fatalf("expected SelectedIndex=4 when matching short CommitID, got %d", mShort.SelectedIndex)
	}
	if mShort.SelectionCount() != 1 {
		t.Fatalf("expected identical left/right to deduplicate to 1 selection, got %d", mShort.SelectionCount())
	}

	// 5. Unknown SHA not in list is ignored so user is never stuck with an invisible checked item.
	mMissing := NewCommitPickerModal(PickerSideLeft, commits, "deadbeef00000000", rightSHA, false, 136, 36)
	if mMissing.SelectionCount() != 1 || mMissing.SelectedSHAs[0] != rightSHA {
		t.Fatalf("expected unknown SHA to be skipped, got %#v", mMissing.SelectedSHAs)
	}

	// 6. Zero commits with IncludeLive=false and IncludeLive=true.
	mEmptyNoLive := NewCommitPickerModal(PickerSideLeft, nil, "", "", false, 80, 24)
	if mEmptyNoLive.SelectedIndex != 0 || mEmptyNoLive.SelectedSHA() != "" || mEmptyNoLive.SelectionCount() != 0 {
		t.Fatalf("expected empty selection for 0 commits without live, got idx=%d sha=%q count=%d",
			mEmptyNoLive.SelectedIndex, mEmptyNoLive.SelectedSHA(), mEmptyNoLive.SelectionCount())
	}
	mEmptyWithLive := NewCommitPickerModal(PickerSideRight, nil, "", LiveRunningSentinel, true, 80, 24)
	if mEmptyWithLive.SelectedIndex != 0 || mEmptyWithLive.SelectedSHA() != LiveRunningSentinel || mEmptyWithLive.SelectionCount() != 1 {
		t.Fatalf("expected LiveRunningSentinel for 0 commits with IncludeLive=true, got idx=%d sha=%q count=%d",
			mEmptyWithLive.SelectedIndex, mEmptyWithLive.SelectedSHA(), mEmptyWithLive.SelectionCount())
	}
}

func TestCommitPickerMultiSelectStrictMaxTwoAutoOrderAndClear(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	commits := samplePickerCommits(25)
	// Initially commits[0] (idx 1) and LiveRunningSentinel (idx 0) are checked; cursor on idx 1.
	m := NewCommitPickerModal(PickerSideLeft, commits, commits[0].FullSHA, LiveRunningSentinel, true, 120, 30)
	if m.SelectedIndex != 1 || m.SelectionCount() != 2 {
		t.Fatalf("expected initial idx=1 count=2, got idx=%d count=%d", m.SelectedIndex, m.SelectionCount())
	}

	// Navigation keys: k/up, j/down, pgdown/ctrl+d, pgup/ctrl+u, end/G, home/g.
	m, _, _, confirmed, closed := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.SelectedIndex != 0 || confirmed || closed {
		t.Fatalf("expected 'k' to move to 0 without closing, got idx=%d confirmed=%v closed=%v", m.SelectedIndex, confirmed, closed)
	}
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.SelectedIndex != 0 {
		t.Fatalf("expected Up at top to clamp at 0, got %d", m.SelectedIndex)
	}
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.SelectedIndex != 2 {
		t.Fatalf("expected SelectedIndex=2 after j + Down, got %d", m.SelectedIndex)
	}

	// Strict max-2 rule: cursor is on idx 2 (commits[1]), which is NOT selected, while 2 are already selected.
	// Pressing Space MUST NOT select a 3rd item and MUST show an inline notice.
	m, _, _, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if confirmed || closed || m.SelectionCount() != 2 {
		t.Fatalf("expected max-2 rejection without closing, got count=%d confirmed=%v closed=%v", m.SelectionCount(), confirmed, closed)
	}
	if !strings.Contains(m.Notice, "2 already selected") {
		t.Fatalf("expected inline max-2 warning notice, got %q", m.Notice)
	}
	if !strings.Contains(stripANSI(m.View()), "2 already selected") {
		t.Fatalf("expected rendered view to show inline max-2 notice:\n%s", stripANSI(m.View()))
	}

	// Unselect one checked row (idx 0 = LiveRunningSentinel), then check idx 2 (commits[1]).
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome}) // idx 0
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if m.SelectionCount() != 1 || m.SelectedSHAs[0] != commits[0].FullSHA {
		t.Fatalf("expected unselecting Live to leave only commits[0], got %#v", m.SelectedSHAs)
	}
	// Attempting Enter with only 1 selected MUST NOT confirm or close.
	m, leftSHA, rightSHA, confirmed, closed := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if confirmed || closed || leftSHA != "" || rightSHA != "" {
		t.Fatalf("expected Enter with 1/2 selected to stay open, got left=%q right=%q confirmed=%v closed=%v", leftSHA, rightSHA, confirmed, closed)
	}
	if !strings.Contains(m.Notice, "Select 2 configurations") {
		t.Fatalf("expected need-2 notice on Enter with 1/2, got %q", m.Notice)
	}

	// Select idx 2 (commits[1], older than commits[0]) -> auto-ordered A=commits[1], B=commits[0]!
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // idx 2 -> commits[1]
	m, _, _, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if confirmed || closed {
		t.Fatalf("expected Space to toggle without closing")
	}
	gotA, gotB := m.ResolvedEndpoints()
	if gotA != commits[1].FullSHA || gotB != commits[0].FullSHA {
		t.Fatalf("expected auto-ordered A=commits[1] (%q) B=commits[0] (%q), got A=%q B=%q",
			commits[1].FullSHA, commits[0].FullSHA, gotA, gotB)
	}

	// 'c' clears all selections cleanly.
	m, _, _, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if confirmed || closed || m.SelectionCount() != 0 {
		t.Fatalf("expected 'c' to deselect all without closing, got count=%d confirmed=%v closed=%v", m.SelectionCount(), confirmed, closed)
	}
	// Enter with 0 selected stays open with notice.
	m, _, _, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if confirmed || closed || !strings.Contains(m.Notice, "0/2") {
		t.Fatalf("expected Enter with 0/2 to stay open with notice, got notice=%q confirmed=%v closed=%v", m.Notice, confirmed, closed)
	}

	// Pick two fresh configs in NEWER-FIRST order: Live (idx 0), then older commits[5] (idx 6).
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome}) // idx 0 (Live)
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	for i := 0; i < 6; i++ {
		m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.SelectedIndex != 6 {
		t.Fatalf("expected idx=6, got %d", m.SelectedIndex)
	}
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	// Now Enter confirms with older commits[5] on Left (A) and LiveRunningSentinel on Right (B)!
	_, leftSHA, rightSHA, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !confirmed || !closed || leftSHA != commits[5].FullSHA || rightSHA != LiveRunningSentinel {
		t.Fatalf("expected Enter to confirm A=commits[5] (%q) B=LiveRunningSentinel, got left=%q right=%q confirmed=%v closed=%v",
			commits[5].FullSHA, leftSHA, rightSHA, confirmed, closed)
	}

	// Page/End navigation checks.
	beforePgDn := m.SelectedIndex
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.SelectedIndex <= beforePgDn {
		t.Fatalf("expected PgDown to advance selection from %d, got %d", beforePgDn, m.SelectedIndex)
	}
	afterPgDn := m.SelectedIndex
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.SelectedIndex <= afterPgDn {
		t.Fatalf("expected Ctrl+D to advance selection from %d, got %d", afterPgDn, m.SelectedIndex)
	}
	afterCtrlD := m.SelectedIndex
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.SelectedIndex >= afterCtrlD {
		t.Fatalf("expected PgUp to move selection up from %d, got %d", afterCtrlD, m.SelectedIndex)
	}
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.SelectedIndex != len(commits) {
		t.Fatalf("expected End to jump to last entry %d, got %d", len(commits), m.SelectedIndex)
	}
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.SelectedIndex != 0 {
		t.Fatalf("expected 'g' to jump to 0, got %d", m.SelectedIndex)
	}
	m, _, _, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.SelectedIndex != len(commits) {
		t.Fatalf("expected 'G' to jump to last entry %d, got %d", len(commits), m.SelectedIndex)
	}

	// Tab is a no-op (no A/B tab switch inside modal).
	beforeTab := m.View()
	m, _, _, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if confirmed || closed || m.View() != beforeTab {
		t.Fatalf("expected Tab to be a no-op with no A/B tab switch")
	}

	// Esc and q cancel cleanly.
	_, leftSHA, rightSHA, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if confirmed || !closed || leftSHA != "" || rightSHA != "" {
		t.Fatalf("expected Esc to cancel cleanly, got left=%q right=%q confirmed=%v closed=%v", leftSHA, rightSHA, confirmed, closed)
	}
	_, leftSHA, rightSHA, confirmed, closed = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if confirmed || !closed || leftSHA != "" || rightSHA != "" {
		t.Fatalf("expected 'q' to cancel cleanly, got left=%q right=%q confirmed=%v closed=%v", leftSHA, rightSHA, confirmed, closed)
	}

	// Empty list: navigation does not panic, Enter closes without confirming.
	mEmpty := NewCommitPickerModal(PickerSideLeft, nil, "", "", false, 80, 24)
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
		{Type: tea.KeyPgDown},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyHome},
		{Type: tea.KeyEnd},
		{Type: tea.KeySpace, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune{'c'}},
	} {
		mEmpty, _, _, _, _ = mEmpty.Update(k)
	}
	_, leftSHA, rightSHA, confirmed, closed = mEmpty.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if confirmed || !closed || leftSHA != "" || rightSHA != "" {
		t.Fatalf("expected Enter on empty list to close without confirming, got left=%q right=%q confirmed=%v closed=%v", leftSHA, rightSHA, confirmed, closed)
	}
}

func TestCommitPickerMouseWheelClickToggleAndHeaderConfirm(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	commits := samplePickerCommits(18)
	m := NewCommitPickerModal(PickerSideRight, commits, commits[3].FullSHA, LiveRunningSentinel, true, 120, 32)
	if m.SelectedIndex != 0 {
		t.Fatalf("expected initial SelectedIndex=0, got %d", m.SelectedIndex)
	}

	// MouseWheelDown / MouseWheelUp scroll cursor without closing.
	m, _, _, confirmed, closed := m.Update(tea.MouseMsg{X: 60, Y: 15, Type: tea.MouseWheelDown})
	if m.SelectedIndex != 1 || confirmed || closed {
		t.Fatalf("expected MouseWheelDown to select 1, got idx=%d confirmed=%v closed=%v", m.SelectedIndex, confirmed, closed)
	}
	m, _, _, _, _ = m.Update(tea.MouseMsg{X: 60, Y: 15, Type: tea.MouseWheelUp})
	if m.SelectedIndex != 0 {
		t.Fatalf("expected MouseWheelUp to select 0, got %d", m.SelectedIndex)
	}

	// Verify listGeometry() matches rendered View() coordinates exactly.
	plainLines := strings.Split(stripANSI(m.View()), "\n")
	g := m.listGeometry()
	if !strings.Contains(plainLines[g.boxTopY], "╭") || !strings.Contains(plainLines[g.boxBottomY], "╰") {
		t.Fatalf("geometry boxTopY=%d boxBottomY=%d does not align with rendered borders:\ntop=%q\nbot=%q",
			g.boxTopY, g.boxBottomY, plainLines[g.boxTopY], plainLines[g.boxBottomY])
	}
	if !strings.Contains(plainLines[g.listStartY], "RUNNING CONFIG") {
		t.Fatalf("expected row 0 at listStartY=%d to be RUNNING CONFIG, got %q", g.listStartY, plainLines[g.listStartY])
	}

	clickX := (g.boxLeftX + g.boxRightX) / 2

	// Clicking row 3 (commits[2]) while 2 are already selected rejects with notice and stays open.
	m, _, _, confirmed, closed = m.Update(tea.MouseMsg{X: clickX, Y: g.listStartY + 3, Type: tea.MouseLeft})
	if confirmed || closed || m.SelectionCount() != 2 || !strings.Contains(m.Notice, "2 already selected") {
		t.Fatalf("expected clicking 3rd row while 2 selected to show max-2 notice, got count=%d notice=%q confirmed=%v closed=%v",
			m.SelectionCount(), m.Notice, confirmed, closed)
	}

	// Click row 0 (RUNNING CONFIG) to UNCHECK it, then click row 2 (commits[1]) to CHECK it.
	m, _, _, confirmed, closed = m.Update(tea.MouseMsg{X: clickX, Y: g.listStartY, Type: tea.MouseLeft})
	if confirmed || closed || m.SelectionCount() != 1 {
		t.Fatalf("expected clicking checked row 0 to unselect it (count=1), got count=%d confirmed=%v closed=%v",
			m.SelectionCount(), confirmed, closed)
	}
	m, _, _, confirmed, closed = m.Update(tea.MouseMsg{X: clickX, Y: g.listStartY + 2, Type: tea.MouseLeft})
	if confirmed || closed || m.SelectionCount() != 2 {
		t.Fatalf("expected clicking row 2 (commits[1]) to select it (count=2), got count=%d confirmed=%v closed=%v",
			m.SelectionCount(), confirmed, closed)
	}
	gotA, gotB := m.ResolvedEndpoints()
	if gotA != commits[3].FullSHA || gotB != commits[1].FullSHA {
		t.Fatalf("expected A=commits[3] B=commits[1], got A=%q B=%q", gotA, gotB)
	}

	// Clicking the right header "✓ 2/2 · Enter" badge confirms immediately!
	_, _, bStart, bEnd := m.headerBadgeLayout(g.innerW)
	if bStart < 0 || bEnd <= bStart {
		t.Fatalf("expected positive header badge range, got [%d, %d)", bStart, bEnd)
	}
	badgeClickX := g.innerLeftX + (bStart+bEnd)/2
	_, leftSHA, rightSHA, confirmed, closed := m.Update(tea.MouseMsg{X: badgeClickX, Y: g.headerY, Type: tea.MouseLeft})
	if !confirmed || !closed || leftSHA != commits[3].FullSHA || rightSHA != commits[1].FullSHA {
		t.Fatalf("expected clicking ✓ 2/2 header badge to confirm A=commits[3] B=commits[1], got left=%q right=%q confirmed=%v closed=%v",
			leftSHA, rightSHA, confirmed, closed)
	}

	// Click outside modal box closes without confirming.
	for _, pt := range []struct{ x, y int }{
		{0, 0},
		{g.boxLeftX - 1, g.listStartY},
		{g.boxRightX + 1, g.listStartY},
		{clickX, g.boxTopY - 1},
		{clickX, g.boxBottomY + 1},
	} {
		_, leftSHA, rightSHA, confirmed, closed = m.Update(tea.MouseMsg{X: pt.x, Y: pt.y, Type: tea.MouseLeft})
		if confirmed || !closed || leftSHA != "" || rightSHA != "" {
			t.Fatalf("expected outside click (%d,%d) to close without confirming, got left=%q right=%q confirmed=%v closed=%v",
				pt.x, pt.y, leftSHA, rightSHA, confirmed, closed)
		}
	}

	// Verify scrolled geometry alignment across odd/even heights.
	for _, sz := range []struct{ w, h int }{{120, 31}, {120, 32}, {80, 24}, {40, 15}} {
		ms := NewCommitPickerModal(PickerSideLeft, commits, commits[15].FullSHA, LiveRunningSentinel, true, sz.w, sz.h)
		gs := ms.listGeometry()
		lines := strings.Split(stripANSI(ms.View()), "\n")
		if !strings.Contains(lines[gs.boxTopY], "╭") || !strings.Contains(lines[gs.boxBottomY], "╰") {
			t.Fatalf("%dx%d scrolled: border alignment mismatch (topY=%d %q, botY=%d %q)",
				sz.w, sz.h, gs.boxTopY, lines[gs.boxTopY], gs.boxBottomY, lines[gs.boxBottomY])
		}
		firstCommit, _, ok := ms.commitAtIndex(gs.startIdx)
		if !ok || !strings.Contains(lines[gs.listStartY], firstCommit.CommitID) {
			t.Fatalf("%dx%d scrolled: listStartY=%d expected %q, got %q",
				sz.w, sz.h, gs.listStartY, firstCommit.CommitID, lines[gs.listStartY])
		}
		// Clicking first visible scrolled row after clearing checks that exact commit.
		ms.ClearSelections()
		midX := (gs.boxLeftX + gs.boxRightX) / 2
		ms, _, _, gotConf, gotClosed := ms.Update(tea.MouseMsg{X: midX, Y: gs.listStartY, Type: tea.MouseLeft})
		if gotConf || gotClosed || ms.SelectionCount() != 1 || ms.SelectedSHAs[0] != firstCommit.FullSHA {
			t.Fatalf("%dx%d scrolled click: expected %q selected, got %#v confirmed=%v closed=%v",
				sz.w, sz.h, firstCommit.FullSHA, ms.SelectedSHAs, gotConf, gotClosed)
		}
	}
}

func TestCommitPickerRenderingAcrossSizesThemesAndContent(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() { _, _ = SetTheme("dark") }()

	commits := samplePickerCommits(16)
	leftSHA := commits[2].FullSHA
	rightSHA := commits[0].FullSHA

	// Check visual elements on standard 136x36 view (2/2 ready state).
	_, _ = SetTheme("dark")
	mStd := NewCommitPickerModal(PickerSideLeft, commits, leftSHA, rightSHA, true, 136, 36)
	outStd := mStd.View()
	assertModalFullBleed(t, "picker-136x36-dark", outStd, 136, 36)
	plain := stripANSI(outStd)

	requiredStrings := []string{
		"Select 2 Configurations to Compare",
		"✓ 2/2 · Enter",
		"[✓]",
		"[ ]",
		"● RUNNING CONFIG",
		"Live switch state",
		"▎",
		"●",
		"◆",
		"[A]",
		"[B]",
		"(older)",
		"(newer)",
		commits[0].CommitID,
		commits[2].CommitID,
		"Space Toggle · c Clear All · Enter Compare · Esc Cancel",
	}
	for _, want := range requiredStrings {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected 136x36 view to contain %q:\n%s", want, plain)
		}
	}

	// Uncheck one -> verify 1/2 state renders [1/2] badge and pick-1-more hint.
	mStd.ToggleSelected()
	plainOne := stripANSI(mStd.View())
	if !strings.Contains(plainOne, "1/2 selected") || !strings.Contains(plainOne, "[1/2]") {
		t.Fatalf("expected 1/2 selected view to show count and [1/2] badge:\n%s", plainOne)
	}

	// Clear all -> verify 0/2 state.
	mStd.ClearSelections()
	plainZero := stripANSI(mStd.View())
	if !strings.Contains(plainZero, "0/2 selected") || !strings.Contains(plainZero, "Press Space on 2 configurations") {
		t.Fatalf("expected 0/2 selected view to show prompt:\n%s", plainZero)
	}

	// Jump to bottom to verify INIT node glyph (◌) and top scroll indicator (↑).
	mStd, _, _, _, _ = mStd.Update(tea.KeyMsg{Type: tea.KeyEnd})
	plainBottom := stripANSI(mStd.View())
	if !strings.Contains(plainBottom, "◌") || !strings.Contains(plainBottom, "more above") {
		t.Fatalf("expected bottom view to contain INIT glyph ◌ and scroll-up hint:\n%s", plainBottom)
	}

	// Verify full-bleed invariants and zero border clipping across all 11 themes (dark + light),
	// 3 required terminal sizes (136x36, 80x24, 40x15), 0/1/2 selected + notice states, and 0-commit edge cases.
	sizes := []struct{ w, h int }{
		{136, 36},
		{80, 24},
		{40, 15},
	}

	themes := AvailableThemes()
	if len(themes) != 11 {
		t.Fatalf("expected 11 themes, got %d", len(themes))
	}

	for _, th := range themes {
		_, ok := SetTheme(th.Name)
		if !ok {
			t.Fatalf("SetTheme(%q) failed", th.Name)
		}
		for _, sz := range sizes {
			// 2/2 selected with LiveRunningSentinel
			mp := NewCommitPickerModal(PickerSideLeft, commits, leftSHA, LiveRunningSentinel, true, sz.w, sz.h)
			out := mp.View()
			assertModalFullBleed(t, fmt.Sprintf("theme=%s/%dx%d/2of2", th.Name, sz.w, sz.h), out, sz.w, sz.h)
			if !strings.Contains(out, "╭") || !strings.Contains(out, "╰") {
				t.Fatalf("theme=%s/%dx%d/2of2: modal borders clipped:\n%s", th.Name, sz.w, sz.h, stripANSI(out))
			}

			// Notice banner active (max-2 rejection)
			mp.SelectedIndex = 1 // commits[0] unchecked
			mp.ToggleSelected()
			outNotice := mp.View()
			assertModalFullBleed(t, fmt.Sprintf("theme=%s/%dx%d/notice", th.Name, sz.w, sz.h), outNotice, sz.w, sz.h)

			// 1/2 and 0/2 states
			mp.ClearSelections()
			assertModalFullBleed(t, fmt.Sprintf("theme=%s/%dx%d/0of2", th.Name, sz.w, sz.h), mp.View(), sz.w, sz.h)
			mp.ToggleSelected()
			assertModalFullBleed(t, fmt.Sprintf("theme=%s/%dx%d/1of2", th.Name, sz.w, sz.h), mp.View(), sz.w, sz.h)

			// 0-commit cases (with and without IncludeLive).
			for _, incLive := range []bool{false, true} {
				label := fmt.Sprintf("theme=%s/%dx%d/empty/live=%v", th.Name, sz.w, sz.h, incLive)
				me := NewCommitPickerModal(PickerSideLeft, nil, "", "", incLive, sz.w, sz.h)
				outEmpty := me.View()
				assertModalFullBleed(t, label, outEmpty, sz.w, sz.h)
				if !strings.Contains(outEmpty, "╭") || !strings.Contains(outEmpty, "╰") {
					t.Fatalf("%s: empty modal borders clipped:\n%s", label, stripANSI(outEmpty))
				}
			}
		}
	}
}
