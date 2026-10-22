package differ

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"timeline/pkg/filter"
	"timeline/pkg/models"
	"timeline/pkg/normalizer"
	"timeline/pkg/schema"
)

// SemanticDiff compares two configurations and returns a SemanticDiffResult.
func SemanticDiff(oldCfg, newCfg map[string]interface{}, filterPath string) models.SemanticDiffResult {
	normOld, _ := normalizer.NormalizeStructure(oldCfg, true).(map[string]interface{})
	if normOld == nil {
		normOld = make(map[string]interface{})
	}
	normNew, _ := normalizer.NormalizeStructure(newCfg, true).(map[string]interface{})
	if normNew == nil {
		normNew = make(map[string]interface{})
	}

	cleanFilter := strings.TrimSpace(filterPath)

	var changes []models.PathChange
	diffJSONTrees("", normOld, normNew, &changes)

	// Filter changes if filterPath is supplied
	var filteredChanges []models.PathChange
	for _, c := range changes {
		if cleanFilter == "" || filter.IsPathMatching(c.Path, cleanFilter) {
			filteredChanges = append(filteredChanges, c)
		}
	}

	var added, modified, deleted int
	for i, c := range filteredChanges {
		if (c.DiffType == models.DiffAdded || c.DiffType == models.DiffDeleted) &&
			i+1 < len(filteredChanges) && strings.HasPrefix(filteredChanges[i+1].Path, c.Path+"/") {
			continue
		}
		switch c.DiffType {
		case models.DiffAdded:
			added++
		case models.DiffModified:
			modified++
		case models.DiffDeleted:
			deleted++
		}
	}

	// Generate Unified Diff: if filtered, diff the filtered subtrees
	var oldJSON, newJSON string
	if cleanFilter != "" {
		filtOld := filter.FilterConfigSubtree(normOld, filterPath)
		filtNew := filter.FilterConfigSubtree(normNew, filterPath)
		oldJSON, _ = normalizer.CanonicalJSONString(filtOld, 2)
		newJSON, _ = normalizer.CanonicalJSONString(filtNew, 2)
	} else {
		oldJSON, _ = normalizer.CanonicalJSONString(normOld, 2)
		newJSON, _ = normalizer.CanonicalJSONString(normNew, 2)
	}
	unifiedDiff := generateUnifiedDiff(oldJSON, newJSON, "")

	// Generate CLI Diff
	oldCLI := normalizer.JSONToFlatCLI(normOld, "")
	newCLI := normalizer.JSONToFlatCLI(normNew, "")
	cliDiff := generateCLIDiff(oldCLI, newCLI, cleanFilter)

	hasChanges := len(filteredChanges) > 0 || len(cliDiff) > 0 || len(unifiedDiff) > 2

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

// SemanticDiffFast computes structural PathChanges and CLIDiffLines without serializing full JSON unified diffs.
func SemanticDiffFast(oldCfg, newCfg map[string]interface{}) models.SemanticDiffResult {
	normOld, _ := normalizer.NormalizeStructure(oldCfg, true).(map[string]interface{})
	if normOld == nil {
		normOld = make(map[string]interface{})
	}
	normNew, _ := normalizer.NormalizeStructure(newCfg, true).(map[string]interface{})
	if normNew == nil {
		normNew = make(map[string]interface{})
	}

	var changes []models.PathChange
	diffJSONTrees("", normOld, normNew, &changes)

	var added, modified, deleted int
	for i, c := range changes {
		if (c.DiffType == models.DiffAdded || c.DiffType == models.DiffDeleted) &&
			i+1 < len(changes) && strings.HasPrefix(changes[i+1].Path, c.Path+"/") {
			continue
		}
		switch c.DiffType {
		case models.DiffAdded:
			added++
		case models.DiffModified:
			modified++
		case models.DiffDeleted:
			deleted++
		}
	}

	oldCLI := normalizer.JSONToFlatCLI(normOld, "")
	newCLI := normalizer.JSONToFlatCLI(normNew, "")
	cliDiff := generateCLIDiff(oldCLI, newCLI, "")

	return models.SemanticDiffResult{
		HasChanges:    len(changes) > 0 || len(cliDiff) > 0,
		AddedCount:    added,
		ModifiedCount: modified,
		DeletedCount:  deleted,
		Changes:       changes,
		CLIDiffLines:  cliDiff,
	}
}

func isConsumedListKeyField(currentPath, fieldName string, itemMap map[string]interface{}) bool {
	if !strings.HasSuffix(currentPath, "]") {
		return false
	}
	listName := ""
	if idx := strings.LastIndex(currentPath, "/"); idx != -1 {
		listName = currentPath[idx+1:]
		if bIdx := strings.Index(listName, "["); bIdx != -1 {
			listName = listName[:bIdx]
		}
	}
	cleanField := normalizer.StripNamespace(fieldName)
	keys := schema.GetRegistry().GetListKeys(currentPath, listName, itemMap)
	for _, k := range keys {
		if strings.EqualFold(cleanField, k) {
			return true
		}
	}
	return false
}

func diffJSONTrees(currentPath string, oldVal, newVal interface{}, changes *[]models.PathChange) {
	if reflect.DeepEqual(oldVal, newVal) {
		return
	}

	if oldVal == nil && newVal != nil {
		if list, ok := newVal.([]interface{}); ok && len(list) > 0 {
			if _, isMap := list[0].(map[string]interface{}); isMap {
				newKeyed := makeKeyedListMap(currentPath, list)
				var sortedListKeys []string
				for k := range newKeyed {
					sortedListKeys = append(sortedListKeys, k)
				}
				sort.Strings(sortedListKeys)
				for _, lk := range sortedListKeys {
					itemSubPath := currentPath
					if lk != "" {
						itemSubPath = fmt.Sprintf("%s[%s]", currentPath, lk)
					}
					diffJSONTrees(itemSubPath, nil, newKeyed[lk], changes)
				}
				return
			}
		}
		*changes = append(*changes, models.PathChange{
			Path:     currentPath,
			DiffType: models.DiffAdded,
			NewValue: newVal,
		})
		if m, ok := newVal.(map[string]interface{}); ok {
			var sortedKeys []string
			for k := range m {
				if isConsumedListKeyField(currentPath, k, m) {
					continue
				}
				sortedKeys = append(sortedKeys, k)
			}
			sort.Strings(sortedKeys)
			for _, k := range sortedKeys {
				subPath := fmt.Sprintf("%s/%s", currentPath, k)
				diffJSONTrees(subPath, nil, m[k], changes)
			}
		}
		return
	}

	if oldVal != nil && newVal == nil {
		if list, ok := oldVal.([]interface{}); ok && len(list) > 0 {
			if _, isMap := list[0].(map[string]interface{}); isMap {
				oldKeyed := makeKeyedListMap(currentPath, list)
				var sortedListKeys []string
				for k := range oldKeyed {
					sortedListKeys = append(sortedListKeys, k)
				}
				sort.Strings(sortedListKeys)
				for _, lk := range sortedListKeys {
					itemSubPath := currentPath
					if lk != "" {
						itemSubPath = fmt.Sprintf("%s[%s]", currentPath, lk)
					}
					diffJSONTrees(itemSubPath, oldKeyed[lk], nil, changes)
				}
				return
			}
		}
		*changes = append(*changes, models.PathChange{
			Path:     currentPath,
			DiffType: models.DiffDeleted,
			OldValue: oldVal,
		})
		if m, ok := oldVal.(map[string]interface{}); ok {
			var sortedKeys []string
			for k := range m {
				if isConsumedListKeyField(currentPath, k, m) {
					continue
				}
				sortedKeys = append(sortedKeys, k)
			}
			sort.Strings(sortedKeys)
			for _, k := range sortedKeys {
				subPath := fmt.Sprintf("%s/%s", currentPath, k)
				diffJSONTrees(subPath, m[k], nil, changes)
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
		oldKeyed := makeKeyedListMap(currentPath, oldList)
		newKeyed := makeKeyedListMap(currentPath, newList)

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

func makeKeyedListMap(currentPath string, list []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(list))
	listName := ""
	if idx := strings.LastIndex(currentPath, "/"); idx != -1 {
		listName = currentPath[idx+1:]
		if bIdx := strings.Index(listName, "["); bIdx != -1 {
			listName = listName[:bIdx]
		}
	}

	for idx, item := range list {
		if elemMap, ok := item.(map[string]interface{}); ok {
			keys := schema.GetRegistry().GetListKeys(currentPath, listName, elemMap)
			if len(keys) > 0 {
				var keyParts []string
				for _, k := range keys {
					if val, found := elemMap[k]; found {
						keyParts = append(keyParts, fmt.Sprintf("%s=%v", k, val))
					}
				}
				if len(keyParts) == len(keys) {
					m[strings.Join(keyParts, "][")] = item
					continue
				}
			}

			var keyVal string
			for _, k := range normalizer.WellKnownListKeys {
				if val, found := elemMap[k]; found {
					switch val.(type) {
					case map[string]interface{}, []interface{}:
						continue
					default:
						keyVal = fmt.Sprintf("%s=%v", k, val)
					}
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

// computeLCSDiff computes a line diff with common prefix/suffix trimming and bounded memory.
func computeLCSDiff(oldLines, newLines []string) []diffChunk {
	n := len(oldLines)
	m := len(newLines)

	// 1. Trim common prefix lines in O(min(N, M))
	prefixLen := 0
	for prefixLen < n && prefixLen < m && oldLines[prefixLen] == newLines[prefixLen] {
		prefixLen++
	}

	// 2. Trim common suffix lines in O(min(N, M))
	suffixLen := 0
	for suffixLen < (n-prefixLen) && suffixLen < (m-prefixLen) &&
		oldLines[n-1-suffixLen] == newLines[m-1-suffixLen] {
		suffixLen++
	}

	midOld := oldLines[prefixLen : n-suffixLen]
	midNew := newLines[prefixLen : m-suffixLen]

	var midChunks []diffChunk
	if len(midOld) == 0 {
		for _, l := range midNew {
			midChunks = append(midChunks, diffChunk{op: diffInsert, line: l})
		}
	} else if len(midNew) == 0 {
		for _, l := range midOld {
			midChunks = append(midChunks, diffChunk{op: diffDelete, line: l})
		}
	} else if int64(len(midOld))*int64(len(midNew)) <= 250000 {
		midChunks = computeSmallDPDiff(midOld, midNew)
	} else {
		midChunks = computeMyersDiff(midOld, midNew)
	}

	totalCap := prefixLen + len(midChunks) + suffixLen
	result := make([]diffChunk, 0, totalCap)
	for i := 0; i < prefixLen; i++ {
		result = append(result, diffChunk{op: diffEqual, line: oldLines[i]})
	}
	result = append(result, midChunks...)
	for i := n - suffixLen; i < n; i++ {
		result = append(result, diffChunk{op: diffEqual, line: oldLines[i]})
	}
	return result
}

func computeSmallDPDiff(oldLines, newLines []string) []diffChunk {
	n := len(oldLines)
	m := len(newLines)

	// Allocate a single flat backing slice instead of n+1 separate heap allocations
	stride := m + 1
	flat := make([]int32, (n+1)*stride)

	for i := 1; i <= n; i++ {
		row := i * stride
		prevRow := (i - 1) * stride
		oldLine := oldLines[i-1]
		for j := 1; j <= m; j++ {
			if oldLine == newLines[j-1] {
				flat[row+j] = flat[prevRow+j-1] + 1
			} else if flat[prevRow+j] >= flat[row+j-1] {
				flat[row+j] = flat[prevRow+j]
			} else {
				flat[row+j] = flat[row+j-1]
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
		} else if j > 0 && (i == 0 || flat[i*stride+j-1] >= flat[(i-1)*stride+j]) {
			chunks = append(chunks, diffChunk{op: diffInsert, line: newLines[j-1]})
			j--
		} else if i > 0 {
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

// computeMyersDiff implements Myers O(ND) diff with a safe maximum edit bound so large files never OOM.
func computeMyersDiff(oldLines, newLines []string) []diffChunk {
	n := len(oldLines)
	m := len(newLines)
	maxD := n + m
	const maxEditBound = 2048
	if maxD > maxEditBound {
		maxD = maxEditBound
	}

	vSize := 2*maxD + 3
	offset := maxD + 1
	v := make([]int, vSize)
	var trace [][]int

	reached := false
	for d := 0; d <= maxD; d++ {
		snapshot := make([]int, vSize)
		copy(snapshot, v)
		trace = append(trace, snapshot)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+offset] < v[k+1+offset]) {
				x = v[k+1+offset]
			} else {
				x = v[k-1+offset] + 1
			}
			y := x - k
			for x < n && y < m && oldLines[x] == newLines[y] {
				x++
				y++
			}
			v[k+offset] = x
			if x >= n && y >= m {
				reached = true
				break
			}
		}
		if reached {
			break
		}
	}

	if !reached {
		// Linear fallback when edit distance exceeds maxEditBound
		chunks := make([]diffChunk, 0, n+m)
		for _, l := range oldLines {
			chunks = append(chunks, diffChunk{op: diffDelete, line: l})
		}
		for _, l := range newLines {
			chunks = append(chunks, diffChunk{op: diffInsert, line: l})
		}
		return chunks
	}

	var chunks []diffChunk
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		vPrev := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && vPrev[k-1+offset] < vPrev[k+1+offset]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vPrev[prevK+offset]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			chunks = append(chunks, diffChunk{op: diffEqual, line: oldLines[x-1]})
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				chunks = append(chunks, diffChunk{op: diffInsert, line: newLines[y-1]})
				y--
			} else {
				chunks = append(chunks, diffChunk{op: diffDelete, line: oldLines[x-1]})
				x--
			}
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
			if filterQuery == "" || filter.IsPathMatching(l, filterQuery) {
				delStmt := strings.Replace(l, "set /", "- delete /", 1)
				diffLines = append(diffLines, delStmt)
			}
		}
	}

	for _, l := range newCLI {
		if !oldSet[l] {
			if filterQuery == "" || filter.IsPathMatching(l, filterQuery) {
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
