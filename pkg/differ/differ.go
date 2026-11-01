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

// computeLCSDiff computes a line diff with common prefix/suffix trimming,
// structural JSON path awareness, and bracket-aligned slider compaction.
func computeLCSDiff(oldLines, newLines []string) []diffChunk {
	n := len(oldLines)
	m := len(newLines)

	oldKeys, okOld := annotateJSONLines(oldLines)
	newKeys, okNew := annotateJSONLines(newLines)
	if !okOld || !okNew {
		oldKeys = oldLines
		newKeys = newLines
	}

	// 1. Trim common prefix lines in O(min(N, M))
	prefixLen := 0
	for prefixLen < n && prefixLen < m && oldKeys[prefixLen] == newKeys[prefixLen] {
		prefixLen++
	}

	// 2. Trim common suffix lines in O(min(N, M))
	suffixLen := 0
	for suffixLen < (n-prefixLen) && suffixLen < (m-prefixLen) &&
		oldKeys[n-1-suffixLen] == newKeys[m-1-suffixLen] {
		suffixLen++
	}

	midOld := oldLines[prefixLen : n-suffixLen]
	midNew := newLines[prefixLen : m-suffixLen]
	midOldKeys := oldKeys[prefixLen : n-suffixLen]
	midNewKeys := newKeys[prefixLen : m-suffixLen]

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
		midChunks = computeSmallDPDiffWithKeys(midOld, midNew, midOldKeys, midNewKeys)
	} else {
		midChunks = computeMyersDiffWithKeys(midOld, midNew, midOldKeys, midNewKeys)
	}

	totalCap := prefixLen + len(midChunks) + suffixLen
	result := make([]diffChunk, 0, totalCap)
	for i := 0; i < prefixLen; i++ {
		result = append(result, diffChunk{op: diffEqual, line: newLines[i]})
	}
	result = append(result, midChunks...)
	for s := 0; s < suffixLen; s++ {
		result = append(result, diffChunk{op: diffEqual, line: newLines[m-suffixLen+s]})
	}
	return compactDiffSliders(result)
}

type parsedJSONLine struct {
	trimmed    string
	normLine   string
	propKey    string
	rest       string
	hasKey     bool
	opensObj   bool
	opensArr   bool
	closesObj  bool
	closesArr  bool
	matchClose int
}

