package tui

import (
	"strings"
	"timeline/pkg/tui/modals"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"
	"github.com/muesli/reflow/truncate"
)

// Theme defines a complete semantic color palette for the TUI.
type Theme = modals.Theme

// Deterministic single-width geometric Unicode glyphs (no emoji width ambiguity).
const (
	GlyphLive        = "●"
	GlyphSelected    = "◉"
	GlyphPaused      = "○"
	GlyphMilestone   = "◆"
	GlyphEmpty       = "◌"
	GlyphPipe        = "│"
	GlyphBar         = "▎"
	GlyphCollapsed   = "▸"
	GlyphExpanded    = "▾"
	GlyphCheck       = "✓"
	GlyphCross       = "✕"
	GlyphUp          = "↑"
	GlyphDown        = "↓"
	GlyphScrollThumb = "█"
	GlyphScrollTrack = "░"
)

var (
	// Core Palette
	ColorBackground   lipgloss.Color
	ColorSurface      lipgloss.Color
	ColorSurfaceFocus lipgloss.Color
	ColorPanel        lipgloss.Color
	ColorBadgeBg      lipgloss.Color
	ColorBorder       lipgloss.Color
	ColorActive       lipgloss.Color
	ColorPrimary      lipgloss.Color
	ColorSuccess      lipgloss.Color
	ColorSuccessBg    lipgloss.Color
	ColorWarning      lipgloss.Color
	ColorWarningBg    lipgloss.Color
	ColorOnWarning    lipgloss.Color
	ColorDanger       lipgloss.Color
	ColorDangerBg     lipgloss.Color
	ColorMuted        lipgloss.Color
	ColorText         lipgloss.Color
	ColorTextDim      lipgloss.Color
	ColorHighlight    lipgloss.Color
	ColorPurple       lipgloss.Color
	ColorCyan         lipgloss.Color
	ColorOnAccent     lipgloss.Color

	// Semantic Aliases
	ColorTextPrimary   lipgloss.Color
	ColorTextSecondary lipgloss.Color

	// Diff & Syntax Palette
	ColorDiffAddBg       lipgloss.Color
	ColorDiffDelBg       lipgloss.Color
	ColorDiffHunkBg      lipgloss.Color
	ColorSyntaxInterface lipgloss.Color
	ColorSyntaxNetwork   lipgloss.Color
	ColorSyntaxSystem    lipgloss.Color
	ColorSyntaxACL       lipgloss.Color
	ColorSyntaxValue     lipgloss.Color

	// Header Styles
	StyleHeaderContainer  lipgloss.Style
	StyleAppTitle         lipgloss.Style
	StyleHeaderApp        lipgloss.Style
	StyleBadgeDevice      lipgloss.Style
	StyleHeaderTarget     lipgloss.Style
	StyleBadgeVersion     lipgloss.Style
	StyleBadgeActive      lipgloss.Style
	StyleHeaderDaemonRun  lipgloss.Style
	StyleHeaderDaemonStop lipgloss.Style
	StyleBadgeSync        lipgloss.Style

	// Filter Bar Styles
	StyleFilterContainer        lipgloss.Style
	StyleFilterContainerFocused lipgloss.Style
	StyleFilterLabel            lipgloss.Style
	StyleFilterBadge            lipgloss.Style

	// Panel Styles
	StylePanelHeader    lipgloss.Style
	StyleActiveBorder   lipgloss.Style
	StyleInactiveBorder lipgloss.Style

	// Tab Styles
	StyleTabActive   lipgloss.Style
	StyleActiveTab   lipgloss.Style
	StyleTabInactive lipgloss.Style
	StyleInactiveTab lipgloss.Style

	// Visual Timeline Graph Styles
	StyleTimelineNodeActive    lipgloss.Style
	StyleTimelineNodeNormal    lipgloss.Style
	StyleTimelineNodeMilestone lipgloss.Style
	StyleTimelineLine          lipgloss.Style
	StyleTimelineSelectedRow   lipgloss.Style
	StyleSelectedRow           lipgloss.Style
	StyleCommitMessage         lipgloss.Style
	StyleAuthorAdmin           lipgloss.Style
	StyleAuthorOther           lipgloss.Style
	StyleDiffStatAdd           lipgloss.Style
	StyleDiffStatMod           lipgloss.Style
	StyleDiffStatDel           lipgloss.Style

	// Diff Line Styles
	StyleDiffAddLine  lipgloss.Style
	StyleDiffDelLine  lipgloss.Style
	StyleDiffHunkLine lipgloss.Style

	// Config Syntax Styles
	StyleConfigInterface   lipgloss.Style
	StyleConfigNetwork     lipgloss.Style
	StyleConfigSystem      lipgloss.Style
	StyleConfigACL         lipgloss.Style
	StyleConfigDefault     lipgloss.Style
	StyleConfigKey         lipgloss.Style
	StyleConfigValue       lipgloss.Style
	StyleConfigPunctuation lipgloss.Style

	// Footer Styles
	StyleFooterContainer lipgloss.Style
	StyleKeybindKey      lipgloss.Style
	StyleKeybindDesc     lipgloss.Style

	// Modal Dialog Styles
	StyleModalBox   lipgloss.Style
	StyleModalTitle lipgloss.Style
)

