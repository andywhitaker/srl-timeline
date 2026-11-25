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
	ModalTheme
)

// RealtimeCommitMsg is sent when the daemon detects a new commit.
type RealtimeCommitMsg struct {
	Commit models.TimelineCommit
}

// NotificationMsg is sent to display an inline footer toast alert.
type NotificationMsg struct {
	Message  string
	Severity string // "info", "success", "error", "warning"
}

type clearNotificationMsg struct {
	seq int
}

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

type daemonStatusMsg struct {
	running bool
}

type summaryScopesMsg struct {
	scopes map[string]string
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
	ThemeModal      modals.ThemeModal

	// Backend services
	GitBackend  *gitbackend.GitBackend
	SRLClient   *srlclient.SRLClient
	Daemon      *daemon.TimelineDaemon
	Restorer    *restorer.ConfigRestorer
	Exporter    *exporter.ConfigExporter
	BlameEngine *blame.BlameEngine

	// State
	FocusedPane         FocusedPane
	ActiveTab           ActiveTab
	Notification        string
	NotifyStyle         lipgloss.Style
	notifyKind          string
	notifySeq           int
	Width               int
	Height              int
	FocusFilter         bool
	VsLive              bool
	Zoomed              bool
	BaseCommitSHA       string
	baseCommit          *models.TimelineCommit
	parentSHAByFullSHA  map[string]string
	lastHeadSHA         string
	lastDetailCommitSHA string
	lastDetailTab       ActiveTab
	lastCompareKey      string
	scopeBackfilled     map[string]bool
	cachedLiveConfig    map[string]interface{}
	liveConfigFetched   time.Time
	isFetchingLiveCfg   bool
	isCheckingHead      bool
	isRestoring         bool

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

	hb := NewHeaderBarModel(100)
	running, _, _ := daemon.IsAnyDaemonRunning()
	hb.IsRealtime = running

	fb := NewFilterBarModel(100)
	if initialFilter != "" {
		fb.TextInput.SetValue(initialFilter)
	}

	commitChan := make(chan models.TimelineCommit, 10)

	m := AppModel{
		HeaderBar:    hb,
		FilterBar:    fb,
		TimelineView: NewTimelineViewModel(40, 25),
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

		FocusedPane:        PaneTimeline,
		ActiveTab:          TabDiff,
		ActiveModal:        ModalNone,
		parentSHAByFullSHA: make(map[string]string),
		lastHeadSHA:        backend.GetHeadSHA(),
		scopeBackfilled:    make(map[string]bool),
		commitChan:         commitChan,
	}

	// Register daemon listener
	d.RegisterListener(func(c models.TimelineCommit) {
		commitChan <- c
	})

	// Initial timeline load (without blocking on legacy SummaryScope diffs)
	m.refreshTimeline()

	return m
}

// RefreshTheme re-renders all stateful sub-components after a theme change while preserving scroll offsets.
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
		m.NotifyStyle = lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1)
	case "primary":
		m.NotifyStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
	}

	prevTimelineY := m.TimelineView.Viewport.YOffset
	m.TimelineView.UpdateViewportContent()
	m.TimelineView.Viewport.SetYOffset(prevTimelineY)

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

