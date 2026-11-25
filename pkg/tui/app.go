package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/blame"
	"timeline/pkg/daemon"
	"timeline/pkg/differ"
	"timeline/pkg/exporter"
	"timeline/pkg/filter"
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
	PaneRibbon
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
	ModalCommitPicker
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

type headWatchMsg struct {
	sha string
}

type headWatchController struct {
	mu      sync.Mutex
	started bool
	stopped bool
	watcher *gitbackend.FileWatcher
	stopCh  chan struct{}
}

func (c *headWatchController) start(repoDir string, backend *gitbackend.GitBackend, out chan<- string) {
	if c == nil || backend == nil || repoDir == "" {
		return
	}
	c.mu.Lock()
	if c.started || c.stopped {
		c.mu.Unlock()
		return
	}
	refsDir := filepath.Join(repoDir, ".git", "refs", "heads")
	headFile := filepath.Join(repoDir, ".git", "HEAD")
	w, err := gitbackend.NewFileWatcher(refsDir, headFile)
	if err != nil {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.watcher = w
	c.stopCh = make(chan struct{})
	stopCh := c.stopCh
	c.mu.Unlock()

	go func() {
		for {
			select {
			case <-stopCh:
				return
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				if sha := backend.GetHeadSHA(); sha != "" {
					select {
					case out <- sha:
					default:
					}
				}
			}
		}
	}()
}

func (c *headWatchController) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	w := c.watcher
	stopCh := c.stopCh
	c.watcher = nil
	c.stopCh = nil
	c.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	if w != nil {
		_ = w.Close()
	}
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
	HeaderBar      HeaderBarModel
	FilterBar      FilterBarModel
	TimelineRibbon TimelineRibbonModel
	TimelineView   TimelineViewModel
	DiffView       DiffViewModel
	ConfigView     ConfigViewModel
	BlameView      BlameViewModel
	FooterBar      FooterBarModel

	// Modals
	ActiveModal       ActiveModal
	RestoreModal      modals.RestoreModalModel
	CherryPickModal   modals.CherryPickModalModel
	ExportModal       modals.ExportModalModel
	RemoteModal       modals.RemoteModalModel
	HelpModal         modals.HelpModalModel
	ThemeModal        modals.ThemeModal
	CommitPickerModal modals.CommitPickerModal

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
	FocusFilter           bool
	DetailFilterBypass    bool
	VsLive                bool
	Zoomed                bool
	SplitDiff             bool
	FoldUnchanged         bool
	BaseCommitSHA         string
	baseCommit            *models.TimelineCommit
	LeftCommitSHA         string
	leftCommit            *models.TimelineCommit
	RightCommitSHA        string
	rightCommit           *models.TimelineCommit
	allCommits            []models.TimelineCommit
	filteredCommits       []models.TimelineCommit
	userCollapsedRibbon   bool
	parentSHAByFullSHA    map[string]string
	unfilteredDiffChanges map[string][]models.PathChange
	lastHeadSHA           string
	lastDetailCommitSHA string
	lastDetailTab       ActiveTab
	lastCompareKey      string
	scopeBackfilled     map[string]bool
	cachedLiveConfig    map[string]interface{}
	liveConfigFetched   time.Time
	isFetchingLiveCfg   bool
	isCheckingHead      bool
	isRestoring         bool
	scrollDragTarget     string // "", "timeline", "detail"
	scrollDragGrabOffset int    // cursor row offset inside thumb when drag began

	// Realtime channels & kernel inotify HEAD watcher
	commitChan    chan models.TimelineCommit
	headEventChan chan string
	headWatch     *headWatchController
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
		HeaderBar:      hb,
		FilterBar:      fb,
		TimelineRibbon: NewTimelineRibbonModel(100),
		TimelineView:   NewTimelineViewModel(40, 25),
		DiffView:       NewDiffViewModel(70, 25),
		ConfigView:     NewConfigViewModel(70, 25),
		BlameView:      NewBlameViewModel(70, 25),
		FooterBar:      NewFooterBarModel(100),

		GitBackend:  backend,
		SRLClient:   client,
		Daemon:      d,
		Restorer:    r,
		Exporter:    e,
		BlameEngine: b,

		FocusedPane:        PaneTimeline,
		ActiveTab:          TabDiff,
		ActiveModal:        ModalNone,
		FoldUnchanged:      true,
		parentSHAByFullSHA: make(map[string]string),
		lastHeadSHA:        backend.GetHeadSHA(),
		scopeBackfilled:    make(map[string]bool),
		commitChan:         commitChan,
		headEventChan:      make(chan string, 8),
		headWatch:          &headWatchController{},
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
	// Safety-net fallback poll (5s) for daemon status & non-inotify platforms;
	// live commits on Linux arrive instantly (<1ms) via headWatch inotify subscription.
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return checkLiveDiskSyncMsg{}
	})
}

func waitForHeadEvent(ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		sha, ok := <-ch
		if !ok {
			return nil
		}
		return headWatchMsg{sha: sha}
	}
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

