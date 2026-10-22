package blame

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"timeline/pkg/gitbackend"
)

func TestBlameEngine(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "blame_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable"},
		},
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-node"},
		},
	}
	_, _, err = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial", nil)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewBlameEngine(backend)
	entries := engine.GetBlameLines("cli", "")
	if len(entries) == 0 {
		t.Fatalf("expected blame lines")
	}

	stats := engine.GetContributorStats("cli")
	if stats.TotalLines == 0 {
		t.Fatalf("expected positive total lines")
	}
	if stat, ok := stats.Authors["admin"]; !ok || stat.Count == 0 {
		t.Fatalf("expected author 'admin' with lines")
	}
}

func TestBlameAtCommitAndSubsystemClassification(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg1 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "uplink for bgp and acl testing",
			},
		},
	}
	cfg2 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "updated by bob",
			},
		},
	}

	_, sha1, err := backend.RecordConfigChange(cfg1, "alice", "1", "", time.Now().UTC(), "Commit 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, sha2, err := backend.RecordConfigChange(cfg2, "bob", "2", "", time.Now().UTC(), "Commit 2", nil)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewBlameEngine(backend)

	// Blame at sha1 should attribute to alice, while sha2 attributes to bob
	stats1 := engine.GetContributorStatsAtCommit("cli", sha1)
	if _, ok := stats1.Authors["alice"]; !ok {
		t.Fatalf("expected alice in stats at sha1, got %+v", stats1.Authors)
	}
	if _, ok := stats1.Authors["bob"]; ok {
		t.Fatalf("did not expect bob in stats at historical commit sha1")
	}
	// Subsystem must be interfaces even though description mentions bgp and acl
	if stats1.Subsystems["interfaces"]["alice"] == 0 {
		t.Fatalf("expected interfaces subsystem for /interface line mentioning bgp/acl, got %+v", stats1.Subsystems)
	}

	stats2 := engine.GetContributorStatsAtCommit("cli", sha2)
	if _, ok := stats2.Authors["bob"]; !ok {
		t.Fatalf("expected bob in stats at sha2, got %+v", stats2.Authors)
	}
}

