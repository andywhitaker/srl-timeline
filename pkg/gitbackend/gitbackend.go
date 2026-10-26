package gitbackend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"timeline/pkg/differ"
	"timeline/pkg/filter"
	"timeline/pkg/models"
	"timeline/pkg/normalizer"
)

const (
	DefaultRepoDir  = "/etc/opt/srlinux/timeline/repo"
	FallbackRepoDir = "/tmp/srlinux-timeline/repo"
)

var blameSHARegex = regexp.MustCompile(`^[0-9a-f]{40}\s+\d+\s+\d+`)

// ResolveSharedGID returns the numeric GID for ntwkadmin, ntwkuser, or srlinux so all
// authorized switch users (root, srlinux, linuxadmin, admin) share write access under 02775 + POSIX ACLs.
func ResolveSharedGID() int {
	for _, grpName := range []string{"ntwkadmin", "ntwkuser", "srlinux"} {
		if g, err := user.LookupGroup(grpName); err == nil && g != nil {
			if gid, convErr := strconv.Atoi(g.Gid); convErr == nil && gid > 0 {
				return gid
			}
		}
	}
	return -1
}

// ApplySharedDirPermissions sets 02775 (rwxrwsr-x), group ntwkadmin, and POSIX ACLs for ntwkuser/ntwkadmin/srlinux.
func ApplySharedDirPermissions(dirPath string) {
	_ = os.Chmod(dirPath, os.ModeSetgid|0775)
	if gid := ResolveSharedGID(); gid > 0 {
		_ = os.Chown(dirPath, -1, gid)
		_ = os.Chmod(dirPath, os.ModeSetgid|0775)
		if _, err := os.Stat("/usr/bin/setfacl"); err == nil {
			_ = exec.Command("/usr/bin/setfacl", "-m", "g::rwx,g:ntwkuser:rwx,g:ntwkadmin:rwx,g:srlinux:rwx", "-d", "-m", "g::rwx,g:ntwkuser:rwx,g:ntwkadmin:rwx,g:srlinux:rwx", dirPath).Run()
		}
	}
}

// ApplySharedFilePermissions sets 0664, group ntwkadmin, and POSIX ACLs for ntwkuser/ntwkadmin/srlinux.
func ApplySharedFilePermissions(filePath string) {
	_ = os.Chmod(filePath, 0664)
	if gid := ResolveSharedGID(); gid > 0 {
		_ = os.Chown(filePath, -1, gid)
		_ = os.Chmod(filePath, 0664)
		if _, err := os.Stat("/usr/bin/setfacl"); err == nil {
			_ = exec.Command("/usr/bin/setfacl", "-m", "g::rw,g:ntwkuser:rw,g:ntwkadmin:rw,g:srlinux:rw", filePath).Run()
		}
	}
}

// GitBackend manages atomic configuration commits, history extraction, blame, and sync.
type GitBackend struct {
	RepoDir          string
	RemoteConfigFile string

	mu              sync.RWMutex
	lastHeadSHA     string
	configCache     map[string]map[string]interface{}
	cliCache        map[string]string
	metaCache       map[string]map[string]interface{}
	timelineCache   []models.TimelineCommit
	blameCache      map[string][]models.BlameEntry
	commitDiffCache map[string]models.SemanticDiffResult
}

// NewGitBackend creates and initializes a GitBackend instance.
func NewGitBackend(repoDir string) *GitBackend {
	dir := repoDir
	if dir == "" {
		dir = determineRepoDir()
	}

	b := &GitBackend{
		RepoDir:          dir,
		RemoteConfigFile: filepath.Join(dir, ".timeline_remote.json"),
		configCache:      make(map[string]map[string]interface{}),
		cliCache:         make(map[string]string),
		metaCache:        make(map[string]map[string]interface{}),
		blameCache:       make(map[string][]models.BlameEntry),
		commitDiffCache:  make(map[string]models.SemanticDiffResult),
	}
	b.ensureRepo()
	return b
}

