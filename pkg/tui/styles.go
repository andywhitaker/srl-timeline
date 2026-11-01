package tui

import (
	"timeline/pkg/tui/modals"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// Theme defines a complete semantic color palette for the TUI.
type Theme = modals.Theme

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
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottomForeground(ColorBorder).
		BorderBottomBackground(ColorSurface).
		Padding(0, 1)

	StyleAppTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorSurface)
	StyleHeaderApp = StyleAppTitle

	StyleBadgeDevice = lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorCyan).
		Bold(true).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorSurface)
	StyleHeaderTarget = StyleBadgeDevice

	StyleBadgeVersion = lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorMuted).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorSurface)

	StyleBadgeActive = lipgloss.NewStyle().
		Background(ColorSuccessBg).
		Foreground(ColorOnAccent).
		Bold(true).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorSurface)
	StyleHeaderDaemonRun = StyleBadgeActive

	StyleHeaderDaemonStop = lipgloss.NewStyle().
		Background(ColorMuted).
		Foreground(ColorOnAccent).
		Bold(true).
		Padding(0, 1).
		MarginRight(1).
		MarginBackground(ColorSurface)

	StyleBadgeSync = lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorActive).
		Bold(true).
		Padding(0, 1)

	StyleFilterContainer = lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorText).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottomForeground(ColorBorder).
		BorderBottomBackground(ColorSurface).
		Padding(0, 1)

	StyleFilterContainerFocused = lipgloss.NewStyle().
		Background(ColorSurfaceFocus).
		Foreground(ColorText).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottomForeground(ColorActive).
		BorderBottomBackground(ColorSurfaceFocus).
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
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTopForeground(ColorBorder).
		BorderTopBackground(ColorSurface).
		Padding(0, 1)

	StyleKeybindKey = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorActive).
		Background(ColorSurface)

	StyleKeybindDesc = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Background(ColorSurface).
		MarginRight(2).
		MarginBackground(ColorSurface)

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
