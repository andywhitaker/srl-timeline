package modals

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

func (m RestoreModalModel) impactCounts() (added, modified, deleted int) {
	added = m.Impact.AddedCount
	modified = m.Impact.ModifiedCount
	deleted = m.Impact.DeletedCount
	if added == 0 && modified == 0 && deleted == 0 && len(m.Impact.Changes) > 0 {
		for _, ch := range m.Impact.Changes {
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
		PaintBarLine(fmt.Sprintf("%s %s", lblStyle.Render("Summary:      "), valStyle.Render(truncateDisplay(m.Commit.Message, valMaxW))), innerW, ColorSurface, ColorText),
	)

	var impactLines []string
	if m.HasImpact {
		added, modified, deleted := m.impactCounts()
		hasDiff := m.Impact.HasChanges || added > 0 || modified > 0 || deleted > 0 || len(m.Impact.Changes) > 0
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

			shown := 0
			for i, ch := range m.Impact.Changes {
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
				pathText := truncateDisplay(ch.Path, innerW-5)
				impactLines = append(impactLines, PaintBarLine(prefix+valStyle.Render(pathText), innerW, ColorSurface, ColorText))
				shown++
			}
			if remaining := len(m.Impact.Changes) - shown; remaining > 0 && !ultraCompact {
				impactLines = append(impactLines, PaintBarLine(
					lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("  … and %d more affected paths", remaining)),
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

	footerText := "[←/→/Tab] Select  |  [Enter/Space] Activate  |  [y] Confirm  |  [n/Esc] Cancel"
	if innerW < 56 {
		footerText = "[←/→/Tab] Pick | [Enter] OK | [y] Yes | [Esc] No"
	}
	footer := PaintBarLine(
		lipgloss.NewStyle().Foreground(ColorMuted).Render(footerText),
		innerW,
		ColorSurface,
		ColorMuted,
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