func determineRepoDir() string {
	if env := os.Getenv("TIMELINE_REPO_DIR"); env != "" {
		return env
	}
	if _, err := os.Stat("/etc/opt/srlinux"); err == nil {
		_ = os.MkdirAll("/etc/opt/srlinux/timeline", os.ModeSetgid|0775)
		ApplySharedDirPermissions("/etc/opt/srlinux/timeline")
		return DefaultRepoDir
	}
	if err := os.MkdirAll("/etc/opt/srlinux/timeline", os.ModeSetgid|0775); err == nil {
		ApplySharedDirPermissions("/etc/opt/srlinux/timeline")
		return DefaultRepoDir
	}
	return FallbackRepoDir
}

func (b *GitBackend) ClearCache() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configCache = make(map[string]map[string]interface{})
	b.cliCache = make(map[string]string)
	b.metaCache = make(map[string]map[string]interface{})
	b.timelineCache = nil
	b.blameCache = make(map[string][]models.BlameEntry)
	b.commitDiffCache = make(map[string]models.SemanticDiffResult)
}

func isRemoteGitOp(args []string) bool {
	for _, a := range args {
		switch a {
		case "push", "fetch", "pull", "ls-remote", "clone":
			return true
		}
	}
	return false
}

func (b *GitBackend) runGit(args []string, env map[string]string) (string, error) {
	if !hasGitBinary() && !isRemoteGitOp(args) && len(args) > 0 {
		switch args[0] {
		case "rev-parse":
			return b.resolveRevNative(args[len(args)-1])
		case "show":
			return b.showFileNative(args[len(args)-1])
		case "log":
			limit := 250
			for _, a := range args[1:] {
				if strings.HasPrefix(a, "--max-count=") {
					if n, err := strconv.Atoi(strings.TrimPrefix(a, "--max-count=")); err == nil && n > 0 {
						limit = n
					}
				}
			}
			return b.logLinesNative(limit)
		case "config", "add", "reset", "remote":
			return "", nil
		}
	}

	// Disable git hooks execution (-c core.hooksPath=/dev/null) to prevent privilege escalation
	fullArgs := append([]string{"-c", "safe.directory=*", "-c", "core.hooksPath=/dev/null", "-C", b.RepoDir}, args...)
	cmdEnv := os.Environ()
	if env != nil {
		for k, v := range env {
			cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
		}
	}

	var cmd *exec.Cmd
	// Only resolve SSH key, configure GIT_SSH_COMMAND, and enter srbase-mgmt netns for remote network operations
	if isRemoteGitOp(args) {
		sshCmd := b.getSSHCommand()
		if sshCmd != "" {
			hasSSH := false
			if env != nil {
				if _, exists := env["GIT_SSH_COMMAND"]; exists {
					hasSSH = true
				}
			}
			if !hasSSH {
				cmdEnv = append(cmdEnv, fmt.Sprintf("GIT_SSH_COMMAND=%s", sshCmd))
			}
		}
		if _, err := os.Stat("/var/run/netns/srbase-mgmt"); err == nil && os.Geteuid() == 0 {
			netnsArgs := append([]string{"netns", "exec", "srbase-mgmt", "git"}, fullArgs...)
			cmd = exec.Command("ip", netnsArgs...)
		}
	}
	if cmd == nil {
		cmd = exec.Command("git", fullArgs...)
	}

	cmd.Env = cmdEnv
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git %v failed: %s (%w)", args, strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ensureUserOwnedSSHKey ensures the returned private key is owned by the current effective UID
// with strict 0600 permissions required by OpenSSH, materializing a per-UID copy in /tmp when
// reading a group-shared key from /etc/opt/srlinux/timeline.
func ensureUserOwnedSSHKey(keyPath string, privData []byte) string {
	info, err := os.Stat(keyPath)
	if err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if int(stat.Uid) == os.Geteuid() && info.Mode().Perm() == 0600 {
				return keyPath
			}
		}
	}
	userKey := fmt.Sprintf("/tmp/timeline_id_ed25519_%d", os.Geteuid())
	if writeErr := os.WriteFile(userKey, privData, 0600); writeErr == nil {
		_ = os.Chmod(userKey, 0600)
		if pubData, pubErr := os.ReadFile(keyPath + ".pub"); pubErr == nil && len(pubData) > 0 {
			_ = os.WriteFile(userKey+".pub", pubData, 0644)
		}
		return userKey
	}
	return keyPath
}

