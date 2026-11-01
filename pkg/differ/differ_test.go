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

func TestUnifiedDiffBracketAlignmentOnAddedListElementBeforeExisting(t *testing.T) {
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
	}
	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"description": "This is a test",
				"name":        "ethernet-1/1",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
	}

	res := SemanticDiff(oldCfg, newCfg, "")
	joined := strings.Join(res.UnifiedDiffLines, "\n")
	wantBlock := strings.Join([]string{
		"    \"interface\": [",
		"+     {",
		"+       \"description\": \"This is a test\",",
		"+       \"name\": \"ethernet-1/1\"",
		"+     },",
		"      {",
		"        \"admin-state\": \"enable\",",
	}, "\n")
	if !strings.Contains(joined, wantBlock) {
		t.Fatalf("expected added interface block to highlight its own enclosing brackets:\nwant:\n%s\n\ngot:\n%s", wantBlock, joined)
	}
}

func TestUnifiedDiffBracketAlignmentOnAddedNestedListElement(t *testing.T) {
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"description": "This is a test",
				"name":        "ethernet-1/1",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
	}
	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"description": "This is a test",
				"name":        "ethernet-1/1",
			},
			map[string]interface{}{
				"name": "ethernet-1/2",
				"subinterface": []interface{}{
					map[string]interface{}{
						"index": 0,
						"ipv4": map[string]interface{}{
							"address": []interface{}{
								map[string]interface{}{
									"ip-prefix": "10.1.2.1/24",
								},
							},
							"admin-state": "enable",
						},
					},
				},
			},
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
	}

	res := SemanticDiff(oldCfg, newCfg, "")
	joined := strings.Join(res.UnifiedDiffLines, "\n")
	if strings.Contains(joined, "+     },\n+     {") {
		t.Fatalf("expected diff not to highlight closing-then-opening brackets across adjacent items:\n%s", joined)
	}
	wantPrefix := strings.Join([]string{
		"        \"name\": \"ethernet-1/1\"",
		"      },",
		"+     {",
		"+       \"name\": \"ethernet-1/2\",",
	}, "\n")
	wantSuffix := strings.Join([]string{
		"+       ]",
		"+     },",
		"      {",
		"        \"admin-state\": \"enable\",",
	}, "\n")
	if !strings.Contains(joined, wantPrefix) || !strings.Contains(joined, wantSuffix) {
		t.Fatalf("unexpected bracket highlighting around nested added element:\n%s", joined)
	}
}

func TestUnifiedDiffBracketAlignmentOnAddedAndDeletedListElementAtEnd(t *testing.T) {
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "ethernet-1/1",
			},
		},
	}
	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "ethernet-1/1",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "ethernet-1/2",
			},
		},
	}

	resAdd := SemanticDiff(oldCfg, newCfg, "")
	joinedAdd := strings.Join(resAdd.UnifiedDiffLines, "\n")
	wantAdd := strings.Join([]string{
		"        \"name\": \"ethernet-1/1\"",
		"      },",
		"+     {",
		"+       \"admin-state\": \"enable\",",
		"+       \"name\": \"ethernet-1/2\"",
		"+     }",
		"    ]",
	}, "\n")
	if !strings.Contains(joinedAdd, wantAdd) {
		t.Fatalf("expected trailing list addition to highlight enclosing brackets cleanly:\nwant:\n%s\n\ngot:\n%s", wantAdd, joinedAdd)
	}

	resDel := SemanticDiff(newCfg, oldCfg, "")
	joinedDel := strings.Join(resDel.UnifiedDiffLines, "\n")
	wantDel := strings.Join([]string{
		"        \"name\": \"ethernet-1/1\"",
		"      }",
		"-     {",
		"-       \"admin-state\": \"enable\",",
		"-       \"name\": \"ethernet-1/2\"",
		"-     }",
		"    ]",
	}, "\n")
	if !strings.Contains(joinedDel, wantDel) {
		t.Fatalf("expected trailing list deletion to highlight enclosing brackets cleanly:\nwant:\n%s\n\ngot:\n%s", wantDel, joinedDel)
	}
}

