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
