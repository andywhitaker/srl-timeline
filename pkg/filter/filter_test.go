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

	// 4. Test direct raw query "ethernet-1/1"
	res4 := FilterConfigSubtree(cfg, "ethernet-1/1")
	ifList4, ok := res4["interface"].([]interface{})
	if !ok || len(ifList4) != 1 {
		t.Fatalf("expected 1 interface in result for raw 'ethernet-1/1', got %+v", res4)
	}
	ifMap4 := ifList4[0].(map[string]interface{})
	if ifMap4["name"] != "ethernet-1/1" {
		t.Fatalf("expected ethernet-1/1, got %v", ifMap4["name"])
	}
}

func TestNumericSuffixNoCollision(t *testing.T) {
	if IsPathMatching("/interface[name=ethernet-1/10]/description", "ethernet-1/1") {
		t.Fatalf("ethernet-1/1 must NOT match ethernet-1/10")
	}
	if IsPathMatching("set / interface ethernet-1/19 admin-state enable", "interface ethernet-1/1") {
		t.Fatalf("interface ethernet-1/1 must NOT match ethernet-1/19")
	}
	if IsPathMatching("set / interface ethernet-1/1.0 admin-state enable", "ethernet-1/1") {
		t.Fatalf("ethernet-1/1 must NOT match ethernet-1/1.0")
	}
	if !IsPathMatching("set / interface ethernet-1/1 subinterface 0 admin-state enable", "ethernet-1/1") {
		t.Fatalf("ethernet-1/1 MUST match ethernet-1/1 subinterface 0")
	}

	cfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{"name": "ethernet-1/1", "description": "Port 1"},
			map[string]interface{}{"name": "ethernet-1/10", "description": "Port 10"},
			map[string]interface{}{"name": "ethernet-1/11", "description": "Port 11"},
		},
	}
	res := FilterConfigSubtree(cfg, "interface ethernet-1/1")
	ifList, ok := res["interface"].([]interface{})
	if !ok || len(ifList) != 1 {
		t.Fatalf("expected exactly 1 interface for 'interface ethernet-1/1', got %+v", res)
	}
	if ifList[0].(map[string]interface{})["name"] != "ethernet-1/1" {
		t.Fatalf("expected ethernet-1/1, got %v", ifList[0])
	}
}

func TestUnifiedXPathAndCLIQueryCompatibility(t *testing.T) {
	cliLines := []string{
		"set / interface ethernet-1/1 admin-state enable",
		"set / interface ethernet-1/10 admin-state enable",
		"set / network-instance default protocols bgp autonomous-system 65001",
	}

	// XPath query against CLI lines
	matchedByXPath := FilterCLILines(cliLines, "/interface[name=ethernet-1/1]")
	if len(matchedByXPath) != 1 || matchedByXPath[0] != cliLines[0] {
		t.Fatalf("expected XPath query to match only ethernet-1/1 CLI line, got %v", matchedByXPath)
	}

	// CLI query against XPath target
	if !IsPathMatching("/interface[name=ethernet-1/1]/admin-state", "interface ethernet-1/1") {
		t.Fatalf("expected CLI query 'interface ethernet-1/1' to match XPath '/interface[name=ethernet-1/1]/admin-state'")
	}
}

func TestCompoundKeyACLFilterSubtree(t *testing.T) {
	cfg := map[string]interface{}{
		"acl": map[string]interface{}{
			"acl-filter": []interface{}{
				map[string]interface{}{
					"name":        "cpm",
					"type":        "ipv4",
					"description": "IPv4 CPM Filter",
					"entry": []interface{}{
						map[string]interface{}{"sequence-id": 10},
					},
				},
				map[string]interface{}{
					"name":        "cpm",
					"type":        "ipv6",
					"description": "IPv6 CPM Filter",
					"entry": []interface{}{
						map[string]interface{}{"sequence-id": 20},
					},
				},
			},
		},
	}

	res := FilterConfigSubtree(cfg, "/acl/acl-filter[name=cpm][type=ipv4]/description")
	aclMap, ok := res["acl"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected acl map, got %+v", res)
	}
	filters, ok := aclMap["acl-filter"].([]interface{})
	if !ok || len(filters) != 1 {
		t.Fatalf("expected 1 acl-filter (ipv4 only), got %+v", aclMap["acl-filter"])
	}
	f0 := filters[0].(map[string]interface{})
	if f0["name"] != "cpm" || f0["type"] != "ipv4" || f0["description"] != "IPv4 CPM Filter" {
		t.Fatalf("unexpected filtered acl-filter item: %+v", f0)
	}
	if _, leakedEntry := f0["entry"]; leakedEntry {
		t.Fatalf("child list 'entry' must NOT be copied as a list key when filtering description leaf: %+v", f0)
	}
}

func TestXPathToCLIPath(t *testing.T) {
	cases := []struct {
		xpath string
		want  string
	}{
		{"/interface[name=ethernet-1/1]/description", "interface ethernet-1/1 description"},
		{"/acl/acl-filter[name=cpm][type=ipv4]/entry[sequence-id=10]", "acl acl-filter cpm type ipv4 entry 10"},
		{"/routing-policy/prefix-set[name=p1]/prefix[ip-prefix=10.0.0.0/8][mask-length-range=24..32]", "routing-policy prefix-set p1 prefix 10.0.0.0/8 mask-length-range 24..32"},
	}
	for _, tc := range cases {
		got := XPathToCLIPath(tc.xpath)
		if got != tc.want {
			t.Errorf("XPathToCLIPath(%q) = %q, want %q", tc.xpath, got, tc.want)
		}
	}
}

func TestAnchoredXPathDoesNotMatchDescendantSubinterface(t *testing.T) {
	if IsPathMatching("/interface[name=ethernet-1/1]/subinterface[index=0]/description", "/interface[name=ethernet-1/1]/description") {
		t.Fatalf("anchored XPath /interface[name=ethernet-1/1]/description must not match subinterface description")
	}

	cfg := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"description": "Port 1",
				"subinterface": []interface{}{
					map[string]interface{}{
						"index":       0,
						"description": "Subif 0",
					},
				},
			},
		},
	}

	res := FilterConfigSubtree(cfg, "/interface[name=ethernet-1/1]/description")
	ifList, ok := res["interface"].([]interface{})
	if !ok || len(ifList) != 1 {
		t.Fatalf("expected 1 interface, got %+v", res)
	}
	ifMap := ifList[0].(map[string]interface{})
	if _, leakedSubif := ifMap["subinterface"]; leakedSubif {
		t.Fatalf("anchored XPath /interface[name=ethernet-1/1]/description must not leak subinterface: %+v", ifMap)
	}
}

func TestCompoundKeyCLIQueryMatchesXPath(t *testing.T) {
	if !IsPathMatching("/acl/acl-filter[name=cpm][type=ipv4]/entry[sequence-id=10]", "acl acl-filter cpm type ipv4") {
		t.Fatalf("expected CLI query with secondary key keyword 'type ipv4' to match XPath")
	}
	if !IsPathMatching("/routing-policy/prefix-set[name=p1]/prefix[ip-prefix=10.0.0.0/8][mask-length-range=24..32]", "routing-policy prefix-set p1 prefix 10.0.0.0/8 mask-length-range 24..32") {
		t.Fatalf("expected CLI query with secondary key keyword 'mask-length-range 24..32' to match XPath")
	}
}
