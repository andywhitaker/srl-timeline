package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"timeline/pkg/gitbackend"
)

func TestConfigExporter(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "exporter_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")
	backend := gitbackend.NewGitBackend(repoDir)

	cfg := map[string]interface{}{
		"system": map[string]interface{}{
			"name": map[string]interface{}{"host-name": "srl-test-node"},
		},
	}
	_, _, err = backend.RecordConfigChange(cfg, "admin", "1", "", time.Now().UTC(), "Initial", nil)
	if err != nil {
		t.Fatal(err)
	}

	exp := NewConfigExporter(backend, nil)

	jsonOut, err := exp.ExportAsStartupJSON("HEAD", "")
	if err != nil {
		t.Fatalf("failed export json: %v", err)
	}
	if !strings.Contains(jsonOut, "srl-test-node") {
		t.Fatalf("expected srl-test-node in json")
	}

	cliOut, err := exp.ExportAsFlatCLI("HEAD", "")
	if err != nil {
		t.Fatalf("failed export cli: %v", err)
	}
	if !strings.Contains(cliOut, "set / system name host-name srl-test-node") {
		t.Fatalf("expected cli statement, got: %s", cliOut)
	}
}
