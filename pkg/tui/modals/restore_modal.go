package modals

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"

	"timeline/pkg/models"
)

// RestoreModal is an alias for RestoreModalModel for ergonomic Wave 3 integration.
type RestoreModal = RestoreModalModel

const (
	RestoreButtonCancel  = 0
	RestoreButtonConfirm = 1
)

// RestoreModalModel handles full configuration restore confirmation with optional impact preview
// and safe default focus on Cancel.
type RestoreModalModel struct {
	Commit        models.TimelineCommit
	Impact        models.SemanticDiffResult
	HasImpact     bool
	FocusedButton int // 0: Cancel (DEFAULT — safe), 1: Confirm Restore
	Confirmed     bool
	Width         int
	Height        int
}

// NewRestoreModal creates a new restore confirmation modal defaulting focus to Cancel.
func NewRestoreModal(commit models.TimelineCommit, width, height int) RestoreModalModel {
	return RestoreModalModel{
		Commit:        commit,
		FocusedButton: RestoreButtonCancel,
		Width:         width,
		Height:        height,
	}
}

// NewRestoreModalWithImpact creates a restore confirmation modal populated with an impact diff
// comparing the target revision against the running configuration.
func NewRestoreModalWithImpact(commit models.TimelineCommit, impact models.SemanticDiffResult, width, height int) RestoreModalModel {
	m := NewRestoreModal(commit, width, height)
	m.SetImpact(impact)
	return m
}

// SetImpact attaches or updates the semantic impact diff displayed in the restore dialog.
func (m *RestoreModalModel) SetImpact(impact models.SemanticDiffResult) {
	m.Impact = impact
	m.HasImpact = true
}

// renderModalFooter formats concise keybinding hints so they fit cleanly within innerW
// without clipping mid-word. When allowWrap is true and all items do not fit on one line,
// items wrap cleanly across lines at item boundaries (never splitting inside an item).
// When allowWrap is false (e.g. ultra-compact terminals <= 14 rows), compactFallback is
// used if needed so vertical height stays strictly bounded.
func renderModalFooter(items []string, compactFallback string, innerW int, allowWrap bool) string {
	style := lipgloss.NewStyle().Foreground(ColorMuted)
	const sep = " · "

	joined := strings.Join(items, sep)
	if innerW <= 0 || ansi.PrintableRuneWidth(joined) <= innerW {
		return PaintBarLine(style.Render(joined), innerW, ColorSurface, ColorMuted)
	}

	if !allowWrap {
		text := compactFallback
		if text == "" {
			text = joined
		}
		if ansi.PrintableRuneWidth(text) <= innerW {
			return PaintBarLine(style.Render(text), innerW, ColorSurface, ColorMuted)
		}
		fitted := ""
		for _, item := range items {
			cand := item
			if fitted != "" {
				cand = fitted + sep + item
			}
			if ansi.PrintableRuneWidth(cand) <= innerW {
				fitted = cand
			} else {
				break
			}
		}
		if fitted != "" {
			return PaintBarLine(style.Render(fitted), innerW, ColorSurface, ColorMuted)
		}
		return PaintBarLine(style.Render(truncateDisplay(text, innerW)), innerW, ColorSurface, ColorMuted)
	}

	var lines []string
	cur := ""
	for _, item := range items {
		if cur == "" {
			cur = item
			continue
		}
		cand := cur + sep + item
		if ansi.PrintableRuneWidth(cand) <= innerW {
			cur = cand
		} else {
			lines = append(lines, PaintBarLine(style.Render(cur), innerW, ColorSurface, ColorMuted))
			cur = item
		}
	}
	if cur != "" {
		lines = append(lines, PaintBarLine(style.Render(cur), innerW, ColorSurface, ColorMuted))
	}
	return strings.Join(lines, "\n")
}

// cleanCommitSummary returns a clean human-readable commit summary by preferring explicit
// user comments (stripping raw daemon/gitbackend boilerplate prefixes) or Commit.SummaryScope.
func cleanCommitSummary(c models.TimelineCommit) string {
	cleanMsg := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(c.Message, "\r", ""), "\n", " "))
	scope := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(c.SummaryScope, "\r", ""), "\n", " "))

	if cleanMsg != "" {
		lower := strings.ToLower(cleanMsg)
		if strings.HasPrefix(lower, "config commit by ") {
			if idx := strings.Index(cleanMsg, " — "); idx >= 0 {
				if comment := strings.TrimSpace(cleanMsg[idx+len(" — "):]); comment != "" {
					return comment
				}
			}
			if scope != "" {
				return scope
			}
			rest := strings.TrimSpace(cleanMsg[len("config commit by "):])
			if sp := strings.IndexByte(rest, ' '); sp >= 0 {
				if tail := strings.TrimSpace(rest[sp+1:]); tail != "" {
					return tail
				}
			}
			return "Configuration update"
		}
		switch lower {
		case "configuration change",
			"config change",
			"auto-captured configuration change",
			"initial baseline configuration":
			if scope != "" {
				return scope
			}
		}
		return cleanMsg
	}

	if scope != "" {
		return scope
	}
	return "Configuration update"
}

