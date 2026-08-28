package filter

import (
	"strings"

	"timeline/pkg/models"
	"timeline/pkg/normalizer"
	"timeline/pkg/schema"
)

// IsPathMatching checks if a target path matches a filter query.
func IsPathMatching(targetPath, filterQuery string) bool {
	cleanFilter := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(filterQuery, "/")))
	if cleanFilter == "" {
		return true
	}
	cleanTarget := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(targetPath, "/")))
	return strings.Contains(cleanTarget, cleanFilter)
}

// TokenizePath converts an XPath, CLI path, or slash path into lower-cased semantic tokens.
func TokenizePath(path string) []string {
	var tokens []string
	var current strings.Builder
	inBracket := false

	trimmed := strings.TrimSpace(path)
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		switch ch {
		case '[':
			inBracket = true
			if current.Len() > 0 {
				token := strings.Trim(current.String(), "/ ")
				if token != "" {
					tokens = append(tokens, strings.ToLower(token))
				}
				current.Reset()
			}
		case ']':
			inBracket = false
			val := current.String()
			if idx := strings.Index(val, "="); idx != -1 {
				val = val[idx+1:]
			}
			val = strings.Trim(val, "\"' ")
			if val != "" {
				tokens = append(tokens, strings.ToLower(val))
			}
			current.Reset()
		case '/':
			if inBracket {
				current.WriteByte(ch)
			} else {
				token := strings.Trim(current.String(), "/ ")
				if token != "" {
					tokens = append(tokens, strings.ToLower(token))
				}
				current.Reset()
			}
		case ' ', '\t':
			if inBracket {
				current.WriteByte(ch)
			} else {
				token := strings.Trim(current.String(), "/ ")
				if token != "" {
					tokens = append(tokens, strings.ToLower(token))
				}
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		token := strings.Trim(current.String(), "/ ")
		if token != "" {
			tokens = append(tokens, strings.ToLower(token))
		}
	}

	return tokens
}

func isPrefixOf(tokensA, tokensB []string) bool {
	if len(tokensA) > len(tokensB) {
		return false
	}
	for i := range tokensA {
		if tokensA[i] != tokensB[i] {
			return false
		}
	}
	return true
}

func tokensMatch(queryTokens, targetTokens []string) bool {
	if len(queryTokens) == 0 {
		return true
	}
	if len(targetTokens) == 0 {
		return false
	}
	if isPrefixOf(queryTokens, targetTokens) || isPrefixOf(targetTokens, queryTokens) {
		return true
	}
	qIdx := 0
	for _, t := range targetTokens {
		if qIdx < len(queryTokens) && (t == queryTokens[qIdx] || strings.Contains(t, queryTokens[qIdx])) {
			qIdx++
		}
	}
	return qIdx == len(queryTokens)
}

// FilterConfigSubtree extracts configuration subtrees matching the filter query.
func FilterConfigSubtree(config map[string]interface{}, filterQuery string) map[string]interface{} {
	queryTokens := TokenizePath(filterQuery)
	if len(queryTokens) == 0 {
		return config
	}

	result := make(map[string]interface{})
	for k, v := range config {
		cleanKey := strings.ToLower(normalizer.StripNamespace(k))
		keyTokens := []string{cleanKey}
		if tokensMatch(queryTokens, keyTokens) || isPrefixOf(keyTokens, queryTokens) {
			childRes := filterSubtreeHelper(v, queryTokens, keyTokens)
			if childRes != nil {
				result[k] = childRes
			}
		}
	}

	if len(result) == 0 {
		// Try deeper search across all top-level keys
		for k, v := range config {
			cleanKey := strings.ToLower(normalizer.StripNamespace(k))
			keyTokens := []string{cleanKey}
			childRes := filterSubtreeHelper(v, queryTokens, keyTokens)
			if childRes != nil {
				result[k] = childRes
			}
		}
	}

	return result
}

func filterSubtreeHelper(node interface{}, queryTokens, currentTokens []string) interface{} {
	if isPrefixOf(queryTokens, currentTokens) {
		return node
	}

	switch v := node.(type) {
	case map[string]interface{}:
		filteredMap := make(map[string]interface{})
		for k, child := range v {
			cleanK := strings.ToLower(normalizer.StripNamespace(k))
			childTokens := append(append([]string{}, currentTokens...), cleanK)
			if isPrefixOf(childTokens, queryTokens) || isPrefixOf(queryTokens, childTokens) || tokensMatch(queryTokens, childTokens) {
				childRes := filterSubtreeHelper(child, queryTokens, childTokens)
				if childRes != nil {
					filteredMap[k] = childRes
				}
			}
		}
		if len(filteredMap) > 0 {
			return filteredMap
		}
		return nil

	case []interface{}:
		var filteredList []interface{}
		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				var keyVal string
				var listName string
				if len(currentTokens) > 0 {
					listName = currentTokens[len(currentTokens)-1]
				}
				keys := schema.GetRegistry().GetListKeys("", listName, itemMap)
				for _, lk := range keys {
					for ik, iv := range itemMap {
						if strings.ToLower(normalizer.StripNamespace(ik)) == strings.ToLower(lk) {
							keyVal = strings.ToLower(normalizer.FormatScalarCLI(iv))
							break
						}
					}
					if keyVal != "" {
						break
					}
				}
				itemTokens := currentTokens
				if keyVal != "" {
					itemTokens = append(append([]string{}, currentTokens...), keyVal)
				}

				if isPrefixOf(queryTokens, itemTokens) {
					filteredList = append(filteredList, item)
				} else if isPrefixOf(itemTokens, queryTokens) || tokensMatch(queryTokens, itemTokens) {
					childRes := filterSubtreeHelper(item, queryTokens, itemTokens)
					if childRes != nil {
						if childMap, ok := childRes.(map[string]interface{}); ok {
							// Always preserve list identifying key fields from the original itemMap
							for k, v := range itemMap {
								cleanK := normalizer.StripNamespace(k)
								if isListKeyName(cleanK) {
									if _, exists := childMap[k]; !exists {
										childMap[k] = v
									}
								}
							}
							filteredList = append(filteredList, childMap)
						} else {
							filteredList = append(filteredList, childRes)
						}
					}
				}
			} else {
				if tokensMatch(queryTokens, currentTokens) {
					filteredList = append(filteredList, item)
				}
			}
		}
		if len(filteredList) > 0 {
			return filteredList
		}
		return nil

	default:
		if tokensMatch(queryTokens, currentTokens) {
			return node
		}
		return nil
	}
}

