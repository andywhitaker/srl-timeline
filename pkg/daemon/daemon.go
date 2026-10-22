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
	return acquireLockInternal(syscall.LOCK_EX | syscall.LOCK_NB)
}

// AcquireRecordLock acquires a blocking exclusive file lock for synchronous restore/cherry-pick commits.
func AcquireRecordLock() func() {
	unlock, _ := acquireLockInternal(syscall.LOCK_EX)
	return unlock
}

func acquireLockInternal(flockFlags int) (func(), bool) {
	lockPath := DefaultLockFile
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

// CaptureInitialState records baseline if repository is empty.
func (d *TimelineDaemon) CaptureInitialState() {
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

func (d *TimelineDaemon) monitorLoop() {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			d.checkAndRecordChange()
		}
	}
}

// hasNewCommitEvent checks whether /var/log/srlinux/buffer/messages has a new commitSucceeded event
// or /etc/opt/srlinux/config.json was modified, avoiding expensive sr_cli polling on idle ticks.
func (d *TimelineDaemon) hasNewCommitEvent() bool {
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
						if strings.Contains(string(buf), "commitSucceeded") {
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

func (d *TimelineDaemon) checkAndRecordChange() {
	unlock, acquired := TryAcquireRecordLock()
	if !acquired {
		// Another daemon or TUI/CLI restore instance is actively checking/recording
		return
	}
	defer unlock()

	// If repository has no baseline commit yet, capture it now!
	if !d.GitBackend.HasCommits() {
		d.CaptureInitialState()
		return
	}

	if !d.hasNewCommitEvent() {
		return
	}

	runningCfg, err := d.SRLClient.GetRunningConfig("/")
	if err != nil || len(runningCfg) == 0 {
		return
	}

	// Correlate author and session from system logs
	author := "admin"
	sessionID := ""
	commitID := ""
	comment := ""
	commitTime := time.Now().UTC()

	logs := d.SRLClient.GetCommitLogs(3)
	if len(logs) > 0 {
		latest := logs[0]
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

	if err == nil && recorded && sha != "" {
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
	}
}

// WriteCurrentPIDFile writes the current process PID to DefaultPIDFile or FallbackPIDFile.
func WriteCurrentPIDFile() string {
	pidFile := DefaultPIDFile
	_ = os.MkdirAll(filepath.Dir(pidFile), 0755)
	pidStr := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(pidFile, []byte(pidStr), 0644); err != nil {
		pidFile = FallbackPIDFile
		_ = os.WriteFile(pidFile, []byte(pidStr), 0644)
	}
	return pidFile
}

// RemoveOwnPIDFile removes DefaultPIDFile or FallbackPIDFile only if it contains the current process PID.
func RemoveOwnPIDFile() {
	pidStr := strconv.Itoa(os.Getpid())
	for _, pf := range []string{DefaultPIDFile, FallbackPIDFile} {
		if data, err := os.ReadFile(pf); err == nil && strings.TrimSpace(string(data)) == pidStr {
			_ = os.Remove(pf)
		}
	}
}

// ManageDaemonProcess handles start/stop/status CLI actions.
func ManageDaemonProcess(action string) error {
	switch action {
	case "start":
		if IsDaemonRunning(DefaultPIDFile) || IsDaemonRunning(FallbackPIDFile) {
			fmt.Println("Timeline daemon is already running.")
			return nil
		}
		d := NewTimelineDaemon(nil, nil)
		d.Start()
		pid := os.Getpid()
		pidFile := WriteCurrentPIDFile()
		fmt.Printf("Started Timeline daemon (PID %d)\n", pid)

		// Wait for signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		d.Stop()
		_ = os.Remove(pidFile)
		fmt.Println("Timeline daemon stopped.")
		return nil

	case "stop":
		data, err := os.ReadFile(DefaultPIDFile)
		targetPIDFile := DefaultPIDFile
		if err != nil {
			data, err = os.ReadFile(FallbackPIDFile)
			targetPIDFile = FallbackPIDFile
		}
		if err != nil {
			fmt.Println("Timeline daemon is not running.")
			return nil
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
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
		_ = os.Remove(targetPIDFile)
		return nil

	case "status":
		if IsDaemonRunning(DefaultPIDFile) {
			data, _ := os.ReadFile(DefaultPIDFile)
			fmt.Printf("Status: RUNNING (PID %s)\n", strings.TrimSpace(string(data)))
		} else if IsDaemonRunning(FallbackPIDFile) {
			data, _ := os.ReadFile(FallbackPIDFile)
			fmt.Printf("Status: RUNNING (PID %s)\n", strings.TrimSpace(string(data)))
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
