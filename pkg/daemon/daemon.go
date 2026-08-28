package daemon

import (
	"fmt"
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
	DefaultPIDFile   = "/var/run/timeline_daemon.pid"
	DefaultLockFile  = "/var/run/timeline_record.lock"
	FallbackLockFile = "/tmp/timeline_record.lock"
)

type CommitListener func(models.TimelineCommit)

// TimelineDaemon monitors Nokia SR Linux for configuration changes in realtime.
type TimelineDaemon struct {
	SRLClient  *srlclient.SRLClient
	GitBackend *gitbackend.GitBackend
	PIDFile    string

	mu          sync.Mutex
	listeners   []CommitListener
	stopChan    chan struct{}
	lastChange  string
	isRunning   bool
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
		fmt.Printf("Captured initial baseline commit %s\n", sha[:8])
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

func (d *TimelineDaemon) checkAndRecordChange() {
	lockPath := DefaultLockFile
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		lockPath = FallbackLockFile
		lockFile, err = os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	}
	if err == nil {
		defer lockFile.Close()
		if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			// Another daemon or TUI instance is actively checking/recording
			return
		}
		defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	}

	// If repository has no baseline commit yet, capture it now!
	if !d.GitBackend.HasCommits() {
		d.CaptureInitialState()
		return
	}

	runningCfg, err := d.SRLClient.GetRunningConfig("/")
	if err != nil || len(runningCfg) == 0 {
		return
	}

	// Correlate author and session from system logs
	author := "admin"
	sessionID := ""
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
		if parsedTime, err := time.Parse(time.RFC3339, latest.Timestamp); err == nil {
			commitTime = parsedTime
		}
	}

	recorded, sha, err := d.GitBackend.RecordConfigChange(
		runningCfg,
		author,
		"",
		sessionID,
		commitTime,
		"",
		nil,
	)

	if err == nil && recorded && sha != "" {
		commitID := sha
		if len(commitID) > 8 {
			commitID = commitID[:8]
		}
		commitObj := models.TimelineCommit{
			CommitID:  commitID,
			FullSHA:   sha,
			Timestamp: commitTime,
			Author:    author,
			SessionID: sessionID,
			IsCurrent: true,
		}
		d.notifyListeners(commitObj)
	}
}

// ManageDaemonProcess handles start/stop/status CLI actions.
func ManageDaemonProcess(action string) error {
	pidFile := DefaultPIDFile
	_ = os.MkdirAll(filepath.Dir(pidFile), 0755)

	switch action {
	case "start":
		if IsDaemonRunning(pidFile) {
			fmt.Println("Timeline daemon is already running.")
			return nil
		}
		d := NewTimelineDaemon(nil, nil)
		d.Start()
		pid := os.Getpid()
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644)
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
		data, err := os.ReadFile(pidFile)
		if err != nil {
			fmt.Println("Timeline daemon is not running.")
			return nil
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			proc, err := os.FindProcess(pid)
			if err == nil {
				_ = proc.Signal(syscall.SIGTERM)
				fmt.Printf("Stopped Timeline daemon (PID %d)\n", pid)
			}
		}
		_ = os.Remove(pidFile)
		return nil

	case "status":
		if IsDaemonRunning(pidFile) {
			data, _ := os.ReadFile(pidFile)
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
	return proc.Signal(syscall.Signal(0)) == nil
}
