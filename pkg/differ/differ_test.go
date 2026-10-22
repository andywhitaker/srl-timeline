package differ

import (
	"fmt"
	"strings"
	"testing"
)

func TestSemanticDiffZeroChangeOnReorderedDict(t *testing.T) {
	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl"},
			"banner": map[string]interface{}{"login-banner": "Welcome"},
		},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"banner": map[string]interface{}{"login-banner": "Welcome"},
			"name": map[string]interface{}{"host-name": "srl"},
		},
	}

	res := SemanticDiff(cfg1, cfg2, "")
	if res.HasChanges {
		t.Fatalf("expected zero changes for reordered map, got %d", len(res.Changes))
	}
}

func TestSemanticDiffTrueChange(t *testing.T) {
	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-old"},
		},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-new"},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
			},
		},
	}

	res := SemanticDiff(cfg1, cfg2, "")
	if !res.HasChanges {
		t.Fatalf("expected changes")
	}
	if res.AddedCount != 1 {
		t.Fatalf("expected 1 added, got %d", res.AddedCount)
	}
	if res.ModifiedCount != 1 {
		t.Fatalf("expected 1 modified, got %d", res.ModifiedCount)
	}
}

func TestSemanticDiffScopedFilter(t *testing.T) {
	cfg1 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-old"}},
	}
	cfg2 := map[string]interface{}{
		"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "srl-new"}},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable"},
		},
	}

	res := SemanticDiff(cfg1, cfg2, "interface")
	if !res.HasChanges {
		t.Fatalf("expected changes for interface")
	}
	if res.ModifiedCount != 0 {
		t.Fatalf("expected 0 modified (system was filtered out), got %d", res.ModifiedCount)
	}
	if res.AddedCount == 0 {
		t.Fatalf("expected added elements (interface), got %d", res.AddedCount)
	}
}

func TestSemanticDiffInterfaceDescriptionChange(t *testing.T) {
	cfg1 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "Original Description",
				"admin-state": "enable",
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"description": "Updated Description",
				"admin-state": "enable",
			},
		},
	}
	cfg2 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "Updated Description",
				"admin-state": "enable",
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"description": "Updated Description",
				"admin-state": "enable",
			},
		},
	}

	res := SemanticDiff(cfg1, cfg2, "")
	if !res.HasChanges {
		t.Fatalf("expected changes when description updated")
	}

	// Verify unified diff contains both deleted old description and added new description
	var hasDel, hasAdd bool
	for _, l := range res.UnifiedDiffLines {
		if strings.Contains(l, "-") && strings.Contains(l, "Original Description") {
			hasDel = true
		}
		if strings.Contains(l, "+") && strings.Contains(l, "Updated Description") {
			hasAdd = true
		}
	}
	if !hasDel || !hasAdd {
		t.Fatalf("expected unified diff to show -Original and +Updated description, got:\n%v", res.UnifiedDiffLines)
	}
}

func TestSemanticDiffWithInterfaceFilter(t *testing.T) {
	cfg1 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "Original Description",
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"description": "Old Port 2",
			},
		},
	}
	cfg2 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "New Description",
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"description": "New Port 2",
			},
		},
	}

	res := SemanticDiff(cfg1, cfg2, "ethernet-1/1")
	if !res.HasChanges {
		t.Fatalf("expected changes for ethernet-1/1")
	}
	if len(res.Changes) != 1 {
		t.Fatalf("expected 1 change for ethernet-1/1, got %d: %+v", len(res.Changes), res.Changes)
	}
	if !strings.Contains(res.Changes[0].Path, "ethernet-1/1") {
		t.Fatalf("expected path to contain ethernet-1/1, got %s", res.Changes[0].Path)
	}

	var hasDel, hasAdd bool
	for _, l := range res.UnifiedDiffLines {
		if strings.Contains(l, "-") && strings.Contains(l, "Original Description") {
			hasDel = true
		}
		if strings.Contains(l, "+") && strings.Contains(l, "New Description") {
			hasAdd = true
		}
		if strings.Contains(l, "Port 2") {
			t.Fatalf("did not expect Port 2 to appear in filtered diff, got: %s", l)
		}
	}
	if !hasDel || !hasAdd {
		t.Fatalf("expected unified diff to show -Original and +New description for ethernet-1/1, got:\n%v", res.UnifiedDiffLines)
	}
}

func TestSemanticDiffCompoundKeyACLNoCollision(t *testing.T) {
	cfg1 := map[string]interface{}{
		"acl": map[string]interface{}{
			"acl-filter": []interface{}{
				map[string]interface{}{
					"name":        "cpm",
					"type":        "ipv4",
					"description": "IPv4 Old",
				},
				map[string]interface{}{
					"name":        "cpm",
					"type":        "ipv6",
					"description": "IPv6 Unchanged",
				},
			},
		},
	}
	cfg2 := map[string]interface{}{
		"acl": map[string]interface{}{
			"acl-filter": []interface{}{
				map[string]interface{}{
					"name":        "cpm",
					"type":        "ipv4",
					"description": "IPv4 New",
				},
				map[string]interface{}{
					"name":        "cpm",
					"type":        "ipv6",
					"description": "IPv6 Unchanged",
				},
			},
		},
	}

	res := SemanticDiff(cfg1, cfg2, "")
	if len(res.Changes) != 1 {
		t.Fatalf("expected exactly 1 change (ipv4 description only), got %d: %+v", len(res.Changes), res.Changes)
	}
	wantPath := "/acl/acl-filter[name=cpm][type=ipv4]/description"
	if res.Changes[0].Path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, res.Changes[0].Path)
	}
}

func TestComputeLCSDiffLargeSlice(t *testing.T) {
	const total = 4000
	a := make([]string, total)
	b := make([]string, total)
	for i := 0; i < total; i++ {
		line := fmt.Sprintf("set / interface ethernet-1/%d admin-state enable", i)
		a[i] = line
		b[i] = line
	}
	b[1500] = "set / interface ethernet-1/1500 admin-state disable"
	b[2500] = "set / interface ethernet-1/2500 admin-state disable"

	ops := computeLCSDiff(a, b)
	var dels, adds int
	for _, op := range ops {
		if op.op == diffDelete {
			dels++
		} else if op.op == diffInsert {
			adds++
		}
	}
	if dels != 2 || adds != 2 {
		t.Fatalf("expected 2 deletions and 2 additions in 4000-line diff, got dels=%d adds=%d", dels, adds)
	}
}

func TestSemanticDiffAddedAndDeletedTopLevelList(t *testing.T) {
	oldCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl"},
		},
	}
	newCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl"},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "Uplink",
			},
		},
	}

	resAdd := SemanticDiff(oldCfg, newCfg, "ethernet-1/1")
	if !resAdd.HasChanges || len(resAdd.Changes) == 0 {
		t.Fatalf("expected filtered changes for newly added top-level interface list, got %+v", resAdd.Changes)
	}
	for _, ch := range resAdd.Changes {
		if strings.HasSuffix(ch.Path, "/name") {
			t.Fatalf("consumed list key leaf /name must not be emitted as separate change: %+v", ch)
		}
	}

	resDel := SemanticDiff(newCfg, oldCfg, "ethernet-1/1")
	if !resDel.HasChanges || len(resDel.Changes) == 0 {
		t.Fatalf("expected filtered changes for deleted top-level interface list, got %+v", resDel.Changes)
	}
}
