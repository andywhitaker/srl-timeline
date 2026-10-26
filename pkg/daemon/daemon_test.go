package daemon

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	// 2. Subsequent idle call with no file changes must return false
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

func TestNativeCommitStateDetectionAndRecording(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "sr_json_rpc")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket: %v", err)
	}
	defer ln.Close()

	var mu sync.Mutex
	currentHostName := "srl-init"
	failRunningCfg := false
	commitList := []interface{}{
		map[string]interface{}{
			"id":       float64(1),
			"username": "admin",
			"comment":  "initial boot",
			"ended":    "2026-10-25T20:00:00Z",
			"status":   "complete",
		},
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			params, _ := req["params"].(map[string]interface{})
			cmds, _ := params["commands"].([]interface{})
			if len(cmds) > 0 {
				cmd0, _ := cmds[0].(map[string]interface{})
				if cmd0["path"] == "/" {
					mu.Lock()
					shouldFail := failRunningCfg
					mu.Unlock()
					if shouldFail {
						http.Error(w, "transient server busy", http.StatusServiceUnavailable)
						return
					}
				}
				if cmd0["path"] == "/system/configuration/commit" {
					mu.Lock()
					listCopy := append([]interface{}{}, commitList...)
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      req["id"],
						"result": []interface{}{
							map[string]interface{}{
								"srl_nokia-configuration:commit": listCopy,
							},
						},
					})
					return
				}
			}
			mu.Lock()
			hn := currentHostName
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": []interface{}{
					map[string]interface{}{
						"srl_nokia-system:system": map[string]interface{}{
							"name": map[string]interface{}{
								"host-name": hn,
							},
						},
					},
				},
			})
		}),
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer srv.Close()

	client := &srlclient.SRLClient{
		UnixSocketPath: sockPath,
		IsOnBox:        true,
	}
	backend := gitbackend.NewGitBackend(filepath.Join(tmpDir, "repo"))
	d := NewTimelineDaemon(client, backend)

	// 1. Capture baseline commit
	d.CaptureInitialState()
	if !backend.HasCommits() {
		t.Fatalf("expected baseline commit to be recorded")
	}

	// 2. First checkAndRecordChange initializes watch state from /system/configuration/commit id=1
	d.checkAndRecordChange()
	if d.hasNewCommitEvent() {
		t.Fatalf("expected idle hasNewCommitEvent() to return false when /system/configuration/commit id has not changed")
	}

	// 3. Simulate a new commit in /system/configuration/commit state without any syslog or config.json files
	mu.Lock()
	currentHostName = "srl-updated"
	commitList = append(commitList, map[string]interface{}{
		"id":       float64(2),
		"username": "netops",
		"comment":  "update hostname via native state",
		"ended":    "2026-10-25T20:05:00Z",
		"status":   "complete",
	})
	mu.Unlock()

	d.checkAndRecordChange()

	commits := backend.GetTimeline(10, "")
	if len(commits) < 2 {
		t.Fatalf("expected 2 commits after native commit state update, got %d", len(commits))
	}
	latest := commits[0]
	if latest.Author != "netops" || latest.SRLCommitID != "2" || !strings.Contains(latest.Message, "update hostname via native state") {
		t.Fatalf("unexpected latest commit metadata from native state: %+v", latest)
	}

	// 4. Subsequent idle poll must return false
	if d.hasNewCommitEvent() {
		t.Fatalf("expected subsequent idle hasNewCommitEvent() to return false")
	}

	// 5. Simulate rapid consecutive commits (#3 and #4) within a single polling interval AND a transient GetRunningConfig failure on first attempt
	mu.Lock()
	failRunningCfg = true
	currentHostName = "srl-rapid-4"
	commitList = append(commitList,
		map[string]interface{}{
			"id":       float64(3),
			"username": "alice",
			"comment":  "rapid commit 3",
			"ended":    "2026-10-25T20:06:00Z",
			"status":   "complete",
		},
		map[string]interface{}{
			"id":       float64(4),
			"username": "bob",
			"comment":  "rapid commit 4",
			"ended":    "2026-10-25T20:06:01Z",
			"status":   "complete",
		},
	)
	mu.Unlock()

	// First poll fails GetRunningConfig("/") -> must roll back watch cursor so next poll retries
	d.checkAndRecordChange()
	if len(backend.GetTimeline(10, "")) != 2 {
		t.Fatalf("expected commit count to remain 2 when GetRunningConfig failed")
	}

	// Next poll succeeds and records net config with commit #4 metadata
	mu.Lock()
	failRunningCfg = false
	mu.Unlock()
	d.checkAndRecordChange()

	commits = backend.GetTimeline(10, "")
	if len(commits) != 3 {
		t.Fatalf("expected 3 commits after retry of rapid commits, got %d", len(commits))
	}
	if commits[0].Author != "bob" || commits[0].SRLCommitID != "4" || !strings.Contains(commits[0].Message, "rapid commit 4") {
		t.Fatalf("expected commit #4 metadata to be recorded after retry, got %+v", commits[0])
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