// GetSSHKeyPath returns the path to the SSH private key used for remote operations.
func (b *GitBackend) GetSSHKeyPath() string {
	homeDir, _ := os.UserHomeDir()
	candidateKeys := []string{
		filepath.Join(b.RepoDir, "id_ed25519"),
		filepath.Join(filepath.Dir(b.RepoDir), "id_ed25519"),
		"/etc/opt/srlinux/timeline/id_ed25519",
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
		"/root/.ssh/id_ed25519",
		"/root/.ssh/id_rsa",
		"/home/srlinux/.ssh/id_ed25519",
	}

	for _, k := range candidateKeys {
		if info, err := os.Stat(k); err == nil && !info.IsDir() {
			if privData, readErr := os.ReadFile(k); readErr == nil && len(privData) > 0 {
				return ensureUserOwnedSSHKey(k, privData)
			}
		}
	}

	// Generate key if none exists
	keyDir := filepath.Dir(b.RepoDir)
	if keyDir == "" || keyDir == "." {
		keyDir = "/etc/opt/srlinux/timeline"
	}
	_ = os.MkdirAll(keyDir, os.ModeSetgid|0775)
	ApplySharedDirPermissions(keyDir)
	newKey := filepath.Join(keyDir, "id_ed25519")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", newKey, "-C", "srl-timeline@device")
	if err := cmd.Run(); err == nil {
		if ResolveSharedGID() > 0 {
			ApplySharedFilePermissions(newKey)
			ApplySharedFilePermissions(newKey + ".pub")
		} else {
			_ = os.Chmod(newKey, 0600)
			_ = os.Chmod(newKey+".pub", 0644)
		}
		if privData, readErr := os.ReadFile(newKey); readErr == nil && len(privData) > 0 {
			return ensureUserOwnedSSHKey(newKey, privData)
		}
		return newKey
	}

	return ""
}

// GetPublicSSHKey returns the public key string to display to the user.
func (b *GitBackend) GetPublicSSHKey() string {
	privKey := b.GetSSHKeyPath()
	if privKey == "" {
		return ""
	}
	pubKeyPath := privKey + ".pub"
	data, err := os.ReadFile(pubKeyPath)
	if err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func (b *GitBackend) getSSHCommand() string {
	keyPath := b.GetSSHKeyPath()
	var prefix string
	if _, err := os.Stat("/var/run/netns/srbase-mgmt"); err == nil && os.Geteuid() == 0 {
		prefix = "ip netns exec srbase-mgmt "
	}
	if keyPath != "" {
		return fmt.Sprintf("%sssh -i %s -o StrictHostKeyChecking=accept-new -o BatchMode=yes", prefix, keyPath)
	}
	return fmt.Sprintf("%sssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes", prefix)
}

func (b *GitBackend) ensureRepo() {
	_ = os.MkdirAll(b.RepoDir, os.ModeSetgid|0775)
	ApplySharedDirPermissions(b.RepoDir)
	if parent := filepath.Dir(b.RepoDir); parent != "" && parent != "/" {
		ApplySharedDirPermissions(parent)
	}
	gitDir := filepath.Join(b.RepoDir, ".git")
	if !hasGitBinary() {
		b.ensureRepoNative()
	} else if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		cmd := exec.Command("git", "-c", "safe.directory=*", "init", "--shared=group", "-b", "main", b.RepoDir)
		_ = cmd.Run()
		_, _ = b.runGit([]string{"config", "user.name", "SR Linux Timeline"}, nil)
		_, _ = b.runGit([]string{"config", "user.email", "timeline@srl-timeline"}, nil)
		_, _ = b.runGit([]string{"config", "commit.gpgSign", "false"}, nil)
		_, _ = b.runGit([]string{"config", "core.sharedRepository", "group"}, nil)
	} else {
		_, _ = b.runGit([]string{"config", "core.sharedRepository", "group"}, nil)
	}
	if gid := ResolveSharedGID(); gid > 0 {
		_ = filepath.Walk(b.RepoDir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			base := filepath.Base(p)
			if base == "id_ed25519" || base == "id_rsa" {
				return nil
			}
			_ = os.Chown(p, -1, gid)
			if info.IsDir() {
				_ = os.Chmod(p, os.ModeSetgid|0775)
			} else {
				_ = os.Chmod(p, info.Mode()|0060)
			}
			return nil
		})
		if _, err := os.Stat("/usr/bin/setfacl"); err == nil {
			_ = exec.Command("/usr/bin/setfacl", "-R", "-m", "g::rwX,g:ntwkuser:rwX,g:ntwkadmin:rwX,g:srlinux:rwX", b.RepoDir).Run()
		}
	}
}

