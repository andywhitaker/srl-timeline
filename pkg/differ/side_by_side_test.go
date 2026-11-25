package differ

import (
	"fmt"
	"strings"
	"testing"
)

func assertRowInvariants(t *testing.T, res SideBySideDiffResult) {
	t.Helper()
	var added, modified, deleted int
	prevLeft := 0
	prevRight := 0
	unfoldedCount := 0

	for idx, r := range res.Rows {
		switch r.Kind {
		case SideBySideEqual:
			if r.LeftLineNo <= 0 || r.RightLineNo <= 0 {
				t.Errorf("row %d (EQUAL): expected positive line numbers, got L=%d R=%d", idx, r.LeftLineNo, r.RightLineNo)
			}
			unfoldedCount++
		case SideBySideAdded:
			if r.LeftLineNo != 0 || r.LeftText != "" {
				t.Errorf("row %d (ADDED): expected empty left, got L=%d %q", idx, r.LeftLineNo, r.LeftText)
			}
			if r.RightLineNo <= 0 {
				t.Errorf("row %d (ADDED): expected positive RightLineNo, got %d", idx, r.RightLineNo)
			}
			added++
			unfoldedCount++
		case SideBySideDeleted:
			if r.RightLineNo != 0 || r.RightText != "" {
				t.Errorf("row %d (DELETED): expected empty right, got R=%d %q", idx, r.RightLineNo, r.RightText)
			}
			if r.LeftLineNo <= 0 {
				t.Errorf("row %d (DELETED): expected positive LeftLineNo, got %d", idx, r.LeftLineNo)
			}
			deleted++
			unfoldedCount++
		case SideBySideModified:
			if r.LeftLineNo <= 0 || r.RightLineNo <= 0 {
				t.Errorf("row %d (MODIFIED): expected positive line numbers, got L=%d R=%d", idx, r.LeftLineNo, r.RightLineNo)
			}
			modified++
			unfoldedCount++
		case SideBySideFold:
			if r.LeftLineNo != 0 || r.RightLineNo != 0 {
				t.Errorf("row %d (FOLD): expected 0 line numbers, got L=%d R=%d", idx, r.LeftLineNo, r.RightLineNo)
			}
			if r.FoldCount <= 2 {
				t.Errorf("row %d (FOLD): expected FoldCount > 2, got %d", idx, r.FoldCount)
			}
			unfoldedCount += r.FoldCount
		}

		// Clean raw text convention: no leading "+ " or "- " diff prefixes
		if strings.HasPrefix(r.LeftText, "- ") || strings.HasPrefix(r.LeftText, "+ ") {
			t.Errorf("row %d: LeftText must be clean raw text without +/- prefix, got %q", idx, r.LeftText)
		}
		if strings.HasPrefix(r.RightText, "- ") || strings.HasPrefix(r.RightText, "+ ") {
			t.Errorf("row %d: RightText must be clean raw text without +/- prefix, got %q", idx, r.RightText)
		}

		if r.LeftLineNo > 0 {
			if r.LeftLineNo <= prevLeft {
				t.Errorf("row %d: LeftLineNo not strictly increasing (%d <= %d)", idx, r.LeftLineNo, prevLeft)
			}
			prevLeft = r.LeftLineNo
		}
		if r.RightLineNo > 0 {
			if r.RightLineNo <= prevRight {
				t.Errorf("row %d: RightLineNo not strictly increasing (%d <= %d)", idx, r.RightLineNo, prevRight)
			}
			prevRight = r.RightLineNo
		}
	}

	if res.HasChanges {
		if added != res.AddedCount || modified != res.ModifiedCount || deleted != res.DeletedCount {
			t.Errorf("counts mismatch: rows=(+%d ~%d -%d) struct=(+%d ~%d -%d)",
				added, modified, deleted, res.AddedCount, res.ModifiedCount, res.DeletedCount)
		}
		if unfoldedCount != res.TotalLines {
			t.Errorf("TotalLines mismatch: unfolded=%d struct.TotalLines=%d", unfoldedCount, res.TotalLines)
		}
	}
}

