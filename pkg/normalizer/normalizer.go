package normalizer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"timeline/pkg/schema"
)

// WellKnownListKeys contains common YANG list key names for deterministic sorting.
var WellKnownListKeys = []string{
	"name",
	"username",
	"file-name",
	"group-name",
	"community-entry",
	"peer-address",
	"index",
	"sequence-id",
	"entry",
	"neighbor",
	"peer-group",
	"prefix",
	"id",
	"rule-id",
	"ip-prefix",
	"address",
	"type",
	"protocol",
}

// StripNamespace removes YANG module prefixes like "srl_nokia-interfaces:interface" -> "interface".
func StripNamespace(key string) string {
	if idx := strings.Index(key, ":"); idx != -1 {
		return key[idx+1:]
	}
	return key
}

// NormalizeStructure sorts map keys and YANG list elements deterministically.
func NormalizeStructure(data interface{}, sortLists bool) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		sortedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			cleanKey := StripNamespace(k)
			sortedMap[cleanKey] = NormalizeStructure(val, sortLists)
		}
		return sortedMap

	case []interface{}:
		normalizedList := make([]interface{}, len(v))
		for i, item := range v {
			normalizedList[i] = NormalizeStructure(item, sortLists)
		}

		if sortLists && len(normalizedList) > 1 {
			sort.SliceStable(normalizedList, func(i, j int) bool {
				return listElementKey(normalizedList[i]) < listElementKey(normalizedList[j])
			})
		}
		return normalizedList

	default:
		return v
	}
}

func listElementKey(item interface{}) string {
	m, ok := item.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", item)
	}

	for _, k := range WellKnownListKeys {
		if val, found := m[k]; found {
			return fmt.Sprintf("%s=%v", k, val)
		}
	}

	// Fallback to sorted json of the object
	b, _ := json.Marshal(m)
	return string(b)
}

// CanonicalJSONString produces formatted JSON with deterministic key ordering.
func CanonicalJSONString(data interface{}, indent int) (string, error) {
	normalized := NormalizeStructure(data, true)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent > 0 {
		enc.SetIndent("", strings.Repeat(" ", indent))
	}
	if err := enc.Encode(normalized); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// JSONToFlatCLI converts a configuration JSON map into sorted flat CLI set statements.
func JSONToFlatCLI(data interface{}, currentPath string) []string {
	var lines []string

	switch v := data.(type) {
	case map[string]interface{}:
		var keys []string
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var scalarLeaves []string
		for _, k := range keys {
			cleanKey := StripNamespace(k)
			if strings.HasPrefix(cleanKey, "_") {
				continue // Skip internal annotations
			}
			val := v[k]
			switch child := val.(type) {
			case map[string]interface{}:
				subPath := strings.TrimSpace(fmt.Sprintf("%s %s", currentPath, cleanKey))
				lines = append(lines, JSONToFlatCLI(child, subPath)...)
			case []interface{}:
				subPath := strings.TrimSpace(fmt.Sprintf("%s %s", currentPath, cleanKey))
				lines = append(lines, JSONToFlatCLI(child, subPath)...)
			default:
				valStr := FormatScalarCLI(child)
				if valStr != "" {
					scalarLeaves = append(scalarLeaves, fmt.Sprintf("%s %s", cleanKey, valStr))
				}
			}
		}

		if len(scalarLeaves) > 0 {
			stmt := strings.TrimSpace(fmt.Sprintf("set / %s %s", currentPath, strings.Join(scalarLeaves, " ")))
			// Clean extra spaces
			stmt = strings.Join(strings.Fields(stmt), " ")
			lines = append(lines, stmt)
		} else if currentPath != "" && len(v) == 0 {
			stmt := strings.TrimSpace(fmt.Sprintf("set / %s", currentPath))
			stmt = strings.Join(strings.Fields(stmt), " ")
			lines = append(lines, stmt)
		}

	case []interface{}:
		if len(v) == 0 {
			return lines
		}
		// Check if it is a list of objects or a leaf-list of scalars
		if _, isMap := v[0].(map[string]interface{}); !isMap {
			var formattedElems []string
			for _, item := range v {
				formattedElems = append(formattedElems, FormatScalarCLI(item))
			}
			stmt := strings.TrimSpace(fmt.Sprintf("set / %s [ %s ]", currentPath, strings.Join(formattedElems, " ")))
			lines = append(lines, stmt)
			return lines
		}

		for _, item := range v {
			switch child := item.(type) {
			case map[string]interface{}:
				keyPart, consumedKeys := extractListKeyCLI(currentPath, child)
				var subPath string
				if keyPart != "" {
					subPath = strings.TrimSpace(fmt.Sprintf("%s %s", currentPath, keyPart))
				} else {
					subPath = currentPath
				}

				// Create the remaining leaves without re-emitting the consumed keys
				remainingMap := make(map[string]interface{}, len(child))
				for ck, cv := range child {
					cleanKey := StripNamespace(ck)
					consumed := false
					for _, k := range consumedKeys {
						if cleanKey == k {
							consumed = true
							break
						}
					}
					if !consumed {
						remainingMap[ck] = cv
					}
				}

				if len(remainingMap) == 0 {
					stmt := strings.TrimSpace(fmt.Sprintf("set / %s", subPath))
					stmt = strings.Join(strings.Fields(stmt), " ")
					lines = append(lines, stmt)
				} else {
					lines = append(lines, JSONToFlatCLI(remainingMap, subPath)...)
				}

			default:
				valStr := FormatScalarCLI(child)
				stmt := strings.TrimSpace(fmt.Sprintf("set / %s %s", currentPath, valStr))
				stmt = strings.Join(strings.Fields(stmt), " ")
				lines = append(lines, stmt)
			}
		}
	}

	sort.Strings(lines)
	return lines
}

func isListKey(k string) bool {
	return schema.GetRegistry().IsListKey("", "", k, nil)
}

func extractListKeyCLI(currentPath string, m map[string]interface{}) (string, []string) {
	fields := strings.Fields(currentPath)
	listName := ""
	if len(fields) > 0 {
		listName = fields[len(fields)-1]
	}
	return schema.GetRegistry().FormatListKeyCLI(currentPath, listName, m, FormatScalarCLI)
}

// FormatScalarCLI formats scalar values for flat CLI syntax.
func FormatScalarCLI(v interface{}) string {
	switch val := v.(type) {
	case string:
		cleanVal := StripNamespace(val)
		if strings.Contains(cleanVal, " ") || strings.Contains(cleanVal, "\t") {
			return fmt.Sprintf("%q", cleanVal)
		}
		return cleanVal
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// FlatCLIString returns the flat CLI set statements as a newline-separated string.
func FlatCLIString(data interface{}) string {
	lines := JSONToFlatCLI(data, "")
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
