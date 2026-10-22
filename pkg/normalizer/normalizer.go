package normalizer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"timeline/pkg/schema"
)

// WellKnownListKeys contains common YANG list key names for deterministic sorting.
var WellKnownListKeys = []string{
	"name",
	"username",
	"file-name",
	"buffer-name",
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
	"ip-address",
	"type",
	"protocol",
	"slot",
	"vni",
	"evi",
	"value",
}

var yangModulePrefixRe = regexp.MustCompile(`^(?:srl_nokia|openconfig|ietf|iana)-[a-zA-Z0-9_-]+:([a-zA-Z_][a-zA-Z0-9._-]*)$`)

// StripNamespace removes YANG module prefixes on JSON object keys like "srl_nokia-interfaces:interface" -> "interface".
func StripNamespace(key string) string {
	if idx := strings.Index(key, ":"); idx != -1 {
		return key[idx+1:]
	}
	return key
}

// StripModulePrefixValue removes YANG module prefixes only when a string scalar matches a YANG identityref
// (e.g. "srl_nokia-network-instance:ip-vrf" -> "ip-vrf"), preserving IPv6 addresses, MACs, communities, and URLs.
func StripModulePrefixValue(val string) string {
	if m := yangModulePrefixRe.FindStringSubmatch(val); len(m) == 2 {
		return m[1]
	}
	return val
}

// NormalizeStructure sorts map keys and YANG list elements deterministically, stripping internal "_" metadata keys.
func NormalizeStructure(data interface{}, sortLists bool) interface{} {
	return normalizeStructureAtPath(data, "", "", sortLists)
}

func normalizeStructureAtPath(data interface{}, currentPath, listName string, sortLists bool) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		sortedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			cleanKey := StripNamespace(k)
			if strings.HasPrefix(cleanKey, "_") {
				continue
			}
			subPath := cleanKey
			if currentPath != "" {
				subPath = currentPath + "/" + cleanKey
			} else {
				subPath = "/" + cleanKey
			}
			sortedMap[cleanKey] = normalizeStructureAtPath(val, subPath, cleanKey, sortLists)
		}
		return sortedMap

	case []interface{}:
		normalizedList := make([]interface{}, len(v))
		for i, item := range v {
			normalizedList[i] = normalizeStructureAtPath(item, currentPath, listName, sortLists)
		}

		if sortLists && len(normalizedList) > 1 {
			sort.SliceStable(normalizedList, func(i, j int) bool {
				return listElementKeyAtPath(currentPath, listName, normalizedList[i]) < listElementKeyAtPath(currentPath, listName, normalizedList[j])
			})
		}
		return normalizedList

	case string:
		return StripModulePrefixValue(v)

	default:
		return v
	}
}

func listElementKey(item interface{}) string {
	return listElementKeyAtPath("", "", item)
}

func listElementKeyAtPath(currentPath, listName string, item interface{}) string {
	m, ok := item.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", item)
	}

	keys := schema.GetRegistry().GetListKeys(currentPath, listName, m)
	if len(keys) > 0 {
		var parts []string
		for _, k := range keys {
			if val, found := m[k]; found {
				parts = append(parts, fmt.Sprintf("%s=%v", k, val))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ",")
		}
	}

	var wkParts []string
	for _, k := range WellKnownListKeys {
		if val, found := m[k]; found {
			switch val.(type) {
			case map[string]interface{}, []interface{}:
				continue
			default:
				wkParts = append(wkParts, fmt.Sprintf("%s=%v", k, val))
			}
		}
	}
	if len(wkParts) > 0 {
		return strings.Join(wkParts, ",")
	}

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

func joinPathTokens(base, next string) string {
	base = strings.TrimSpace(base)
	next = strings.TrimSpace(next)
	if base == "" {
		return next
	}
	if next == "" {
		return base
	}
	return base + " " + next
}

// JSONToFlatCLI converts a configuration JSON map into sorted flat CLI set statements (one scalar leaf per line).
func JSONToFlatCLI(data interface{}, currentPath string) []string {
	var lines []string
	cleanCurrentPath := strings.TrimSpace(currentPath)

	switch v := data.(type) {
	case map[string]interface{}:
		var keys []string
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		hasChildren := false
		for _, k := range keys {
			cleanKey := StripNamespace(k)
			if strings.HasPrefix(cleanKey, "_") {
				continue // Skip internal annotations (_preamble, _annotate)
			}
			val := v[k]
			switch child := val.(type) {
			case map[string]interface{}:
				hasChildren = true
				subPath := joinPathTokens(cleanCurrentPath, cleanKey)
				lines = append(lines, JSONToFlatCLI(child, subPath)...)
			case []interface{}:
				hasChildren = true
				subPath := joinPathTokens(cleanCurrentPath, cleanKey)
				lines = append(lines, JSONToFlatCLI(child, subPath)...)
			default:
				valStr := FormatScalarCLI(child)
				if valStr != "" {
					hasChildren = true
					leafPath := joinPathTokens(cleanCurrentPath, cleanKey)
					lines = append(lines, fmt.Sprintf("set / %s %s", leafPath, valStr))
				}
			}
		}

		if !hasChildren && cleanCurrentPath != "" && len(v) == 0 {
			lines = append(lines, fmt.Sprintf("set / %s", cleanCurrentPath))
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
			stmt := fmt.Sprintf("set / %s [ %s ]", cleanCurrentPath, strings.Join(formattedElems, " "))
			lines = append(lines, stmt)
			return lines
		}

		for _, item := range v {
			switch child := item.(type) {
			case map[string]interface{}:
				keyPart, consumedKeys := extractListKeyCLI(cleanCurrentPath, child)
				subPath := joinPathTokens(cleanCurrentPath, keyPart)

				// Create the remaining leaves without re-emitting the consumed keys
				remainingMap := make(map[string]interface{}, len(child))
				for ck, cv := range child {
					cleanKey := StripNamespace(ck)
					if strings.HasPrefix(cleanKey, "_") {
						continue
					}
					consumed := false
					for _, k := range consumedKeys {
						if strings.EqualFold(cleanKey, StripNamespace(k)) {
							consumed = true
							break
						}
					}
					if !consumed {
						remainingMap[ck] = cv
					}
				}

				if len(remainingMap) == 0 {
					if subPath != "" {
						lines = append(lines, fmt.Sprintf("set / %s", subPath))
					}
				} else {
					lines = append(lines, JSONToFlatCLI(remainingMap, subPath)...)
				}

			default:
				valStr := FormatScalarCLI(child)
				lines = append(lines, fmt.Sprintf("set / %s %s", cleanCurrentPath, valStr))
			}
		}
	}

	sort.Strings(lines)
	return lines
}

func extractListKeyCLI(currentPath string, m map[string]interface{}) (string, []string) {
	fields := strings.Fields(currentPath)
	listName := ""
	if len(fields) > 0 {
		listName = fields[len(fields)-1]
	}
	return schema.GetRegistry().FormatListKeyCLI(currentPath, listName, m, FormatScalarCLI)
}

// FormatScalarCLI formats scalar values for flat CLI syntax without corrupting colon-containing strings.
func FormatScalarCLI(v interface{}) string {
	switch val := v.(type) {
	case string:
		cleanVal := StripModulePrefixValue(val)
		if cleanVal == "" || strings.ContainsAny(cleanVal, " \t\n\r\"") {
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
