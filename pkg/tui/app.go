package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/blame"
	"timeline/pkg/daemon"
	"timeline/pkg/differ"
	"timeline/pkg/exporter"
	"timeline/pkg/gitbackend"
	"timeline/pkg/models"
	"timeline/pkg/restorer"
	"timeline/pkg/srlclient"
	"timeline/pkg/tui/modals"
)

// FocusedPane defines which pane currently has user focus.
type FocusedPane int

const (
	PaneTimeline FocusedPane = iota
	PaneDetail
	PaneFilter
)

// ActiveTab defines which detail view is visible.
type ActiveTab int

const (
	TabDiff ActiveTab = iota
	TabConfig
	TabBlame
)

// ActiveModal defines which modal overlay is displayed.
type ActiveModal int

const (
	ModalNone ActiveModal = iota
	ModalRestore
	ModalCherryPick
	ModalExport
	ModalRemote
	ModalHelp
)

// RealtimeCommitMsg is sent when the daemon detects a new commit.
type RealtimeCommitMsg struct {
	Commit models.TimelineCommit
}

// NotificationMsg is sent to display a banner alert.
type NotificationMsg struct {
	Message  string
	Severity string // "info", "success", "error", "warning"
}

type clearNotificationMsg struct{}

type systemInfoMsg struct {
	info map[string]string
}

type headSHAMsg struct {
	sha string
}

type remoteSyncStatusMsg struct {
	status string
	pubKey string
}

// AppModel is the primary Bubble Tea model.
type AppModel struct {
	// Sub-components
	HeaderBar    HeaderBarModel
	FilterBar    FilterBarModel
	TimelineView TimelineViewModel
	DiffView     DiffViewModel
	ConfigView   ConfigViewModel
	BlameView    BlameViewModel
	FooterBar    FooterBarModel

	// Modals
	ActiveModal     ActiveModal
	RestoreModal    modals.RestoreModalModel
	CherryPickModal modals.CherryPickModalModel
	ExportModal     modals.ExportModalModel
	RemoteModal     modals.RemoteModalModel
	HelpModal       modals.HelpModalModel

	// Backend services
	GitBackend  *gitbackend.GitBackend
	SRLClient   *srlclient.SRLClient
	Daemon      *daemon.TimelineDaemon
	Restorer    *restorer.ConfigRestorer
	Exporter    *exporter.ConfigExporter
	BlameEngine *blame.BlameEngine

	// State
	FocusedPane       FocusedPane
	ActiveTab         ActiveTab
	Notification      string
	NotifyStyle       lipgloss.Style
	notifyKind        string
	Width             int
	Height            int
	FocusFilter       bool
	VsLive            bool
	lastHeadSHA       string
	cachedLiveConfig  map[string]interface{}
	liveConfigFetched time.Time
	isFetchingLiveCfg bool
	isCheckingHead    bool
	isRestoring       bool

	// Realtime channel
	commitChan chan models.TimelineCommit
}

// NewAppModel initializes the Bubble Tea application model.
func NewAppModel(backend *gitbackend.GitBackend, client *srlclient.SRLClient, initialFilter string) AppModel {
	if backend == nil {
		backend = gitbackend.NewGitBackend("")
	}
	if client == nil {
		client = srlclient.NewSRLClient()
	}

	d := daemon.NewTimelineDaemon(client, backend)
	r := restorer.NewConfigRestorer(client, backend)
	e := exporter.NewConfigExporter(backend, client)
	b := blame.NewBlameEngine(backend)

	if !backend.HasCommits() {
		if unlock, ok := daemon.TryAcquireRepoRecordLock(backend.RepoDir); ok {
			if !backend.HasCommits() {
				if cfg, err := client.GetRunningConfig("/"); err == nil && len(cfg) > 0 {
					_, _, _ = backend.EnsureBaseline(cfg)
				}
			}
			unlock()
		}
	}

	fb := NewFilterBarModel(100)
	if initialFilter != "" {
		fb.TextInput.SetValue(initialFilter)
	}

	commitChan := make(chan models.TimelineCommit, 10)

	m := AppModel{
		HeaderBar:    NewHeaderBarModel(100),
		FilterBar:    fb,
		TimelineView: NewTimelineViewModel(45, 25),
		DiffView:     NewDiffViewModel(70, 25),
		ConfigView:   NewConfigViewModel(70, 25),
		BlameView:    NewBlameViewModel(70, 25),
		FooterBar:    NewFooterBarModel(100),

		GitBackend:  backend,
		SRLClient:   client,
		Daemon:      d,
		Restorer:    r,
		Exporter:    e,
		BlameEngine: b,

		FocusedPane: PaneTimeline,
		ActiveTab:   TabDiff,
		ActiveModal: ModalNone,
		lastHeadSHA: backend.GetHeadSHA(),
		commitChan:  commitChan,
	}

	// Register daemon listener
	d.RegisterListener(func(c models.TimelineCommit) {
		commitChan <- c
	})

	return m
}

// RefreshTheme re-renders all stateful sub-components after a theme change.
func (m *AppModel) RefreshTheme() {
	m.FilterBar.RefreshStyles()
	StyleTextInput(&m.ExportModal.PathInput, ColorSurface, ColorText, ColorTextDim, ColorActive)
	StyleTextInput(&m.RemoteModal.URLInput, ColorSurface, ColorText, ColorTextDim, ColorActive)
	StyleTextInput(&m.CherryPickModal.CustomInput, ColorSurface, ColorText, ColorTextDim, ColorActive)
	switch m.notifyKind {
	case "success":
		m.NotifyStyle = lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
	case "error":
		m.NotifyStyle = lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
	case "warning":
		m.NotifyStyle = lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
	case "primary":
		m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
	}
	m.TimelineView.UpdateViewportContent()
	m.DiffView.UpdateContent()
	m.ConfigView.lastRendered = ""
	m.ConfigView.UpdateContent()
	m.BlameView.lastRendered = ""
	m.BlameView.UpdateContent()
}

