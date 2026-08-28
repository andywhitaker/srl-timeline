package schema

import (
	"testing"
)

func TestSchemaRegistry(t *testing.T) {
	reg := GetRegistry()

	// Test interface keys
	ifKeys := reg.GetListKeys("", "interface", nil)
	if len(ifKeys) != 1 || ifKeys[0] != "name" {
		t.Fatalf("expected interface key 'name', got %v", ifKeys)
	}

	// Test subinterface keys
	subKeys := reg.GetListKeys("", "subinterface", nil)
	if len(subKeys) != 1 || subKeys[0] != "index" {
		t.Fatalf("expected subinterface key 'index', got %v", subKeys)
	}

	// Test acl-filter compound keys
	aclKeys := reg.GetListKeys("", "acl-filter", nil)
	if len(aclKeys) != 2 || aclKeys[0] != "name" || aclKeys[1] != "type" {
		t.Fatalf("expected acl-filter keys ['name', 'type'], got %v", aclKeys)
	}

	// Test user keys
	userKeys := reg.GetListKeys("", "user", nil)
	if len(userKeys) != 1 || userKeys[0] != "username" {
		t.Fatalf("expected user key 'username', got %v", userKeys)
	}

	// Test buffer and file keys
	bufKeys := reg.GetListKeys("", "buffer", nil)
	if len(bufKeys) != 1 || bufKeys[0] != "buffer-name" {
		t.Fatalf("expected buffer key 'buffer-name', got %v", bufKeys)
	}

	fileKeys := reg.GetListKeys("", "file", nil)
	if len(fileKeys) != 1 || fileKeys[0] != "file-name" {
		t.Fatalf("expected file key 'file-name', got %v", fileKeys)
	}

	// Test FormatListKeyCLI for acl-filter
	aclItem := map[string]interface{}{
		"name": "cpm",
		"type": "ipv4",
		"description": "test filter",
	}
	formatScalar := func(v interface{}) string {
		return v.(string)
	}
	cliStr, consumed := reg.FormatListKeyCLI("", "acl-filter", aclItem, formatScalar)
	if cliStr != "cpm type ipv4" {
		t.Fatalf("expected 'cpm type ipv4', got '%s'", cliStr)
	}
	if len(consumed) != 2 {
		t.Fatalf("expected 2 consumed keys, got %v", consumed)
	}
}
