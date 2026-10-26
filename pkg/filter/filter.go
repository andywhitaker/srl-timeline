package filter

import (
	"fmt"
	"strings"

	"timeline/pkg/models"
	"timeline/pkg/normalizer"
	"timeline/pkg/schema"
)

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// tokenSegmentMatches checks if targetToken matches queryToken without numeric suffix collisions
// (e.g. "ethernet-1/1" must NOT match "ethernet-1/10" or "ethernet-1/1.0").
func tokenSegmentMatches(targetToken, queryToken string) bool {
	if queryToken == "" {
		return true
	}
	if targetToken == queryToken {
		return true
	}
	searchFrom := 0
	for searchFrom <= len(targetToken)-len(queryToken) {
		idx := strings.Index(targetToken[searchFrom:], queryToken)
		if idx == -1 {
			return false
		}
		idx += searchFrom
		endIdx := idx + len(queryToken)

		valid := true
		if idx > 0 && isDigit(queryToken[0]) && isDigit(targetToken[idx-1]) {
			valid = false
		}
		if endIdx < len(targetToken) && isDigit(queryToken[len(queryToken)-1]) {
			nextCh := targetToken[endIdx]
			if isDigit(nextCh) || nextCh == '.' {
				valid = false
			}
		}
		if valid {
			return true
		}
		searchFrom = idx + 1
	}
	return false
}

// IsPathMatching checks if a target path (XPath, slash path, or CLI line) matches a filter query.
func IsPathMatching(targetPath, filterQuery string) bool {
	queryTokens := TokenizePath(filterQuery)
	if len(queryTokens) == 0 {
		return true
	}
	targetTokens := TokenizePath(targetPath)
	if strings.HasPrefix(strings.TrimSpace(filterQuery), "/") {
		return isExactPrefixOf(queryTokens, targetTokens)
	}
	return tokensMatch(queryTokens, targetTokens)
}

// TokenizePath converts an XPath, CLI statement, or slash path into lower-cased semantic tokens.
func TokenizePath(path string) []string {
	var rawTokens []string
	var current strings.Builder
	inBracket := false
	inQuote := false
	lastElement := ""

	flushElement := func() {
		if current.Len() > 0 {
			token := strings.Trim(current.String(), "/ ")
			if token != "" {
				lowerTok := strings.ToLower(normalizer.StripNamespace(token))
				rawTokens = append(rawTokens, lowerTok)
				lastElement = lowerTok
			}
			current.Reset()
		}
	}

	trimmed := strings.TrimSpace(path)
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		switch ch {
		case '"':
			inQuote = !inQuote
		case '[':
			if inQuote {
				current.WriteByte(ch)
				continue
			}
			inBracket = true
			flushElement()
		case ']':
			if inQuote {
				current.WriteByte(ch)
				continue
			}
			inBracket = false
			pred := strings.TrimSpace(current.String())
			current.Reset()
			keyName := ""
			val := pred
			if idx := strings.Index(pred, "="); idx != -1 {
				keyName = strings.ToLower(normalizer.StripNamespace(strings.TrimSpace(pred[:idx])))
				val = pred[idx+1:]
			}
			val = strings.Trim(val, "\"' ")
			if val != "" {
				lowerVal := strings.ToLower(val)
				if (lastElement == "acl-filter" || lastElement == "acl-set") && keyName == "type" {
					rawTokens = append(rawTokens, "type", lowerVal)
				} else if lastElement == "prefix" && keyName == "mask-length-range" {
					rawTokens = append(rawTokens, "mask-length-range", lowerVal)
				} else {
					rawTokens = append(rawTokens, lowerVal)
				}
			}
		case '/':
			if inBracket || inQuote || (i > 0 && i+1 < len(trimmed) && isDigit(trimmed[i+1]) && (isDigit(trimmed[i-1]) || strings.Contains(current.String(), ":"))) {
				current.WriteByte(ch)
			} else {
				flushElement()
			}
		case ' ', '\t':
			if inBracket || inQuote {
				current.WriteByte(ch)
			} else {
				flushElement()
			}
		default:
			current.WriteByte(ch)
		}
	}

	flushElement()

	// Strip leading CLI command verbs ("+", "-", "set", "delete")
	start := 0
	for start < len(rawTokens) {
		tok := rawTokens[start]
		if tok == "+" || tok == "-" || tok == "set" || tok == "delete" {
			start++
		} else {
			break
		}
	}
	return rawTokens[start:]
}

