package blame

import (
	"math"
	"strings"

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
	TotalLines int                            `json:"total_lines"`
	Authors    map[string]AuthorStat          `json:"authors"`
	Subsystems map[string]map[string]int      `json:"subsystems"`
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

// GetBlameLines returns blame entries, optionally filtered.
func (e *BlameEngine) GetBlameLines(mode, filterQuery string) []models.BlameEntry {
	if mode == "" {
		mode = "cli"
	}
	allEntries := e.GitBackend.GetBlameEntries(mode)

	cleanFilter := strings.ToLower(strings.TrimSpace(filterQuery))
	if cleanFilter == "" {
		return allEntries
	}

	var filtered []models.BlameEntry
	for _, entry := range allEntries {
		if strings.Contains(strings.ToLower(entry.Content), cleanFilter) ||
			strings.Contains(strings.ToLower(entry.Author), cleanFilter) ||
			strings.Contains(strings.ToLower(entry.CommitSHA), cleanFilter) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// GetContributorStats calculates author contributions and subsystem metrics.
func (e *BlameEngine) GetContributorStats(mode string) ContributorStats {
	entries := e.GitBackend.GetBlameEntries(mode)
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

		// Subsystem categorization
		content := strings.ToLower(entry.Content)
		sub := "other"
		if strings.Contains(content, " acl ") || strings.Contains(content, "/acl") {
			sub = "acl"
		} else if strings.Contains(content, " interface") || strings.Contains(content, "/interface") {
			sub = "interfaces"
		} else if strings.Contains(content, " network-instance") || strings.Contains(content, "/network-instance") {
			sub = "network-instance"
		} else if strings.Contains(content, " system") || strings.Contains(content, "/system") {
			sub = "system"
		}
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
