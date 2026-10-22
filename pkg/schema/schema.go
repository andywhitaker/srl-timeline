package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	defaultRegistry *SchemaRegistry
	once            sync.Once

	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentRe  = regexp.MustCompile(`(?m)(?:^|\s)//[^\n]*`)
	listHeaderRe   = regexp.MustCompile(`(?s)(?:^|[\s;{])list\s+([a-zA-Z_][\w\-:]*)\s*\{`)
	keyStmtRe      = regexp.MustCompile(`(?s)(?:^|[\s;{])key\s+(?:["']([^"']+)["']|([a-zA-Z_][\w\-:]*))\s*;`)
)

// fallbackKeyOrder defines the deterministic priority order for list key heuristics.
var fallbackKeyOrder = []string{
	"name",
	"index",
	"sequence-id",
	"username",
	"prefix",
	"ip-prefix",
	"address",
	"ip-address",
	"peer-address",
	"neighbor",
	"id",
	"rule-id",
	"file-name",
	"buffer-name",
	"group-name",
	"community-entry",
	"peer-group",
	"type",
	"protocol",
	"slot",
	"vni",
	"evi",
	"value",
}

// SchemaRegistry stores YANG list keys and path-disambiguated key definitions for SR Linux.
type SchemaRegistry struct {
	mu                sync.RWMutex
	listKeysByPath    map[string][]string
	listKeysByName    map[string][]string
	listKeyCandidates map[string][][]string
}

// NewSchemaRegistry creates a new empty schema registry.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		listKeysByPath:    make(map[string][]string),
		listKeysByName:    make(map[string][]string),
		listKeyCandidates: make(map[string][][]string),
	}
}

// GetRegistry returns the global singleton schema registry.
func GetRegistry() *SchemaRegistry {
	once.Do(func() {
		defaultRegistry = NewSchemaRegistry()
		// 1. Load embedded standard SR Linux schema definitions (covers /opt/srlinux/models)
		defaultRegistry.loadEmbeddedSchema()

		// 2. Load any custom third-party NDK / user YANG models
		customModelPaths := []string{
			"/etc/opt/srlinux/models",
			"/etc/opt/srlinux/appmgr/yang",
			"/etc/opt/srlinux/appmgr/user_agents",
			"/etc/opt/srlinux/appmgr",
		}
		if envDir := os.Getenv("TIMELINE_YANG_DIR"); envDir != "" {
			customModelPaths = append(customModelPaths, envDir)
		}
		for _, p := range customModelPaths {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				_ = defaultRegistry.LoadFromDirectory(p)
			}
		}
	})
	return defaultRegistry
}

func stripYANGComments(raw string) string {
	noBlock := blockCommentRe.ReplaceAllString(raw, "")
	return lineCommentRe.ReplaceAllString(noBlock, "")
}

type yangListDef struct {
	name string
	keys []string
}

// extractYANGLists scans YANG content for `list <name> { ... }` blocks and extracts the direct
// child `key` statement at brace depth 1 (handling nested `must { ... }` blocks and both quoted and unquoted keys).
func extractYANGLists(content string) []yangListDef {
	var results []yangListDef
	indices := listHeaderRe.FindAllStringSubmatchIndex(content, -1)
	for _, idx := range indices {
		if len(idx) < 4 {
			continue
		}
		rawListName := content[idx[2]:idx[3]]
		listName := strings.ToLower(stripPrefix(rawListName))
		pos := idx[1]
		depth := 1
		inQuote := byte(0)
		var topLevel strings.Builder

		for pos < len(content) && depth > 0 {
			ch := content[pos]
			if inQuote != 0 {
				if ch == '\\' && pos+1 < len(content) {
					if depth == 1 {
						topLevel.WriteByte(ch)
						topLevel.WriteByte(content[pos+1])
					}
					pos += 2
					continue
				}
				if ch == inQuote {
					inQuote = 0
				}
				if depth == 1 {
					topLevel.WriteByte(ch)
				}
			} else {
				switch ch {
				case '"', '\'':
					inQuote = ch
					if depth == 1 {
						topLevel.WriteByte(ch)
					}
				case '{':
					if depth == 1 {
						topLevel.WriteByte(';')
					}
					depth++
				case '}':
					depth--
					if depth == 1 {
						topLevel.WriteByte(';')
					}
				default:
					if depth == 1 {
						topLevel.WriteByte(ch)
					}
				}
			}
			pos++
		}

		if km := keyStmtRe.FindStringSubmatch(topLevel.String()); len(km) >= 3 {
			keySpec := km[1]
			if keySpec == "" {
				keySpec = km[2]
			}
			rawKeys := strings.Fields(keySpec)
			var cleanKeys []string
			for _, k := range rawKeys {
				cleanKeys = append(cleanKeys, stripPrefix(k))
			}
			if len(cleanKeys) > 0 {
				results = append(results, yangListDef{
					name: listName,
					keys: cleanKeys,
				})
			}
		}
	}
	return results
}