type checkLiveDiskSyncMsg struct{}

func tickLiveDiskSyncCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return checkLiveDiskSyncMsg{}
	})
}

type liveConfigMsg struct {
	config map[string]interface{}
}

func fetchLiveConfigCmd(client *srlclient.SRLClient) tea.Cmd {
	return func() tea.Msg {
		cfg, err := client.GetRunningConfig("/")
		if err != nil || len(cfg) == 0 {
			return liveConfigMsg{config: nil}
		}
		return liveConfigMsg{config: cfg}
	}
}

func fetchSystemInfoCmd(client *srlclient.SRLClient) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return systemInfoMsg{info: nil}
		}
		return systemInfoMsg{info: client.GetSystemInfo()}
	}
}

func checkHeadSHACmd(backend *gitbackend.GitBackend) tea.Cmd {
	return func() tea.Msg {
		if backend == nil {
			return headSHAMsg{}
		}
		return headSHAMsg{sha: backend.GetHeadSHA()}
	}
}

func fetchRemoteStatusCmd(backend *gitbackend.GitBackend) tea.Cmd {
	return func() tea.Msg {
		if backend == nil {
			return remoteSyncStatusMsg{}
		}
		return remoteSyncStatusMsg{
			status: backend.CheckRemoteSyncStatus(),
			pubKey: backend.GetPublicSSHKey(),
		}
	}
}

// Init starts the Bubble Tea loop and background daemon if not already running.
func (m AppModel) Init() tea.Cmd {
	if running, _, _ := daemon.IsAnyDaemonRunning(); !running {
		m.Daemon.Start()
	}

	return tea.Batch(
		fetchSystemInfoCmd(m.SRLClient),
		tickLiveDiskSyncCmd(),
		waitForRealtimeCommit(m.commitChan),
		tea.EnterAltScreen,
		tea.EnableMouseCellMotion,
	)
}

func waitForRealtimeCommit(sub chan models.TimelineCommit) tea.Cmd {
	return func() tea.Msg {
		c := <-sub
		return RealtimeCommitMsg{Commit: c}
	}
}

func clearNotificationCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
		return clearNotificationMsg{}
	})
}

func (m *AppModel) setNotification(text string, style lipgloss.Style) {
	wasEmpty := m.Notification == ""
	m.Notification = text
	m.NotifyStyle = style
	switch style.GetBackground() {
	case ColorSuccessBg:
		m.notifyKind = "success"
	case ColorDangerBg:
		m.notifyKind = "error"
	case ColorWarning:
		m.notifyKind = "warning"
	case ColorPrimary:
		m.notifyKind = "primary"
	}
	isEmpty := m.Notification == ""
	if wasEmpty != isEmpty && m.Width > 0 && m.Height > 0 {
		m.syncPaneHeights()
	}
}

func computeSplitOuterWidths(totalWidth int) (int, int) {
	timelineOuterWidth := (totalWidth * 38) / 100
	if timelineOuterWidth < 35 {
		timelineOuterWidth = 35
	}
	if timelineOuterWidth > totalWidth-42 {
		timelineOuterWidth = totalWidth - 42
	}
	if timelineOuterWidth < 20 {
		timelineOuterWidth = 20
	}

	detailOuterWidth := totalWidth - timelineOuterWidth - 1
	if detailOuterWidth < 20 {
		detailOuterWidth = 20
	}

	if totalWidth >= 7 && timelineOuterWidth+1+detailOuterWidth > totalWidth {
		timelineOuterWidth = (totalWidth - 1) / 2
		if timelineOuterWidth < 3 {
			timelineOuterWidth = 3
		}
		detailOuterWidth = totalWidth - timelineOuterWidth - 1
		if detailOuterWidth < 3 {
			detailOuterWidth = 3
		}
	}
	return timelineOuterWidth, detailOuterWidth
}

func (m *AppModel) syncPaneHeights() {
	if m.Width == 0 || m.Height == 0 {
		return
	}
	notifExtra := 0
	if m.Notification != "" {
		notifExtra = 1
	}

	timelinePaneHeight := m.Height - 8 - notifExtra
	if timelinePaneHeight < 5 {
		timelinePaneHeight = 5
	}

	detailPaneHeight := m.Height - 9 - notifExtra
	if detailPaneHeight < 4 {
		detailPaneHeight = 4
	}

	timelineOuterWidth, detailOuterWidth := computeSplitOuterWidths(m.Width)

	timelineInnerWidth := timelineOuterWidth - 2
	if timelineInnerWidth < 1 {
		timelineInnerWidth = 1
	}
	detailInnerWidth := detailOuterWidth - 2
	if detailInnerWidth < 1 {
		detailInnerWidth = 1
	}

	m.TimelineView.SetSize(timelineInnerWidth, timelinePaneHeight)
	m.DiffView.SetSize(detailInnerWidth, detailPaneHeight)
	m.ConfigView.SetSize(detailInnerWidth, detailPaneHeight)
	m.BlameView.SetSize(detailInnerWidth, detailPaneHeight)
}

