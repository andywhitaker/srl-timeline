package modals

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"

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

const (
	cherryFocusTree   = 0
	cherryFocusCustom = 1
	cherryFocusApply  = 2
	cherryFocusCancel = 3
)

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
	FocusArea     int // 0: Tree, 1: CustomInput, 2: Apply button, 3: Cancel button
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
		isLast := (i == len(steps)-1)

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
	ti.Width = 56
	StyleTextInput(&ti, ColorSurface, ColorText, ColorTextDim, ColorActive)

	roots := buildTree(changes, config)

	m := CherryPickModalModel{
		Commit:        commit,
		RootNodes:     roots,
		SelectedIndex: 0,
		ScrollOffset:  0,
		SelectedPaths: make(map[string]bool),
		CustomInput:   ti,
		UseCustom:     false,
		FocusArea:     cherryFocusTree,
		Width:         width,
		Height:        height,
	}
	m.VisibleNodes = m.flattenVisibleNodes()
	return m
}

func (m *CherryPickModalModel) setFocusArea(area int) {
	m.FocusArea = area
	m.UseCustom = (area == cherryFocusCustom)
	if m.UseCustom {
		m.CustomInput.Focus()
	} else {
		m.CustomInput.Blur()
	}
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

func (m *CherryPickModalModel) toggleSelectNode(index int) {
	if len(m.VisibleNodes) == 0 || index < 0 || index >= len(m.VisibleNodes) {
		return
	}
	node := m.VisibleNodes[index]
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

func (m *CherryPickModalModel) toggleSelectCurrent() {
	m.toggleSelectNode(m.SelectedIndex)
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

// PrunedSelectedPaths returns the deduplicated, ancestor-pruned list of checked paths.
func (m CherryPickModalModel) PrunedSelectedPaths() []string {
	if len(m.SelectedPaths) == 0 {
		return nil
	}
	paths := make([]string, 0, len(m.SelectedPaths))
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

// GetSelectedPaths returns the list of selected paths for cherry-pick restoration.
func (m CherryPickModalModel) GetSelectedPaths() []string {
	if m.UseCustom {
		val := strings.TrimSpace(m.CustomInput.Value())
		if val != "" {
			return []string{val}
		}
		return nil
	}

	if pruned := m.PrunedSelectedPaths(); len(pruned) > 0 {
		return pruned
	}

	if len(m.VisibleNodes) > 0 && m.SelectedIndex < len(m.VisibleNodes) {
		return []string{m.VisibleNodes[m.SelectedIndex].FullPath}
	}

	return nil
}

func (m CherryPickModalModel) visibleRowsCount() int {
	if m.Height > 0 && m.Height <= 14 {
		rows := m.Height - 9
		if rows < 2 {
			rows = 2
		}
		return rows
	}
	if m.Height > 0 && m.Height <= 26 {
		rows := m.Height - 17
		if rows < 3 {
			rows = 3
		}
		return rows
	}
	modalHeight := m.Height - 4
	if modalHeight < 16 {
		modalHeight = 16
	}
	if modalHeight > 30 {
		modalHeight = 30
	}
	visibleRows := modalHeight - 14
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

func formatScalarPreview(val interface{}, maxW int) string {
	if val == nil || maxW <= 0 {
		return ""
	}
	var raw string
	switch v := val.(type) {
	case string:
		raw = v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		raw = "[" + strings.Join(parts, ", ") + "]"
	case map[string]interface{}:
		raw = fmt.Sprintf("{%d fields}", len(v))
	default:
		raw = fmt.Sprintf("%v", v)
	}
	return truncateDisplay(raw, maxW)
}

func pluralPaths(n int) string {
	if n == 1 {
		return "1 path"
	}
	return fmt.Sprintf("%d paths", n)
}

func (m CherryPickModalModel) renderLayout() (content string, innerW, vPad, hPad, maxInnerH, startIdx, endIdx, listStartY, buttonsY, boxLeftX, boxRightX int) {
	preferredW := 90
	if m.Width > 0 && m.Width-10 < preferredW {
		preferredW = m.Width - 10
	}
	if preferredW < 68 && m.Width >= 74 {
		preferredW = 68
	}
	innerW, vPad, hPad, maxInnerH = modalBoxMetrics(m.Width, m.Height, preferredW)

	compact := m.Height > 0 && m.Height <= 26
	ultraCompact := m.Height > 0 && m.Height <= 14

	inputW := innerW - 8
	if inputW < 10 {
		inputW = 10
	}
	m.CustomInput.Width = inputW
	StyleTextInput(&m.CustomInput, ColorSurface, ColorText, ColorTextDim, ColorActive)

	visibleRows := m.visibleRowsCount()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorOnWarning).
		Background(ColorWarningBg).
		Padding(0, 1).
		Render("◆ CHERRY-PICK CONFIGURATION RESTORE")

	shaStr := m.Commit.FullSHA
	if len(shaStr) > 8 {
		shaStr = shaStr[:8]
	}

	totalVisible := len(m.VisibleNodes)
	pct := 0
	if totalVisible > 1 {
		pct = (m.SelectedIndex * 100) / (totalVisible - 1)
	}

	prunedSelected := m.PrunedSelectedPaths()
	selectedCount := len(prunedSelected)

	var badgeText string
	pos := 0
	if totalVisible > 0 {
		pos = m.SelectedIndex + 1
	}
	if selectedCount > 0 {
		badgeText = fmt.Sprintf("Item %d/%d (%d%%) · %s selected", pos, totalVisible, pct, pluralPaths(selectedCount))
	} else {
		badgeText = fmt.Sprintf("Item %d/%d (%d%%)", pos, totalVisible, pct)
	}

	scrollBadge := lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorActive).
		Bold(true).
		Padding(0, 1).
		Render(badgeText)

	headerBar := PaintBarLine(
		lipgloss.JoinHorizontal(
			lipgloss.Center,
			title,
			lipgloss.NewStyle().Background(ColorSurface).Render("  "),
			scrollBadge,
		),
		innerW,
		ColorSurface,
		ColorText,
	)

	textStyle := lipgloss.NewStyle().Foreground(ColorText)
	infoLines := []string{
		PaintBarLine(
			fmt.Sprintf(
				"%s %s %s",
				textStyle.Render("Target Commit:"),
				lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render(shaStr),
				textStyle.Render(fmt.Sprintf("(%s)", truncateDisplay(cleanCommitSummary(m.Commit), innerW-28))),
			),
			innerW,
			ColorSurface,
			ColorText,
		),
	}
	if !compact {
		infoLines = append(infoLines, PaintBarLine(
			lipgloss.NewStyle().Foreground(ColorMuted).Render("Select one or more subtrees/leaves to restore onto the live switch:"),
			innerW,
			ColorSurface,
			ColorMuted,
		))
	}

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

	startIdx = scrollOffset
	endIdx = scrollOffset + visibleRows
	if endIdx > totalVisible {
		endIdx = totalVisible
	}

	var listLines []string
	aboveCount := 0
	if startIdx > 0 {
		aboveCount = 1
		aboveLine := lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("  ↑ [%d items above]", startIdx))
		listLines = append(listLines, PaintBarLine(aboveLine, innerW, ColorSurface, ColorMuted))
	}

	for i := startIdx; i < endIdx; i++ {
		node := m.VisibleNodes[i]
		indent := strings.Repeat("  ", node.Depth)

		rowBg := ColorSurface
		isCursor := m.FocusArea == cherryFocusTree && !m.UseCustom && i == m.SelectedIndex
		if isCursor {
			rowBg = ColorHighlight
		}

		isChecked := m.SelectedPaths[node.FullPath]
		var checkMark string
		if isChecked {
			checkMark = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorSuccess).Render("[✓] ")
		} else {
			checkMark = lipgloss.NewStyle().Background(rowBg).Foreground(ColorMuted).Render("[ ] ")
		}

		var icon string
		var nameStyled string
		var detailStyled string

		// Compute remaining budget for scalar preview so long values/certs never overflow
		prefixW := 2 + len(indent) + 4 + 2 + ansi.PrintableRuneWidth(node.Name)
		remW := innerW - prefixW
		if remW < 0 {
			remW = 0
		}

		if node.IsLeaf {
			switch node.DiffType {
			case models.DiffAdded:
				icon = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorSuccess).Render("+ ")
				nameStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorSuccess).Render(node.Name)
				if node.NewValue != nil && remW > 4 {
					preview := formatScalarPreview(node.NewValue, remW-2)
					if preview != "" {
						detailStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorMuted).Render(": " + preview)
					}
				}
			case models.DiffDeleted:
				icon = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorDanger).Render("- ")
				nameStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorDanger).Render(node.Name)
				if node.OldValue != nil && remW > 12 {
					preview := formatScalarPreview(node.OldValue, remW-12)
					if preview != "" {
						detailStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorMuted).Render(": " + preview + " (deleted)")
					}
				}
			case models.DiffModified:
				icon = lipgloss.NewStyle().Bold(true).Background(ColorWarningBg).Foreground(ColorOnWarning).Render("~") +
					lipgloss.NewStyle().Background(rowBg).Render(" ")
				nameStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorWarning).Render(node.Name)
				if remW > 6 {
					colon := lipgloss.NewStyle().Background(rowBg).Foreground(ColorMuted).Render(": ")
					arrow := lipgloss.NewStyle().Background(rowBg).Foreground(ColorMuted).Render(" → ")
					if node.OldValue != nil && node.NewValue != nil {
						sideBudget := (remW - 5) / 2
						if sideBudget > 28 {
							sideBudget = 28
						}
						if sideBudget < 4 {
							sideBudget = 4
						}
						oldStr := formatScalarPreview(node.OldValue, sideBudget)
						newStr := formatScalarPreview(node.NewValue, sideBudget)
						detailStyled = colon +
							lipgloss.NewStyle().Background(rowBg).Foreground(ColorDanger).Render(oldStr) +
							arrow +
							lipgloss.NewStyle().Background(rowBg).Foreground(ColorSuccess).Render(newStr)
					} else if node.NewValue != nil {
						newStr := formatScalarPreview(node.NewValue, remW-2)
						detailStyled = colon + lipgloss.NewStyle().Background(rowBg).Foreground(ColorSuccess).Render(newStr)
					} else if node.OldValue != nil {
						oldStr := formatScalarPreview(node.OldValue, remW-2)
						detailStyled = colon + lipgloss.NewStyle().Background(rowBg).Foreground(ColorDanger).Render(oldStr)
					}
				}
			default:
				icon = lipgloss.NewStyle().Background(rowBg).Foreground(ColorActive).Render("● ")
				nameStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorText).Render(node.Name)
			}
		} else {
			if node.Expanded {
				icon = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorActive).Render("▾ ")
			} else {
				icon = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorMuted).Render("▸ ")
			}
			nameStyled = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorText).Render(node.Name)
			if node.ChangeCount == 1 {
				detailStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorActive).Render(" (1 change)")
			} else if node.ChangeCount > 1 {
				detailStyled = lipgloss.NewStyle().Background(rowBg).Foreground(ColorActive).Render(fmt.Sprintf(" (%d changes)", node.ChangeCount))
			}
		}

		cursor := lipgloss.NewStyle().Background(rowBg).Render("  ")
		if isCursor {
			cursor = lipgloss.NewStyle().Bold(true).Background(rowBg).Foreground(ColorActive).Render("▸ ")
		}

		indentStyled := lipgloss.NewStyle().Background(rowBg).Render(indent)
		rowContent := cursor + indentStyled + checkMark + icon + nameStyled + detailStyled
		listLines = append(listLines, PaintBarLine(rowContent, innerW, rowBg, ColorText))
	}

	if endIdx < totalVisible {
		belowLine := lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("  ↓ [%d items below]", totalVisible-endIdx))
		listLines = append(listLines, PaintBarLine(belowLine, innerW, ColorSurface, ColorMuted))
	}

	// Selection summary
	var summaryTitle string
	var summaryScope string
	if m.UseCustom {
		summaryTitle = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("Custom XPath: " + m.CustomInput.Value())
		summaryScope = "Applies manually entered XPath"
	} else if selectedCount > 0 {
		samplePaths := prunedSelected
		suffix := ""
		if len(samplePaths) > 2 {
			samplePaths = samplePaths[:2]
			suffix = ", …"
		}
		summaryTitle = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Render(
			fmt.Sprintf("%s selected (%s%s)", pluralPaths(selectedCount), strings.Join(samplePaths, ", "), suffix),
		)
		if selectedCount == 1 {
			summaryScope = "Restores 1 selected path atomically in a single candidate commit"
		} else {
			summaryScope = fmt.Sprintf("Restores %d selected paths together atomically in a single candidate commit", selectedCount)
		}
	} else if len(m.VisibleNodes) > 0 && m.SelectedIndex < len(m.VisibleNodes) {
		selNode := m.VisibleNodes[m.SelectedIndex]
		summaryTitle = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render(selNode.FullPath)
		if selNode.IsLeaf {
			summaryScope = "Restores individual leaf (Space/x to multi-select)"
		} else {
			summaryScope = fmt.Sprintf("Container with %d changes (Enter expands; Space/x selects subtree)", selNode.ChangeCount)
		}
	}

	summaryW := innerW - 3
	if summaryW < 6 {
		summaryW = 6
	}
	summaryLine1 := PaintBarLine(fmt.Sprintf("%s %s", textStyle.Bold(true).Render("Target:"), summaryTitle), summaryW, ColorSurface, ColorText)
	summaryLine2 := PaintBarLine(fmt.Sprintf("%s %s", textStyle.Bold(true).Render("Scope: "), lipgloss.NewStyle().Foreground(ColorMuted).Render(summaryScope)), summaryW, ColorSurface, ColorText)
	selectedSummary := lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorText).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(ColorPrimary).
		BorderBackground(ColorSurface).
		Padding(0, 1).
		Render(summaryLine1 + "\n" + summaryLine2)

	customPrompt := lipgloss.NewStyle().Foreground(ColorMuted).Render("Custom XPath:")
	if m.UseCustom {
		customPrompt = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("▸ Custom XPath:")
	}
	customView := PaintBarLine(customPrompt, innerW, ColorSurface, ColorText) + "\n" +
		PaintBarLine(m.CustomInput.View(), innerW, ColorSurface, ColorText)

	// Explicit [ Apply ] and [ Cancel ] buttons
	applyLabel := "[ Apply ]"
	if selectedCount > 0 {
		applyLabel = fmt.Sprintf("[ Apply (%s) ]", pluralPaths(selectedCount))
	}
	applyStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorText)
	if m.FocusArea == cherryFocusApply {
		applyStyle = applyStyle.Background(ColorSuccessBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnApply := applyStyle.Render(applyLabel)

	cancelStyle := lipgloss.NewStyle().Padding(0, 1).Background(ColorBadgeBg).Foreground(ColorMuted)
	if m.FocusArea == cherryFocusCancel {
		cancelStyle = cancelStyle.Background(ColorDangerBg).Foreground(ColorOnAccent).Bold(true)
	}
	btnCancel := cancelStyle.Render("[ Cancel ]")

	btnGap := lipgloss.NewStyle().Background(ColorSurface).Render("  ")
	buttonsRow := PaintBarLine(
		lipgloss.JoinHorizontal(lipgloss.Center, btnApply, btnGap, btnCancel),
		innerW,
		ColorSurface,
		ColorText,
	)

	footerItems := []string{"Space/x Select", "a All", "Enter Expand/Apply", "A Apply", "Tab Focus", "Esc Cancel"}
	if innerW < 78 {
		footerItems = []string{"Space/x Sel", "a All", "Enter Expand", "A Apply", "Tab Focus", "Esc Cancel"}
	}
	footer := renderModalFooter(
		footerItems,
		"Space Sel · Enter OK · Esc",
		innerW,
		!ultraCompact,
	)

	// Assemble lines while tracking exact vertical coordinates for mouse hit-testing
	var lines []string
	appendBlock := func(block string) {
		lines = append(lines, strings.Split(block, "\n")...)
	}

	appendBlock(headerBar)
	if !compact {
		lines = append(lines, "")
	}
	for _, il := range infoLines {
		appendBlock(il)
	}
	if !compact {
		lines = append(lines, "")
	}

	listOffsetInContent := len(lines) + aboveCount
	for _, ll := range listLines {
		appendBlock(ll)
	}

	if !ultraCompact {
		if !compact {
			lines = append(lines, "")
		}
		appendBlock(selectedSummary)
		if !compact {
			lines = append(lines, "")
		}
		appendBlock(customView)
	}

	buttonsOffsetInContent := len(lines)
	appendBlock(buttonsRow)
	appendBlock(footer)

	contentLineCount := len(lines)
	if maxInnerH > 0 && contentLineCount > maxInnerH {
		contentLineCount = maxInnerH
	}
	boxH := contentLineCount + 2 + vPad*2
	boxW := innerW + 2 + hPad*2

	topY := 0
	if m.Height > boxH {
		topY = (m.Height - boxH) / 2
	}
	boxLeftX = 0
	if m.Width > boxW {
		boxLeftX = (m.Width - boxW) / 2
	}
	boxRightX = boxLeftX + boxW - 1
	listStartY = topY + 1 + vPad + listOffsetInContent
	buttonsY = topY + 1 + vPad + buttonsOffsetInContent

	content = strings.Join(lines, "\n")
	return content, innerW, vPad, hPad, maxInnerH, startIdx, endIdx, listStartY, buttonsY, boxLeftX, boxRightX
}

// View renders the cherry-pick subtree selection dialog with interactive tree, checkboxes, and scrolling.
func (m CherryPickModalModel) View() string {
	content, innerW, vPad, hPad, maxInnerH, _, _, _, _, _, _ := m.renderLayout()
	return renderCenteredModal(content, m.Width, m.Height, innerW, vPad, hPad, maxInnerH, ColorWarningBg)
}

// Update processes cherry-pick selection key and mouse events.
func (m CherryPickModalModel) Update(msg tea.Msg) (CherryPickModalModel, []string, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "esc":
			return m, nil, true

		case "tab":
			m.setFocusArea((m.FocusArea + 1) % 4)
			return m, nil, false

		case "shift+tab":
			m.setFocusArea((m.FocusArea + 3) % 4)
			return m, nil, false
		}

		// Handle button focus areas
		if m.FocusArea == cherryFocusApply {
			switch key {
			case "left", "right", "h", "l":
				m.setFocusArea(cherryFocusCancel)
				return m, nil, false
			case "enter", " ", "space":
				paths := m.GetSelectedPaths()
				if len(paths) > 0 {
					return m, paths, true
				}
				return m, nil, false
			}
			return m, nil, false
		}
		if m.FocusArea == cherryFocusCancel {
			switch key {
			case "left", "right", "h", "l":
				m.setFocusArea(cherryFocusApply)
				return m, nil, false
			case "enter", " ", "space":
				return m, nil, true
			}
			return m, nil, false
		}

		// Handle Custom XPath input focus
		if m.UseCustom || m.FocusArea == cherryFocusCustom {
			if key == "enter" {
				paths := m.GetSelectedPaths()
				if len(paths) > 0 {
					return m, paths, true
				}
				return m, nil, false
			}
			var cmd tea.Cmd
			m.CustomInput, cmd = m.CustomInput.Update(msg)
			_ = cmd
			return m, nil, false
		}

		// Tree focus mode
		switch key {
		case "enter":
			if len(m.VisibleNodes) > 0 && m.SelectedIndex < len(m.VisibleNodes) {
				node := m.VisibleNodes[m.SelectedIndex]
				if !node.IsLeaf && len(node.Children) > 0 && !node.Expanded {
					m.expandCurrent()
					return m, nil, false
				}
			}
			paths := m.GetSelectedPaths()
			if len(paths) > 0 {
				return m, paths, true
			}
			return m, nil, false

		case "A":
			paths := m.GetSelectedPaths()
			if len(paths) > 0 {
				return m, paths, true
			}
			return m, nil, false

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
			m.SelectedIndex -= 8
			if m.SelectedIndex < 0 {
				m.SelectedIndex = 0
			}
			m.syncScrollOffset()
			return m, nil, false

		case "pgdown", "ctrl+d", "f":
			m.SelectedIndex += 8
			if m.SelectedIndex >= len(m.VisibleNodes) {
				if len(m.VisibleNodes) > 0 {
					m.SelectedIndex = len(m.VisibleNodes) - 1
				} else {
					m.SelectedIndex = 0
				}
			}
			m.syncScrollOffset()
			return m, nil, false

		case "home":
			m.SelectedIndex = 0
			m.syncScrollOffset()
			return m, nil, false

		case "end":
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

		case tea.MouseLeft:
			_, _, _, _, _, startIdx, endIdx, listStartY, buttonsY, leftX, rightX := m.renderLayout()
			if msg.X >= leftX && msg.X <= rightX {
				row := msg.Y - listStartY
				if row >= 0 && startIdx+row < endIdx {
					m.setFocusArea(cherryFocusTree)
					m.SelectedIndex = startIdx + row
					m.toggleSelectCurrent()
					m.syncScrollOffset()
					return m, nil, false
				}
				if msg.Y == buttonsY {
					relX := msg.X - leftX
					if relX >= 3 && relX <= 22 {
						paths := m.GetSelectedPaths()
						if len(paths) > 0 {
							return m, paths, true
						}
					} else if relX > 22 && relX <= 36 {
						return m, nil, true
					}
				}
			}
		}
	}

	if m.UseCustom {
		var cmd tea.Cmd
		m.CustomInput, cmd = m.CustomInput.Update(msg)
		_ = cmd
	}

	return m, nil, false
}
