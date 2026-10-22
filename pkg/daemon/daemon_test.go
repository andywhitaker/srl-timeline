package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"timeline/pkg/gitbackend"
	"timeline/pkg/srlclient"
)

func TestHasNewCommitEvent(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "messages")
	cfgFile := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(logFile, []byte("initial boot log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, []byte(`{"system":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	client := &srlclient.SRLClient{IsOnBox: true}
	backend := gitbackend.NewGitBackend(filepath.Join(tmpDir, "repo"))
	d := NewTimelineDaemon(client, backend)
	d.ConfigWatchPath = cfgFile
	d.LogWatchPaths = []string{logFile}

	// 1. Initial call must return true so baseline/startup state is checked
	if !d.hasNewCommitEvent() {
		t.Fatalf("expected initial hasNewCommitEvent() to return true")
	}

	// 2. Subsequent idle call with no file changes must return false (no sr_cli polling)
	if d.hasNewCommitEvent() {
		t.Fatalf("expected idle hasNewCommitEvent() to return false")
	}

	// 3. Appending a non-commit syslog line (e.g. AAA accounting) must return false
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("2026-10-22T12:00:00Z srl1 aaa_mgr: accounting info From cli\n")
	_ = f.Close()
	if d.hasNewCommitEvent() {
		t.Fatalf("expected non-commit syslog append to return false")
	}

	// 4. Appending a commitSucceeded line must return true
	f, err = os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("2026-10-22T12:01:00Z srl1 MGMT_SERVER_69-INFO-commitSucceeded [commit-id 1]: The commit performed by user admin from cli (127.0.0.1) session 10.\n")
	_ = f.Close()
	if !d.hasNewCommitEvent() {
		t.Fatalf("expected commitSucceeded log append to return true")
	}

	// 5. Next idle tick after consuming commitSucceeded must return false
	if d.hasNewCommitEvent() {
		t.Fatalf("expected tick after consuming commitSucceeded to return false")
	}

	// 6. Modifying config.json must return true
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(cfgFile, []byte(`{"system":{"name":{"host-name":"srl1"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !d.hasNewCommitEvent() {
		t.Fatalf("expected config.json modification to return true")
	}
}

func TestRecordLockExclusion(t *testing.T) {
	unlock, ok := TryAcquireRecordLock()
	if !ok {
		t.Fatalf("expected first TryAcquireRecordLock to succeed")
	}
	defer unlock()

	// A second non-blocking lock attempt from another file descriptor must fail while held
	_, ok2 := TryAcquireRecordLock()
	if ok2 {
		t.Fatalf("expected second concurrent TryAcquireRecordLock to fail while lock is held")
	}
}