func checkDaemonStatusCmd() tea.Cmd {
	return func() tea.Msg {
		running, _, _ := daemon.IsAnyDaemonRunning()
		return daemonStatusMsg{running: running}
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

func fetchSummaryScopesCmd(backend *gitbackend.GitBackend, shas []string) tea.Cmd {
	if backend == nil || len(shas) == 0 {
		return nil
	}
	batch := append([]string(nil), shas...)
	return func() tea.Msg {
		scopes := make(map[string]string, len(batch))
		for _, sha := range batch {
			scopes[sha] = backend.GetCommitSummaryScope(sha)
		}
		return summaryScopesMsg{scopes: scopes}
	}
}

func (m *AppModel) backfillSummaryScopesCmd() tea.Cmd {
	if m.GitBackend == nil || len(m.TimelineView.Commits) == 0 {
		return nil
	}
	const maxPerPass = 30
	var missing []string
	for _, c := range m.TimelineView.Commits {
		if len(missing) >= maxPerPass {
			break
		}
		if c.FullSHA == "" || strings.TrimSpace(c.SummaryScope) != "" {
			continue
		}
		if m.scopeBackfilled != nil && m.scopeBackfilled[c.FullSHA] {
			continue
		}
		missing = append(missing, c.FullSHA)
	}
	if len(missing) == 0 {
		return nil
	}
	if m.scopeBackfilled == nil {
		m.scopeBackfilled = make(map[string]bool, len(missing))
	}
	for _, sha := range missing {
		m.scopeBackfilled[sha] = true
	}
	return fetchSummaryScopesCmd(m.GitBackend, missing)
}

// Init starts the Bubble Tea loop and background daemon if not already running.
func (m AppModel) Init() tea.Cmd {
	if running, _, _ := daemon.IsAnyDaemonRunning(); !running && m.Daemon != nil {
		m.Daemon.Start()
	}

	return tea.Batch(
		fetchSystemInfoCmd(m.SRLClient),
		fetchRemoteStatusCmd(m.GitBackend),
		checkDaemonStatusCmd(),
		m.backfillSummaryScopesCmd(),
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

func clearNotificationCmd(seq int) tea.Cmd {
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
		return clearNotificationMsg{seq: seq}
	})
}

func (m *AppModel) setNotification(text string, style lipgloss.Style) int {
	m.notifySeq++
	m.Notification = text
	m.NotifyStyle = style
	if text == "" {
		m.notifyKind = ""
		return m.notifySeq
	}
	switch style.GetBackground() {
	case ColorSuccessBg:
		m.notifyKind = "success"
	case ColorDangerBg:
		m.notifyKind = "error"
	case ColorWarningBg, ColorWarning:
		m.notifyKind = "warning"
	case ColorPrimary:
		m.notifyKind = "primary"
	default:
		m.notifyKind = "primary"
	}
	return m.notifySeq
}

func (m *AppModel) setFocusedPane(pane FocusedPane) {
	prevPane := m.FocusedPane
	m.FocusedPane = pane
	m.FocusFilter = (pane == PaneFilter)
	if pane != PaneFilter {
		m.FilterBar.Blur()
	}
	m.TimelineView.IsFocused = (pane == PaneTimeline)
	detailFocused := (pane == PaneDetail)
	m.DiffView.IsFocused = detailFocused
	m.ConfigView.IsFocused = detailFocused
	m.BlameView.IsFocused = detailFocused
	if prevPane != pane {
		prevY := m.TimelineView.Viewport.YOffset
		m.TimelineView.UpdateViewportContent()
		m.TimelineView.Viewport.SetYOffset(prevY)
		if m.ActiveTab == TabBlame {
			m.BlameView.lastRendered = ""
			m.BlameView.UpdateContent()
		}
	}
}

func computeSplitOuterWidths(totalWidth int) (int, int) {
	if totalWidth <= 0 {
		return 0, 0
	}
	if totalWidth < 7 {
		tl := totalWidth / 2
		return tl, totalWidth - tl
	}

	timelineOuterWidth := (totalWidth * 34) / 100
	if timelineOuterWidth < 34 {
		timelineOuterWidth = 34
	}
	if timelineOuterWidth > 50 {
		timelineOuterWidth = 50
	}

	// Keep safe narrow fallback when totalWidth is small (e.g. 80 -> 34/45, 40 -> 19/20)
	maxTimeline := totalWidth - 42
	if half := (totalWidth - 1) / 2; maxTimeline < half {
		maxTimeline = half
	}
	if timelineOuterWidth > maxTimeline {
		timelineOuterWidth = maxTimeline
	}
	if timelineOuterWidth < 3 {
		timelineOuterWidth = 3
	}

	detailOuterWidth := totalWidth - timelineOuterWidth - 1
	if detailOuterWidth < 3 {
		detailOuterWidth = 3
		timelineOuterWidth = totalWidth - detailOuterWidth - 1
		if timelineOuterWidth < 3 {
			timelineOuterWidth = 3
		}
	}
	return timelineOuterWidth, detailOuterWidth
}

func (m *AppModel) syncPaneHeights() {
	m.DiffView.Zoomed = m.Zoomed
	m.ConfigView.Zoomed = m.Zoomed
	m.BlameView.Zoomed = m.Zoomed

	if m.Width == 0 || m.Height == 0 {
		return
	}

	// Rows: 0=Header, 1=Filter, 2..Height-2=Main Split (outer=Height-3, inner=Height-5), Height-1=Footer
	paneInnerHeight := m.Height - 5
	if paneInnerHeight < 3 {
		paneInnerHeight = 3
	}

	timelineOuterWidth, detailOuterWidth := computeSplitOuterWidths(m.Width)
	if m.Zoomed {
		detailOuterWidth = m.Width
	}

	timelineInnerWidth := timelineOuterWidth - 2
	if timelineInnerWidth < 1 {
		timelineInnerWidth = 1
	}
	detailInnerWidth := detailOuterWidth - 2
	if detailInnerWidth < 1 {
		detailInnerWidth = 1
	}

	m.TimelineView.SetSize(timelineInnerWidth, paneInnerHeight)
	m.DiffView.SetSize(detailInnerWidth, paneInnerHeight)
	m.ConfigView.SetSize(detailInnerWidth, paneInnerHeight)
	m.BlameView.SetSize(detailInnerWidth, paneInnerHeight)
}

func (m *AppModel) toggleVsLive() tea.Cmd {
	var cmds []tea.Cmd
	m.VsLive = !m.VsLive
	m.DiffView.VsLive = m.VsLive

	if m.VsLive && m.BaseCommitSHA != "" {
		m.BaseCommitSHA = ""
		m.baseCommit = nil
		m.TimelineView.BaseCommitSHA = ""
		prevY := m.TimelineView.Viewport.YOffset
		m.TimelineView.UpdateViewportContent()
		m.TimelineView.Viewport.SetYOffset(prevY)
	}

	m.DiffView.ResetScroll()

	var notif string
	var toastStyle lipgloss.Style
	if m.VsLive {
		id := "revision"
		if sel := m.TimelineView.SelectedCommit(); sel != nil && strings.TrimSpace(sel.CommitID) != "" {
			id = sel.CommitID
		}
		notif = fmt.Sprintf("Comparing %s → Live Running Config (press 4 to return to vs Prev)", id)
		toastStyle = lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1)
		if !m.isFetchingLiveCfg {
			m.isFetchingLiveCfg = true
			cmds = append(cmds, fetchLiveConfigCmd(m.SRLClient))
		}
	} else {
		notif = "Comparing vs Previous Commit (press 4 to compare vs Running Config)"
		toastStyle = lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
	}

	seq := m.setNotification(notif, toastStyle)
	cmds = append(cmds, clearNotificationCmd(seq))
	m.updateActiveTabContent()
	return tea.Batch(cmds...)
}

func (m *AppModel) toggleBaseCommit() tea.Cmd {
	selected := m.TimelineView.SelectedCommit()
	if selected == nil {
		return nil
	}

	if m.BaseCommitSHA == selected.FullSHA {
		m.BaseCommitSHA = ""
		m.baseCommit = nil
		m.TimelineView.BaseCommitSHA = ""
		prevY := m.TimelineView.Viewport.YOffset
		m.TimelineView.UpdateViewportContent()
		m.TimelineView.Viewport.SetYOffset(prevY)
		m.updateActiveTabContent()
		seq := m.setNotification(
			"Unpinned compare base commit",
			lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
		)
		return clearNotificationCmd(seq)
	}

	bc := *selected
	m.baseCommit = &bc
	m.BaseCommitSHA = selected.FullSHA
	m.TimelineView.BaseCommitSHA = m.BaseCommitSHA
	if m.VsLive {
		m.VsLive = false
		m.DiffView.VsLive = false
	}

	prevY := m.TimelineView.Viewport.YOffset
	m.TimelineView.UpdateViewportContent()
	m.TimelineView.Viewport.SetYOffset(prevY)
	m.updateActiveTabContent()

	seq := m.setNotification(
		fmt.Sprintf("Pinned base %s — select another commit to compare (m to unpin)", selected.CommitID),
		lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
	)
	return clearNotificationCmd(seq)
}

func (m *AppModel) jumpToBlameSelectedCommit() tea.Cmd {
	entry := m.BlameView.SelectedEntry()
	if entry == nil || strings.TrimSpace(entry.CommitSHA) == "" {
		return nil
	}

	target := strings.TrimSpace(entry.CommitSHA)
	foundIdx := -1
	for i, c := range m.TimelineView.Commits {
		if c.FullSHA == target ||
			(len(target) >= 6 && strings.HasPrefix(c.FullSHA, target)) ||
			(len(c.FullSHA) >= 6 && strings.HasPrefix(target, c.FullSHA)) ||
			c.CommitID == target {
			foundIdx = i
			break
		}
	}

	if foundIdx < 0 {
		seq := m.setNotification(
			fmt.Sprintf("Commit %s not in current filtered timeline", entry.ShortSHA()),
			lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1),
		)
		return clearNotificationCmd(seq)
	}

	m.TimelineView.SelectIndex(foundIdx)
	m.ActiveTab = TabDiff
	m.updateActiveTabContent()
	return nil
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

	case daemonStatusMsg:
		m.HeaderBar.IsRealtime = msg.running

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

	case summaryScopesMsg:
		if len(msg.scopes) > 0 {
			if m.scopeBackfilled == nil {
				m.scopeBackfilled = make(map[string]bool, len(msg.scopes))
			}
			for sha := range msg.scopes {
				m.scopeBackfilled[sha] = true
			}
			updated := false
			for i := range m.TimelineView.Commits {
				sha := m.TimelineView.Commits[i].FullSHA
				if scope, ok := msg.scopes[sha]; ok && scope != "" && m.TimelineView.Commits[i].SummaryScope == "" {
					m.TimelineView.Commits[i].SummaryScope = scope
					updated = true
				}
			}
			if updated {
				prevY := m.TimelineView.Viewport.YOffset
				m.TimelineView.UpdateViewportContent()
				m.TimelineView.Viewport.SetYOffset(prevY)
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
		cmds = append(cmds, checkDaemonStatusCmd())
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
			cmds = append(cmds, m.backfillSummaryScopesCmd())
			selected := m.TimelineView.SelectedCommit()
			if selected != nil && selected.FullSHA == currentHead {
				seq := m.setNotification(
					fmt.Sprintf("Live update by %s: %s", selected.Author, selected.Message),
					lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				cmds = append(cmds, clearNotificationCmd(seq))
			}
		}

	case RealtimeCommitMsg:
		m.lastHeadSHA = msg.Commit.FullSHA
		m.cachedLiveConfig = m.GitBackend.GetLatestCommitConfig()
		m.liveConfigFetched = time.Now()
		seq := m.setNotification(
			fmt.Sprintf("Realtime commit by %s: %s", msg.Commit.Author, msg.Commit.Message),
			lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
		)
		cmds = append(cmds, clearNotificationCmd(seq))
		m.refreshTimeline()
		cmds = append(cmds, m.backfillSummaryScopesCmd())
		cmds = append(cmds, waitForRealtimeCommit(m.commitChan))

	case NotificationMsg:
		m.isRestoring = false
		var style lipgloss.Style
		switch msg.Severity {
		case "error":
			style = lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
		case "warning":
			style = lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1)
		default:
			style = lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1)
		}
		seq := m.setNotification(msg.Message, style)
		m.refreshTimeline()
		cmds = append(cmds, m.backfillSummaryScopesCmd(), clearNotificationCmd(seq))

	case clearNotificationMsg:
		if msg.seq == m.notifySeq {
			m.Notification = ""
			m.notifyKind = ""
		}

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
				m.setFocusedPane(PaneTimeline)
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			case "enter", "tab", "down":
				m.setFocusedPane(PaneTimeline)
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			case "ctrl+c":
				if m.FilterBar.Value() != "" {
					m.FilterBar.Clear()
					m.refreshTimeline()
					return m, m.backfillSummaryScopesCmd()
				}
				if m.Daemon != nil {
					m.Daemon.Stop()
				}
				return m, tea.Quit
			case "ctrl+u":
				m.FilterBar.Clear()
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			case "ctrl+t":
				m.ThemeModal = modals.NewThemeModal(m.Width, m.Height)
				m.ActiveModal = ModalTheme
				return m, nil
			default:
				var cmd tea.Cmd
				m.FilterBar, cmd = m.FilterBar.Update(msg)
				m.refreshTimeline()
				return m, tea.Batch(cmd, m.backfillSummaryScopesCmd())
			}
		}

		// 2. Filter Activation Shortcut
		if msg.String() == "/" {
			m.setFocusedPane(PaneFilter)
			return m, m.FilterBar.Focus()
		}

		// 3. Esc hierarchy & 'x' filter clear
		switch msg.String() {
		case "esc":
			if m.Zoomed {
				m.Zoomed = false
				m.syncPaneHeights()
				return m, nil
			}
			if m.FocusedPane == PaneDetail {
				m.setFocusedPane(PaneTimeline)
				return m, nil
			}
			if m.FilterBar.Value() != "" {
				m.FilterBar.Clear()
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			}
			return m, nil

		case "x":
			if m.FilterBar.Value() != "" {
				m.FilterBar.Clear()
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			}
		}

		// 4. Pane Navigation (Tab, Shift+Tab, Left, Right, h, l)
		switch msg.String() {
		case "tab", "shift+tab":
			if !m.Zoomed {
				if m.FocusedPane == PaneTimeline {
					m.setFocusedPane(PaneDetail)
				} else {
					m.setFocusedPane(PaneTimeline)
				}
			}
			return m, nil

		case "right", "l":
			m.setFocusedPane(PaneDetail)
			return m, nil

		case "left", "h":
			if !m.Zoomed {
				m.setFocusedPane(PaneTimeline)
			}
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
			if m.Daemon != nil {
				m.Daemon.Stop()
			}
			return m, tea.Quit

		case "z":
			m.Zoomed = !m.Zoomed
			if m.Zoomed {
				m.setFocusedPane(PaneDetail)
			}
			m.syncPaneHeights()
			return m, nil

		case "m":
			return m, m.toggleBaseCommit()

		case "T":
			t := NextTheme()
			m.RefreshTheme()
			seq := m.setNotification(
				fmt.Sprintf("Theme: %s (%s) — [T] cycle · [Ctrl+t] picker", t.DisplayName, t.Name),
				lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
			)
			return m, clearNotificationCmd(seq)

		case "ctrl+t":
			m.ThemeModal = modals.NewThemeModal(m.Width, m.Height)
			m.ActiveModal = ModalTheme
			return m, nil

		case "r":
			if m.isRestoring {
				seq := m.setNotification(
					"Restore/cherry-pick operation already in progress...",
					lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1),
				)
				return m, clearNotificationCmd(seq)
			}
			selected := m.TimelineView.SelectedCommit()
			if selected != nil {
				runningCfg := m.cachedLiveConfig
				if len(runningCfg) == 0 {
					runningCfg = m.GitBackend.GetLatestCommitConfig()
				}
				targetCfg := m.GitBackend.GetConfigAtCommit(selected.FullSHA)
				impact := differ.SemanticDiff(runningCfg, targetCfg, "")
				m.RestoreModal = modals.NewRestoreModalWithImpact(*selected, impact, m.Width, m.Height)
				m.ActiveModal = ModalRestore
			}
			return m, nil

		case "R", "ctrl+r", "f5":
			m.lastHeadSHA = m.GitBackend.GetHeadSHA()
			m.refreshTimeline()
			seq := m.setNotification(
				"Refreshed timeline from disk",
				lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
			)
			return m, tea.Batch(m.backfillSummaryScopesCmd(), clearNotificationCmd(seq))

		case "p":
			if m.isRestoring {
				seq := m.setNotification(
					"Restore/cherry-pick operation already in progress...",
					lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1),
				)
				return m, clearNotificationCmd(seq)
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

		// 7. View-specific sub-options (instant visual toggles without noisy toasts)
		switch m.ActiveTab {
		case TabDiff:
			switch msg.String() {
			case "1", "u":
				m.DiffView.DiffMode = "unified"
				m.DiffView.ResetScroll()
				m.DiffView.UpdateContent()
				return m, nil
			case "2":
				m.DiffView.DiffMode = "path"
				m.DiffView.ResetScroll()
				m.DiffView.UpdateContent()
				return m, nil
			case "3":
				m.DiffView.DiffMode = "cli"
				m.DiffView.ResetScroll()
				m.DiffView.UpdateContent()
				return m, nil
			case "4", "v":
				return m, m.toggleVsLive()
			}

		case TabConfig:
			switch msg.String() {
			case "1":
				m.ConfigView.FormatMode = "cli"
				m.ConfigView.ResetScroll()
				m.ConfigView.UpdateContent()
				return m, nil
			case "2":
				m.ConfigView.FormatMode = "json"
				m.ConfigView.ResetScroll()
				m.ConfigView.UpdateContent()
				return m, nil
			}

		case TabBlame:
			switch msg.String() {
			case "1":
				m.BlameView.ViewMode = "attribution"
				m.BlameView.ResetScroll()
				m.BlameView.UpdateContent()
				return m, nil
			case "2":
				m.BlameView.ViewMode = "analytics"
				m.BlameView.ResetScroll()
				m.BlameView.UpdateContent()
				return m, nil
			case "w":
				m.BlameView.WrapLines = !m.BlameView.WrapLines
				m.BlameView.ResetScroll()
				m.BlameView.UpdateContent()
				return m, nil
			}
		}

		// 8. Navigation inside active pane
		if m.FocusedPane == PaneDetail {
			switch msg.String() {
			case "enter":
				if m.ActiveTab == TabBlame && m.BlameView.ViewMode == "attribution" {
					return m, m.jumpToBlameSelectedCommit()
				}
				return m, nil
			case "up", "k":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.LineUp(2)
				case TabConfig:
					m.ConfigView.Viewport.LineUp(2)
				case TabBlame:
					if m.BlameView.ViewMode == "attribution" {
						m.BlameView.SelectPrev()
					} else {
						m.BlameView.Viewport.LineUp(2)
					}
				}
				return m, nil
			case "down", "j":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.LineDown(2)
				case TabConfig:
					m.ConfigView.Viewport.LineDown(2)
				case TabBlame:
					if m.BlameView.ViewMode == "attribution" {
						m.BlameView.SelectNext()
					} else {
						m.BlameView.Viewport.LineDown(2)
					}
				}
				return m, nil
			case "pgup", "ctrl+u":
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
			case "home":
				switch m.ActiveTab {
				case TabDiff:
					m.DiffView.Viewport.GotoTop()
				case TabConfig:
					m.ConfigView.Viewport.GotoTop()
				case TabBlame:
					if m.BlameView.ViewMode == "attribution" && len(m.BlameView.Entries) > 0 {
						m.BlameView.SelectedLine = 0
						m.BlameView.lastRendered = ""
						m.BlameView.UpdateContent()
					}
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
					if m.BlameView.ViewMode == "attribution" && len(m.BlameView.Entries) > 0 {
						m.BlameView.SelectedLine = len(m.BlameView.Entries) - 1
						m.BlameView.lastRendered = ""
						m.BlameView.UpdateContent()
					}
					m.BlameView.Viewport.GotoBottom()
				}
				return m, nil
			}
			return m, nil
		}

		// Navigate timeline commits
		switch msg.String() {
		case "enter":
			m.setFocusedPane(PaneDetail)
			return m, nil
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

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m AppModel) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Forward mouse events to active scrollable/interactive modals
	switch m.ActiveModal {
	case ModalTheme:
		var changed, confirmed, close bool
		m.ThemeModal, changed, confirmed, close = m.ThemeModal.Update(msg)
		if changed {
			m.RefreshTheme()
		}
		if close {
			m.ActiveModal = ModalNone
			if confirmed {
				t := CurrentTheme()
				seq := m.setNotification(
					fmt.Sprintf("Theme: %s", t.DisplayName),
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				return m, clearNotificationCmd(seq)
			}
		}
		return m, nil

	case ModalHelp:
		var close bool
		m.HelpModal, close = m.HelpModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
		}
		return m, nil

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
					lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1),
				)
				sha := m.CherryPickModal.Commit.FullSHA
				return m, performCherryPickCmd(m.Restorer, sha, selectedSubtrees...)
			}
		}
		return m, nil

	case ModalNone:
		// proceed to main screen hit-testing
	default:
		return m, nil
	}

	if m.Width <= 0 || m.Height <= 0 || msg.Y < 0 || msg.X < 0 || msg.X >= m.Width {
		return m, nil
	}

	// Row 0: Header Bar (only Theme pill is clickable; never changes pane focus)
	if msg.Y == 0 {
		if msg.Type == tea.MouseLeft {
			startX, endX := m.HeaderBar.ThemeHitRange()
			if startX >= 0 && msg.X >= startX && msg.X < endX {
				m.ThemeModal = modals.NewThemeModal(m.Width, m.Height)
				m.ActiveModal = ModalTheme
			}
		}
		return m, nil
	}

	// Row 1: Filter Bar (explicit Clear hit-range, quick chips, else focus Filter)
	if msg.Y == 1 {
		if msg.Type == tea.MouseLeft {
			if clearStart, clearEnd := m.FilterBar.ClearHitRange(); clearStart >= 0 && msg.X >= clearStart && msg.X < clearEnd {
				m.FilterBar.Clear()
				m.setFocusedPane(PaneTimeline)
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			}
			for _, chip := range m.FilterBar.QuickChipHitRanges() {
				if msg.X >= chip.StartX && msg.X < chip.EndX {
					m.FilterBar.TextInput.SetValue(chip.Label)
					m.setFocusedPane(PaneTimeline)
					m.refreshTimeline()
					return m, m.backfillSummaryScopesCmd()
				}
			}
			m.setFocusedPane(PaneFilter)
			return m, m.FilterBar.Focus()
		}
		return m, nil
	}

	// Row m.Height-1 (and beyond): Footer Bar — never leaks into panes
	if msg.Y >= m.Height-1 {
		return m, nil
	}

	// Main Panes: Y in [2, m.Height-2]
	timelineOuterW, detailOuterW := computeSplitOuterWidths(m.Width)
	detailStartX := timelineOuterW + 1
	if m.Zoomed {
		timelineOuterW = 0
		detailOuterW = m.Width
		detailStartX = 0
	}

	// Scroll wheel: strictly routed by cursor X (never hijacks Timeline wheel when Detail is focused)
	switch msg.Type {
	case tea.MouseWheelUp:
		if !m.Zoomed && msg.X < detailStartX {
			m.TimelineView.Prev()
			m.updateActiveTabContent()
			return m, nil
		}
		if msg.X >= detailStartX {
			switch m.ActiveTab {
			case TabDiff:
				m.DiffView.Viewport.LineUp(3)
			case TabConfig:
				m.ConfigView.Viewport.LineUp(3)
			case TabBlame:
				m.BlameView.Viewport.LineUp(3)
			}
			return m, nil
		}
		return m, nil

	case tea.MouseWheelDown:
		if !m.Zoomed && msg.X < detailStartX {
			m.TimelineView.Next()
			m.updateActiveTabContent()
			return m, nil
		}
		if msg.X >= detailStartX {
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
		return m, nil
	}

	if msg.Type != tea.MouseLeft {
		return m, nil
	}

	// Left click on Timeline Pane
	if !m.Zoomed && msg.X < timelineOuterW {
		m.setFocusedPane(PaneTimeline)
		if msg.Y >= 3 && msg.Y <= m.Height-3 {
			lpc := m.TimelineView.LinesPerCommit()
			if lpc < 1 {
				lpc = 2
			}
			clickedRow := (msg.Y - 3 + m.TimelineView.Viewport.YOffset) / lpc
			if clickedRow >= 0 && clickedRow < len(m.TimelineView.Commits) {
				m.TimelineView.SelectIndex(clickedRow)
				m.updateActiveTabContent()
			}
		}
		return m, nil
	}

	// Left click on Detail Pane
	if msg.X >= detailStartX {
		// Detail Top Border (Y == 2): embedded tabs hit-testing
		if msg.Y == 2 {
			relX := msg.X - detailStartX
			for _, chip := range DetailTabHitRanges(detailOuterW, m.Zoomed) {
				if relX >= chip.StartX && relX < chip.EndX {
					switch chip.Label {
					case "diff":
						m.ActiveTab = TabDiff
					case "config":
						m.ActiveTab = TabConfig
					case "blame":
						m.ActiveTab = TabBlame
					}
					m.setFocusedPane(PaneDetail)
					m.updateActiveTabContent()
					return m, nil
				}
			}
			m.setFocusedPane(PaneDetail)
			return m, nil
		}

		// Detail Toolbar (Y == 3): active view's ControlHitRanges
		if msg.Y == 3 {
			m.setFocusedPane(PaneDetail)
			innerX := msg.X - detailStartX - 1
			if innerX < 0 {
				return m, nil
			}
			switch m.ActiveTab {
			case TabDiff:
				for _, chip := range m.DiffView.ControlHitRanges() {
					if innerX >= chip.StartX && innerX < chip.EndX {
						switch chip.Label {
						case "unified":
							m.DiffView.DiffMode = "unified"
							m.DiffView.ResetScroll()
							m.DiffView.UpdateContent()
							return m, nil
						case "path":
							m.DiffView.DiffMode = "path"
							m.DiffView.ResetScroll()
							m.DiffView.UpdateContent()
							return m, nil
						case "cli":
							m.DiffView.DiffMode = "cli"
							m.DiffView.ResetScroll()
							m.DiffView.UpdateContent()
							return m, nil
						case "live":
							return m, m.toggleVsLive()
						}
					}
				}
			case TabConfig:
				for _, chip := range m.ConfigView.ControlHitRanges() {
					if innerX >= chip.StartX && innerX < chip.EndX {
						switch chip.Label {
						case "cli":
							m.ConfigView.FormatMode = "cli"
							m.ConfigView.ResetScroll()
							m.ConfigView.UpdateContent()
							return m, nil
						case "json":
							m.ConfigView.FormatMode = "json"
							m.ConfigView.ResetScroll()
							m.ConfigView.UpdateContent()
							return m, nil
						}
					}
				}
			case TabBlame:
				for _, chip := range m.BlameView.ControlHitRanges() {
					if innerX >= chip.StartX && innerX < chip.EndX {
						switch chip.Label {
						case "attribution":
							m.BlameView.ViewMode = "attribution"
							m.BlameView.ResetScroll()
							m.BlameView.UpdateContent()
							return m, nil
						case "analytics":
							m.BlameView.ViewMode = "analytics"
							m.BlameView.ResetScroll()
							m.BlameView.UpdateContent()
							return m, nil
						case "wrap":
							m.BlameView.WrapLines = !m.BlameView.WrapLines
							m.BlameView.ResetScroll()
							m.BlameView.UpdateContent()
							return m, nil
						}
					}
				}
			}
			return m, nil
		}

		// Detail Content Area (Y >= 4 && Y <= m.Height-2)
		m.setFocusedPane(PaneDetail)
		if m.ActiveTab == TabBlame && m.BlameView.ViewMode == "attribution" && msg.Y <= m.Height-3 {
			vpRow := (msg.Y - 3) - m.BlameView.HeaderRows()
			if vpRow >= 0 {
				if entry := m.BlameView.EntryAtRow(vpRow); entry != nil {
					for i := range m.BlameView.Entries {
						if &m.BlameView.Entries[i] == entry {
							m.BlameView.SelectedLine = i
							m.BlameView.lastRendered = ""
							m.BlameView.UpdateContent()
							break
						}
					}
				}
			}
		}
		return m, nil
	}

	return m, nil
}

