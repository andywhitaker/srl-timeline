package restorer

import (
	"fmt"
	"strings"
	"time"

	"timeline/pkg/filter"
	"timeline/pkg/gitbackend"
	"timeline/pkg/normalizer"
	"timeline/pkg/srlclient"
)

// ConfigRestorer implements device-first full and cherry-pick configuration restoration.
type ConfigRestorer struct {
	SRLClient  *srlclient.SRLClient
	GitBackend *gitbackend.GitBackend
}

// NewConfigRestorer creates a new restorer.
func NewConfigRestorer(client *srlclient.SRLClient, backend *gitbackend.GitBackend) *ConfigRestorer {
	if client == nil {
		client = srlclient.NewSRLClient()
	}
	if backend == nil {
		backend = gitbackend.NewGitBackend("")
	}
	return &ConfigRestorer{
		SRLClient:  client,
		GitBackend: backend,
	}
}

// RestoreFullConfig restores the switch configuration to a specific commit SHA (device-first).
func (r *ConfigRestorer) RestoreFullConfig(targetCommitSHA string) (bool, string, error) {
	targetCfg := r.GitBackend.GetConfigAtCommit(targetCommitSHA)
	if len(targetCfg) == 0 {
		return false, "", fmt.Errorf("target commit %s contains no configuration data", targetCommitSHA)
	}

	// 1. Apply to the network device first (Source of Truth)
	success, err := r.SRLClient.ReplaceFullConfig(targetCfg)
	if err != nil || !success {
		return false, "", fmt.Errorf("device failed to apply configuration restore: %w", err)
	}

	shortSHA := targetCommitSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	msg := fmt.Sprintf("Restored full configuration to revision %s", shortSHA)
	meta := map[string]interface{}{
		"is_restored":        true,
		"restored_from_sha": targetCommitSHA,
	}

	// 2. Record timeline commit using target configuration
	_, newSHA, recErr := r.GitBackend.RecordConfigChange(
		targetCfg,
		"admin",
		"",
		"",
		time.Now().UTC(),
		msg,
		meta,
	)
	if recErr != nil {
		return true, fmt.Sprintf("Restored on switch, but timeline record failed: %v", recErr), nil
	}

	newShort := newSHA
	if len(newShort) > 8 {
		newShort = newShort[:8]
	}
	return true, fmt.Sprintf("Successfully restored device configuration to %s (New commit: %s)", shortSHA, newShort), nil
}

// CherryPickRestore applies one or more subtree/path configurations from a commit to the live switch.
func (r *ConfigRestorer) CherryPickRestore(targetCommitSHA string, paths ...string) (bool, string, error) {
	if len(paths) == 0 {
		return false, "", fmt.Errorf("no cherry-pick paths specified")
	}
	targetCfg := r.GitBackend.GetConfigAtCommit(targetCommitSHA)
	if len(targetCfg) == 0 {
		return false, "", fmt.Errorf("commit %s contains no configuration", targetCommitSHA)
	}

	var allCLIStatements []string
	seenStmts := make(map[string]bool)

	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		subtree := filter.FilterConfigSubtree(targetCfg, p)
		if len(subtree) == 0 {
			continue
		}
		stmts := normalizer.JSONToFlatCLI(subtree, "")
		for _, s := range stmts {
			if !seenStmts[s] {
				seenStmts[s] = true
				allCLIStatements = append(allCLIStatements, s)
			}
		}
	}

	if len(allCLIStatements) == 0 {
		return false, "", fmt.Errorf("no configuration statements generated for specified paths %v in revision %s", paths, targetCommitSHA)
	}

	// 1. Prepare transactional CLI statements
	cliCommands := []string{"enter candidate"}
	cliCommands = append(cliCommands, allCLIStatements...)
	cliCommands = append(cliCommands, "commit save", "quit")

	// 2. Apply to device first
	ok, outMsg, err := r.SRLClient.ExecuteCLI(cliCommands)
	if err != nil || !ok {
		return false, "", fmt.Errorf("failed to apply cherry-pick to device: %v (CLI output: %s)", err, outMsg)
	}

	shortSHA := targetCommitSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	var pathSummary string
	if len(paths) == 1 {
		pathSummary = fmt.Sprintf("'%s'", paths[0])
	} else if len(paths) <= 2 {
		pathSummary = fmt.Sprintf("'%s'", strings.Join(paths, "', '"))
	} else {
		pathSummary = fmt.Sprintf("%d paths ('%s', '%s', ...)", len(paths), paths[0], paths[1])
	}

	msg := fmt.Sprintf("Cherry-pick restored %s from %s", pathSummary, shortSHA)
	meta := map[string]interface{}{
		"cherry_picked_from": targetCommitSHA,
		"cherry_pick_paths":  paths,
	}

	// 3. Asynchronously record the new running state in timeline git
	go func() {
		time.Sleep(100 * time.Millisecond)
		verifiedCfg, err := r.SRLClient.GetRunningConfig("/")
		if err == nil && len(verifiedCfg) > 0 {
			_, _, _ = r.GitBackend.RecordConfigChange(
				verifiedCfg,
				"admin",
				"",
				"",
				time.Now().UTC(),
				msg,
				meta,
			)
		}
	}()

	return true, fmt.Sprintf("Successfully cherry-picked %s to device from %s", pathSummary, shortSHA), nil
}