// XPathToCLIPath converts an XPath (e.g. "/interface[name=ethernet-1/1]/description" or
// "/acl/acl-filter[name=cpm][type=ipv4]/entry[sequence-id=10]") into flat SR Linux CLI path tokens.
func XPathToCLIPath(xpath string) string {
	trimmed := strings.TrimSpace(xpath)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	if !strings.Contains(trimmed, "[") && !strings.HasPrefix(trimmed, "/") {
		return strings.Join(strings.Fields(trimmed), " ")
	}

	var parts []string
	var cur strings.Builder
	inBracket := false
	lastElement := ""

	flushElement := func() {
		if cur.Len() > 0 {
			el := normalizer.StripNamespace(strings.Trim(cur.String(), "/ "))
			if el != "" {
				parts = append(parts, el)
				lastElement = strings.ToLower(el)
			}
			cur.Reset()
		}
	}

	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		switch ch {
		case '[':
			flushElement()
			inBracket = true
		case ']':
			inBracket = false
			pred := strings.TrimSpace(cur.String())
			cur.Reset()
			keyName := ""
			keyVal := pred
			if idx := strings.Index(pred, "="); idx != -1 {
				keyName = strings.ToLower(normalizer.StripNamespace(strings.TrimSpace(pred[:idx])))
				keyVal = strings.Trim(pred[idx+1:], "\"' ")
			}
			if keyVal != "" {
				formattedVal := normalizer.FormatScalarCLI(keyVal)
				if (lastElement == "acl-filter" || lastElement == "acl-set") && keyName == "type" {
					parts = append(parts, "type", formattedVal)
				} else if lastElement == "prefix" && keyName == "mask-length-range" {
					parts = append(parts, "mask-length-range", formattedVal)
				} else {
					parts = append(parts, formattedVal)
				}
			}
		case '/':
			if inBracket {
				cur.WriteByte(ch)
			} else {
				flushElement()
			}
		default:
			cur.WriteByte(ch)
		}
	}
	flushElement()

	return strings.Join(parts, " ")
}