func TestUnifiedDiffBracketAlignmentOnSiblingContainers(t *testing.T) {
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name": "ethernet-1/1",
				"subinterface": []interface{}{
					map[string]interface{}{
						"index": 0,
						"ipv4": map[string]interface{}{
							"admin-state": "enable",
						},
					},
				},
			},
		},
	}
	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name": "ethernet-1/1",
				"subinterface": []interface{}{
					map[string]interface{}{
						"index": 0,
						"ipv4": map[string]interface{}{
							"admin-state": "enable",
						},
						"ipv6": map[string]interface{}{
							"admin-state": "enable",
						},
					},
				},
			},
		},
	}

	res := SemanticDiff(oldCfg, newCfg, "")
	joined := strings.Join(res.UnifiedDiffLines, "\n")
	want := strings.Join([]string{
		"              \"admin-state\": \"enable\"",
		"            },",
		"+           \"ipv6\": {",
		"+             \"admin-state\": \"enable\"",
		"+           }",
		"          }",
	}, "\n")
	if !strings.Contains(joined, want) {
		t.Fatalf("expected added ipv6 sibling container to highlight its own brackets without stealing ipv4 closing brace:\nwant:\n%s\n\ngot:\n%s", want, joined)
	}
}

func TestUnifiedDiffBracketAlignmentOnCustomKeyArrayWithAdjacentModification(t *testing.T) {
	oldCfg := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"code":        100,
				"description": "first",
			},
			map[string]interface{}{
				"code":        200,
				"description": "second",
			},
		},
	}
	newCfg := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"code":        100,
				"description": "updated",
			},
			map[string]interface{}{
				"code":        150,
				"description": "middle",
			},
			map[string]interface{}{
				"code":        200,
				"description": "second",
			},
		},
	}

	res := SemanticDiff(oldCfg, newCfg, "")
	joined := strings.Join(res.UnifiedDiffLines, "\n")
	want := strings.Join([]string{
		"        \"code\": 100,",
		"-       \"description\": \"first\"",
		"+       \"description\": \"updated\"",
		"      },",
		"+     {",
		"+       \"code\": 150,",
		"+       \"description\": \"middle\"",
		"+     },",
		"      {",
		"        \"code\": 200,",
	}, "\n")
	if !strings.Contains(joined, want) {
		t.Fatalf("expected custom-keyed array with adjacent modification and insertion to align brackets cleanly:\nwant:\n%s\n\ngot:\n%s", want, joined)
	}
}

func TestUnifiedDiffBracketAlignmentOnSliderDeleteAndAdjacentModify(t *testing.T) {
	// Directly exercise compactDiffSliders when a deletion hunk slides right across the end of an array
	chunks := []diffChunk{
		{op: diffEqual, line: "    {"},
		{op: diffEqual, line: "      \"val\": 2"},
		{op: diffDelete, line: "    },"},
		{op: diffDelete, line: "    {"},
		{op: diffDelete, line: "      \"val\": 3"},
		{op: diffEqual, line: "    }"},
		{op: diffEqual, line: "  ]"},
	}
	slid := compactDiffSliders(chunks)
	if slid[2].op != diffEqual || slid[2].line != "    }" {
		t.Fatalf("expected slid[2] to be diffEqual with comma-free '    }', got op=%v line=%q", slid[2].op, slid[2].line)
	}
	if slid[3].op != diffDelete || slid[3].line != "    {" || slid[5].op != diffDelete || slid[5].line != "    }" {
		t.Fatalf("expected slid[3..5] to be deleted '{ ... }', got %+v", slid[3:6])
	}

	// Also exercise compactDiffSliders when a modification is immediately followed by an inserted block with a stolen closing brace
	modPlusInsert := []diffChunk{
		{op: diffEqual, line: "    {"},
		{op: diffDelete, line: "      \"description\": \"first\""},
		{op: diffInsert, line: "      \"description\": \"updated\""},
		{op: diffInsert, line: "    },"},
		{op: diffInsert, line: "    {"},
		{op: diffInsert, line: "      \"description\": \"middle\""},
		{op: diffEqual, line: "    },"},
	}
	slidMod := compactDiffSliders(modPlusInsert)
	if slidMod[3].op != diffEqual || slidMod[3].line != "    }," {
		t.Fatalf("expected slidMod[3] to become diffEqual '    },', got op=%v line=%q", slidMod[3].op, slidMod[3].line)
	}
	if slidMod[4].op != diffInsert || slidMod[4].line != "    {" || slidMod[6].op != diffInsert || slidMod[6].line != "    }," {
		t.Fatalf("expected slidMod[4..6] to be inserted '{ ... },', got %+v", slidMod[4:7])
	}
}