// Init starts the Bubble Tea loop, kernel inotify HEAD watcher, and background daemon if needed.
func (m AppModel) Init() tea.Cmd {
	if running, _, _ := daemon.IsAnyDaemonRunning(); !running && m.Daemon != nil {
		m.Daemon.Start()
	}
	if m.headWatch != nil && m.GitBackend != nil {
		m.headWatch.start(m.GitBackend.RepoDir, m.GitBackend, m.headEventChan)
	}

	return tea.Batch(
		fetchSystemInfoCmd(m.SRLClient),
		fetchRemoteStatusCmd(m.GitBackend),
		checkDaemonStatusCmd(),
		m.backfillSummaryScopesCmd(),
		tickLiveDiskSyncCmd(),
		waitForHeadEvent(m.headEventChan),
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
	m.TimelineRibbon.IsFocused = (pane == PaneRibbon)
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

func (m *AppModel) syncRibbonCollapsed() {
	m.TimelineRibbon.Collapsed = m.userCollapsedRibbon || m.Height < 26
}

func (m AppModel) isSplitWorkspaceActive() bool {
	return m.ActiveTab == TabDiff && m.SplitDiff && m.Width >= SplitMinOuterWidth
}

func (m AppModel) hideTimelineList() bool {
	return m.Zoomed
}

func (m AppModel) collapsedRailOuterWidth() int {
	if m.Zoomed && m.Width >= 24 {
		return 3
	}
	return 0
}

// computeWorkspaceOuterWidths computes the exact outer widths for the left sidebar
// (full TimelineView when open, or 3-col collapsed arrow rail when minimized),
// the 1-col separator gap, and the Detail pane.
func (m AppModel) computeWorkspaceOuterWidths() (leftOuterW, detailOuterW, sepW int) {
	if m.Width <= 0 {
		return 0, 0, 0
	}
	if m.hideTimelineList() {
		railW := m.collapsedRailOuterWidth()
		if railW > 0 {
			return railW, m.Width - railW - 1, 1
		}
		return 0, m.Width, 0
	}
	if m.isSplitWorkspaceActive() {
		// Give the left Commit List a compact, readable sidebar so Side-by-Side Split Diff
		// retains generous width (>=68 outer cols even at 100-col terminals).
		leftW := (m.Width * 26) / 100
		if leftW < 28 {
			leftW = 28
		}
		if leftW > 36 {
			leftW = 36
		}
		if maxLeft := m.Width - (SplitMinDetailInnerWidth + 2) - 1; leftW > maxLeft {
			leftW = maxLeft
		}
		if leftW < 20 {
			leftW = 20
		}
		return leftW, m.Width - leftW - 1, 1
	}
	tlW, dtW := computeSplitOuterWidths(m.Width)
	return tlW, dtW, 1
}

func (m *AppModel) focusWorkspaceDefault() {
	if m.hideTimelineList() {
		m.setFocusedPane(PaneDetail)
		return
	}
	m.setFocusedPane(PaneTimeline)
}

func (m *AppModel) setDetailFilterBypass(bypass bool) {
	m.DetailFilterBypass = bypass
	m.FilterBar.DetailBypass = bypass
}

func (m *AppModel) toggleDetailFilterBypass() tea.Cmd {
	if strings.TrimSpace(m.FilterBar.Value()) == "" {
		return nil
	}
	m.setDetailFilterBypass(!m.DetailFilterBypass)
	switch m.ActiveTab {
	case TabDiff:
		m.DiffView.ResetScroll()
	case TabConfig:
		m.ConfigView.ResetScroll()
	case TabBlame:
		m.BlameView.ResetScroll()
	}
	m.updateActiveTabContent()
	return nil
}

func (m AppModel) selectedCommitSHA() string {
	if sel := m.TimelineView.SelectedCommit(); sel != nil {
		return sel.FullSHA
	}
	return ""
}

func semanticChangeCount(res models.SemanticDiffResult) int {
	sum := res.AddedCount + res.ModifiedCount + res.DeletedCount
	if sum > 0 {
		return sum
	}
	return len(filterRedundantPathChanges(res.Changes))
}

func countSemanticLeafChanges(changes []models.PathChange, filterQuery string) int {
	cleanFilter := strings.TrimSpace(filterQuery)
	if cleanFilter != "" {
		return semanticChangeCount(filter.FilterDiffResult(models.SemanticDiffResult{Changes: changes}, cleanFilter))
	}
	count := 0
	for i, c := range changes {
		if (c.DiffType == models.DiffAdded || c.DiffType == models.DiffDeleted) &&
			i+1 < len(changes) && strings.HasPrefix(changes[i+1].Path, c.Path+"/") {
			continue
		}
		count++
	}
	return count
}

func (m *AppModel) getUnfilteredDiffChanges(cacheKey string, oldCfg, newCfg map[string]interface{}) []models.PathChange {
	if m.unfilteredDiffChanges == nil {
		m.unfilteredDiffChanges = make(map[string][]models.PathChange)
	}
	if cached, ok := m.unfilteredDiffChanges[cacheKey]; ok {
		return cached
	}
	res := differ.SemanticDiffFast(oldCfg, newCfg)
	m.unfilteredDiffChanges[cacheKey] = res.Changes
	return res.Changes
}

func (m AppModel) mainStartY() int {
	return 2 + m.TimelineRibbon.OuterHeight()
}

func (m AppModel) findCommitBySHA(sha string) *models.TimelineCommit {
	if sha == "" {
		return nil
	}
	for _, c := range m.allCommits {
		if c.FullSHA == sha || c.CommitID == sha {
			cp := c
			return &cp
		}
	}
	for _, c := range m.TimelineView.Commits {
		if c.FullSHA == sha || c.CommitID == sha {
			cp := c
			return &cp
		}
	}
	return nil
}

func (m AppModel) commitOrderIndex(sha string) int {
	for i, c := range m.allCommits {
		if c.FullSHA == sha {
			return i
		}
	}
	return -1
}

// isOlderCommit reports whether commit a is chronologically older than commit b.
// Uses allCommits newest-first ordering (higher index = older) as a deterministic
// tie-breaker when two commits share the same second-precision timestamp.
func (m AppModel) isOlderCommit(a, b *models.TimelineCommit) bool {
	if a == nil || b == nil {
		return false
	}
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	idxA := m.commitOrderIndex(a.FullSHA)
	idxB := m.commitOrderIndex(b.FullSHA)
	if idxA >= 0 && idxB >= 0 && idxA != idxB {
		return idxA > idxB
	}
	return false
}

func (m AppModel) findParentCommit(sha string) *models.TimelineCommit {
	idx := m.commitOrderIndex(sha)
	if idx >= 0 && idx+1 < len(m.allCommits) {
		cp := m.allCommits[idx+1]
		return &cp
	}
	if len(m.allCommits) > 0 {
		cp := m.allCommits[len(m.allCommits)-1]
		return &cp
	}
	return nil
}

func (m AppModel) selectedCommitTime() time.Time {
	if sel := m.TimelineView.SelectedCommit(); sel != nil && !sel.Timestamp.IsZero() {
		return sel.Timestamp
	}
	if !m.TimelineRibbon.WindowStart.IsZero() && !m.TimelineRibbon.WindowEnd.IsZero() {
		return m.TimelineRibbon.WindowStart.Add(m.TimelineRibbon.WindowEnd.Sub(m.TimelineRibbon.WindowStart) / 2)
	}
	return time.Time{}
}

func (m AppModel) ribbonTimeAtX(x int) time.Time {
	if m.Width > 2 && !m.TimelineRibbon.WindowStart.IsZero() && m.TimelineRibbon.WindowEnd.After(m.TimelineRibbon.WindowStart) {
		innerW := m.Width - 2
		frac := float64(x-1) / float64(maxInt(1, innerW-1))
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		span := m.TimelineRibbon.WindowEnd.Sub(m.TimelineRibbon.WindowStart)
		return m.TimelineRibbon.WindowStart.Add(time.Duration(float64(span) * frac))
	}
	return m.selectedCommitTime()
}

func (m *AppModel) selectCommitBySHA(sha string) bool {
	if sha == "" {
		return false
	}
	for i, c := range m.TimelineView.Commits {
		if c.FullSHA == sha {
			m.TimelineView.SelectIndex(i)
			return true
		}
	}
	return false
}

// normalizeSplitEndpoints guarantees older revision is always Left (A) and newer revision
// (or Live Running Config when VsLive is true) is always Right (B).
func (m *AppModel) normalizeSplitEndpoints() {
	if m.VsLive {
		if m.LeftCommitSHA == "" {
			if m.RightCommitSHA != "" {
				m.LeftCommitSHA = m.RightCommitSHA
			} else if sel := m.TimelineView.SelectedCommit(); sel != nil {
				m.LeftCommitSHA = sel.FullSHA
			} else if len(m.allCommits) > 0 {
				m.LeftCommitSHA = m.allCommits[0].FullSHA
			}
		}
		m.leftCommit = m.findCommitBySHA(m.LeftCommitSHA)
		m.RightCommitSHA = ""
		m.rightCommit = nil
		return
	}

	if m.RightCommitSHA == "" {
		if sel := m.TimelineView.SelectedCommit(); sel != nil {
			m.RightCommitSHA = sel.FullSHA
		} else if len(m.allCommits) > 0 {
			m.RightCommitSHA = m.allCommits[0].FullSHA
		}
	}
	if m.LeftCommitSHA == "" {
		if m.BaseCommitSHA != "" && m.BaseCommitSHA != m.RightCommitSHA {
			m.LeftCommitSHA = m.BaseCommitSHA
		} else if parent := m.findParentCommit(m.RightCommitSHA); parent != nil {
			m.LeftCommitSHA = parent.FullSHA
		} else {
			m.LeftCommitSHA = m.RightCommitSHA
		}
	}

	m.leftCommit = m.findCommitBySHA(m.LeftCommitSHA)
	m.rightCommit = m.findCommitBySHA(m.RightCommitSHA)

	if m.leftCommit != nil && m.rightCommit != nil && m.LeftCommitSHA != m.RightCommitSHA {
		if m.isOlderCommit(m.rightCommit, m.leftCommit) {
			m.LeftCommitSHA, m.RightCommitSHA = m.RightCommitSHA, m.LeftCommitSHA
			m.leftCommit, m.rightCommit = m.rightCommit, m.leftCommit
		}
	}
}

func (m *AppModel) syncCompareMarkers() {
	selSHA := ""
	if sel := m.TimelineView.SelectedCommit(); sel != nil {
		selSHA = sel.FullSHA
	}
	m.TimelineRibbon.SelectedSHA = selSHA
	m.TimelineRibbon.HeadSHA = m.lastHeadSHA

	if m.isSplitWorkspaceActive() || (m.SplitDiff && m.LeftCommitSHA != "") {
		m.TimelineRibbon.SplitCompare = true
		m.TimelineRibbon.LeftSHA = m.LeftCommitSHA
		if m.VsLive {
			m.TimelineRibbon.RightSHA = modals.LiveRunningSentinel
		} else {
			m.TimelineRibbon.RightSHA = m.RightCommitSHA
		}
		m.TimelineView.LeftSHA = m.LeftCommitSHA
		if m.VsLive {
			m.TimelineView.RightSHA = ""
		} else {
			m.TimelineView.RightSHA = m.RightCommitSHA
		}
	} else if m.BaseCommitSHA != "" {
		m.TimelineRibbon.SplitCompare = true
		m.TimelineRibbon.LeftSHA = m.BaseCommitSHA
		m.TimelineRibbon.RightSHA = selSHA
		m.TimelineView.LeftSHA = ""
		m.TimelineView.RightSHA = ""
	} else {
		m.TimelineRibbon.SplitCompare = false
		m.TimelineRibbon.LeftSHA = ""
		m.TimelineRibbon.RightSHA = ""
		m.TimelineView.LeftSHA = ""
		m.TimelineView.RightSHA = ""
	}
}

func (m *AppModel) toggleSplitDiff() tea.Cmd {
	if !m.SplitDiff {
		if m.Width < SplitMinOuterWidth {
			seq := m.setNotification(
				"Split view requires terminal width ≥100 cols — staying in Single Pane",
				lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1),
			)
			return clearNotificationCmd(seq)
		}
		m.SplitDiff = true
		m.ActiveTab = TabDiff
		if !m.VsLive && m.LeftCommitSHA == "" && m.RightCommitSHA == "" {
			if sel := m.TimelineView.SelectedCommit(); sel != nil {
				m.RightCommitSHA = sel.FullSHA
				if m.BaseCommitSHA != "" && m.BaseCommitSHA != sel.FullSHA {
					m.LeftCommitSHA = m.BaseCommitSHA
				} else if parent := m.findParentCommit(sel.FullSHA); parent != nil {
					m.LeftCommitSHA = parent.FullSHA
				}
			}
		}
		m.normalizeSplitEndpoints()
		m.setFocusedPane(PaneDetail)
		m.syncPaneHeights()
		m.syncCompareMarkers()
		m.DiffView.ResetScroll()
		m.updateActiveTabContent()
		if m.VsLive && !m.isFetchingLiveCfg && len(m.cachedLiveConfig) == 0 {
			m.isFetchingLiveCfg = true
			return fetchLiveConfigCmd(m.SRLClient)
		}
		return nil
	}

	m.SplitDiff = false
	m.syncPaneHeights()
	m.syncCompareMarkers()
	m.DiffView.ResetScroll()
	m.updateActiveTabContent()
	return nil
}

func (m *AppModel) toggleFoldUnchanged() {
	m.FoldUnchanged = !m.FoldUnchanged
	m.DiffView.FoldUnchanged = m.FoldUnchanged
	m.DiffView.ResetScroll()
	m.updateActiveTabContent()
}

func (m *AppModel) openCommitPicker(side modals.PickerSide) {
	commits := m.filteredCommits
	if len(commits) == 0 {
		commits = m.allCommits
	}
	leftSHA := m.LeftCommitSHA
	rightSHA := m.RightCommitSHA
	if m.VsLive {
		rightSHA = modals.LiveRunningSentinel
	}
	if leftSHA == "" && rightSHA == "" {
		if sel := m.TimelineView.SelectedCommit(); sel != nil {
			if m.VsLive {
				leftSHA = sel.FullSHA
			} else {
				rightSHA = sel.FullSHA
				if m.BaseCommitSHA != "" && m.BaseCommitSHA != sel.FullSHA {
					leftSHA = m.BaseCommitSHA
				} else if parent := m.findParentCommit(sel.FullSHA); parent != nil {
					leftSHA = parent.FullSHA
				}
			}
		}
	}
	m.CommitPickerModal = modals.NewCommitPickerModal(side, commits, leftSHA, rightSHA, true, m.Width, m.Height)
	m.ActiveModal = ModalCommitPicker
}

// exitSplitToCommit leaves Side-by-Side Split Diff (clearing A/B compare endpoints and
// live compare state) and selects targetSHA in single-commit view.
func (m *AppModel) exitSplitToCommit(targetSHA string) {
	prevSHA := m.selectedCommitSHA()
	wasSplit := m.SplitDiff
	m.SplitDiff = false
	m.LeftCommitSHA = ""
	m.RightCommitSHA = ""
	m.leftCommit = nil
	m.rightCommit = nil
	m.VsLive = false
	m.DiffView.VsLive = false
	if targetSHA != "" {
		m.selectCommitBySHA(targetSHA)
	}
	if wasSplit || m.selectedCommitSHA() != prevSHA {
		m.setDetailFilterBypass(false)
	}
	m.syncPaneHeights()
	m.syncCompareMarkers()
	m.DiffView.ResetScroll()
	m.updateActiveTabContent()
}

// splitCompareWithSelected opens a Side-by-Side Split Diff comparing the currently selected
// commit against secondSHA, automatically placing the older commit on Left (A) and the newer
// commit on Right (B). On narrow terminals (< SplitMinOuterWidth), falls back to pinning
// secondSHA as Base Commit for single-pane comparison.
func (m *AppModel) splitCompareWithSelected(secondSHA string) tea.Cmd {
	if secondSHA == "" {
		return nil
	}
	firstSHA := m.selectedCommitSHA()
	if firstSHA == "" && !m.VsLive && m.RightCommitSHA != "" {
		firstSHA = m.RightCommitSHA
	}
	if firstSHA != "" && firstSHA == secondSHA {
		seq := m.setNotification(
			"Ctrl/Shift+Click a different commit to split-compare",
			lipgloss.NewStyle().Background(ColorWarningBg).Foreground(ColorOnWarning).Bold(true).Padding(0, 1),
		)
		return clearNotificationCmd(seq)
	}

	m.setDetailFilterBypass(false)
	m.VsLive = false
	m.DiffView.VsLive = false
	m.BaseCommitSHA = ""
	m.baseCommit = nil
	m.TimelineView.BaseCommitSHA = ""

	if m.Width >= SplitMinOuterWidth {
		m.LeftCommitSHA = secondSHA
		m.RightCommitSHA = firstSHA
		if m.RightCommitSHA == "" {
			if parent := m.findParentCommit(secondSHA); parent != nil {
				m.RightCommitSHA = parent.FullSHA
			} else {
				m.RightCommitSHA = secondSHA
			}
		}
		m.SplitDiff = true
		m.ActiveTab = TabDiff
		m.normalizeSplitEndpoints()
		m.setFocusedPane(PaneDetail)
		m.syncPaneHeights()
		m.syncCompareMarkers()
		m.DiffView.ResetScroll()
		m.updateActiveTabContent()

		shortA := m.LeftCommitSHA
		if len(shortA) > 8 {
			shortA = shortA[:8]
		}
		shortB := m.RightCommitSHA
		if len(shortB) > 8 {
			shortB = shortB[:8]
		}
		seq := m.setNotification(
			fmt.Sprintf("Split Compare A: %s ↔ B: %s", shortA, shortB),
			lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
		)
		return clearNotificationCmd(seq)
	}

	// Narrow terminal fallback (<100 cols): pin secondSHA as Base Commit and show Diff.
	m.BaseCommitSHA = secondSHA
	m.baseCommit = m.findCommitBySHA(secondSHA)
	m.TimelineView.BaseCommitSHA = secondSHA
	m.ActiveTab = TabDiff
	m.syncCompareMarkers()
	m.DiffView.ResetScroll()
	m.updateActiveTabContent()
	short := secondSHA
	if len(short) > 8 {
		short = short[:8]
	}
	seq := m.setNotification(
		fmt.Sprintf("Pinned Base (narrow): %s", short),
		lipgloss.NewStyle().Background(ColorPrimary).Foreground(ColorOnAccent).Bold(true).Padding(0, 1),
	)
	return clearNotificationCmd(seq)
}

func (m *AppModel) applyCommitPickerSelection(leftSHA, rightSHA string) tea.Cmd {
	m.setDetailFilterBypass(false)
	if leftSHA == "" || rightSHA == "" {
		return nil
	}

	// If either selected configuration is LiveRunningSentinel, place the historical commit
	// on Left (A) and Live Running Config on Right (B).
	if leftSHA == modals.LiveRunningSentinel || rightSHA == modals.LiveRunningSentinel {
		commitSHA := leftSHA
		if leftSHA == modals.LiveRunningSentinel {
			commitSHA = rightSHA
		}
		m.VsLive = true
		m.DiffView.VsLive = true
		m.LeftCommitSHA = commitSHA
		m.leftCommit = m.findCommitBySHA(commitSHA)
		m.RightCommitSHA = ""
		m.rightCommit = nil
		m.BaseCommitSHA = ""
		m.baseCommit = nil
		m.TimelineView.BaseCommitSHA = ""

		if !m.SplitDiff && m.Width >= SplitMinOuterWidth {
			m.SplitDiff = true
		}
		m.ActiveTab = TabDiff
		m.normalizeSplitEndpoints()
		m.setFocusedPane(PaneDetail)
		m.syncPaneHeights()
		m.syncCompareMarkers()
		m.DiffView.ResetScroll()
		m.updateActiveTabContent()
		if !m.isFetchingLiveCfg {
			m.isFetchingLiveCfg = true
			return fetchLiveConfigCmd(m.SRLClient)
		}
		return nil
	}

	// Two historical commits selected: older is guaranteed Left (A), newer is Right (B).
	m.VsLive = false
	m.DiffView.VsLive = false
	m.LeftCommitSHA = leftSHA
	m.RightCommitSHA = rightSHA
	m.leftCommit = m.findCommitBySHA(leftSHA)
	m.rightCommit = m.findCommitBySHA(rightSHA)
	m.BaseCommitSHA = ""
	m.baseCommit = nil
	m.TimelineView.BaseCommitSHA = ""

	if !m.SplitDiff && m.Width >= SplitMinOuterWidth {
		m.SplitDiff = true
	}
	m.ActiveTab = TabDiff
	m.normalizeSplitEndpoints()
	m.setFocusedPane(PaneDetail)
	m.syncPaneHeights()
	m.syncCompareMarkers()
	m.DiffView.ResetScroll()
	m.updateActiveTabContent()
	return nil
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
	m.DiffView.FoldUnchanged = m.FoldUnchanged

	if m.Width == 0 || m.Height == 0 {
		return
	}

	m.TimelineRibbon.SetWidth(m.Width)
	m.syncRibbonCollapsed()
	ribbonH := m.TimelineRibbon.OuterHeight()

	// Rows: 0=Header, 1=Filter, 2..2+ribbonH-1=Ribbon, 2+ribbonH..Height-2=Main Workspace, Height-1=Footer
	paneInnerHeight := m.Height - 5 - ribbonH
	if paneInnerHeight < 3 {
		paneInnerHeight = 3
	}

	splitActive := m.isSplitWorkspaceActive()
	m.DiffView.SplitMode = splitActive
	if m.hideTimelineList() && m.FocusedPane == PaneTimeline {
		m.setFocusedPane(PaneDetail)
	}

	leftOuterWidth, detailOuterWidth, _ := m.computeWorkspaceOuterWidths()
	timelineOuterWidth := leftOuterWidth
	if m.hideTimelineList() {
		// Keep TimelineView's internal width ready for quick expand.
		timelineOuterWidth, _ = computeSplitOuterWidths(m.Width)
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
	m.setDetailFilterBypass(false)
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

	if m.SplitDiff {
		if !m.VsLive && m.RightCommitSHA == "" {
			if sel := m.TimelineView.SelectedCommit(); sel != nil {
				m.RightCommitSHA = sel.FullSHA
			}
		}
		m.normalizeSplitEndpoints()
	}

	m.DiffView.ResetScroll()

	var notif string
	var toastStyle lipgloss.Style
	if m.VsLive {
		id := "revision"
		if m.SplitDiff && m.leftCommit != nil && strings.TrimSpace(m.leftCommit.CommitID) != "" {
			id = m.leftCommit.CommitID
		} else if sel := m.TimelineView.SelectedCommit(); sel != nil && strings.TrimSpace(sel.CommitID) != "" {
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
	m.syncCompareMarkers()
	m.updateActiveTabContent()
	return tea.Batch(cmds...)
}

func (m *AppModel) toggleBaseCommit() tea.Cmd {
	selected := m.TimelineView.SelectedCommit()
	if selected == nil {
		return nil
	}
	m.setDetailFilterBypass(false)

	if m.BaseCommitSHA == selected.FullSHA {
		m.BaseCommitSHA = ""
		m.baseCommit = nil
		m.TimelineView.BaseCommitSHA = ""
		prevY := m.TimelineView.Viewport.YOffset
		m.TimelineView.UpdateViewportContent()
		m.TimelineView.Viewport.SetYOffset(prevY)
		m.syncCompareMarkers()
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
	m.syncCompareMarkers()
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

	prevSHA := m.selectedCommitSHA()
	m.TimelineView.SelectIndex(foundIdx)
	if m.selectedCommitSHA() != prevSHA {
		m.setDetailFilterBypass(false)
	}
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

	case headWatchMsg:
		cmds = append(cmds, waitForHeadEvent(m.headEventChan))
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
		newHead := m.GitBackend.GetHeadSHA()
		headAdvanced := newHead != "" && newHead != m.lastHeadSHA
		if headAdvanced {
			m.lastHeadSHA = newHead
			m.cachedLiveConfig = m.GitBackend.GetLatestCommitConfig()
			m.liveConfigFetched = time.Now()
		}
		m.refreshTimeline()
		if headAdvanced {
			m.TimelineRibbon.EnsureTimeVisible(m.TimelineRibbon.FullMaxTime)
			m.syncWindowCommits()
			if m.selectCommitBySHA(newHead) {
				m.syncCompareMarkers()
				m.updateActiveTabContent()
			}
		}
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
				m.setDetailFilterBypass(false)
				m.focusWorkspaceDefault()
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			case "enter", "tab", "down":
				m.setDetailFilterBypass(false)
				m.focusWorkspaceDefault()
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			case "ctrl+c":
				if m.FilterBar.Value() != "" {
					m.FilterBar.Clear()
					m.setDetailFilterBypass(false)
					m.refreshTimeline()
					return m, m.backfillSummaryScopesCmd()
				}
				if m.headWatch != nil {
					m.headWatch.stop()
				}
				if m.Daemon != nil {
					m.Daemon.Stop()
				}
				return m, tea.Quit
			case "ctrl+u":
				m.FilterBar.Clear()
				m.setDetailFilterBypass(false)
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			case "ctrl+t":
				m.ThemeModal = modals.NewThemeModal(m.Width, m.Height)
				m.ActiveModal = ModalTheme
				return m, nil
			default:
				prevVal := m.FilterBar.Value()
				var cmd tea.Cmd
				m.FilterBar, cmd = m.FilterBar.Update(msg)
				if m.FilterBar.Value() != prevVal {
					m.setDetailFilterBypass(false)
				}
				m.refreshTimeline()
				return m, tea.Batch(cmd, m.backfillSummaryScopesCmd())
			}
		}

		// 2. Filter Activation Shortcut & Decoupled Detail Filter Toggle (\)
		if msg.String() == "/" {
			m.setFocusedPane(PaneFilter)
			return m, m.FilterBar.Focus()
		}
		if msg.String() == "\\" && !m.FilterBar.TextInput.Focused() {
			return m, m.toggleDetailFilterBypass()
		}

		// 3. Esc hierarchy & 'x' filter clear
		switch msg.String() {
		case "esc":
			if m.Zoomed {
				m.Zoomed = false
				m.syncPaneHeights()
				return m, nil
			}
			if m.FocusedPane == PaneRibbon {
				if !m.hideTimelineList() {
					m.setFocusedPane(PaneTimeline)
				} else {
					m.setFocusedPane(PaneDetail)
				}
				return m, nil
			}
			if m.FocusedPane == PaneDetail {
				if !m.hideTimelineList() {
					m.setFocusedPane(PaneTimeline)
				} else {
					m.setFocusedPane(PaneRibbon)
				}
				return m, nil
			}
			if m.FilterBar.Value() != "" {
				m.FilterBar.Clear()
				m.setDetailFilterBypass(false)
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			}
			return m, nil

		case "x":
			if m.FilterBar.Value() != "" {
				m.FilterBar.Clear()
				m.setDetailFilterBypass(false)
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			}
		}

		// 4. Pane Navigation (Tab, Shift+Tab, Left, Right, h, l)
		switch msg.String() {
		case "tab":
			if !m.Zoomed {
				if m.hideTimelineList() {
					if m.FocusedPane == PaneDetail {
						m.setFocusedPane(PaneRibbon)
					} else {
						m.setFocusedPane(PaneDetail)
					}
				} else {
					switch m.FocusedPane {
					case PaneTimeline:
						m.setFocusedPane(PaneDetail)
					case PaneDetail:
						m.setFocusedPane(PaneRibbon)
					default:
						m.setFocusedPane(PaneTimeline)
					}
				}
			}
			return m, nil

		case "shift+tab":
			if !m.Zoomed {
				if m.hideTimelineList() {
					if m.FocusedPane == PaneDetail {
						m.setFocusedPane(PaneRibbon)
					} else {
						m.setFocusedPane(PaneDetail)
					}
				} else {
					switch m.FocusedPane {
					case PaneTimeline:
						m.setFocusedPane(PaneRibbon)
					case PaneRibbon:
						m.setFocusedPane(PaneDetail)
					default:
						m.setFocusedPane(PaneTimeline)
					}
				}
			}
			return m, nil

		case "right", "l":
			if m.FocusedPane == PaneRibbon {
				if sha := m.TimelineRibbon.SelectNextInWindow(); sha != "" {
					prevSHA := m.selectedCommitSHA()
					prevRight := m.RightCommitSHA
					m.selectCommitBySHA(sha)
					if m.SplitDiff && !m.VsLive {
						m.RightCommitSHA = sha
						m.normalizeSplitEndpoints()
					}
					if m.selectedCommitSHA() != prevSHA || m.RightCommitSHA != prevRight {
						m.setDetailFilterBypass(false)
					}
					m.syncCompareMarkers()
					m.updateActiveTabContent()
				}
				return m, nil
			}
			m.setFocusedPane(PaneDetail)
			return m, nil

		case "left", "h":
			if m.FocusedPane == PaneRibbon {
				if sha := m.TimelineRibbon.SelectPrevInWindow(); sha != "" {
					prevSHA := m.selectedCommitSHA()
					prevRight := m.RightCommitSHA
					m.selectCommitBySHA(sha)
					if m.SplitDiff && !m.VsLive {
						m.RightCommitSHA = sha
						m.normalizeSplitEndpoints()
					}
					if m.selectedCommitSHA() != prevSHA || m.RightCommitSHA != prevRight {
						m.setDetailFilterBypass(false)
					}
					m.syncCompareMarkers()
					m.updateActiveTabContent()
				}
				return m, nil
			}
			if !m.hideTimelineList() {
				m.setFocusedPane(PaneTimeline)
			}
			return m, nil
		}

		// 5. Top Tab Switchers: d (Diff), c (Config), b (Blame), t (cycle)
		switch msg.String() {
		case "d":
			m.ActiveTab = TabDiff
			m.syncPaneHeights()
			m.syncCompareMarkers()
			m.updateActiveTabContent()
			return m, nil
		case "c":
			m.ActiveTab = TabConfig
			m.syncPaneHeights()
			m.syncCompareMarkers()
			m.updateActiveTabContent()
			return m, nil
		case "b":
			m.ActiveTab = TabBlame
			m.syncPaneHeights()
			m.syncCompareMarkers()
			m.updateActiveTabContent()
			return m, nil
		case "t":
			m.ActiveTab = (m.ActiveTab + 1) % 3
			m.syncPaneHeights()
			m.syncCompareMarkers()
			m.updateActiveTabContent()
			return m, nil
		}

		// 5b. Timeline Window & Ribbon Controls + Side-by-Side Split Diff Controls
		switch msg.String() {
		case "[":
			m.TimelineRibbon.NudgeStart(0.05)
			m.syncWindowCommits()
			return m, nil
		case "{":
			m.TimelineRibbon.NudgeStart(-0.05)
			m.syncWindowCommits()
			return m, nil
		case "]":
			m.TimelineRibbon.NudgeEnd(-0.05)
			m.syncWindowCommits()
			return m, nil
		case "}":
			m.TimelineRibbon.NudgeEnd(0.05)
			m.syncWindowCommits()
			return m, nil
		case "<":
			m.TimelineRibbon.PanWindow(-0.10)
			m.syncWindowCommits()
			return m, nil
		case ">":
			m.TimelineRibbon.PanWindow(0.10)
			m.syncWindowCommits()
			return m, nil
		case "+", "=":
			m.TimelineRibbon.ZoomAround(m.selectedCommitTime(), 0.70)
			m.syncWindowCommits()
			return m, nil
		case "-":
			m.TimelineRibbon.ZoomAround(m.selectedCommitTime(), 1.40)
			m.syncWindowCommits()
			return m, nil
		case "0":
			m.TimelineRibbon.ResetWindow()
			m.syncWindowCommits()
			return m, nil
		case "F":
			m.TimelineRibbon.FitToMatches()
			m.syncWindowCommits()
			return m, nil
		case "f":
			if m.FocusedPane != PaneDetail {
				m.TimelineRibbon.FitToMatches()
				m.syncWindowCommits()
				return m, nil
			}
		case "w":
			if !(m.ActiveTab == TabBlame && m.FocusedPane == PaneDetail) {
				m.userCollapsedRibbon = !m.userCollapsedRibbon
				m.syncPaneHeights()
				return m, nil
			}
		case "S":
			return m, m.toggleSplitDiff()
		case "E":
			if m.ActiveTab == TabDiff && m.DiffView.IsSplitActive() {
				m.toggleFoldUnchanged()
			}
			return m, nil
		case "A":
			m.openCommitPicker(modals.PickerSideLeft)
			return m, nil
		case "B":
			m.openCommitPicker(modals.PickerSideRight)
			return m, nil
		}

		// 6. Global action keys
		switch msg.String() {
		case "q", "ctrl+c":
			if m.headWatch != nil {
				m.headWatch.stop()
			}
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
			selected := m.targetCommitForRestore()
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
			selected := m.targetCommitForRestore()
			if selected != nil {
				curCfg := m.GitBackend.GetConfigAtCommit(selected.FullSHA)
				var baseCfg map[string]interface{}
				if m.SplitDiff && m.leftCommit != nil && selected.FullSHA == m.leftCommit.FullSHA {
					// In Split View comparing older A against HEAD/Running, show paths that differ
					// from current running config so cherry-pick offers meaningful rollback targets.
					baseCfg = m.cachedLiveConfig
					if len(baseCfg) == 0 {
						baseCfg = m.GitBackend.GetLatestCommitConfig()
					}
				} else {
					baseCfg = m.GitBackend.GetConfigAtCommit(fmt.Sprintf("%s~1", selected.FullSHA))
				}
				diffRes := differ.SemanticDiff(baseCfg, curCfg, "")
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
				if m.DiffView.IsSplitActive() {
					m.updateActiveTabContent()
				} else {
					m.DiffView.UpdateContent()
				}
				return m, nil
			case "2":
				m.DiffView.DiffMode = "path"
				m.DiffView.ResetScroll()
				if m.DiffView.IsSplitActive() {
					m.updateActiveTabContent()
				} else {
					m.DiffView.UpdateContent()
				}
				return m, nil
			case "3":
				m.DiffView.DiffMode = "cli"
				m.DiffView.ResetScroll()
				if m.DiffView.IsSplitActive() {
					m.updateActiveTabContent()
				} else {
					m.DiffView.UpdateContent()
				}
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
		if m.FocusedPane == PaneRibbon {
			switch msg.String() {
			case "enter", "down", "j":
				if !m.hideTimelineList() {
					m.setFocusedPane(PaneTimeline)
				} else {
					m.setFocusedPane(PaneDetail)
				}
			}
			return m, nil
		}

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
		prevSHA := m.selectedCommitSHA()
		switch msg.String() {
		case "enter":
			m.setFocusedPane(PaneDetail)
			return m, nil
		case "j", "down":
			m.TimelineView.Next()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
			m.updateActiveTabContent()
		case "k", "up":
			m.TimelineView.Prev()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
			m.updateActiveTabContent()
		case "home":
			m.TimelineView.GotoTop()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
			m.updateActiveTabContent()
		case "end", "G":
			m.TimelineView.GotoBottom()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
			m.updateActiveTabContent()
		case "pgup", "ctrl+u":
			m.TimelineView.PageUp()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
			m.updateActiveTabContent()
		case "pgdown", " ", "space", "ctrl+d":
			m.TimelineView.PageDown()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
			m.updateActiveTabContent()
		}

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	}

	return m, tea.Batch(cmds...)
}

func isMouseRelease(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease
}

func isMouseDragMotion(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion
}

func isMouseLeftPress(msg tea.MouseMsg) bool {
	if isMouseRelease(msg) || isMouseDragMotion(msg) {
		return false
	}
	return msg.Button == tea.MouseButtonLeft || msg.Type == tea.MouseLeft
}

func isMouseRightPress(msg tea.MouseMsg) bool {
	if isMouseRelease(msg) || isMouseDragMotion(msg) {
		return false
	}
	return msg.Button == tea.MouseButtonRight || msg.Type == tea.MouseRight
}

func isSplitCompareClick(msg tea.MouseMsg) bool {
	if isMouseRightPress(msg) {
		return true
	}
	return isMouseLeftPress(msg) && (msg.Shift || msg.Ctrl || msg.Alt)
}

func (m *AppModel) scrollbarTargetMetrics(target string, mainStartY int) (vp *viewport.Model, totalLines, trackStartY, trackH, innerW int) {
	switch target {
	case "timeline":
		vp = &m.TimelineView.Viewport
		totalLines = len(m.TimelineView.renderedLines)
		if totalLines == 0 {
			totalLines = vp.TotalLineCount()
		}
		trackStartY = mainStartY + 1
		trackH = vp.Height
		innerW = m.TimelineView.Width
	case "detail":
		var hdrRows int
		switch m.ActiveTab {
		case TabDiff:
			vp = &m.DiffView.Viewport
			totalLines = len(m.DiffView.renderedLines)
			hdrRows = m.DiffView.HeaderRows()
			innerW = m.DiffView.Width
		case TabConfig:
			vp = &m.ConfigView.Viewport
			totalLines = len(m.ConfigView.renderedLines)
			hdrRows = m.ConfigView.HeaderRows()
			innerW = m.ConfigView.Width
		case TabBlame:
			vp = &m.BlameView.Viewport
			totalLines = len(m.BlameView.renderedLines)
			hdrRows = m.BlameView.HeaderRows()
			innerW = m.BlameView.Width
		default:
			return nil, 0, 0, 0, 0
		}
		if totalLines == 0 {
			totalLines = vp.TotalLineCount()
		}
		trackStartY = mainStartY + 1 + hdrRows
		trackH = vp.Height
	}
	return vp, totalLines, trackStartY, trackH, innerW
}

// handleScrollbarPress handles clicking on either the thumb (starting an offset-preserving drag)
// or the track (jumping immediately to that position and allowing smooth drag scrubbing).
func (m *AppModel) handleScrollbarPress(target string, clickY, mainStartY int) bool {
	vp, totalLines, trackStartY, trackH, innerW := m.scrollbarTargetMetrics(target, mainStartY)
	if vp == nil || innerW <= 4 || trackH <= 0 || totalLines <= trackH {
		return false
	}
	relY := clickY - trackStartY
	if relY < 0 || relY >= trackH {
		return false
	}

	thumbStart, thumbH, maxOffset, trackRange := ScrollbarMetrics(trackH, totalLines, vp.YOffset)
	if maxOffset <= 0 {
		return false
	}

	if relY >= thumbStart && relY < thumbStart+thumbH {
		// Grabbed directly on the thumb: preserve grab offset so it never jumps on initial click.
		m.scrollDragTarget = target
		m.scrollDragGrabOffset = relY - thumbStart
		return true
	}

	// Clicked on the track outside the thumb: jump immediately and allow continued scrubbing.
	var targetThumb int
	switch {
	case relY == 0:
		targetThumb = 0
	case relY == trackH-1:
		targetThumb = trackRange
	default:
		targetThumb = relY - thumbH/2
		if targetThumb < 0 {
			targetThumb = 0
		}
		if targetThumb > trackRange {
			targetThumb = trackRange
		}
	}
	vp.SetYOffset(ScrollbarYOffsetForThumb(targetThumb, trackH, totalLines))
	m.scrollDragTarget = target
	grab := relY - targetThumb
	if grab < 0 {
		grab = 0
	}
	if grab >= thumbH {
		grab = thumbH - 1
	}
	m.scrollDragGrabOffset = grab
	return true
}

func (m *AppModel) handleScrollbarDrag(mouseY, mainStartY int) bool {
	if m.scrollDragTarget == "" {
		return false
	}
	vp, totalLines, trackStartY, trackH, innerW := m.scrollbarTargetMetrics(m.scrollDragTarget, mainStartY)
	if vp == nil || innerW <= 4 || trackH <= 0 || totalLines <= trackH {
		return false
	}
	_, _, maxOffset, trackRange := ScrollbarMetrics(trackH, totalLines, vp.YOffset)
	if maxOffset <= 0 {
		return false
	}
	newThumb := (mouseY - trackStartY) - m.scrollDragGrabOffset
	if newThumb < 0 {
		newThumb = 0
	}
	if newThumb > trackRange {
		newThumb = trackRange
	}
	vp.SetYOffset(ScrollbarYOffsetForThumb(newThumb, trackH, totalLines))
	return true
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

	case ModalCommitPicker:
		var leftSHA, rightSHA string
		var confirmed, close bool
		m.CommitPickerModal, leftSHA, rightSHA, confirmed, close = m.CommitPickerModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if confirmed && leftSHA != "" && rightSHA != "" {
				return m, m.applyCommitPickerSelection(leftSHA, rightSHA)
			}
		}
		return m, nil

	case ModalNone:
		// proceed to main screen hit-testing
	default:
		return m, nil
	}

	// Complete or continue active scrollbar drag anywhere on screen (even if cursor drifts horizontally).
	if m.scrollDragTarget != "" {
		switch {
		case isMouseRelease(msg):
			m.scrollDragTarget = ""
			m.scrollDragGrabOffset = 0
			return m, nil
		case isMouseDragMotion(msg):
			m.handleScrollbarDrag(msg.Y, m.mainStartY())
			return m, nil
		}
	}

	// Complete or continue active ribbon drag (overview scrubber OR direct timeline brush) anywhere on screen.
	if m.TimelineRibbon.DragMode != "" {
		switch {
		case isMouseRelease(msg):
			if m.TimelineRibbon.EndDrag() {
				m.syncWindowCommits()
			}
			return m, nil
		case isMouseDragMotion(msg):
			if m.TimelineRibbon.HandleDrag(msg.X) {
				m.syncWindowCommits()
			}
			return m, nil
		}
	}

	if m.Width <= 0 || m.Height <= 0 || msg.Y < 0 || msg.X < 0 || msg.X >= m.Width {
		return m, nil
	}

	// Row 0: Header Bar (only Theme pill is clickable; never changes pane focus)
	if msg.Y == 0 {
		if isMouseLeftPress(msg) {
			startX, endX := m.HeaderBar.ThemeHitRange()
			if startX >= 0 && msg.X >= startX && msg.X < endX {
				m.ThemeModal = modals.NewThemeModal(m.Width, m.Height)
				m.ActiveModal = ModalTheme
			}
		}
		return m, nil
	}

	// Row 1: Filter Bar (explicit Scope pill, Clear hit-range, quick chips, else focus Filter)
	if msg.Y == 1 {
		if isMouseLeftPress(msg) {
			m.FilterBar.DetailBypass = m.DetailFilterBypass
			if scopeStart, scopeEnd := m.FilterBar.ScopeHitRange(); scopeStart >= 0 && msg.X >= scopeStart && msg.X < scopeEnd {
				return m, m.toggleDetailFilterBypass()
			}
			if clearStart, clearEnd := m.FilterBar.ClearHitRange(); clearStart >= 0 && msg.X >= clearStart && msg.X < clearEnd {
				m.FilterBar.Clear()
				m.setDetailFilterBypass(false)
				m.focusWorkspaceDefault()
				m.refreshTimeline()
				return m, m.backfillSummaryScopesCmd()
			}
			for _, chip := range m.FilterBar.QuickChipHitRanges() {
				if msg.X >= chip.StartX && msg.X < chip.EndX {
					m.FilterBar.TextInput.SetValue(chip.Label)
					m.setDetailFilterBypass(false)
					m.focusWorkspaceDefault()
					m.refreshTimeline()
					return m, m.backfillSummaryScopesCmd()
				}
			}
			m.setFocusedPane(PaneFilter)
			return m, m.FilterBar.Focus()
		}
		return m, nil
	}

	// Rows 2 .. mainStartY-1: Visual Timeline Ribbon
	mainStartY := m.mainStartY()
	if msg.Y >= 2 && msg.Y < mainStartY {
		relY := msg.Y - 2
		switch {
		case msg.Type == tea.MouseWheelUp || msg.Button == tea.MouseButtonWheelUp:
			m.TimelineRibbon.ZoomAround(m.ribbonTimeAtX(msg.X), 0.70)
			m.syncWindowCommits()
			return m, nil
		case msg.Type == tea.MouseWheelDown || msg.Button == tea.MouseButtonWheelDown:
			m.TimelineRibbon.ZoomAround(m.ribbonTimeAtX(msg.X), 1.40)
			m.syncWindowCommits()
			return m, nil
		case isMouseLeftPress(msg) || isMouseRightPress(msg):
			if isMouseRightPress(msg) && relY != 1 && relY != 2 {
				return m, nil
			}
			action, payload := m.TimelineRibbon.HandleClick(msg.X, relY, isSplitCompareClick(msg))
			switch {
			case strings.HasPrefix(action, "preset:"):
				m.TimelineRibbon.SetPreset(strings.TrimPrefix(action, "preset:"))
				m.syncWindowCommits()
				return m, nil
			case action == "toggle_collapse":
				m.userCollapsedRibbon = !m.userCollapsedRibbon
				m.syncPaneHeights()
				return m, nil
			case action == "select_commit":
				if m.SplitDiff {
					m.exitSplitToCommit(payload)
					return m, nil
				}
				prevSHA := m.selectedCommitSHA()
				m.selectCommitBySHA(payload)
				if m.selectedCommitSHA() != prevSHA {
					m.setDetailFilterBypass(false)
				}
				m.syncCompareMarkers()
				m.updateActiveTabContent()
				return m, nil
			case action == "pin_left":
				return m, m.splitCompareWithSelected(payload)
			case strings.HasPrefix(action, "drag_start:"):
				m.setFocusedPane(PaneRibbon)
				return m, nil
			case action == "window_changed":
				m.setFocusedPane(PaneRibbon)
				m.syncWindowCommits()
				return m, nil
			default:
				m.setFocusedPane(PaneRibbon)
				return m, nil
			}
		}
		return m, nil
	}

	// Row m.Height-1 (and beyond): Footer Bar — never leaks into panes
	if msg.Y >= m.Height-1 {
		return m, nil
	}

	// Main Panes: Y in [mainStartY, m.Height-2]
	hideTimeline := m.hideTimelineList()
	leftOuterW, detailOuterW, sepW := m.computeWorkspaceOuterWidths()
	timelineOuterW := leftOuterW
	detailStartX := leftOuterW + sepW
	if hideTimeline {
		timelineOuterW = 0
	}

	// Scroll wheel: strictly routed by cursor X (never hijacks Timeline wheel when Detail is focused)
	switch msg.Type {
	case tea.MouseWheelUp:
		if !hideTimeline && msg.X < detailStartX {
			prevSHA := m.selectedCommitSHA()
			m.TimelineView.Prev()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
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
		if !hideTimeline && msg.X < detailStartX {
			prevSHA := m.selectedCommitSHA()
			m.TimelineView.Next()
			if m.selectedCommitSHA() != prevSHA {
				m.setDetailFilterBypass(false)
			}
			m.syncCompareMarkers()
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

	if !isMouseLeftPress(msg) && !isMouseRightPress(msg) {
		return m, nil
	}

	// Clicking anywhere on the collapsed left sidebar rail (`▶`) expands the Left Panel!
	if hideTimeline && leftOuterW > 0 && msg.X < leftOuterW {
		if isMouseLeftPress(msg) {
			m.Zoomed = false
			m.syncPaneHeights()
		}
		return m, nil
	}

	// Left click (or Ctrl/Alt/Shift/Right-click for split-compare) on Timeline Pane
	if !hideTimeline && msg.X < timelineOuterW {
		// Top border `[◀]` collapse button minimizes the Left Panel into the `▶` rail.
		if msg.Y == mainStartY && isMouseLeftPress(msg) {
			if cStart, cEnd := m.TimelineView.CollapseHitRange(); cStart >= 0 && msg.X >= cStart && msg.X < cEnd {
				m.Zoomed = true
				m.syncPaneHeights()
				return m, nil
			}
			m.setFocusedPane(PaneTimeline)
			return m, nil
		}
		m.setFocusedPane(PaneTimeline)
		// Scrollbar click/drag on the rightmost inner column (or border edge) of TimelineView.
		if isMouseLeftPress(msg) && msg.X >= timelineOuterW-2 {
			if m.handleScrollbarPress("timeline", msg.Y, mainStartY) {
				return m, nil
			}
		}
		contentStartY := mainStartY + 1
		if msg.Y >= contentStartY && msg.Y <= m.Height-3 {
			lpc := m.TimelineView.LinesPerCommit()
			if lpc < 1 {
				lpc = 2
			}
			clickedRow := (msg.Y - contentStartY + m.TimelineView.Viewport.YOffset) / lpc
			if clickedRow >= 0 && clickedRow < len(m.TimelineView.Commits) {
				clickedSHA := m.TimelineView.Commits[clickedRow].FullSHA
				if isSplitCompareClick(msg) {
					return m, m.splitCompareWithSelected(clickedSHA)
				}
				if m.SplitDiff {
					m.exitSplitToCommit(clickedSHA)
					return m, nil
				}
				prevSHA := m.selectedCommitSHA()
				m.TimelineView.SelectIndex(clickedRow)
				if m.selectedCommitSHA() != prevSHA {
					m.setDetailFilterBypass(false)
				}
				m.syncCompareMarkers()
				m.updateActiveTabContent()
			}
		}
		return m, nil
	}

	if !isMouseLeftPress(msg) {
		return m, nil
	}

	// Left click on Detail Pane
	if msg.X >= detailStartX {
		// Scrollbar click/drag on the rightmost inner column (or right border edge) of Diff/Config/Blame.
		if msg.X >= detailStartX+detailOuterW-2 && msg.Y > mainStartY && msg.Y <= m.Height-3 {
			m.setFocusedPane(PaneDetail)
			if m.handleScrollbarPress("detail", msg.Y, mainStartY) {
				return m, nil
			}
		}

		// Detail Top Border (Y == mainStartY): embedded tabs hit-testing
		if msg.Y == mainStartY {
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
					m.syncPaneHeights()
					m.syncCompareMarkers()
					m.updateActiveTabContent()
					return m, nil
				}
			}
			m.setFocusedPane(PaneDetail)
			return m, nil
		}

		// Detail Toolbar (Y == mainStartY + 1): active view's ControlHitRanges
		if msg.Y == mainStartY+1 {
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
							if m.DiffView.IsSplitActive() {
								m.updateActiveTabContent()
							} else {
								m.DiffView.UpdateContent()
							}
							return m, nil
						case "path":
							m.DiffView.DiffMode = "path"
							m.DiffView.ResetScroll()
							if m.DiffView.IsSplitActive() {
								m.updateActiveTabContent()
							} else {
								m.DiffView.UpdateContent()
							}
							return m, nil
						case "cli":
							m.DiffView.DiffMode = "cli"
							m.DiffView.ResetScroll()
							if m.DiffView.IsSplitActive() {
								m.updateActiveTabContent()
							} else {
								m.DiffView.UpdateContent()
							}
							return m, nil
						case "split":
							return m, m.toggleSplitDiff()
						case "fold":
							if m.DiffView.IsSplitActive() {
								m.toggleFoldUnchanged()
							}
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

		// Dual A/B Split Header Strip (Y == mainStartY + 2 when split diff is active)
		if msg.Y == mainStartY+2 && m.ActiveTab == TabDiff && m.DiffView.IsSplitActive() {
			m.setFocusedPane(PaneDetail)
			innerX := msg.X - detailStartX - 1
			if innerX >= 0 {
				for _, chip := range m.DiffView.SplitEndpointHitRanges() {
					if innerX >= chip.StartX && innerX < chip.EndX {
						switch chip.Label {
						case "pick_left":
							m.openCommitPicker(modals.PickerSideLeft)
							return m, nil
						case "pick_right":
							m.openCommitPicker(modals.PickerSideRight)
							return m, nil
						}
					}
				}
			}
			return m, nil
		}

		// Detail Filter Scope Banner (active view's FilterBannerRow & FilterBannerHitRange)
		innerRow := msg.Y - (mainStartY + 1)
		innerX := msg.X - detailStartX - 1
		if innerRow > 0 && innerX >= 0 {
			bannerRow := -1
			var bannerStartX, bannerEndX int
			switch m.ActiveTab {
			case TabDiff:
				bannerRow = m.DiffView.FilterBannerRow()
				if bannerRow == innerRow {
					bannerStartX, bannerEndX = m.DiffView.FilterBannerHitRange()
				}
			case TabConfig:
				bannerRow = m.ConfigView.FilterBannerRow()
				if bannerRow == innerRow {
					bannerStartX, bannerEndX = m.ConfigView.FilterBannerHitRange()
				}
			case TabBlame:
				bannerRow = m.BlameView.FilterBannerRow()
				if bannerRow == innerRow {
					bannerStartX, bannerEndX = m.BlameView.FilterBannerHitRange()
				}
			}
			if bannerRow >= 0 && innerRow == bannerRow {
				m.setFocusedPane(PaneDetail)
				if bannerStartX >= 0 && innerX >= bannerStartX && innerX < bannerEndX {
					return m, m.toggleDetailFilterBypass()
				}
				return m, nil
			}
		}

		// Detail Content Area (Y >= mainStartY+2 && Y <= m.Height-2)
		m.setFocusedPane(PaneDetail)
		if m.ActiveTab == TabBlame && m.BlameView.ViewMode == "attribution" && msg.Y <= m.Height-3 {
			vpRow := (msg.Y - (mainStartY + 1)) - m.BlameView.HeaderRows()
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
			m.ActiveModal == ModalCommitPicker ||
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

	case ModalCommitPicker:
		var leftSHA, rightSHA string
		var confirmed, close bool
		m.CommitPickerModal, leftSHA, rightSHA, confirmed, close = m.CommitPickerModal.Update(msg)
		if close {
			m.ActiveModal = ModalNone
			if confirmed && leftSHA != "" && rightSHA != "" {
				return m, m.applyCommitPickerSelection(leftSHA, rightSHA)
			}
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

// targetCommitForRestore returns the commit targeted by Restore ('r') or Cherry-Pick ('p').
// In Split Diff when comparing an older revision A against HEAD or Live Running Config, it
// targets the historical A revision rather than no-op'ing on HEAD.
func (m AppModel) targetCommitForRestore() *models.TimelineCommit {
	selected := m.TimelineView.SelectedCommit()
	if m.SplitDiff && m.leftCommit != nil {
		if m.VsLive ||
			(m.RightCommitSHA != "" && m.RightCommitSHA == m.lastHeadSHA) ||
			(selected != nil && selected.FullSHA == m.lastHeadSHA) {
			return m.leftCommit
		}
	}
	return selected
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
		severity := "success"
		if strings.HasPrefix(msg, "No effective change") || strings.Contains(msg, "record failed") {
			severity = "warning"
		}
		return NotificationMsg{
			Message:  msg,
			Severity: severity,
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
		severity := "success"
		if strings.HasPrefix(msg, "No effective change") || strings.Contains(msg, "failed") {
			severity = "warning"
		}
		return NotificationMsg{
			Message:  msg,
			Severity: severity,
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
	prevSplitActive := m.DiffView.IsSplitActive()

	m.HeaderBar.Width = width
	m.FilterBar.SetWidth(width)
	m.TimelineRibbon.SetWidth(width)
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
	m.CommitPickerModal.Width = width
	m.CommitPickerModal.Height = height

	// Only resize components and re-wrap viewports — never re-query Git timeline on resize!
	m.syncPaneHeights()
	m.syncCompareMarkers()
	if m.ActiveTab == TabDiff && prevSplitActive != m.DiffView.IsSplitActive() {
		m.updateActiveTabContent()
	}
}

func (m *AppModel) refreshTimeline() {
	filterQuery := m.FilterBar.Value()
	allCommits := m.GitBackend.GetTimeline(200, "")
	m.allCommits = allCommits

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
	m.filteredCommits = filteredCommits

	if m.BaseCommitSHA != "" && m.baseCommit == nil {
		if c := m.findCommitBySHA(m.BaseCommitSHA); c != nil {
			m.baseCommit = c
		}
	}
	if m.LeftCommitSHA != "" && m.leftCommit == nil {
		if c := m.findCommitBySHA(m.LeftCommitSHA); c != nil {
			m.leftCommit = c
		}
	}
	if m.RightCommitSHA != "" && m.rightCommit == nil {
		if c := m.findCommitBySHA(m.RightCommitSHA); c != nil {
			m.rightCommit = c
		}
	}

	m.TimelineRibbon.SetCommits(filteredCommits, len(allCommits), filterQuery)
	m.FilterBar.SetCounts(len(filteredCommits), len(allCommits))
	m.syncWindowCommits()
}

func (m *AppModel) syncWindowCommits() {
	prevSHA := m.selectedCommitSHA()
	filterQuery := m.FilterBar.Value()
	windowNarrowed := m.TimelineRibbon.ActivePreset != "all"

	m.TimelineView.FilterQuery = filterQuery
	m.TimelineView.HeadSHA = m.lastHeadSHA
	m.TimelineView.BaseCommitSHA = m.BaseCommitSHA
	m.TimelineView.WindowNarrowed = windowNarrowed
	m.TimelineView.TotalMatching = len(m.filteredCommits)
	m.TimelineView.SetCommits(m.TimelineRibbon.WindowCommits)

	if m.selectedCommitSHA() != prevSHA {
		m.setDetailFilterBypass(false)
	}

	m.syncCompareMarkers()
	m.updateActiveTabContent()
}

// updateActiveTabContent updates only the active detail tab, clearing all detail views
// when no commit matches and resetting scroll only when commit, tab, or compare target changes.
func (m *AppModel) updateActiveTabContent() {
	filterQuery := m.FilterBar.Value()
	cleanFilter := strings.TrimSpace(filterQuery)
	if cleanFilter == "" && m.DetailFilterBypass {
		m.setDetailFilterBypass(false)
	}
	m.FilterBar.DetailBypass = m.DetailFilterBypass
	m.TimelineView.FilterQuery = filterQuery
	m.TimelineView.HeadSHA = m.lastHeadSHA
	m.TimelineView.BaseCommitSHA = m.BaseCommitSHA
	m.BlameView.FilterQuery = filterQuery
	m.BlameView.DetailFilterBypass = m.DetailFilterBypass

	selected := m.TimelineView.SelectedCommit()
	if selected == nil && !(m.ActiveTab == TabDiff && m.DiffView.IsSplitActive() && (m.LeftCommitSHA != "" || m.RightCommitSHA != "")) {
		msg := "No commits available."
		if m.TimelineView.WindowNarrowed && m.TimelineView.TotalMatching > 0 {
			msg = "No commits in current time window. Press 0 (All) or f (Fit) to expand."
		} else if cleanFilter != "" {
			msg = fmt.Sprintf("No commits match filter '%s'. Press Esc to clear.", cleanFilter)
		}
		m.DiffView.Clear(msg)
		m.ConfigView.Clear(msg)
		m.BlameView.Clear(msg)
		m.lastDetailCommitSHA = ""
		m.lastCompareKey = ""
		return
	}

	// Side-by-side Split Diff mode
	if m.ActiveTab == TabDiff && m.DiffView.IsSplitActive() {
		m.normalizeSplitEndpoints()
		m.syncCompareMarkers()

		liveStamp := int64(0)
		if m.VsLive {
			liveStamp = m.liveConfigFetched.UnixNano()
		}
		splitKey := fmt.Sprintf("split:%s|%s|live=%v:%d|mode=%s|fold=%v|bypass=%v|%s",
			m.LeftCommitSHA, m.RightCommitSHA, m.VsLive, liveStamp, m.DiffView.DiffMode, m.FoldUnchanged, m.DetailFilterBypass, filterQuery)
		needsReset := m.ActiveTab != m.lastDetailTab || splitKey != m.lastCompareKey
		if !needsReset && m.DiffView.Viewport.TotalLineCount() > 0 {
			return
		}
		m.lastDetailTab = m.ActiveTab
		m.lastCompareKey = splitKey
		if selected != nil {
			m.lastDetailCommitSHA = selected.FullSHA
		}

		var oldCfg, newCfg map[string]interface{}
		if m.LeftCommitSHA != "" {
			oldCfg = m.GitBackend.GetConfigAtCommit(m.LeftCommitSHA)
		}
		if m.VsLive {
			if len(m.cachedLiveConfig) > 0 {
				newCfg = m.cachedLiveConfig
			} else {
				newCfg = m.GitBackend.GetLatestCommitConfig()
			}
		} else if m.RightCommitSHA != "" {
			newCfg = m.GitBackend.GetConfigAtCommit(m.RightCommitSHA)
		}

		m.DiffView.SplitMode = true
		m.DiffView.FoldUnchanged = m.FoldUnchanged
		m.DiffView.VsLive = m.VsLive
		m.DiffView.FilterQuery = filterQuery
		m.DiffView.DetailFilterBypass = m.DetailFilterBypass
		m.DiffView.LeftCommit = m.leftCommit
		m.DiffView.RightCommit = m.rightCommit
		if selected != nil {
			selCopy := *selected
			m.DiffView.Commit = &selCopy
			m.DiffView.ParentSHA = m.parentSHAByFullSHA[selected.FullSHA]
		}

		effectiveFilter := filterQuery
		if m.DetailFilterBypass {
			effectiveFilter = ""
		}
		if cleanFilter == "" {
			m.DiffView.ShownChangeCount = 0
			m.DiffView.TotalUnfilteredChanges = 0
		} else {
			unfilteredKey := fmt.Sprintf("%s|%s|live=%v:%d", m.LeftCommitSHA, m.RightCommitSHA, m.VsLive, liveStamp)
			allChanges := m.getUnfilteredDiffChanges(unfilteredKey, oldCfg, newCfg)
			totalUnfiltered := countSemanticLeafChanges(allChanges, "")
			m.DiffView.TotalUnfilteredChanges = totalUnfiltered
			if m.DetailFilterBypass {
				m.DiffView.ShownChangeCount = totalUnfiltered
			} else {
				m.DiffView.ShownChangeCount = countSemanticLeafChanges(allChanges, cleanFilter)
			}
		}

		res := differ.ComputeSideBySideDiff(oldCfg, newCfg, m.DiffView.DiffMode, effectiveFilter, m.FoldUnchanged, 3)
		m.DiffView.SetSideBySideDiff(res)
		if needsReset {
			m.DiffView.ResetScroll()
		}
		return
	}

	if selected == nil {
		return
	}

	compareKey := fmt.Sprintf("parent|bypass=%v|%s", m.DetailFilterBypass, filterQuery)
	if m.VsLive {
		compareKey = fmt.Sprintf("live:%d|bypass=%v|%s", m.liveConfigFetched.UnixNano(), m.DetailFilterBypass, filterQuery)
	} else if m.BaseCommitSHA != "" {
		compareKey = fmt.Sprintf("base:%s|bypass=%v|%s", m.BaseCommitSHA, m.DetailFilterBypass, filterQuery)
	}

	needsReset := selected.FullSHA != m.lastDetailCommitSHA ||
		m.ActiveTab != m.lastDetailTab ||
		compareKey != m.lastCompareKey
	if !needsReset {
		switch m.ActiveTab {
		case TabDiff:
			if m.DiffView.Viewport.TotalLineCount() > 0 {
				return
			}
		case TabConfig:
			if m.ConfigView.Viewport.TotalLineCount() > 0 {
				return
			}
		case TabBlame:
			if m.BlameView.Viewport.TotalLineCount() > 0 {
				return
			}
		}
	}

	m.lastDetailCommitSHA = selected.FullSHA
	m.lastDetailTab = m.ActiveTab
	m.lastCompareKey = compareKey

	selCopy := *selected
	m.DiffView.Commit = &selCopy
	m.DiffView.ParentSHA = m.parentSHAByFullSHA[selected.FullSHA]
	m.DiffView.FilterQuery = filterQuery
	m.DiffView.DetailFilterBypass = m.DetailFilterBypass
	m.DiffView.BaseCommit = m.baseCommit
	m.ConfigView.Commit = &selCopy
	m.ConfigView.BaseCommit = m.baseCommit
	m.BlameView.Commit = &selCopy
	m.BlameView.BaseCommit = m.baseCommit

	switch m.ActiveTab {
	case TabDiff:
		var prevCfg, currCfg map[string]interface{}
		var oldKey, newKey string
		liveStamp := int64(0)
		if m.VsLive {
			liveStamp = m.liveConfigFetched.UnixNano()
			if len(m.cachedLiveConfig) > 0 {
				currCfg = m.cachedLiveConfig
			} else {
				currCfg = m.GitBackend.GetLatestCommitConfig()
			}
			prevCfg = m.GitBackend.GetConfigAtCommit(selected.FullSHA)
			oldKey = selected.FullSHA
			newKey = "live"
		} else if m.BaseCommitSHA != "" {
			prevCfg = m.GitBackend.GetConfigAtCommit(m.BaseCommitSHA)
			currCfg = m.GitBackend.GetConfigAtCommit(selected.FullSHA)
			oldKey = m.BaseCommitSHA
			newKey = selected.FullSHA
		} else {
			parentSHA := fmt.Sprintf("%s~1", selected.FullSHA)
			prevCfg = m.GitBackend.GetConfigAtCommit(parentSHA)
			currCfg = m.GitBackend.GetConfigAtCommit(selected.FullSHA)
			oldKey = parentSHA
			newKey = selected.FullSHA
		}

		effectiveFilter := filterQuery
		if m.DetailFilterBypass {
			effectiveFilter = ""
		}
		diffRes := differ.SemanticDiff(prevCfg, currCfg, effectiveFilter)
		if cleanFilter == "" {
			m.DiffView.ShownChangeCount = 0
			m.DiffView.TotalUnfilteredChanges = 0
		} else {
			unfilteredKey := fmt.Sprintf("%s|%s|live=%v:%d", oldKey, newKey, m.VsLive, liveStamp)
			if m.DetailFilterBypass {
				if m.unfilteredDiffChanges == nil {
					m.unfilteredDiffChanges = make(map[string][]models.PathChange)
				}
				m.unfilteredDiffChanges[unfilteredKey] = diffRes.Changes
				totalUnfiltered := semanticChangeCount(diffRes)
				m.DiffView.ShownChangeCount = totalUnfiltered
				m.DiffView.TotalUnfilteredChanges = totalUnfiltered
			} else {
				allChanges := m.getUnfilteredDiffChanges(unfilteredKey, prevCfg, currCfg)
				m.DiffView.TotalUnfilteredChanges = countSemanticLeafChanges(allChanges, "")
				m.DiffView.ShownChangeCount = countSemanticLeafChanges(allChanges, cleanFilter)
			}
		}
		m.DiffView.SetDiff(diffRes)
		if needsReset {
			m.DiffView.ResetScroll()
		}

	case TabConfig:
		cfg := m.GitBackend.GetConfigAtCommit(selected.FullSHA)
		m.ConfigView.SetConfigScoped(cfg, filterQuery, m.DetailFilterBypass)
		if needsReset {
			m.ConfigView.ResetScroll()
		}

	case TabBlame:
		allEntries := m.BlameEngine.GetBlameLinesAtCommit("cli", selected.FullSHA, "")
		stats := m.BlameEngine.GetContributorStatsAtCommit("cli", selected.FullSHA)
		var entries []models.BlameEntry
		if cleanFilter == "" {
			entries = allEntries
			m.BlameView.TotalUnfilteredEntries = 0
			m.BlameView.ShownEntryCount = 0
		} else {
			m.BlameView.TotalUnfilteredEntries = len(allEntries)
			if m.DetailFilterBypass {
				entries = allEntries
				m.BlameView.ShownEntryCount = len(allEntries)
			} else {
				lowerFilter := strings.ToLower(cleanFilter)
				for _, entry := range allEntries {
					if filter.IsPathMatching(entry.Content, cleanFilter) ||
						strings.Contains(strings.ToLower(entry.Author), lowerFilter) ||
						strings.Contains(strings.ToLower(entry.CommitSHA), lowerFilter) {
						entries = append(entries, entry)
					}
				}
				m.BlameView.ShownEntryCount = len(entries)
			}
		}
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
	m.FilterBar.DetailBypass = m.DetailFilterBypass
	filterBar := m.FilterBar.View()

	// Rows 2 .. 2+ribbonH-1: Visual Timeline Ribbon (6 rows expanded, 1 row collapsed)
	ribbon := m.TimelineRibbon.View()
	ribbonH := m.TimelineRibbon.OuterHeight()

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

	// Rows 2+ribbonH .. m.Height-2: Main Workspace (exact outer height m.Height-3-ribbonH)
	paneInnerHeight := m.Height - 5 - ribbonH
	if paneInnerHeight < 3 {
		paneInnerHeight = 3
	}
	splitHeight := paneInnerHeight + 2
	var mainSplit string
	if m.hideTimelineList() {
		if m.collapsedRailOuterWidth() > 0 {
			rail := RenderCollapsedTimelineRail(splitHeight)
			splitSep := PaintSurface("", 1, splitHeight, ColorBackground, ColorText)
			mainSplit = lipgloss.JoinHorizontal(
				lipgloss.Top,
				rail,
				splitSep,
				detailContent,
			)
		} else {
			mainSplit = detailContent
		}
	} else {
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
		FilterActive:    strings.TrimSpace(m.FilterBar.Value()) != "",
		DetailBypass:    m.DetailFilterBypass,
		FocusedPane:     int(m.FocusedPane),
		ActiveTab:       int(m.ActiveTab),
		Zoomed:          m.Zoomed,
		HasBase:         m.BaseCommitSHA != "",
		VsLive:          m.VsLive,
		SplitMode:       m.DiffView.IsSplitActive(),
		FoldUnchanged:   m.FoldUnchanged,
		RibbonCollapsed: m.TimelineRibbon.Collapsed,
		WindowNarrowed:  m.TimelineRibbon.ActivePreset != "all",
		ToastText:       m.Notification,
		ToastKind:       m.notifyKind,
	})

	baseScreen := lipgloss.JoinVertical(lipgloss.Left, header, filterBar, ribbon, mainSplit, footer)

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
	case ModalCommitPicker:
		return PaintSurface(m.CommitPickerModal.View(), m.Width, m.Height, ColorBackground, ColorText)
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
