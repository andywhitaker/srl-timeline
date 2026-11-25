package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"timeline/pkg/gitbackend"
	"timeline/pkg/models"
	"timeline/pkg/srlclient"
)

const (
	DefaultPIDFile  = "/var/run/timeline_daemon.pid"
	SharedPIDFile   = "/etc/opt/srlinux/timeline/timeline_daemon.pid"
	FallbackPIDFile = "/tmp/timeline_daemon.pid"
	DefaultLockFile = "/etc/opt/srlinux/timeline/timeline.lock"
)

// FallbackLockFile returns a user-scoped lock path in /tmp when /etc/opt/srlinux/timeline is not writable,
// avoiding EACCES across different UIDs when sysctl fs.protected_regular = 2 is active on sticky /tmp.
func FallbackLockFile() string {
	return fmt.Sprintf("/tmp/timeline_record_%d.lock", os.Getuid())
}

// TryAcquireRecordLock attempts to acquire a non-blocking exclusive file lock for recording commits.
func TryAcquireRecordLock() (func(), bool) {
	return acquireLockInternal("", syscall.LOCK_EX|syscall.LOCK_NB)
}

// TryAcquireRepoRecordLock attempts to acquire a non-blocking exclusive file lock scoped to repoDir.
func TryAcquireRepoRecordLock(repoDir string) (func(), bool) {
	return acquireLockInternal(repoDir, syscall.LOCK_EX|syscall.LOCK_NB)
}

// AcquireRecordLock acquires a blocking exclusive file lock for synchronous restore/cherry-pick commits.
func AcquireRecordLock() func() {
	unlock, _ := acquireLockInternal("", syscall.LOCK_EX)
	return unlock
}

// AcquireRepoRecordLock acquires a blocking exclusive file lock scoped to repoDir.
func AcquireRepoRecordLock(repoDir string) func() {
	unlock, _ := acquireLockInternal(repoDir, syscall.LOCK_EX)
	return unlock
}

func acquireLockInternal(repoDir string, flockFlags int) (func(), bool) {
	lockPath := DefaultLockFile
	if repoDir != "" {
		lockPath = filepath.Join(filepath.Dir(repoDir), "timeline.lock")
	}
	lockDir := filepath.Dir(lockPath)
	_ = os.MkdirAll(lockDir, os.ModeSetgid|0775)
	gitbackend.ApplySharedDirPermissions(lockDir)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0664)
	if err != nil {
		lockPath = FallbackLockFile()
		lockFile, err = os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	} else {
		gitbackend.ApplySharedFilePermissions(lockPath)
	}
	if err != nil {
		// If neither lock file can be opened, allow operation to proceed with no-op unlock
		return func() {}, true
	}
	if err := syscall.Flock(int(lockFile.Fd()), flockFlags); err != nil {
		_ = lockFile.Close()
		return func() {}, false
	}
	return func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
	}, true
}

type CommitListener func(models.TimelineCommit)

// TimelineDaemon monitors Nokia SR Linux for configuration changes in realtime.
type TimelineDaemon struct {
	SRLClient       *srlclient.SRLClient
	GitBackend      *gitbackend.GitBackend
	PIDFile         string
	ConfigWatchPath string
	LogWatchPaths   []string

	mu               sync.Mutex
	listeners        []CommitListener
	stopChan         chan struct{}
	lastChange       string
	isRunning        bool
	initializedWatch bool
	lastCommitID     string
	lastCommitEnded  string
	lastCommitState  *srlclient.CommitLog
	lastConfigMtime  time.Time
	lastConfigSize   int64
	lastLogInode     uint64
	lastLogOffset    int64
}

// NewTimelineDaemon creates a new daemon instance.
func NewTimelineDaemon(client *srlclient.SRLClient, backend *gitbackend.GitBackend) *TimelineDaemon {
	if client == nil {
		client = srlclient.NewSRLClient()
	}
	if backend == nil {
		backend = gitbackend.NewGitBackend("")
	}
	return &TimelineDaemon{
		SRLClient:  client,
		GitBackend: backend,
		PIDFile:    DefaultPIDFile,
		stopChan:   make(chan struct{}),
	}
}

// RegisterListener registers a callback invoked when a new commit is detected.
func (d *TimelineDaemon) RegisterListener(l CommitListener) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners = append(d.listeners, l)
}