// Update processes Bubble Tea events and coordinates state changes.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.resizeLayout(msg.Width, msg.Height)

	case systemInfoMsg:
		if len(msg.info) > 0 {
			if hn := msg.info["host-name"]; hn != "" {
				m.HeaderBar.Hostname = hn
			}
			if ver := msg.info["version"]; ver != "" {
				m.HeaderBar.Version = ver
			}
			if ch := msg.info["chassis"]; ch != "" {
				m.HeaderBar.Chassis = ch
			}
		}

	case remoteSyncStatusMsg:
		if msg.status != "" && msg.status != "Not Configured" {
			m.HeaderBar.SyncStatus = msg.status
		} else if msg.status == "Not Configured" {
			m.HeaderBar.SyncStatus = "Local Only"
		}
		if m.ActiveModal == ModalRemote {
			if msg.status != "" {
				m.RemoteModal.Config.SyncStatus = msg.status
			}
			if msg.pubKey != "" {
				m.RemoteModal.Config.PublicKey = msg.pubKey
			}
		}

	case liveConfigMsg:
		m.isFetchingLiveCfg = false
		if len(msg.config) > 0 {
			m.cachedLiveConfig = msg.config
			m.liveConfigFetched = time.Now()
			if m.VsLive && m.ActiveTab == TabDiff {
				m.updateActiveTabContent()
			}
		}

	case checkLiveDiskSyncMsg:
		if !m.isCheckingHead {
			m.isCheckingHead = true
			cmds = append(cmds, checkHeadSHACmd(m.GitBackend))
		}
		if m.VsLive && !m.isFetchingLiveCfg && time.Since(m.liveConfigFetched) > 3*time.Second {
			m.isFetchingLiveCfg = true
			cmds = append(cmds, fetchLiveConfigCmd(m.SRLClient))
		}
		cmds = append(cmds, tickLiveDiskSyncCmd())

	case headSHAMsg:
		m.isCheckingHead = false
		currentHead := msg.sha
		if currentHead != "" && currentHead != m.lastHeadSHA {
			m.lastHeadSHA = currentHead
			m.cachedLiveConfig = m.GitBackend.GetLatestCommitConfig()
			m.liveConfigFetched = time.Now()
			m.refreshTimeline()
			selected := m.TimelineView.SelectedCommit()
			if selected != nil && selected.FullSHA == currentHead {
				m.setNotification(
					fmt.Sprintf("⚡ Live update by %s: %s", selected.Author, selected.Message),
					lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
			}
		}

	case RealtimeCommitMsg:
		m.lastHeadSHA = msg.Commit.FullSHA
		m.cachedLiveConfig = m.GitBackend.GetLatestCommitConfig()
		m.liveConfigFetched = time.Now()
		m.setNotification(
			fmt.Sprintf("⚡ Realtime commit by %s: %s", msg.Commit.Author, msg.Commit.Message),
			lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
		)
		cmds = append(cmds, clearNotificationCmd())
		m.refreshTimeline()
		cmds = append(cmds, waitForRealtimeCommit(m.commitChan))

	case NotificationMsg:
		m.isRestoring = false
		var style lipgloss.Style
		switch msg.Severity {
		case "error":
			style = lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
		case "warning":
			style = lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
		default:
			style = lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
		}
		m.setNotification(msg.Message, style)
		m.refreshTimeline()
		cmds = append(cmds, clearNotificationCmd())

	case clearNotificationMsg:
		m.setNotification("", m.NotifyStyle)

	case tea.KeyMsg:
		// If Modal is open, delegate exclusively to Modal
		if m.ActiveModal != ModalNone {
			return m.handleModalKeys(msg)
		}

		// 1. If Filter input is focused
		if m.FocusedPane == PaneFilter || m.FocusFilter {
			switch msg.String() {
			case "esc":
				if m.FilterBar.Value() != "" {
					m.FilterBar.Clear()
				}
				m.FocusFilter = false
				m.FocusedPane = PaneTimeline
				m.FilterBar.Blur()
				m.TimelineView.IsFocused = true
				m.DiffView.IsFocused = false
				m.ConfigView.IsFocused = false
				m.BlameView.IsFocused = false
				m.refreshTimeline()
				return m, nil
			case "enter":
				m.FocusFilter = false
				m.FocusedPane = PaneTimeline
				m.FilterBar.Blur()
				m.TimelineView.IsFocused = true
				m.DiffView.IsFocused = false
				m.ConfigView.IsFocused = false
				m.BlameView.IsFocused = false
				m.refreshTimeline()
				return m, nil
			case "ctrl+u", "ctrl+c":
				m.FilterBar.Clear()
				m.refreshTimeline()
				return m, nil
			default:
				var cmd tea.Cmd
				m.FilterBar, cmd = m.FilterBar.Update(msg)
				m.refreshTimeline()
				return m, cmd
			}
		}

		// 2. Filter Activation Shortcut
		if msg.String() == "/" {
			m.FocusFilter = true
			m.FocusedPane = PaneFilter
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = false
			m.ConfigView.IsFocused = false
			m.BlameView.IsFocused = false
			return m, m.FilterBar.Focus()
		}

		// 3. Clear filter on Esc or 'x'
		if (msg.String() == "esc" || msg.String() == "x") && m.FilterBar.Value() != "" {
			m.FilterBar.Clear()
			m.refreshTimeline()
			return m, nil
		}

		// 4. Pane Navigation (Tab, Left, Right, h, l)
		switch msg.String() {
		case "tab":
			if m.FocusedPane == PaneTimeline {
				m.FocusedPane = PaneDetail
				m.TimelineView.IsFocused = false
				m.DiffView.IsFocused = true
				m.ConfigView.IsFocused = true
				m.BlameView.IsFocused = true
			} else {
				m.FocusedPane = PaneTimeline
				m.TimelineView.IsFocused = true
				m.DiffView.IsFocused = false
				m.ConfigView.IsFocused = false
				m.BlameView.IsFocused = false
			}
			return m, nil

		case "right", "l":
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			return m, nil

		case "left", "h":
			m.FocusedPane = PaneTimeline
			m.TimelineView.IsFocused = true
			m.DiffView.IsFocused = false
			m.ConfigView.IsFocused = false
			m.BlameView.IsFocused = false
			return m, nil
		}

		// 5. Top Tab Switchers: d (Diff), c (Config), b (Blame), [, ], t
		switch msg.String() {
		case "d":
			m.ActiveTab = TabDiff
			m.updateActiveTabContent()
			return m, nil
		case "c":
			m.ActiveTab = TabConfig
			m.updateActiveTabContent()
			return m, nil
		case "b":
			m.ActiveTab = TabBlame
			m.updateActiveTabContent()
			return m, nil
		case "[":
			m.ActiveTab = (m.ActiveTab + 2) % 3
			m.updateActiveTabContent()
			return m, nil
		case "]", "t":
			m.ActiveTab = (m.ActiveTab + 1) % 3
			m.updateActiveTabContent()
			return m, nil
		}

		// 6. Global action keys
		switch msg.String() {
		case "q", "ctrl+c":
			m.Daemon.Stop()
			return m, tea.Quit

		case "T":
			t := NextTheme()
			m.RefreshTheme()
			m.setNotification(
				fmt.Sprintf("Theme: %s (%s) — press [T] to cycle", t.DisplayName, t.Name),
				lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
			)
			cmds = append(cmds, clearNotificationCmd())
			return m, tea.Batch(cmds...)

		case "r":
			if m.isRestoring {
				m.setNotification(
					"Restore/cherry-pick operation already in progress...",
					lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			}
			selected := m.TimelineView.SelectedCommit()
			if selected != nil {
				m.RestoreModal = modals.NewRestoreModal(*selected, m.Width, m.Height)
				m.ActiveModal = ModalRestore
			}
			return m, nil

		case "R", "ctrl+r", "f5":
			m.lastHeadSHA = m.GitBackend.GetHeadSHA()
			m.refreshTimeline()
			m.setNotification(
				"Refreshed timeline from disk",
				lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
			)
			cmds = append(cmds, clearNotificationCmd())
			return m, tea.Batch(cmds...)

		case "p":
			if m.isRestoring {
				m.setNotification(
					"Restore/cherry-pick operation already in progress...",
					lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			}
			selected := m.TimelineView.SelectedCommit()
			if selected != nil {
				curCfg := m.GitBackend.GetConfigAtCommit(selected.FullSHA)
				prevCfg := m.GitBackend.GetConfigAtCommit(fmt.Sprintf("%s~1", selected.FullSHA))
				diffRes := differ.SemanticDiff(prevCfg, curCfg, "")
				m.CherryPickModal = modals.NewCherryPickModal(*selected, diffRes.Changes, curCfg, m.Width, m.Height)
				m.ActiveModal = ModalCherryPick
			}
			return m, nil

		case "e":
			selected := m.TimelineView.SelectedCommit()
			if selected != nil {
				m.ExportModal = modals.NewExportModal(*selected, m.Width, m.Height)
				m.ActiveModal = ModalExport
			}
			return m, nil

		case "g":
			remoteCfg := m.GitBackend.LoadRemoteConfig()
			if remoteCfg.URL == "" {
				remoteCfg.SyncStatus = "Not Configured"
			} else {
				remoteCfg.SyncStatus = "Checking..."
			}
			m.RemoteModal = modals.NewRemoteModal(remoteCfg, m.Width, m.Height)
			m.ActiveModal = ModalRemote
			return m, fetchRemoteStatusCmd(m.GitBackend)

		case "?":
			m.HelpModal = modals.NewHelpModal(m.Width, m.Height)
			m.ActiveModal = ModalHelp
			return m, nil
		}

		// 7. View-specific sub-options
		switch m.ActiveTab {
		case TabDiff:
			switch msg.String() {
			case "1", "u":
				m.DiffView.DiffMode = "unified"
				m.DiffView.UpdateContent()
				m.setNotification(
					"Diff Format: UNIFIED [1]",
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "2":
				m.DiffView.DiffMode = "path"
				m.DiffView.UpdateContent()
				m.setNotification(
					"Diff Format: PATH CHANGES [2]",
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "3":
				m.DiffView.DiffMode = "cli"
				m.DiffView.UpdateContent()
				m.setNotification(
					"Diff Format: FLAT CLI DIFF [3]",
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "4", "v":
				m.VsLive = !m.VsLive
				m.DiffView.VsLive = m.VsLive
				var notif string
				if m.VsLive {
					notif = "Diff Mode: Comparing revision vs LIVE Running Configuration [4/v]"
					if !m.isFetchingLiveCfg {
						m.isFetchingLiveCfg = true
						cmds = append(cmds, fetchLiveConfigCmd(m.SRLClient))
					}
				} else {
					notif = "Diff Mode: Comparing revision vs Previous Commit"
				}
				m.setNotification(
					notif,
					lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				m.updateActiveTabContent()
				return m, tea.Batch(cmds...)
			}

		case TabConfig:
			switch msg.String() {
			case "1":
				m.ConfigView.FormatMode = "cli"
				m.ConfigView.UpdateContent()
				m.setNotification(
					"Config Format: FLAT CLI SYNTAX [1]",
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "2":
				m.ConfigView.FormatMode = "json"
				m.ConfigView.UpdateContent()
				m.setNotification(
					"Config Format: JSON HIERARCHY [2]",
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			}
		}

		// 8. Navigation inside active pane
		if m.FocusedPane == PaneDetail {
			switch msg.String() {
			case "up", "k":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.LineUp(2)
				case TabConfig:
					m.ConfigView.Viewport.LineUp(2)
				case TabBlame:
					m.BlameView.Viewport.LineUp(2)
				}
				return m, nil
			case "down", "j":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.LineDown(2)
				case TabConfig:
					m.ConfigView.Viewport.LineDown(2)
				case TabBlame:
					m.BlameView.Viewport.LineDown(2)
				}
				return m, nil
			case "pgup", "b", "ctrl+u":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.ViewUp()
				case TabConfig:
					m.ConfigView.Viewport.ViewUp()
				case TabBlame:
					m.BlameView.Viewport.ViewUp()
				}
				return m, nil
			case "pgdown", " ", "space", "f", "ctrl+d":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.ViewDown()
				case TabConfig:
					m.ConfigView.Viewport.ViewDown()
				case TabBlame:
					m.BlameView.Viewport.ViewDown()
				}
				return m, nil
			case "home", "g":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.GotoTop()
				case TabConfig:
					m.ConfigView.Viewport.GotoTop()
				case TabBlame:
					m.BlameView.Viewport.GotoTop()
				}
				return m, nil
			case "end", "G":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.GotoBottom()
				case TabConfig:
					m.ConfigView.Viewport.GotoBottom()
				case TabBlame:
					m.BlameView.Viewport.GotoBottom()
				}
				return m, nil
			}
			return m, nil
		} else {
			// Navigate timeline commits
			switch msg.String() {
			case "j", "down":
				m.TimelineView.Next()
				m.updateActiveTabContent()
			case "k", "up":
				m.TimelineView.Prev()
				m.updateActiveTabContent()
			case "home":
				m.TimelineView.GotoTop()
				m.updateActiveTabContent()
			case "end", "G":
				m.TimelineView.GotoBottom()
				m.updateActiveTabContent()
			case "pgup", "ctrl+u":
				m.TimelineView.PageUp()
				m.updateActiveTabContent()
			case "pgdown", " ", "space", "ctrl+d":
				m.TimelineView.PageDown()
				m.updateActiveTabContent()
			}
		}

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m AppModel) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.ActiveModal == ModalCherryPick {
		var selectedSubtrees []string
		var close bool
		m.CherryPickModal, selectedSubtrees, close = m.CherryPickModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if len(selectedSubtrees) > 0 && !m.isRestoring {
				m.isRestoring = true
				var notifyStr string
				if len(selectedSubtrees) == 1 {
					notifyStr = fmt.Sprintf("Applying cherry-pick '%s' to switch...", selectedSubtrees[0])
				} else {
					notifyStr = fmt.Sprintf("Applying %d cherry-picked paths to switch...", len(selectedSubtrees))
				}
				m.setNotification(
					notifyStr,
					lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				sha := m.CherryPickModal.Commit.FullSHA
				return m, performCherryPickCmd(m.Restorer, sha, selectedSubtrees...)
			}
		}
		return m, nil
	}
	if m.ActiveModal != ModalNone {
		return m, nil
	}

	notifExtra := 0
	if m.Notification != "" {
		notifExtra = 1
	}

	timelineWidth, detailOuterWidth := computeSplitOuterWidths(m.Width)

	// 1. Click on Filter Bar (Y == 2 or Y == 3)
	if (msg.Y == 2 || msg.Y == 3) && msg.Type == tea.MouseLeft {
		if m.FilterBar.Value() != "" && msg.X >= m.Width-25 {
			m.FilterBar.Clear()
			m.FocusFilter = false
			m.FocusedPane = PaneTimeline
			m.FilterBar.Blur()
			m.TimelineView.IsFocused = true
			m.refreshTimeline()
			return m, nil
		}
		m.FocusFilter = true
		m.FocusedPane = PaneFilter
		m.TimelineView.IsFocused = false
		m.DiffView.IsFocused = false
		m.ConfigView.IsFocused = false
		m.BlameView.IsFocused = false
		return m, m.FilterBar.Focus()
	}

	// 2. Click on Top Tab Bar (Y == 4 + notifExtra, X > timelineWidth)
	if msg.Y == 4+notifExtra && msg.X > timelineWidth && msg.Type == tea.MouseLeft {
		tabOffset := msg.X - timelineWidth
		diffTitle := "🔍 Diff View [d]"
		configTitle := "📄 Full Config [c]"
		blameTitle := "🕵️ Blame View [b]"
		if detailOuterWidth < 55 {
			diffTitle = "[d] Diff"
			configTitle = "[c] Config"
			blameTitle = "[b] Blame"
		}
		tabDiffW := lipgloss.Width(StyleTabActive.Render(diffTitle))
		tabConfigW := lipgloss.Width(StyleTabActive.Render(configTitle))
		tabBlameW := lipgloss.Width(StyleTabActive.Render(blameTitle))

		if tabOffset >= 0 && tabOffset <= tabDiffW {
			m.ActiveTab = TabDiff
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			m.updateActiveTabContent()
			return m, nil
		} else if tabOffset > tabDiffW && tabOffset <= tabDiffW+tabConfigW {
			m.ActiveTab = TabConfig
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			m.updateActiveTabContent()
			return m, nil
		} else if tabOffset > tabDiffW+tabConfigW && tabOffset <= tabDiffW+tabConfigW+tabBlameW {
			m.ActiveTab = TabBlame
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			m.updateActiveTabContent()
			return m, nil
		}
	}

	// 3. Mouse Wheel on Right Detail Pane OR when Detail Pane is focused
	if msg.X > timelineWidth || m.FocusedPane == PaneDetail {
		switch msg.Type {
		case tea.MouseWheelUp:
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			switch m.ActiveTab {
			case TabDiff:
				m.DiffView.Viewport.LineUp(3)
			case TabConfig:
				m.ConfigView.Viewport.LineUp(3)
			case TabBlame:
				m.BlameView.Viewport.LineUp(3)
			}
			return m, nil

		case tea.MouseWheelDown:
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			switch m.ActiveTab {
			case TabDiff:
				m.DiffView.Viewport.LineDown(3)
			case TabConfig:
				m.ConfigView.Viewport.LineDown(3)
			case TabBlame:
				m.BlameView.Viewport.LineDown(3)
			}
			return m, nil
		}
	}

	// 4. Mouse on Left Timeline Pane (X <= timelineWidth)
	if msg.X <= timelineWidth {
		switch msg.Type {
		case tea.MouseWheelUp:
			m.TimelineView.Prev()
			m.updateActiveTabContent()
			return m, nil

		case tea.MouseWheelDown:
			m.TimelineView.Next()
			m.updateActiveTabContent()
			return m, nil

		case tea.MouseLeft:
			m.FocusedPane = PaneTimeline
			m.TimelineView.IsFocused = true
			m.DiffView.IsFocused = false
			m.ConfigView.IsFocused = false
			m.BlameView.IsFocused = false
			if m.FocusFilter {
				m.FocusFilter = false
				m.FilterBar.Blur()
			}

			// Sub-row hit testing for commit list:
			if msg.Y >= 6+notifExtra {
				clickedRow := (msg.Y - (6 + notifExtra) + m.TimelineView.Viewport.YOffset) / 3
				if clickedRow >= 0 && clickedRow < len(m.TimelineView.Commits) {
					m.TimelineView.SelectedIndex = clickedRow
					m.TimelineView.UpdateViewportContent()
					m.updateActiveTabContent()
				}
			}
			return m, nil
		}
	}

	// 5. Mouse on Right Detail Pane (X > timelineWidth)
	if msg.X > timelineWidth {
		switch msg.Type {
		case tea.MouseLeft:
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			if m.FocusFilter {
				m.FocusFilter = false
				m.FilterBar.Blur()
			}

			tabOffset := msg.X - timelineWidth

			// Sub-Option Controls Bar Clicks (Y == 6 + notifExtra or Y == 7 + notifExtra)
			if msg.Y == 6+notifExtra || msg.Y == 7+notifExtra {
				if m.ActiveTab == TabDiff {
					if tabOffset >= 2 && tabOffset <= 12 {
						m.DiffView.DiffMode = "unified"
						m.DiffView.UpdateContent()
					} else if tabOffset >= 13 && tabOffset <= 24 {
						m.DiffView.DiffMode = "path"
						m.DiffView.UpdateContent()
					} else if tabOffset >= 25 && tabOffset <= 34 {
						m.DiffView.DiffMode = "cli"
						m.DiffView.UpdateContent()
					} else if tabOffset >= 35 && tabOffset <= 56 {
						m.VsLive = !m.VsLive
						m.DiffView.VsLive = m.VsLive
						m.updateActiveTabContent()
						if m.VsLive && !m.isFetchingLiveCfg {
							m.isFetchingLiveCfg = true
							return m, fetchLiveConfigCmd(m.SRLClient)
						}
						return m, nil
					}
				} else if m.ActiveTab == TabConfig {
					cliEnd := 17
					if m.ConfigView.FormatMode == "cli" {
						cliEnd = 19
					}
					if tabOffset >= 2 && tabOffset <= cliEnd {
						m.ConfigView.FormatMode = "cli"
						m.ConfigView.UpdateContent()
					} else if tabOffset > cliEnd && tabOffset <= 45 {
						m.ConfigView.FormatMode = "json"
						m.ConfigView.UpdateContent()
					}
				}
			}
			return m, nil
		}
	}

	return m, nil
}

func (m AppModel) handleModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "T" {
		canCycleTheme := m.ActiveModal == ModalHelp ||
			m.ActiveModal == ModalRestore ||
			(m.ActiveModal == ModalCherryPick && !m.CherryPickModal.UseCustom) ||
			(m.ActiveModal == ModalExport && m.ExportModal.Format == "startup") ||
			(m.ActiveModal == ModalRemote && m.RemoteModal.FocusedIndex != 0)
		if canCycleTheme {
			NextTheme()
			m.RefreshTheme()
			return m, nil
		}
	}

	switch m.ActiveModal {
	case ModalRestore:
		var confirmed, close bool
		m.RestoreModal, confirmed, close = m.RestoreModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if confirmed && !m.isRestoring {
				m.isRestoring = true
				m.setNotification(
					"Applying configuration restore to SR Linux switch...",
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				sha := m.RestoreModal.Commit.FullSHA
				return m, performRestoreCmd(m.Restorer, sha)
			}
		}

	case ModalCherryPick:
		var selectedSubtrees []string
		var close bool
		m.CherryPickModal, selectedSubtrees, close = m.CherryPickModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if len(selectedSubtrees) > 0 && !m.isRestoring {
				m.isRestoring = true
				var notifyStr string
				if len(selectedSubtrees) == 1 {
					notifyStr = fmt.Sprintf("Applying cherry-pick '%s' to switch...", selectedSubtrees[0])
				} else {
					notifyStr = fmt.Sprintf("Applying %d cherry-picked paths to switch...", len(selectedSubtrees))
				}
				m.setNotification(
					notifyStr,
					lipgloss.NewStyle().Background(ColorWarning).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				sha := m.CherryPickModal.Commit.FullSHA
				return m, performCherryPickCmd(m.Restorer, sha, selectedSubtrees...)
			}
		}

	case ModalExport:
		var format, outputPath string
		var close bool
		m.ExportModal, format, outputPath, close = m.ExportModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if format == "startup" || outputPath != "" {
				sha := m.ExportModal.Commit.FullSHA
				m.setNotification(
					"Exporting configuration...",
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				return m, performExportCmd(m.Exporter, sha, format, outputPath)
			}
		}

	case ModalRemote:
		var save, pushNow, close bool
		m.RemoteModal, save, pushNow, close = m.RemoteModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if save {
				if err := m.GitBackend.SaveRemoteConfig(m.RemoteModal.Config); err != nil {
					m.setNotification(
						fmt.Sprintf("Failed to save remote config: %v", err),
						lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
					)
				} else {
					m.setNotification(
						"Saved Remote Git configuration",
						lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
					)
				}
				return m, clearNotificationCmd()
			}
		}
		if pushNow {
			if err := m.GitBackend.SaveRemoteConfig(m.RemoteModal.Config); err != nil {
				m.ActiveModal = ModalNone
				m.setNotification(
					fmt.Sprintf("Failed to save remote config: %v", err),
					lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				return m, clearNotificationCmd()
			}
			m.ActiveModal = ModalNone
			m.setNotification(
				"Pushing commits to remote repository...",
				lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
			)
			return m, performPushRemoteCmd(m.GitBackend)
		}

	case ModalHelp:
		var close bool
		m.HelpModal, close = m.HelpModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
		}
	}

	return m, nil
}

