package gitbackend

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"timeline/pkg/models"
)

func hasGitBinary() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func (b *GitBackend) ensureRepoNative() {
	gitDir := filepath.Join(b.RepoDir, ".git")
	_ = os.MkdirAll(filepath.Join(gitDir, "objects"), os.ModeSetgid|0775)
	_ = os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), os.ModeSetgid|0775)
	headFile := filepath.Join(gitDir, "HEAD")
	if _, err := os.Stat(headFile); os.IsNotExist(err) {
		_ = os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0664)
		ApplySharedFilePermissions(headFile)
	}
	configFile := filepath.Join(gitDir, "config")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		cfgContent := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n\tsharedRepository = group\n"
		_ = os.WriteFile(configFile, []byte(cfgContent), 0664)
		ApplySharedFilePermissions(configFile)
	}
}

func (b *GitBackend) writeGitObject(objType string, payload []byte) (string, error) {
	header := fmt.Sprintf("%s %d\x00", objType, len(payload))
	raw := make([]byte, 0, len(header)+len(payload))
	raw = append(raw, []byte(header)...)
	raw = append(raw, payload...)

	sum := sha1.Sum(raw)
	shaHex := hex.EncodeToString(sum[:])

	objDir := filepath.Join(b.RepoDir, ".git", "objects", shaHex[:2])
	if err := os.MkdirAll(objDir, os.ModeSetgid|0775); err != nil {
		return "", err
	}
	ApplySharedDirPermissions(objDir)

	objPath := filepath.Join(objDir, shaHex[2:])
	if _, err := os.Stat(objPath); err == nil {
		return shaHex, nil
	}

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}

	if err := os.WriteFile(objPath, compressed.Bytes(), 0664); err != nil {
		return "", err
	}
	ApplySharedFilePermissions(objPath)
	return shaHex, nil
}

func (b *GitBackend) expandShortSHA(prefix string) (string, error) {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if len(prefix) == 40 {
		objPath := filepath.Join(b.RepoDir, ".git", "objects", prefix[:2], prefix[2:])
		if _, err := os.Stat(objPath); err == nil {
			return prefix, nil
		}
		return "", fmt.Errorf("object %s not found", prefix)
	}
	if len(prefix) < 4 {
		return "", fmt.Errorf("short SHA %s too short", prefix)
	}
	objDir := filepath.Join(b.RepoDir, ".git", "objects", prefix[:2])
	entries, err := os.ReadDir(objDir)
	if err != nil {
		return "", err
	}
	restPrefix := prefix[2:]
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), restPrefix) {
			return prefix[:2] + e.Name(), nil
		}
	}
	return "", fmt.Errorf("object prefix %s not found", prefix)
}

func (b *GitBackend) readGitObject(shaOrPrefix string) (string, []byte, error) {
	fullSHA, err := b.expandShortSHA(shaOrPrefix)
	if err != nil {
		return "", nil, err
	}
	objPath := filepath.Join(b.RepoDir, ".git", "objects", fullSHA[:2], fullSHA[2:])
	compressed, err := os.ReadFile(objPath)
	if err != nil {
		return "", nil, err
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", nil, err
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return "", nil, err
	}
	nullIdx := bytes.IndexByte(raw, 0)
	if nullIdx == -1 {
		return "", nil, fmt.Errorf("corrupt git object %s", fullSHA)
	}
	header := string(raw[:nullIdx])
	parts := strings.SplitN(header, " ", 2)
	return parts[0], raw[nullIdx+1:], nil
}

type parsedCommit struct {
	SHA        string
	TreeSHA    string
	ParentSHA  string
	Author     string
	AuthorTime time.Time
	Message    string
}

func (b *GitBackend) parseCommitObject(shaOrPrefix string) (parsedCommit, error) {
	fullSHA, err := b.expandShortSHA(shaOrPrefix)
	if err != nil {
		return parsedCommit{}, err
	}
	objType, payload, err := b.readGitObject(fullSHA)
	if err != nil {
		return parsedCommit{}, err
	}
	if objType != "commit" {
		return parsedCommit{}, fmt.Errorf("object %s is %s, not commit", fullSHA, objType)
	}

	pc := parsedCommit{
		SHA:        fullSHA,
		AuthorTime: time.Now().UTC(),
	}
	text := string(payload)
	headerPart := text
	msgPart := ""
	if sepIdx := strings.Index(text, "\n\n"); sepIdx != -1 {
		headerPart = text[:sepIdx]
		msgPart = strings.TrimSpace(text[sepIdx+2:])
	}
	pc.Message = msgPart

	for _, line := range strings.Split(headerPart, "\n") {
		if strings.HasPrefix(line, "tree ") {
			pc.TreeSHA = strings.TrimSpace(strings.TrimPrefix(line, "tree "))
		} else if strings.HasPrefix(line, "parent ") && pc.ParentSHA == "" {
			pc.ParentSHA = strings.TrimSpace(strings.TrimPrefix(line, "parent "))
		} else if strings.HasPrefix(line, "author ") {
			rest := strings.TrimPrefix(line, "author ")
			ltIdx := strings.Index(rest, " <")
			gtIdx := strings.Index(rest, "> ")
			if ltIdx != -1 {
				pc.Author = rest[:ltIdx]
			}
			if gtIdx != -1 {
				timeFields := strings.Fields(rest[gtIdx+2:])
				if len(timeFields) >= 1 {
					if sec, convErr := strconv.ParseInt(timeFields[0], 10, 64); convErr == nil {
						pc.AuthorTime = time.Unix(sec, 0).UTC()
					}
				}
			}
		}
	}
	return pc, nil
}