// HasCommits returns true if the git repository has at least one commit.
func (b *GitBackend) HasCommits() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	out, err := b.runGit([]string{"rev-parse", "--verify", "HEAD"}, nil)
	return err == nil && strings.TrimSpace(out) != ""
}

// GetHeadSHA returns the current 40-character commit SHA of HEAD.
func (b *GitBackend) GetHeadSHA() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out, err := b.runGit([]string{"rev-parse", "--verify", "HEAD"}, nil)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// EnsureBaseline commits the initial baseline configuration if the repository has no commits yet.
func (b *GitBackend) EnsureBaseline(configDict map[string]interface{}) (bool, string, error) {
	if b.HasCommits() {
		return false, "", nil
	}
	if len(configDict) == 0 {
		return false, "", fmt.Errorf("no configuration provided for baseline")
	}
	return b.RecordConfigChange(
		configDict,
		"initial",
		"",
		"",
		time.Now().UTC(),
		"Initial baseline configuration",
		map[string]interface{}{"is_startup": true},
	)
}

// GetLatestCommitConfig returns the JSON configuration from HEAD.
func (b *GitBackend) GetLatestCommitConfig() map[string]interface{} {
	return b.GetConfigAtCommit("HEAD")
}

// RecordConfigChange commits a new configuration snapshot into the Git repository.
func (b *GitBackend) RecordConfigChange(
	configDict map[string]interface{},
	author string,
	commitID string,
	sessionID string,
	timestamp time.Time,
	message string,
	metadata map[string]interface{},
) (bool, string, error) {
	b.mu.Lock()

	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	if author == "" {
		author = "admin"
	}

	canonicalJSON, err := normalizer.CanonicalJSONString(configDict, 2)
	if err != nil {
		b.mu.Unlock()
		return false, "", err
	}
	cliText := normalizer.FlatCLIString(configDict)

	// Compare with previous config
	prevCfg := b.getConfigAtCommitInternal("HEAD")
	diffRes := differ.SemanticDiff(prevCfg, configDict, "")

	if len(prevCfg) > 0 && !diffRes.HasChanges {
		headSHA, _ := b.runGit([]string{"rev-parse", "HEAD"}, nil)
		b.mu.Unlock()
		return false, headSHA, nil
	}

	// Write files
	configFile := filepath.Join(b.RepoDir, "config.json")
	cliFile := filepath.Join(b.RepoDir, "config.cli")
	metaFile := filepath.Join(b.RepoDir, "metadata.json")

	_ = os.WriteFile(configFile, []byte(canonicalJSON), 0664)
	ApplySharedFilePermissions(configFile)
	_ = os.WriteFile(cliFile, []byte(cliText), 0664)
	ApplySharedFilePermissions(cliFile)

	meta := make(map[string]interface{})
	if metadata != nil {
		for k, v := range metadata {
			meta[k] = v
		}
	}
	meta["author"] = author
	meta["srl_commit_id"] = commitID
	meta["session_id"] = sessionID
	meta["timestamp"] = timestamp.Format(time.RFC3339)
	meta["diff_summary"] = diffRes.StatBadge()

	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(metaFile, metaJSON, 0664)
	ApplySharedFilePermissions(metaFile)

	if message == "" {
		message = fmt.Sprintf("Config commit by %s", author)
		if commitID != "" {
			message += fmt.Sprintf(" (SRL commit #%s)", commitID)
		}
		if diffRes.HasChanges {
			message += fmt.Sprintf(" [%s]", diffRes.StatBadge())
		}
		if comment, ok := meta["comment"].(string); ok && comment != "" {
			message += fmt.Sprintf(" — %s", comment)
		}
	}

	var sha string
	if !hasGitBinary() {
		sha, err = b.commitNative(canonicalJSON, cliText, metaJSON, author, message, timestamp)
		if err != nil {
			b.mu.Unlock()
			return false, "", err
		}
	} else {
		// Stage
		_, err = b.runGit([]string{"add", "config.json", "config.cli", "metadata.json"}, nil)
		if err != nil {
			b.mu.Unlock()
			return false, "", err
		}

		// Verify that at least config.json or config.cli has staged changes against HEAD
		_, errHead := b.runGit([]string{"rev-parse", "--verify", "HEAD"}, nil)
		if errHead == nil {
			stagedDiff, _ := b.runGit([]string{"diff", "--cached", "--name-only", "config.json", "config.cli"}, nil)
			if strings.TrimSpace(stagedDiff) == "" {
				_, _ = b.runGit([]string{"reset", "HEAD"}, nil)
				headSHA, _ := b.runGit([]string{"rev-parse", "HEAD"}, nil)
				b.mu.Unlock()
				return false, headSHA, nil
			}
		}

		authorEmail := fmt.Sprintf("%s@srl-timeline", author)
		authorStr := fmt.Sprintf("%s <%s>", author, authorEmail)
		dateStr := timestamp.Format("2006-01-02 15:04:05 -0700")

		env := map[string]string{
			"GIT_AUTHOR_NAME":     author,
			"GIT_AUTHOR_EMAIL":    authorEmail,
			"GIT_AUTHOR_DATE":     dateStr,
			"GIT_COMMITTER_NAME":  "SR Linux Timeline",
			"GIT_COMMITTER_EMAIL": "timeline@srl-timeline",
			"GIT_COMMITTER_DATE":  dateStr,
		}

		_, err = b.runGit([]string{"commit", "-m", message, "--author", authorStr}, env)
		if err != nil {
			b.mu.Unlock()
			return false, "", err
		}

		sha, _ = b.runGit([]string{"rev-parse", "HEAD"}, nil)
	}

	if ResolveSharedGID() > 0 {
		if _, err := os.Stat("/usr/bin/setfacl"); err == nil {
			_ = exec.Command("/usr/bin/setfacl", "-R", "-m", "g::rwX,g:ntwkuser:rwX,g:ntwkadmin:rwX,g:srlinux:rwX", b.RepoDir).Run()
		}
	}

	// Invalidate caches
	b.configCache = make(map[string]map[string]interface{})
	b.cliCache = make(map[string]string)
	b.metaCache = make(map[string]map[string]interface{})
	b.timelineCache = nil
	b.blameCache = make(map[string][]models.BlameEntry)
	b.commitDiffCache = make(map[string]models.SemanticDiffResult)

	remoteCfg := b.loadRemoteConfigLocked()
	b.mu.Unlock()

	// Auto-push if enabled
	if remoteCfg.AutoPush && remoteCfg.URL != "" {
		_ = b.PushRemote()
	}

	return true, sha, nil
}