func init() {
	modals.RegisterThemeListener(applyThemeStyles)
}

func applyThemeStyles(t Theme) {
	ColorBackground = t.Background
	ColorSurface = t.Surface
	ColorSurfaceFocus = t.SurfaceFocus
	ColorPanel = t.Panel
	ColorBadgeBg = t.BadgeBg
	ColorBorder = t.Border
	ColorActive = t.Active
	ColorPrimary = t.Primary
	ColorSuccess = t.Success
	ColorSuccessBg = t.SuccessBg
	ColorWarning = t.Warning
	ColorWarningBg = t.WarningBg
	ColorOnWarning = t.OnWarning
	ColorDanger = t.Danger
	ColorDangerBg = t.DangerBg
	ColorMuted = t.Muted
	ColorText = t.Text
	ColorTextDim = t.TextDim
	ColorHighlight = t.Highlight
	ColorPurple = t.Purple
	ColorCyan = t.Cyan
	ColorOnAccent = t.OnAccent

	ColorTextPrimary = t.Text
	ColorTextSecondary = t.Muted

	ColorDiffAddBg = t.DiffAddBg
	ColorDiffDelBg = t.DiffDelBg
	ColorDiffHunkBg = t.DiffHunkBg
	ColorSyntaxInterface = t.SyntaxInterface
	ColorSyntaxNetwork = t.SyntaxNetwork
	ColorSyntaxSystem = t.SyntaxSystem
	ColorSyntaxACL = t.SyntaxACL
	ColorSyntaxValue = t.SyntaxValue

	StyleHeaderContainer = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(0, 1)

	StyleAppTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorActive).
		Background(ColorSurface)
	StyleHeaderApp = StyleAppTitle

	StyleBadgeDevice = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorCyan).
		Bold(true)
	StyleHeaderTarget = StyleBadgeDevice

	StyleBadgeVersion = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorMuted)

	StyleBadgeActive = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorSuccess).
		Bold(true)
	StyleHeaderDaemonRun = StyleBadgeActive

	StyleHeaderDaemonStop = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorTextDim)

	StyleBadgeSync = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorMuted)

	StyleFilterContainer = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(0, 1)

	StyleFilterContainerFocused = lipgloss.NewStyle().
		Background(ColorSurfaceFocus).
		Foreground(ColorText).
		Padding(0, 1)

	StyleFilterLabel = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorMuted).
		Background(ColorSurface).
		MarginRight(1).
		MarginBackground(ColorSurface)

	StyleFilterBadge = lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorActive).
		Bold(true).
		Padding(0, 1).
		MarginLeft(1).
		MarginBackground(ColorSurface)

	StylePanelHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorMuted).
		Background(ColorSurface).
		Padding(0, 1)

	StyleActiveBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorActive).
		BorderBackground(ColorBackground).
		Background(ColorBackground).
		Foreground(ColorText)

	StyleInactiveBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		BorderBackground(ColorBackground).
		Background(ColorBackground).
		Foreground(ColorText)

	StyleTabActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorBackground)
	StyleActiveTab = StyleTabActive

	StyleTabInactive = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Background(ColorBadgeBg).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorBackground)
	StyleInactiveTab = StyleTabInactive

	StyleTimelineNodeActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorActive)

	StyleTimelineNodeNormal = lipgloss.NewStyle().
		Foreground(ColorMuted)

	StyleTimelineNodeMilestone = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWarning)

	StyleTimelineLine = lipgloss.NewStyle().
		Foreground(ColorBorder)

	StyleTimelineSelectedRow = lipgloss.NewStyle().
		Background(ColorHighlight).
		Foreground(ColorActive).
		Bold(true)
	StyleSelectedRow = StyleTimelineSelectedRow

	StyleCommitMessage = lipgloss.NewStyle().
		Foreground(ColorText)

	StyleAuthorAdmin = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorSuccess)

	StyleAuthorOther = lipgloss.NewStyle().
		Foreground(ColorPurple)

	StyleDiffStatAdd = lipgloss.NewStyle().
		Foreground(ColorSuccess).
		Bold(true)

	StyleDiffStatMod = lipgloss.NewStyle().
		Foreground(ColorWarning)

	StyleDiffStatDel = lipgloss.NewStyle().
		Foreground(ColorDanger)

	StyleDiffAddLine = lipgloss.NewStyle().
		Foreground(ColorSuccess).
		Background(ColorDiffAddBg)

	StyleDiffDelLine = lipgloss.NewStyle().
		Foreground(ColorDanger).
		Background(ColorDiffDelBg)

	StyleDiffHunkLine = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorActive).
		Background(ColorDiffHunkBg)

	StyleConfigInterface = lipgloss.NewStyle().
		Foreground(ColorSyntaxInterface)

	StyleConfigNetwork = lipgloss.NewStyle().
		Foreground(ColorSyntaxNetwork)

	StyleConfigSystem = lipgloss.NewStyle().
		Foreground(ColorSyntaxSystem)

	StyleConfigACL = lipgloss.NewStyle().
		Foreground(ColorSyntaxACL)

	StyleConfigDefault = lipgloss.NewStyle().
		Foreground(ColorText)

	StyleConfigKey = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorSyntaxInterface)

	StyleConfigValue = lipgloss.NewStyle().
		Foreground(ColorSyntaxValue)

	StyleConfigPunctuation = lipgloss.NewStyle().
		Foreground(ColorMuted)

	StyleFooterContainer = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorMuted).
		Padding(0, 1)

	StyleKeybindKey = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorActive).
		Background(ColorSurface)

	StyleKeybindDesc = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Background(ColorSurface)

	StyleModalBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorActive).
		BorderBackground(ColorSurface).
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(1, 2)

	StyleModalTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		MarginBottom(1)
}

