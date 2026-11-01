package modals

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/models"
)

// RemoteModalModel handles remote Git repository settings with interactive form navigation.
type RemoteModalModel struct {
	Config       models.RemoteRepoConfig
	URLInput     textinput.Model
	AutoPush     bool
	FocusedIndex int // 0: URL, 1: AutoPush, 2: CopyKey, 3: PushNow, 4: Save, 5: Cancel
	StatusMsg    string
	IsError      bool
	Width        int
	Height       int
}

// NewRemoteModal creates a remote settings modal.
func NewRemoteModal(cfg models.RemoteRepoConfig, width, height int) RemoteModalModel {
	ti := textinput.New()
	ti.Placeholder = "git@github.com:org/srl-configs.git"
	ti.Prompt = "Remote URL: "
	ti.SetValue(cfg.URL)
	ti.Width = 50
	StyleTextInput(&ti, ColorSurface, ColorText, ColorTextDim, ColorActive)
	ti.Focus()

	return RemoteModalModel{
		Config:       cfg,
		URLInput:     ti,
		AutoPush:     cfg.AutoPush,
		FocusedIndex: 0,
		Width:        width,
		Height:       height,
	}
}

// CopyToClipboard copies text using OSC 52 escape sequences and saves to /tmp file.
func CopyToClipboard(text string) {
	if text == "" {
		return
	}
	_ = os.WriteFile("/tmp/srl_timeline_deploy_key.pub", []byte(text+"\n"), 0644)
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	var osc52 string
	if os.Getenv("TMUX") != "" {
		osc52 = fmt.Sprintf("\x1bPtmux;\x1b\x1b]52;c;%s\x07\x1b\\", encoded)
	} else if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		osc52 = fmt.Sprintf("\x1bP\x1b]52;c;%s\x07\x1b\\", encoded)
	} else {
		osc52 = fmt.Sprintf("\x1b]52;c;%s\x07", encoded)
	}
	_, _ = fmt.Fprint(os.Stdout, osc52)
}

// View renders the remote Git dialog.
func (m RemoteModalModel) View() string {
	StyleTextInput(&m.URLInput, ColorSurface, ColorText, ColorTextDim, ColorActive)
	innerW := 68
	if m.Width > 0 && m.Width-6 < innerW {
		innerW = m.Width - 6
	}
	if innerW < 10 {
		innerW = 10
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPurple).
		Padding(0, 1).
		Render("🌐 REMOTE GIT REPOSITORY SETTINGS")

	// 1. URL Input Box
	urlBorderFg := ColorBorder
	if m.FocusedIndex == 0 {
		urlBorderFg = ColorActive
	}
	urlBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(urlBorderFg).
		BorderBackground(ColorSurface).
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(0, 1)
	urlInnerW := innerW - 4
	if urlInnerW < 4 {
		urlInnerW = 4
	}
	urlInner := PaintBarLine(m.URLInput.View(), urlInnerW, ColorSurface, ColorText)
	urlView := urlBoxStyle.Render(urlInner)

	// 2. Auto-push Checkbox
	checkMark := "[ ]"
	if m.AutoPush {
		checkMark = "[✓]"
	}
	autoPushLabel := fmt.Sprintf("%s Auto-push on every configuration commit", checkMark)
	autoPushStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorSurface)
	if m.FocusedIndex == 1 {
		autoPushStyle = autoPushStyle.
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true)
	} else if m.AutoPush {
		autoPushStyle = autoPushStyle.Foreground(ColorSuccess).Bold(true)
	} else {
		autoPushStyle = autoPushStyle.Foreground(ColorMuted)
	}
	autoPushView := autoPushStyle.Render(autoPushLabel)

	// 3. Action Buttons
	btnCopyStyle := lipgloss.NewStyle().Padding(0, 2).MarginRight(1).MarginBackground(ColorSurface).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusedIndex == 2 {
		btnCopyStyle = btnCopyStyle.Background(ColorWarning).Foreground(ColorOnAccent).Bold(true)
	}
	btnCopy := btnCopyStyle.Render("📋 Copy Key")

	btnPushStyle := lipgloss.NewStyle().Padding(0, 2).MarginRight(1).MarginBackground(ColorSurface).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusedIndex == 3 {
		btnPushStyle = btnPushStyle.Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true)
	}
	btnPush := btnPushStyle.Render("🚀 Push Now")

	btnSaveStyle := lipgloss.NewStyle().Padding(0, 2).MarginRight(1).MarginBackground(ColorSurface).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusedIndex == 4 {
		btnSaveStyle = btnSaveStyle.Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnSave := btnSaveStyle.Render("💾 Save & Close")

	btnCancelStyle := lipgloss.NewStyle().Padding(0, 2).Background(ColorBadgeBg).Foreground(ColorMuted)
	if m.FocusedIndex == 5 {
		btnCancelStyle = btnCancelStyle.Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnCancel := btnCancelStyle.Render("❌ Cancel")

	buttonsRow := lipgloss.JoinHorizontal(lipgloss.Center, btnCopy, btnPush, btnSave, btnCancel)

	// 4. Public Key Display (for easy copying to GitHub Deploy Keys)
	var pubKeySection string
	if m.Config.PublicKey != "" {
		keyHeader := lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render("🔑 Device Public SSH Key (Add to GitHub Deploy Keys):")
		keyBox := PaintLine(
			lipgloss.NewStyle().Foreground(ColorActive).Render(" "+m.Config.PublicKey),
			innerW,
			ColorBackground,
			ColorActive,
		)
		keyHint := lipgloss.NewStyle().Foreground(ColorMuted).Render("💡 Use [Copy Key] or cat /tmp/srl_timeline_deploy_key.pub")
		pubKeySection = fmt.Sprintf("\n%s\n%s\n%s", keyHeader, keyBox, keyHint)
	}

	// 5. Status / Error Message
	var statusView string
	if m.StatusMsg != "" {
		statusStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
		if m.IsError {
			statusStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorDanger)
		}
		statusView = statusStyle.Render(fmt.Sprintf("Status: %s", m.StatusMsg))
	} else if m.Config.SyncStatus != "" {
		statusView = lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("Sync Status: %s", m.Config.SyncStatus))
	}

	footerRaw := lipgloss.NewStyle().Foreground(ColorMuted).Render(
		"[Tab/↓/↑] Navigate fields  |  [Space/Enter] Toggle / Activate  |  [c] Copy Key  |  [Esc] Close",
	)
	footer := PaintLine(footerRaw, innerW, ColorSurface, ColorMuted)

	compact := m.Height > 0 && m.Height <= 24
	vPad := 1
	var content string
	if compact {
		vPad = 0
		content = fmt.Sprintf("%s\n%s\n%s\n%s\n%s%s\n%s",
			title,
			urlView,
			autoPushView,
			buttonsRow,
			statusView,
			pubKeySection,
			footer,
		)
	} else {
		content = fmt.Sprintf("%s\n\n%s\n%s\n\n%s\n\n%s%s\n\n%s",
			title,
			urlView,
			autoPushView,
			buttonsRow,
			statusView,
			pubKeySection,
			footer,
		)
	}

	paintedContent := PaintSurface(content, innerW, 0, ColorSurface, ColorText)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPurple).
		BorderBackground(ColorSurface).
		Background(ColorSurface).
		Foreground(ColorText).
		Padding(vPad, 2).
		Render(paintedContent)

	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(ColorBackground),
		lipgloss.WithWhitespaceForeground(ColorTextDim),
	)
}

