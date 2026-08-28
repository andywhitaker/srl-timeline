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