func TestUnifiedDiffBracketAlignmentOnSimultaneousAddDeleteModify(t *testing.T) {
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"admin-state": "enable",
				"description": "old-1",
				"name":        "ethernet-1/1",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"description": "to-delete",
				"name":        "ethernet-1/2",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
	}
	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"admin-state": "enable",
				"description": "new-1",
				"name":        "ethernet-1/1",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"description": "added-3",
				"name":        "ethernet-1/3",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
	}

	res := SemanticDiff(oldCfg, newCfg, "")
	joined := strings.Join(res.UnifiedDiffLines, "\n")
	want := strings.Join([]string{
		"        \"admin-state\": \"enable\",",
		"-       \"description\": \"old-1\",",
		"+       \"description\": \"new-1\",",
		"        \"name\": \"ethernet-1/1\"",
		"      },",
		"-     {",
		"-       \"admin-state\": \"enable\",",
		"-       \"description\": \"to-delete\",",
		"-       \"name\": \"ethernet-1/2\"",
		"-     },",
		"+     {",
		"+       \"admin-state\": \"enable\",",
		"+       \"description\": \"added-3\",",
		"+       \"name\": \"ethernet-1/3\"",
		"+     },",
		"      {",
		"        \"admin-state\": \"enable\",",
	}, "\n")
	if !strings.Contains(joined, want) {
		t.Fatalf("expected simultaneous modify, delete, and add to align all brackets properly:\nwant:\n%s\n\ngot:\n%s", want, joined)
	}
}

func TestUnifiedDiffBracketAlignmentOnLargeJSONMyersPath(t *testing.T) {
	// Build two configs whose midOld * midNew exceeds 250,000 lines (~550 lines each between first and last change)
	var oldIfaces, newIfaces []interface{}
	oldIfaces = append(oldIfaces, map[string]interface{}{
		"admin-state": "enable",
		"description": "first-old",
		"name":        "ethernet-1/01",
	})
	newIfaces = append(newIfaces, map[string]interface{}{
		"admin-state": "enable",
		"description": "first-new",
		"name":        "ethernet-1/01",
	})
	newIfaces = append(newIfaces, map[string]interface{}{
		"admin-state": "enable",
		"description": "inserted-early",
		"name":        "ethernet-1/01a",
	})

	for i := 2; i <= 120; i++ {
		item := map[string]interface{}{
			"admin-state": "enable",
			"description": fmt.Sprintf("port-%03d", i),
			"mtu":         9212,
			"name":        fmt.Sprintf("ethernet-1/%03d", i),
		}
		oldIfaces = append(oldIfaces, item)
		newIfaces = append(newIfaces, item)
	}

	oldIfaces = append(oldIfaces, map[string]interface{}{
		"admin-state": "enable",
		"description": "last-old",
		"name":        "mgmt0",
	})
	newIfaces = append(newIfaces, map[string]interface{}{
		"admin-state": "enable",
		"description": "inserted-before-mgmt0",
		"name":        "ethernet-1/999",
	})
	newIfaces = append(newIfaces, map[string]interface{}{
		"admin-state": "enable",
		"description": "last-new",
		"name":        "mgmt0",
	})

	oldCfg := map[string]interface{}{"interface": oldIfaces}
	newCfg := map[string]interface{}{"interface": newIfaces}

	res := SemanticDiff(oldCfg, newCfg, "")
	joined := strings.Join(res.UnifiedDiffLines, "\n")
	wantEarly := strings.Join([]string{
		"+     {",
		"+       \"admin-state\": \"enable\",",
		"+       \"description\": \"inserted-early\",",
		"+       \"name\": \"ethernet-1/01a\"",
		"+     },",
	}, "\n")
	wantLate := strings.Join([]string{
		"+     {",
		"+       \"admin-state\": \"enable\",",
		"+       \"description\": \"inserted-before-mgmt0\",",
		"+       \"name\": \"ethernet-1/999\"",
		"+     },",
		"      {",
		"        \"admin-state\": \"enable\",",
		"-       \"description\": \"last-old\",",
		"+       \"description\": \"last-new\",",
	}, "\n")
	if !strings.Contains(joined, wantEarly) || !strings.Contains(joined, wantLate) {
		t.Fatalf("expected large Myers JSON diff to align brackets cleanly:\n%s", joined)
	}
}

