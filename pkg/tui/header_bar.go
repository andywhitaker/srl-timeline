package tui

import (
	"github.com/charmbracelet/lipgloss"
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

// View renders the top status banner.
func (m HeaderBarModel) View() string {
	title := StyleAppTitle.Render("⏳ TIMELINE")
	device := StyleBadgeDevice.Render("NODE: " + m.Hostname)
	version := StyleBadgeVersion.Render(m.Version)

	realtime := StyleBadgeActive.Render("● REALTIME ACTIVE")
	if !m.IsRealtime {
		realtime = lipgloss.NewStyle().Background(ColorMuted).Foreground(lipgloss.Color("#ffffff")).Padding(0, 1).Render("○ PAUSED")
	}

	sync := StyleBadgeSync.Render("SYNC: " + m.SyncStatus)

	bar := lipgloss.JoinHorizontal(
		lipgloss.Center,
		title,
		device,
		version,
		realtime,
		sync,
	)

	return StyleHeaderContainer.Width(m.Width).Render(bar)
}
