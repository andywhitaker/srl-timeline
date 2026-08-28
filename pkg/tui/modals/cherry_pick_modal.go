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
	}

	curr := rootNode
	for i := 1; i < len(steps); i++ {
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
			child.IsLeaf = true
			child.DiffType = ch.DiffType
			child.OldValue = ch.OldValue
			child.NewValue = ch.NewValue
		}

		curr = child
	}
}

func countTreeChanges(node *TreeNode) int {
	if node.IsLeaf {
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
		return paths
	}

	if len(m.VisibleNodes) > 0 && m.SelectedIndex < len(m.VisibleNodes) {
		return []string{m.VisibleNodes[m.SelectedIndex].FullPath}
	}

	return nil
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
		var samplePaths []string
		for p := range m.SelectedPaths {
			samplePaths = append(samplePaths, p)
			if len(samplePaths) >= 2 {
				break
			}
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
	// Returns (model, selectedSubtrees, close)
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

		case "up", "k":
			if !m.UseCustom && m.SelectedIndex > 0 {
				m.SelectedIndex--
			}
			return m, nil, false

		case "down", "j":
			if !m.UseCustom && m.SelectedIndex < len(m.VisibleNodes)-1 {
				m.SelectedIndex++
			}
			return m, nil, false

		case " ", "x":
			if !m.UseCustom {
				m.toggleSelectCurrent()
			}
			return m, nil, false

		case "a":
			if !m.UseCustom {
				m.toggleSelectAll()
			}
			return m, nil, false

		case "right", "l", "+":
			if !m.UseCustom {
				m.expandCurrent()
			}
			return m, nil, false

		case "left", "h", "-":
			if !m.UseCustom {
				m.collapseCurrent()
			}
			return m, nil, false

		case "e", "*":
			if !m.UseCustom {
				m.toggleExpandAll()
			}
			return m, nil, false

		case "pgup", "ctrl+u", "b":
			if !m.UseCustom {
				m.SelectedIndex = m.SelectedIndex - 8
				if m.SelectedIndex < 0 {
					m.SelectedIndex = 0
				}
			}
			return m, nil, false

		case "pgdown", "ctrl+d", "f":
			if !m.UseCustom {
				m.SelectedIndex = m.SelectedIndex + 8
				if m.SelectedIndex >= len(m.VisibleNodes) {
					if len(m.VisibleNodes) > 0 {
						m.SelectedIndex = len(m.VisibleNodes) - 1
					} else {
						m.SelectedIndex = 0
					}
				}
			}
			return m, nil, false

		case "home", "g":
			if !m.UseCustom {
				m.SelectedIndex = 0
			}
			return m, nil, false

		case "end", "G":
			if !m.UseCustom && len(m.VisibleNodes) > 0 {
				m.SelectedIndex = len(m.VisibleNodes) - 1
			}
			return m, nil, false

		case "enter":
			paths := m.GetSelectedPaths()
			if len(paths) > 0 {
				return m, paths, true
			}
			return m, nil, false
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			if !m.UseCustom && m.SelectedIndex > 0 {
				m.SelectedIndex--
			}
			return m, nil, false
		case tea.MouseWheelDown:
			if !m.UseCustom && m.SelectedIndex < len(m.VisibleNodes)-1 {
				m.SelectedIndex++
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