func markXPathAncestors(path string, ancestors map[string]bool) {
	trimmed := strings.TrimSpace(path)
	if len(trimmed) <= 1 {
		return
	}
	bracketDepth := 0
	for i := 1; i < len(trimmed); i++ {
		switch trimmed[i] {
		case '[':
			if bracketDepth == 0 {
				ancestors[trimmed[:i]] = true
			}
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '/':
			if bracketDepth == 0 {
				ancestors[trimmed[:i]] = true
			}
		}
	}
}

// deduplicatedLeafChanges filters out parent container/list rows whose child leaf paths
// are also present in changes, and removes exact (path, diffType) duplicates while
// preserving original order.
func deduplicatedLeafChanges(changes []models.PathChange) []models.PathChange {
	if len(changes) == 0 {
		return nil
	}
	ancestors := make(map[string]bool, len(changes)*2)
	for _, ch := range changes {
		markXPathAncestors(ch.Path, ancestors)
	}

	type dedupKey struct {
		path     string
		diffType models.DiffType
	}
	seen := make(map[dedupKey]bool, len(changes))
	leaves := make([]models.PathChange, 0, len(changes))
	for _, ch := range changes {
		cleanPath := strings.TrimSpace(ch.Path)
		if cleanPath == "" || ancestors[cleanPath] {
			continue
		}
		key := dedupKey{path: cleanPath, diffType: ch.DiffType}
		if seen[key] {
			continue
		}
		seen[key] = true
		leaves = append(leaves, ch)
	}
	return leaves
}