func TestSideBySideDiffCLIModeBasicAndPairing(t *testing.T) {
	oldCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf-01"},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "Old Uplink",
				"mtu":         1500,
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"admin-state": "disable",
			},
		},
	}
	newCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "leaf-01"},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "New Uplink",
				"mtu":         9212,
				"vlan-tagging": true,
			},
		},
	}

	res := ComputeSideBySideDiff(oldCfg, newCfg, "cli", "", false, 3)
	assertRowInvariants(t, res)

	if res.Mode != "cli" {
		t.Fatalf("expected mode 'cli', got %q", res.Mode)
	}
	if !res.HasChanges {
		t.Fatalf("expected HasChanges=true")
	}
	// description + mtu changed -> 2 modified; vlan-tagging added -> 1 added; ethernet-1/2 deleted -> 1 deleted
	// Note: in flat CLI sorted order:
	//   ethernet-1/1 admin-state enable (equal)
	//   ethernet-1/1 description "Old Uplink" -> "New Uplink" (modified)
	//   ethernet-1/1 mtu 1500 -> 9212 (modified)
	//   ethernet-1/1 vlan-tagging true (added) / ethernet-1/2 admin-state disable (deleted) -> adjacent del/ins!
	// Because vlan-tagging (new) and ethernet-1/2 (old) are adjacent in sorted CLI lines without an equal line between them,
	// row-for-row hunk pairing pairs all 3 old lines (description, mtu, ethernet-1/2) with all 3 new lines (description, mtu, vlan-tagging).
	if res.ModifiedCount != 3 || res.AddedCount != 0 || res.DeletedCount != 0 {
		t.Fatalf("expected 3 paired modifications in contiguous CLI hunk, got +%d ~%d -%d (rows: %+v)",
			res.AddedCount, res.ModifiedCount, res.DeletedCount, res.Rows)
	}

	// Verify clean raw CLI text (starts with "set / ")
	for _, r := range res.Rows {
		if r.LeftText != "" && !strings.HasPrefix(r.LeftText, "set / ") {
			t.Errorf("expected LeftText to start with 'set / ', got %q", r.LeftText)
		}
		if r.RightText != "" && !strings.HasPrefix(r.RightText, "set / ") {
			t.Errorf("expected RightText to start with 'set / ', got %q", r.RightText)
		}
	}
}

func TestSideBySideDiffPairedUnevenRuns(t *testing.T) {
	// Test leftover additions (1 del + 3 ins -> 1 modified + 2 added)
	// and leftover deletions (3 dels + 1 ins -> 1 modified + 2 deleted),
	// separated by an unchanged BGP anchor using full AFI/SAFI names.
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "Port 1 Old",
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"admin-state": "enable",
				"description": "Port 2 Old",
				"mtu":         1500,
				"vlan-tagging": true,
			},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65001,
						"afi-safi": []interface{}{
							map[string]interface{}{
								"afi-safi-name": "ipv4-unicast",
								"admin-state":   "enable",
							},
							map[string]interface{}{
								"afi-safi-name": "ipv6-unicast",
								"admin-state":   "enable",
							},
						},
					},
				},
			},
		},
	}

	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "Port 1 New",
				"mtu":         9212,
				"vlan-tagging": true,
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"admin-state": "enable",
				"description": "Port 2 New",
			},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65001,
						"afi-safi": []interface{}{
							map[string]interface{}{
								"afi-safi-name": "ipv4-unicast",
								"admin-state":   "enable",
							},
							map[string]interface{}{
								"afi-safi-name": "ipv6-unicast",
								"admin-state":   "enable",
							},
						},
					},
				},
			},
		},
	}

	res := ComputeSideBySideDiff(oldCfg, newCfg, "cli", "", false, 3)
	assertRowInvariants(t, res)

	// ethernet-1/1: 1 del (description Old) + 3 ins (description New, mtu 9212, vlan-tagging true) => 1 modified + 2 added
	// ethernet-1/2: 3 dels (description Old, mtu 1500, vlan-tagging true) + 1 ins (description New) => 1 modified + 2 deleted
	if res.ModifiedCount != 2 || res.AddedCount != 2 || res.DeletedCount != 2 {
		t.Fatalf("expected +2 ~2 -2, got +%d ~%d -%d", res.AddedCount, res.ModifiedCount, res.DeletedCount)
	}
}

