package normalizer

import (
	"encoding/json"
	"testing"
)

func TestStripNamespace(t *testing.T) {
	if StripNamespace("srl_nokia-interfaces:interface") != "interface" {
		t.Fatalf("expected interface")
	}
	if StripNamespace("system") != "system" {
		t.Fatalf("expected system")
	}
}

func TestNormalizeStructure(t *testing.T) {
	rawJSON := `{"srl_nokia-system:system": {"name": {"host-name": "srl-1"}}}`
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(rawJSON), &data)

	norm := NormalizeStructure(data, true).(map[string]interface{})
	if _, ok := norm["system"]; !ok {
		t.Fatalf("expected stripped key 'system'")
	}
}

func TestJSONToFlatCLI(t *testing.T) {
	raw := map[string]interface{}{
		"interface": []interface{}{
			map[string]interface{}{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
			},
		},
	}
	lines := JSONToFlatCLI(raw, "")
	if len(lines) == 0 {
		t.Fatalf("expected CLI lines")
	}
	expected := "set / interface ethernet-1/1 admin-state enable"
	found := false
	for _, l := range lines {
		if l == expected {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected '%s' in %v", expected, lines)
	}

	aclRaw := map[string]interface{}{
		"acl": map[string]interface{}{
			"acl-filter": []interface{}{
				map[string]interface{}{
					"name":        "cpm",
					"type":        "ipv4",
					"description": "CPM Filter",
				},
			},
		},
	}
	aclLines := JSONToFlatCLI(aclRaw, "")
	aclExpected := "set / acl acl-filter cpm type ipv4 description \"CPM Filter\""
	aclFound := false
	for _, l := range aclLines {
		if l == aclExpected {
			aclFound = true
			break
		}
	}
	if !aclFound {
		t.Fatalf("expected '%s' in %v", aclExpected, aclLines)
	}

	userRaw := map[string]interface{}{
		"system": map[string]interface{}{
			"aaa": map[string]interface{}{
				"authentication": map[string]interface{}{
					"user": []interface{}{
						map[string]interface{}{
							"username": "admin",
							"role":     []interface{}{"admin"},
							"ssh-key":  []interface{}{"ssh-rsa AAAA..."},
						},
					},
				},
			},
		},
	}
	userLines := JSONToFlatCLI(userRaw, "")
	expectedRole := "set / system aaa authentication user admin role [ admin ]"
	expectedSSH := "set / system aaa authentication user admin ssh-key [ \"ssh-rsa AAAA...\" ]"
	foundRole, foundSSH := false, false
	for _, l := range userLines {
		if l == expectedRole {
			foundRole = true
		}
		if l == expectedSSH {
			foundSSH = true
		}
	}
	if !foundRole || !foundSSH {
		t.Fatalf("expected leaf-lists in %v", userLines)
	}
}

func TestFormatScalarCLIPreservesColonValues(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{"2001:db8:100::1/64", "2001:db8:100::1/64"},
		{"00:1a:2b:3c:4d:5e", "00:1a:2b:3c:4d:5e"},
		{"65001:100", "65001:100"},
		{"10.0.0.1:100", "10.0.0.1:100"},
		{"https://10.0.0.1:8443/api", "https://10.0.0.1:8443/api"},
		{"srl_nokia-network-instance:ip-vrf", "ip-vrf"},
		{"srl_nokia-common:ipv4-unicast", "ipv4-unicast"},
		{"Line 1\nLine 2\tTabbed", "\"Line 1\\nLine 2\\tTabbed\""},
	}
	for _, tc := range cases {
		got := FormatScalarCLI(tc.in)
		if got != tc.want {
			t.Errorf("FormatScalarCLI(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJSONToFlatCLIMultiLeafAndMetadata(t *testing.T) {
	raw := map[string]interface{}{
		"_preamble": "comment",
		"network-instance": []interface{}{
			map[string]interface{}{
				"_annotate":   "vrf comment",
				"name":        "default",
				"description": "Two  Spaces   Preserved",
				"type":        "srl_nokia-network-instance:default",
				"static-routes": map[string]interface{}{
					"route": []interface{}{
						map[string]interface{}{
							"prefix":         "2001:db8:100::/48",
							"admin-state":    "enable",
							"metric":         10,
							"next-hop-group": "nhg-v6",
						},
					},
				},
			},
		},
	}

	norm := NormalizeStructure(raw, true).(map[string]interface{})
	if _, hasPreamble := norm["_preamble"]; hasPreamble {
		t.Fatalf("expected _preamble to be stripped during NormalizeStructure")
	}

	lines := JSONToFlatCLI(norm, "")
	wantLines := []string{
		"set / network-instance default description \"Two  Spaces   Preserved\"",
		"set / network-instance default static-routes route 2001:db8:100::/48 admin-state enable",
		"set / network-instance default static-routes route 2001:db8:100::/48 metric 10",
		"set / network-instance default static-routes route 2001:db8:100::/48 next-hop-group nhg-v6",
		"set / network-instance default type default",
	}
	if len(lines) != len(wantLines) {
		t.Fatalf("expected %d CLI lines (one leaf per line), got %d: %v", len(wantLines), len(lines), lines)
	}
	for i, w := range wantLines {
		if lines[i] != w {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], w)
		}
	}
}

