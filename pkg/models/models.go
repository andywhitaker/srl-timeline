package models

import (
	"fmt"
	"strings"
	"time"
)

// DiffType specifies the type of semantic change.
type DiffType string

const (
	DiffAdded    DiffType = "ADDED"
	DiffModified DiffType = "MODIFIED"
	DiffDeleted  DiffType = "DELETED"
)

// PathChange represents a semantic change to a specific path/leaf.
type PathChange struct {
	Path       string      `json:"path"`
	DiffType   DiffType    `json:"diff_type"`
	OldValue   interface{} `json:"old_value,omitempty"`
	NewValue   interface{} `json:"new_value,omitempty"`
	YANGModule string      `json:"yang_module,omitempty"`
}

// SemanticDiffResult encapsulates structured, unified, and CLI diff representations.
type SemanticDiffResult struct {
	HasChanges       bool         `json:"has_changes"`
	AddedCount       int          `json:"added_count"`
	ModifiedCount    int          `json:"modified_count"`
	DeletedCount     int          `json:"deleted_count"`
	Changes          []PathChange `json:"changes"`
	UnifiedDiffLines []string     `json:"unified_diff_lines"`
	CLIDiffLines     []string     `json:"cli_diff_lines"`
}

// StatBadge returns a formatted change summary string like "+3 / ~1 / -2".
func (d SemanticDiffResult) StatBadge() string {
	if !d.HasChanges {
		return "No changes"
	}
	var parts []string
	if d.AddedCount > 0 {
		parts = append(parts, fmt.Sprintf("+%d", d.AddedCount))
	}
	if d.ModifiedCount > 0 {
		parts = append(parts, fmt.Sprintf("~%d", d.ModifiedCount))
	}
	if d.DeletedCount > 0 {
		parts = append(parts, fmt.Sprintf("-%d", d.DeletedCount))
	}
	if len(parts) == 0 {
		return "Identical"
	}
	return strings.Join(parts, " / ")
}

// TimelineCommit represents a single configuration commit in the repository.
type TimelineCommit struct {
	CommitID    string                 `json:"commit_id"`
	FullSHA     string                 `json:"full_sha"`
	Timestamp   time.Time              `json:"timestamp"`
	Author      string                 `json:"author"`
	Message     string                 `json:"message"`
	SRLCommitID string                 `json:"srl_commit_id,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	DiffStat    string                 `json:"diff_stat"`
	IsCurrent   bool                   `json:"is_current"`
	IsStartup   bool                   `json:"is_startup"`
	IsRestored  bool                   `json:"is_restored"`
	Tags        []string               `json:"tags,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// RelativeTime returns human-friendly relative time string (e.g. "2m ago", "1h ago").
func (c TimelineCommit) RelativeTime() string {
	diff := time.Since(c.Timestamp)
	secs := int(diff.Seconds())
	if secs < 5 {
		return "just now"
	}
	if secs < 60 {
		return fmt.Sprintf("%ds ago", secs)
	}
	mins := secs / 60
	if mins < 60 {
		return fmt.Sprintf("%dm ago", mins)
	}
	hours := mins / 60
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	if days < 30 {
		return fmt.Sprintf("%dd ago", days)
	}
	months := days / 30
	if months < 12 {
		return fmt.Sprintf("%dmo ago", months)
	}
	years := days / 365
	return fmt.Sprintf("%dy ago", years)
}

// FormattedTime returns the timestamp formatted as "YYYY-MM-DD HH:MM:SS".
func (c TimelineCommit) FormattedTime() string {
	return c.Timestamp.Format("2006-01-02 15:04:05")
}

// BlameEntry represents line-by-line attribution of a configuration line.
type BlameEntry struct {
	Path          string    `json:"path"`
	LineNumber    int       `json:"line_number"`
	Content       string    `json:"content"`
	CommitSHA     string    `json:"commit_sha"`
	Author        string    `json:"author"`
	Timestamp     time.Time `json:"timestamp"`
	CommitMessage string    `json:"commit_message"`
}

// ShortSHA returns 8-character commit hash prefix.
func (b BlameEntry) ShortSHA() string {
	if len(b.CommitSHA) >= 8 {
		return b.CommitSHA[:8]
	}
	return b.CommitSHA
}

// RemoteRepoConfig encapsulates remote Git synchronization configuration.
type RemoteRepoConfig struct {
	URL        string `json:"url"`
	Branch     string `json:"branch"`
	AutoPush   bool   `json:"auto_push"`
	SyncStatus string `json:"sync_status"`
	PublicKey  string `json:"public_key,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}