// LoadFromDirectory recursively parses YANG files in a directory and registers list keys.
func (s *SchemaRegistry) LoadFromDirectory(dirPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".yang") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := stripYANGComments(string(data))
		for _, def := range extractYANGLists(content) {
			if _, exists := s.listKeysByName[def.name]; !exists {
				s.listKeysByName[def.name] = def.keys
			}
			s.addCandidateKeysLocked(def.name, def.keys)
		}

		return nil
	})
}

func (s *SchemaRegistry) addCandidateKeysLocked(listName string, keys []string) {
	existing := s.listKeyCandidates[listName]
	for _, cand := range existing {
		if len(cand) == len(keys) {
			same := true
			for i := range cand {
				if !strings.EqualFold(cand[i], keys[i]) {
					same = false
					break
				}
			}
			if same {
				return
			}
		}
	}
	s.listKeyCandidates[listName] = append(existing, keys)
}

// RegisterListKey explicitly registers keys for a list name or path.
func (s *SchemaRegistry) RegisterListKey(listName string, keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clean := strings.ToLower(stripPrefix(listName))
	if strings.HasPrefix(clean, "/") {
		s.listKeysByPath[clean] = keys
		parts := strings.Split(strings.Trim(clean, "/"), "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			s.addCandidateKeysLocked(last, keys)
		}
		return
	}
	s.listKeysByName[clean] = keys
	s.addCandidateKeysLocked(clean, keys)
}

