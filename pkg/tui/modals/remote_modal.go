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

const (
	RemoteFocusURL      = 0
	RemoteFocusAutoPush = 1
	RemoteFocusCopyKey  = 2
	RemoteFocusPushNow  = 3
	RemoteFocusSave     = 4
	RemoteFocusCancel   = 5
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
	ti.Width = 48
	StyleTextInput(&ti, ColorSurface, ColorText, ColorTextDim, ColorActive)
	ti.Focus()

	return RemoteModalModel{
		Config:       cfg,
		URLInput:     ti,
		AutoPush:     cfg.AutoPush,
		FocusedIndex: RemoteFocusURL,
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

func (m *RemoteModalModel) setFocus(idx int) {
	m.FocusedIndex = idx
	if m.FocusedIndex == RemoteFocusURL {
		m.URLInput.Focus()
	} else {
		m.URLInput.Blur()
	}
}

func (m *RemoteModalModel) activateFocused() (save, pushNow, close bool) {
	switch m.FocusedIndex {
	case RemoteFocusURL:
		m.setFocus(RemoteFocusAutoPush)
		return false, false, false
	case RemoteFocusAutoPush:
		m.AutoPush = !m.AutoPush
		return false, false, false
	case RemoteFocusCopyKey:
		CopyToClipboard(m.Config.PublicKey)
		m.StatusMsg = "✓ Public key copied to clipboard & saved to /tmp/srl_timeline_deploy_key.pub"
		m.IsError = false
		return false, false, false
	case RemoteFocusPushNow:
		m.Config.URL = strings.TrimSpace(m.URLInput.Value())
		m.Config.AutoPush = m.AutoPush
		return true, true, false
	case RemoteFocusSave:
		m.Config.URL = strings.TrimSpace(m.URLInput.Value())
		m.Config.AutoPush = m.AutoPush
		return true, false, true
	case RemoteFocusCancel:
		return false, false, true
	}
	return false, false, false
}

// View renders the remote Git dialog.
func (m RemoteModalModel) View() string {
	innerW, vPad, hPad, maxInnerH := modalBoxMetrics(m.Width, m.Height, 66)
	compact := m.Height > 0 && m.Height <= 24
	ultraCompact := m.Height > 0 && m.Height <= 14

	urlW := innerW - 18
	if urlW < 8 {
		urlW = 8
	}
	m.URLInput.Width = urlW
	StyleTextInput(&m.URLInput, ColorSurface, ColorText, ColorTextDim, ColorActive)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnAccent).
		Background(ColorPrimary).
		Padding(0, 1).
		Render("◆ REMOTE GIT REPOSITORY SETTINGS")

	// 1. URL Input Box
	urlBorderFg := ColorBorder
	if m.FocusedIndex == RemoteFocusURL {
		urlBorderFg = ColorActive
	}
	var urlView string
	if ultraCompact {
		prefix := "  "
		if m.FocusedIndex == RemoteFocusURL {
			prefix = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("▸ ")
		}
		urlView = PaintBarLine(prefix+m.URLInput.View(), innerW, ColorSurface, ColorText)
	} else {
		urlInnerW := innerW - 4
		if urlInnerW < 4 {
			urlInnerW = 4
		}
		urlInner := PaintBarLine(m.URLInput.View(), urlInnerW, ColorSurface, ColorText)
		urlView = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(urlBorderFg).
			BorderBackground(ColorSurface).
			Background(ColorSurface).
			Foreground(ColorText).
			Padding(0, 1).
			Render(urlInner)
	}

	// 2. Auto-push Checkbox
	checkMark := "[ ]"
	if m.AutoPush {
		checkMark = "[✓]"
	}
	autoPushLabel := fmt.Sprintf("%s Auto-push on every configuration commit", checkMark)
	autoPushStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorSurface)
	if m.FocusedIndex == RemoteFocusAutoPush {
		autoPushStyle = autoPushStyle.
			Background(ColorPrimary).
			Foreground(ColorOnAccent).
			Bold(true)
	} else if m.AutoPush {
		autoPushStyle = autoPushStyle.Foreground(ColorSuccess).Bold(true)
	} else {
		autoPushStyle = autoPushStyle.Foreground(ColorMuted)
	}
	autoPushView := PaintBarLine(autoPushStyle.Render(autoPushLabel), innerW, ColorSurface, ColorText)

	// 3. Action Buttons — compact padding so terminals < 72 cols never clip or wrap awkwardly
	btnCopyStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusedIndex == RemoteFocusCopyKey {
		btnCopyStyle = btnCopyStyle.Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true)
	}
	btnCopy := btnCopyStyle.Render("Copy Key")

	btnPushStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusedIndex == RemoteFocusPushNow {
		btnPushStyle = btnPushStyle.Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true)
	}
	btnPush := btnPushStyle.Render("Push Now")

	btnSaveStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusedIndex == RemoteFocusSave {
		btnSaveStyle = btnSaveStyle.Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnSave := btnSaveStyle.Render("Save")

	btnCancelStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorMuted)
	if m.FocusedIndex == RemoteFocusCancel {
		btnCancelStyle = btnCancelStyle.Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnCancel := btnCancelStyle.Render("Cancel")

	gap := lipgloss.NewStyle().Background(ColorSurface).Render(" ")
	var buttonsRow string
	if innerW < 40 {
		row1 := PaintBarLine(lipgloss.JoinHorizontal(lipgloss.Center, btnCopy, gap, btnPush), innerW, ColorSurface, ColorText)
		row2 := PaintBarLine(lipgloss.JoinHorizontal(lipgloss.Center, btnSave, gap, btnCancel), innerW, ColorSurface, ColorText)
		buttonsRow = row1 + "\n" + row2
	} else {
		buttonsRow = PaintBarLine(
			lipgloss.JoinHorizontal(lipgloss.Center, btnCopy, gap, btnPush, gap, btnSave, gap, btnCancel),
			innerW,
			ColorSurface,
			ColorText,
		)
	}

	// 4. Status / Error Message
	var statusView string
	if m.StatusMsg != "" {
		statusStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
		if m.IsError {
			statusStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorDanger)
		}
		statusView = PaintBarLine(statusStyle.Render("Status: "+truncateDisplay(m.StatusMsg, innerW-8)), innerW, ColorSurface, ColorText)
	} else if m.Config.SyncStatus != "" {
		statusView = PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Render("Sync Status: "+truncateDisplay(m.Config.SyncStatus, innerW-13)),
			innerW,
			ColorSurface,
			ColorMuted,
		)
	}

	// 5. Public Key Display (for easy copying to GitHub Deploy Keys)
	var pubKeyLines []string
	if m.Config.PublicKey != "" && !ultraCompact {
		keyHeader := PaintBarLine(
			lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render("● Device Public SSH Key (Deploy Key):"),
			innerW,
			ColorSurface,
			ColorWarning,
		)
		pubKeyDisplay := m.Config.PublicKey
		if compact {
			pubKeyDisplay = truncateDisplay(pubKeyDisplay, innerW-2)
			keyBox := PaintBarLine(
				lipgloss.NewStyle().Foreground(ColorActive).Render(" "+pubKeyDisplay),
				innerW,
				ColorBackground,
				ColorActive,
			)
			pubKeyLines = append(pubKeyLines, keyHeader, keyBox)
		} else {
			keyBox := PaintLine(
				lipgloss.NewStyle().Foreground(ColorActive).Render(" "+pubKeyDisplay),
				innerW,
				ColorBackground,
				ColorActive,
			)
			keyHint := PaintBarLine(
				lipgloss.NewStyle().Foreground(ColorMuted).Render("▸ Use [Copy Key] or cat /tmp/srl_timeline_deploy_key.pub"),
				innerW,
				ColorSurface,
				ColorMuted,
			)
			pubKeyLines = append(pubKeyLines, keyHeader, keyBox, keyHint)
		}
	}

	footer := renderModalFooter(
		[]string{"Tab/↑↓←→ Move", "Space/Enter Select", "c Copy Key", "Esc Close"},
		"Tab/←→ Move · Enter OK · Esc Close",
		innerW,
		!ultraCompact,
	)

	var sections []string
	sections = append(sections, PaintBarLine(title, innerW, ColorSurface, ColorText))
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, urlView)
	sections = append(sections, autoPushView)
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, buttonsRow)
	if statusView != "" {
		if !compact {
			sections = append(sections, "")
		}
		sections = append(sections, statusView)
	}
	if len(pubKeyLines) > 0 {
		sections = append(sections, strings.Join(pubKeyLines, "\n"))
	}
	if !compact {
		sections = append(sections, "")
	}
	sections = append(sections, footer)

	content := strings.Join(sections, "\n")
	return renderCenteredModal(content, m.Width, m.Height, innerW, vPad, hPad, maxInnerH, ColorPurple)
}