func TestSideBySideDiffUnifiedJSONModeAndBracketAlignment(t *testing.T) {
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65000,
						"afi-safi": []interface{}{
							map[string]interface{}{
								"afi-safi-name": "ipv4-unicast",
								"admin-state":   "enable",
							},
						},
					},
				},
			},
		},
	}
	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"description": "Fabric Uplink",
				"name":        "ethernet-1/1",
			},
			map[string]interface{}{
				"admin-state": "enable",
				"name":        "mgmt0",
			},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65001,
						"afi-safi": []interface{}{
							map[string]interface{}{
								"afi-safi-name": "ipv4-unicast",
								"admin-state":   "enable",
							},
							map[string]interface{}{
								"afi-safi-name": "ipv6-unicast",
								"admin-state":   "enable",
							},
						},
					},
				},
			},
		},
	}

	res := ComputeSideBySideDiff(oldCfg, newCfg, "unified", "", false, 3)
	assertRowInvariants(t, res)

	if res.Mode != "unified" || !res.HasChanges {
		t.Fatalf("expected unified mode with changes, got mode=%q hasChanges=%v", res.Mode, res.HasChanges)
	}

	// Verify bracket alignment: added ethernet-1/1 object has opening "{" and closing "}," as SideBySideAdded
	foundAddedEthOpen := false
	foundAddedEthClose := false
	foundModAS := false
	foundAddedIPv6Unicast := false
	for idx, r := range res.Rows {
		if r.Kind == SideBySideAdded && strings.Contains(r.RightText, `"ethernet-1/1"`) {
			if idx >= 2 && res.Rows[idx-2].Kind == SideBySideAdded && strings.TrimSpace(res.Rows[idx-2].RightText) == "{" {
				foundAddedEthOpen = true
			}
			if idx+1 < len(res.Rows) && res.Rows[idx+1].Kind == SideBySideAdded && strings.TrimSpace(res.Rows[idx+1].RightText) == "}," {
				foundAddedEthClose = true
			}
		}
		if r.Kind == SideBySideModified && strings.Contains(r.LeftText, "65000") && strings.Contains(r.RightText, "65001") {
			foundModAS = true
		}
		if r.Kind == SideBySideAdded && strings.Contains(r.RightText, `"ipv6-unicast"`) {
			foundAddedIPv6Unicast = true
		}
	}
	if !foundAddedEthOpen || !foundAddedEthClose {
		t.Fatalf("expected added ethernet-1/1 block to align enclosing brackets cleanly; rows:\n%+v", res.Rows)
	}
	if !foundModAS {
		t.Fatalf("expected autonomous-system 65000 -> 65001 as SideBySideModified")
	}
	if !foundAddedIPv6Unicast {
		t.Fatalf("expected ipv6-unicast afi-safi as SideBySideAdded")
	}
}

func TestSideBySideDiffPathModeDeduplication(t *testing.T) {
	oldCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "spine-old"},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"description": "Removed Port",
			},
		},
	}
	newCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "spine-new"},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": 65100,
						"afi-safi": []interface{}{
							map[string]interface{}{
								"afi-safi-name": "ipv4-unicast",
								"admin-state":   "enable",
							},
						},
					},
				},
			},
		},
	}

	res := ComputeSideBySideDiff(oldCfg, newCfg, "path", "", true, 3)
	assertRowInvariants(t, res)

	if res.Mode != "path" || !res.HasChanges {
		t.Fatalf("expected path mode with changes")
	}

	// Verify redundant parent container paths were deduplicated (no row with just "/interface[name=ethernet-1/1]" or "/network-instance[name=default]")
	var sawHostMod, sawDeletedDesc, sawAddedAS, sawAddedAfi bool
	for _, r := range res.Rows {
		if strings.Contains(r.LeftText, "keys}") || strings.Contains(r.RightText, "keys}") {
			t.Errorf("unexpected redundant parent container row: %+v", r)
		}
		if r.Kind == SideBySideModified &&
			r.LeftText == "/system/name/host-name = spine-old" &&
			r.RightText == "/system/name/host-name = spine-new" {
			sawHostMod = true
		}
		if r.Kind == SideBySideDeleted &&
			r.LeftText == "/interface[name=ethernet-1/1]/description = Removed Port" &&
			r.RightText == "" {
			sawDeletedDesc = true
		}
		if r.Kind == SideBySideAdded &&
			r.LeftText == "" &&
			r.RightText == "/network-instance[name=default]/protocols/bgp/autonomous-system = 65100" {
			sawAddedAS = true
		}
		if r.Kind == SideBySideAdded &&
			strings.Contains(r.RightText, "ipv4-unicast") &&
			strings.HasSuffix(r.RightText, "= enable") {
			sawAddedAfi = true
		}
	}

	if !sawHostMod || !sawDeletedDesc || !sawAddedAS || !sawAddedAfi {
		t.Fatalf("missing expected path rows (hostMod=%v delDesc=%v addAS=%v addAfi=%v); rows:\n%+v",
			sawHostMod, sawDeletedDesc, sawAddedAS, sawAddedAfi, res.Rows)
	}
	if res.ModifiedCount != 1 || res.DeletedCount != 2 || res.AddedCount != 2 {
		t.Fatalf("expected +2 ~1 -2 leaf changes, got +%d ~%d -%d; rows:\n%+v",
			res.AddedCount, res.ModifiedCount, res.DeletedCount, res.Rows)
	}
}

