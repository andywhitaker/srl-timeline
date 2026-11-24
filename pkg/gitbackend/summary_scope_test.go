package gitbackend

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"timeline/pkg/differ"
	"timeline/pkg/models"
)

func TestComputeSummaryScope(t *testing.T) {
	t.Run("empty diff returns empty string", func(t *testing.T) {
		if got := ComputeSummaryScope(models.SemanticDiffResult{}); got != "" {
			t.Fatalf("expected empty scope, got %q", got)
		}
	})

	t.Run("deduplicates parent and child paths within one interface", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/interface[name=ethernet-1/1]", DiffType: models.DiffAdded},
				{Path: "/interface[name=ethernet-1/1]/admin-state", DiffType: models.DiffAdded},
				{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffAdded},
				{Path: "/interface[name=ethernet-1/1]/subinterface[index=0]", DiffType: models.DiffAdded},
				{Path: "/interface[name=ethernet-1/1]/subinterface[index=0]/ipv4/address[ip-prefix=10.0.0.1/30]", DiffType: models.DiffAdded},
			},
		}
		got := ComputeSummaryScope(res)
		if got != "interface ethernet-1/1" {
			t.Fatalf("expected %q, got %q", "interface ethernet-1/1", got)
		}
	})

	t.Run("network-instance protocol grouping and acl", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/network-instance[name=default]", DiffType: models.DiffAdded},
				{Path: "/network-instance[name=default]/protocols", DiffType: models.DiffAdded},
				{Path: "/network-instance[name=default]/protocols/bgp/autonomous-system", DiffType: models.DiffAdded},
				{Path: "/network-instance[name=default]/protocols/bgp/neighbor[peer-address=10.0.0.2]/admin-state", DiffType: models.DiffAdded},
				{Path: "/network-instance[name=default]/type", DiffType: models.DiffAdded},
				{Path: "/acl/cpm-filter/ipv4-filter/entry[sequence-id=10]/match/protocol", DiffType: models.DiffModified},
			},
		}
		got := ComputeSummaryScope(res)
		want := "network-instance default (bgp), acl cpm-filter"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("network-instance merges multiple protocols without duplicating instance name", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/network-instance[name=default]/protocols/bgp/router-id", DiffType: models.DiffModified},
				{Path: "/network-instance[name=default]/protocols/bgp/autonomous-system", DiffType: models.DiffModified},
				{Path: "/network-instance[name=default]/protocols/ospf/instance[name=main]/admin-state", DiffType: models.DiffAdded},
			},
		}
		got := ComputeSummaryScope(res)
		want := "network-instance default (bgp, ospf)"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("strips YANG module prefixes on un-normalized paths", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/srl_nokia-interfaces:interface[name=system0]/subinterface[index=0]/ipv4/address[ip-prefix=1.1.1.1/32]", DiffType: models.DiffAdded},
				{Path: "/srl_nokia-system:system/srl_nokia-system-name:name/host-name", DiffType: models.DiffModified},
			},
		}
		got := ComputeSummaryScope(res)
		want := "interface system0, system name"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("collapses multiple system subcontainers into system", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/system/name/host-name", DiffType: models.DiffModified},
				{Path: "/system/banner/login-banner", DiffType: models.DiffModified},
				{Path: "/system/ntp/server[address=192.0.2.1]/iburst", DiffType: models.DiffAdded},
			},
		}
		got := ComputeSummaryScope(res)
		if got != "system" {
			t.Fatalf("expected %q, got %q", "system", got)
		}
	})

	t.Run("ranks by impact and truncates to max 3 items and <= 60 chars with +N more", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/acl/ipv4-filter[name=edge-in]/entry[sequence-id=10]/action", DiffType: models.DiffAdded},
				{Path: "/interface[name=ethernet-1/1]/admin-state", DiffType: models.DiffModified},
				{Path: "/interface[name=ethernet-1/1]/description", DiffType: models.DiffModified},
				{Path: "/interface[name=ethernet-1/1]/mtu", DiffType: models.DiffModified},
				{Path: "/interface[name=ethernet-1/2]/admin-state", DiffType: models.DiffModified},
				{Path: "/interface[name=ethernet-1/2]/description", DiffType: models.DiffModified},
				{Path: "/interface[name=system0]/description", DiffType: models.DiffModified},
				{Path: "/routing-policy/policy[name=export-all]/default-action/policy-result", DiffType: models.DiffAdded},
				{Path: "/system/gnmi-server/admin-state", DiffType: models.DiffModified},
			},
		}
		got := ComputeSummaryScope(res)
		if len([]rune(got)) > maxSummaryScopeLen {
			t.Fatalf("summary scope exceeded max length %d (%d chars): %q", maxSummaryScopeLen, len([]rune(got)), got)
		}
		if !strings.HasPrefix(got, "interface ethernet-1/1, interface ethernet-1/2") {
			t.Fatalf("expected top affected interfaces first, got %q", got)
		}
		if !strings.Contains(got, "+") || !strings.HasSuffix(got, "more") {
			t.Fatalf("expected +N more truncation suffix, got %q", got)
		}
	})

	t.Run("caps at 3 items even when 4 short items would fit in 60 chars", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/acl/policers/p1", DiffType: models.DiffAdded},
				{Path: "/bfd/admin-state", DiffType: models.DiffAdded},
				{Path: "/qos/admin-state", DiffType: models.DiffAdded},
				{Path: "/snmp/admin-state", DiffType: models.DiffAdded},
			},
		}
		got := ComputeSummaryScope(res)
		want := "acl, bfd, qos, +1 more"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("single overly long scope label is truncated within 60 chars", func(t *testing.T) {
		res := models.SemanticDiffResult{
			HasChanges: true,
			Changes: []models.PathChange{
				{Path: "/network-instance[name=customer-production-backbone-vrf-us-east-01-primary]/protocols/bgp/router-id", DiffType: models.DiffAdded},
			},
		}
		got := ComputeSummaryScope(res)
		if len([]rune(got)) > maxSummaryScopeLen {
			t.Fatalf("expected length <= %d, got %d: %q", maxSummaryScopeLen, len([]rune(got)), got)
		}
		if !strings.HasSuffix(got, "...") {
			t.Fatalf("expected ellipsis truncation, got %q", got)
		}
	})

	t.Run("integrates cleanly with differ.SemanticDiff output", func(t *testing.T) {
		oldCfg := map[string]interface{}{
			"system": map[string]interface{}{
				"name": map[string]interface{}{"host-name": "leaf1"},
			},
		}
		newCfg := map[string]interface{}{
			"system": map[string]interface{}{
				"name": map[string]interface{}{"host-name": "leaf1"},
			},
			"interface": []interface{}{
				map[string]interface{}{
					"name":        "ethernet-1/1",
					"admin-state": "enable",
					"description": "uplink-spine1",
				},
			},
			"network-instance": []interface{}{
				map[string]interface{}{
					"name": "default",
					"protocols": map[string]interface{}{
						"bgp": map[string]interface{}{
							"autonomous-system": 65001,
							"router-id":         "10.0.0.1",
						},
					},
				},
			},
		}
		diffRes := differ.SemanticDiff(oldCfg, newCfg, "")
		got := ComputeSummaryScope(diffRes)
		if !strings.Contains(got, "interface ethernet-1/1") || !strings.Contains(got, "network-instance default (bgp)") {
			t.Fatalf("unexpected diff-derived summary scope: %q", got)
		}
		if len([]rune(got)) > maxSummaryScopeLen {
			t.Fatalf("summary scope too long (%d): %q", len([]rune(got)), got)
		}
	})
}

