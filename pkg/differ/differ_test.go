package differ

import (
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
