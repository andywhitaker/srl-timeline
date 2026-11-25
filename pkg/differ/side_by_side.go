package differ

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"timeline/pkg/filter"
	"timeline/pkg/models"
	"timeline/pkg/normalizer"
)

// SideBySideRowKind classifies a single aligned row in a side-by-side diff.
type SideBySideRowKind int

const (
	SideBySideEqual SideBySideRowKind = iota
	SideBySideAdded
	SideBySideDeleted
	SideBySideModified
	SideBySideFold
)

// String returns a human-readable label for the row kind.
func (k SideBySideRowKind) String() string {
	switch k {
	case SideBySideEqual:
		return "EQUAL"
	case SideBySideAdded:
		return "ADDED"
	case SideBySideDeleted:
		return "DELETED"
	case SideBySideModified:
		return "MODIFIED"
	case SideBySideFold:
		return "FOLD"
	default:
		return fmt.Sprintf("SideBySideRowKind(%d)", int(k))
	}
}

// SideBySideRow represents one aligned row across Left (older) and Right (newer) panes.
// LeftText and RightText always contain clean raw text with no leading "+", "-", or "~"
// diff prefixes so callers can render their own gutter markers and syntax-highlight directly.
type SideBySideRow struct {
	Kind        SideBySideRowKind
	LeftLineNo  int    // 0 when empty/pad or fold
	LeftText    string
	RightLineNo int    // 0 when empty/pad or fold
	RightText   string
	FoldCount   int    // >0 for SideBySideFold
	FoldHint    string // optional context hint e.g. top-level path
}

// SideBySideDiffResult holds aligned side-by-side diff rows and summary counts.
type SideBySideDiffResult struct {
	Mode          string // "cli", "unified", "path"
	HasChanges    bool
	AddedCount    int
	ModifiedCount int
	DeletedCount  int
	Rows          []SideBySideRow
	TotalLines    int
}

// ComputeSideBySideDiff computes an aligned side-by-side diff comparing oldCfg (Left, older)
// against newCfg (Right, newer) in the requested mode ("cli", "unified", or "path").
//
// When filterPath is non-empty, the diff is scoped identically to SemanticDiff.
// For "cli" and "unified" modes, setting foldUnchanged=true collapses consecutive unchanged
// stretches outside +/- contextLines (defaulting to 3 when <= 0) around any change into
// SideBySideFold rows, skipping folds for tiny gaps of <= 2 lines.
func ComputeSideBySideDiff(oldCfg, newCfg map[string]interface{}, mode, filterPath string, foldUnchanged bool, contextLines int) SideBySideDiffResult {
	resolvedMode := normalizeSideBySideMode(mode)
	cleanFilter := strings.TrimSpace(filterPath)

	switch resolvedMode {
	case "unified":
		return computeUnifiedSideBySide(oldCfg, newCfg, cleanFilter, foldUnchanged, contextLines)
	case "path":
		return computePathSideBySide(oldCfg, newCfg, cleanFilter)
	default:
		return computeCLISideBySide(oldCfg, newCfg, cleanFilter, foldUnchanged, contextLines)
	}
}

func normalizeSideBySideMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "unified":
		return "unified"
	case "path":
		return "path"
	default:
		return "cli"
	}
}

func normalizeConfigMap(cfg map[string]interface{}) map[string]interface{} {
	norm, _ := normalizer.NormalizeStructure(cfg, true).(map[string]interface{})
	if norm == nil {
		return make(map[string]interface{})
	}
	return norm
}

func computeCLISideBySide(oldCfg, newCfg map[string]interface{}, cleanFilter string, foldUnchanged bool, contextLines int) SideBySideDiffResult {
	normOld := normalizeConfigMap(oldCfg)
	normNew := normalizeConfigMap(newCfg)

	oldLines := normalizer.JSONToFlatCLI(normOld, "")
	newLines := normalizer.JSONToFlatCLI(normNew, "")

	if cleanFilter != "" {
		oldLines = filter.FilterCLILines(oldLines, cleanFilter)
		newLines = filter.FilterCLILines(newLines, cleanFilter)
	}

	return buildLineSideBySideResult("cli", oldLines, newLines, foldUnchanged, contextLines)
}