func TestSummaryScopePersistenceAndTimelineLoad(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repo")
	backend := NewGitBackend(repoDir)

	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf1"},
		},
	}
	rec1, sha1, err := backend.RecordConfigChange(cfg1, "admin", "1", "sess-1", time.Now().UTC().Add(-time.Minute), "Initial baseline", map[string]interface{}{"is_startup": true})
	if err != nil || !rec1 || sha1 == "" {
		t.Fatalf("commit 1 failed: rec=%v sha=%q err=%v", rec1, sha1, err)
	}

	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf1"},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "to-spine-1",
			},
		},
		"acl": map[string]interface{}{
			"cpm-filter": map[string]interface{}{
				"ipv4-filter": map[string]interface{}{
					"entry": []interface{}{
						map[string]interface{}{"sequence-id": 10, "description": "allow-bgp"},
					},
				},
			},
		},
	}
	rec2, sha2, err := backend.RecordConfigChange(cfg2, "netops", "2", "sess-2", time.Now().UTC(), "Add uplink and cpm acl", nil)
	if err != nil || !rec2 || sha2 == "" {
		t.Fatalf("commit 2 failed: rec=%v sha=%q err=%v", rec2, sha2, err)
	}

	// Verify metadata.json persisted summary_scope in git history
	meta2 := backend.GetCommitMetadata(sha2)
	persisted2, _ := meta2["summary_scope"].(string)
	if persisted2 == "" {
		t.Fatalf("expected summary_scope persisted in metadata.json for commit 2, got meta=%+v", meta2)
	}
	if !strings.Contains(persisted2, "interface ethernet-1/1") || !strings.Contains(persisted2, "acl cpm-filter") {
		t.Fatalf("unexpected persisted summary_scope for commit 2: %q", persisted2)
	}

	meta1 := backend.GetCommitMetadata(sha1)
	persisted1, _ := meta1["summary_scope"].(string)
	if persisted1 != "system name" {
		t.Fatalf("expected persisted summary_scope %q for commit 1, got %q", "system name", persisted1)
	}

	// Fresh backend instance reads summary_scope from metadata.json via GetTimeline
	reloaded := NewGitBackend(repoDir)
	commits := reloaded.GetTimeline(10, "")
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].SummaryScope != persisted2 {
		t.Fatalf("commit 0 SummaryScope mismatch: got %q, want %q", commits[0].SummaryScope, persisted2)
	}
	if commits[1].SummaryScope != persisted1 {
		t.Fatalf("commit 1 SummaryScope mismatch: got %q, want %q", commits[1].SummaryScope, persisted1)
	}

	// Verify JSON serialization of models.TimelineCommit includes summary_scope
	raw, err := json.Marshal(commits[0])
	if err != nil {
		t.Fatalf("failed to marshal TimelineCommit: %v", err)
	}
	if !strings.Contains(string(raw), `"summary_scope":`) {
		t.Fatalf("expected summary_scope JSON key in marshaled TimelineCommit: %s", string(raw))
	}
}