func performExportCmd(exp *exporter.ConfigExporter, sha, format, outputPath string) tea.Cmd {
	return func() tea.Msg {
		if format == "startup" {
			ok, resMsg, err := exp.SaveAsDeviceStartupConfig(sha)
			if err != nil || !ok {
				return NotificationMsg{
					Message:  fmt.Sprintf("Export startup failed: %v", err),
					Severity: "error",
				}
			}
			return NotificationMsg{
				Message:  resMsg,
				Severity: "success",
			}
		}
		var exportErr error
		if format == "json" {
			_, exportErr = exp.ExportAsStartupJSON(sha, outputPath)
		} else {
			_, exportErr = exp.ExportAsFlatCLI(sha, outputPath)
		}
		if exportErr != nil {
			return NotificationMsg{
				Message:  fmt.Sprintf("Export failed: %v", exportErr),
				Severity: "error",
			}
		}
		shortSHA := sha
		if len(shortSHA) > 8 {
			shortSHA = shortSHA[:8]
		}
		return NotificationMsg{
			Message:  fmt.Sprintf("Exported revision %s to %s", shortSHA, outputPath),
			Severity: "success",
		}
	}
}

func performRestoreCmd(restorer *restorer.ConfigRestorer, sha string) tea.Cmd {
	return func() tea.Msg {
		ok, msg, err := restorer.RestoreFullConfig(sha)
		if err != nil || !ok {
			return NotificationMsg{
				Message:  fmt.Sprintf("Restore failed: %v", err),
				Severity: "error",
			}
		}
		return NotificationMsg{
			Message:  msg,
			Severity: "success",
		}
	}
}