func (m AppModel) handleModalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "T" && m.ActiveModal != ModalTheme {
		canCycleTheme := m.ActiveModal == ModalHelp ||
			m.ActiveModal == ModalRestore ||
			(m.ActiveModal == ModalCherryPick && !m.CherryPickModal.UseCustom) ||
			(m.ActiveModal == ModalExport && !m.ExportModal.IsEditingPath()) ||
			(m.ActiveModal == ModalRemote && m.RemoteModal.FocusedIndex != 0)
		if canCycleTheme {
			NextTheme()
			m.RefreshTheme()
			return m, nil
		}
	}

	switch m.ActiveModal {
	case ModalTheme:
		var changed, confirmed, close bool
		m.ThemeModal, changed, confirmed, close = m.ThemeModal.Update(msg)
		if changed {
			m.RefreshTheme()
		}
		if close {
			m.ActiveModal = ModalNone
			if confirmed {
				t := CurrentTheme()
				seq := m.setNotification(
					fmt.Sprintf("Theme: %s", t.DisplayName),
					lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				return m, clearNotificationCmd(seq)
			}
		}

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
					lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1),
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
				var seq int
				if err := m.GitBackend.SaveRemoteConfig(m.RemoteModal.Config); err != nil {
					seq = m.setNotification(
						fmt.Sprintf("Failed to save remote config: %v", err),
						lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
					)
				} else {
					seq = m.setNotification(
						"Saved Remote Git configuration",
						lipgloss.NewStyle().Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
					)
				}
				return m, clearNotificationCmd(seq)
			}
		}
		if pushNow {
			if err := m.GitBackend.SaveRemoteConfig(m.RemoteModal.Config); err != nil {
				m.ActiveModal = ModalNone
				seq := m.setNotification(
					fmt.Sprintf("Failed to save remote config: %v", err),
					lipgloss.NewStyle().Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
				)
				return m, clearNotificationCmd(seq)
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
	m.ThemeModal.Width = width
	m.ThemeModal.Height = height

	// Only resize components and re-wrap viewports — never re-query Git on resize!
	m.syncPaneHeights()
}

func (m *AppModel) refreshTimeline() {
	filterQuery := m.FilterBar.Value()
	allCommits := m.GitBackend.GetTimeline(200, "")
	parentMap := make(map[string]string, len(allCommits))
	for i := 0; i+1 < len(allCommits); i++ {
		if allCommits[i].FullSHA != "" {
			parentMap[allCommits[i].FullSHA] = allCommits[i+1].CommitID
		}
	}
	m.parentSHAByFullSHA = parentMap

	var filteredCommits []models.TimelineCommit
	if filterQuery != "" {
		filteredCommits = m.GitBackend.GetTimeline(200, filterQuery)
	} else {
		filteredCommits = allCommits
	}

	if m.BaseCommitSHA != "" && m.baseCommit == nil {
		for _, c := range allCommits {
			if c.FullSHA == m.BaseCommitSHA {
				bc := c
				m.baseCommit = &bc
				break
			}
		}
	}

	m.TimelineView.FilterQuery = filterQuery
	m.TimelineView.HeadSHA = m.lastHeadSHA
	m.TimelineView.BaseCommitSHA = m.BaseCommitSHA
	m.FilterBar.SetCounts(len(filteredCommits), len(allCommits))
	m.TimelineView.SetCommits(filteredCommits)
	m.updateActiveTabContent()
}

// updateActiveTabContent updates only the active detail tab, clearing all detail views
// when no commit matches and resetting scroll only when commit, tab, or compare target changes.
func (m *AppModel) updateActiveTabContent() {
	filterQuery := m.FilterBar.Value()
	m.TimelineView.FilterQuery = filterQuery
	m.TimelineView.HeadSHA = m.lastHeadSHA
	m.TimelineView.BaseCommitSHA = m.BaseCommitSHA
	m.BlameView.FilterQuery = filterQuery

	selected := m.TimelineView.SelectedCommit()
	if selected == nil {
		msg := "No commits available."
		if q := strings.TrimSpace(filterQuery); q != "" {
			msg = fmt.Sprintf("No commits match filter '%s'. Press Esc to clear.", q)
		}
		m.DiffView.Clear(msg)
		m.ConfigView.Clear(msg)
		m.BlameView.Clear(msg)
		m.lastDetailCommitSHA = ""
		m.lastCompareKey = ""
		return
	}

	compareKey := "parent|" + filterQuery
	if m.VsLive {
		compareKey = "live|" + filterQuery
	} else if m.BaseCommitSHA != "" {
		compareKey = "base:" + m.BaseCommitSHA + "|" + filterQuery
	}

	needsReset := selected.FullSHA != m.lastDetailCommitSHA ||
		m.ActiveTab != m.lastDetailTab ||
		compareKey != m.lastCompareKey

	m.lastDetailCommitSHA = selected.FullSHA
	m.lastDetailTab = m.ActiveTab
	m.lastCompareKey = compareKey

	selCopy := *selected
	m.DiffView.Commit = &selCopy
	m.DiffView.ParentSHA = m.parentSHAByFullSHA[selected.FullSHA]
	m.DiffView.FilterQuery = filterQuery
	m.DiffView.BaseCommit = m.baseCommit
	m.ConfigView.Commit = &selCopy
	m.ConfigView.BaseCommit = m.baseCommit
	m.BlameView.Commit = &selCopy
	m.BlameView.BaseCommit = m.baseCommit

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
		} else if m.BaseCommitSHA != "" {
			prevCfg = m.GitBackend.GetConfigAtCommit(m.BaseCommitSHA)
			currCfg = m.GitBackend.GetConfigAtCommit(selected.FullSHA)
		} else {
			parentSHA := fmt.Sprintf("%s~1", selected.FullSHA)
			prevCfg = m.GitBackend.GetConfigAtCommit(parentSHA)
			currCfg = m.GitBackend.GetConfigAtCommit(selected.FullSHA)
		}
		diffRes := differ.SemanticDiff(prevCfg, currCfg, filterQuery)
		m.DiffView.SetDiff(diffRes)
		if needsReset {
			m.DiffView.ResetScroll()
		}

	case TabConfig:
		cfg := m.GitBackend.GetConfigAtCommit(selected.FullSHA)
		m.ConfigView.SetConfig(cfg, filterQuery)
		if needsReset {
			m.ConfigView.ResetScroll()
		}

	case TabBlame:
		entries := m.BlameEngine.GetBlameLinesAtCommit("cli", selected.FullSHA, filterQuery)
		stats := m.BlameEngine.GetContributorStatsAtCommit("cli", selected.FullSHA)
		m.BlameView.SetBlame(entries, stats)
		if needsReset {
			m.BlameView.ResetScroll()
		}
	}
}

