package restorer

import (
	"reflect"
	"testing"
	"time"

	"timeline/pkg/gitbackend"
)

func TestBuildCherryPickCLIStatementsDeletionsAndSubtrees(t *testing.T) {
	tmpDir := t.TempDir()
	backend := gitbackend.NewGitBackend(tmpDir)

	cfg1 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "Old Port 1",
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"admin-state": "enable",
				"description": "Port 2 To Be Deleted",
			},
		},
	}

	cfg2 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
			},
		},
	}

	_, sha1, err := backend.RecordConfigChange(cfg1, "admin", "1", "", time.Now().UTC(), "Commit 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha2, err := backend.RecordConfigChange(cfg2, "admin", "2", "", time.Now().UTC(), "Delete Port 1 desc and Port 2", nil)
	if err != nil {
		t.Fatal(err)
	}

	restorer := NewConfigRestorer(nil, backend)

	// Case 1: Cherry-pick a single deleted leaf (/interface[name=ethernet-1/1]/description)
	stmts1, err := restorer.BuildCherryPickCLIStatements(sha2, "/interface[name=ethernet-1/1]/description")
	if err != nil {
		t.Fatalf("unexpected error for deleted leaf: %v", err)
	}
	want1 := []string{"delete / interface ethernet-1/1 description"}
	if !reflect.DeepEqual(stmts1, want1) {
		t.Fatalf("got %v, want %v", stmts1, want1)
	}

	// Case 2: Cherry-pick a deleted list item (/interface[name=ethernet-1/2])
	stmts2, err := restorer.BuildCherryPickCLIStatements(sha2, "/interface[name=ethernet-1/2]")
	if err != nil {
		t.Fatalf("unexpected error for deleted list item: %v", err)
	}
	want2 := []string{"delete / interface ethernet-1/2"}
	if !reflect.DeepEqual(stmts2, want2) {
		t.Fatalf("got %v, want %v", stmts2, want2)
	}

	// Case 3: Cherry-pick parent subtree (/interface) containing both deletions and remaining state
	stmts3, err := restorer.BuildCherryPickCLIStatements(sha2, "/interface")
	if err != nil {
		t.Fatalf("unexpected error for parent subtree: %v", err)
	}
	want3 := []string{
		"delete / interface ethernet-1/1 description",
		"delete / interface ethernet-1/2",
		"set / interface ethernet-1/1 admin-state enable",
	}
	if !reflect.DeepEqual(stmts3, want3) {
		t.Fatalf("got %v, want %v", stmts3, want3)
	}

	// Case 4: Non-existent path in parent, target, and HEAD returns error
	if _, err := restorer.BuildCherryPickCLIStatements(sha2, "/system/nonexistent"); err == nil {
		t.Fatalf("expected error for nonexistent path")
	}

	// Case 5: Add lo0 in Commit 3 (HEAD), then cherry-pick/restore /interface[name=lo0] from baseline sha1
	cfg3 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
			},
			map[string]interface{}{
				"name":        "lo0",
				"admin-state": "enable",
				"description": "Loopback 0",
			},
		},
	}
	if _, _, err := backend.RecordConfigChange(cfg3, "admin", "3", "", time.Now().UTC(), "Add lo0", nil); err != nil {
		t.Fatal(err)
	}
	stmts5, err := restorer.BuildCherryPickCLIStatements(sha1, "/interface[name=lo0]")
	if err != nil {
		t.Fatalf("unexpected error when restoring absent path from baseline commit: %v", err)
	}
	want5 := []string{"delete / interface lo0"}
	if !reflect.DeepEqual(stmts5, want5) {
		t.Fatalf("got %v, want %v", stmts5, want5)
	}

	// Case 6: BuildCherryPickCommands produces native JSON-RPC delete and update commands
	cmds, err := restorer.BuildCherryPickCommands(sha2, "/interface")
	if err != nil {
		t.Fatalf("BuildCherryPickCommands failed: %v", err)
	}
	if len(cmds) != 4 {
		t.Fatalf("expected 4 JSON-RPC commands (3 deletes + 1 update), got %d: %+v", len(cmds), cmds)
	}
	if cmds[0]["action"] != "delete" || cmds[0]["path"] != "/interface[name=ethernet-1/1]/description" {
		t.Fatalf("unexpected first command: %+v", cmds[0])
	}
	if cmds[1]["action"] != "delete" || cmds[1]["path"] != "/interface[name=ethernet-1/2]" {
		t.Fatalf("unexpected second command: %+v", cmds[1])
	}
	if cmds[2]["action"] != "delete" || cmds[2]["path"] != "/interface[name=lo0]" {
		t.Fatalf("unexpected third command: %+v", cmds[2])
	}
	if cmds[3]["action"] != "update" || cmds[3]["path"] != "/" {
		t.Fatalf("unexpected fourth command: %+v", cmds[3])
	}
}

