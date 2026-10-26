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

func TestTimelineCachePreservesStartupAndRestoredFlags(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	backend1 := NewGitBackend(repoDir)

	cfg1 := map[string]interface{}{"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl1"}}}
	cfg2 := map[string]interface{}{"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl2"}}}

	_, _, err := backend1.RecordConfigChange(cfg1, "system", "startup", "", time.Now().UTC(), "Startup baseline", map[string]interface{}{"is_startup": true})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = backend1.RecordConfigChange(cfg2, "admin", "10", "", time.Now().UTC(), "Restored revision", map[string]interface{}{"is_restored": true})
	if err != nil {
		t.Fatal(err)
	}

	// Open a fresh GitBackend instance to force reading from timeline_cache.json
	backend2 := NewGitBackend(repoDir)
	commits := backend2.GetTimeline(10, "")
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if !commits[0].IsRestored {
		t.Fatalf("expected latest commit IsRestored=true after cache reload, got %+v", commits[0])
	}
	if !commits[1].IsStartup {
		t.Fatalf("expected initial commit IsStartup=true after cache reload, got %+v", commits[1])
	}
}

func TestGitBackendNativeFallbackWithoutGitBinary(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")

	repoDir := filepath.Join(t.TempDir(), "native_repo")
	backend := NewGitBackend(repoDir)

	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "native-srl-1"},
		},
	}
	rec1, sha1, err := backend.RecordConfigChange(cfg1, "alice", "1", "", time.Now().UTC(), "Native commit 1", map[string]interface{}{"is_startup": true})
	if err != nil || !rec1 || len(sha1) != 40 {
		t.Fatalf("native commit 1 failed: rec=%v sha=%q err=%v", rec1, sha1, err)
	}

	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "native-srl-1"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Port 1 — Native"},
		},
	}
	rec2, sha2, err := backend.RecordConfigChange(cfg2, "bob", "2", "", time.Now().UTC(), "Native commit 2", nil)
	if err != nil || !rec2 || len(sha2) != 40 {
		t.Fatalf("native commit 2 failed: rec=%v sha=%q err=%v", rec2, sha2, err)
	}

	commits := backend.GetTimeline(10, "")
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits in native timeline, got %d", len(commits))
	}
	if commits[0].Author != "bob" || commits[1].Author != "alice" {
		t.Fatalf("unexpected commit authors: %+v", commits)
	}

	// Verify short 8-char SHA resolution and ~1 parent resolution
	shortSHA2 := sha2[:8]
	gotCfg2 := backend.GetConfigAtCommit(shortSHA2)
	if len(gotCfg2) == 0 {
		t.Fatalf("failed to load config at short SHA %s", shortSHA2)
	}
	gotParentCfg := backend.GetConfigAtCommit(shortSHA2 + "~1")
	if len(gotParentCfg) == 0 {
		t.Fatalf("failed to load parent config at %s~1", shortSHA2)
	}
	if badCfg := backend.GetConfigAtCommit("HEAD~invalid"); len(badCfg) != 0 {
		t.Fatalf("expected empty config for invalid revision HEAD~invalid, got %+v", badCfg)
	}

	blameEntries := backend.GetBlameEntriesAtCommit("cli", shortSHA2)
	if len(blameEntries) == 0 {
		t.Fatalf("expected native blame entries, got 0")
	}

	// Restore PATH and verify standard git CLI can read the loose objects written by native_git.go
	t.Setenv("PATH", origPath)
	backendWithGit := NewGitBackend(repoDir)
	gitCommits := backendWithGit.GetTimeline(10, "")
	if len(gitCommits) != 2 || gitCommits[0].FullSHA != sha2 || gitCommits[1].FullSHA != sha1 {
		t.Fatalf("standard git CLI failed to read native commits: %+v", gitCommits)
	}
}