// canonicalPathTokens converts an XPath, slash path, or CLI path into clean lower-case tokens without predicates.
func canonicalPathTokens(path string) []string {
	var tokens []string
	var cur strings.Builder
	inBracket := false
	for i := 0; i < len(path); i++ {
		ch := path[i]
		switch ch {
		case '[':
			inBracket = true
			if cur.Len() > 0 {
				tok := strings.ToLower(stripPrefix(strings.TrimSpace(cur.String())))
				if tok != "" {
					tokens = append(tokens, tok)
				}
				cur.Reset()
			}
		case ']':
			inBracket = false
			cur.Reset()
		case '/', ' ', '\t':
			if !inBracket {
				if cur.Len() > 0 {
					tok := strings.ToLower(stripPrefix(strings.TrimSpace(cur.String())))
					if tok != "" {
						tokens = append(tokens, tok)
					}
					cur.Reset()
				}
			}
		default:
			if !inBracket {
				cur.WriteByte(ch)
			}
		}
	}
	if cur.Len() > 0 {
		tok := strings.ToLower(stripPrefix(strings.TrimSpace(cur.String())))
		if tok != "" {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}

func (s *SchemaRegistry) isKnownListLocked(name string) bool {
	if _, ok := s.listKeysByName[name]; ok {
		return true
	}
	if _, ok := s.listKeyCandidates[name]; ok {
		return true
	}
	return false
}

// cliPathTokensMatchSchema checks if pathTokens (from a flat CLI path with interleaved list instance values)
// matches schemaTokens from root, allowing only list instance key values between consecutive schema nodes.
func (s *SchemaRegistry) cliPathTokensMatchSchema(pathTokens, schemaTokens []string) bool {
	if len(schemaTokens) == 0 || len(pathTokens) < len(schemaTokens) {
		return false
	}
	if pathTokens[0] != schemaTokens[0] || pathTokens[len(pathTokens)-1] != schemaTokens[len(schemaTokens)-1] {
		return false
	}

	var matchFrom func(sIdx, pIdx int) bool
	matchFrom = func(sIdx, pIdx int) bool {
		if sIdx == len(schemaTokens)-1 {
			return pIdx == len(pathTokens)-1
		}
		tok := schemaTokens[sIdx]
		maxSkip := 0
		if tok == "acl-filter" || tok == "acl-set" || tok == "prefix" {
			maxSkip = 3
		} else if s.isKnownListLocked(tok) {
			maxSkip = 1
			if keys, ok := s.listKeysByName[tok]; ok && len(keys) > maxSkip {
				maxSkip = len(keys)
			}
		}

		nextSchemaTok := schemaTokens[sIdx+1]
		for skip := 0; skip <= maxSkip; skip++ {
			nextPIdx := pIdx + 1 + skip
			if nextPIdx >= len(pathTokens) {
				break
			}
			if pathTokens[nextPIdx] == nextSchemaTok {
				if matchFrom(sIdx+1, nextPIdx) {
					return true
				}
			}
		}
		return false
	}

	return matchFrom(0, 0)
}

func findItemKey(item map[string]interface{}, keyName string) (string, interface{}, bool) {
	if item == nil {
		return "", nil, false
	}
	if v, ok := item[keyName]; ok && isScalarValue(v) {
		return keyName, v, true
	}
	target := strings.ToLower(keyName)
	var itemKeys []string
	for k := range item {
		itemKeys = append(itemKeys, k)
	}
	sort.Strings(itemKeys)
	for _, k := range itemKeys {
		if strings.ToLower(stripPrefix(k)) == target && isScalarValue(item[k]) {
			return k, item[k], true
		}
	}
	return "", nil, false
}

func isScalarValue(v interface{}) bool {
	if v == nil {
		return false
	}
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return false
	default:
		return true
	}
}

func itemHasAllKeys(item map[string]interface{}, keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	if item == nil {
		return true
	}
	for _, k := range keys {
		if _, _, ok := findItemKey(item, k); !ok {
			return false
		}
	}
	return true
}

// GetListKeys returns the schema key names for a given path and list name.
func (s *SchemaRegistry) GetListKeys(currentPath, listName string, item map[string]interface{}) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pathTokens := canonicalPathTokens(currentPath)
	cleanName := strings.ToLower(stripPrefix(listName))
	if cleanName == "" && len(pathTokens) > 0 {
		cleanName = pathTokens[len(pathTokens)-1]
	}
	if cleanName != "" && (len(pathTokens) == 0 || pathTokens[len(pathTokens)-1] != cleanName) {
		pathTokens = append(pathTokens, cleanName)
	}

	// 1. Special compound keys
	if cleanName == "acl-filter" || cleanName == "acl-set" {
		keys := []string{"name", "type"}
		if item == nil || itemHasAllKeys(item, keys) {
			return keys
		}
	}
	if cleanName == "prefix" && item != nil && itemHasAllKeys(item, []string{"ip-prefix", "mask-length-range"}) {
		return []string{"ip-prefix", "mask-length-range"}
	}

	// 2. Direct or schema-aligned CLI path match in s.listKeysByPath
	if len(pathTokens) > 0 {
		slashPath := "/" + strings.Join(pathTokens, "/")
		if keys, ok := s.listKeysByPath[slashPath]; ok && len(keys) > 0 {
			if item == nil || itemHasAllKeys(item, keys) {
				return keys
			}
		}

		// When currentPath is a flat CLI path (no "/" separator), pathTokens interleaves
		// schema node names with list instance key values (e.g. "network-instance default static-routes route").
		if !strings.Contains(currentPath, "/") {
			var bestKeys []string
			bestDepth := 0
			bestPath := ""
			for schemaPath, keys := range s.listKeysByPath {
				sTokens := strings.Split(strings.Trim(schemaPath, "/"), "/")
				if (len(sTokens) > bestDepth || (len(sTokens) == bestDepth && (bestPath == "" || schemaPath < bestPath))) &&
					s.cliPathTokensMatchSchema(pathTokens, sTokens) {
					if item == nil || itemHasAllKeys(item, keys) {
						bestKeys = keys
						bestDepth = len(sTokens)
						bestPath = schemaPath
					}
				}
			}
			if len(bestKeys) > 0 {
				return bestKeys
			}
		}
	}

	// 3. Candidate key sets for listName validated against actual item fields
	if item != nil {
		if candidates, ok := s.listKeyCandidates[cleanName]; ok {
			for _, cand := range candidates {
				if itemHasAllKeys(item, cand) {
					return cand
				}
			}
		}
	}

	// 4. Direct list name match
	if keys, ok := s.listKeysByName[cleanName]; ok && len(keys) > 0 {
		if item == nil || itemHasAllKeys(item, keys) {
			return keys
		}
	}

	// 5. Deterministic fallback heuristic for item map
	if item != nil {
		for _, candidate := range fallbackKeyOrder {
			if _, _, ok := findItemKey(item, candidate); ok {
				return []string{candidate}
			}
		}

		var sortedKeys []string
		for k, v := range item {
			if isScalarValue(v) {
				sortedKeys = append(sortedKeys, strings.ToLower(stripPrefix(k)))
			}
		}
		sort.Strings(sortedKeys)
		for _, ck := range sortedKeys {
			if strings.HasSuffix(ck, "-name") || strings.HasSuffix(ck, "-id") || strings.HasSuffix(ck, "-address") || strings.HasSuffix(ck, "-index") || strings.HasSuffix(ck, "-prefix") {
				return []string{ck}
			}
		}
	}

	return nil
}