func (d *TimelineDaemon) notifyListeners(commit models.TimelineCommit) {
	d.mu.Lock()
	listeners := append([]CommitListener{}, d.listeners...)
	d.mu.Unlock()

	for _, l := range listeners {
		go l(commit)
	}
}

func (d *TimelineDaemon) repoDir() string {
	if d.GitBackend != nil {
		return d.GitBackend.RepoDir
	}
	return ""
}

// CaptureInitialState records baseline if repository is empty.
func (d *TimelineDaemon) CaptureInitialState() {
	unlock, acquired := TryAcquireRepoRecordLock(d.repoDir())
	if !acquired {
		return
	}
	defer unlock()
	d.captureInitialStateLocked()
}

func (d *TimelineDaemon) captureInitialStateLocked() {
	if d.GitBackend.HasCommits() {
		return
	}

	runningCfg, err := d.SRLClient.GetRunningConfig("/")
	if err != nil || len(runningCfg) == 0 {
		return
	}

	ok, sha, err := d.GitBackend.EnsureBaseline(runningCfg)
	if err == nil && ok && sha != "" {
		shortSHA := sha
		if len(shortSHA) > 8 {
			shortSHA = shortSHA[:8]
		}
		fmt.Printf("Captured initial baseline commit %s\n", shortSHA)
	}
}

// Start runs the monitoring loop in a background goroutine.
func (d *TimelineDaemon) Start() {
	d.mu.Lock()
	if d.isRunning {
		d.mu.Unlock()
		return
	}
	d.isRunning = true
	d.stopChan = make(chan struct{})
	d.mu.Unlock()

	if d.SRLClient != nil && d.SRLClient.IsOnBox && os.Geteuid() == 0 {
		_ = d.SRLClient.EnsureUnixSocket()
	}

	d.CaptureInitialState()

	go d.monitorLoop()
}

// Stop terminates the daemon monitoring loop.
func (d *TimelineDaemon) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.isRunning {
		return
	}
	d.isRunning = false
	close(d.stopChan)
}

func (d *TimelineDaemon) resolveWatchTargets() (logPath, cfgPath string) {
	if d.SRLClient != nil && !d.SRLClient.IsOnBox && d.ConfigWatchPath == "" && len(d.LogWatchPaths) == 0 {
		return "", ""
	}
	cfgCandidate := d.ConfigWatchPath
	if cfgCandidate == "" {
		cfgCandidate = "/etc/opt/srlinux/config.json"
	}
	if _, err := os.Stat(cfgCandidate); err == nil {
		cfgPath = cfgCandidate
	}
	logCandidates := d.LogWatchPaths
	if len(logCandidates) == 0 {
		logCandidates = []string{
			"/var/log/srlinux/buffer/messages",
			"/var/log/messages",
		}
	}
	for _, p := range logCandidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			logPath = p
			break
		}
	}
	return logPath, cfgPath
}

func initLogTailPosition(path string) (inode uint64, offset int64) {
	if path == "" {
		return 0, 0
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return 0, 0
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		inode = st.Ino
	}
	return inode, fi.Size()
}

func logTailHasCommit(path string, lastInode *uint64, lastOffset *int64) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	var inode uint64
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		inode = st.Ino
	}
	size := fi.Size()
	if inode != *lastInode || size < *lastOffset {
		*lastInode = inode
		*lastOffset = 0
	}
	if size <= *lastOffset {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	readOffset := *lastOffset
	if readOffset == 0 && size > 65536 {
		readOffset = size - 65536
	}
	if _, err := f.Seek(readOffset, io.SeekStart); err != nil {
		return false
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return false
	}
	*lastOffset = readOffset + int64(len(buf))
	s := string(buf)
	return strings.Contains(s, "commitSucceeded") || strings.Contains(s, "committed successfully")
}