// View renders the full application screen with exact m.Height rows and zero notification jump.
func (m AppModel) View() string {
	if m.Width == 0 {
		return "Loading Timeline..."
	}

	// Row 0: Header Bar (1 row, borderless)
	header := m.HeaderBar.View()

	// Row 1: Filter Bar (1 row, borderless)
	filterBar := m.FilterBar.View()

	// Active Detail Panel (tabs embedded in Detail's top border)
	var detailContent string
	switch m.ActiveTab {
	case TabDiff:
		detailContent = m.DiffView.View()
	case TabConfig:
		detailContent = m.ConfigView.View()
	case TabBlame:
		detailContent = m.BlameView.View()
	}

	// Rows 2 .. m.Height-2: Main Split View (exact outer height m.Height-3)
	var mainSplit string
	if m.Zoomed {
		mainSplit = detailContent
	} else {
		paneInnerHeight := m.Height - 5
		if paneInnerHeight < 3 {
			paneInnerHeight = 3
		}
		splitHeight := paneInnerHeight + 2
		splitSep := PaintSurface("", 1, splitHeight, ColorBackground, ColorText)
		mainSplit = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.TimelineView.View(),
			splitSep,
			detailContent,
		)
	}

	// Row m.Height-1: Footer Bar (1 row, borderless, inline toast when Notification != "")
	footer := m.FooterBar.ViewContext(FooterContext{
		FilterActive: m.FilterBar.Value() != "",
		FocusedPane:  int(m.FocusedPane),
		ActiveTab:    int(m.ActiveTab),
		Zoomed:       m.Zoomed,
		HasBase:      m.BaseCommitSHA != "",
		VsLive:       m.VsLive,
		ToastText:    m.Notification,
		ToastKind:    m.notifyKind,
	})

	baseScreen := lipgloss.JoinVertical(lipgloss.Left, header, filterBar, mainSplit, footer)

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
	case ModalTheme:
		return PaintSurface(m.ThemeModal.View(), m.Width, m.Height, ColorBackground, ColorText)
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
	)
	_, err := p.Run()
	return err
}