// IsListKey returns whether fieldName is a list key for the given path/listName/item.
func (s *SchemaRegistry) IsListKey(currentPath, listName, fieldName string, item map[string]interface{}) bool {
	if item != nil {
		if _, v, ok := findItemKey(item, fieldName); !ok || !isScalarValue(v) {
			return false
		}
	}
	cleanField := strings.ToLower(stripPrefix(fieldName))
	keys := s.GetListKeys(currentPath, listName, item)
	if len(keys) > 0 {
		for _, k := range keys {
			if strings.ToLower(k) == cleanField {
				return true
			}
		}
		return false
	}

	for _, wk := range fallbackKeyOrder {
		if cleanField == wk {
			return true
		}
	}
	return strings.HasSuffix(cleanField, "-name") ||
		strings.HasSuffix(cleanField, "-id") ||
		strings.HasSuffix(cleanField, "-address") ||
		strings.HasSuffix(cleanField, "-index") ||
		strings.HasSuffix(cleanField, "-prefix")
}

// FormatListKeyCLI formats the list key into CLI tokens and returns the consumed map keys.
func (s *SchemaRegistry) FormatListKeyCLI(currentPath, listName string, item map[string]interface{}, formatScalar func(interface{}) string) (string, []string) {
	if item == nil {
		return "", nil
	}

	cleanName := strings.ToLower(stripPrefix(listName))
	cleanPath := strings.ToLower(stripPrefix(currentPath))

	// Special compound key: acl-filter / acl-set requires "<name> type <type>"
	if cleanName == "acl-filter" || strings.HasSuffix(cleanPath, "acl-filter") ||
		cleanName == "acl-set" || strings.HasSuffix(cleanPath, "acl-set") {
		if nameKey, nameVal, hasName := findItemKey(item, "name"); hasName {
			if typeKey, typeVal, hasType := findItemKey(item, "type"); hasType {
				return fmt.Sprintf("%s type %s", formatScalar(nameVal), formatScalar(typeVal)), []string{nameKey, typeKey}
			}
			return formatScalar(nameVal), []string{nameKey}
		}
	}

	// Special compound key: routing-policy prefix-set prefix requires "<ip-prefix> mask-length-range <mask-length-range>"
	if cleanName == "prefix" || strings.HasSuffix(cleanPath, "prefix") {
		if pfxKey, pfxVal, hasPfx := findItemKey(item, "ip-prefix"); hasPfx {
			if mlrKey, mlrVal, hasMLR := findItemKey(item, "mask-length-range"); hasMLR {
				return fmt.Sprintf("%s mask-length-range %s", formatScalar(pfxVal), formatScalar(mlrVal)), []string{pfxKey, mlrKey}
			}
		}
	}

	keys := s.GetListKeys(currentPath, listName, item)
	if len(keys) > 0 {
		var keyVals []string
		var matchedConsumed []string
		for _, k := range keys {
			if itemK, itemV, ok := findItemKey(item, k); ok {
				matchedConsumed = append(matchedConsumed, itemK)
				keyVals = append(keyVals, formatScalar(itemV))
			}
		}
		if len(keyVals) == len(keys) {
			return strings.Join(keyVals, " "), matchedConsumed
		}
	}

	// Deterministic fallback search in item map
	for _, candidate := range fallbackKeyOrder {
		if itemK, itemV, ok := findItemKey(item, candidate); ok {
			return formatScalar(itemV), []string{itemK}
		}
	}

	var sortedItemKeys []string
	for itemK, itemV := range item {
		if isScalarValue(itemV) {
			sortedItemKeys = append(sortedItemKeys, itemK)
		}
	}
	sort.Strings(sortedItemKeys)
	for _, itemK := range sortedItemKeys {
		ck := strings.ToLower(stripPrefix(itemK))
		if strings.HasSuffix(ck, "-name") || strings.HasSuffix(ck, "-id") || strings.HasSuffix(ck, "-address") || strings.HasSuffix(ck, "-index") || strings.HasSuffix(ck, "-prefix") {
			return formatScalar(item[itemK]), []string{itemK}
		}
	}

	return "", nil
}

func stripPrefix(s string) string {
	if idx := strings.Index(s, ":"); idx != -1 {
		return s[idx+1:]
	}
	return s
}