func TestSideBySideDiffFilteringAllModes(t *testing.T) {
	oldCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "old-host"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Old Eth1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "Old Eth2"},
		},
	}
	newCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "new-host"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "New Eth1"},
			map[string]interface{}{"name": "ethernet-1/2", "description": "New Eth2"},
		},
	}

	for _, mode := range []string{"cli", "unified", "path"} {
		res := ComputeSideBySideDiff(oldCfg, newCfg, mode, "ethernet-1/1", false, 3)
		assertRowInvariants(t, res)

		if !res.HasChanges {
			t.Fatalf("mode %s: expected changes for ethernet-1/1", mode)
		}
		if res.ModifiedCount != 1 || res.AddedCount != 0 || res.DeletedCount != 0 {
			t.Fatalf("mode %s: expected exactly 1 modification, got +%d ~%d -%d",
				mode, res.AddedCount, res.ModifiedCount, res.DeletedCount)
		}
		for _, r := range res.Rows {
			combined := r.LeftText + " " + r.RightText
			if strings.Contains(combined, "host-name") || strings.Contains(combined, "ethernet-1/2") {
				t.Errorf("mode %s: filter leaked unrelated content in row: %+v", mode, r)
			}
		}
	}
}

func TestSideBySideDiffFoldingVsExpanded(t *testing.T) {
	// Build 25 interfaces so there are large unchanged stretches before, between, and after changes.
	var oldIfaces, newIfaces []interface{}
	for i := 1; i <= 25; i++ {
		name := fmt.Sprintf("ethernet-1/%02d", i)
		descOld := fmt.Sprintf("port-%02d", i)
		descNew := descOld
		if i == 10 {
			descNew = "port-10-updated"
		}
		if i == 22 {
			descNew = "port-22-updated"
		}
		oldIfaces = append(oldIfaces, map[string]interface{}{
			"name":        name,
			"description": descOld,
		})
		newIfaces = append(newIfaces, map[string]interface{}{
			"name":        name,
			"description": descNew,
		})
	}
	oldCfg := map[string]interface{}{"interface": oldIfaces}
	newCfg := map[string]interface{}{"interface": newIfaces}

	for _, mode := range []string{"cli", "unified"} {
		expanded := ComputeSideBySideDiff(oldCfg, newCfg, mode, "", false, 3)
		folded := ComputeSideBySideDiff(oldCfg, newCfg, mode, "", true, 0) // <= 0 defaults to 3

		assertRowInvariants(t, expanded)
		assertRowInvariants(t, folded)

		if len(folded.Rows) >= len(expanded.Rows) {
			t.Fatalf("mode %s: expected folded rows (%d) < expanded rows (%d)", mode, len(folded.Rows), len(expanded.Rows))
		}
		if folded.TotalLines != expanded.TotalLines {
			t.Fatalf("mode %s: expected same TotalLines (folded=%d expanded=%d)", mode, folded.TotalLines, expanded.TotalLines)
		}
		if folded.ModifiedCount != 2 {
			t.Fatalf("mode %s: expected 2 modifications, got %d", mode, folded.ModifiedCount)
		}

		foldCount := 0
		for _, r := range folded.Rows {
			if r.Kind == SideBySideFold {
				foldCount++
				if r.FoldCount <= 2 {
					t.Errorf("mode %s: fold row should have FoldCount > 2, got %d", mode, r.FoldCount)
				}
				if r.FoldHint == "" {
					t.Errorf("mode %s: expected non-empty FoldHint for interface fold row", mode)
				}
			}
		}
		if foldCount < 2 {
			t.Fatalf("mode %s: expected at least 2 fold rows (leading + middle), got %d", mode, foldCount)
		}
	}
}