func performCherryPickCmd(restorer *restorer.ConfigRestorer, sha string, paths ...string) tea.Cmd {
	return func() tea.Msg {
		ok, msg, err := restorer.CherryPickRestore(sha, paths...)
		if err != nil || !ok {
			return NotificationMsg{
				Message:  fmt.Sprintf("Cherry-pick failed: %v", err),
				Severity: "error",
			}
		}
		return NotificationMsg{
			Message:  msg,
			Severity: "success",
		}
	}
}

func performPushRemoteCmd(backend *gitbackend.GitBackend) tea.Cmd {
	return func() tea.Msg {
		err := backend.PushRemote()
		if err != nil {
			return NotificationMsg{
				Message:  fmt.Sprintf("Push failed: %v", err),
				Severity: "error",
			}
		}
		return NotificationMsg{
			Message:  "Successfully pushed to remote Git!",
			Severity: "success",
		}
	}
}

func (m *AppModel) resizeLayout(width, height int) {
	m.HeaderBar.Width = width
	m.FilterBar.SetWidth(width)
	m.FooterBar.Width = width

	m.RestoreModal.Width = width
	m.RestoreModal.Height = height
	m.CherryPickModal.Width = width
	m.CherryPickModal.Height = height
	m.ExportModal.Width = width
	m.ExportModal.Height = height
	m.RemoteModal.Width = width
	m.RemoteModal.Height = height
	m.HelpModal.Width = width
	m.HelpModal.Height = height

	m.syncPaneHeights()
	m.refreshTimeline()
}

