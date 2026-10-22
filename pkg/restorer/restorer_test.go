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
}

