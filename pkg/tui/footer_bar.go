package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterContext describes the active application state for rendering contextual
// keybinding hints or an inline single-row toast without vertical layout shifts.
type FooterContext struct {
	FilterActive bool
	FocusedPane  int    // 0=timeline, 1=detail, 2=filter
	ActiveTab    int    // 0=diff, 1=config, 2=blame
	Zoomed       bool
	HasBase      bool
	ToastText    string
	ToastKind    string // "primary", "info", "success", "warning", "error"
}

// FooterBarModel renders contextual keybinding shortcuts and inline toasts at the bottom.
type FooterBarModel struct {
	Width int
}

// NewFooterBarModel creates a footer bar.
func NewFooterBarModel(width int) FooterBarModel {
	return FooterBarModel{Width: width}
}

type footerShortcut struct {
	key  string
	desc string
}

// View renders the bottom keyboard shortcuts bar (backward-compatible entrypoint).
func (m FooterBarModel) View(filterActive bool) string {
	return m.ViewContext(FooterContext{
		FilterActive: filterActive,
	})
}

// ViewContext renders a 1-row borderless footer bar with either an inline toast
// (when ctx.ToastText != "") or contextual keybinding hints tailored to the active pane.
func (m FooterBarModel) ViewContext(ctx FooterContext) string {
	if m.Width <= 0 {
		return ""
	}

	if ctx.ToastText != "" {
		return m.renderToast(ctx.ToastText, ctx.ToastKind)
	}

	if m.Width < 4 {
		return PaintBarLine("?", m.Width, ColorSurface, ColorActive)
	}

	innerW := m.Width - 2 // 1-col left and right padding

	primary, trailing := buildFooterShortcuts(ctx)

	renderItem := func(sc footerShortcut) string {
		k := StyleKeybindKey.Render(sc.key)
		d := StyleKeybindDesc.Render(" " + sc.desc)
		return k + d
	}

	sep := lipgloss.NewStyle().Background(ColorSurface).Render("  ")
	sepW := 2

	// Always reserve space for trailing help/quit hints when possible.
	var trailingRendered []string
	trailingW := 0
	for i, sc := range trailing {
		item := renderItem(sc)
		w := lipgloss.Width(item)
		addW := w
		if i > 0 {
			addW += sepW
		}
		if trailingW+addW <= innerW {
			if i > 0 {
				trailingRendered = append(trailingRendered, sep)
			}
			trailingRendered = append(trailingRendered, item)
			trailingW += addW
		}
	}

	budget := innerW
	if trailingW > 0 {
		budget = innerW - trailingW - sepW
		if budget < 0 {
			budget = 0
		}
	}

	var leftRendered []string
	leftW := 0
	for _, sc := range primary {
		item := renderItem(sc)
		w := lipgloss.Width(item)
		addW := w
		if len(leftRendered) > 0 {
			addW += sepW
		}
		if leftW+addW > budget {
			continue
		}
		if len(leftRendered) > 0 {
			leftRendered = append(leftRendered, sep)
		}
		leftRendered = append(leftRendered, item)
		leftW += addW
	}

	var combined strings.Builder
	pad := lipgloss.NewStyle().Background(ColorSurface).Render(" ")
	combined.WriteString(pad)
	if len(leftRendered) > 0 {
		combined.WriteString(strings.Join(leftRendered, ""))
		if len(trailingRendered) > 0 {
			gapW := innerW - leftW - trailingW
			if gapW < sepW {
				gapW = sepW
			}
			combined.WriteString(lipgloss.NewStyle().Background(ColorSurface).Render(strings.Repeat(" ", gapW)))
			combined.WriteString(strings.Join(trailingRendered, ""))
		}
	} else if len(trailingRendered) > 0 {
		combined.WriteString(strings.Join(trailingRendered, ""))
	}
	combined.WriteString(pad)

	return PaintBarLine(combined.String(), m.Width, ColorSurface, ColorTextSecondary)
}

func (m FooterBarModel) renderToast(text, kind string) string {
	clean := strings.ReplaceAll(text, "\r\n", " ")
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.ReplaceAll(clean, "\r", " ")
	clean = strings.TrimSpace(clean)

	bg := ColorPrimary
	fg := ColorOnAccent
	icon := GlyphLive + " "

	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "success":
		bg = ColorSuccessBg
		fg = ColorOnAccent
		icon = GlyphCheck + " "
	case "warning":
		bg = ColorWarningBg
		fg = ColorOnWarning
		icon = GlyphMilestone + " "
	case "error":
		bg = ColorDangerBg
		fg = ColorOnAccent
		icon = GlyphCross + " "
	default:
		bg = ColorPrimary
		fg = ColorOnAccent
		icon = GlyphLive + " "
	}

	msg := lipgloss.NewStyle().
		Bold(true).
		Foreground(fg).
		Background(bg).
		Render(" " + icon + clean + " ")

	return PaintBarLine(msg, m.Width, bg, fg)
}

func buildFooterShortcuts(ctx FooterContext) (primary []footerShortcut, trailing []footerShortcut) {
	trailing = []footerShortcut{
		{"?", "Help"},
		{"q", "Quit"},
	}

	compareDesc := "Compare"
	if ctx.HasBase {
		compareDesc = "Unmark"
	}
	zoomDesc := "Zoom"
	if ctx.Zoomed {
		zoomDesc = "Unzoom"
	}

	switch ctx.FocusedPane {
	case 2: // PaneFilter
		primary = []footerShortcut{
			{"Enter", "Apply"},
			{"Esc", "Cancel"},
			{"Ctrl+t", "Theme"},
		}
		return primary, trailing

	case 1: // PaneDetail
		primary = append(primary, footerShortcut{"Esc", "Timeline"})
		primary = append(primary, footerShortcut{"z", zoomDesc})
		switch ctx.ActiveTab {
		case 0: // TabDiff
			primary = append(primary,
				footerShortcut{"1-3", "Mode"},
				footerShortcut{"4/v", "Live"},
			)
		case 1: // TabConfig
			primary = append(primary, footerShortcut{"1-2", "Format"})
		}
		if ctx.FilterActive {
			primary = append(primary, footerShortcut{"x", "Clear"})
		}
		primary = append(primary,
			footerShortcut{"d/c/b", "Tabs"},
			footerShortcut{"m", compareDesc},
			footerShortcut{"r", "Restore"},
			footerShortcut{"p", "Cherry"},
			footerShortcut{"e", "Export"},
			footerShortcut{"g", "Git"},
			footerShortcut{"T/Ctrl+t", "Theme"},
		)
		return primary, trailing

	default: // 0: PaneTimeline
		if ctx.FilterActive {
			primary = append(primary, footerShortcut{"Esc", "Clear"})
		}
		primary = append(primary,
			footerShortcut{"Enter", "Inspect"},
			footerShortcut{"/", "Filter"},
			footerShortcut{"d/c/b", "Tabs"},
			footerShortcut{"m", compareDesc},
			footerShortcut{"z", zoomDesc},
			footerShortcut{"r", "Restore"},
			footerShortcut{"p", "Cherry"},
			footerShortcut{"e", "Export"},
			footerShortcut{"g", "Git"},
			footerShortcut{"T/Ctrl+t", "Theme"},
		)
		return primary, trailing
	}
}