func TestSideBySideDiffFoldingSkipsSmallGaps(t *testing.T) {
	// 12 CLI lines: change at index 0 and index 9 with contextLines=3 ->
	// kept: [0..3] and [6..11]; hidden gap is indices [4, 5] (length 2 <= 2), must NOT fold.
	// Change at index 0 and index 10 with contextLines=3 ->
	// kept: [0..3] and [7..11]; hidden gap is indices [4, 5, 6] (length 3 > 2), MUST fold.
	makeCfg := func(mutIdx int) (map[string]interface{}, map[string]interface{}) {
		var oldIfaces, newIfaces []interface{}
		for i := 0; i < 12; i++ {
			name := fmt.Sprintf("ethernet-1/%02d", i)
			oldDesc := fmt.Sprintf("d-%02d", i)
			newDesc := oldDesc
			if i == 0 || i == mutIdx {
				newDesc = oldDesc + "-chg"
			}
			oldIfaces = append(oldIfaces, map[string]interface{}{"name": name, "description": oldDesc})
			newIfaces = append(newIfaces, map[string]interface{}{"name": name, "description": newDesc})
		}
		return map[string]interface{}{"interface": oldIfaces}, map[string]interface{}{"interface": newIfaces}
	}

	old2, new2 := makeCfg(9)
	resGap2 := ComputeSideBySideDiff(old2, new2, "cli", "", true, 3)
	assertRowInvariants(t, resGap2)
	for _, r := range resGap2.Rows {
		if r.Kind == SideBySideFold {
			t.Fatalf("expected gap of 2 lines NOT to fold, got fold row: %+v", r)
		}
	}
	if len(resGap2.Rows) != 12 {
		t.Fatalf("expected all 12 rows shown for 2-line gap, got %d", len(resGap2.Rows))
	}

	old3, new3 := makeCfg(10)
	resGap3 := ComputeSideBySideDiff(old3, new3, "cli", "", true, 3)
	assertRowInvariants(t, resGap3)
	folds := 0
	for _, r := range resGap3.Rows {
		if r.Kind == SideBySideFold {
			folds++
			if r.FoldCount != 3 {
				t.Fatalf("expected FoldCount=3 for 3-line gap, got %d", r.FoldCount)
			}
		}
	}
	if folds != 1 {
		t.Fatalf("expected exactly 1 fold row for 3-line gap, got %d", folds)
	}
}

func TestSideBySideDiffIdenticalConfigs(t *testing.T) {
	cfg1 := map[string]interface{}{
		"srl_nokia-system:system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-leaf1"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/2", "admin-state": "enable"},
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable"},
		},
	}
	cfg2 := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"admin-state": "enable", "name": "ethernet-1/1"},
			map[string]interface{}{"admin-state": "enable", "name": "ethernet-1/2"},
		},
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-leaf1"},
		},
	}

	for _, mode := range []string{"cli", "unified", "path"} {
		folded := ComputeSideBySideDiff(cfg1, cfg2, mode, "", true, 3)
		if folded.HasChanges {
			t.Fatalf("mode %s (folded): expected HasChanges=false", mode)
		}
		if folded.Rows != nil {
			t.Fatalf("mode %s (folded): expected Rows=nil when identical, got %d rows", mode, len(folded.Rows))
		}
		if folded.AddedCount != 0 || folded.ModifiedCount != 0 || folded.DeletedCount != 0 {
			t.Fatalf("mode %s (folded): expected zero counts", mode)
		}

		expanded := ComputeSideBySideDiff(cfg1, cfg2, mode, "", false, 3)
		if expanded.HasChanges {
			t.Fatalf("mode %s (expanded): expected HasChanges=false", mode)
		}
		if mode == "path" {
			if len(expanded.Rows) != 0 {
				t.Fatalf("path mode (expanded): expected 0 rows when identical, got %d", len(expanded.Rows))
			}
		} else {
			if len(expanded.Rows) == 0 {
				t.Fatalf("mode %s (expanded): expected equal rows when !foldUnchanged", mode)
			}
			for _, r := range expanded.Rows {
				if r.Kind != SideBySideEqual {
					t.Errorf("mode %s (expanded): expected only SideBySideEqual rows, got %v", mode, r.Kind)
				}
			}
			assertRowInvariants(t, expanded)
		}
	}
}

