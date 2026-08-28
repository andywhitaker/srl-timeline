package tui

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	FocusedPane  FocusedPane
	ActiveTab    ActiveTab
	Notification string
	NotifyStyle  lipgloss.Style
	Width              int
	Height             int
	FocusFilter        bool
	VsLive             bool
	lastHeadSHA        string
	cachedLiveConfig   map[string]interface{}
	liveConfigFetched  time.Time
	isFetchingLiveCfg  bool

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
		if cfg, err := client.GetRunningConfig("/"); err == nil && len(cfg) > 0 {
			_, _, _ = backend.EnsureBaseline(cfg)
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

// Init starts the Bubble Tea loop and background daemon if not already running.
func (m AppModel) Init() tea.Cmd {
	if !daemon.IsDaemonRunning(daemon.DefaultPIDFile) {
		m.Daemon.Start()
	}

	// Load system info
	go func() {
		info := m.SRLClient.GetSystemInfo()
		m.HeaderBar.Hostname = info["host-name"]
		m.HeaderBar.Version = info["version"]
		m.HeaderBar.Chassis = info["chassis"]
	}()

	return tea.Batch(
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

// Update processes Bubble Tea events and coordinates state changes.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.resizeLayout(msg.Width, msg.Height)

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
		currentHead := m.GitBackend.GetHeadSHA()
		if currentHead != "" && currentHead != m.lastHeadSHA {
			m.lastHeadSHA = currentHead
			m.cachedLiveConfig = m.GitBackend.GetLatestCommitConfig()
			m.liveConfigFetched = time.Now()
			m.refreshTimeline()
			selected := m.TimelineView.SelectedCommit()
			if selected != nil && selected.FullSHA == currentHead {
				m.Notification = fmt.Sprintf("⚡ Live update by %s: %s", selected.Author, selected.Message)
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
				cmds = append(cmds, clearNotificationCmd())
			}
		} else if m.VsLive && !m.isFetchingLiveCfg && time.Since(m.liveConfigFetched) > 3*time.Second {
			m.isFetchingLiveCfg = true
			cmds = append(cmds, fetchLiveConfigCmd(m.SRLClient))
		}
		cmds = append(cmds, tickLiveDiskSyncCmd())

	case RealtimeCommitMsg:
		m.lastHeadSHA = msg.Commit.FullSHA
		m.cachedLiveConfig = m.GitBackend.GetLatestCommitConfig()
		m.liveConfigFetched = time.Now()
		m.Notification = fmt.Sprintf("⚡ Realtime commit by %s: %s", msg.Commit.Author, msg.Commit.Message)
		m.NotifyStyle = lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
		cmds = append(cmds, clearNotificationCmd())
		m.refreshTimeline()
		cmds = append(cmds, waitForRealtimeCommit(m.commitChan))

	case NotificationMsg:
		m.Notification = msg.Message
		switch msg.Severity {
		case "error":
			m.NotifyStyle = lipgloss.NewStyle().Background(ColorDanger).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
		case "warning":
			m.NotifyStyle = lipgloss.NewStyle().Background(ColorWarning).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
		default:
			m.NotifyStyle = lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
		}
		m.refreshTimeline()
		cmds = append(cmds, clearNotificationCmd())

	case clearNotificationMsg:
		m.Notification = ""

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

		case "r":
			selected := m.TimelineView.SelectedCommit()
			if selected != nil {
				m.RestoreModal = modals.NewRestoreModal(*selected, m.Width, m.Height)
				m.ActiveModal = ModalRestore
			}
			return m, nil

		case "R", "ctrl+r", "f5":
			m.lastHeadSHA = m.GitBackend.GetHeadSHA()
			m.refreshTimeline()
			m.Notification = "Refreshed timeline from disk"
			m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
			cmds = append(cmds, clearNotificationCmd())
			return m, tea.Batch(cmds...)

		case "p":
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
			remoteCfg.SyncStatus = m.GitBackend.CheckRemoteSyncStatus()
			remoteCfg.PublicKey = m.GitBackend.GetPublicSSHKey()
			m.RemoteModal = modals.NewRemoteModal(remoteCfg, m.Width, m.Height)
			m.ActiveModal = ModalRemote
			return m, nil

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
				m.Notification = "Diff Format: UNIFIED [1]"
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "2":
				m.DiffView.DiffMode = "path"
				m.DiffView.UpdateContent()
				m.Notification = "Diff Format: PATH CHANGES [2]"
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "3":
				m.DiffView.DiffMode = "cli"
				m.DiffView.UpdateContent()
				m.Notification = "Diff Format: FLAT CLI DIFF [3]"
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "4", "v":
				m.VsLive = !m.VsLive
				m.DiffView.VsLive = m.VsLive
				if m.VsLive {
					m.Notification = "Diff Mode: Comparing revision vs LIVE Running Configuration [4/v]"
					if !m.isFetchingLiveCfg {
						m.isFetchingLiveCfg = true
						cmds = append(cmds, fetchLiveConfigCmd(m.SRLClient))
					}
				} else {
					m.Notification = "Diff Mode: Comparing revision vs Previous Commit"
				}
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorWarning).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
				cmds = append(cmds, clearNotificationCmd())
				m.updateActiveTabContent()
				return m, tea.Batch(cmds...)
			}

		case TabConfig:
			switch msg.String() {
			case "1":
				m.ConfigView.FormatMode = "cli"
				m.ConfigView.UpdateContent()
				m.Notification = "Config Format: FLAT CLI SYNTAX [1]"
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
				cmds = append(cmds, clearNotificationCmd())
				return m, tea.Batch(cmds...)
			case "2", "j":
				m.ConfigView.FormatMode = "json"
				m.ConfigView.UpdateContent()
				m.Notification = "Config Format: JSON HIERARCHY [2]"
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
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
			case "pgdown", "space", "f", "ctrl+d":
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
			}
		}

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m AppModel) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.ActiveModal != ModalNone {
		return m, nil
	}

	timelineWidth := (m.Width * 38) / 100
	if timelineWidth < 35 {
		timelineWidth = 35
	}

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

	// 2. Click on Top Tab Bar (Y == 4, X > timelineWidth)
	if msg.Y == 4 && msg.X > timelineWidth && msg.Type == tea.MouseLeft {
		tabOffset := msg.X - timelineWidth
		if tabOffset >= 1 && tabOffset <= 22 {
			m.ActiveTab = TabDiff
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			m.updateActiveTabContent()
			return m, nil
		} else if tabOffset >= 23 && tabOffset <= 46 {
			m.ActiveTab = TabConfig
			m.FocusedPane = PaneDetail
			m.TimelineView.IsFocused = false
			m.DiffView.IsFocused = true
			m.ConfigView.IsFocused = true
			m.BlameView.IsFocused = true
			m.updateActiveTabContent()
			return m, nil
		} else if tabOffset >= 47 && tabOffset <= 70 {
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
			if msg.Y >= 6 {
				clickedRow := (msg.Y - 6 + m.TimelineView.Viewport.YOffset) / 3
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

			// Sub-Option Controls Bar Clicks (Y == 6 or Y == 7)
			if msg.Y == 6 || msg.Y == 7 {
				if m.ActiveTab == TabDiff {
					if tabOffset >= 2 && tabOffset <= 14 {
						m.DiffView.DiffMode = "unified"
						m.DiffView.UpdateContent()
					} else if tabOffset >= 15 && tabOffset <= 26 {
						m.DiffView.DiffMode = "path"
						m.DiffView.UpdateContent()
					} else if tabOffset >= 27 && tabOffset <= 37 {
						m.DiffView.DiffMode = "cli"
						m.DiffView.UpdateContent()
					} else if tabOffset >= 38 && tabOffset <= 56 {
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
					if tabOffset >= 2 && tabOffset <= 18 {
						m.ConfigView.FormatMode = "cli"
						m.ConfigView.UpdateContent()
					} else if tabOffset >= 19 && tabOffset <= 45 {
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
	switch m.ActiveModal {
	case ModalRestore:
		var confirmed, close bool
		m.RestoreModal, confirmed, close = m.RestoreModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if confirmed {
				m.Notification = "Applying configuration restore to SR Linux switch..."
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
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
			if len(selectedSubtrees) > 0 {
				var notifyStr string
				if len(selectedSubtrees) == 1 {
					notifyStr = fmt.Sprintf("Applying cherry-pick '%s' to switch...", selectedSubtrees[0])
				} else {
					notifyStr = fmt.Sprintf("Applying %d cherry-picked paths to switch...", len(selectedSubtrees))
				}
				m.Notification = notifyStr
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorWarning).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
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
			if format == "startup" {
				ok, resMsg, _ := m.Exporter.SaveAsDeviceStartupConfig(m.ExportModal.Commit.FullSHA)
				if ok {
					m.Notification = resMsg
					m.NotifyStyle = lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
				}
			} else if outputPath != "" {
				if format == "json" {
					_, _ = m.Exporter.ExportAsStartupJSON(m.ExportModal.Commit.FullSHA, outputPath)
				} else {
					_, _ = m.Exporter.ExportAsFlatCLI(m.ExportModal.Commit.FullSHA, outputPath)
				}
				m.Notification = fmt.Sprintf("Exported revision %s to %s", m.ExportModal.Commit.FullSHA[:8], outputPath)
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
			}
		}

	case ModalRemote:
		var save, pushNow, close bool
		m.RemoteModal, save, pushNow, close = m.RemoteModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if save {
				_ = m.GitBackend.SaveRemoteConfig(m.RemoteModal.Config)
				m.Notification = "Saved Remote Git configuration"
				m.NotifyStyle = lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
			}
		}
		if pushNow {
			_ = m.GitBackend.SaveRemoteConfig(m.RemoteModal.Config)
			m.ActiveModal = ModalNone
			m.Notification = "Pushing commits to remote repository..."
			m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)
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

	timelinePaneHeight := height - 8
	if timelinePaneHeight < 5 {
		timelinePaneHeight = 5
	}

	detailPaneHeight := height - 9
	if detailPaneHeight < 4 {
		detailPaneHeight = 4
	}

	timelineWidth := (width * 38) / 100
	if timelineWidth < 35 {
		timelineWidth = 35
	}
	detailWidth := width - timelineWidth - 2
	if detailWidth < 40 {
		detailWidth = 40
	}

	m.TimelineView.SetSize(timelineWidth, timelinePaneHeight)
	m.DiffView.SetSize(detailWidth, detailPaneHeight)
	m.ConfigView.SetSize(detailWidth, detailPaneHeight)
	m.BlameView.SetSize(detailWidth, detailPaneHeight)

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

// updateActiveTabContent only updates the currently active tab in microsecond time!
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
		entries := m.BlameEngine.GetBlameLines("cli", filterQuery)
		stats := m.BlameEngine.GetContributorStats("cli")
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
	tabDiff := StyleTabInactive.Render("🔍 Diff View [d]")
	if m.ActiveTab == TabDiff {
		tabDiff = StyleTabActive.Render("🔍 Diff View [d]")
	}
	tabConfig := StyleTabInactive.Render("📄 Full Config [c]")
	if m.ActiveTab == TabConfig {
		tabConfig = StyleTabActive.Render("📄 Full Config [c]")
	}
	tabBlame := StyleTabInactive.Render("🕵️ Blame View [b]")
	if m.ActiveTab == TabBlame {
		tabBlame = StyleTabActive.Render("🕵️ Blame View [b]")
	}

	tabBar := lipgloss.JoinHorizontal(lipgloss.Center, tabDiff, tabConfig, tabBlame)

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

	mainSplit := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.TimelineView.View(),
		" ",
		detailWithTabs,
	)

	// 6. Notification Banner (if any)
	var notifView string
	if m.Notification != "" {
		notifView = m.NotifyStyle.Width(m.Width).Render(m.Notification)
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
		return m.RestoreModal.View()
	case ModalCherryPick:
		return m.CherryPickModal.View()
	case ModalExport:
		return m.ExportModal.View()
	case ModalRemote:
		return m.RemoteModal.View()
	case ModalHelp:
		return m.HelpModal.View()
	}

	return baseScreen
}

// RunTUI launches the Bubble Tea interactive TUI.
func RunTUI(initialFilter string) error {
	if os.Getenv("NO_COLOR") == "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	p := tea.NewProgram(
		NewAppModel(nil, nil, initialFilter),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithMouseAllMotion(),
	)
	_, err := p.Run()
	return err
}