func TestGetCommitSummaryScopeLazyBackfillForLegacyCommits(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "legacy_repo")
	backend := NewGitBackend(repoDir)

	// Helper to create legacy commits directly via git without summary_scope in metadata.json
	commitLegacy := func(cfg map[string]interface{}, author, msg string) string {
		t.Helper()
		cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		metaBytes, err := json.MarshalIndent(map[string]interface{}{
			"author":       author,
			"diff_summary": "+1",
		}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "config.json"), cfgBytes, 0664); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "config.cli"), []byte("# legacy cli\n"), 0664); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "metadata.json"), metaBytes, 0664); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"-C", repoDir, "add", "config.json", "config.cli", "metadata.json"},
			{"-C", repoDir, "commit", "-m", msg, "--author", author + " <" + author + "@srl-timeline>"},
		} {
			cmd := exec.Command("git", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v failed: %s (%v)", args, string(out), err)
			}
		}
		out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}

	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "legacy-srl"},
		},
	}
	legacySHA1 := commitLegacy(cfg1, "alice", "Legacy initial commit")

	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "legacy-srl"},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"ospf": map[string]interface{}{
						"instance": []interface{}{
							map[string]interface{}{"name": "main", "admin-state": "enable"},
						},
					},
				},
			},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "system0", "admin-state": "enable"},
		},
	}
	legacySHA2 := commitLegacy(cfg2, "bob", "Legacy OSPF and system0")

	// GetTimeline should NOT block or compute diffs for legacy commits without summary_scope
	commits := backend.GetTimeline(10, "")
	if len(commits) != 2 {
		t.Fatalf("expected 2 legacy commits, got %d", len(commits))
	}
	if commits[0].SummaryScope != "" || commits[1].SummaryScope != "" {
		t.Fatalf("expected empty SummaryScope on legacy commits prior to lazy backfill, got %q and %q", commits[0].SummaryScope, commits[1].SummaryScope)
	}

	// Lazily backfill both commits concurrently to verify thread-safety
	var wg sync.WaitGroup
	results1 := make([]string, 8)
	results2 := make([]string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			results1[idx] = backend.GetCommitSummaryScope(legacySHA1)
		}(i)
		go func(idx int) {
			defer wg.Done()
			results2[idx] = backend.GetCommitSummaryScope(legacySHA2)
		}(i)
	}
	wg.Wait()

	for i, s := range results1 {
		if s != "system name" {
			t.Fatalf("goroutine %d root legacy commit scope = %q, want %q", i, s, "system name")
		}
	}
	for i, s := range results2 {
		if !strings.Contains(s, "network-instance default (ospf)") || !strings.Contains(s, "interface system0") {
			t.Fatalf("goroutine %d legacy commit 2 scope = %q, expected ospf + system0", i, s)
		}
	}

	// Verify short SHA resolution also hits cache/returns same scope
	if shortScope := backend.GetCommitSummaryScope(legacySHA2[:8]); shortScope != results2[0] {
		t.Fatalf("short SHA lookup mismatch: got %q, want %q", shortScope, results2[0])
	}

	// Verify timeline cache was enriched after lazy backfill
	refreshed := backend.GetTimeline(10, "")
	if refreshed[0].SummaryScope != results2[0] || refreshed[1].SummaryScope != results1[0] {
		t.Fatalf("expected refreshed timeline cache to reflect backfilled scopes, got %+v", refreshed)
	}

	// Verify invalid/unknown commit returns empty string cleanly
	if bad := backend.GetCommitSummaryScope("0000000000000000000000000000000000000000"); bad != "" {
		t.Fatalf("expected empty scope for nonexistent commit, got %q", bad)
	}
	if bad := backend.GetCommitSummaryScope("not-a-valid-ref"); bad != "" {
		t.Fatalf("expected empty scope for invalid ref, got %q", bad)
	}
}
