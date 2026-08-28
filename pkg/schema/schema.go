package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	defaultRegistry *SchemaRegistry
	once            sync.Once

	commentRe  = regexp.MustCompile(`(?s)/\*.*?\*/|//.*`)
	listRe     = regexp.MustCompile(`(?s)list\s+([\w\-:]+)\s*\{[^}]*?key\s+["']([^"']+)["']\s*;`)
	leafListRe = regexp.MustCompile(`leaf-list\s+([\w\-:]+)`)
)

// SchemaRegistry stores YANG list keys and leaf-list definitions for SR Linux.
type SchemaRegistry struct {
	mu             sync.RWMutex
	listKeysByPath map[string][]string
	listKeysByName map[string][]string
	leafLists      map[string]bool
}

// NewSchemaRegistry creates a new empty schema registry.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		listKeysByPath: make(map[string][]string),
		listKeysByName: make(map[string][]string),
		leafLists:      make(map[string]bool),
	}
}

// GetRegistry returns the global singleton schema registry.
func GetRegistry() *SchemaRegistry {
	once.Do(func() {
		defaultRegistry = NewSchemaRegistry()
		// 1. Load embedded standard SR Linux schema definitions
		defaultRegistry.loadEmbeddedSchema()

		// 2. If running on SR Linux or models directory exists, load dynamic switch models
		onBoxModelPaths := []string{
			"/opt/srlinux/models/srl_nokia/models",
			"/opt/srlinux/models/openconfig",
			"/opt/srlinux/models/ietf",
			"/etc/opt/srlinux/models",
		}
		for _, p := range onBoxModelPaths {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				_ = defaultRegistry.LoadFromDirectory(p)
			}
		}
	})
	return defaultRegistry
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

		content := commentRe.ReplaceAllString(string(data), "")

		// Extract leaf-lists
		llMatches := leafListRe.FindAllStringSubmatch(content, -1)
		for _, m := range llMatches {
			if len(m) >= 2 {
				name := stripPrefix(m[1])
				s.leafLists[name] = true
			}
		}

		// Extract lists and keys
		matches := listRe.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) >= 3 {
				listName := stripPrefix(m[1])
				rawKeys := strings.Fields(m[2])
				var cleanKeys []string
				for _, k := range rawKeys {
					cleanKeys = append(cleanKeys, stripPrefix(k))
				}

				if len(cleanKeys) > 0 {
					if _, exists := s.listKeysByName[listName]; !exists {
						s.listKeysByName[listName] = cleanKeys
					}
				}
			}
		}

		return nil
	})
}

// RegisterListKey explicitly registers keys for a list name or path.
func (s *SchemaRegistry) RegisterListKey(listName string, keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listKeysByName[stripPrefix(listName)] = keys
}

// GetListKeys returns the schema key names for a given path and list name.
func (s *SchemaRegistry) GetListKeys(currentPath, listName string, item map[string]interface{}) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanPath := strings.ToLower(stripPrefix(currentPath))
	cleanName := strings.ToLower(stripPrefix(listName))

	// Special compound keys
	if cleanName == "acl-filter" || strings.HasSuffix(cleanPath, "acl-filter") {
		return []string{"name", "type"}
	}
	if cleanName == "acl-set" || strings.HasSuffix(cleanPath, "acl-set") {
		return []string{"name", "type"}
	}

	// 1. Direct path match
	if keys, ok := s.listKeysByPath[cleanPath]; ok && len(keys) > 0 {
		return keys
	}

	// 2. Direct list name match
	if keys, ok := s.listKeysByName[cleanName]; ok && len(keys) > 0 {
		return keys
	}

	// 3. Fallback heuristic for item map
	if item != nil {
		for k := range item {
			ck := strings.ToLower(stripPrefix(k))
			if strings.HasSuffix(ck, "-name") || strings.HasSuffix(ck, "-id") || strings.HasSuffix(ck, "-address") || strings.HasSuffix(ck, "-index") || strings.HasSuffix(ck, "-prefix") {
				return []string{ck}
			}
		}
		if _, ok := item["name"]; ok {
			return []string{"name"}
		}
		if _, ok := item["index"]; ok {
			return []string{"index"}
		}
	}

	return nil
}

// IsListKey returns whether fieldName is a list key.
func (s *SchemaRegistry) IsListKey(currentPath, listName, fieldName string, item map[string]interface{}) bool {
	keys := s.GetListKeys(currentPath, listName, item)
	cleanField := strings.ToLower(stripPrefix(fieldName))
	for _, k := range keys {
		if strings.ToLower(k) == cleanField {
			return true
		}
	}
	// Also check general key naming patterns
	if strings.HasSuffix(cleanField, "-name") || strings.HasSuffix(cleanField, "-id") || strings.HasSuffix(cleanField, "-address") || strings.HasSuffix(cleanField, "-index") || strings.HasSuffix(cleanField, "-prefix") {
		return true
	}
	return cleanField == "name" || cleanField == "index" || cleanField == "username" || cleanField == "entry"
}

// IsLeafList returns whether a property is a leaf-list.
func (s *SchemaRegistry) IsLeafList(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.leafLists[stripPrefix(name)]
}

// FormatListKeyCLI formats the list key into CLI tokens and returns the consumed map keys.
func (s *SchemaRegistry) FormatListKeyCLI(currentPath, listName string, item map[string]interface{}, formatScalar func(interface{}) string) (string, []string) {
	keys := s.GetListKeys(currentPath, listName, item)
	var consumed []string

	cleanName := strings.ToLower(stripPrefix(listName))
	cleanPath := strings.ToLower(stripPrefix(currentPath))

	// Compound key: acl-filter requires "name <name> type <type>"
	if cleanName == "acl-filter" || strings.HasSuffix(cleanPath, "acl-filter") {
		if nameVal, hasName := item["name"]; hasName {
			consumed = append(consumed, "name")
			if typeVal, hasType := item["type"]; hasType {
				consumed = append(consumed, "type")
				return fmt.Sprintf("%s type %s", formatScalar(nameVal), formatScalar(typeVal)), consumed
			}
			return formatScalar(nameVal), consumed
		}
	}

	// Format keys in schema order
	if len(keys) > 0 {
		var keyVals []string
		for _, k := range keys {
			for itemK, itemV := range item {
				if strings.ToLower(stripPrefix(itemK)) == strings.ToLower(k) {
					consumed = append(consumed, itemK)
					keyVals = append(keyVals, formatScalar(itemV))
					break
				}
			}
		}
		if len(keyVals) == len(keys) {
			return strings.Join(keyVals, " "), consumed
		}
	}

	// Fallback to searching item map
	if item != nil {
		for itemK, itemV := range item {
			ck := strings.ToLower(stripPrefix(itemK))
			if strings.HasSuffix(ck, "-name") || strings.HasSuffix(ck, "-id") || strings.HasSuffix(ck, "-address") || strings.HasSuffix(ck, "-index") || strings.HasSuffix(ck, "-prefix") || ck == "name" || ck == "index" || ck == "username" {
				consumed = append(consumed, itemK)
				return formatScalar(itemV), consumed
			}
		}
	}

	return "", consumed
}

func stripPrefix(s string) string {
	if idx := strings.Index(s, ":"); idx != -1 {
		return s[idx+1:]
	}
	return s
}