// Update processes remote settings events.
func (m RemoteModalModel) Update(msg tea.Msg) (RemoteModalModel, bool, bool, bool) {
	// Returns (model, save, pushNow, close)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, false, false, true

		case "tab", "down":
			m.FocusedIndex = (m.FocusedIndex + 1) % 6
			if m.FocusedIndex == 0 {
				m.URLInput.Focus()
			} else {
				m.URLInput.Blur()
			}
			return m, false, false, false

		case "shift+tab", "up":
			m.FocusedIndex = (m.FocusedIndex + 5) % 6
			if m.FocusedIndex == 0 {
				m.URLInput.Focus()
			} else {
				m.URLInput.Blur()
			}
			return m, false, false, false

		case " ", "space":
			if m.FocusedIndex == 1 {
				m.AutoPush = !m.AutoPush
				return m, false, false, false
			}

		case "c", "k":
			if m.FocusedIndex != 0 {
				CopyToClipboard(m.Config.PublicKey)
				m.StatusMsg = "✓ Public key copied to clipboard & saved to /tmp/srl_timeline_deploy_key.pub!"
				m.IsError = false
				return m, false, false, false
			}

		case "enter":
			switch m.FocusedIndex {
			case 0:
				// Advance from URL to auto-push
				m.FocusedIndex = 1
				m.URLInput.Blur()
				return m, false, false, false
			case 1:
				m.AutoPush = !m.AutoPush
				return m, false, false, false
			case 2:
				// Copy key
				CopyToClipboard(m.Config.PublicKey)
				m.StatusMsg = "✓ Public key copied to clipboard & saved to /tmp/srl_timeline_deploy_key.pub!"
				m.IsError = false
				return m, false, false, false
			case 3:
				// Push now
				m.Config.URL = strings.TrimSpace(m.URLInput.Value())
				m.Config.AutoPush = m.AutoPush
				return m, true, true, false
			case 4:
				// Save & Close
				m.Config.URL = strings.TrimSpace(m.URLInput.Value())
				m.Config.AutoPush = m.AutoPush
				return m, true, false, true
			case 5:
				// Cancel
				return m, false, false, true
			}
		}
	}

	if m.FocusedIndex == 0 {
		var cmd tea.Cmd
		m.URLInput, cmd = m.URLInput.Update(msg)
		_ = cmd
	}

	return m, false, false, false
}
