package differ

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"timeline/pkg/models"
	"timeline/pkg/normalizer"
)

// SemanticDiff compares two configurations and returns a SemanticDiffResult.
func SemanticDiff(oldCfg, newCfg map[string]interface{}, filterPath string) models.SemanticDiffResult {
	normOld := normalizer.NormalizeStructure(oldCfg, true).(map[string]interface{})
	normNew := normalizer.NormalizeStructure(newCfg, true).(map[string]interface{})

	oldJSON, _ := normalizer.CanonicalJSONString(normOld, 2)
	newJSON, _ := normalizer.CanonicalJSONString(normNew, 2)

	cleanFilter := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(filterPath, "/")))

	if oldJSON == newJSON {
		return models.SemanticDiffResult{
			HasChanges:       false,
			Changes:          []models.PathChange{},
			UnifiedDiffLines: []string{},
			CLIDiffLines:     []string{},
		}
	}

	var changes []models.PathChange
	diffJSONTrees("", normOld, normNew, &changes)

	// Filter changes if filterPath is supplied
	var filteredChanges []models.PathChange
	for _, c := range changes {
		if cleanFilter == "" || strings.Contains(strings.ToLower(c.Path), cleanFilter) {
			filteredChanges = append(filteredChanges, c)
		}
	}

	var added, modified, deleted int
	for _, c := range filteredChanges {
		switch c.DiffType {
		case models.DiffAdded:
			added++
		case models.DiffModified:
			modified++
		case models.DiffDeleted:
			deleted++
		}
	}

	// Generate Unified Diff with LCS
	unifiedDiff := generateUnifiedDiff(oldJSON, newJSON, cleanFilter)

	// Generate CLI Diff
	oldCLI := normalizer.JSONToFlatCLI(normOld, "")
	newCLI := normalizer.JSONToFlatCLI(normNew, "")
	cliDiff := generateCLIDiff(oldCLI, newCLI, cleanFilter)

	hasChanges := len(filteredChanges) > 0 || len(unifiedDiff) > 0 || len(cliDiff) > 0

	return models.SemanticDiffResult{
		HasChanges:       hasChanges,
		AddedCount:       added,
		ModifiedCount:    modified,
		DeletedCount:     deleted,
		Changes:          filteredChanges,
		UnifiedDiffLines: unifiedDiff,
		CLIDiffLines:     cliDiff,
	}
}

func diffJSONTrees(currentPath string, oldVal, newVal interface{}, changes *[]models.PathChange) {
	if reflect.DeepEqual(oldVal, newVal) {
		return
	}

	if oldVal == nil && newVal != nil {
		*changes = append(*changes, models.PathChange{
			Path:     currentPath,
			DiffType: models.DiffAdded,
			NewValue: newVal,
		})
		if m, ok := newVal.(map[string]interface{}); ok {
			for k, v := range m {
				subPath := fmt.Sprintf("%s/%s", currentPath, k)
				diffJSONTrees(subPath, nil, v, changes)
			}
		}
		return
	}

	if oldVal != nil && newVal == nil {
		*changes = append(*changes, models.PathChange{
			Path:     currentPath,
			DiffType: models.DiffDeleted,
			OldValue: oldVal,
		})
		if m, ok := oldVal.(map[string]interface{}); ok {
			for k, v := range m {
				subPath := fmt.Sprintf("%s/%s", currentPath, k)
				diffJSONTrees(subPath, v, nil, changes)
			}
		}
		return
	}

	oldMap, oldIsMap := oldVal.(map[string]interface{})
	newMap, newIsMap := newVal.(map[string]interface{})

	if oldIsMap && newIsMap {
		allKeys := make(map[string]bool)
		for k := range oldMap {
			allKeys[k] = true
		}
		for k := range newMap {
			allKeys[k] = true
		}

		var sortedKeys []string
		for k := range allKeys {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)

		for _, k := range sortedKeys {
			subPath := fmt.Sprintf("%s/%s", currentPath, k)
			if currentPath == "" {
				subPath = fmt.Sprintf("/%s", k)
			}
			ov := oldMap[k]
			nv := newMap[k]
			diffJSONTrees(subPath, ov, nv, changes)
		}
		return
	}

	oldList, oldIsList := oldVal.([]interface{})
	newList, newIsList := newVal.([]interface{})

	if oldIsList && newIsList {
		// Compare keyed lists
		oldKeyed := makeKeyedListMap(oldList)
		newKeyed := makeKeyedListMap(newList)

		allListKeys := make(map[string]bool)
		for k := range oldKeyed {
			allListKeys[k] = true
		}
		for k := range newKeyed {
			allListKeys[k] = true
		}

		var sortedListKeys []string
		for k := range allListKeys {
			sortedListKeys = append(sortedListKeys, k)
		}
		sort.Strings(sortedListKeys)

		for _, lk := range sortedListKeys {
			itemSubPath := currentPath
			if lk != "" {
				itemSubPath = fmt.Sprintf("%s[%s]", currentPath, lk)
			}
			ov := oldKeyed[lk]
			nv := newKeyed[lk]
			diffJSONTrees(itemSubPath, ov, nv, changes)
		}
		return
	}

	// Leaf scalar modified
	*changes = append(*changes, models.PathChange{
		Path:     currentPath,
		DiffType: models.DiffModified,
		OldValue: oldVal,
		NewValue: newVal,
	})
}

