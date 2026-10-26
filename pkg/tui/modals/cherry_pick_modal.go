package modals

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"timeline/pkg/models"
	"timeline/pkg/normalizer"
	"timeline/pkg/schema"
)

// CommonSubtrees provides quick selectable items for SR Linux configuration.
var CommonSubtrees = []string{
	"/interface",
	"/network-instance[name=default]",
	"/acl",
	"/system/name",
	"/system/banner",
	"/system/information",
	"/system/ntp",
	"/system/snmp",
	"/system/aaa",
	"/system/logging",
	"/system/tls",
}

// TreeNode represents a hierarchical node in the interactive cherry-pick tree.
type TreeNode struct {
	Name        string          // Display title: "/interface", "[name=ethernet-1/1]", "description"
	FullPath    string          // Full XPath: "/interface[name=ethernet-1/1]/description"
	IsLeaf      bool            // True if single property/scalar
	DiffType    models.DiffType // ADDED, MODIFIED, DELETED
	OldValue    interface{}
	NewValue    interface{}
	ChangeCount int
	Expanded    bool
	Children    []*TreeNode
	Parent      *TreeNode
	Depth       int
}

// CherryPickModalModel handles interactive cherry-pick subtree restoration with expandable tree, multi-select, and scrolling.
type CherryPickModalModel struct {
	Commit        models.TimelineCommit
	RootNodes     []*TreeNode
	VisibleNodes  []*TreeNode
	SelectedIndex int
	ScrollOffset  int
	SelectedPaths map[string]bool
	CustomInput   textinput.Model
	UseCustom     bool
	Width         int
	Height        int
}

type stepInfo struct {
	displayName string
	fullPath    string
}

func parsePathSteps(path string) []stepInfo {
	var steps []stepInfo
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return steps
	}

	var rawTokens []string
	var cur strings.Builder
	inBracket := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if ch == '[' {
			inBracket = true
			cur.WriteByte(ch)
		} else if ch == ']' {
			inBracket = false
			cur.WriteByte(ch)
		} else if ch == '/' && !inBracket {
			if cur.Len() > 0 {
				rawTokens = append(rawTokens, cur.String())
				cur.Reset()
			}
		} else {
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 {
		rawTokens = append(rawTokens, cur.String())
	}

	var cumulativePath strings.Builder
	for i, tok := range rawTokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}

		bracketIdx := strings.Index(tok, "[")
		if bracketIdx != -1 && strings.HasPrefix(tok[bracketIdx:], "[idx_") {
			tok = tok[:bracketIdx]
			bracketIdx = -1
		}
		if bracketIdx != -1 {
			prefix := tok[:bracketIdx]
			keyPart := tok[bracketIdx:]

			displayName := prefix
			if i == 0 {
				displayName = "/" + prefix
			}

			cumulativePath.WriteString("/" + prefix)
			steps = append(steps, stepInfo{
				displayName: displayName,
				fullPath:    cumulativePath.String(),
			})

			cumulativePath.WriteString(keyPart)
			steps = append(steps, stepInfo{
				displayName: keyPart,
				fullPath:    cumulativePath.String(),
			})
		} else {
			displayName := tok
			if i == 0 {
				displayName = "/" + tok
			}
			cumulativePath.WriteString("/" + tok)
			steps = append(steps, stepInfo{
				displayName: displayName,
				fullPath:    cumulativePath.String(),
			})
		}
	}

	return steps
}