func (d *TimelineDaemon) monitorLoop() {
	logPath, cfgPath := d.resolveWatchTargets()
	var targets []string
	var resolvedLogPath string
	if logPath != "" {
		targets = append(targets, logPath)
		if eval, err := filepath.EvalSymlinks(logPath); err == nil && eval != "" {
			resolvedLogPath = eval
		} else {
			resolvedLogPath = logPath
		}
	}
	if cfgPath != "" {
		targets = append(targets, cfgPath)
	}

	tailInode, tailOffset := initLogTailPosition(resolvedLogPath)

	var watcher *gitbackend.FileWatcher
	var watchEvents <-chan string
	fallbackInterval := 1500 * time.Millisecond
	if len(targets) > 0 {
		if w, err := gitbackend.NewFileWatcher(targets...); err == nil {
			watcher = w
			watchEvents = w.Events
			// inotify accelerates commit detection to ~35ms; keep a 2s structured JSON-RPC
			// /system/configuration/commit fallback as the version-proof source of truth.
			fallbackInterval = 2 * time.Second
			defer func() { _ = watcher.Close() }()
		}
	}

	// Perform one immediate check on startup to catch any changes that happened while stopped.
	d.checkAndRecordChange()

	ticker := time.NewTicker(fallbackInterval)
	defer ticker.Stop()

	var retryTimer *time.Timer
	var retryC <-chan time.Time
	stopRetry := func() {
		if retryTimer != nil {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retryTimer = nil
			retryC = nil
		}
	}
	defer stopRetry()

	for {
		select {
		case <-d.stopChan:
			return
		case evPath, ok := <-watchEvents:
			if !ok {
				watchEvents = nil
				continue
			}
			shouldCheck := false
			if resolvedLogPath != "" && filepath.Base(evPath) == filepath.Base(resolvedLogPath) {
				shouldCheck = logTailHasCommit(resolvedLogPath, &tailInode, &tailOffset)
			} else {
				shouldCheck = true
			}
			if !shouldCheck {
				continue
			}
			if !d.checkAndRecordChange() {
				// If syslog flushed slightly before state datastore updated or a restore held the lock,
				// schedule one quick 250ms re-check.
				stopRetry()
				retryTimer = time.NewTimer(250 * time.Millisecond)
				retryC = retryTimer.C
			} else {
				stopRetry()
			}
		case <-retryC:
			retryTimer = nil
			retryC = nil
			d.checkAndRecordChange()
		case <-ticker.C:
			d.checkAndRecordChange()
		}
	}
}

// hasNewCommitEvent queries /system/configuration/commit in the state datastore via native JSON-RPC
// as the primary commit trigger, falling back to config/log file watch only when state is unavailable.
func (d *TimelineDaemon) hasNewCommitEvent() bool {
	d.lastCommitState = nil
	if d.SRLClient != nil && d.ConfigWatchPath == "" && len(d.LogWatchPaths) == 0 {
		if stateCommit, ok := d.SRLClient.GetLatestCommitState(); ok {
			d.lastCommitState = &stateCommit
			if !d.initializedWatch || stateCommit.CommitID != d.lastCommitID || stateCommit.Timestamp != d.lastCommitEnded {
				d.initializedWatch = true
				d.lastCommitID = stateCommit.CommitID
				d.lastCommitEnded = stateCommit.Timestamp
				return true
			}
			return false
		}
	}

	if !d.SRLClient.IsOnBox {
		return true
	}

	configPath := d.ConfigWatchPath
	if configPath == "" {
		configPath = "/etc/opt/srlinux/config.json"
	}
	logPaths := d.LogWatchPaths
	if len(logPaths) == 0 {
		logPaths = []string{
			"/var/log/srlinux/buffer/messages",
			"/var/log/messages",
		}
	}

	var activeLogPath string
	var logInfo os.FileInfo
	for _, p := range logPaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			activeLogPath = p
			logInfo = fi
			break
		}
	}
	cfgInfo, cfgErr := os.Stat(configPath)

	// If neither switch file exists (e.g. unit test environment), allow check
	if activeLogPath == "" && cfgErr != nil {
		return true
	}

	changed := false
	if !d.initializedWatch {
		d.initializedWatch = true
		changed = true
	}

	if cfgErr == nil {
		if !cfgInfo.ModTime().Equal(d.lastConfigMtime) || cfgInfo.Size() != d.lastConfigSize {
			d.lastConfigMtime = cfgInfo.ModTime()
			d.lastConfigSize = cfgInfo.Size()
			changed = true
		}
	}

	if activeLogPath != "" && logInfo != nil {
		var inode uint64
		if stat, ok := logInfo.Sys().(*syscall.Stat_t); ok {
			inode = stat.Ino
		}
		size := logInfo.Size()

		if inode != d.lastLogInode || size < d.lastLogOffset {
			d.lastLogInode = inode
			d.lastLogOffset = 0
		}

		if size > d.lastLogOffset {
			f, err := os.Open(activeLogPath)
			if err == nil {
				readOffset := d.lastLogOffset
				// On initial attach to a large log file, only scan the last 64 KB
				if readOffset == 0 && size > 65536 {
					readOffset = size - 65536
				}
				if _, seekErr := f.Seek(readOffset, io.SeekStart); seekErr == nil {
					buf, readErr := io.ReadAll(f)
					if readErr == nil {
						d.lastLogOffset = readOffset + int64(len(buf))
						s := string(buf)
						if strings.Contains(s, "commitSucceeded") || strings.Contains(s, "committed successfully by user") {
							changed = true
						}
					}
				}
				_ = f.Close()
			}
		}
	}

	return changed
}

