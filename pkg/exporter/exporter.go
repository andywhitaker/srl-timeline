package exporter

import (
	"encoding/json"
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

// SaveAsDeviceStartupConfig saves the revision as the SR Linux startup configuration.
// When saving the active/latest configuration on-box, it uses SR Linux's native "/tools/system/configuration/save"
// JSON-RPC action so /etc/opt/srlinux/config.json retains its _preamble, module prefixes, and POSIX ACLs.
func (e *ConfigExporter) SaveAsDeviceStartupConfig(commitSHA string) (bool, string, error) {
	targetFile := "/etc/opt/srlinux/config.json"

	isLatest := commitSHA == "" || commitSHA == "HEAD" || commitSHA == "running" || commitSHA == "live"
	if !isLatest && e.GitBackend != nil {
		headSHA := e.GitBackend.GetHeadSHA()
		if headSHA != "" && (commitSHA == headSHA || (len(commitSHA) >= 7 && len(headSHA) >= len(commitSHA) && headSHA[:len(commitSHA)] == commitSHA)) {
			isLatest = true
		}
	}

	if isLatest && e.SRLClient != nil && e.SRLClient.IsOnBox {
		if ok, err := e.SRLClient.SaveStartupConfig(); err == nil && ok {
			return true, fmt.Sprintf("Successfully saved revision %s as device startup config (%s)", commitSHA, targetFile), nil
		}
	}

	// For historical revisions or off-box fallback, write JSON while preserving existing _preamble and file mode/ACLs if present
	rawCfg := e.GetConfigDict(commitSHA)
	cfg := make(map[string]interface{}, len(rawCfg)+1)
	for k, v := range rawCfg {
		cfg[k] = v
	}
	if existingBytes, err := os.ReadFile(targetFile); err == nil && len(existingBytes) > 0 {
		var existingMap map[string]interface{}
		if json.Unmarshal(existingBytes, &existingMap) == nil {
			if preamble, ok := existingMap["_preamble"]; ok {
				cfg["_preamble"] = preamble
			}
		}
	}

	jsonStr, err := normalizer.CanonicalJSONString(cfg, 2)
	if err != nil {
		return false, "", fmt.Errorf("failed to serialize startup config: %w", err)
	}

	_ = os.MkdirAll(filepath.Dir(targetFile), 0755)
	f, err := os.OpenFile(targetFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0664)
	if err != nil {
		return false, "", fmt.Errorf("failed to write startup config: %w", err)
	}
	_, writeErr := f.Write([]byte(jsonStr))
	closeErr := f.Close()
	if writeErr != nil {
		return false, "", fmt.Errorf("failed to write startup config: %w", writeErr)
	}
	if closeErr != nil {
		return false, "", fmt.Errorf("failed to close startup config: %w", closeErr)
	}
	return true, fmt.Sprintf("Successfully saved revision %s as device startup config (%s)", commitSHA, targetFile), nil
}