// GetTimeline retrieves the list of commits in reverse chronological order.
func (b *GitBackend) GetTimeline(limit int, filterPath string) []models.TimelineCommit {
	b.mu.Lock()
	currentHead, _ := b.runGit([]string{"rev-parse", "--verify", "HEAD"}, nil)
	cacheLimit := 250
	if limit > cacheLimit {
		cacheLimit = limit
	}
	if b.timelineCache == nil || currentHead != b.lastHeadSHA || (limit > len(b.timelineCache) && len(b.timelineCache) >= 250) {
		b.loadTimelineCache(cacheLimit)
		b.lastHeadSHA = currentHead
	}

	allCommits := append([]models.TimelineCommit{}, b.timelineCache...)
	b.mu.Unlock()

	cleanFilter := strings.TrimSpace(filterPath)
	if cleanFilter == "" {
		if limit > 0 && len(allCommits) > limit {
			return allCommits[:limit]
		}
		return allCommits
	}

	var filtered []models.TimelineCommit
	for i, c := range allCommits {
		parentSHA := ""
		if i+1 < len(allCommits) {
			parentSHA = allCommits[i+1].FullSHA
		}
		if b.commitMatchesFilterWithParent(c.FullSHA, parentSHA, cleanFilter) {
			filtered = append(filtered, c)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}
	return filtered
}

func (b *GitBackend) loadTimelineCache(limit int) {
	if limit < 250 {
		limit = 250
	}
	out, err := b.runGit([]string{"rev-parse", "--verify", "HEAD"}, nil)
	if err != nil || out == "" {
		b.timelineCache = []models.TimelineCommit{}
		return
	}

	logFormat := "%H|%an|%aI|%s"
	out, err = b.runGit([]string{"log", fmt.Sprintf("--max-count=%d", limit), fmt.Sprintf("--pretty=format:%s", logFormat)}, nil)
	if err != nil {
		b.timelineCache = []models.TimelineCommit{}
		return
	}

	lines := strings.Split(out, "\n")
	var commits []models.TimelineCommit
	isFirst := true

	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		parts := strings.SplitN(l, "|", 4)
		if len(parts) < 4 {
			continue
		}
		fullSHA, author, dateISO, msg := parts[0], parts[1], parts[2], parts[3]
		dt, parseErr := time.Parse(time.RFC3339, dateISO)
		if parseErr != nil {
			dt = time.Now().UTC()
		}

		meta := b.getCommitMetadataInternal(fullSHA)
		srlCommitID, _ := meta["srl_commit_id"].(string)
		sessionID, _ := meta["session_id"].(string)
		diffStat, _ := meta["diff_summary"].(string)
		isStartup, _ := meta["is_startup"].(bool)
		isRestored, _ := meta["is_restored"].(bool)
		if !isRestored && meta["cherry_picked_from"] != nil {
			isRestored = true
		}

		commitID := fullSHA
		if len(commitID) > 8 {
			commitID = commitID[:8]
		}

		commitObj := models.TimelineCommit{
			CommitID:    commitID,
			FullSHA:     fullSHA,
			Timestamp:   dt,
			Author:      author,
			Message:     msg,
			SRLCommitID: srlCommitID,
			SessionID:   sessionID,
			DiffStat:    diffStat,
			IsCurrent:   isFirst,
			IsStartup:   isStartup,
			IsRestored:  isRestored,
			Metadata:    meta,
		}
		isFirst = false
		commits = append(commits, commitObj)
	}

	b.timelineCache = commits
}