func computeUnifiedSideBySide(oldCfg, newCfg map[string]interface{}, cleanFilter string, foldUnchanged bool, contextLines int) SideBySideDiffResult {
	normOld := normalizeConfigMap(oldCfg)
	normNew := normalizeConfigMap(newCfg)

	oldTarget := normOld
	newTarget := normNew
	if cleanFilter != "" {
		oldTarget = filter.FilterConfigSubtree(normOld, cleanFilter)
		newTarget = filter.FilterConfigSubtree(normNew, cleanFilter)
	}

	oldLines := canonicalJSONLines(oldTarget)
	newLines := canonicalJSONLines(newTarget)

	return buildLineSideBySideResult("unified", oldLines, newLines, foldUnchanged, contextLines)
}

func canonicalJSONLines(cfg map[string]interface{}) []string {
	if len(cfg) == 0 {
		return nil
	}
	raw, err := normalizer.CanonicalJSONString(cfg, 2)
	if err != nil || raw == "" || raw == "{}" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func buildLineSideBySideResult(mode string, oldLines, newLines []string, foldUnchanged bool, contextLines int) SideBySideDiffResult {
	chunks := computeLCSDiff(oldLines, newLines)
	allRows, added, modified, deleted := alignDiffChunks(chunks, oldLines, newLines)

	hasChanges := added > 0 || modified > 0 || deleted > 0
	res := SideBySideDiffResult{
		Mode:          mode,
		HasChanges:    hasChanges,
		AddedCount:    added,
		ModifiedCount: modified,
		DeletedCount:  deleted,
		TotalLines:    len(allRows),
	}

	if !hasChanges {
		if foldUnchanged {
			res.Rows = nil
		} else if len(allRows) > 0 {
			res.Rows = allRows
		}
		return res
	}

	if foldUnchanged {
		res.Rows = foldEqualRows(allRows, mode, contextLines)
	} else {
		res.Rows = allRows
	}
	return res
}

type numberedLine struct {
	lineNo int
	text   string
}

// alignDiffChunks converts LCS diff chunks into aligned side-by-side rows, pairing
// contiguous delete+insert hunks row-for-row into SideBySideModified rows.
func alignDiffChunks(chunks []diffChunk, oldLines, newLines []string) ([]SideBySideRow, int, int, int) {
	if len(chunks) == 0 {
		return nil, 0, 0, 0
	}

	rows := make([]SideBySideRow, 0, len(chunks))
	var added, modified, deleted int
	leftIdx := 0
	rightIdx := 0

	i := 0
	for i < len(chunks) {
		if chunks[i].op == diffEqual {
			leftText := chunks[i].line
			if leftIdx < len(oldLines) {
				leftText = oldLines[leftIdx]
			}
			rightText := chunks[i].line
			if rightIdx < len(newLines) {
				rightText = newLines[rightIdx]
			}
			leftIdx++
			rightIdx++
			rows = append(rows, SideBySideRow{
				Kind:        SideBySideEqual,
				LeftLineNo:  leftIdx,
				LeftText:    leftText,
				RightLineNo: rightIdx,
				RightText:   rightText,
			})
			i++
			continue
		}

		// Collect maximal contiguous run of non-equal chunks (deletes and inserts)
		var dels []numberedLine
		var ins []numberedLine
		for i < len(chunks) && chunks[i].op != diffEqual {
			switch chunks[i].op {
			case diffDelete:
				text := chunks[i].line
				if leftIdx < len(oldLines) {
					text = oldLines[leftIdx]
				}
				leftIdx++
				dels = append(dels, numberedLine{lineNo: leftIdx, text: text})
			case diffInsert:
				text := chunks[i].line
				if rightIdx < len(newLines) {
					text = newLines[rightIdx]
				}
				rightIdx++
				ins = append(ins, numberedLine{lineNo: rightIdx, text: text})
			}
			i++
		}

		paired := len(dels)
		if len(ins) < paired {
			paired = len(ins)
		}

		for k := 0; k < paired; k++ {
			rows = append(rows, SideBySideRow{
				Kind:        SideBySideModified,
				LeftLineNo:  dels[k].lineNo,
				LeftText:    dels[k].text,
				RightLineNo: ins[k].lineNo,
				RightText:   ins[k].text,
			})
			modified++
		}

		for k := paired; k < len(dels); k++ {
			rows = append(rows, SideBySideRow{
				Kind:       SideBySideDeleted,
				LeftLineNo: dels[k].lineNo,
				LeftText:   dels[k].text,
			})
			deleted++
		}

		for k := paired; k < len(ins); k++ {
			rows = append(rows, SideBySideRow{
				Kind:        SideBySideAdded,
				RightLineNo: ins[k].lineNo,
				RightText:   ins[k].text,
			})
			added++
		}
	}

	return rows, added, modified, deleted
}

// foldEqualRows collapses consecutive SideBySideEqual runs outside the +/- contextLines
// window around any changed row into SideBySideFold markers (skipping gaps <= 2 lines).
func foldEqualRows(rows []SideBySideRow, mode string, contextLines int) []SideBySideRow {
	if len(rows) == 0 {
		return nil
	}
	if contextLines <= 0 {
		contextLines = 3
	}

	keep := make([]bool, len(rows))
	for i, r := range rows {
		if r.Kind != SideBySideEqual {
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			end := i + contextLines
			if end >= len(rows) {
				end = len(rows) - 1
			}
			for j := start; j <= end; j++ {
				keep[j] = true
			}
		}
	}

	var sectionHints []string
	if mode == "unified" {
		sectionHints = computeJSONTopLevelHints(rows)
	}

	out := make([]SideBySideRow, 0, len(rows))
	for i := 0; i < len(rows); {
		if keep[i] {
			out = append(out, rows[i])
			i++
			continue
		}
		end := i
		for end < len(rows) && !keep[end] {
			end++
		}
		gap := end - i
		if gap <= 2 {
			out = append(out, rows[i:end]...)
		} else {
			out = append(out, SideBySideRow{
				Kind:      SideBySideFold,
				FoldCount: gap,
				FoldHint:  buildFoldHint(rows, sectionHints, i, end, mode),
			})
		}
		i = end
	}

	return out
}

func computeJSONTopLevelHints(rows []SideBySideRow) []string {
	hints := make([]string, len(rows))
	current := ""
	for i, r := range rows {
		line := r.RightText
		if line == "" {
			line = r.LeftText
		}
		if key := extractTopLevelJSONKey(line); key != "" {
			current = "/" + key
		} else if line == "}" {
			current = ""
		}
		hints[i] = current
	}
	return hints
}

func extractTopLevelJSONKey(line string) string {
	// Top-level keys in 2-space indented canonical JSON begin with exactly two spaces and a quote.
	if len(line) < 5 || line[0] != ' ' || line[1] != ' ' || line[2] != '"' {
		return ""
	}
	endQuote := strings.IndexByte(line[3:], '"')
	if endQuote <= 0 {
		return ""
	}
	rest := line[3+endQuote+1:]
	if !strings.HasPrefix(rest, ":") {
		return ""
	}
	return normalizer.StripNamespace(line[3 : 3+endQuote])
}

func extractTopLevelCLIPath(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "set /")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	return "/" + fields[0]
}