func TestSideBySideDiffEmptyConfigs(t *testing.T) {
	populated := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-1"},
		},
	}

	for _, mode := range []string{"cli", "unified", "path"} {
		// nil vs nil
		bothEmpty := ComputeSideBySideDiff(nil, nil, mode, "", true, 3)
		if bothEmpty.HasChanges || len(bothEmpty.Rows) != 0 || bothEmpty.TotalLines != 0 {
			t.Fatalf("mode %s (nil vs nil): unexpected result %+v", mode, bothEmpty)
		}

		// empty -> populated (all additions, no bogus modifications)
		allAdded := ComputeSideBySideDiff(nil, populated, mode, "", true, 3)
		assertRowInvariants(t, allAdded)
		if !allAdded.HasChanges || allAdded.AddedCount == 0 || allAdded.ModifiedCount != 0 || allAdded.DeletedCount != 0 {
			t.Fatalf("mode %s (nil -> populated): expected pure additions, got +%d ~%d -%d",
				mode, allAdded.AddedCount, allAdded.ModifiedCount, allAdded.DeletedCount)
		}

		// populated -> empty (all deletions, no bogus modifications)
		allDeleted := ComputeSideBySideDiff(populated, nil, mode, "", true, 3)
		assertRowInvariants(t, allDeleted)
		if !allDeleted.HasChanges || allDeleted.DeletedCount == 0 || allDeleted.ModifiedCount != 0 || allDeleted.AddedCount != 0 {
			t.Fatalf("mode %s (populated -> nil): expected pure deletions, got +%d ~%d -%d",
				mode, allDeleted.AddedCount, allDeleted.ModifiedCount, allDeleted.DeletedCount)
		}
	}
}

func TestSideBySideDiffDefaultModeFallback(t *testing.T) {
	oldCfg := map[string]interface{}{"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "a"}}}
	newCfg := map[string]interface{}{"system": map[string]interface{}{"name": map[string]interface{}{"host-name": "b"}}}

	res := ComputeSideBySideDiff(oldCfg, newCfg, "", "", true, 3)
	assertRowInvariants(t, res)
	if res.Mode != "cli" {
		t.Fatalf("expected empty mode to default to 'cli', got %q", res.Mode)
	}
	if !res.HasChanges || res.ModifiedCount != 1 {
		t.Fatalf("expected 1 modification, got %+v", res)
	}
}

func TestSideBySideDiffDocumentReconstructionRoundTrip(t *testing.T) {
	// Adding and deleting list elements at start, middle, and end (triggering comma-differing equal lines and slider compaction).
	oldCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable", "description": "old-1"},
			map[string]interface{}{"name": "ethernet-1/2", "admin-state": "enable", "description": "to-delete"},
			map[string]interface{}{"name": "mgmt0", "admin-state": "enable"},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": float64(65001),
						"afi-safi": []interface{}{
							map[string]interface{}{"afi-safi-name": "ipv4-unicast", "admin-state": "enable"},
							map[string]interface{}{"afi-safi-name": "ipv6-unicast", "admin-state": "enable"},
						},
					},
				},
			},
		},
	}
	newCfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable", "description": "new-1"},
			map[string]interface{}{"name": "ethernet-1/3", "admin-state": "enable", "description": "added-3"},
			map[string]interface{}{"name": "mgmt0", "admin-state": "enable"},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": float64(65001),
						"afi-safi": []interface{}{
							map[string]interface{}{"afi-safi-name": "ipv4-unicast", "admin-state": "enable"},
						},
					},
				},
			},
		},
	}

	normOld := normalizeConfigMap(oldCfg)
	normNew := normalizeConfigMap(newCfg)

	for _, mode := range []string{"cli", "unified"} {
		res := ComputeSideBySideDiff(oldCfg, newCfg, mode, "", false, 3)
		assertRowInvariants(t, res)

		var wantOld, wantNew []string
		if mode == "cli" {
			wantOld = computeCLILinesForTest(normOld)
			wantNew = computeCLILinesForTest(normNew)
		} else {
			wantOld = canonicalJSONLines(normOld)
			wantNew = canonicalJSONLines(normNew)
		}

		var gotOld, gotNew []string
		for _, r := range res.Rows {
			if r.LeftLineNo > 0 {
				gotOld = append(gotOld, r.LeftText)
			}
			if r.RightLineNo > 0 {
				gotNew = append(gotNew, r.RightText)
			}
		}

		if strings.Join(gotOld, "\n") != strings.Join(wantOld, "\n") {
			t.Fatalf("mode %s: reconstructed Left pane does not match old document verbatim", mode)
		}
		if strings.Join(gotNew, "\n") != strings.Join(wantNew, "\n") {
			t.Fatalf("mode %s: reconstructed Right pane does not match new document verbatim", mode)
		}
	}
}