func (m *AppModel) refreshTimeline() {
	filterQuery := m.FilterBar.Value()
	allCommits := m.GitBackend.GetTimeline(200, "")
	var filteredCommits []models.TimelineCommit
	if filterQuery != "" {
		filteredCommits = m.GitBackend.GetTimeline(200, filterQuery)
	} else {
		filteredCommits = allCommits
	}

	m.FilterBar.SetCounts(len(filteredCommits), len(allCommits))
	m.TimelineView.SetCommits(filteredCommits)
	m.updateActiveTabContent()
}

// updateActiveTabContent only updates the currently active tab.
func (m *AppModel) updateActiveTabContent() {
	selected := m.TimelineView.SelectedCommit()
	if selected == nil {
		return
	}

	filterQuery := m.FilterBar.Value()

	switch m.ActiveTab {
	case TabDiff:
		var prevCfg, currCfg map[string]interface{}
		if m.VsLive {
			if len(m.cachedLiveConfig) > 0 {
				currCfg = m.cachedLiveConfig
			} else {
				currCfg = m.GitBackend.GetLatestCommitConfig()
			}
			prevCfg = m.GitBackend.GetConfigAtCommit(selected.FullSHA)
		} else {
			parentSHA := fmt.Sprintf("%s~1", selected.FullSHA)
			prevCfg = m.GitBackend.GetConfigAtCommit(parentSHA)
			currCfg = m.GitBackend.GetConfigAtCommit(selected.FullSHA)
		}
		diffRes := differ.SemanticDiff(prevCfg, currCfg, filterQuery)
		m.DiffView.SetDiff(diffRes)

	case TabConfig:
		cfg := m.GitBackend.GetConfigAtCommit(selected.FullSHA)
		m.ConfigView.SetConfig(cfg, filterQuery)

	case TabBlame:
		entries := m.BlameEngine.GetBlameLinesAtCommit("cli", selected.FullSHA, filterQuery)
		stats := m.BlameEngine.GetContributorStatsAtCommit("cli", selected.FullSHA)
		m.BlameView.SetBlame(entries, stats)
	}
}