func (b *GitBackend) readRefNative(refRel string) (string, error) {
	refBytes, err := os.ReadFile(filepath.Join(b.RepoDir, ".git", refRel))
	if err == nil && strings.TrimSpace(string(refBytes)) != "" {
		return strings.TrimSpace(string(refBytes)), nil
	}
	packedBytes, pErr := os.ReadFile(filepath.Join(b.RepoDir, ".git", "packed-refs"))
	if pErr == nil {
		for _, line := range strings.Split(string(packedBytes), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == refRel {
				return fields[0], nil
			}
		}
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("ref %s not found", refRel)
}

func (b *GitBackend) resolveRevNative(rev string) (string, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		return "", fmt.Errorf("empty revision")
	}

	steps := 0
	base := rev
	if tildeIdx := strings.Index(rev, "~"); tildeIdx != -1 {
		base = rev[:tildeIdx]
		stepStr := rev[tildeIdx+1:]
		if stepStr == "" {
			steps = 1
		} else if n, err := strconv.Atoi(stepStr); err == nil && n >= 0 {
			steps = n
		} else {
			return "", fmt.Errorf("invalid revision %s", rev)
		}
	} else if caretIdx := strings.Index(rev, "^"); caretIdx != -1 {
		base = rev[:caretIdx]
		rest := rev[caretIdx:]
		for _, ch := range rest {
			if ch != '^' {
				return "", fmt.Errorf("invalid revision %s", rev)
			}
			steps++
		}
	}

	var sha string
	if base == "HEAD" {
		headBytes, err := os.ReadFile(filepath.Join(b.RepoDir, ".git", "HEAD"))
		if err != nil {
			return "", err
		}
		headStr := strings.TrimSpace(string(headBytes))
		if strings.HasPrefix(headStr, "ref:") {
			refRel := strings.TrimSpace(strings.TrimPrefix(headStr, "ref:"))
			resolvedRef, err := b.readRefNative(refRel)
			if err != nil {
				return "", err
			}
			sha = resolvedRef
		} else {
			sha = headStr
		}
	} else if base == "main" || base == "master" {
		resolvedRef, err := b.readRefNative(filepath.Join("refs", "heads", base))
		if err != nil {
			return "", err
		}
		sha = resolvedRef
	} else {
		expanded, err := b.expandShortSHA(base)
		if err != nil {
			return "", err
		}
		sha = expanded
	}

	if sha == "" {
		return "", fmt.Errorf("unresolved revision %s", rev)
	}

	for i := 0; i < steps; i++ {
		pc, err := b.parseCommitObject(sha)
		if err != nil || pc.ParentSHA == "" {
			return "", fmt.Errorf("revision %s has no parent at step %d", rev, i+1)
		}
		sha = pc.ParentSHA
	}
	return sha, nil
}

func (b *GitBackend) commitNative(configJSON, cliText string, metaJSON []byte, author, message string, timestamp time.Time) (string, error) {
	b.ensureRepoNative()

	files := map[string][]byte{
		"config.cli":    []byte(cliText),
		"config.json":   []byte(configJSON),
		"metadata.json": metaJSON,
	}
	names := []string{"config.cli", "config.json", "metadata.json"}
	sort.Strings(names)

	var treeBuf bytes.Buffer
	for _, name := range names {
		blobSHA, err := b.writeGitObject("blob", files[name])
		if err != nil {
			return "", err
		}
		shaBytes, err := hex.DecodeString(blobSHA)
		if err != nil {
			return "", err
		}
		treeBuf.WriteString(fmt.Sprintf("100644 %s\x00", name))
		treeBuf.Write(shaBytes)
	}

	treeSHA, err := b.writeGitObject("tree", treeBuf.Bytes())
	if err != nil {
		return "", err
	}

	parentSHA, _ := b.resolveRevNative("HEAD")
	if parentSHA != "" {
		if parentCommit, err := b.parseCommitObject(parentSHA); err == nil && parentCommit.TreeSHA == treeSHA {
			return parentSHA, nil
		}
	}

	var commitBuf bytes.Buffer
	commitBuf.WriteString(fmt.Sprintf("tree %s\n", treeSHA))
	if parentSHA != "" {
		commitBuf.WriteString(fmt.Sprintf("parent %s\n", parentSHA))
	}
	sec := timestamp.UTC().Unix()
	commitBuf.WriteString(fmt.Sprintf("author %s <%s@srl-timeline> %d +0000\n", author, author, sec))
	commitBuf.WriteString(fmt.Sprintf("committer SR Linux Timeline <timeline@srl-timeline> %d +0000\n\n", sec))
	commitBuf.WriteString(message + "\n")

	commitSHA, err := b.writeGitObject("commit", commitBuf.Bytes())
	if err != nil {
		return "", err
	}

	refPath := filepath.Join(b.RepoDir, ".git", "refs", "heads", "main")
	_ = os.MkdirAll(filepath.Dir(refPath), os.ModeSetgid|0775)
	if err := os.WriteFile(refPath, []byte(commitSHA+"\n"), 0664); err != nil {
		return "", err
	}
	ApplySharedFilePermissions(refPath)
	return commitSHA, nil
}

