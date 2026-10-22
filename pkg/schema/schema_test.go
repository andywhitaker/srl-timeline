package schema

import (
	"os"
	"path/filepath"
	"reflect"
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
		"name":        "cpm",
		"type":        "ipv4",
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

func TestSchemaMultiContextListKeys(t *testing.T) {
	reg := GetRegistry()

	cases := []struct {
		pathPrefix string
		listName   string
		item       map[string]interface{}
		wantKeys   []string
	}{
		{
			pathPrefix: "network-instance default static-routes",
			listName:   "route",
			item:       map[string]interface{}{"prefix": "10.0.0.0/24", "next-hop-group": "nhg1"},
			wantKeys:   []string{"prefix"},
		},
		{
			pathPrefix: "network-instance default aggregate-routes",
			listName:   "route",
			item:       map[string]interface{}{"prefix": "10.0.0.0/16"},
			wantKeys:   []string{"prefix"},
		},
		{
			pathPrefix: "system ntp",
			listName:   "server",
			item:       map[string]interface{}{"address": "10.1.1.1"},
			wantKeys:   []string{"address"},
		},
		{
			pathPrefix: "system logging remote-server",
			listName:   "server",
			item:       map[string]interface{}{"host": "10.2.2.2"},
			wantKeys:   []string{"host"},
		},
		{
			pathPrefix: "acl acl-filter cpm type ipv4",
			listName:   "entry",
			item:       map[string]interface{}{"sequence-id": 10},
			wantKeys:   []string{"sequence-id"},
		},
		{
			pathPrefix: "network-instance default next-hop-groups group nhg1",
			listName:   "nexthop",
			item:       map[string]interface{}{"index": 1, "ip-address": "192.0.2.1"},
			wantKeys:   []string{"index"},
		},
		{
			pathPrefix: "network-instance default protocols bgp",
			listName:   "neighbor",
			item:       map[string]interface{}{"peer-address": "192.0.2.2", "peer-as": 65001},
			wantKeys:   []string{"peer-address"},
		},
		{
			pathPrefix: "network-instance default protocols bgp",
			listName:   "group",
			item:       map[string]interface{}{"group-name": "ebgp-peers"},
			wantKeys:   []string{"group-name"},
		},
		{
			pathPrefix: "routing-policy prefix-set pset1",
			listName:   "prefix",
			item:       map[string]interface{}{"ip-prefix": "10.0.0.0/8", "mask-length-range": "24..32"},
			wantKeys:   []string{"ip-prefix", "mask-length-range"},
		},
	}

	for _, tc := range cases {
		got := reg.GetListKeys(tc.pathPrefix, tc.listName, tc.item)
		if !reflect.DeepEqual(got, tc.wantKeys) {
			t.Errorf("GetListKeys(%q, %q) = %v, want %v", tc.pathPrefix, tc.listName, got, tc.wantKeys)
		}
	}

	// Ensure dead English words from YANG prose are not registered as list names
	for _, stopWord := range []string{"a", "the", "all", "with", "to", "in", "of", "that"} {
		if keys := reg.GetListKeys("", stopWord, nil); len(keys) != 0 {
			t.Errorf("expected stopWord %q to have no list keys, got %v", stopWord, keys)
		}
	}
}

func TestLoadFromDirectoryCommentsAndURLs(t *testing.T) {
	tmpDir := t.TempDir()
	yangContent := `module srl_nokia-test {
  namespace "urn:nokia.com:srlinux:test";
  // Single line comment before URL
  description "See http://documentation.nokia.com/srlinux for details";
  /* Multi-line
     block comment */
  container test-container {
    list custom-widget {
      // comment inside list
      key "widget-id widget-type";
      leaf widget-id { type string; }
      leaf widget-type { type string; }
    }
    list must-before-key-list {
      max-elements 4;
      must "(../../type = 'mac-vrf')" {
        error-message "only mac-vrf allowed";
      }
      key "pw-name";
      leaf pw-name { type string; }
    }
    list unquoted-key-list {
      key unquoted-id;
      leaf unquoted-id { type uint32; }
    }
    list keyless-outer-list {
      config false;
      leaf summary { type string; }
      list nested-keyed-list {
        key "nested-key";
        leaf nested-key { type string; }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "srl_nokia-test.yang"), []byte(yangContent), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewSchemaRegistry()
	if err := reg.LoadFromDirectory(tmpDir); err != nil {
		t.Fatal(err)
	}

	keys := reg.GetListKeys("", "custom-widget", nil)
	want := []string{"widget-id", "widget-type"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("expected %v after comment and URL stripping, got %v", want, keys)
	}

	mustKeys := reg.GetListKeys("", "must-before-key-list", nil)
	if !reflect.DeepEqual(mustKeys, []string{"pw-name"}) {
		t.Fatalf("expected ['pw-name'] for list with must block before key, got %v", mustKeys)
	}

	unquotedKeys := reg.GetListKeys("", "unquoted-key-list", nil)
	if !reflect.DeepEqual(unquotedKeys, []string{"unquoted-id"}) {
		t.Fatalf("expected ['unquoted-id'] for list with unquoted key, got %v", unquotedKeys)
	}

	if outerKeys := reg.GetListKeys("", "keyless-outer-list", nil); len(outerKeys) != 0 {
		t.Fatalf("expected keyless-outer-list to have no keys, got %v", outerKeys)
	}

	nestedKeys := reg.GetListKeys("", "nested-keyed-list", nil)
	if !reflect.DeepEqual(nestedKeys, []string{"nested-key"}) {
		t.Fatalf("expected ['nested-key'] for nested-keyed-list, got %v", nestedKeys)
	}
}

func TestCustomYANGPathNonCollision(t *testing.T) {
	reg := GetRegistry()

	// Custom protocol interface list under /network-instance/protocols/eigrp/interface
	// must not falsely match "/network-instance/interface" or "/interface" in listKeysByPath.
	eigrpIfItem := map[string]interface{}{
		"interface-name": "ethernet-1/1.0",
	}
	gotCLI := reg.GetListKeys("network-instance default protocols eigrp", "interface", eigrpIfItem)
	if !reflect.DeepEqual(gotCLI, []string{"interface-name"}) {
		t.Fatalf("expected ['interface-name'] for EIGRP CLI path, got %v", gotCLI)
	}

	gotXPath := reg.GetListKeys("/network-instance[name=default]/protocols/eigrp/interface", "interface", eigrpIfItem)
	if !reflect.DeepEqual(gotXPath, []string{"interface-name"}) {
		t.Fatalf("expected ['interface-name'] for EIGRP XPath, got %v", gotXPath)
	}
}

func TestFormatListKeyCLIPartialCompoundKey(t *testing.T) {
	reg := GetRegistry()
	// Item only has "name", missing "type" required by acl-filter compound key ["name", "type"].
	// FormatListKeyCLI should not leave "name" marked as consumed if the compound key fails to match,
	// or should fall back cleanly to the single "name" key.
	partialItem := map[string]interface{}{
		"name": "only-name",
	}
	cliStr, consumed := reg.FormatListKeyCLI("", "acl-filter", partialItem, func(v interface{}) string {
		return v.(string)
	})
	if cliStr != "only-name" {
		t.Fatalf("expected fallback key 'only-name', got %q", cliStr)
	}
	if len(consumed) != 1 || consumed[0] != "name" {
		t.Fatalf("expected only 'name' consumed, got %v", consumed)
	}
}