func TestBuildCherryPickCommandsLeafListsAndMultiKeyAndUnixSocket(t *testing.T) {
	tmpDir := t.TempDir()
	backend := gitbackend.NewGitBackend(tmpDir)

	// Commit 1: Pre-bootstrap system config without unix-socket, with 2 NTP servers (leaf-list) and multi-key ACL
	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{
				"host-name": "srl-1",
			},
			"ntp": map[string]interface{}{
				"server": []interface{}{"10.0.0.1", "10.0.0.2"},
			},
		},
		"acl": map[string]interface{}{
			"acl-filter": []interface{}{
				map[string]interface{}{
					"name": "cpm",
					"type": "ipv4",
				},
				map[string]interface{}{
					"name": "mgmt",
					"type": "ipv6",
				},
			},
		},
	}

	// Commit 2: Remove 10.0.0.2 from leaf-list, remove cpm/ipv4 multi-key acl-filter while keeping mgmt/ipv6, and enable unix-socket
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{
				"host-name": "srl-2",
			},
			"ntp": map[string]interface{}{
				"server": []interface{}{"10.0.0.1"},
			},
			"json-rpc-server": map[string]interface{}{
				"admin-state": "enable",
				"unix-socket": map[string]interface{}{
					"admin-state": "enable",
				},
			},
		},
		"acl": map[string]interface{}{
			"acl-filter": []interface{}{
				map[string]interface{}{
					"name": "mgmt",
					"type": "ipv6",
				},
			},
		},
	}

	_, sha1, err := backend.RecordConfigChange(cfg1, "admin", "1", "", time.Now().UTC(), "Commit 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha2, err := backend.RecordConfigChange(cfg2, "admin", "2", "", time.Now().UTC(), "Commit 2", nil)
	if err != nil {
		t.Fatal(err)
	}

	restorer := NewConfigRestorer(nil, backend)
	restorer.SRLClient.IsOnBox = true

	// 1. Cherry-pick /system/ntp from sha2: must strip synthetic [idx_1] predicate and emit clean delete on /system/ntp/server
	cmds, err := restorer.BuildCherryPickCommands(sha2, "/system/ntp")
	if err != nil {
		t.Fatalf("BuildCherryPickCommands for /system/ntp failed: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands (delete leaf-list + update), got %d: %+v", len(cmds), cmds)
	}
	if cmds[0]["action"] != "delete" || cmds[0]["path"] != "/system/ntp/server" {
		t.Fatalf("expected clean leaf-list delete /system/ntp/server without [idx_N], got %+v", cmds[0])
	}

	// 2. Cherry-pick /acl from sha2: must emit multi-key delete /acl/acl-filter[name=cpm][type=ipv4] + update
	aclCmds, err := restorer.BuildCherryPickCommands(sha2, "/acl")
	if err != nil {
		t.Fatalf("BuildCherryPickCommands for /acl failed: %v", err)
	}
	if len(aclCmds) != 2 || aclCmds[0]["action"] != "delete" || aclCmds[0]["path"] != "/acl/acl-filter[name=cpm][type=ipv4]" {
		t.Fatalf("expected multi-key delete /acl/acl-filter[name=cpm][type=ipv4], got %+v", aclCmds)
	}
	aclCLI, err := restorer.BuildCherryPickCLIStatements(sha2, "/acl/acl-filter[name=cpm][type=ipv4]")
	if err != nil {
		t.Fatalf("BuildCherryPickCLIStatements for /acl failed: %v", err)
	}
	wantACLCLI := []string{"delete / acl acl-filter cpm type ipv4"}
	if !reflect.DeepEqual(aclCLI, wantACLCLI) {
		t.Fatalf("got %v, want %v", aclCLI, wantACLCLI)
	}

	// 3. Cherry-pick /system from sha1 (where unix-socket was absent) while IsOnBox == true:
	// must NOT emit a delete on /system/json-rpc-server/unix-socket and must preserve unix-socket in update payload
	sysCmds, err := restorer.BuildCherryPickCommands(sha1, "/system")
	if err != nil {
		t.Fatalf("BuildCherryPickCommands for /system from sha1 failed: %v", err)
	}
	for _, cmd := range sysCmds {
		if cmd["action"] == "delete" {
			pathStr, _ := cmd["path"].(string)
			if pathStr == "/system/json-rpc-server" || pathStr == "/system/json-rpc-server/unix-socket" {
				t.Fatalf("on-box cherry-pick must not delete %s", pathStr)
			}
		}
		if cmd["action"] == "update" {
			valMap, _ := cmd["value"].(map[string]interface{})
			sysMap, _ := valMap["system"].(map[string]interface{})
			jrpc, _ := sysMap["json-rpc-server"].(map[string]interface{})
			usock, _ := jrpc["unix-socket"].(map[string]interface{})
			if usock["admin-state"] != "enable" {
				t.Fatalf("expected unix-socket admin-state=enable to be preserved on-box, got %+v", sysMap)
			}
		}
	}
}