func computeCLILinesForTest(norm map[string]interface{}) []string {
	// Uses same normalizer helper via package differ.
	res := ComputeSideBySideDiff(norm, norm, "cli", "", false, 3)
	out := make([]string, 0, len(res.Rows))
	for _, r := range res.Rows {
		out = append(out, r.LeftText)
	}
	return out
}

func TestSideBySideDiffFilterExcludesChanges(t *testing.T) {
	oldCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "old-host"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable"},
		},
	}
	newCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "new-host"},
		},
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "admin-state": "enable"},
		},
	}

	for _, mode := range []string{"cli", "unified", "path"} {
		folded := ComputeSideBySideDiff(oldCfg, newCfg, mode, "ethernet-1/1", true, 3)
		if folded.HasChanges || folded.Rows != nil {
			t.Fatalf("mode %s (folded): expected no changes & nil rows when filtered section is unchanged, got %+v", mode, folded)
		}
		expanded := ComputeSideBySideDiff(oldCfg, newCfg, mode, "ethernet-1/1", false, 3)
		if expanded.HasChanges {
			t.Fatalf("mode %s (expanded): expected HasChanges=false", mode)
		}
		if mode != "path" && len(expanded.Rows) == 0 {
			t.Fatalf("mode %s (expanded): expected equal rows for unchanged filtered section", mode)
		}
	}
}

func TestSideBySideDiffPathModeScalarAndContainerFormatting(t *testing.T) {
	oldCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"dns": map[string]interface{}{
				"server-list": []interface{}{"1.1.1.1", "8.8.8.8"},
			},
			"name": map[string]interface{}{
				"host-name":   "srl-a",
				"domain-name": "",
			},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": float64(65001),
					},
				},
			},
		},
	}
	newCfg := map[string]interface{}{
		"system": map[string]interface{}{
			"dns": map[string]interface{}{
				"server-list": []interface{}{"1.1.1.1", "9.9.9.9"},
			},
			"name": map[string]interface{}{
				"host-name":   "srl-b",
				"domain-name": "example.net",
			},
		},
		"network-instance": []interface{}{
			map[string]interface{}{
				"name": "default",
				"protocols": map[string]interface{}{
					"bgp": map[string]interface{}{
						"autonomous-system": float64(4200000001),
						"ebgp-default-policy": map[string]interface{}{},
					},
				},
			},
		},
	}

	res := ComputeSideBySideDiff(oldCfg, newCfg, "path", "", true, 3)
	assertRowInvariants(t, res)

	foundFloat := false
	foundPresence := false
	foundEmptyStr := false
	for _, r := range res.Rows {
		if r.Kind == SideBySideModified && strings.HasSuffix(r.RightText, "= 4200000001") && strings.HasSuffix(r.LeftText, "= 65001") {
			foundFloat = true
		}
		if r.Kind == SideBySideAdded && strings.HasSuffix(r.RightText, "ebgp-default-policy = {}") {
			foundPresence = true
		}
		if r.Kind == SideBySideModified && strings.HasSuffix(r.LeftText, `domain-name = ""`) && strings.HasSuffix(r.RightText, "domain-name = example.net") {
			foundEmptyStr = true
		}
	}
	if !foundFloat || !foundPresence || !foundEmptyStr {
		t.Fatalf("expected clean formatting for 4-byte ASN float (%v), empty presence container (%v), and empty string (%v); rows:\n%+v",
			foundFloat, foundPresence, foundEmptyStr, res.Rows)
	}
}