// annotateJSONLines tags each line of a formatted multi-line JSON document with its
// structural path and trailing-comma-normalized content so sibling blocks never steal brackets.
func annotateJSONLines(lines []string) ([]string, bool) {
	if len(lines) < 2 {
		return nil, false
	}
	firstTrimmed := strings.TrimSpace(lines[0])
	if firstTrimmed != "{" && firstTrimmed != "[" {
		return nil, false
	}

	parsed := make([]parsedJSONLine, len(lines))
	var openStack []int

	for i, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		normLine := strings.TrimSuffix(strings.TrimRight(rawLine, " \t\r"), ",")
		p := parsedJSONLine{
			trimmed:    trimmed,
			normLine:   normLine,
			matchClose: -1,
		}
		if strings.HasPrefix(trimmed, "\"") {
			escaped := false
			quoteEnd := -1
			for j := 1; j < len(trimmed); j++ {
				if escaped {
					escaped = false
					continue
				}
				if trimmed[j] == '\\' {
					escaped = true
					continue
				}
				if trimmed[j] == '"' {
					quoteEnd = j
					break
				}
			}
			if quoteEnd > 0 && strings.HasPrefix(trimmed[quoteEnd+1:], ": ") {
				p.hasKey = true
				p.propKey = normalizer.StripNamespace(trimmed[1:quoteEnd])
				p.rest = strings.TrimSpace(trimmed[quoteEnd+3:])
			}
		}

		if p.hasKey {
			if p.rest == "{" {
				p.opensObj = true
			} else if p.rest == "[" {
				p.opensArr = true
			}
		} else {
			switch trimmed {
			case "{":
				p.opensObj = true
			case "[":
				p.opensArr = true
			case "}", "},":
				p.closesObj = true
			case "]", "],":
				p.closesArr = true
			}
		}

		parsed[i] = p
		if p.opensObj || p.opensArr {
			openStack = append(openStack, i)
		} else if p.closesObj || p.closesArr {
			if len(openStack) == 0 {
				return nil, false
			}
			top := openStack[len(openStack)-1]
			openStack = openStack[:len(openStack)-1]
			if (parsed[top].opensObj && !p.closesObj) || (parsed[top].opensArr && !p.closesArr) {
				return nil, false
			}
			parsed[top].matchClose = i
		}
	}

	if len(openStack) != 0 || parsed[0].matchClose != len(parsed)-1 {
		return nil, false
	}

	keys := make([]string, len(parsed))
	var walkBlock func(openIdx int, currentPath, listName string)
	walkBlock = func(openIdx int, currentPath, listName string) {
		closeIdx := parsed[openIdx].matchClose
		keys[openIdx] = currentPath + "\x00" + parsed[openIdx].normLine
		keys[closeIdx] = currentPath + "\x00" + parsed[closeIdx].normLine

		if parsed[openIdx].opensObj {
			for k := openIdx + 1; k < closeIdx; k++ {
				childPath := currentPath
				propKey := parsed[k].propKey
				if propKey != "" {
					if currentPath == "" || currentPath == "/" {
						childPath = "/" + propKey
					} else {
						childPath = currentPath + "/" + propKey
					}
				}
				if parsed[k].matchClose > k {
					walkBlock(k, childPath, propKey)
					k = parsed[k].matchClose
				} else {
					keys[k] = childPath + "\x00" + parsed[k].normLine
				}
			}
		} else if parsed[openIdx].opensArr {
			seenQuals := make(map[string]int)
			for k := openIdx + 1; k < closeIdx; k++ {
				if parsed[k].opensObj && parsed[k].matchClose > k {
					elemQual := extractJSONArrayElemQualifier(parsed, k, parsed[k].matchClose, currentPath, listName)
					if elemQual != "" {
						seenQuals[elemQual]++
						if seenQuals[elemQual] > 1 {
							elemQual = fmt.Sprintf("%s#%d", elemQual, seenQuals[elemQual])
						}
					}
					elemPath := currentPath + elemQual
					walkBlock(k, elemPath, listName)
					k = parsed[k].matchClose
				} else if parsed[k].matchClose > k {
					walkBlock(k, currentPath, listName)
					k = parsed[k].matchClose
				} else {
					keys[k] = currentPath + "\x00" + parsed[k].normLine
				}
			}
		}
	}

	walkBlock(0, "/", "")
	return keys, true
}

var nonIdentifyingJSONFields = map[string]bool{
	"admin-state": true,
	"comment":     true,
	"description": true,
	"enable":      true,
	"enabled":     true,
	"message":     true,
	"oper-state":  true,
	"state":       true,
	"status":      true,
	"text":        true,
}