func (b *GitBackend) commitMatchesFilter(commitSHA, filterQuery string) bool {
	return b.commitMatchesFilterWithParent(commitSHA, "", filterQuery)
}

func (b *GitBackend) commitMatchesFilterWithParent(commitSHA, knownParentSHA, filterQuery string) bool {
	b.mu.Lock()
	baseDiff, found := b.commitDiffCache[commitSHA]
	if !found {
		parentSHA := knownParentSHA
		if parentSHA == "" {
			resolved, err := b.runGit([]string{"rev-parse", "--verify", fmt.Sprintf("%s~1", commitSHA)}, nil)
			if err == nil {
				parentSHA = strings.TrimSpace(resolved)
			}
		}
		if parentSHA == "" {
			// Initial commit
			cfg := b.getConfigAtCommitInternal(commitSHA)
			baseDiff = differ.SemanticDiffFast(map[string]interface{}{}, cfg)
		} else {
			cfgCurr := b.getConfigAtCommitInternal(commitSHA)
			cfgPrev := b.getConfigAtCommitInternal(parentSHA)
			baseDiff = differ.SemanticDiffFast(cfgPrev, cfgCurr)
		}
		b.commitDiffCache[commitSHA] = baseDiff
	}
	b.mu.Unlock()

	filteredDiff := filter.FilterDiffResult(baseDiff, filterQuery)
	return filteredDiff.HasChanges
}