func insertStepsIntoTree(rootMap map[string]*TreeNode, rootOrder *[]string, steps []stepInfo, ch models.PathChange) {
	if len(steps) == 0 {
		return
	}

	firstStep := steps[0]
	rootKey := firstStep.fullPath
	rootNode, exists := rootMap[rootKey]
	if !exists {
		rootNode = &TreeNode{
			Name:     firstStep.displayName,
			FullPath: firstStep.fullPath,
			IsLeaf:   len(steps) == 1,
			Expanded: false,
			Depth:    0,
		}
		rootMap[rootKey] = rootNode
		*rootOrder = append(*rootOrder, rootKey)
	} else if len(steps) > 1 {
		rootNode.IsLeaf = false
	}

	if len(steps) == 1 {
		if len(rootNode.Children) == 0 {
			rootNode.DiffType = ch.DiffType
			rootNode.OldValue = ch.OldValue
			rootNode.NewValue = ch.NewValue
		} else {
			rootNode.IsLeaf = false
		}
		return
	}

	curr := rootNode
	for i := 1; i < len(steps); i++ {
		curr.IsLeaf = false
		step := steps[i]
		isLast := (i == len(steps) - 1)

		var child *TreeNode
		for _, c := range curr.Children {
			if c.FullPath == step.fullPath {
				child = c
				break
			}
		}

		if child == nil {
			child = &TreeNode{
				Name:     step.displayName,
				FullPath: step.fullPath,
				IsLeaf:   isLast,
				Expanded: false,
				Parent:   curr,
				Depth:    i,
			}
			curr.Children = append(curr.Children, child)
		}

		if isLast {
			if len(child.Children) == 0 {
				child.IsLeaf = true
			} else {
				child.IsLeaf = false
			}
			child.DiffType = ch.DiffType
			child.OldValue = ch.OldValue
			child.NewValue = ch.NewValue
		} else {
			child.IsLeaf = false
		}

		curr = child
	}
}

// expandCompositeChange expands a PathChange whose value is a map or keyed list into leaf PathChanges
// so added/deleted containers and lists can be expanded and cherry-picked at any granularity.
func expandCompositeChange(basePath string, diffType models.DiffType, val interface{}, out *[]models.PathChange) {
	switch v := val.(type) {
	case map[string]interface{}:
		var keys []string
		for k := range v {
			cleanK := normalizer.StripNamespace(k)
			if !strings.HasPrefix(cleanK, "_") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			cleanK := normalizer.StripNamespace(k)
			subPath := fmt.Sprintf("%s/%s", basePath, cleanK)
			expandCompositeChange(subPath, diffType, v[k], out)
		}
	case []interface{}:
		if len(v) == 0 {
			return
		}
		if _, isMap := v[0].(map[string]interface{}); !isMap {
			// Scalar leaf-list
			ch := models.PathChange{Path: basePath, DiffType: diffType}
			if diffType == models.DiffDeleted {
				ch.OldValue = v
			} else {
				ch.NewValue = v
			}
			*out = append(*out, ch)
			return
		}
		listName := ""
		if idx := strings.LastIndex(basePath, "/"); idx != -1 {
			listName = basePath[idx+1:]
		}
		for _, item := range v {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			keys := schema.GetRegistry().GetListKeys(basePath, listName, itemMap)
			var keyParts []string
			consumed := make(map[string]bool)
			for _, lk := range keys {
				for ik, iv := range itemMap {
					if strings.EqualFold(normalizer.StripNamespace(ik), lk) {
						keyParts = append(keyParts, fmt.Sprintf("%s=%v", lk, iv))
						consumed[strings.ToLower(normalizer.StripNamespace(ik))] = true
						break
					}
				}
			}
			itemPath := basePath
			if len(keyParts) > 0 {
				itemPath = fmt.Sprintf("%s[%s]", basePath, strings.Join(keyParts, "]["))
			}
			remaining := make(map[string]interface{})
			for ik, iv := range itemMap {
				cleanIK := strings.ToLower(normalizer.StripNamespace(ik))
				if !consumed[cleanIK] && !strings.HasPrefix(cleanIK, "_") {
					remaining[ik] = iv
				}
			}
			if len(remaining) == 0 {
				ch := models.PathChange{Path: itemPath, DiffType: diffType}
				if diffType == models.DiffDeleted {
					ch.OldValue = itemMap
				} else {
					ch.NewValue = itemMap
				}
				*out = append(*out, ch)
			} else {
				expandCompositeChange(itemPath, diffType, remaining, out)
			}
		}
	default:
		ch := models.PathChange{Path: basePath, DiffType: diffType}
		if diffType == models.DiffDeleted {
			ch.OldValue = val
		} else {
			ch.NewValue = val
		}
		*out = append(*out, ch)
	}
}