// FilterCLILines filters CLI statements by query.
func FilterCLILines(lines []string, filterQuery string) []string {
	cleanFilter := strings.ToLower(strings.TrimSpace(filterQuery))
	if cleanFilter == "" {
		return lines
	}

	var matched []string
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), cleanFilter) {
			matched = append(matched, l)
		}
	}
	return matched
}

// FilterDiffResult filters a SemanticDiffResult to only contain changes matching the filter.
func FilterDiffResult(diffRes models.SemanticDiffResult, filterQuery string) models.SemanticDiffResult {
	cleanFilter := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(filterQuery, "/")))
	if cleanFilter == "" {
		return diffRes
	}

	var filteredChanges []models.PathChange
	for _, c := range diffRes.Changes {
		if strings.Contains(strings.ToLower(c.Path), cleanFilter) {
			filteredChanges = append(filteredChanges, c)
		}
	}

	var filteredUnified []string
	for _, l := range diffRes.UnifiedDiffLines {
		if strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") {
			filteredUnified = append(filteredUnified, l)
		} else if strings.Contains(strings.ToLower(l), cleanFilter) {
			filteredUnified = append(filteredUnified, l)
		}
	}

	var filteredCLI []string
	for _, l := range diffRes.CLIDiffLines {
		if strings.Contains(strings.ToLower(l), cleanFilter) {
			filteredCLI = append(filteredCLI, l)
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

	return models.SemanticDiffResult{
		HasChanges:       len(filteredChanges) > 0 || len(filteredCLI) > 0,
		AddedCount:       added,
		ModifiedCount:    modified,
		DeletedCount:     deleted,
		Changes:          filteredChanges,
		UnifiedDiffLines: filteredUnified,
		CLIDiffLines:     filteredCLI,
	}
}

func isListKeyName(k string) bool {
	return schema.GetRegistry().IsListKey("", "", k, nil)
}