// GetConfigAtCommit fetches the configuration JSON map at a specific commit SHA (cached).
func (b *GitBackend) GetConfigAtCommit(commitSHA string) map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.getConfigAtCommitInternal(commitSHA)
}

func (b *GitBackend) getConfigAtCommitInternal(commitSHA string) map[string]interface{} {
	realSHA := commitSHA
	if len(commitSHA) < 40 || commitSHA == "HEAD" || strings.Contains(commitSHA, "~") || strings.Contains(commitSHA, "^") {
		resolved, err := b.runGit([]string{"rev-parse", commitSHA}, nil)
		if err == nil && resolved != "" {
			realSHA = resolved
		} else {
			return make(map[string]interface{})
		}
	}

	if cached, ok := b.configCache[realSHA]; ok {
		return cached
	}

	out, err := b.runGit([]string{"show", fmt.Sprintf("%s:config.json", realSHA)}, nil)
	if err == nil && out != "" {
		var m map[string]interface{}
		if json.Unmarshal([]byte(out), &m) == nil {
			b.configCache[realSHA] = m
			return m
		}
	}
	return make(map[string]interface{})
}

// GetCLIAtCommit fetches the flat CLI text at a specific commit SHA (cached).
func (b *GitBackend) GetCLIAtCommit(commitSHA string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	realSHA := commitSHA
	if len(commitSHA) < 40 || commitSHA == "HEAD" || strings.Contains(commitSHA, "~") || strings.Contains(commitSHA, "^") {
		resolved, err := b.runGit([]string{"rev-parse", commitSHA}, nil)
		if err == nil && resolved != "" {
			realSHA = resolved
		}
	}

	if cached, ok := b.cliCache[realSHA]; ok {
		return cached
	}

	out, err := b.runGit([]string{"show", fmt.Sprintf("%s:config.cli", realSHA)}, nil)
	if err == nil {
		b.cliCache[realSHA] = out
		return out
	}
	return ""
}

// GetCommitMetadata fetches metadata JSON stored at a specific commit SHA (cached).
func (b *GitBackend) GetCommitMetadata(commitSHA string) map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.getCommitMetadataInternal(commitSHA)
}

func (b *GitBackend) getCommitMetadataInternal(commitSHA string) map[string]interface{} {
	realSHA := commitSHA
	if len(commitSHA) < 40 || commitSHA == "HEAD" || strings.Contains(commitSHA, "~") || strings.Contains(commitSHA, "^") {
		resolved, err := b.runGit([]string{"rev-parse", commitSHA}, nil)
		if err == nil && resolved != "" {
			realSHA = resolved
		}
	}

	if cached, ok := b.metaCache[realSHA]; ok {
		return cached
	}

	out, err := b.runGit([]string{"show", fmt.Sprintf("%s:metadata.json", realSHA)}, nil)
	if err == nil && out != "" {
		var m map[string]interface{}
		if json.Unmarshal([]byte(out), &m) == nil {
			b.metaCache[realSHA] = m
			return m
		}
	}
	return make(map[string]interface{})
}

// GetBlameEntries runs git blame on HEAD to track line-by-line attribution (cached).
func (b *GitBackend) GetBlameEntries(mode string) []models.BlameEntry {
	return b.GetBlameEntriesAtCommit(mode, "HEAD")
}

