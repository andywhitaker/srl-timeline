package restorer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"timeline/pkg/daemon"
	"timeline/pkg/differ"
	"timeline/pkg/filter"
	"timeline/pkg/gitbackend"
	"timeline/pkg/models"
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
		"is_restored":       true,
		"restored_from_sha": targetCommitSHA,
	}

	// 2. Record timeline commit synchronously under the record lock
	unlock := daemon.AcquireRecordLock()
	defer unlock()

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

// BuildCherryPickCLIStatements generates the ordered "delete / ..." and "set / ..." CLI statements
// needed to cherry-pick the specified paths from targetCommitSHA.
func (r *ConfigRestorer) BuildCherryPickCLIStatements(targetCommitSHA string, paths ...string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no cherry-pick paths specified")
	}
	targetCfg := r.GitBackend.GetConfigAtCommit(targetCommitSHA)
	if len(targetCfg) == 0 {
		return nil, fmt.Errorf("commit %s contains no configuration", targetCommitSHA)
	}
	parentCfg := r.GitBackend.GetConfigAtCommit(fmt.Sprintf("%s~1", targetCommitSHA))
	headCfg := r.GitBackend.GetLatestCommitConfig()

	var deletePaths []string
	var setStatements []string
	seenDeletes := make(map[string]bool)
	seenSets := make(map[string]bool)

	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		subtree := filter.FilterConfigSubtree(targetCfg, p)
		diffRes := differ.SemanticDiff(parentCfg, targetCfg, p)
		headDiffRes := differ.SemanticDiff(headCfg, targetCfg, p)

		// Collect deletions from commit delta and from current HEAD under path p
		var pathDeletes []string
		for _, c := range append(diffRes.Changes, headDiffRes.Changes...) {
			if c.DiffType == models.DiffDeleted {
				if len(filter.FilterConfigSubtree(targetCfg, c.Path)) == 0 {
					cliDelPath := filter.XPathToCLIPath(c.Path)
					if cliDelPath != "" && !seenDeletes[cliDelPath] {
						seenDeletes[cliDelPath] = true
						pathDeletes = append(pathDeletes, cliDelPath)
					}
				}
			}
		}

		// If p itself is absent in targetCfg and existed in parentCfg or headCfg, emit delete for p
		if len(subtree) == 0 && len(pathDeletes) == 0 {
			if len(filter.FilterConfigSubtree(parentCfg, p)) > 0 || len(filter.FilterConfigSubtree(headCfg, p)) > 0 {
				cliDelPath := filter.XPathToCLIPath(p)
				if cliDelPath != "" && !seenDeletes[cliDelPath] {
					seenDeletes[cliDelPath] = true
					pathDeletes = append(pathDeletes, cliDelPath)
				}
			}
		}

		deletePaths = append(deletePaths, pathDeletes...)

		if len(subtree) > 0 {
			stmts := normalizer.JSONToFlatCLI(subtree, "")
			for _, s := range stmts {
				if !seenSets[s] {
					seenSets[s] = true
					setStatements = append(setStatements, s)
				}
			}
		}
	}

	// Prune redundant child delete paths when a parent container/list item is already deleted
	sort.Strings(deletePaths)
	var prunedDeletes []string
	for _, dp := range deletePaths {
		redundant := false
		for _, parent := range prunedDeletes {
			if strings.HasPrefix(dp, parent+" ") {
				redundant = true
				break
			}
		}
		if !redundant {
			prunedDeletes = append(prunedDeletes, dp)
		}
	}

	var allCLIStatements []string
	for _, dp := range prunedDeletes {
		allCLIStatements = append(allCLIStatements, fmt.Sprintf("delete / %s", dp))
	}
	allCLIStatements = append(allCLIStatements, setStatements...)

	if len(allCLIStatements) == 0 {
		return nil, fmt.Errorf("no configuration statements generated for specified paths %v in revision %s", paths, targetCommitSHA)
	}
	return allCLIStatements, nil
}

// CherryPickRestore applies one or more subtree/path configurations from a commit to the live switch.
func (r *ConfigRestorer) CherryPickRestore(targetCommitSHA string, paths ...string) (bool, string, error) {
	allCLIStatements, err := r.BuildCherryPickCLIStatements(targetCommitSHA, paths...)
	if err != nil {
		return false, "", err
	}

	// 1. Apply to device first (ExecuteCLI wraps statements in an isolated timeline-tx candidate transaction)
	ok, outMsg, err := r.SRLClient.ExecuteCLI(allCLIStatements)
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
		"is_restored":        true,
		"cherry_picked_from": targetCommitSHA,
		"cherry_pick_paths":  paths,
	}

	// 2. Synchronously record the new running state in timeline git under the record lock
	unlock := daemon.AcquireRecordLock()
	defer unlock()

	verifiedCfg, fetchErr := r.SRLClient.GetRunningConfig("/")
	if fetchErr == nil && len(verifiedCfg) > 0 {
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

	return true, fmt.Sprintf("Successfully cherry-picked %s to device from %s", pathSummary, shortSHA), nil
}