func buildFoldHint(rows []SideBySideRow, jsonHints []string, start, end int, mode string) string {
	firstHint := ""
	lastHint := ""

	for i := start; i < end; i++ {
		var h string
		switch mode {
		case "cli":
			line := rows[i].RightText
			if line == "" {
				line = rows[i].LeftText
			}
			h = extractTopLevelCLIPath(line)
		case "unified":
			if i < len(jsonHints) {
				h = jsonHints[i]
			}
		}
		if h == "" {
			continue
		}
		if firstHint == "" {
			firstHint = h
		}
		lastHint = h
	}

	if firstHint == "" {
		return ""
	}
	if firstHint == lastHint {
		return firstHint
	}
	return firstHint + " … " + lastHint
}

func computePathSideBySide(oldCfg, newCfg map[string]interface{}, cleanFilter string) SideBySideDiffResult {
	sem := SemanticDiff(oldCfg, newCfg, cleanFilter)
	deduped := deduplicateRedundantPathChanges(sem.Changes)

	res := SideBySideDiffResult{
		Mode: "path",
	}
	if len(deduped) == 0 {
		return res
	}

	rows := make([]SideBySideRow, 0, len(deduped))
	leftLine := 0
	rightLine := 0

	for _, c := range deduped {
		switch c.DiffType {
		case models.DiffAdded:
			rightLine++
			rows = append(rows, SideBySideRow{
				Kind:        SideBySideAdded,
				RightLineNo: rightLine,
				RightText:   formatPathChangeEntry(c.Path, c.NewValue),
			})
			res.AddedCount++
		case models.DiffDeleted:
			leftLine++
			rows = append(rows, SideBySideRow{
				Kind:       SideBySideDeleted,
				LeftLineNo: leftLine,
				LeftText:   formatPathChangeEntry(c.Path, c.OldValue),
			})
			res.DeletedCount++
		case models.DiffModified:
			leftLine++
			rightLine++
			rows = append(rows, SideBySideRow{
				Kind:        SideBySideModified,
				LeftLineNo:  leftLine,
				LeftText:    formatPathChangeEntry(c.Path, c.OldValue),
				RightLineNo: rightLine,
				RightText:   formatPathChangeEntry(c.Path, c.NewValue),
			})
			res.ModifiedCount++
		}
	}

	res.HasChanges = res.AddedCount > 0 || res.ModifiedCount > 0 || res.DeletedCount > 0
	res.Rows = rows
	res.TotalLines = len(rows)
	return res
}

