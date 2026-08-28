package filter

import (
	"testing"
)

func TestIsPathMatching(t *testing.T) {
	if !IsPathMatching("/interface/ethernet-1/1", "ethernet-1/1") {
		t.Fatalf("expected match")
	}
	if !IsPathMatching("interface ethernet-1/1", "interface") {
		t.Fatalf("expected match")
	}
	if IsPathMatching("/system/name", "bgp") {
		t.Fatalf("did not expect match")
	}
}

func TestFilterCLILines(t *testing.T) {
	lines := []string{
		"set / interface ethernet-1/1 admin-state enable",
		"set / network-instance default protocols bgp autonomous-system 65001",
	}
	matched := FilterCLILines(lines, "bgp")
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0] != lines[1] {
		t.Fatalf("expected bgp line, got %s", matched[0])
	}
}

func TestFilterConfigSubtree(t *testing.T) {
	cfg := map[string]interface{}{
		"system": map[string]interface{}{
			"information": map[string]interface{}{
				"location": "Test Location",
			},
			"name": map[string]interface{}{
				"host-name": "srl1",
			},
		},
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "Port 1",
			},
			map[string]interface{}{
				"name":        "ethernet-1/2",
				"description": "Port 2",
			},
		},
	}

	// 1. Test /system/information
	res1 := FilterConfigSubtree(cfg, "/system/information")
	sys, ok := res1["system"].(map[string]interface{})
	if !ok || sys["information"] == nil {
		t.Fatalf("expected /system/information in result, got %+v", res1)
	}
	if sys["name"] != nil {
		t.Fatalf("did not expect /system/name in result")
	}

	// 2. Test /interface[name=ethernet-1/1]
	t.Logf("tokens: %v", TokenizePath("/interface[name=ethernet-1/1]"))
	res2 := FilterConfigSubtree(cfg, "/interface[name=ethernet-1/1]")
	t.Logf("res2: %+v", res2)
	ifList, ok := res2["interface"].([]interface{})
	if !ok || len(ifList) != 1 {
		t.Fatalf("expected 1 interface in result, got %+v", res2)
	}
	ifMap := ifList[0].(map[string]interface{})
	if ifMap["name"] != "ethernet-1/1" {
		t.Fatalf("expected ethernet-1/1, got %v", ifMap["name"])
	}

	// 3. Test /interface[name=ethernet-1/1]/description (leaf inside list item)
	res3 := FilterConfigSubtree(cfg, "/interface[name=ethernet-1/1]/description")
	ifList3, ok := res3["interface"].([]interface{})
	if !ok || len(ifList3) != 1 {
		t.Fatalf("expected 1 interface in result, got %+v", res3)
	}
	ifMap3 := ifList3[0].(map[string]interface{})
	if ifMap3["name"] != "ethernet-1/1" {
		t.Fatalf("expected list key 'name' to be preserved, got %v", ifMap3["name"])
	}
	if ifMap3["description"] != "Port 1" {
		t.Fatalf("expected description Port 1, got %v", ifMap3["description"])
	}
}