func extractJSONArrayElemQualifier(parsed []parsedJSONLine, openIdx, closeIdx int, currentPath, listName string) string {
	elemMap := make(map[string]interface{})
	for m := openIdx + 1; m < closeIdx; m++ {
		if parsed[m].matchClose > m {
			m = parsed[m].matchClose
			continue
		}
		if parsed[m].hasKey {
			valStr := strings.TrimSuffix(parsed[m].rest, ",")
			if valStr == "{}" || valStr == "[]" {
				continue
			}
			if len(valStr) >= 2 && strings.HasPrefix(valStr, "\"") && strings.HasSuffix(valStr, "\"") {
				valStr = normalizer.StripModulePrefixValue(valStr[1 : len(valStr)-1])
			}
			elemMap[strings.ToLower(parsed[m].propKey)] = valStr
		}
	}
	if len(elemMap) == 0 {
		// Check nested containers (e.g., OpenConfig config/state subcontainers) for a scalar key
		for m := openIdx + 1; m < closeIdx; m++ {
			if parsed[m].hasKey && ! parsed[m].opensObj && !parsed[m].opensArr {
				valStr := strings.TrimSuffix(parsed[m].rest, ",")
				if valStr == "{}" || valStr == "[]" {
					continue
				}
				if len(valStr) >= 2 && strings.HasPrefix(valStr, "\"") && strings.HasSuffix(valStr, "\"") {
					valStr = normalizer.StripModulePrefixValue(valStr[1 : len(valStr)-1])
				}
				kLower := strings.ToLower(parsed[m].propKey)
				if !nonIdentifyingJSONFields[kLower] {
					return fmt.Sprintf("[%s=%v]", kLower, valStr)
				}
			}
		}
		return ""
	}

	regKeys := schema.GetRegistry().GetListKeys(currentPath, listName, elemMap)
	if len(regKeys) > 0 {
		var keyParts []string
		for _, rk := range regKeys {
			if val, found := elemMap[strings.ToLower(rk)]; found {
				keyParts = append(keyParts, fmt.Sprintf("%s=%v", rk, val))
			}
		}
		if len(keyParts) == len(regKeys) {
			return "[" + strings.Join(keyParts, "][") + "]"
		}
	}

	for _, wk := range normalizer.WellKnownListKeys {
		if val, found := elemMap[wk]; found {
			return fmt.Sprintf("[%s=%v]", wk, val)
		}
	}

	var sortedKeys []string
	for k := range elemMap {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	for _, k := range sortedKeys {
		if !nonIdentifyingJSONFields[k] {
			return fmt.Sprintf("[%s=%v]", k, elemMap[k])
		}
	}
	return fmt.Sprintf("[%s=%v]", sortedKeys[0], elemMap[sortedKeys[0]])
}

func trimJSONLineComma(s string) string {
	return strings.TrimSuffix(strings.TrimRight(s, " \t\r"), ",")
}

func lineOpensJSONBlock(trimmed string) bool {
	if trimmed == "{" || trimmed == "[" {
		return true
	}
	return strings.HasPrefix(trimmed, "\"") && (strings.HasSuffix(trimmed, ": {") || strings.HasSuffix(trimmed, ": ["))
}

func lineClosesJSONBlock(trimmed string) bool {
	switch trimmed {
	case "}", "},", "]", "],":
		return true
	default:
		return false
	}
}

func countLeadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

func scoreDiffHunkWindow(chunks []diffChunk, start, length int) int {
	if length <= 0 {
		return 0
	}
	firstLine := chunks[start].line
	lastLine := chunks[start+length-1].line
	firstTrim := strings.TrimSpace(firstLine)
	lastTrim := strings.TrimSpace(lastLine)

	depth := 0
	minDepth := 0
	for i := start; i < start+length; i++ {
		t := strings.TrimSpace(chunks[i].line)
		if lineOpensJSONBlock(t) {
			depth++
		} else if lineClosesJSONBlock(t) {
			depth--
			if depth < minDepth {
				minDepth = depth
			}
		}
	}

	score := 0
	if minDepth == 0 && depth == 0 {
		score += 100
	}
	if lineOpensJSONBlock(firstTrim) && lineClosesJSONBlock(lastTrim) &&
		countLeadingSpaces(firstLine) == countLeadingSpaces(lastLine) {
		score += 200
	}
	if lineClosesJSONBlock(firstTrim) {
		score -= 100
	}
	if lineOpensJSONBlock(lastTrim) {
		score -= 100
	}
	return score
}

// compactDiffSliders slides insertion or deletion hunks across adjacent equal lines
// so that added or deleted JSON blocks align cleanly with their enclosing brackets.
func compactDiffSliders(chunks []diffChunk) []diffChunk {
	n := len(chunks)
	i := 0
	for i < n {
		op := chunks[i].op
		if op == diffEqual {
			i++
			continue
		}
		rawStart := i
		rawEnd := i
		for rawEnd < n && chunks[rawEnd].op == op {
			rawEnd++
		}

		start := rawStart
		end := rawEnd
		prevNotEqual := start > 0 && chunks[start-1].op != diffEqual
		nextNotEqual := end < n && chunks[end].op != diffEqual

		if prevNotEqual && nextNotEqual {
			i = rawEnd
			continue
		}
		if prevNotEqual {
			// If preceded by a modification, check if a suffix starting at a closing bracket can slide right
			subFound := -1
			if end < n && chunks[end].op == diffEqual {
				for s := start + 1; s < end; s++ {
					if lineClosesJSONBlock(strings.TrimSpace(chunks[s].line)) &&
						trimJSONLineComma(chunks[s].line) == trimJSONLineComma(chunks[end].line) {
						subFound = s
						break
					}
				}
			}
			if subFound == -1 {
				i = rawEnd
				continue
			}
			start = subFound
		} else if nextNotEqual {
			// If followed by a modification, check if a prefix ending at an opening bracket can slide left
			subFound := -1
			if start > 0 && chunks[start-1].op == diffEqual {
				for s := end - 1; s > start; s-- {
					if lineOpensJSONBlock(strings.TrimSpace(chunks[s-1].line)) &&
						trimJSONLineComma(chunks[start-1].line) == trimJSONLineComma(chunks[s-1].line) {
						subFound = s
						break
					}
				}
			}
			if subFound == -1 {
				i = rawEnd
				continue
			}
			end = subFound
		}

		runLen := end - start
		minStart := start
		if !prevNotEqual {
			for minStart > 0 && chunks[minStart-1].op == diffEqual &&
				trimJSONLineComma(chunks[minStart-1].line) == trimJSONLineComma(chunks[minStart+runLen-1].line) {
				minStart--
			}
		}
		maxStart := start
		if !nextNotEqual {
			for maxStart+runLen < n && chunks[maxStart+runLen].op == diffEqual &&
				trimJSONLineComma(chunks[maxStart+runLen].line) == trimJSONLineComma(chunks[maxStart].line) {
				maxStart++
			}
		}

		if minStart < maxStart {
			var eqLines []string
			if op == diffDelete {
				for k := minStart; k < maxStart+runLen; k++ {
					if chunks[k].op == diffEqual {
						eqLines = append(eqLines, chunks[k].line)
					}
				}
			}
			bestStart := minStart
			bestScore := scoreDiffHunkWindow(chunks, minStart, runLen)
			for s := minStart + 1; s <= maxStart; s++ {
				sc := scoreDiffHunkWindow(chunks, s, runLen)
				if sc > bestScore {
					bestScore = sc
					bestStart = s
				}
			}
			eqIdx := 0
			for k := minStart; k < maxStart+runLen; k++ {
				if k >= bestStart && k < bestStart+runLen {
					chunks[k].op = op
				} else {
					chunks[k].op = diffEqual
					if op == diffDelete && eqIdx < len(eqLines) {
						chunks[k].line = eqLines[eqIdx]
						eqIdx++
					}
				}
			}
		}
		if maxStart+runLen > rawEnd {
			i = maxStart + runLen
		} else {
			i = rawEnd
		}
	}
	return chunks
}

func computeSmallDPDiff(oldLines, newLines []string) []diffChunk {
	return computeSmallDPDiffWithKeys(oldLines, newLines, oldLines, newLines)
}

func computeSmallDPDiffWithKeys(oldLines, newLines, oldKeys, newKeys []string) []diffChunk {
	n := len(oldLines)
	m := len(newLines)

	// Allocate a single flat backing slice instead of n+1 separate heap allocations
	stride := m + 1
	flat := make([]int32, (n+1)*stride)

	for i := 1; i <= n; i++ {
		row := i * stride
		prevRow := (i - 1) * stride
		oldKey := oldKeys[i-1]
		for j := 1; j <= m; j++ {
			if oldKey == newKeys[j-1] {
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
		if i > 0 && j > 0 && oldKeys[i-1] == newKeys[j-1] {
			chunks = append(chunks, diffChunk{op: diffEqual, line: newLines[j-1]})
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
	return computeMyersDiffWithKeys(oldLines, newLines, oldLines, newLines)
}

func computeMyersDiffWithKeys(oldLines, newLines, oldKeys, newKeys []string) []diffChunk {
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
			for x < n && y < m && oldKeys[x] == newKeys[y] {
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
			chunks = append(chunks, diffChunk{op: diffEqual, line: newLines[y-1]})
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
