package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func init() {
	if os.Getenv("NO_COLOR") == "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
}

var (
	// Palette
	ColorBackground = lipgloss.Color("#0d1117")
	ColorSurface    = lipgloss.Color("#161b22")
	ColorPanel      = lipgloss.Color("#090d13")
	ColorBorder     = lipgloss.Color("#30363d")
	ColorActive     = lipgloss.Color("#58a6ff")
	ColorPrimary    = lipgloss.Color("#1f6feb")
	ColorSuccess    = lipgloss.Color("#3fb950")
	ColorWarning    = lipgloss.Color("#d29922")
	ColorDanger     = lipgloss.Color("#f85149")
	ColorMuted      = lipgloss.Color("#8b949e")
	ColorText       = lipgloss.Color("#e6edf3")
	ColorTextDim    = lipgloss.Color("#7d8590")
	ColorHighlight  = lipgloss.Color("#1c2128")
	ColorPurple     = lipgloss.Color("#bc8cff")
	ColorCyan       = lipgloss.Color("#39c5cf")

	// Header Styles
	StyleHeaderContainer = lipgloss.NewStyle().
				Background(ColorSurface).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottomForeground(ColorBorder).
				Padding(0, 1)

	StyleAppTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(ColorPrimary).
			Padding(0, 1).
			MarginRight(1)

	StyleBadgeDevice = lipgloss.NewStyle().
				Background(lipgloss.Color("#21262d")).
				Foreground(ColorCyan).
				Bold(true).
				Padding(0, 1).
				MarginRight(1)

	StyleBadgeVersion = lipgloss.NewStyle().
				Background(lipgloss.Color("#21262d")).
				Foreground(ColorMuted).
				Padding(0, 1).
				MarginRight(1)

	StyleBadgeActive = lipgloss.NewStyle().
				Background(lipgloss.Color("#238636")).
				Foreground(lipgloss.Color("#ffffff")).
				Bold(true).
				Padding(0, 1).
				MarginRight(1)

	StyleBadgeSync = lipgloss.NewStyle().
			Background(lipgloss.Color("#21262d")).
			Foreground(ColorActive).
			Bold(true).
			Padding(0, 1)

	// Filter Bar Styles
	StyleFilterContainer = lipgloss.NewStyle().
				Background(ColorSurface).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottomForeground(ColorBorder).
				Padding(0, 1)

	StyleFilterLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorMuted).
				MarginRight(1)

	StyleFilterBadge = lipgloss.NewStyle().
				Background(lipgloss.Color("#21262d")).
				Foreground(ColorActive).
				Bold(true).
				Padding(0, 1).
				MarginLeft(1)

	// Panel Styles
	StylePanelHeader = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorMuted).
				Background(ColorSurface).
				Padding(0, 1)

	StyleActiveBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorActive)

	StyleInactiveBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorder)

	// Tab Styles
	StyleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(ColorPrimary).
			Padding(0, 1).
			MarginRight(1)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Background(lipgloss.Color("#21262d")).
				Padding(0, 1).
				MarginRight(1)

	// Visual Timeline Graph Styles
	StyleTimelineNodeActive = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorActive)

	StyleTimelineNodeNormal = lipgloss.NewStyle().
				Foreground(ColorMuted)

	StyleTimelineNodeMilestone = lipgloss.NewStyle().
					Bold(true).
					Foreground(ColorWarning)

	StyleTimelineLine = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#30363d"))

	StyleTimelineSelectedRow = lipgloss.NewStyle().
					Background(ColorHighlight).
					Bold(true)

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

	// Footer Styles
	StyleFooterContainer = lipgloss.NewStyle().
				Background(ColorSurface).
				BorderTop(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderTopForeground(ColorBorder).
				Padding(0, 1)

	StyleKeybindKey = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorActive)

	StyleKeybindDesc = lipgloss.NewStyle().
				Foreground(ColorMuted).
				MarginRight(2)

	// Modal Dialog Styles
	StyleModalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorActive).
			Background(ColorSurface).
			Padding(1, 2)

	StyleModalTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(ColorPrimary).
			Padding(0, 1).
			MarginBottom(1)
)