// splitXPathSegments splits an XPath into top-level slash-delimited segments while
// preserving slashes inside [...] brackets (e.g. interface[name=ethernet-1/1]).
func splitXPathSegments(path string) []string {
	var segs []string
	var cur strings.Builder
	bracketDepth := 0
	for i := 0; i < len(path); i++ {
		ch := path[i]
		switch ch {
		case '[':
			bracketDepth++
			cur.WriteByte(ch)
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			cur.WriteByte(ch)
		case '/':
			if bracketDepth == 0 {
				if cur.Len() > 0 {
					segs = append(segs, cur.String())
					cur.Reset()
				}
			} else {
				cur.WriteByte(ch)
			}
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 {
		segs = append(segs, cur.String())
	}
	return segs
}

// truncateXPath truncates an XPath to fit within maxW visual columns while preserving
// the root subsystem and as many unique trailing leaf/entry segments as possible
// (e.g. "/acl/.../entry[sequence-id=20]/action/drop").
func truncateXPath(path string, maxW int) string {
	clean := strings.ReplaceAll(path, "\r", "")
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.ReplaceAll(clean, "\t", " ")
	clean = strings.TrimSpace(clean)
	if maxW <= 0 || ansi.PrintableRuneWidth(clean) <= maxW {
		return clean
	}
	if maxW <= 3 {
		return truncateDisplay(clean, maxW)
	}

	segs := splitXPathSegments(clean)
	if !strings.HasPrefix(clean, "/") || len(segs) < 2 {
		return truncateDisplay(clean, maxW)
	}

	fitWithRoot := func(root string) (best string, tailCount int) {
		prefix := root + "/.../"
		prefixW := ansi.PrintableRuneWidth(prefix)
		minStart := 2
		if root != "/"+segs[0] {
			minStart = 1
		}
		var suffix string
		for k := len(segs) - 1; k >= minStart; k-- {
			candSuffix := segs[k]
			if suffix != "" {
				candSuffix = segs[k] + "/" + suffix
			}
			if prefixW+ansi.PrintableRuneWidth(candSuffix) <= maxW {
				suffix = candSuffix
				tailCount = len(segs) - k
			} else {
				break
			}
		}
		if tailCount > 0 {
			return prefix + suffix, tailCount
		}
		return "", 0
	}

	fullRoot := "/" + segs[0]
	best, bestCount := fitWithRoot(fullRoot)

	if bIdx := strings.IndexByte(segs[0], '['); bIdx > 0 {
		shortRoot := "/" + segs[0][:bIdx]
		if cand, count := fitWithRoot(shortRoot); count > bestCount {
			best, bestCount = cand, count
		}
	}

	if best != "" {
		return best
	}

	// Fallback when even "<root>/.../<lastSeg>" exceeds maxW: preserve root prefix + "..." + trailing tail.
	rootPrefix := fullRoot
	if bIdx := strings.IndexByte(segs[0], '['); bIdx > 0 {
		rootPrefix = "/" + segs[0][:bIdx]
	}
	rootPrefix += "/"
	const dots = "..."
	if maxHead := maxW / 3; ansi.PrintableRuneWidth(rootPrefix) > maxHead {
		if maxHead < 1 {
			maxHead = 1
		}
		rootPrefix = string([]rune(rootPrefix)[:maxHead])
	}
	headW := ansi.PrintableRuneWidth(rootPrefix)
	tailBudget := maxW - headW - len(dots)
	if tailBudget <= 0 {
		return truncateDisplay(clean, maxW)
	}
	runes := []rune(clean)
	tailW := 0
	startIdx := len(runes)
	for startIdx > 0 {
		rw := ansi.PrintableRuneWidth(string(runes[startIdx-1]))
		if tailW+rw > tailBudget {
			break
		}
		tailW += rw
		startIdx--
	}
	return rootPrefix + dots + string(runes[startIdx:])
}

func (m RestoreModalModel) impactCounts(leaves []models.PathChange) (added, modified, deleted int) {
	added = m.Impact.AddedCount
	modified = m.Impact.ModifiedCount
	deleted = m.Impact.DeletedCount
	if added == 0 && modified == 0 && deleted == 0 && len(leaves) > 0 {
		for _, ch := range leaves {
			switch ch.DiffType {
			case models.DiffAdded:
				added++
			case models.DiffModified:
				modified++
			case models.DiffDeleted:
				deleted++
			}
		}
	}
	return added, modified, deleted
}

// View renders the restore confirmation dialog.
func (m RestoreModalModel) View() string {
	innerW, vPad, hPad, maxInnerH := modalBoxMetrics(m.Width, m.Height, 68)
	compact := m.Height > 0 && m.Height <= 24
	ultraCompact := m.Height > 0 && m.Height <= 14

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorDangerBg).
		Padding(0, 1).
		Render("◆ CONFIRM FULL CONFIGURATION RESTORE")

	warning := lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render(
		"WARNING: Replaces the LIVE running configuration with the selected snapshot.",
	)

	shortSHA := m.Commit.FullSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	lblStyle := lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
	valStyle := lipgloss.NewStyle().Foreground(ColorText)
	shaStyle := lipgloss.NewStyle().Foreground(ColorActive).Bold(true)

	valMaxW := innerW - 15
	if valMaxW < 10 {
		valMaxW = 10
	}

	detailLines := []string{
		PaintBarLine(fmt.Sprintf("%s %s", lblStyle.Render("Target Commit:"), shaStyle.Render(shortSHA)), innerW, ColorSurface, ColorText),
		PaintBarLine(fmt.Sprintf("%s %s", lblStyle.Render("Author:       "), valStyle.Render(truncateDisplay(m.Commit.Author, valMaxW))), innerW, ColorSurface, ColorText),
	}
	if !ultraCompact {
		detailLines = append(detailLines,
			PaintBarLine(fmt.Sprintf("%s %s", lblStyle.Render("Date:         "), valStyle.Render(m.Commit.FormattedTime())), innerW, ColorSurface, ColorText),
		)
	}
	detailLines = append(detailLines,
		PaintBarLine(fmt.Sprintf("%s %s", lblStyle.Render("Summary:      "), valStyle.Render(truncateDisplay(cleanCommitSummary(m.Commit), valMaxW))), innerW, ColorSurface, ColorText),
	)

	var impactLines []string
	if m.HasImpact {
		leafChanges := deduplicatedLeafChanges(m.Impact.Changes)
		added, modified, deleted := m.impactCounts(leafChanges)
		hasDiff := m.Impact.HasChanges || added > 0 || modified > 0 || deleted > 0 || len(leafChanges) > 0
		if !hasDiff {
			impactLines = append(impactLines, PaintBarLine(
				lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render("● No differences vs running config"),
				innerW,
				ColorSurface,
				ColorText,
			))
		} else {
			sep := lipgloss.NewStyle().Foreground(ColorMuted).Render(" · ")
			summaryRow := fmt.Sprintf(
				"%s %s%s%s%s%s",
				lblStyle.Render("Impact:"),
				lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(fmt.Sprintf("+%d added", added)),
				sep,
				lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render(fmt.Sprintf("~%d modified", modified)),
				sep,
				lipgloss.NewStyle().Bold(true).Foreground(ColorDanger).Render(fmt.Sprintf("-%d deleted", deleted)),
			)
			impactLines = append(impactLines, PaintBarLine(summaryRow, innerW, ColorSurface, ColorText))

			maxPaths := 5
			if ultraCompact {
				maxPaths = 0
			} else if compact {
				// Leave room for borders, header, warning, details, buttons, footer at 80x24
				avail := maxInnerH - len(detailLines) - 7
				if avail < 0 {
					avail = 0
				}
				if avail < maxPaths {
					maxPaths = avail
				}
			}

			pathBudget := innerW - 4
			if pathBudget < 4 {
				pathBudget = 4
			}

			shown := 0
			for i, ch := range leafChanges {
				if i >= maxPaths {
					break
				}
				var prefix string
				switch ch.DiffType {
				case models.DiffAdded:
					prefix = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render("  + ")
				case models.DiffDeleted:
					prefix = lipgloss.NewStyle().Bold(true).Foreground(ColorDanger).Render("  - ")
				default:
					prefix = lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render("  ~ ")
				}
				pathText := truncateXPath(ch.Path, pathBudget)
				impactLines = append(impactLines, PaintBarLine(prefix+valStyle.Render(pathText), innerW, ColorSurface, ColorText))
				shown++
			}
			if remaining := len(leafChanges) - shown; remaining > 0 && !ultraCompact {
				moreText := fmt.Sprintf("  … and %d more affected paths", remaining)
				if remaining == 1 {
					moreText = "  … and 1 more affected path"
				}
				impactLines = append(impactLines, PaintBarLine(
					lipgloss.NewStyle().Foreground(ColorMuted).Render(moreText),
					innerW,
					ColorSurface,
					ColorMuted,
				))
			}
		}
	}

	// Explicit focusable buttons: [ Cancel ] (default 0) and [ Confirm Restore ] (1)
	cancelStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Background(ColorBadgeBg).
		Foreground(ColorText)
	if m.FocusedButton == RestoreButtonCancel {
		cancelStyle = cancelStyle.
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true)
	}
	btnCancel := cancelStyle.Render("[ Cancel ]")

	confirmStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Background(ColorBadgeBg).
		Foreground(ColorDanger)
	if m.FocusedButton == RestoreButtonConfirm {
		confirmStyle = confirmStyle.
			Background(ColorDangerBg).
			Foreground(ColorOnAccent).
			Bold(true)
	}
	btnConfirm := confirmStyle.Render("[ Confirm Restore ]")

	btnGap := lipgloss.NewStyle().Background(ColorSurface).Render("  ")
	buttonsRow := PaintBarLine(
		lipgloss.JoinHorizontal(lipgloss.Center, btnCancel, btnGap, btnConfirm),
		innerW,
		ColorSurface,
		ColorText,
	)

	footer := renderModalFooter(
		[]string{"←/→/Tab Switch", "Enter Select", "y Confirm", "n/Esc Cancel"},
		"←/→ Pick · Enter OK · y/Esc",
		innerW,
		!ultraCompact,
	)

	var sections []string
	sections = append(sections, PaintBarLine(title, innerW, ColorSurface, ColorText))
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, PaintLine(warning, innerW, ColorSurface, ColorDanger))
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, strings.Join(detailLines, "\n"))
	if len(impactLines) > 0 {
		if !compact {
			sections = append(sections, "")
		}
		sections = append(sections, strings.Join(impactLines, "\n"))
	}
	if !ultraCompact {
		sections = append(sections, "")
	}
	sections = append(sections, buttonsRow)
	sections = append(sections, footer)

	content := strings.Join(sections, "\n")
	return renderCenteredModal(content, m.Width, m.Height, innerW, vPad, hPad, maxInnerH, ColorDangerBg)
}

// Update processes key events for the restore confirmation dialog.
// Bare Enter activates the currently focused button (default: Cancel) and never accidentally restores.
func (m RestoreModalModel) Update(msg tea.Msg) (RestoreModalModel, bool, bool) {
	// Returns (model, confirmed, close)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "right", "tab", "shift+tab", "h", "l":
			if m.FocusedButton == RestoreButtonCancel {
				m.FocusedButton = RestoreButtonConfirm
			} else {
				m.FocusedButton = RestoreButtonCancel
			}
			return m, false, false

		case "enter", " ", "space":
			if m.FocusedButton == RestoreButtonConfirm {
				m.Confirmed = true
				return m, true, true
			}
			m.Confirmed = false
			return m, false, true

		case "y", "Y":
			m.FocusedButton = RestoreButtonConfirm
			m.Confirmed = true
			return m, true, true

		case "n", "N", "esc":
			m.Confirmed = false
			return m, false, true
		}
	}
	return m, false, false
}