// AvailableThemes returns all registered built-in themes.
func AvailableThemes() []Theme {
	return modals.AvailableThemes()
}

// CurrentTheme returns the currently active Theme.
func CurrentTheme() Theme {
	return modals.CurrentTheme()
}

// NormalizeThemeName canonicalizes a theme identifier.
func NormalizeThemeName(name string) string {
	return modals.NormalizeThemeName(name)
}

// SetTheme activates the specified theme by name, display name, or alias.
func SetTheme(name string) (Theme, bool) {
	return modals.SetTheme(name)
}

// NextTheme cycles to the next registered theme and returns it.
func NextTheme() Theme {
	return modals.NextTheme()
}

// SetColorProfile configures the Lip Gloss color profile ("truecolor", "256", "16", "ascii").
func SetColorProfile(profile string) error {
	return modals.SetColorProfile(profile)
}

// StyleTextInput applies explicit foreground and background styles to a textinput.Model.
func StyleTextInput(ti *textinput.Model, bg, fg, dimFg, accentFg lipgloss.Color) {
	modals.StyleTextInput(ti, bg, fg, dimFg, accentFg)
}

// PaintLine wraps or pads a single line to width while preserving and re-injecting base surface SGR.
func PaintLine(line string, width int, bg, defaultFg lipgloss.Color) string {
	return modals.PaintLine(line, width, bg, defaultFg)
}