// Update processes remote settings events.
// Returns (model, save, pushNow, close).
func (m RemoteModalModel) Update(msg tea.Msg) (RemoteModalModel, bool, bool, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, false, false, true

		case "tab", "down":
			m.setFocus((m.FocusedIndex + 1) % 6)
			return m, false, false, false

		case "shift+tab", "up":
			m.setFocus((m.FocusedIndex + 5) % 6)
			return m, false, false, false

		case "right":
			if m.FocusedIndex >= RemoteFocusCopyKey && m.FocusedIndex <= RemoteFocusCancel {
				next := m.FocusedIndex + 1
				if next > RemoteFocusCancel {
					next = RemoteFocusCopyKey
				}
				m.setFocus(next)
				return m, false, false, false
			}

		case "left":
			if m.FocusedIndex >= RemoteFocusCopyKey && m.FocusedIndex <= RemoteFocusCancel {
				prev := m.FocusedIndex - 1
				if prev < RemoteFocusCopyKey {
					prev = RemoteFocusCancel
				}
				m.setFocus(prev)
				return m, false, false, false
			}

		case " ", "space":
			if m.FocusedIndex != RemoteFocusURL {
				save, pushNow, close := m.activateFocused()
				return m, save, pushNow, close
			}

		case "c", "k":
			if m.FocusedIndex != RemoteFocusURL {
				CopyToClipboard(m.Config.PublicKey)
				m.StatusMsg = "✓ Public key copied to clipboard & saved to /tmp/srl_timeline_deploy_key.pub"
				m.IsError = false
				return m, false, false, false
			}

		case "enter":
			save, pushNow, close := m.activateFocused()
			return m, save, pushNow, close
		}
	}

	if m.FocusedIndex == RemoteFocusURL {
		var cmd tea.Cmd
		m.URLInput, cmd = m.URLInput.Update(msg)
		_ = cmd
	}

	return m, false, false, false
}