func countTreeChanges(node *TreeNode) int {
	if node.IsLeaf || len(node.Children) == 0 {
		node.ChangeCount = 1
		return 1
	}
	total := 0
	for _, c := range node.Children {
		total += countTreeChanges(c)
	}
	node.ChangeCount = total
	return total
}

func buildTree(changes []models.PathChange, config map[string]interface{}) []*TreeNode {
	rootMap := make(map[string]*TreeNode)
	var rootOrder []string

	if len(changes) > 0 {
		for _, ch := range changes {
			val := ch.NewValue
			if ch.DiffType == models.DiffDeleted {
				val = ch.OldValue
			}
			if listVal, isList := val.([]interface{}); isList && len(listVal) > 0 {
				if _, isMap := listVal[0].(map[string]interface{}); isMap {
					var expanded []models.PathChange
					expandCompositeChange(ch.Path, ch.DiffType, listVal, &expanded)
					for _, subCh := range expanded {
						steps := parsePathSteps(subCh.Path)
						insertStepsIntoTree(rootMap, &rootOrder, steps, subCh)
					}
					continue
				}
			}
			steps := parsePathSteps(ch.Path)
			insertStepsIntoTree(rootMap, &rootOrder, steps, ch)
		}
	} else if len(config) > 0 {
		var keys []string
		for k := range config {
			cleanK := normalizer.StripNamespace(k)
			if !strings.HasPrefix(cleanK, "_") {
				keys = append(keys, cleanK)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			node := &TreeNode{
				Name:        "/" + k,
				FullPath:    "/" + k,
				IsLeaf:      false,
				Expanded:    false,
				Depth:       0,
				ChangeCount: 1,
			}
			rootMap["/"+k] = node
			rootOrder = append(rootOrder, "/"+k)
		}
	} else {
		for _, s := range CommonSubtrees {
			node := &TreeNode{
				Name:        s,
				FullPath:    s,
				IsLeaf:      false,
				Expanded:    false,
				Depth:       0,
				ChangeCount: 1,
			}
			rootMap[s] = node
			rootOrder = append(rootOrder, s)
		}
	}

	var roots []*TreeNode
	for _, k := range rootOrder {
		if node, ok := rootMap[k]; ok {
			countTreeChanges(node)
			roots = append(roots, node)
		}
	}
	return roots
}

// NewCherryPickModal creates a new cherry-pick modal populated with changed paths in a tree.
func NewCherryPickModal(commit models.TimelineCommit, changes []models.PathChange, config map[string]interface{}, width, height int) CherryPickModalModel {
	ti := textinput.New()
	ti.Placeholder = "Or type custom XPath e.g. /interface[name=ethernet-1/1]..."
	ti.Prompt = "Path: "
	ti.Width = 60

	roots := buildTree(changes, config)

	m := CherryPickModalModel{
		Commit:        commit,
		RootNodes:     roots,
		SelectedIndex: 0,
		ScrollOffset:  0,
		SelectedPaths: make(map[string]bool),
		CustomInput:   ti,
		UseCustom:     false,
		Width:         width,
		Height:        height,
	}
	m.VisibleNodes = m.flattenVisibleNodes()
	return m
}

func (m *CherryPickModalModel) flattenVisibleNodes() []*TreeNode {
	var visible []*TreeNode
	var walk func(node *TreeNode)
	walk = func(node *TreeNode) {
		visible = append(visible, node)
		if node.Expanded {
			for _, child := range node.Children {
				walk(child)
			}
		}
	}
	for _, root := range m.RootNodes {
		walk(root)
	}
	return visible
}

func (m *CherryPickModalModel) toggleSelectCurrent() {
	if len(m.VisibleNodes) == 0 || m.SelectedIndex >= len(m.VisibleNodes) {
		return
	}
	node := m.VisibleNodes[m.SelectedIndex]
	currentChecked := m.SelectedPaths[node.FullPath]
	newChecked := !currentChecked

	var setNodeChecked func(n *TreeNode, chk bool)
	setNodeChecked = func(n *TreeNode, chk bool) {
		if chk {
			m.SelectedPaths[n.FullPath] = true
		} else {
			delete(m.SelectedPaths, n.FullPath)
		}
		for _, child := range n.Children {
			setNodeChecked(child, chk)
		}
	}

	setNodeChecked(node, newChecked)

	// If a child node is unchecked, uncheck ancestor containers so the parent path does not override the unchecked child
	if !newChecked {
		for p := node.Parent; p != nil; p = p.Parent {
			delete(m.SelectedPaths, p.FullPath)
		}
	}
}

func (m *CherryPickModalModel) toggleSelectAll() {
	if len(m.SelectedPaths) > 0 {
		m.SelectedPaths = make(map[string]bool)
	} else {
		for _, root := range m.RootNodes {
			var markAll func(n *TreeNode)
			markAll = func(n *TreeNode) {
				m.SelectedPaths[n.FullPath] = true
				for _, c := range n.Children {
					markAll(c)
				}
			}
			markAll(root)
		}
	}
}

// GetSelectedPaths returns the list of selected paths for cherry-pick restoration.
func (m CherryPickModalModel) GetSelectedPaths() []string {
	if m.UseCustom {
		val := strings.TrimSpace(m.CustomInput.Value())
		if val != "" {
			return []string{val}
		}
		return nil
	}

	if len(m.SelectedPaths) > 0 {
		var paths []string
		for p := range m.SelectedPaths {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		var pruned []string
		for _, p := range paths {
			redundant := false
			for _, parent := range pruned {
				if strings.HasPrefix(p, parent+"/") || strings.HasPrefix(p, parent+"[") {
					redundant = true
					break
				}
			}
			if !redundant {
				pruned = append(pruned, p)
			}
		}
		return pruned
	}

	if len(m.VisibleNodes) > 0 && m.SelectedIndex < len(m.VisibleNodes) {
		return []string{m.VisibleNodes[m.SelectedIndex].FullPath}
	}

	return nil
}

func (m *CherryPickModalModel) visibleRowsCount() int {
	modalHeight := m.Height - 4
	if modalHeight < 16 {
		modalHeight = 16
	}
	if modalHeight > 30 {
		modalHeight = 30
	}
	visibleRows := modalHeight - 13
	if visibleRows < 5 {
		visibleRows = 5
	}
	return visibleRows
}

func (m *CherryPickModalModel) syncScrollOffset() {
	visibleRows := m.visibleRowsCount()
	totalVisible := len(m.VisibleNodes)
	if m.SelectedIndex < m.ScrollOffset {
		m.ScrollOffset = m.SelectedIndex
	}
	if m.SelectedIndex >= m.ScrollOffset+visibleRows {
		m.ScrollOffset = m.SelectedIndex - visibleRows + 1
	}
	if m.ScrollOffset > totalVisible-visibleRows {
		maxOffset := totalVisible - visibleRows
		if maxOffset < 0 {
			maxOffset = 0
		}
		m.ScrollOffset = maxOffset
	}
	if m.ScrollOffset < 0 {
		m.ScrollOffset = 0
	}
}

func (m *CherryPickModalModel) expandCurrent() {
	if len(m.VisibleNodes) == 0 || m.SelectedIndex >= len(m.VisibleNodes) {
		return
	}
	node := m.VisibleNodes[m.SelectedIndex]
	if !node.IsLeaf && len(node.Children) > 0 {
		if !node.Expanded {
			node.Expanded = true
			m.VisibleNodes = m.flattenVisibleNodes()
		} else {
			if m.SelectedIndex < len(m.VisibleNodes)-1 {
				m.SelectedIndex++
			}
		}
	}
	m.syncScrollOffset()
}

func (m *CherryPickModalModel) collapseCurrent() {
	if len(m.VisibleNodes) == 0 || m.SelectedIndex >= len(m.VisibleNodes) {
		return
	}
	node := m.VisibleNodes[m.SelectedIndex]
	if node.Expanded {
		node.Expanded = false
		m.VisibleNodes = m.flattenVisibleNodes()
	} else if node.Parent != nil {
		for i, v := range m.VisibleNodes {
			if v == node.Parent {
				m.SelectedIndex = i
				break
			}
		}
	}
	m.syncScrollOffset()
}

func (m *CherryPickModalModel) toggleExpandAll() {
	allExpanded := true
	for _, v := range m.VisibleNodes {
		if !v.IsLeaf && len(v.Children) > 0 && !v.Expanded {
			allExpanded = false
			break
		}
	}

	var setExpanded func(n *TreeNode, exp bool)
	setExpanded = func(n *TreeNode, exp bool) {
		if !n.IsLeaf {
			n.Expanded = exp
			for _, c := range n.Children {
				setExpanded(c, exp)
			}
		}
	}

	for _, r := range m.RootNodes {
		setExpanded(r, !allExpanded)
	}
	m.VisibleNodes = m.flattenVisibleNodes()
	if m.SelectedIndex >= len(m.VisibleNodes) {
		if len(m.VisibleNodes) > 0 {
			m.SelectedIndex = len(m.VisibleNodes) - 1
		} else {
			m.SelectedIndex = 0
		}
	}
	m.syncScrollOffset()
}

// View renders the cherry-pick subtree selection dialog with interactive tree, checkboxes, and scrolling.
func (m CherryPickModalModel) View() string {
	modalWidth := m.Width - 10
	if modalWidth < 70 {
		modalWidth = 70
	}
	if modalWidth > 98 {
		modalWidth = 98
	}

	visibleRows := m.visibleRowsCount()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#d29922")).
		Padding(0, 1).
		Render("🍒 CHERRY-PICK CONFIGURATION RESTORE")

	shaStr := m.Commit.FullSHA
	if len(shaStr) > 8 {
		shaStr = shaStr[:8]
	}

	totalVisible := len(m.VisibleNodes)
	pct := 0
	if totalVisible > 1 {
		pct = (m.SelectedIndex * 100) / (totalVisible - 1)
	}

	selectedCount := len(m.SelectedPaths)
	var badgeText string
	if selectedCount > 0 {
		badgeText = fmt.Sprintf("Item %d of %d (%d%%) • %d selected", m.SelectedIndex+1, totalVisible, pct, selectedCount)
	} else {
		badgeText = fmt.Sprintf("Item %d of %d (%d%%)", m.SelectedIndex+1, totalVisible, pct)
	}

	scrollBadge := lipgloss.NewStyle().
		Background(lipgloss.Color("#21262d")).
		Foreground(lipgloss.Color("#58a6ff")).
		Bold(true).
		Padding(0, 1).
		Render(badgeText)

	headerBar := lipgloss.JoinHorizontal(
		lipgloss.Center,
		title,
		"   ",
		scrollBadge,
	)

	info := fmt.Sprintf(
		"Target Commit: %s (%s)\nSelect one or more subtrees/lines to restore onto the live switch:",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render(shaStr),
		m.Commit.Message,
	)

	// Keep selection within scroll viewport
	scrollOffset := m.ScrollOffset
	if m.SelectedIndex < scrollOffset {
		scrollOffset = m.SelectedIndex
	}
	if m.SelectedIndex >= scrollOffset+visibleRows {
		scrollOffset = m.SelectedIndex - visibleRows + 1
	}
	if scrollOffset > totalVisible-visibleRows {
		maxOffset := totalVisible - visibleRows
		if maxOffset < 0 {
			maxOffset = 0
		}
		scrollOffset = maxOffset
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}

	startIdx := scrollOffset
	endIdx := scrollOffset + visibleRows
	if endIdx > totalVisible {
		endIdx = totalVisible
	}

	var listSb strings.Builder

	if startIdx > 0 {
		listSb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Italic(true).Render(fmt.Sprintf("  ▲ [%d items above - scroll up]\n", startIdx)))
	}

	for i := startIdx; i < endIdx; i++ {
		node := m.VisibleNodes[i]
		indent := strings.Repeat("  ", node.Depth)

		isChecked := m.SelectedPaths[node.FullPath]
		var checkMark string
		if isChecked {
			checkMark = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3fb950")).Render("[✓] ")
		} else {
			checkMark = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render("[ ] ")
		}

		var icon string
		var nameStyled string
		var countBadge string

		if node.IsLeaf {
			switch node.DiffType {
			case models.DiffAdded:
				icon = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950")).Render("+ 📄 ")
				nameStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950")).Render(node.Name)
				if node.NewValue != nil {
					nameStyled += lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(fmt.Sprintf(": %v", node.NewValue))
				}
			case models.DiffDeleted:
				icon = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")).Render("- 📄 ")
				nameStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")).Render(node.Name)
				if node.OldValue != nil {
					nameStyled += lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(fmt.Sprintf(": %v (deleted)", node.OldValue))
				}
			case models.DiffModified:
				icon = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922")).Render("~ 📄 ")
				nameStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922")).Render(node.Name)
				if node.NewValue != nil {
					nameStyled += lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(fmt.Sprintf(": %v", node.NewValue))
				}
			default:
				icon = lipgloss.NewStyle().Foreground(lipgloss.Color("#79c0ff")).Render("• 📄 ")
				nameStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("#e6edf3")).Render(node.Name)
			}
		} else {
			if node.Expanded {
				icon = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render("▼ 📁 ")
			} else {
				icon = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8b949e")).Render("▶ 📁 ")
			}
			nameStyled = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e6edf3")).Render(node.Name)
			if node.ChangeCount == 1 {
				countBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#79c0ff")).Render(" (1 change)")
			} else if node.ChangeCount > 1 {
				countBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#79c0ff")).Render(fmt.Sprintf(" (%d changes)", node.ChangeCount))
			}
		}

		rowContent := fmt.Sprintf("%s%s%s%s%s", indent, checkMark, icon, nameStyled, countBadge)

		cursor := "  "
		if !m.UseCustom && i == m.SelectedIndex {
			cursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render("➜ ")
			rowContent = lipgloss.NewStyle().
				Background(lipgloss.Color("#1f242c")).
				Bold(true).
				Render(rowContent)
		}

		listSb.WriteString(fmt.Sprintf("%s%s\n", cursor, rowContent))
	}

	if endIdx < totalVisible {
		listSb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Italic(true).Render(fmt.Sprintf("  ▼ [%d items below - scroll down]\n", totalVisible-endIdx)))
	}

	var summaryTitle string
	var summaryScope string
	if m.UseCustom {
		summaryTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render("Custom XPath: " + m.CustomInput.Value())
		summaryScope = "Applies manually entered XPath"
	} else if selectedCount > 0 {
		var sortedSelected []string
		for p := range m.SelectedPaths {
			sortedSelected = append(sortedSelected, p)
		}
		sort.Strings(sortedSelected)
		samplePaths := sortedSelected
		if len(samplePaths) > 2 {
			samplePaths = samplePaths[:2]
		}
		summaryTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3fb950")).Render(fmt.Sprintf("%d items selected (%s...)", selectedCount, strings.Join(samplePaths, ", ")))
		summaryScope = fmt.Sprintf("Restores all %d selected paths together atomically in a single candidate commit", selectedCount)
	} else if len(m.VisibleNodes) > 0 && m.SelectedIndex < len(m.VisibleNodes) {
		selNode := m.VisibleNodes[m.SelectedIndex]
		summaryTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render(selNode.FullPath)
		if selNode.IsLeaf {
			summaryScope = "Restores individual line / property (Tip: press [Space] or [x] to multi-select)"
		} else {
			summaryScope = fmt.Sprintf("Restores all %d changes under this subtree (Tip: press [Space] or [x] to multi-select)", selNode.ChangeCount)
		}
	}

	selectedSummary := lipgloss.NewStyle().
		Background(lipgloss.Color("#161b22")).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("#1f6feb")).
		Padding(0, 1).
		Width(modalWidth - 6).
		Render(fmt.Sprintf(
			"Target: %s\nScope:  %s",
			summaryTitle,
			lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(summaryScope),
		))

	customPrompt := "Custom XPath:"
	if m.UseCustom {
		customPrompt = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render("▶ Custom XPath:")
	}
	customView := fmt.Sprintf("%s\n%s", customPrompt, m.CustomInput.View())

	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e")).Render(
		"[Space/x] Toggle  |  [a] Select All  |  [→/←] Expand/Collapse  |  [e] Expand All  |  [Tab] Custom  |  [Enter] Apply  |  [Esc] Cancel",
	)

	content := fmt.Sprintf("%s\n\n%s\n\n%s\n%s\n\n%s\n\n%s", headerBar, info, listSb.String(), selectedSummary, customView, footer)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#d29922")).
		Background(lipgloss.Color("#0d1117")).
		Padding(1, 2).
		Width(modalWidth).
		Render(content)

	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
}

