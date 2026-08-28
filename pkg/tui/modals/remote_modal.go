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
	ti.Width = 52
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
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#bc8cff")).
		Padding(0, 1).
		Render("🌐 REMOTE GIT REPOSITORY SETTINGS")

	// 1. URL Input Box
	urlBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#30363d")).
		Padding(0, 1).
		Width(66)
	if m.FocusedIndex == 0 {
		urlBoxStyle = urlBoxStyle.BorderForeground(lipgloss.Color("#58a6ff"))
	}
	urlView := urlBoxStyle.Render(m.URLInput.View())

	// 2. Auto-push Checkbox
	checkMark := "[ ]"
	if m.AutoPush {
		checkMark = "[✓]"
	}
	autoPushLabel := fmt.Sprintf("%s Auto-push on every configuration commit", checkMark)
	autoPushStyle := lipgloss.NewStyle().Padding(0, 1)
	if m.FocusedIndex == 1 {
		autoPushStyle = autoPushStyle.
			Background(lipgloss.Color("#1f6feb")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)
	} else if m.AutoPush {
		autoPushStyle = autoPushStyle.Foreground(lipgloss.Color("#3fb950")).Bold(true)
	} else {
		autoPushStyle = autoPushStyle.Foreground(lipgloss.Color("#8b949e"))
	}
	autoPushView := autoPushStyle.Render(autoPushLabel)

	// 3. Action Buttons
	btnCopyStyle := lipgloss.NewStyle().Padding(0, 2).MarginRight(1).Background(lipgloss.Color("#21262d")).Foreground(lipgloss.Color("#e6edf3"))
	if m.FocusedIndex == 2 {
		btnCopyStyle = btnCopyStyle.Background(lipgloss.Color("#e3b341")).Foreground(lipgloss.Color("#000000")).Bold(true)
	}
	btnCopy := btnCopyStyle.Render("📋 Copy Key")

	btnPushStyle := lipgloss.NewStyle().Padding(0, 2).MarginRight(1).Background(lipgloss.Color("#21262d")).Foreground(lipgloss.Color("#e6edf3"))
	if m.FocusedIndex == 3 {
		btnPushStyle = btnPushStyle.Background(lipgloss.Color("#1f6feb")).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	}
	btnPush := btnPushStyle.Render("🚀 Push Now")

	btnSaveStyle := lipgloss.NewStyle().Padding(0, 2).MarginRight(1).Background(lipgloss.Color("#21262d")).Foreground(lipgloss.Color("#e6edf3"))
	if m.FocusedIndex == 4 {
		btnSaveStyle = btnSaveStyle.Background(lipgloss.Color("#238636")).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	}
	btnSave := btnSaveStyle.Render("💾 Save & Close")

	btnCancelStyle := lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("#21262d")).Foreground(lipgloss.Color("#8b949e"))
	if m.FocusedIndex == 5 {
		btnCancelStyle = btnCancelStyle.Background(lipgloss.Color("#da3633")).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	}
	btnCancel := btnCancelStyle.Render("❌ Cancel")

	buttonsRow := lipgloss.JoinHorizontal(lipgloss.Center, btnCopy, btnPush, btnSave, btnCancel)

	// 4. Public Key Display (for easy copying to GitHub Deploy Keys)
	var pubKeySection string
	if m.Config.PublicKey != "" {
		keyHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e3b341")).Render("🔑 Device Public SSH Key (Add to GitHub Deploy Keys):")
		keyBox := lipgloss.NewStyle().
			Background(lipgloss.Color("#0d1117")).
			Foreground(lipgloss.Color("#58a6ff")).
			Padding(0, 1).
			Width(66).
			Render(m.Config.PublicKey)
		keyHint := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render("💡 Use [Copy Key] or cat /tmp/srl_timeline_deploy_key.pub")
		pubKeySection = fmt.Sprintf("\n%s\n%s\n%s", keyHeader, keyBox, keyHint)
	}

	// 5. Status / Error Message
	var statusView string
	if m.StatusMsg != "" {
		statusStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3fb950"))
		if m.IsError {
			statusStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f85149"))
		}
		statusView = statusStyle.Render(fmt.Sprintf("Status: %s", m.StatusMsg))
	} else if m.Config.SyncStatus != "" {
		statusView = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(fmt.Sprintf("Sync Status: %s", m.Config.SyncStatus))
	}

	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(
		"[Tab/↓/↑] Navigate fields  |  [Space/Enter] Toggle / Activate  |  [c] Copy Key  |  [Esc] Close",
	)

	content := fmt.Sprintf("%s\n\n%s\n%s\n\n%s\n\n%s%s\n\n%s",
		title,
		urlView,
		autoPushView,
		buttonsRow,
		statusView,
		pubKeySection,
		footer,
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#bc8cff")).
		Background(lipgloss.Color("#161b22")).
		Padding(1, 2).
		Width(72).
		Render(content)

	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
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

		case "space":
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
