package srlclient

import (
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestParseCommitLogLine(t *testing.T) {
	// 1. Real SR Linux sr_mgmt_server EV|commitSucceeded log format
	srlLine := `2026-10-22T18:00:05.644205+00:00 srl sr_mgmt_server: mgmt|3107|3107|00033|EV|commitSucceeded|I: All changes have been committed successfully by user alice session 84.`
	entry, ok := ParseCommitLogLine(srlLine)
	if !ok {
		t.Fatalf("expected ParseCommitLogLine to match real SR Linux sr_mgmt_server log")
	}
	if entry.Username != "alice" || entry.Session != "84" {
		t.Fatalf("expected user=alice session=84, got user=%q session=%q", entry.Username, entry.Session)
	}

	// 2. Legacy/synthetic commit log format
	legacyLine := `2026-10-22T12:35:00.000Z srl1 sr_mgmt: commitSucceeded username='netops' session='77'`
	entry2, ok := ParseCommitLogLine(legacyLine)
	if !ok {
		t.Fatalf("expected ParseCommitLogLine to match legacy commit log")
	}
	if entry2.Username != "netops" || entry2.Session != "77" {
		t.Fatalf("expected user=netops session=77, got user=%q session=%q", entry2.Username, entry2.Session)
	}

	// 3. SR Linux 25.10+ sr_mgmt_server log format (without EV|commitSucceeded token)
	srl25Line := `2026-10-25T20:15:57.195804+00:00 srl1 sr_mgmt_server: mgmt|2882|2882|00024|I: All changes have been committed successfully by user root session 19.`
	entry3, ok := ParseCommitLogLine(srl25Line)
	if !ok {
		t.Fatalf("expected ParseCommitLogLine to match SR Linux 25.10+ sr_mgmt_server log")
	}
	if entry3.Username != "root" || entry3.Session != "19" {
		t.Fatalf("expected user=root session=19, got user=%q session=%q", entry3.Username, entry3.Session)
	}

	// 4. Unrelated log line
	if _, ok := ParseCommitLogLine("2026-10-22T12:36:00.000Z srl1 sshd[123]: Accepted publickey for admin"); ok {
		t.Fatalf("did not expect non-commit log line to match")
	}
}

func TestParseLatestCommitState(t *testing.T) {
	// Test out-of-order list items as returned by SR Linux /system/configuration/commit
	raw := map[string]interface{}{
		"srl_nokia-configuration:commit": []interface{}{
			map[string]interface{}{
				"id":       float64(2),
				"username": "root",
				"comment":  "earlier commit",
				"ended":    "2026-10-22T20:40:00.000Z",
				"status":   "complete",
			},
			map[string]interface{}{
				"id":       float64(5),
				"username": "admin",
				"comment":  "configure uplink port",
				"ended":    "2026-10-22T20:45:12.100Z (10 minutes ago)",
				"status":   "complete",
			},
			map[string]interface{}{
				"id":       float64(1),
				"username": "__internal__",
				"ended":    "2026-10-22T20:38:54.941Z (16 minutes ago)",
				"status":   "complete",
			},
		},
	}
	logEntry, ok := parseLatestCommitState(raw)
	if !ok {
		t.Fatalf("expected parseLatestCommitState to succeed")
	}
	if logEntry.CommitID != "5" || logEntry.Username != "admin" || logEntry.Comment != "configure uplink port" || logEntry.Timestamp != "2026-10-22T20:45:12.100Z" {
		t.Fatalf("unexpected parsed commit state: %+v", logEntry)
	}
}

func TestUnixSocketJSONRPCClient(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "sr_json_rpc")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket: %v", err)
	}
	defer ln.Close()

	var lastCommands []interface{}
	var lastDatastore string

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			method, _ := req["method"].(string)
			params, _ := req["params"].(map[string]interface{})
			lastDatastore, _ = params["datastore"].(string)
			cmds, _ := params["commands"].([]interface{})
			lastCommands = cmds

			if method == "get" {
				if len(cmds) == 3 {
					// GetSystemInfo request
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      req["id"],
						"result": []interface{}{
							map[string]interface{}{"host-name": "srl-leaf1"},
							map[string]interface{}{
								"version":     "v26.7.2-519",
								"description": "Nokia SR Linux Operating System",
							},
							map[string]interface{}{"type": "7220 IXR-D2L"},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req["id"],
					"result": []interface{}{
						map[string]interface{}{
							"srl_nokia-system:system": map[string]interface{}{
								"srl_nokia-system-name:name": map[string]interface{}{
									"host-name": "srl-leaf1",
								},
							},
						},
					},
				})
				return
			}

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  []interface{}{map[string]interface{}{}},
			})
		}),
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer srv.Close()

	client := &SRLClient{
		UnixSocketPath: sockPath,
		IsOnBox:        true,
	}

	// 1. GetRunningConfig over Unix socket
	cfg, err := client.GetRunningConfig("/")
	if err != nil {
		t.Fatalf("GetRunningConfig failed: %v", err)
	}
	sysMap, _ := cfg["system"].(map[string]interface{})
	nameMap, _ := sysMap["name"].(map[string]interface{})
	if nameMap["host-name"] != "srl-leaf1" {
		t.Fatalf("expected host-name=srl-leaf1, got %+v", cfg)
	}

	// 2. GetSystemInfo preserves chassis type instead of overwriting with description
	info := client.GetSystemInfo()
	if info["host-name"] != "srl-leaf1" || info["version"] != "v26.7.2-519" || info["chassis"] != "7220 IXR-D2L" {
		t.Fatalf("unexpected GetSystemInfo result: %+v", info)
	}

	// 3. ReplaceFullConfig preserves /system/json-rpc-server/unix-socket when on-box
	ok, err := client.ReplaceFullConfig(cfg)
	if err != nil || !ok {
		t.Fatalf("ReplaceFullConfig failed: %v", err)
	}
	if len(lastCommands) != 2 {
		t.Fatalf("expected 2 commands in ReplaceFullConfig (replace / + update unix-socket), got %d", len(lastCommands))
	}
	cmd1, _ := lastCommands[1].(map[string]interface{})
	if cmd1["path"] != "/system/json-rpc-server/unix-socket" || cmd1["action"] != "update" {
		t.Fatalf("expected second command to preserve unix-socket, got %+v", cmd1)
	}

	// 4. SaveStartupConfig uses tools datastore
	ok, err = client.SaveStartupConfig()
	if err != nil || !ok {
		t.Fatalf("SaveStartupConfig failed: %v", err)
	}
	if lastDatastore != "tools" {
		t.Fatalf("expected SaveStartupConfig datastore=tools, got %q", lastDatastore)
	}
}