// View renders the full application screen.
func (m AppModel) View() string {
	if m.Width == 0 {
		return "Loading Timeline..."
	}

	// 1. Header Bar
	header := m.HeaderBar.View()

	// 2. Filter Bar
	filterBar := m.FilterBar.View()

	// 3. Tab Navigation Bar
	_, detailOuterWidth := computeSplitOuterWidths(m.Width)

	diffTitle := "🔍 Diff View [d]"
	configTitle := "📄 Full Config [c]"
	blameTitle := "🕵️ Blame View [b]"
	if detailOuterWidth < 55 {
		diffTitle = "[d] Diff"
		configTitle = "[c] Config"
		blameTitle = "[b] Blame"
	}

	tabDiff := StyleTabInactive.Render(diffTitle)
	if m.ActiveTab == TabDiff {
		tabDiff = StyleTabActive.Render(diffTitle)
	}
	tabConfig := StyleTabInactive.Render(configTitle)
	if m.ActiveTab == TabConfig {
		tabConfig = StyleTabActive.Render(configTitle)
	}
	tabBlame := StyleTabInactive.Render(blameTitle)
	if m.ActiveTab == TabBlame {
		tabBlame = StyleTabActive.Render(blameTitle)
	}

	tabBar := PaintBarLine(
		lipgloss.JoinHorizontal(lipgloss.Center, tabDiff, tabConfig, tabBlame),
		detailOuterWidth,
		ColorBackground,
		ColorText,
	)

	// 4. Detail Panel
	var detailContent string
	switch m.ActiveTab {
	case TabDiff:
		detailContent = m.DiffView.View()
	case TabConfig:
		detailContent = m.ConfigView.View()
	case TabBlame:
		detailContent = m.BlameView.View()
	}

	// 5. Main Split View: Left (Visual Timeline) | Right (Active Tab Content)
	detailWithTabs := lipgloss.JoinVertical(
		lipgloss.Left,
		tabBar,
		detailContent,
	)

	notifExtra := 0
	if m.Notification != "" {
		notifExtra = 1
	}
	timelinePaneHeight := m.Height - 8 - notifExtra
	if timelinePaneHeight < 5 {
		timelinePaneHeight = 5
	}
	splitHeight := timelinePaneHeight + 2
	splitSep := PaintSurface("", 1, splitHeight, ColorBackground, ColorText)

	mainSplit := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.TimelineView.View(),
		splitSep,
		detailWithTabs,
	)

	// 6. Notification Banner (if any)
	var notifView string
	if m.Notification != "" {
		singleLineNotif := strings.ReplaceAll(strings.ReplaceAll(m.Notification, "\r\n", " "), "\n", " ")
		notifBg := ColorPrimary
		if c, ok := m.NotifyStyle.GetBackground().(lipgloss.Color); ok && string(c) != "" {
			notifBg = c
		}
		notifFg := ColorOnAccent
		if c, ok := m.NotifyStyle.GetForeground().(lipgloss.Color); ok && string(c) != "" {
			notifFg = c
		}
		renderedNotif := m.NotifyStyle.UnsetWidth().Render(singleLineNotif)
		notifView = PaintBarLine(renderedNotif, m.Width, notifBg, notifFg)
	}

	// 7. Footer Bar
	footer := m.FooterBar.View(m.FilterBar.Value() != "")

	// Combine all sections cleanly without empty newline gaps
	var sections []string
	sections = append(sections, header)
	sections = append(sections, filterBar)
	if notifView != "" {
		sections = append(sections, notifView)
	}
	sections = append(sections, mainSplit)
	sections = append(sections, footer)

	baseScreen := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Overlay Modals if active
	switch m.ActiveModal {
	case ModalRestore:
		return PaintSurface(m.RestoreModal.View(), m.Width, m.Height, ColorBackground, ColorText)
	case ModalCherryPick:
		return PaintSurface(m.CherryPickModal.View(), m.Width, m.Height, ColorBackground, ColorText)
	case ModalExport:
		return PaintSurface(m.ExportModal.View(), m.Width, m.Height, ColorBackground, ColorText)
	case ModalRemote:
		return PaintSurface(m.RemoteModal.View(), m.Width, m.Height, ColorBackground, ColorText)
	case ModalHelp:
		return PaintSurface(m.HelpModal.View(), m.Width, m.Height, ColorBackground, ColorText)
	}

	return PaintSurface(baseScreen, m.Width, m.Height, ColorBackground, ColorText)
}

// RunTUI launches the Bubble Tea interactive TUI.
func RunTUI(initialFilter string) error {
	p := tea.NewProgram(
		NewAppModel(nil, nil, initialFilter),
		tea.WithOutput(lipgloss.DefaultRenderer().Output()),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithMouseAllMotion(),
	)
	_, err := p.Run()
	return err
}