func (d *TimelineDaemon) checkAndRecordChange() bool {
	unlock, acquired := TryAcquireRepoRecordLock(d.repoDir())
	if !acquired {
		// Another daemon or TUI/CLI restore instance is actively checking/recording
		return false
	}
	defer unlock()

	// If repository has no baseline commit yet, capture it now!
	if !d.GitBackend.HasCommits() {
		d.captureInitialStateLocked()
		return d.GitBackend.HasCommits()
	}

	prevInit := d.initializedWatch
	prevID := d.lastCommitID
	prevEnded := d.lastCommitEnded
	prevMtime := d.lastConfigMtime
	prevSize := d.lastConfigSize
	prevLogOffset := d.lastLogOffset

	if !d.hasNewCommitEvent() {
		return false
	}

	restoreWatchCursor := func() {
		d.initializedWatch = prevInit
		d.lastCommitID = prevID
		d.lastCommitEnded = prevEnded
		d.lastConfigMtime = prevMtime
		d.lastConfigSize = prevSize
		d.lastLogOffset = prevLogOffset
	}

	runningCfg, err := d.SRLClient.GetRunningConfig("/")
	if err != nil || len(runningCfg) == 0 {
		restoreWatchCursor()
		return false
	}

	// Correlate author, commit ID, comment, and timestamp from native /system/configuration/commit state
	author := "admin"
	sessionID := ""
	commitID := ""
	comment := ""
	commitTime := time.Now().UTC()

	var latest srlclient.CommitLog
	hasLatest := false
	if d.lastCommitState != nil {
		latest = *d.lastCommitState
		hasLatest = true
	} else if st, ok := d.SRLClient.GetLatestCommitState(); ok {
		latest = st
		hasLatest = true
	} else if logs := d.SRLClient.GetCommitLogs(1); len(logs) > 0 {
		latest = logs[0]
		hasLatest = true
	}

	if hasLatest {
		if latest.Username != "" {
			author = latest.Username
		}
		if latest.Session != "" {
			sessionID = latest.Session
		}
		if latest.CommitID != "" {
			commitID = latest.CommitID
		}
		if latest.Comment != "" {
			comment = latest.Comment
		}
		if parsedTime, err := time.Parse(time.RFC3339, latest.Timestamp); err == nil {
			commitTime = parsedTime
		} else if parsedTime, err := time.Parse(time.RFC3339Nano, latest.Timestamp); err == nil {
			commitTime = parsedTime
		}
	}

	var meta map[string]interface{}
	if comment != "" {
		meta = map[string]interface{}{"comment": comment}
	}

	recorded, sha, err := d.GitBackend.RecordConfigChange(
		runningCfg,
		author,
		commitID,
		sessionID,
		commitTime,
		"",
		meta,
	)
	if err != nil {
		restoreWatchCursor()
		return false
	}

	if recorded && sha != "" {
		if tl := d.GitBackend.GetTimeline(1, ""); len(tl) > 0 && tl[0].FullSHA == sha {
			d.notifyListeners(tl[0])
			return true
		}
		shortSHA := sha
		if len(shortSHA) > 8 {
			shortSHA = shortSHA[:8]
		}
		msg := fmt.Sprintf("Config commit by %s", author)
		if commitID != "" {
			msg += fmt.Sprintf(" (SRL commit #%s)", commitID)
		}
		if comment != "" {
			msg += fmt.Sprintf(" — %s", comment)
		}
		commitObj := models.TimelineCommit{
			CommitID:    shortSHA,
			FullSHA:     sha,
			Timestamp:   commitTime,
			Author:      author,
			SRLCommitID: commitID,
			SessionID:   sessionID,
			Message:     msg,
			IsCurrent:   true,
		}
		d.notifyListeners(commitObj)
		return true
	}
	return false
}

