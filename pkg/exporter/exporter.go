package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"timeline/pkg/gitbackend"
	"timeline/pkg/normalizer"
	"timeline/pkg/srlclient"
)

// ConfigExporter exports configurations from any point in time.
type ConfigExporter struct {
	GitBackend *gitbackend.GitBackend
	SRLClient  *srlclient.SRLClient
}

// NewConfigExporter creates a new exporter.
func NewConfigExporter(backend *gitbackend.GitBackend, client *srlclient.SRLClient) *ConfigExporter {
	if backend == nil {
		backend = gitbackend.NewGitBackend("")
	}
	if client == nil {
		client = srlclient.NewSRLClient()
	}
	return &ConfigExporter{
		GitBackend: backend,
		SRLClient:  client,
	}
}

// GetConfigDict retrieves the config map at a specific commit or running config.
func (e *ConfigExporter) GetConfigDict(commitSHA string) map[string]interface{} {
	if commitSHA == "running" || commitSHA == "live" {
		cfg, err := e.SRLClient.GetRunningConfig("/")
		if err == nil && len(cfg) > 0 {
			return cfg
		}
	}
	if commitSHA != "" {
		cfg := e.GitBackend.GetConfigAtCommit(commitSHA)
		if len(cfg) > 0 {
			return cfg
		}
	}
	latest := e.GitBackend.GetLatestCommitConfig()
	if len(latest) > 0 {
		return latest
	}
	cfg, _ := e.SRLClient.GetRunningConfig("/")
	return cfg
}

// ExportAsStartupJSON returns JSON formatted for /etc/opt/srlinux/config.json.
func (e *ConfigExporter) ExportAsStartupJSON(commitSHA, outputFile string) (string, error) {
	cfg := e.GetConfigDict(commitSHA)
	jsonStr, err := normalizer.CanonicalJSONString(cfg, 2)
	if err != nil {
		return "", err
	}

	if outputFile != "" {
		_ = os.MkdirAll(filepath.Dir(outputFile), 0755)
		if err := os.WriteFile(outputFile, []byte(jsonStr), 0644); err != nil {
			return jsonStr, err
		}
	}
	return jsonStr, nil
}

// ExportAsFlatCLI returns flat CLI set statements.
func (e *ConfigExporter) ExportAsFlatCLI(commitSHA, outputFile string) (string, error) {
	cfg := e.GetConfigDict(commitSHA)
	cliStr := normalizer.FlatCLIString(cfg)

	if outputFile != "" {
		_ = os.MkdirAll(filepath.Dir(outputFile), 0755)
		if err := os.WriteFile(outputFile, []byte(cliStr), 0644); err != nil {
			return cliStr, err
		}
	}
	return cliStr, nil
}

// SaveAsDeviceStartupConfig saves directly to /etc/opt/srlinux/config.json on the switch.
func (e *ConfigExporter) SaveAsDeviceStartupConfig(commitSHA string) (bool, string, error) {
	targetFile := "/etc/opt/srlinux/config.json"
	_, err := e.ExportAsStartupJSON(commitSHA, targetFile)
	if err != nil {
		return false, "", fmt.Errorf("failed to write startup config: %w", err)
	}
	return true, fmt.Sprintf("Successfully saved revision %s as device startup config (%s)", commitSHA, targetFile), nil
}