func makeKeyedListMap(list []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(list))
	for idx, item := range list {
		if elemMap, ok := item.(map[string]interface{}); ok {
			var keyVal string
			for _, k := range normalizer.WellKnownListKeys {
				if val, found := elemMap[k]; found {
					keyVal = fmt.Sprintf("%s=%v", k, val)
					break
				}
			}
			if keyVal != "" {
				m[keyVal] = item
				continue
			}
		}
		m[fmt.Sprintf("idx_%d", idx)] = item
	}
	return m
}

type diffOp int

const (
	diffEqual diffOp = iota
	diffInsert
	diffDelete
)

type diffChunk struct {
	op   diffOp
	line string
}

func computeLCSDiff(oldLines, newLines []string) []diffChunk {
	n := len(oldLines)
	m := len(newLines)

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	var chunks []diffChunk
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			chunks = append(chunks, diffChunk{op: diffEqual, line: oldLines[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			chunks = append(chunks, diffChunk{op: diffInsert, line: newLines[j-1]})
			j--
		} else if i > 0 && (j == 0 || dp[i-1][j] >= dp[i][j-1]) {
			chunks = append(chunks, diffChunk{op: diffDelete, line: oldLines[i-1]})
			i--
		}
	}

	for k := 0; k < len(chunks)/2; k++ {
		opp := len(chunks) - 1 - k
		chunks[k], chunks[opp] = chunks[opp], chunks[k]
	}

	return chunks
}

func generateUnifiedDiff(oldStr, newStr, filterQuery string) []string {
	if oldStr == newStr || (oldStr == "" && newStr == "") {
		return []string{}
	}

	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")
	if len(oldLines) == 1 && oldLines[0] == "" {
		oldLines = []string{}
	}
	if len(newLines) == 1 && newLines[0] == "" {
		newLines = []string{}
	}

	chunks := computeLCSDiff(oldLines, newLines)

	var hasDiff bool
	for _, c := range chunks {
		if c.op != diffEqual {
			hasDiff = true
			break
		}
	}
	if !hasDiff {
		return []string{}
	}

	var result []string
	result = append(result, "--- previous_config")
	result = append(result, "+++ current_config")

	cleanFilter := strings.ToLower(strings.TrimSpace(filterQuery))

	for idx, c := range chunks {
		switch c.op {
		case diffDelete:
			if cleanFilter == "" || strings.Contains(strings.ToLower(c.line), cleanFilter) {
				result = append(result, fmt.Sprintf("- %s", c.line))
			}
		case diffInsert:
			if cleanFilter == "" || strings.Contains(strings.ToLower(c.line), cleanFilter) {
				result = append(result, fmt.Sprintf("+ %s", c.line))
			}
		case diffEqual:
			isNearChange := false
			contextDist := 2
			for offset := -contextDist; offset <= contextDist; offset++ {
				checkIdx := idx + offset
				if checkIdx >= 0 && checkIdx < len(chunks) {
					if chunks[checkIdx].op != diffEqual {
						isNearChange = true
						break
					}
				}
			}
			if isNearChange && cleanFilter == "" {
				result = append(result, fmt.Sprintf("  %s", c.line))
			}
		}
	}

	return result
}

func generateCLIDiff(oldCLI, newCLI []string, filterQuery string) []string {
	oldSet := make(map[string]bool, len(oldCLI))
	for _, l := range oldCLI {
		oldSet[l] = true
	}
	newSet := make(map[string]bool, len(newCLI))
	for _, l := range newCLI {
		newSet[l] = true
	}

	var diffLines []string

	for _, l := range oldCLI {
		if !newSet[l] {
			if filterQuery == "" || strings.Contains(strings.ToLower(l), filterQuery) {
				delStmt := strings.Replace(l, "set /", "- delete /", 1)
				diffLines = append(diffLines, delStmt)
			}
		}
	}

	for _, l := range newCLI {
		if !oldSet[l] {
			if filterQuery == "" || strings.Contains(strings.ToLower(l), filterQuery) {
				addStmt := strings.Replace(l, "set /", "+ set /", 1)
				diffLines = append(diffLines, addStmt)
			}
		}
	}

	sort.Strings(diffLines)
	return diffLines
}

// DiffJSONStrings is a helper comparing two raw JSON strings.
func DiffJSONStrings(oldJSONStr, newJSONStr string, filterPath string) (models.SemanticDiffResult, error) {
	var oldMap, newMap map[string]interface{}
	if oldJSONStr != "" && oldJSONStr != "{}" {
		if err := json.Unmarshal([]byte(oldJSONStr), &oldMap); err != nil {
			return models.SemanticDiffResult{}, err
		}
	} else {
		oldMap = make(map[string]interface{})
	}

	if newJSONStr != "" && newJSONStr != "{}" {
		if err := json.Unmarshal([]byte(newJSONStr), &newMap); err != nil {
			return models.SemanticDiffResult{}, err
		}
	} else {
		newMap = make(map[string]interface{})
	}

	return SemanticDiff(oldMap, newMap, filterPath), nil
}