// PaintBarLine forces line onto a single visual row of exact width with bg and defaultFg.
func PaintBarLine(line string, width int, bg, defaultFg lipgloss.Color) string {
	return modals.PaintBarLine(line, width, bg, defaultFg)
}

// PaintSurface paints a multi-line block to exact width and height with bg and defaultFg.
func PaintSurface(block string, width, height int, bg, defaultFg lipgloss.Color) string {
	return modals.PaintSurface(block, width, height, bg, defaultFg)
}

func styleSGR(bg, fg lipgloss.Color, bold, italic, underline bool) string {
	return modals.StyleSGR(bg, fg, bold, italic, underline)
}

func wrapSGR(sgr, text string) string {
	return modals.WrapSGR(sgr, text)
}

// RenderTitledBox renders a rounded-border box with an embedded top-border title
// (`╭─ <title> ───╮`, `│`, `╰───╯`) and exact outer dimensions width x height.
// Every row preserves full-bleed ANSI background recovery across all color themes.
// Plain titles are rendered bold in ColorText on titleBg; pre-styled ANSI titles
// preserve their styling while recovering titleBg across resets.
func RenderTitledBox(title string, body string, width, height int, borderFg lipgloss.Color, titleBg, bodyBg lipgloss.Color) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if bodyBg == "" {
		bodyBg = ColorBackground
	}
	if titleBg == "" {
		titleBg = bodyBg
	}
	if borderFg == "" {
		borderFg = ColorBorder
	}

	b := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(borderFg).Background(bodyBg)

	if width == 1 {
		return PaintSurface(strings.Repeat(b.Left+"\n", height-1)+b.Left, 1, height, bodyBg, borderFg)
	}

	// Build top border row: ╭─ <title> ─...─╮
	cleanTitle := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(title, "\r", ""), "\n", " "))
	var topRow string
	if cleanTitle != "" && width >= 7 {
		maxTitleW := width - 6 // ╭─ (2) + space (1) + title + space (1) + ─ (1+) + ╮ (1)
		titleStr := cleanTitle
		if ansi.PrintableRuneWidth(titleStr) > maxTitleW {
			titleStr = truncate.String(titleStr, uint(maxTitleW))
		}
		hasANSI := strings.Contains(titleStr, "\x1b[")
		if hasANSI && !strings.HasSuffix(titleStr, "\x1b[0m") {
			titleStr += "\x1b[0m"
		}
		titleW := ansi.PrintableRuneWidth(titleStr)
		fillW := width - 5 - titleW
		if fillW < 1 {
			fillW = 1
		}

		var titleSeg string
		if hasANSI {
			titleSeg = PaintBarLine(" "+titleStr+" ", titleW+2, titleBg, ColorText)
		} else {
			titleSeg = lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(titleBg).Render(" " + titleStr + " ")
		}

		leftCorner := borderStyle.Render(b.TopLeft + b.Top)
		rightEdge := borderStyle.Render(strings.Repeat(b.Top, fillW) + b.TopRight)
		topRow = PaintBarLine(leftCorner+titleSeg+rightEdge, width, bodyBg, borderFg)
	} else {
		topRow = PaintBarLine(b.TopLeft+strings.Repeat(b.Top, width-2)+b.TopRight, width, bodyBg, borderFg)
	}

	if height == 1 {
		return topRow
	}

	rows := make([]string, 0, height)
	rows = append(rows, topRow)

	innerH := height - 2
	innerW := width - 2
	if innerH > 0 {
		leftBar := borderStyle.Render(b.Left)
		rightBar := borderStyle.Render(b.Right)
		if innerW > 0 {
			paintedBody := PaintSurface(body, innerW, innerH, bodyBg, ColorText)
			for _, line := range strings.Split(paintedBody, "\n") {
				rows = append(rows, leftBar+line+rightBar)
			}
		} else {
			for i := 0; i < innerH; i++ {
				rows = append(rows, leftBar+rightBar)
			}
		}
	}

	bottomRow := PaintBarLine(b.BottomLeft+strings.Repeat(b.Bottom, width-2)+b.BottomRight, width, bodyBg, borderFg)
	rows = append(rows, bottomRow)

	return strings.Join(rows, "\n")
}

