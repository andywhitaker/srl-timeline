package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

// HeaderBarModel displays top device status and monitoring metadata.
type HeaderBarModel struct {
	Hostname   string
	Version    string
	Chassis    string
	SyncStatus string
	IsRealtime bool
	Width      int
}

// NewHeaderBarModel creates a new header bar.
func NewHeaderBarModel(width int) HeaderBarModel {
	return HeaderBarModel{
		Hostname:   "srl-timeline",
		Version:    "SR Linux v26.7.1",
		Chassis:    "7220 IXR-D2L",
		SyncStatus: "Local Only",
		IsRealtime: true,
		Width:      width,
	}
}

type headerLayout struct {
	line        string
	themeStartX int
	themeEndX   int
}

func (m HeaderBarModel) computeLayout() headerLayout {
	if m.Width <= 0 {
		return headerLayout{themeStartX: -1, themeEndX: -1}
	}
	if m.Width < 4 {
		return headerLayout{
			line:        PaintBarLine("srl", m.Width, ColorSurface, ColorActive),
			themeStartX: -1,
			themeEndX:   -1,
		}
	}

	innerW := m.Width - 2 // 1-col left and right padding

	sep := lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorSurface).Render(" · ")
	sepW := lipgloss.Width(sep)

	brandStr := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorSurface).Render("srl-timeline")
	brandW := lipgloss.Width(brandStr)

	var statusStr string
	if m.IsRealtime {
		statusStr = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Background(ColorSurface).Render(GlyphLive + " LIVE")
	} else {
		statusStr = lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorSurface).Render(GlyphPaused + " PAUSED")
	}
	statusW := lipgloss.Width(statusStr)

	themeName := CurrentTheme().DisplayName
	themePill := lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorPurple).
		Bold(true).
		Padding(0, 1).
		Render(themeName)
	themePillW := lipgloss.Width(themePill)

	hostStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(ColorSurface)
	verStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSurface)
	chassisStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorSurface)
	syncStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSurface)

	// Start with core mandatory elements: brand (left) and LIVE/PAUSED (right).
	leftParts := []string{brandStr}
	leftW := brandW

	rightBase := statusStr
	rightW := statusW
	showTheme := false
	themeGapW := 2
	if m.Width < 60 {
		themeGapW = 1
	}

	const minMidGap = 1

	// 1. Add Hostname on left if space permits.
	if m.Hostname != "" {
		hostStr := hostStyle.Render(m.Hostname)
		hostW := lipgloss.Width(hostStr)
		if leftW+sepW+hostW+minMidGap+rightW <= innerW {
			leftParts = append(leftParts, sep, hostStr)
			leftW += sepW + hostW
		} else {
			// On very narrow terminals (e.g. 40 cols with long hostnames), compact the hostname cleanly.
			availHost := innerW - leftW - sepW - minMidGap - rightW
			if availHost >= 4 {
				shortHost := truncate.StringWithTail(m.Hostname, uint(availHost), "…")
				hostStr = hostStyle.Render(shortHost)
				leftParts = append(leftParts, sep, hostStr)
				leftW += sepW + lipgloss.Width(hostStr)
			}
		}
	}

	// 2. Add clickable theme pill on right if it fits.
	if themeName != "" && leftW+minMidGap+rightW+themeGapW+themePillW <= innerW {
		showTheme = true
		rightW += themeGapW + themePillW
	}

	// 3. Add Version on left if space permits.
	if m.Version != "" {
		verStr := verStyle.Render(m.Version)
		verW := lipgloss.Width(verStr)
		if leftW+sepW+verW+minMidGap+rightW <= innerW {
			leftParts = append(leftParts, sep, verStr)
			leftW += sepW + verW
		}
	}

	// 4. Add SyncStatus on right (between LIVE/PAUSED and theme pill) if space permits.
	showSync := false
	var syncStr string
	if m.SyncStatus != "" {
		syncStr = syncStyle.Render(m.SyncStatus)
		syncW := lipgloss.Width(syncStr)
		if leftW+minMidGap+rightW+sepW+syncW <= innerW {
			showSync = true
			rightW += sepW + syncW
		}
	}

	// 5. Add Chassis on left when wide (>= 135 cols) and space permits.
	if m.Width >= 135 && m.Chassis != "" {
		chassisStr := chassisStyle.Render(m.Chassis)
		chassisW := lipgloss.Width(chassisStr)
		if leftW+sepW+chassisW+minMidGap+rightW <= innerW {
			leftParts = append(leftParts, sep, chassisStr)
			leftW += sepW + chassisW
		}
	}

	// Assemble right block in visual order: [LIVE/PAUSED] [· SyncStatus] [  ThemePill]
	var rightParts []string
	rightParts = append(rightParts, rightBase)
	if showSync {
		rightParts = append(rightParts, sep, syncStr)
	}
	if showTheme {
		gap := lipgloss.NewStyle().Background(ColorSurface).Render(strings.Repeat(" ", themeGapW))
		rightParts = append(rightParts, gap, themePill)
	}

	leftContent := strings.Join(leftParts, "")
	rightContent := strings.Join(rightParts, "")

	// Recompute exact rendered widths after assembly.
	leftW = lipgloss.Width(leftContent)
	rightW = lipgloss.Width(rightContent)

	// Extreme fallback if brand + status alone exceed innerW.
	if leftW+rightW > innerW {
		availLeft := innerW - rightW - 1
		if availLeft < 0 {
			availLeft = 0
		}
		if availLeft == 0 {
			leftContent = ""
			leftW = 0
		} else {
			leftContent = PaintBarLine(leftContent, availLeft, ColorSurface, ColorActive)
			leftW = availLeft
		}
	}

	spacerW := innerW - leftW - rightW
	if spacerW < 0 {
		spacerW = 0
	}
	spacer := lipgloss.NewStyle().Background(ColorSurface).Render(strings.Repeat(" ", spacerW))
	pad := lipgloss.NewStyle().Background(ColorSurface).Render(" ")

	fullLine := PaintBarLine(pad+leftContent+spacer+rightContent+pad, m.Width, ColorSurface, ColorText)

	themeStartX, themeEndX := -1, -1
	if showTheme {
		// Col 0 is left pad; content spans [1, 1+innerW); theme pill is rightmost inside innerW.
		themeEndX = 1 + leftW + spacerW + rightW
		if themeEndX > m.Width-1 {
			themeEndX = m.Width - 1
		}
		themeStartX = themeEndX - themePillW
		if themeStartX < 0 {
			themeStartX = -1
			themeEndX = -1
		}
	}

	return headerLayout{
		line:        fullLine,
		themeStartX: themeStartX,
		themeEndX:   themeEndX,
	}
}

// ThemeHitRange returns the half-open horizontal terminal column range [startX, endX)
// occupied by the clickable theme pill in the rendered header bar, or (-1, -1) if not shown.
func (m HeaderBarModel) ThemeHitRange() (startX, endX int) {
	l := m.computeLayout()
	return l.themeStartX, l.themeEndX
}

// View renders the 1-row borderless top status bar.
func (m HeaderBarModel) View() string {
	return m.computeLayout().line
}