// Update processes cherry-pick selection key and mouse events.
func (m CherryPickModalModel) Update(msg tea.Msg) (CherryPickModalModel, []string, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, nil, true

		case "tab":
			m.UseCustom = !m.UseCustom
			if m.UseCustom {
				m.CustomInput.Focus()
			} else {
				m.CustomInput.Blur()
			}
			return m, nil, false

		case "enter":
			paths := m.GetSelectedPaths()
			if len(paths) > 0 {
				return m, paths, true
			}
			return m, nil, false
		}

		if m.UseCustom {
			var cmd tea.Cmd
			m.CustomInput, cmd = m.CustomInput.Update(msg)
			_ = cmd
			return m, nil, false
		}

		switch msg.String() {
		case "up", "k":
			if m.SelectedIndex > 0 {
				m.SelectedIndex--
				m.syncScrollOffset()
			}
			return m, nil, false

		case "down", "j":
			if m.SelectedIndex < len(m.VisibleNodes)-1 {
				m.SelectedIndex++
				m.syncScrollOffset()
			}
			return m, nil, false

		case " ", "space", "x":
			m.toggleSelectCurrent()
			return m, nil, false

		case "a":
			m.toggleSelectAll()
			return m, nil, false

		case "right", "l", "+":
			m.expandCurrent()
			return m, nil, false

		case "left", "h", "-":
			m.collapseCurrent()
			return m, nil, false

		case "e", "*":
			m.toggleExpandAll()
			return m, nil, false

		case "pgup", "ctrl+u", "b":
			m.SelectedIndex = m.SelectedIndex - 8
			if m.SelectedIndex < 0 {
				m.SelectedIndex = 0
			}
			m.syncScrollOffset()
			return m, nil, false

		case "pgdown", "ctrl+d", "f":
			m.SelectedIndex = m.SelectedIndex + 8
			if m.SelectedIndex >= len(m.VisibleNodes) {
				if len(m.VisibleNodes) > 0 {
					m.SelectedIndex = len(m.VisibleNodes) - 1
				} else {
					m.SelectedIndex = 0
				}
			}
			m.syncScrollOffset()
			return m, nil, false

		case "home", "g":
			m.SelectedIndex = 0
			m.syncScrollOffset()
			return m, nil, false

		case "end", "G":
			if len(m.VisibleNodes) > 0 {
				m.SelectedIndex = len(m.VisibleNodes) - 1
				m.syncScrollOffset()
			}
			return m, nil, false
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			if !m.UseCustom && m.SelectedIndex > 0 {
				m.SelectedIndex--
				m.syncScrollOffset()
			}
			return m, nil, false
		case tea.MouseWheelDown:
			if !m.UseCustom && m.SelectedIndex < len(m.VisibleNodes)-1 {
				m.SelectedIndex++
				m.syncScrollOffset()
			}
			return m, nil, false
		}
	}

	if m.UseCustom {
		var cmd tea.Cmd
		m.CustomInput, cmd = m.CustomInput.Update(msg)
		_ = cmd
	}

	return m, nil, false
}