func isPathContainerValue(v interface{}) bool {
	if v == nil {
		return false
	}
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return true
	default:
		return false
	}
}

// deduplicateRedundantPathChanges removes parent container PathChange entries when
// child leaf changes under c.Path+"/" or c.Path+"[" are already present.
func deduplicateRedundantPathChanges(changes []models.PathChange) []models.PathChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]models.PathChange, 0, len(changes))
	for i, c := range changes {
		if isPathContainerValue(c.OldValue) || isPathContainerValue(c.NewValue) {
			slashPrefix := c.Path + "/"
			bracketPrefix := c.Path + "["
			hasChild := false
			for j, other := range changes {
				if i != j && (strings.HasPrefix(other.Path, slashPrefix) || strings.HasPrefix(other.Path, bracketPrefix)) {
					hasChild = true
					break
				}
			}
			if hasChild {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func formatPathChangeEntry(path string, val interface{}) string {
	formatted := formatPathChangeVal(val)
	if formatted == "" {
		return path
	}
	return path + " = " + formatted
}

func formatPathChangeVal(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		cleaned := strings.ReplaceAll(val, "\r\n", " ")
		cleaned = strings.ReplaceAll(cleaned, "\n", " ")
		cleaned = strings.ReplaceAll(cleaned, "\r", " ")
		if cleaned == "" {
			return `""`
		}
		return cleaned
	case bool:
		return strconv.FormatBool(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case json.Number:
		return val.String()
	case map[string]interface{}:
		if len(val) == 0 {
			return "{}"
		}
		return fmt.Sprintf("{%d keys}", len(val))
	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}
		parts := make([]string, 0, len(val))
		for _, item := range val {
			if isPathContainerValue(item) {
				return fmt.Sprintf("[%d items]", len(val))
			}
			parts = append(parts, formatPathChangeVal(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}
