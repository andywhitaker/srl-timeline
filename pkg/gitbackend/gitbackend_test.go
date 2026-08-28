package gitbackend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"timeline/pkg/models"
)

func TestGitBackendRecordAndTimeline(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gitbackend_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := NewGitBackend(repoDir)

	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "node-1"},
		},
	}
	rec, sha1, err := backend.RecordConfigChange(cfg1, "alice", "1", "", time.Now().UTC(), "Initial commit", nil)
	if err != nil || !rec || sha1 == "" {
		t.Fatalf("failed to record commit 1: %v", err)
	}

	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "node-1"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable"},
		},
	}
	rec, sha2, err := backend.RecordConfigChange(cfg2, "bob", "2", "", time.Now().UTC(), "Add interface", nil)
	if err != nil || !rec || sha2 == "" {
		t.Fatalf("failed to record commit 2: %v", err)
	}

	commits := backend.GetTimeline(10, "")
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Author != "bob" {
		t.Fatalf("expected latest author bob, got %s", commits[0].Author)
	}

	// Test filter
	filtered := backend.GetTimeline(10, "ethernet-1/1")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered commit for ethernet-1/1, got %d", len(filtered))
	}

	// Test blame
	blameEntries := backend.GetBlameEntries("cli")
	if len(blameEntries) == 0 {
		t.Fatalf("expected blame entries")
	}

	// Test remote config
	rc := models.RemoteRepoConfig{
		URL:      "/tmp/mock_remote.git",
		Branch:   "main",
		AutoPush: true,
	}
	if err := backend.SaveRemoteConfig(rc); err != nil {
		t.Fatalf("failed to save remote config: %v", err)
	}
	loaded := backend.LoadRemoteConfig()
	if loaded.URL != rc.URL || !loaded.AutoPush {
		t.Fatalf("remote config mismatch: %+v", loaded)
	}
}