// ScrollbarMetrics computes the exact vertical thumb position and range for a viewport
// scrollbar of the given track height, total line count, and current YOffset.
func ScrollbarMetrics(height, totalLines, yOffset int) (thumbStart, thumbH, maxOffset, trackRange int) {
	if height <= 0 || totalLines <= height {
		return 0, height, 0, 0
	}

	thumbH = (height*height + totalLines/2) / totalLines
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > height {
		thumbH = height
	}

	maxOffset = totalLines - height
	if yOffset < 0 {
		yOffset = 0
	}
	if yOffset > maxOffset {
		yOffset = maxOffset
	}

	trackRange = height - thumbH
	if maxOffset > 0 && trackRange > 0 {
		thumbStart = (yOffset*trackRange + maxOffset/2) / maxOffset
		if thumbStart < 0 {
			thumbStart = 0
		}
		if thumbStart > trackRange {
			thumbStart = trackRange
		}
	}
	return thumbStart, thumbH, maxOffset, trackRange
}

// ScrollbarYOffsetForThumb maps a thumb top row (0..trackRange) back to a viewport YOffset
// (0..maxOffset) using the exact inverse rounding of ScrollbarMetrics.
func ScrollbarYOffsetForThumb(thumbStart, height, totalLines int) int {
	_, _, maxOffset, trackRange := ScrollbarMetrics(height, totalLines, 0)
	if maxOffset <= 0 {
		return 0
	}
	if trackRange <= 0 {
		if thumbStart > 0 {
			return maxOffset
		}
		return 0
	}
	if thumbStart <= 0 {
		return 0
	}
	if thumbStart >= trackRange {
		return maxOffset
	}
	yOff := (thumbStart*maxOffset + trackRange/2) / trackRange
	if yOff < 0 {
		return 0
	}
	if yOff > maxOffset {
		return maxOffset
	}
	return yOff
}

// RenderScrollbar returns a height-element slice of 1-column-wide styled scrollbar cells
// using █ (thumb) and ░ (track) painted on bg. When totalLines <= height, returns
// blank cells painted on bg so short viewports stay visually quiet.
func RenderScrollbar(height, totalLines, yOffset int, bg lipgloss.Color) []string {
	if height <= 0 {
		return nil
	}
	if bg == "" {
		bg = ColorPanel
	}

	cells := make([]string, height)
	if totalLines <= height {
		blank := lipgloss.NewStyle().Background(bg).Foreground(ColorBorder).Render(" ")
		for i := range cells {
			cells[i] = blank
		}
		return cells
	}

	thumbStart, thumbH, _, _ := ScrollbarMetrics(height, totalLines, yOffset)

	thumbCell := lipgloss.NewStyle().Foreground(ColorActive).Background(bg).Render(GlyphScrollThumb)
	trackCell := lipgloss.NewStyle().Foreground(ColorBorder).Background(bg).Render(GlyphScrollTrack)

	for i := 0; i < height; i++ {
		if i >= thumbStart && i < thumbStart+thumbH {
			cells[i] = thumbCell
		} else {
			cells[i] = trackCell
		}
	}
	return cells
}