func isExactPrefixOf(tokensA, tokensB []string) bool {
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

func isPrefixOf(tokensA, tokensB []string) bool {
	if len(tokensA) > len(tokensB) {
		return false
	}
	for i := range tokensA {
		if i == len(tokensA)-1 {
			if !tokenSegmentMatches(tokensB[i], tokensA[i]) {
				return false
			}
		} else if tokensA[i] != tokensB[i] {
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
	if isPrefixOf(queryTokens, targetTokens) {
		return true
	}
	qIdx := 0
	for _, t := range targetTokens {
		if qIdx < len(queryTokens) {
			qTok := queryTokens[qIdx]
			if qIdx < len(queryTokens)-1 {
				if t == qTok || tokenSegmentMatches(t, qTok) {
					qIdx++
				}
			} else {
				if tokenSegmentMatches(t, qTok) {
					qIdx++
				}
			}
		}
	}
	return qIdx == len(queryTokens)
}

// pathMatchesQuery returns true if the current token path matches the query.
func pathMatchesQuery(currentTokens, queryTokens []string, rawQuery string) bool {
	if rawQuery == "" || len(queryTokens) == 0 {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(rawQuery), "/") {
		return isExactPrefixOf(queryTokens, currentTokens)
	}
	return tokensMatch(queryTokens, currentTokens)
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
		if strings.HasPrefix(cleanKey, "_") {
			continue
		}
		keyTokens := []string{cleanKey}
		if pathMatchesQuery(keyTokens, queryTokens, filterQuery) {
			result[k] = v
		} else {
			childRes := filterSubtreeHelper(v, queryTokens, keyTokens, filterQuery)
			if childRes != nil {
				result[k] = childRes
			}
		}
	}

	return result
}

func filterSubtreeHelper(node interface{}, queryTokens, currentTokens []string, rawQuery string) interface{} {
	if pathMatchesQuery(currentTokens, queryTokens, rawQuery) {
		return node
	}

	switch v := node.(type) {
	case map[string]interface{}:
		filteredMap := make(map[string]interface{})
		for k, child := range v {
			cleanK := strings.ToLower(normalizer.StripNamespace(k))
			if strings.HasPrefix(cleanK, "_") {
				continue
			}
			childTokens := append(append([]string{}, currentTokens...), cleanK)
			if pathMatchesQuery(childTokens, queryTokens, rawQuery) {
				filteredMap[k] = child
			} else {
				childRes := filterSubtreeHelper(child, queryTokens, childTokens, rawQuery)
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
		currentPath := strings.Join(currentTokens, " ")
		listName := ""
		if len(currentTokens) > 0 {
			listName = currentTokens[len(currentTokens)-1]
		}

		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				keys := schema.GetRegistry().GetListKeys(currentPath, listName, itemMap)
				itemTokens := append([]string{}, currentTokens...)
				for _, lk := range keys {
					for ik, iv := range itemMap {
						if strings.EqualFold(normalizer.StripNamespace(ik), lk) {
							keyVal := strings.ToLower( strings.Trim(normalizer.FormatScalarCLI(iv), "\"") )
							if keyVal != "" {
								if (listName == "acl-filter" || listName == "acl-set") && strings.EqualFold(lk, "type") {
									itemTokens = append(itemTokens, "type", keyVal)
								} else if listName == "prefix" && strings.EqualFold(lk, "mask-length-range") {
									itemTokens = append(itemTokens, "mask-length-range", keyVal)
								} else {
									itemTokens = append(itemTokens, keyVal)
								}
							}
							break
						}
					}
				}

				if pathMatchesQuery(itemTokens, queryTokens, rawQuery) {
					filteredList = append(filteredList, item)
				} else {
					childRes := filterSubtreeHelper(item, queryTokens, itemTokens, rawQuery)
					if childRes != nil {
						if childMap, ok := childRes.(map[string]interface{}); ok {
							// Preserve only the actual list identifying key fields from itemMap
							for _, lk := range keys {
								for k, val := range itemMap {
									if strings.EqualFold(normalizer.StripNamespace(k), lk) {
										if _, exists := childMap[k]; !exists {
											childMap[k] = val
										}
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
				scalarToken := strings.ToLower(strings.Trim(normalizer.FormatScalarCLI(item), "\""))
				elemTokens := append(append([]string{}, currentTokens...), scalarToken)
				if pathMatchesQuery(elemTokens, queryTokens, rawQuery) {
					filteredList = append(filteredList, item)
				}
			}
		}
		if len(filteredList) > 0 {
			return filteredList
		}
		return nil

	default:
		scalarStr := strings.ToLower(strings.Trim(normalizer.FormatScalarCLI(node), "\""))
		leafTokens := append(append([]string{}, currentTokens...), scalarStr)
		if pathMatchesQuery(leafTokens, queryTokens, rawQuery) {
			return node
		}
		if !strings.HasPrefix(strings.TrimSpace(rawQuery), "/") && len(queryTokens) == 1 && tokenSegmentMatches(scalarStr, queryTokens[0]) {
			return node
		}
		return nil
	}
}

// FilterCLILines filters CLI statements by query (supporting both XPath and CLI queries).
func FilterCLILines(lines []string, filterQuery string) []string {
	queryTokens := TokenizePath(filterQuery)
	if len(queryTokens) == 0 {
		return lines
	}

	var matched []string
	for _, l := range lines {
		if IsPathMatching(l, filterQuery) {
			matched = append(matched, l)
		}
	}
	return matched
}

// FilterDiffResult filters a SemanticDiffResult to only contain changes matching the filter.
func FilterDiffResult(diffRes models.SemanticDiffResult, filterQuery string) models.SemanticDiffResult {
	queryTokens := TokenizePath(filterQuery)
	if len(queryTokens) == 0 {
		return diffRes
	}

	var filteredChanges []models.PathChange
	for _, c := range diffRes.Changes {
		if IsPathMatching(c.Path, filterQuery) || valueMatchesFilter(c.OldValue, c.NewValue, queryTokens) {
			filteredChanges = append(filteredChanges, c)
		}
	}

	var filteredCLI []string
	for _, l := range diffRes.CLIDiffLines {
		if IsPathMatching(l, filterQuery) {
			filteredCLI = append(filteredCLI, l)
		}
	}

	var filteredUnified []string
	for _, l := range diffRes.UnifiedDiffLines {
		if strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") {
			filteredUnified = append(filteredUnified, l)
		} else if IsPathMatching(l, filterQuery) {
			filteredUnified = append(filteredUnified, l)
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

func valueMatchesFilter(oldVal, newVal interface{}, queryTokens []string) bool {
	if len(queryTokens) != 1 {
		return false
	}
	q := queryTokens[0]
	if oldVal != nil {
		s := strings.ToLower(fmt.Sprintf("%v", oldVal))
		if tokenSegmentMatches(s, q) {
			return true
		}
	}
	if newVal != nil {
		s := strings.ToLower(fmt.Sprintf("%v", newVal))
		if tokenSegmentMatches(s, q) {
			return true
		}
	}
	return false
}