// WriteCurrentPIDFile writes the current process PID to DefaultPIDFile, SharedPIDFile, or FallbackPIDFile.
func WriteCurrentPIDFile() string {
	pidStr := strconv.Itoa(os.Getpid())
	var chosen string

	_ = os.MkdirAll(filepath.Dir(DefaultPIDFile), 0755)
	if err := os.WriteFile(DefaultPIDFile, []byte(pidStr), 0644); err == nil {
		chosen = DefaultPIDFile
	}

	sharedDir := filepath.Dir(SharedPIDFile)
	if info, err := os.Stat(sharedDir); err == nil && info.IsDir() {
		if err := os.WriteFile(SharedPIDFile, []byte(pidStr), 0664); err == nil {
			gitbackend.ApplySharedFilePermissions(SharedPIDFile)
			if chosen == "" {
				chosen = SharedPIDFile
			}
		}
	}

	if chosen == "" {
		chosen = FallbackPIDFile
		_ = os.WriteFile(FallbackPIDFile, []byte(pidStr), 0644)
	}
	return chosen
}

// RemoveOwnPIDFile removes PID files only if they contain the current process PID.
func RemoveOwnPIDFile() {
	pidStr := strconv.Itoa(os.Getpid())
	for _, pf := range []string{DefaultPIDFile, SharedPIDFile, FallbackPIDFile} {
		if data, err := os.ReadFile(pf); err == nil && strings.TrimSpace(string(data)) == pidStr {
			_ = os.Remove(pf)
		}
	}
}

// IsAnyDaemonRunning checks all candidate PID files for an active daemon process.
func IsAnyDaemonRunning() (bool, string, string) {
	for _, pf := range []string{DefaultPIDFile, SharedPIDFile, FallbackPIDFile} {
		if IsDaemonRunning(pf) {
			data, _ := os.ReadFile(pf)
			return true, strings.TrimSpace(string(data)), pf
		}
	}
	return false, "", ""
}

// ManageDaemonProcess handles start/stop/status/bootstrap CLI actions.
func ManageDaemonProcess(action string) error {
	switch action {
	case "bootstrap":
		client := srlclient.NewSRLClient()
		_ = client.EnsureUnixSocket()
		d := NewTimelineDaemon(client, nil)
		d.CaptureInitialState()
		_, _ = client.SaveStartupConfig()
		_, reloadErr := client.ReloadAppMgr()
		return reloadErr

	case "start":
		if running, _, _ := IsAnyDaemonRunning(); running {
			fmt.Println("Timeline daemon is already running.")
			return nil
		}
		pid := os.Getpid()
		pidFile := WriteCurrentPIDFile()
		defer func() {
			RemoveOwnPIDFile()
			_ = os.Remove(pidFile)
		}()

		d := NewTimelineDaemon(nil, nil)
		d.Start()
		fmt.Printf("Started Timeline daemon (PID %d)\n", pid)

		// Wait for signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		d.Stop()
		fmt.Println("Timeline daemon stopped.")
		return nil

	case "stop":
		_, pidStr, _ := IsAnyDaemonRunning()
		if pidStr == "" {
			for _, pf := range []string{DefaultPIDFile, SharedPIDFile, FallbackPIDFile} {
				if data, err := os.ReadFile(pf); err == nil && strings.TrimSpace(string(data)) != "" {
					pidStr = strings.TrimSpace(string(data))
					break
				}
			}
		}
		if pidStr == "" {
			fmt.Println("Timeline daemon is not running.")
			return nil
		}
		pid, _ := strconv.Atoi(pidStr)
		if pid > 0 {
			proc, err := os.FindProcess(pid)
			if err == nil {
				sigErr := proc.Signal(syscall.SIGTERM)
				if sigErr != nil && !errors.Is(sigErr, os.ErrProcessDone) {
					return fmt.Errorf("failed to stop daemon (PID %d): %w", pid, sigErr)
				}
				fmt.Printf("Stopped Timeline daemon (PID %d)\n", pid)
			}
		}
		for _, pf := range []string{DefaultPIDFile, SharedPIDFile, FallbackPIDFile} {
			_ = os.Remove(pf)
		}
		return nil

	case "status":
		if running, pidStr, _ := IsAnyDaemonRunning(); running {
			fmt.Printf("Status: RUNNING (PID %s)\n", pidStr)
		} else {
			fmt.Println("Status: STOPPED")
		}
		return nil

	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

// IsDaemonRunning checks if the background daemon is currently running.
func IsDaemonRunning(pidFile string) bool {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true
	}
	if _, statErr := os.Stat(fmt.Sprintf("/proc/%d", pid)); statErr == nil {
		return true
	}
	return false
}