func (b *GitBackend) showFileNative(spec string) (string, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid show spec %s", spec)
	}
	commitSHA, err := b.resolveRevNative(parts[0])
	if err != nil {
		return "", err
	}
	targetFile := parts[1]

	pc, err := b.parseCommitObject(commitSHA)
	if err != nil {
		return "", err
	}
	objType, treeBytes, err := b.readGitObject(pc.TreeSHA)
	if err != nil || objType != "tree" {
		return "", fmt.Errorf("failed to read tree %s", pc.TreeSHA)
	}

	idx := 0
	for idx < len(treeBytes) {
		nullIdx := bytes.IndexByte(treeBytes[idx:], 0)
		if nullIdx == -1 || idx+nullIdx+21 > len(treeBytes) {
			break
		}
		entryHeader := string(treeBytes[idx : idx+nullIdx])
		shaBin := treeBytes[idx+nullIdx+1 : idx+nullIdx+21]
		idx += nullIdx + 21

		spIdx := strings.IndexByte(entryHeader, ' ')
		if spIdx == -1 {
			continue
		}
		name := entryHeader[spIdx+1:]
		if name == targetFile {
			blobSHA := hex.EncodeToString(shaBin)
			_, blobData, err := b.readGitObject(blobSHA)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(blobData)), nil
		}
	}
	return "", fmt.Errorf("file %s not found in commit %s", targetFile, commitSHA)
}

func (b *GitBackend) logLinesNative(limit int) (string, error) {
	if limit <= 0 {
		limit = 250
	}
	currSHA, err := b.resolveRevNative("HEAD")
	if err != nil || currSHA == "" {
		return "", fmt.Errorf("no commits")
	}

	var lines []string
	for currSHA != "" && len(lines) < limit {
		pc, err := b.parseCommitObject(currSHA)
		if err != nil {
			break
		}
		firstLineMsg := strings.SplitN(pc.Message, "\n", 2)[0]
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%s", pc.SHA, pc.Author, pc.AuthorTime.Format(time.RFC3339), firstLineMsg))
		currSHA = pc.ParentSHA
	}
	return strings.Join(lines, "\n"), nil
}

func (b *GitBackend) blameNative(realSHA, filename string) []models.BlameEntry {
	commitSHA, err := b.resolveRevNative(realSHA)
	if err != nil || commitSHA == "" {
		return nil
	}

	var history []parsedCommit
	curr := commitSHA
	for curr != "" && len(history) < 250 {
		pc, err := b.parseCommitObject(curr)
		if err != nil {
			break
		}
		history = append(history, pc)
		curr = pc.ParentSHA
	}
	if len(history) == 0 {
		return nil
	}

	targetContent, err := b.showFileNative(fmt.Sprintf("%s:%s", commitSHA, filename))
	if err != nil || targetContent == "" {
		return nil
	}
	targetLines := strings.Split(targetContent, "\n")

	// Track which commit (from oldest to newest) introduced each line content
	lineOrigin := make(map[string]parsedCommit, len(targetLines))
	for i := len(history) - 1; i >= 0; i-- {
		c := history[i]
		content, err := b.showFileNative(fmt.Sprintf("%s:%s", c.SHA, filename))
		if err != nil {
			continue
		}
		currSet := make(map[string]bool)
		for _, l := range strings.Split(content, "\n") {
			currSet[l] = true
			if _, exists := lineOrigin[l]; !exists {
				lineOrigin[l] = c
			}
		}
		// If a line disappeared in this commit and re-appears later, reset its origin
		for existingLine := range lineOrigin {
			if !currSet[existingLine] {
				delete(lineOrigin, existingLine)
			}
		}
	}

	entries := make([]models.BlameEntry, 0, len(targetLines))
	for idx, l := range targetLines {
		origin, ok := lineOrigin[l]
		if !ok {
			origin = history[0]
		}
		entries = append(entries, models.BlameEntry{
			LineNumber:    idx + 1,
			Content:       l,
			CommitSHA:     origin.SHA,
			Author:        origin.Author,
			Timestamp:     origin.AuthorTime,
			CommitMessage: strings.SplitN(origin.Message, "\n", 2)[0],
		})
	}
	return entries
}
