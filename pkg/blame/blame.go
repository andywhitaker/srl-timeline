package blame

import (
	"math"
	"strings"

	"timeline/pkg/filter"
	"timeline/pkg/gitbackend"
	"timeline/pkg/models"
)

// AuthorStat holds line counts and percentage for an author.
type AuthorStat struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// ContributorStats holds contributor metrics and subsystem breakdowns.
type ContributorStats struct {
	TotalLines int                       `json:"total_lines"`
	Authors    map[string]AuthorStat     `json:"authors"`
	Subsystems map[string]map[string]int `json:"subsystems"`
}

// BlameEngine analyzes configuration line attribution and contributors.
type BlameEngine struct {
	GitBackend *gitbackend.GitBackend
}

// NewBlameEngine creates a new BlameEngine.
func NewBlameEngine(backend *gitbackend.GitBackend) *BlameEngine {
	if backend == nil {
		backend = gitbackend.NewGitBackend("")
	}
	return &BlameEngine{GitBackend: backend}
}

// GetBlameLines returns blame entries at HEAD, optionally filtered.
func (e *BlameEngine) GetBlameLines(mode, filterQuery string) []models.BlameEntry {
	return e.GetBlameLinesAtCommit(mode, "HEAD", filterQuery)
}

// GetBlameLinesAtCommit returns blame entries for a specific commit SHA, optionally filtered.
func (e *BlameEngine) GetBlameLinesAtCommit(mode, commitSHA, filterQuery string) []models.BlameEntry {
	if mode == "" {
		mode = "cli"
	}
	allEntries := e.GitBackend.GetBlameEntriesAtCommit(mode, commitSHA)

	cleanFilter := strings.ToLower(strings.TrimSpace(filterQuery))
	if cleanFilter == "" {
		return allEntries
	}

	var filtered []models.BlameEntry
	for _, entry := range allEntries {
		if filter.IsPathMatching(entry.Content, filterQuery) ||
			strings.Contains(strings.ToLower(entry.Author), cleanFilter) ||
			strings.Contains(strings.ToLower(entry.CommitSHA), cleanFilter) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// GetContributorStats calculates author contributions and subsystem metrics at HEAD.
func (e *BlameEngine) GetContributorStats(mode string) ContributorStats {
	return e.GetContributorStatsAtCommit(mode, "HEAD")
}

// GetContributorStatsAtCommit calculates author contributions and subsystem metrics at a specific commit SHA.
func (e *BlameEngine) GetContributorStatsAtCommit(mode, commitSHA string) ContributorStats {
	entries := e.GitBackend.GetBlameEntriesAtCommit(mode, commitSHA)
	totalLines := len(entries)

	authorsCount := make(map[string]int)
	subsystems := map[string]map[string]int{
		"acl":              make(map[string]int),
		"interfaces":       make(map[string]int),
		"network-instance": make(map[string]int),
		"system":           make(map[string]int),
		"other":            make(map[string]int),
	}

	for _, entry := range entries {
		author := entry.Author
		if author == "" {
			author = "unknown"
		}
		authorsCount[author]++

		sub := classifySubsystem(entry.Content)
		subsystems[sub][author]++
	}

	authors := make(map[string]AuthorStat)
	for author, count := range authorsCount {
		pct := 0.0
		if totalLines > 0 {
			pct = math.Round((float64(count)/float64(totalLines))*1000.0) / 10.0
		}
		authors[author] = AuthorStat{
			Count:      count,
			Percentage: pct,
		}
	}

	return ContributorStats{
		TotalLines: totalLines,
		Authors:    authors,
		Subsystems: subsystems,
	}
}

func classifySubsystem(content string) string {
	trimmed := strings.ToLower(strings.TrimSpace(content))
	// Check root container prefix first so "/system/control-plane-traffic/input/acl" is classified as "system"
	// and "/network-instance/default/interface" is classified as "network-instance".
	if strings.HasPrefix(trimmed, "set / system ") || trimmed == "set / system" || strings.HasPrefix(trimmed, "/system") {
		return "system"
	}
	if strings.HasPrefix(trimmed, "set / network-instance ") || trimmed == "set / network-instance" || strings.HasPrefix(trimmed, "/network-instance") {
		return "network-instance"
	}
	if strings.HasPrefix(trimmed, "set / interface ") || trimmed == "set / interface" || strings.HasPrefix(trimmed, "/interface") {
		return "interfaces"
	}
	if strings.HasPrefix(trimmed, "set / acl ") || trimmed == "set / acl" || strings.HasPrefix(trimmed, "/acl") {
		return "acl"
	}

	// Fallback for JSON lines or partial paths
	if strings.Contains(trimmed, " network-instance") || strings.Contains(trimmed, "/network-instance") {
		return "network-instance"
	}
	if strings.Contains(trimmed, " system") || strings.Contains(trimmed, "/system") {
		return "system"
	}
	if strings.Contains(trimmed, " interface") || strings.Contains(trimmed, "/interface") {
		return "interfaces"
	}
	if strings.Contains(trimmed, " acl ") || strings.Contains(trimmed, "/acl") {
		return "acl"
	}
	return "other"
}