// GetBlameEntriesAtCommit runs git blame at a specific commit SHA (cached).
func (b *GitBackend) GetBlameEntriesAtCommit(mode, commitSHA string) []models.BlameEntry {
	b.mu.Lock()
	defer b.mu.Unlock()

	if commitSHA == "" {
		commitSHA = "HEAD"
	}
	realSHA := commitSHA
	if len(commitSHA) < 40 || commitSHA == "HEAD" || strings.Contains(commitSHA, "~") || strings.Contains(commitSHA, "^") {
		if resolved, err := b.runGit([]string{"rev-parse", commitSHA}, nil); err == nil && resolved != "" {
			realSHA = resolved
		}
	}

	cacheKey := fmt.Sprintf("%s:%s", mode, realSHA)
	if cached, ok := b.blameCache[cacheKey]; ok {
		return cached
	}

	filename := "config.cli"
	if mode == "json" {
		filename = "config.json"
	}

	targetPath := filepath.Join(b.RepoDir, filename)
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return []models.BlameEntry{}
	}

	if !hasGitBinary() {
		entries := b.blameNative(realSHA, filename)
		b.blameCache[cacheKey] = entries
		return entries
	}

	out, err := b.runGit([]string{"blame", "--line-porcelain", realSHA, "--", filename}, nil)
	if err != nil {
		return []models.BlameEntry{}
	}

	lines := strings.Split(out, "\n")
	var entries []models.BlameEntry

	var currSHA, currAuthor, currSummary string
	var currTime time.Time
	var currLineNo int

	for _, l := range lines {
		if blameSHARegex.MatchString(l) {
			parts := strings.Fields(l)
			currSHA = parts[0]
			currLineNo, _ = strconv.Atoi(parts[2])
		} else if strings.HasPrefix(l, "author ") {
			currAuthor = strings.TrimPrefix(l, "author ")
		} else if strings.HasPrefix(l, "author-time ") {
			epoch, err := strconv.ParseInt(strings.TrimPrefix(l, "author-time "), 10, 64)
			if err == nil {
				currTime = time.Unix(epoch, 0).UTC()
			}
		} else if strings.HasPrefix(l, "summary ") {
			currSummary = strings.TrimPrefix(l, "summary ")
		} else if strings.HasPrefix(l, "\t") {
			content := strings.TrimPrefix(l, "\t")
			entries = append(entries, models.BlameEntry{
				LineNumber:    currLineNo,
				Content:       content,
				CommitSHA:     currSHA,
				Author:        currAuthor,
				Timestamp:     currTime,
				CommitMessage: currSummary,
			})
		}
	}

	b.blameCache[cacheKey] = entries
	return entries
}

// LoadRemoteConfig loads saved remote Git settings.
func (b *GitBackend) LoadRemoteConfig() models.RemoteRepoConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loadRemoteConfigLocked()
}

func (b *GitBackend) loadRemoteConfigLocked() models.RemoteRepoConfig {
	data, err := os.ReadFile(b.RemoteConfigFile)
	if err != nil {
		return models.RemoteRepoConfig{Branch: "main"}
	}
	var cfg models.RemoteRepoConfig
	_ = json.Unmarshal(data, &cfg)
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	return cfg
}

// SaveRemoteConfig persists remote Git settings.
func (b *GitBackend) SaveRemoteConfig(cfg models.RemoteRepoConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(b.RemoteConfigFile, data, 0664); err != nil {
		return err
	}
	ApplySharedFilePermissions(b.RemoteConfigFile)

	// Configure git remote
	if cfg.URL != "" {
		_, _ = b.runGit([]string{"remote", "remove", "origin"}, nil)
		_, err = b.runGit([]string{"remote", "add", "origin", cfg.URL}, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// PushRemote pushes local commits to the configured remote repository.
func (b *GitBackend) PushRemote() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	cfg := b.loadRemoteConfigLocked()
	if cfg.URL == "" {
		return fmt.Errorf("no remote URL configured")
	}
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}
	_, err := b.runGit([]string{"push", "-u", "origin", branch}, nil)
	return err
}

// CheckRemoteSyncStatus checks if local branch is in sync with origin.
func (b *GitBackend) CheckRemoteSyncStatus() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	cfg := b.loadRemoteConfigLocked()
	if cfg.URL == "" {
		return "Not Configured"
	}
	_, err := b.runGit([]string{"fetch", "origin", cfg.Branch}, nil)
	if err != nil {
		return "Fetch Failed"
	}
	local, _ := b.runGit([]string{"rev-parse", "HEAD"}, nil)
	remote, _ := b.runGit([]string{"rev-parse", fmt.Sprintf("origin/%s", cfg.Branch)}, nil)
	if local != "" && local == remote {
		return "Synced"
	}
	return "Unsynced"
}
