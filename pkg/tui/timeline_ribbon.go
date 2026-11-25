package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"
	"github.com/muesli/reflow/truncate"

	"timeline/pkg/models"
)

// Unicode block elements ordered from smallest (1/8) to full (8/8) for log-scaled bars.
var ribbonBlockLevels = [8]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// TimelinePlotPoint represents a single (or column-aggregated) commit plotted on the ribbon.
type TimelinePlotPoint struct {
	CommitIndex int
	Commit      models.TimelineCommit
	Pos         float64 // normalized [0..1] in active window
	Magnitude   int     // parsed total lines changed (add+mod+del)
	Dominant    string  // "add", "mod", "del", "restore", "init"
	IsSelected  bool
	IsHead      bool
	IsA         bool
	IsB         bool
	ClusterSize int // number of commits collapsed into this column
}

// TimelinePlotSpec describes the geometry, time bounds, data points, and optional A<->B span
// passed to a TimelinePlotRenderer implementation.
type TimelinePlotSpec struct {
	Width, Height          int // inner plot area (e.g. innerW x 2 rows)
	WindowStart, WindowEnd time.Time
	Points                 []TimelinePlotPoint
	SpanA, SpanB           *float64 // optional [0..1] endpoints for A<->B highlight
	BrushStart, BrushEnd   int      // inclusive inner-column range for active brush selection (-1 when inactive)
	Focused                bool
}

// TimelinePlotRenderer decouples ribbon state & hit-testing from terminal graphics rendering,
// allowing higher-resolution backends (Braille / Kitty / Sixel) to drop in later.
type TimelinePlotRenderer interface {
	RenderPlot(spec TimelinePlotSpec) (rows []string, colToCommit map[int]int)
}

// BlockPlotRenderer is the default pure-Unicode block histogram + geometric rail renderer.
type BlockPlotRenderer struct{}

type plotColumnBucket struct {
	col         int
	rep         TimelinePlotPoint
	totalMag    int
	clusterSize int
	isSelected  bool
	isHead      bool
	isA         bool
	isB         bool
	isRestored  bool
	isStartup   bool
	dominant    string
	aSHA        string
	bSHA        string
	selectedSHA string
}

func plotPointPriority(p TimelinePlotPoint) int {
	switch {
	case p.IsSelected:
		return 500
	case p.IsA || p.IsB:
		return 400
	case p.IsHead:
		return 300
	case p.Commit.IsRestored:
		return 200
	default:
		return 100
	}
}

func posToCol(pos float64, width int) int {
	if width <= 1 {
		return 0
	}
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	col := int(math.Round(pos * float64(width-1)))
	if col < 0 {
		return 0
	}
	if col >= width {
		return width - 1
	}
	return col
}

// RenderPlot renders the 2-row (or Height-row) Unicode block histogram and geometric baseline rail.
// Row 0 contains log-scaled histogram bars, subtle grid ticks, optional A<->B span background,
// and non-overlapping inline badges. Row 1 (bottom row) contains the baseline rail and commit nodes.
func (BlockPlotRenderer) RenderPlot(spec TimelinePlotSpec) (rows []string, colToCommit map[int]int) {
	colToCommit = make(map[int]int)
	if spec.Width <= 0 {
		return nil, colToCommit
	}
	h := spec.Height
	if h <= 0 {
		h = 2
	}

	// Aggregate points by column so dense clusters render deterministically.
	buckets := make(map[int]*plotColumnBucket, len(spec.Points))
	orderedCols := make([]int, 0, len(spec.Points))
	maxMag := 0

	for _, p := range spec.Points {
		col := posToCol(p.Pos, spec.Width)
		cSize := p.ClusterSize
		if cSize < 1 {
			cSize = 1
		}
		mag := p.Magnitude
		if mag < 0 {
			mag = 0
		}

		pShort := shortCommitID(p.Commit)
		b, exists := buckets[col]
		if !exists {
			b = &plotColumnBucket{
				col:         col,
				rep:         p,
				totalMag:    mag,
				clusterSize: cSize,
				isSelected:  p.IsSelected,
				isHead:      p.IsHead,
				isA:         p.IsA,
				isB:         p.IsB,
				isRestored:  p.Commit.IsRestored,
				isStartup:   p.Commit.IsStartup,
				dominant:    p.Dominant,
			}
			if p.IsA {
				b.aSHA = pShort
			}
			if p.IsB {
				b.bSHA = pShort
			}
			if p.IsSelected {
				b.selectedSHA = pShort
			}
			buckets[col] = b
			orderedCols = append(orderedCols, col)
		} else {
			b.totalMag += mag
			b.clusterSize += cSize
			b.isSelected = b.isSelected || p.IsSelected
			b.isHead = b.isHead || p.IsHead
			b.isA = b.isA || p.IsA
			b.isB = b.isB || p.IsB
			b.isRestored = b.isRestored || p.Commit.IsRestored
			b.isStartup = b.isStartup || p.Commit.IsStartup
			if p.IsA && pShort != "" {
				b.aSHA = pShort
			}
			if p.IsB && pShort != "" {
				b.bSHA = pShort
			}
			if p.IsSelected && pShort != "" {
				b.selectedSHA = pShort
			}

			newPrio := plotPointPriority(p)
			oldPrio := plotPointPriority(b.rep)
			if newPrio > oldPrio ||
				(newPrio == oldPrio && p.Magnitude > b.rep.Magnitude) ||
				(newPrio == oldPrio && p.Magnitude == b.rep.Magnitude && p.CommitIndex < b.rep.CommitIndex) {
				b.rep = p
				b.dominant = p.Dominant
			}
		}
	}

	for _, col := range orderedCols {
		b := buckets[col]
		colToCommit[col] = b.rep.CommitIndex
		if b.isRestored {
			b.dominant = "restore"
		}
		if b.totalMag > maxMag {
			maxMag = b.totalMag
		}
	}

	// Determine A<->B span highlight column range if both endpoints exist and intersect [0..1].
	hasSpan := false
	spanStartCol, spanEndCol := -1, -1
	if spec.SpanA != nil && spec.SpanB != nil {
		lo, hi := *spec.SpanA, *spec.SpanB
		if lo > hi {
			lo, hi = hi, lo
		}
		if hi >= 0 && lo <= 1 {
			hasSpan = true
			spanStartCol = posToCol(lo, spec.Width)
			spanEndCol = posToCol(hi, spec.Width)
		}
	}

	// Determine active brush selection highlight column range [brushLo..brushHi].
	hasBrush := spec.BrushStart >= 0 && spec.BrushEnd >= 0
	brushLo, brushHi := spec.BrushStart, spec.BrushEnd
	if hasBrush {
		if brushLo > brushHi {
			brushLo, brushHi = brushHi, brushLo
		}
		if brushLo < 0 {
			brushLo = 0
		}
		if brushHi >= spec.Width {
			brushHi = spec.Width - 1
		}
		if brushLo > brushHi {
			hasBrush = false
		}
	}

	inBrush := func(x int) bool {
		return hasBrush && x >= brushLo && x <= brushHi
	}
	isBrushEdge := func(x int) bool {
		return hasBrush && (x == brushLo || x == brushHi)
	}
	inSpan := func(x int) bool {
		return hasSpan && x >= spanStartCol && x <= spanEndCol
	}

	// Build Row 1 (baseline rail + geometric commit nodes) using run-length grouped styles.
	railBaseStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(ColorBackground)
	railSpanStyle := lipgloss.NewStyle().Foreground(ColorActive).Background(ColorBackground).Bold(true)
	railBrushStyle := lipgloss.NewStyle().Foreground(ColorActive).Background(ColorHighlight).Bold(true)
	railBrushEdgeStyle := lipgloss.NewStyle().Foreground(ColorOnAccent).Background(ColorPrimary).Bold(true)
	railOverrides := make(map[int]string, len(orderedCols))

	for _, col := range orderedCols {
		b := buckets[col]
		railBg := ColorBackground
		if inBrush(col) {
			railBg = ColorHighlight
		} else if inSpan(col) {
			railBg = ColorDiffHunkBg
		}
		var glyph string
		var st lipgloss.Style
		switch {
		case b.isA:
			glyph = "A"
			st = lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorPrimary)
		case b.isB:
			glyph = "B"
			st = lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorSuccessBg)
		case b.isRestored:
			glyph = GlyphMilestone
			st = lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Background(railBg)
		case b.isSelected:
			glyph = GlyphSelected
			if spec.Focused {
				st = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(railBg)
			} else {
				st = lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(railBg)
			}
		case b.isHead:
			glyph = GlyphLive
			st = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess).Background(railBg)
		case b.isStartup:
			glyph = GlyphEmpty
			st = lipgloss.NewStyle().Foreground(ColorMuted).Background(railBg)
		default:
			glyph = GlyphPaused
			st = lipgloss.NewStyle().Foreground(ColorMuted).Background(railBg)
		}
		railOverrides[col] = st.Render(glyph)
	}

	var railBuf strings.Builder
	for x := 0; x < spec.Width; {
		if ov, ok := railOverrides[x]; ok {
			railBuf.WriteString(ov)
			x++
			continue
		}
		if isBrushEdge(x) {
			if x == brushLo {
				railBuf.WriteString(railBrushEdgeStyle.Render("◄"))
			} else {
				railBuf.WriteString(railBrushEdgeStyle.Render("►"))
			}
			x++
			continue
		}
		brushMode := inBrush(x)
		spanMode := !brushMode && inSpan(x)
		start := x
		for x < spec.Width {
			if _, ok := railOverrides[x]; ok || isBrushEdge(x) || inBrush(x) != brushMode || (!brushMode && inSpan(x) != spanMode) {
				break
			}
			x++
		}
		switch {
		case brushMode:
			railBuf.WriteString(railBrushStyle.Render(strings.Repeat("━", x-start)))
		case spanMode:
			railBuf.WriteString(railSpanStyle.Render(strings.Repeat("━", x-start)))
		default:
			railBuf.WriteString(railBaseStyle.Render(strings.Repeat("─", x-start)))
		}
	}
	railRow := PaintBarLine(railBuf.String(), spec.Width, ColorBackground, ColorText)

	if h == 1 {
		return []string{railRow}, colToCommit
	}

	// Build Row 0 (log-scaled histogram bars + subtle grid ticks + span tint + inline badges)
	// using run-length grouped background spaces so we emit ~10 SGR runs instead of Width runs.
	barCols := make([]bool, spec.Width)
	occupied := make([]bool, spec.Width)
	histOverrides := make(map[int]string, len(orderedCols)+12)
	histSpans := make(map[int]int, 12) // col -> width (>1 for multi-col badges)

	gridCols := computeGridTickCols(spec.Width)

	scaleMax := float64(maxMag)
	if scaleMax < 8 {
		scaleMax = 8
	}
	denom := math.Log1p(scaleMax)

	for _, col := range orderedCols {
		b := buckets[col]
		level := 0
		if b.totalMag > 0 && denom > 0 {
			ratio := math.Log1p(float64(b.totalMag)) / denom
			level = int(math.Round(ratio * 7.0))
			if level < 1 {
				level = 1
			}
			if level > 7 {
				level = 7
			}
		}
		barGlyph := ribbonBlockLevels[level]
		fg := dominantBarColor(b.dominant)
		cellBg := ColorBackground
		if inBrush(col) {
			cellBg = ColorHighlight
		} else if inSpan(col) {
			cellBg = ColorDiffHunkBg
		}
		st := lipgloss.NewStyle().Foreground(fg).Background(cellBg)
		if b.isSelected {
			st = st.Bold(true)
		}
		histOverrides[col] = st.Render(barGlyph)
		barCols[col] = true
		occupied[col] = true
	}

	// Place prioritized non-overlapping inline badges on Row 0 without overwriting bars.
	type inlineBadge struct {
		anchorCol int
		priority  int
		width     int
		rendered  string
	}

	var candidates []inlineBadge
	for _, col := range orderedCols {
		b := buckets[col]

		if b.isA && b.aSHA != "" {
			txt := "A:" + b.aSHA
			styled := lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorPrimary).Render(txt)
			candidates = append(candidates, inlineBadge{anchorCol: col, priority: 100, width: ansi.PrintableRuneWidth(txt), rendered: styled})
		}
		if b.isB && b.bSHA != "" {
			txt := "B:" + b.bSHA
			styled := lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorSuccessBg).Render(txt)
			candidates = append(candidates, inlineBadge{anchorCol: col, priority: 95, width: ansi.PrintableRuneWidth(txt), rendered: styled})
		}
		if b.isSelected && b.selectedSHA != "" && b.selectedSHA != b.aSHA && b.selectedSHA != b.bSHA {
			styled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorHighlight).Render(b.selectedSHA)
			candidates = append(candidates, inlineBadge{anchorCol: col, priority: 90, width: ansi.PrintableRuneWidth(b.selectedSHA), rendered: styled})
		}
		if b.isHead {
			txt := "HEAD"
			styled := lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorSuccessBg).Render(txt)
			candidates = append(candidates, inlineBadge{anchorCol: col, priority: 80, width: ansi.PrintableRuneWidth(txt), rendered: styled})
		}
		if b.isRestored {
			txt := "RSTR"
			styled := lipgloss.NewStyle().Bold(true).Foreground(ColorOnWarning).Background(ColorWarningBg).Render(txt)
			candidates = append(candidates, inlineBadge{anchorCol: col, priority: 70, width: ansi.PrintableRuneWidth(txt), rendered: styled})
		}
		if b.clusterSize > 1 {
			txt := fmt.Sprintf("×%d", b.clusterSize)
			styled := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorBadgeBg).Render(txt)
			candidates = append(candidates, inlineBadge{anchorCol: col, priority: 50, width: ansi.PrintableRuneWidth(txt), rendered: styled})
		}
	}

	// Deterministic insertion sort by descending priority, then ascending anchorCol.
	for i := 1; i < len(candidates); i++ {
		cur := candidates[i]
		j := i - 1
		for j >= 0 && (candidates[j].priority < cur.priority ||
			(candidates[j].priority == cur.priority && candidates[j].anchorCol > cur.anchorCol)) {
			candidates[j+1] = candidates[j]
			j--
		}
		candidates[j+1] = cur
	}

	canPlace := func(start, width int, requireGap bool) bool {
		if start < 0 || start+width > spec.Width {
			return false
		}
		for x := start; x < start+width; x++ {
			if occupied[x] {
				return false
			}
		}
		if requireGap {
			if start > 0 && occupied[start-1] {
				return false
			}
			if start+width < spec.Width && occupied[start+width] {
				return false
			}
		}
		return true
	}

	findBadgeSlot := func(anchor, width int) int {
		const maxScan = 24
		// Helper to check no other commit bar sits between anchor and candidate range.
		rightLimit := minInt(spec.Width-width, anchor+maxScan)
		leftLimit := maxInt(0, anchor-maxScan-width)

		// Try with 1-col breathing gap first, then tight placement if needed.
		for _, requireGap := range []bool{true, false} {
			// Scan right from anchor without crossing any other commit bar.
			for sx := anchor + 1; sx <= rightLimit; sx++ {
				if sx+width-1 >= spec.Width {
					break
				}
				crossedBar := false
				for k := anchor + 1; k < sx+width; k++ {
					if barCols[k] {
						crossedBar = true
						break
					}
				}
				if crossedBar {
					break
				}
				if canPlace(sx, width, requireGap) {
					return sx
				}
			}
			// Scan left from anchor without crossing any other commit bar.
			for sx := anchor - width; sx >= leftLimit; sx-- {
				if sx < 0 {
					break
				}
				crossedBar := false
				for k := sx; k < anchor; k++ {
					if barCols[k] {
						crossedBar = true
						break
					}
				}
				if crossedBar {
					break
				}
				if canPlace(sx, width, requireGap) {
					return sx
				}
			}
		}
		return -1
	}

	for _, badge := range candidates {
		if badge.width <= 0 || badge.width >= spec.Width {
			continue
		}
		if placed := findBadgeSlot(badge.anchorCol, badge.width); placed >= 0 {
			histOverrides[placed] = badge.rendered
			histSpans[placed] = badge.width
			for x := placed; x < placed+badge.width; x++ {
				occupied[x] = true
			}
		}
	}

	bgSpaceStyle := lipgloss.NewStyle().Background(ColorBackground)
	spanSpaceStyle := lipgloss.NewStyle().Background(ColorDiffHunkBg)
	brushSpaceStyle := lipgloss.NewStyle().Background(ColorHighlight)
	brushEdgeStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorHighlight)
	gridBgStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(ColorBackground)
	gridSpanStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(ColorDiffHunkBg)
	gridBrushStyle := lipgloss.NewStyle().Foreground(ColorActive).Background(ColorHighlight)

	var histBuf strings.Builder
	for x := 0; x < spec.Width; {
		if ov, ok := histOverrides[x]; ok {
			histBuf.WriteString(ov)
			step := histSpans[x]
			if step < 1 {
				step = 1
			}
			x += step
			continue
		}
		if isBrushEdge(x) {
			histBuf.WriteString(brushEdgeStyle.Render("│"))
			x++
			continue
		}
		if gridCols[x] {
			switch {
			case inBrush(x):
				histBuf.WriteString(gridBrushStyle.Render("┆"))
			case inSpan(x):
				histBuf.WriteString(gridSpanStyle.Render("┆"))
			default:
				histBuf.WriteString(gridBgStyle.Render("┆"))
			}
			x++
			continue
		}
		brushMode := inBrush(x)
		spanMode := !brushMode && inSpan(x)
		start := x
		for x < spec.Width {
			if _, ok := histOverrides[x]; ok || isBrushEdge(x) || gridCols[x] || inBrush(x) != brushMode || (!brushMode && inSpan(x) != spanMode) {
				break
			}
			x++
		}
		switch {
		case brushMode:
			histBuf.WriteString(brushSpaceStyle.Render(strings.Repeat(" ", x-start)))
		case spanMode:
			histBuf.WriteString(spanSpaceStyle.Render(strings.Repeat(" ", x-start)))
		default:
			histBuf.WriteString(bgSpaceStyle.Render(strings.Repeat(" ", x-start)))
		}
	}

	histRow := PaintBarLine(histBuf.String(), spec.Width, ColorBackground, ColorText)

	rows = make([]string, 0, h)
	for i := 0; i < h-2; i++ {
		rows = append(rows, PaintBarLine("", spec.Width, ColorBackground, ColorText))
	}
	rows = append(rows, histRow, railRow)
	return rows, colToCommit
}

func computeGridTickCols(width int) map[int]bool {
	ticks := make(map[int]bool)
	if width < 16 {
		return ticks
	}
	stepCount := 4
	if width < 48 {
		stepCount = 2
	}
	for i := 1; i < stepCount; i++ {
		col := int(math.Round(float64(i) / float64(stepCount) * float64(width-1)))
		if col > 0 && col < width-1 {
			ticks[col] = true
		}
	}
	return ticks
}

func dominantBarColor(dominant string) lipgloss.Color {
	switch dominant {
	case "add":
		return ColorSuccess
	case "mod", "restore":
		return ColorWarning
	case "del":
		return ColorDanger
	case "init":
		return ColorMuted
	default:
		return ColorActive
	}
}

func shortCommitID(c models.TimelineCommit) string {
	id := strings.TrimSpace(c.CommitID)
	if id == "" {
		id = strings.TrimSpace(c.FullSHA)
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

// parseDiffStat parses DiffStat strings like "+3 / ~1 / -2" or "+12 -4" into total magnitude
// and dominant change category ("add", "mod", "del", "restore", "init").
func parseDiffStat(c models.TimelineCommit) (magnitude int, dominant string) {
	adds, mods, dels := 0, 0, 0
	s := strings.TrimSpace(c.DiffStat)
	if s != "" && s != "-" && !strings.EqualFold(s, "no changes") && !strings.EqualFold(s, "identical") {
		cleaned := strings.NewReplacer("/", " ", ",", " ").Replace(s)
		for _, tok := range strings.Fields(cleaned) {
			if len(tok) < 2 {
				continue
			}
			prefix := tok[0]
			if prefix != '+' && prefix != '~' && prefix != '-' {
				continue
			}
			digits := tok[1:]
			// Strip trailing non-digits (e.g. "+12lines")
			end := 0
			for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
				end++
			}
			if end == 0 {
				continue
			}
			n, err := strconv.Atoi(digits[:end])
			if err != nil || n < 0 {
				continue
			}
			switch prefix {
			case '+':
				adds += n
			case '~':
				mods += n
			case '-':
				dels += n
			}
		}
	}

	magnitude = adds + mods + dels
	switch {
	case c.IsRestored:
		dominant = "restore"
	case c.IsStartup:
		dominant = "init"
	case magnitude == 0:
		dominant = "init"
	case adds >= mods && adds >= dels:
		dominant = "add"
	case mods >= dels:
		dominant = "mod"
	default:
		dominant = "del"
	}
	return magnitude, dominant
}

// TimelineRibbonModel implements the Focus+Context horizontal Visual Timeline Ribbon.
// All commits are stored newest-first to match GitBackend.GetTimeline.
type TimelineRibbonModel struct {
	AllCommits      []models.TimelineCommit // already path-filtered commits (newest-first)
	WindowCommits   []models.TimelineCommit // commits inside [WindowStart..WindowEnd] (newest-first)
	FullMinTime     time.Time
	FullMaxTime     time.Time
	WindowStart     time.Time
	WindowEnd       time.Time
	ActivePreset    string // "all" (default), "7d", "24h", "1h", "fit", "" (custom)
	FilterQuery     string
	TotalUnfiltered int
	SelectedSHA     string
	HeadSHA         string
	LeftSHA         string // A (older) when in split/compare
	RightSHA        string // B (newer) when in split/compare
	SplitCompare    bool
	Collapsed       bool
	IsFocused       bool
	Width           int
	Renderer        TimelinePlotRenderer

	// Drag state for overview window scrubber & direct timeline brush selection
	DragMode      string // "", "left", "right", "pan", "brush", "overview_brush"
	dragAnchorX   int
	brushCurrentX int
	brushDragged  bool
	dragOrigStart time.Time
	dragOrigEnd   time.Time

	// Double-click progressive zoom tracking
	lastClickAt   time.Time
	lastClickX    int
	lastClickArea string // "plot" or "overview"
	nowFunc       func() time.Time
}

// NewTimelineRibbonModel constructs a TimelineRibbonModel with default preset "all"
// and BlockPlotRenderer.
func NewTimelineRibbonModel(width int) TimelineRibbonModel {
	return TimelineRibbonModel{
		AllCommits:    []models.TimelineCommit{},
		WindowCommits: []models.TimelineCommit{},
		ActivePreset:  "all",
		Width:         width,
		Renderer:      BlockPlotRenderer{},
	}
}

// OuterHeight returns 6 when expanded (and Width >= 20), or 1 when Collapsed (or narrow < 20).
func (m TimelineRibbonModel) OuterHeight() int {
	if m.Collapsed || m.Width < 20 {
		return 1
	}
	return 6
}

// SetWidth updates the outer width of the ribbon.
func (m *TimelineRibbonModel) SetWidth(width int) {
	m.Width = width
}

// SetCommits updates the filtered commit list, computes full history bounds,
// applies the active preset (or clamps a custom window), and refreshes WindowCommits.
func (m *TimelineRibbonModel) SetCommits(filteredCommits []models.TimelineCommit, totalUnfiltered int, filterQuery string) {
	prevFullMax := m.FullMaxTime
	prevFullSpan := m.FullMaxTime.Sub(m.FullMinTime)

	m.AllCommits = make([]models.TimelineCommit, len(filteredCommits))
	copy(m.AllCommits, filteredCommits)
	m.TotalUnfiltered = totalUnfiltered
	m.FilterQuery = filterQuery

	switch len(m.AllCommits) {
	case 0:
		now := time.Now().UTC()
		m.FullMinTime = now.Add(-1 * time.Hour)
		m.FullMaxTime = now
	default:
		minT := m.AllCommits[0].Timestamp
		maxT := m.AllCommits[0].Timestamp
		for _, c := range m.AllCommits[1:] {
			if c.Timestamp.Before(minT) {
				minT = c.Timestamp
			}
			if c.Timestamp.After(maxT) {
				maxT = c.Timestamp
			}
		}
		if !maxT.After(minT) {
			// Single commit (or identical timestamps): pad symmetrically so the node sits centered.
			m.FullMinTime = minT.Add(-30 * time.Minute)
			m.FullMaxTime = maxT.Add(30 * time.Minute)
		} else {
			m.FullMinTime = minT
			m.FullMaxTime = maxT
		}
	}

	if m.ActivePreset == "" && !m.WindowStart.IsZero() && !m.WindowEnd.IsZero() && m.WindowEnd.After(m.WindowStart) {
		// If the user's custom window right edge was pinned to the previous live edge,
		// slide WindowEnd forward automatically so newly recorded commits appear immediately.
		if !prevFullMax.IsZero() && m.FullMaxTime.After(prevFullMax) {
			edgeTol := 2 * time.Second
			if pct := time.Duration(float64(prevFullSpan) * 0.01); pct > edgeTol {
				edgeTol = pct
			}
			if prevFullMax.Sub(m.WindowEnd) <= edgeTol {
				m.WindowEnd = m.FullMaxTime
			}
		}
		m.clampCustomWindowToBounds()
		m.recomputeWindowCommits()
		return
	}

	preset := m.ActivePreset
	if preset == "" {
		preset = "all"
	}
	m.applyPreset(preset)
}

// EnsureTimeVisible expands a custom zoomed window's right edge (if needed) so a newly
// recorded commit at ts is immediately visible in WindowCommits and the Left Panel.
func (m *TimelineRibbonModel) EnsureTimeVisible(ts time.Time) {
	if ts.IsZero() || m.ActivePreset != "" {
		return
	}
	changed := false
	if ts.After(m.WindowEnd) {
		m.WindowEnd = ts
		if !m.FullMaxTime.IsZero() && m.WindowEnd.After(m.FullMaxTime) {
			m.WindowEnd = m.FullMaxTime
		}
		changed = true
	}
	if ts.Before(m.WindowStart) {
		m.WindowStart = ts
		if !m.FullMinTime.IsZero() && m.WindowStart.Before(m.FullMinTime) {
			m.WindowStart = m.FullMinTime
		}
		changed = true
	}
	if changed {
		m.recomputeWindowCommits()
	}
}

func (m *TimelineRibbonModel) minWindowSpan() time.Duration {
	fullSpan := m.FullMaxTime.Sub(m.FullMinTime)
	minSpan := 1 * time.Second
	if fullSpan > 0 && fullSpan < minSpan {
		return fullSpan
	}
	return minSpan
}

func (m *TimelineRibbonModel) clampCustomWindowToBounds() {
	if m.FullMinTime.IsZero() || m.FullMaxTime.IsZero() || !m.FullMaxTime.After(m.FullMinTime) {
		return
	}
	if m.WindowStart.Before(m.FullMinTime) {
		m.WindowStart = m.FullMinTime
	}
	if m.WindowEnd.After(m.FullMaxTime) {
		m.WindowEnd = m.FullMaxTime
	}
	minSpan := m.minWindowSpan()
	if !m.WindowEnd.After(m.WindowStart) || m.WindowEnd.Sub(m.WindowStart) < minSpan {
		// If the preserved window fell outside the new repo bounds, reset cleanly to full span.
		if m.WindowStart.After(m.FullMaxTime) || m.WindowEnd.Before(m.FullMinTime) {
			m.WindowStart = m.FullMinTime
			m.WindowEnd = m.FullMaxTime
			return
		}
		m.WindowEnd = m.WindowStart.Add(minSpan)
		if m.WindowEnd.After(m.FullMaxTime) {
			m.WindowEnd = m.FullMaxTime
			m.WindowStart = m.WindowEnd.Add(-minSpan)
			if m.WindowStart.Before(m.FullMinTime) {
				m.WindowStart = m.FullMinTime
			}
		}
	}
}

func (m *TimelineRibbonModel) applyPreset(preset string) {
	norm := strings.ToLower(strings.TrimSpace(preset))
	switch norm {
	case "fit":
		m.FitToMatches()
		return
	case "1h":
		m.setTrailingDuration("1h", 1*time.Hour)
	case "24h":
		m.setTrailingDuration("24h", 24*time.Hour)
	case "7d":
		m.setTrailingDuration("7d", 7*24*time.Hour)
	default:
		m.ActivePreset = "all"
		m.WindowStart = m.FullMinTime
		m.WindowEnd = m.FullMaxTime
		m.recomputeWindowCommits()
	}
}

func (m *TimelineRibbonModel) setTrailingDuration(name string, dur time.Duration) {
	m.ActivePreset = name
	end := m.FullMaxTime
	start := end.Add(-dur)
	if start.Before(m.FullMinTime) {
		start = m.FullMinTime
	}
	if !end.After(start) {
		end = m.FullMaxTime
		start = m.FullMinTime
	}
	m.WindowStart = start
	m.WindowEnd = end
	m.recomputeWindowCommits()
}

func (m *TimelineRibbonModel) recomputeWindowCommits() {
	if len(m.AllCommits) == 0 {
		m.WindowCommits = []models.TimelineCommit{}
		return
	}
	out := make([]models.TimelineCommit, 0, len(m.AllCommits))
	for _, c := range m.AllCommits {
		if !c.Timestamp.Before(m.WindowStart) && !c.Timestamp.After(m.WindowEnd) {
			out = append(out, c)
		}
	}
	m.WindowCommits = out
}

// SetPreset activates a named time window preset ("all", "7d", "24h", "1h", "fit").
func (m *TimelineRibbonModel) SetPreset(preset string) {
	m.applyPreset(preset)
}

// FitToMatches tightly fits WindowStart..WindowEnd to AllCommits with 3% padding on each side.
func (m *TimelineRibbonModel) FitToMatches() {
	m.ActivePreset = "fit"
	if len(m.AllCommits) == 0 {
		m.WindowStart = m.FullMinTime
		m.WindowEnd = m.FullMaxTime
		m.recomputeWindowCommits()
		return
	}

	minT := m.AllCommits[0].Timestamp
	maxT := m.AllCommits[0].Timestamp
	for _, c := range m.AllCommits[1:] {
		if c.Timestamp.Before(minT) {
			minT = c.Timestamp
		}
		if c.Timestamp.After(maxT) {
			maxT = c.Timestamp
		}
	}

	if !maxT.After(minT) {
		m.WindowStart = minT.Add(-30 * time.Minute)
		m.WindowEnd = maxT.Add(30 * time.Minute)
	} else {
		span := maxT.Sub(minT)
		pad := time.Duration(float64(span) * 0.03)
		if pad < 1*time.Second {
			pad = 1 * time.Second
		}
		m.WindowStart = minT.Add(-pad)
		m.WindowEnd = maxT.Add(pad)
	}
	m.recomputeWindowCommits()
}

// NudgeStart moves the left (older) window edge by deltaFrac of the current window span.
// Sign convention: positive moves start right (later in time / narrows window),
// negative moves start left (earlier in time / widens window). Clamped to [FullMinTime, WindowEnd-minSpan].
func (m *TimelineRibbonModel) NudgeStart(deltaFrac float64) {
	span := m.WindowEnd.Sub(m.WindowStart)
	if span <= 0 {
		span = m.FullMaxTime.Sub(m.FullMinTime)
	}
	if span <= 0 {
		return
	}
	minSpan := m.minWindowSpan()
	newStart := m.WindowStart.Add(time.Duration(float64(span) * deltaFrac))
	if newStart.Before(m.FullMinTime) {
		newStart = m.FullMinTime
	}
	maxStart := m.WindowEnd.Add(-minSpan)
	if newStart.After(maxStart) {
		newStart = maxStart
	}
	if !newStart.Equal(m.WindowStart) {
		m.WindowStart = newStart
		m.ActivePreset = ""
		m.recomputeWindowCommits()
	}
}

// NudgeEnd moves the right (newer) window edge by deltaFrac of the current window span.
// Sign convention: positive moves end right (later in time / widens window),
// negative moves end left (earlier in time / narrows window). Clamped to [WindowStart+minSpan, FullMaxTime].
func (m *TimelineRibbonModel) NudgeEnd(deltaFrac float64) {
	span := m.WindowEnd.Sub(m.WindowStart)
	if span <= 0 {
		span = m.FullMaxTime.Sub(m.FullMinTime)
	}
	if span <= 0 {
		return
	}
	minSpan := m.minWindowSpan()
	newEnd := m.WindowEnd.Add(time.Duration(float64(span) * deltaFrac))
	if newEnd.After(m.FullMaxTime) {
		newEnd = m.FullMaxTime
	}
	minEnd := m.WindowStart.Add(minSpan)
	if newEnd.Before(minEnd) {
		newEnd = minEnd
	}
	if !newEnd.Equal(m.WindowEnd) {
		m.WindowEnd = newEnd
		m.ActivePreset = ""
		m.recomputeWindowCommits()
	}
}

// PanWindow shifts both WindowStart and WindowEnd by deltaFrac of the current window span
// while strictly preserving the window duration. Positive shifts later (right), negative earlier (left).
func (m *TimelineRibbonModel) PanWindow(deltaFrac float64) {
	span := m.WindowEnd.Sub(m.WindowStart)
	if span <= 0 {
		return
	}
	delta := time.Duration(float64(span) * deltaFrac)
	if delta == 0 {
		return
	}
	newStart := m.WindowStart.Add(delta)
	newEnd := m.WindowEnd.Add(delta)

	if newStart.Before(m.FullMinTime) {
		newStart = m.FullMinTime
		newEnd = newStart.Add(span)
		if newEnd.After(m.FullMaxTime) {
			newEnd = m.FullMaxTime
		}
	} else if newEnd.After(m.FullMaxTime) {
		newEnd = m.FullMaxTime
		newStart = newEnd.Add(-span)
		if newStart.Before(m.FullMinTime) {
			newStart = m.FullMinTime
		}
	}

	if !newStart.Equal(m.WindowStart) || !newEnd.Equal(m.WindowEnd) {
		m.WindowStart = newStart
		m.WindowEnd = newEnd
		m.ActivePreset = ""
		m.recomputeWindowCommits()
	}
}

// ZoomAround scales the active window around centerTime by factor.
// factor < 1 zooms in (narrows window); factor > 1 zooms out (widens window).
func (m *TimelineRibbonModel) ZoomAround(centerTime time.Time, factor float64) {
	if factor <= 0 {
		return
	}
	curSpan := m.WindowEnd.Sub(m.WindowStart)
	fullSpan := m.FullMaxTime.Sub(m.FullMinTime)
	if curSpan <= 0 || fullSpan <= 0 {
		return
	}

	if centerTime.IsZero() {
		centerTime = m.WindowStart.Add(curSpan / 2)
	}
	if centerTime.Before(m.WindowStart) {
		centerTime = m.WindowStart
	}
	if centerTime.After(m.WindowEnd) {
		centerTime = m.WindowEnd
	}

	ratio := float64(centerTime.Sub(m.WindowStart)) / float64(curSpan)
	minSpan := m.minWindowSpan()
	newSpan := time.Duration(float64(curSpan) * factor)
	if newSpan < minSpan {
		newSpan = minSpan
	}
	if newSpan > fullSpan {
		newSpan = fullSpan
	}

	newStart := centerTime.Add(-time.Duration(float64(newSpan) * ratio))
	newEnd := newStart.Add(newSpan)

	if newStart.Before(m.FullMinTime) {
		newStart = m.FullMinTime
		newEnd = newStart.Add(newSpan)
	}
	if newEnd.After(m.FullMaxTime) {
		newEnd = m.FullMaxTime
		newStart = newEnd.Add(-newSpan)
	}
	if newStart.Before(m.FullMinTime) {
		newStart = m.FullMinTime
	}

	if !newStart.Equal(m.WindowStart) || !newEnd.Equal(m.WindowEnd) {
		m.WindowStart = newStart
		m.WindowEnd = newEnd
		m.ActivePreset = ""
		m.recomputeWindowCommits()
	}
}

// ResetWindow resets the window to cover the full history ("all" preset).
func (m *TimelineRibbonModel) ResetWindow() {
	m.SetPreset("all")
}

// ToggleCollapsed toggles between the 6-row expanded ribbon and 1-row compact status strip.
func (m *TimelineRibbonModel) ToggleCollapsed() {
	m.Collapsed = !m.Collapsed
}

// SelectPrevInWindow steps selection one commit chronologically LEFT (older) within WindowCommits,
// updating SelectedSHA and returning the newly selected FullSHA (or "" if window is empty).
func (m *TimelineRibbonModel) SelectPrevInWindow() string {
	if len(m.WindowCommits) == 0 {
		return ""
	}
	idx := m.findSelectedIndexInWindow()
	if idx < 0 {
		idx = 0
	} else if idx+1 < len(m.WindowCommits) {
		// WindowCommits is newest-first: higher index = older = left on horizontal axis.
		idx++
	}
	m.SelectedSHA = m.WindowCommits[idx].FullSHA
	return m.SelectedSHA
}

// SelectNextInWindow steps selection one commit chronologically RIGHT (newer) within WindowCommits,
// updating SelectedSHA and returning the newly selected FullSHA (or "" if window is empty).
func (m *TimelineRibbonModel) SelectNextInWindow() string {
	if len(m.WindowCommits) == 0 {
		return ""
	}
	idx := m.findSelectedIndexInWindow()
	if idx < 0 {
		idx = len(m.WindowCommits) - 1
	} else if idx-1 >= 0 {
		// WindowCommits is newest-first: lower index = newer = right on horizontal axis.
		idx--
	}
	m.SelectedSHA = m.WindowCommits[idx].FullSHA
	return m.SelectedSHA
}

func (m TimelineRibbonModel) findSelectedIndexInWindow() int {
	if m.SelectedSHA == "" {
		return -1
	}
	for i, c := range m.WindowCommits {
		if c.FullSHA == m.SelectedSHA {
			return i
		}
	}
	return -1
}

type ribbonLayout struct {
	lines        []string
	topChips     []HitChip // exact [StartX, EndX) on Row 0 (outer coordinates)
	colToCommit  map[int]int
	trackStartX  int
	trackW       int
	leftHandleX  int
	rightHandleX int
}

func (m TimelineRibbonModel) buildPlotSpec(innerW int) TimelinePlotSpec {
	span := m.WindowEnd.Sub(m.WindowStart)
	points := make([]TimelinePlotPoint, 0, len(m.WindowCommits))

	for i, c := range m.WindowCommits {
		pos := 0.5
		if span > 0 {
			pos = float64(c.Timestamp.Sub(m.WindowStart)) / float64(span)
			if pos < 0 {
				pos = 0
			}
			if pos > 1 {
				pos = 1
			}
		}
		mag, dom := parseDiffStat(c)
		isHead := false
		if m.HeadSHA != "" {
			isHead = c.FullSHA == m.HeadSHA
		} else {
			isHead = c.IsCurrent
		}
		isSelected := m.SelectedSHA != "" && c.FullSHA == m.SelectedSHA
		isA := m.SplitCompare && m.LeftSHA != "" && c.FullSHA == m.LeftSHA
		isB := m.SplitCompare && m.RightSHA != "" && c.FullSHA == m.RightSHA

		points = append(points, TimelinePlotPoint{
			CommitIndex: i,
			Commit:      c,
			Pos:         pos,
			Magnitude:   mag,
			Dominant:    dom,
			IsSelected:  isSelected,
			IsHead:      isHead,
			IsA:         isA,
			IsB:         isB,
			ClusterSize: 1,
		})
	}

	var spanA, spanB *float64
	if m.SplitCompare && m.LeftSHA != "" && m.RightSHA != "" && span > 0 {
		tA, okA := m.findCommitTime(m.LeftSHA)
		tB, okB := m.findCommitTime(m.RightSHA)
		if !okB && m.RightSHA == "__LIVE_RUNNING__" {
			tB = m.FullMaxTime
			okB = !tB.IsZero()
		}
		if okA && okB {
			pA := float64(tA.Sub(m.WindowStart)) / float64(span)
			pB := float64(tB.Sub(m.WindowStart)) / float64(span)
			spanA = &pA
			spanB = &pB
		}
	}

	brushStart, brushEnd := -1, -1
	if lo, hi, active := m.activeBrushInnerCols(innerW); active {
		brushStart, brushEnd = lo, hi
	}

	return TimelinePlotSpec{
		Width:       innerW,
		Height:      2,
		WindowStart: m.WindowStart,
		WindowEnd:   m.WindowEnd,
		Points:      points,
		SpanA:       spanA,
		SpanB:       spanB,
		BrushStart:  brushStart,
		BrushEnd:    brushEnd,
		Focused:     m.IsFocused,
	}
}

func (m TimelineRibbonModel) findCommitTime(sha string) (time.Time, bool) {
	for _, c := range m.AllCommits {
		if c.FullSHA == sha {
			return c.Timestamp, true
		}
	}
	return time.Time{}, false
}

func (m TimelineRibbonModel) presetDisplayLabel() string {
	switch strings.ToLower(m.ActivePreset) {
	case "1h":
		return "1h"
	case "24h":
		return "24h"
	case "7d":
		return "7d"
	case "fit":
		return "Fit"
	case "all":
		return "All"
	default:
		return "Custom"
	}
}

func (m TimelineRibbonModel) countSummary() (winCount, totalCount int) {
	winCount = len(m.WindowCommits)
	totalCount = m.TotalUnfiltered
	if totalCount < len(m.AllCommits) {
		totalCount = len(m.AllCommits)
	}
	return winCount, totalCount
}

func (m TimelineRibbonModel) formatRangeShort() string {
	if m.WindowStart.IsZero() || m.WindowEnd.IsZero() {
		return ""
	}
	span := m.WindowEnd.Sub(m.WindowStart)
	var layout string
	switch {
	case span < 24*time.Hour:
		layout = "15:04"
	case span < 14*24*time.Hour:
		layout = "Jan 02 15:04"
	default:
		layout = "2006-01-02"
	}
	return m.WindowStart.Format(layout) + " → " + m.WindowEnd.Format(layout)
}

func (m TimelineRibbonModel) renderCollapsedLine() string {
	if m.Width <= 0 {
		return ""
	}
	bg := ColorSurface
	if m.IsFocused {
		bg = ColorSurfaceFocus
	}
	if m.Width < 12 {
		return PaintBarLine(GlyphCollapsed+" TIMELINE", m.Width, bg, ColorActive)
	}

	winN, totN := m.countSummary()
	presetBadge := lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorActive).
		Bold(true).
		Render("[" + m.presetDisplayLabel() + "]")
	expandHint := lipgloss.NewStyle().
		Background(ColorBadgeBg).
		Foreground(ColorMuted).
		Render("[w: expand]")

	sp := lipgloss.NewStyle().Background(bg).Render(" ")
	dot := lipgloss.NewStyle().Foreground(ColorTextDim).Background(bg).Render(" · ")

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Background(bg)
	if m.IsFocused {
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(bg)
	}
	leftHead := titleStyle.Render(GlyphCollapsed + " TIMELINE")
	countPart := lipgloss.NewStyle().Foreground(ColorText).Background(bg).Render(fmt.Sprintf("%d/%d commits", winN, totN))
	rangeStr := m.formatRangeShort()
	rangePart := ""
	if rangeStr != "" {
		rangePart = lipgloss.NewStyle().Foreground(ColorMuted).Background(bg).Render(rangeStr)
	}

	rightStr := presetBadge + sp + expandHint
	rightW := ansi.PrintableRuneWidth(rightStr)

	innerW := m.Width - 2 // 1-col padding on each side
	if innerW < 1 {
		return PaintBarLine(leftHead, m.Width, bg, ColorText)
	}

	if rightW+14 > innerW {
		// Narrow terminal: drop [w: expand] first, then preset badge if needed.
		rightStr = presetBadge
		rightW = ansi.PrintableRuneWidth(rightStr)
		if rightW+12 > innerW {
			rightStr = ""
			rightW = 0
		}
	}

	maxLeftW := innerW - rightW
	if rightW > 0 {
		maxLeftW-- // at least 1 space gap
	}
	if maxLeftW < 4 {
		maxLeftW = 4
	}

	// Build left progressively to fit maxLeftW cleanly.
	leftStr := leftHead
	if ansi.PrintableRuneWidth(leftStr+dot+countPart) <= maxLeftW {
		leftStr += dot + countPart
	} else {
		shortCount := lipgloss.NewStyle().Foreground(ColorText).Background(bg).Render(fmt.Sprintf("%d/%d", winN, totN))
		if ansi.PrintableRuneWidth(leftStr+dot+shortCount) <= maxLeftW {
			leftStr += dot + shortCount
		}
	}
	if rangePart != "" && ansi.PrintableRuneWidth(leftStr+dot+rangePart) <= maxLeftW {
		leftStr += dot + rangePart
	}
	if ansi.PrintableRuneWidth(leftStr) > maxLeftW {
		leftStr = truncate.String(leftStr, uint(maxLeftW))
	}

	leftW := ansi.PrintableRuneWidth(leftStr)
	gapW := innerW - leftW - rightW
	if gapW < 0 {
		gapW = 0
	}
	gap := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gapW))

	line := sp + leftStr + gap + rightStr + sp
	return PaintBarLine(line, m.Width, bg, ColorText)
}

type topChipSpec struct {
	action   string
	text     string
	active   bool
	collapse bool
}

func (m TimelineRibbonModel) buildTopBorder(borderFg lipgloss.Color) (string, []HitChip) {
	b := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(borderFg).Background(ColorBackground)

	winN, totN := m.countSummary()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Background(ColorBackground)
	if m.IsFocused {
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorBackground)
	}
	metaStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorBackground)

	// Candidate left titles from richest to most compact.
	titleCandidates := []string{
		titleStyle.Render("VISUAL TIMELINE") + metaStyle.Render(fmt.Sprintf(" · %d/%d commits", winN, totN)),
		titleStyle.Render("VISUAL TIMELINE") + metaStyle.Render(fmt.Sprintf(" · %d/%d", winN, totN)),
		titleStyle.Render("TIMELINE") + metaStyle.Render(fmt.Sprintf(" · %d/%d", winN, totN)),
		titleStyle.Render("TIMELINE"),
	}

	activePreset := strings.ToLower(m.ActivePreset)
	allChips := []topChipSpec{
		{action: "preset:1h", text: "[1h]", active: activePreset == "1h"},
		{action: "preset:24h", text: "[24h]", active: activePreset == "24h"},
		{action: "preset:7d", text: "[7d]", active: activePreset == "7d"},
		{action: "preset:all", text: "[All]", active: activePreset == "all"},
		{action: "preset:fit", text: "[Fit]", active: activePreset == "fit"},
		{action: "toggle_collapse", text: GlyphExpanded, collapse: true},
	}

	// Candidate chip sets from full to minimal (always keeping collapse toggle when possible).
	chipSets := [][]topChipSpec{
		allChips,
		{allChips[1], allChips[2], allChips[3], allChips[5]}, // 24h, 7d, All, ▾
		{allChips[3], allChips[5]},                           // All, ▾
		{allChips[5]},                                        // ▾
	}

	measureChips := func(cs []topChipSpec) int {
		w := 0
		for i, c := range cs {
			if i > 0 {
				w++
			}
			w += ansi.PrintableRuneWidth(c.text)
		}
		return w
	}

	chosenTitle := titleCandidates[len(titleCandidates)-1]
	var chosenChips []topChipSpec
	found := false

	// Prefer keeping all presets down to medium widths, shortening the title first.
	for _, cs := range chipSets {
		cw := measureChips(cs)
		for _, tc := range titleCandidates {
			tw := ansi.PrintableRuneWidth(tc)
			// ╭─ (2) + " " + title + " " (tw+2) + fill (>=1) + " " + chips + " " (cw+2) + ─╮ (2)
			if m.Width-8-tw-cw >= 1 {
				chosenTitle = tc
				chosenChips = cs
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		// Ultra-narrow fallback without right chips.
		tw := ansi.PrintableRuneWidth(chosenTitle)
		maxTW := m.Width - 6
		if maxTW < 1 {
			return PaintBarLine(b.TopLeft+strings.Repeat(b.Top, maxInt(0, m.Width-2))+b.TopRight, m.Width, ColorBackground, borderFg), nil
		}
		if tw > maxTW {
			chosenTitle = truncate.String(chosenTitle, uint(maxTW))
			tw = ansi.PrintableRuneWidth(chosenTitle)
		}
		fillW := m.Width - 5 - tw
		if fillW < 1 {
			fillW = 1
		}
		sp := lipgloss.NewStyle().Background(ColorBackground).Render(" ")
		raw := borderStyle.Render(b.TopLeft+b.Top) + sp + chosenTitle + sp + borderStyle.Render(strings.Repeat(b.Top, fillW)+b.TopRight)
		return PaintBarLine(raw, m.Width, ColorBackground, borderFg), nil
	}

	tw := ansi.PrintableRuneWidth(chosenTitle)
	cw := measureChips(chosenChips)
	fillW := m.Width - 8 - tw - cw
	if fillW < 1 {
		fillW = 1
	}

	sp := lipgloss.NewStyle().Background(ColorBackground).Render(" ")
	var chipParts []string
	var hitChips []HitChip

	// Exact starting column of first chip:
	// leftCorner(2) + sp(1) + title(tw) + sp(1) + fill(fillW) + sp(1) = 5 + tw + fillW
	cursorX := 5 + tw + fillW
	for i, c := range chosenChips {
		if i > 0 {
			chipParts = append(chipParts, sp)
			cursorX++
		}
		var st lipgloss.Style
		switch {
		case c.collapse:
			st = lipgloss.NewStyle().Foreground(ColorActive).Background(ColorBackground).Bold(true)
		case c.active:
			st = lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorPrimary)
		default:
			st = lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorBadgeBg)
		}
		rendered := st.Render(c.text)
		w := ansi.PrintableRuneWidth(c.text)
		hitChips = append(hitChips, HitChip{
			Label:  c.action,
			StartX: cursorX,
			EndX:   cursorX + w,
		})
		chipParts = append(chipParts, rendered)
		cursorX += w
	}

	raw := borderStyle.Render(b.TopLeft+b.Top) +
		sp + chosenTitle + sp +
		borderStyle.Render(strings.Repeat(b.Top, fillW)) +
		sp + strings.Join(chipParts, "") + sp +
		borderStyle.Render(b.Top+b.TopRight)

	return PaintBarLine(raw, m.Width, ColorBackground, borderFg), hitChips
}

func (m TimelineRibbonModel) renderAxisRow(innerW int) string {
	if innerW <= 0 {
		return ""
	}
	if startT, endT, ok := m.brushTimeBounds(innerW); ok {
		span := endT.Sub(startT)
		layout := "15:04"
		switch {
		case span < 10*time.Minute:
			layout = "15:04:05"
		case span >= 14*24*time.Hour:
			layout = "2006-01-02"
		case span >= 24*time.Hour:
			layout = "Jan 02 15:04"
		}
		txt := fmt.Sprintf(" ◆ Drag window: %s → %s (release to zoom)", startT.Format(layout), endT.Format(layout))
		if ansi.PrintableRuneWidth(txt) > innerW {
			txt = fmt.Sprintf(" ◆ %s → %s", startT.Format(layout), endT.Format(layout))
		}
		if ansi.PrintableRuneWidth(txt) > innerW {
			txt = truncate.String(txt, uint(innerW))
		}
		styled := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorSurfaceFocus).Render(txt)
		return PaintBarLine(styled, innerW, ColorSurfaceFocus, ColorActive)
	}
	if m.WindowStart.IsZero() || m.WindowEnd.IsZero() || !m.WindowEnd.After(m.WindowStart) {
		return PaintBarLine("", innerW, ColorBackground, ColorTextDim)
	}

	span := m.WindowEnd.Sub(m.WindowStart)
	var timeFmt string
	switch {
	case span < 10*time.Minute:
		timeFmt = "15:04:05"
	case span < 24*time.Hour:
		timeFmt = "15:04"
	case span < 14*24*time.Hour:
		timeFmt = "Jan 02 15:04"
	default:
		timeFmt = "2006-01-02"
	}

	sampleW := ansi.PrintableRuneWidth(m.WindowStart.Format(timeFmt))
	if sampleW < 4 {
		sampleW = 4
	}

	var fracs []float64
	switch {
	case innerW >= 5*sampleW+12:
		fracs = []float64{0.0, 0.25, 0.5, 0.75, 1.0}
	case innerW >= 3*sampleW+6:
		fracs = []float64{0.0, 0.5, 1.0}
	case innerW >= 2*sampleW+2:
		fracs = []float64{0.0, 1.0}
	default:
		fracs = []float64{0.0}
	}

	buf := make([]rune, innerW)
	for i := range buf {
		buf[i] = ' '
	}
	used := make([]bool, innerW)

	for _, f := range fracs {
		ts := m.WindowStart.Add(time.Duration(float64(span) * f))
		lbl := ts.Format(timeFmt)
		runes := []rune(lbl)
		lw := len(runes)
		if lw > innerW {
			runes = runes[:innerW]
			lw = innerW
		}

		var start int
		switch {
		case f <= 0.0:
			start = 0
		case f >= 1.0:
			start = innerW - lw
		default:
			center := int(math.Round(f * float64(innerW-1)))
			start = center - lw/2
			if start < 0 {
				start = 0
			}
			if start+lw > innerW {
				start = innerW - lw
			}
		}

		// Ensure zero overlap with already placed tick labels (require 1-col breathing gap).
		conflict := false
		for x := maxInt(0, start-1); x < minInt(innerW, start+lw+1); x++ {
			if used[x] {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		for i, r := range runes {
			buf[start+i] = r
			used[start+i] = true
		}
	}

	return PaintBarLine(string(buf), innerW, ColorBackground, ColorTextDim)
}

func (m TimelineRibbonModel) renderOverviewRow(innerW int) (row string, trackStartX, trackW, leftHandleX, rightHandleX int) {
	if innerW <= 0 {
		return "", -1, 0, -1, -1
	}

	var leftLabel, rightHint string
	switch {
	case innerW >= 104:
		leftLabel = " WINDOW "
		rightHint = " drag/2×click zoom · [/] </> "
	case innerW >= 78:
		leftLabel = " WINDOW "
		rightHint = " drag/2×click · [/] "
	case innerW >= 46:
		leftLabel = " WINDOW "
		rightHint = " "
	case innerW >= 24:
		leftLabel = " WIN "
		rightHint = " "
	default:
		leftLabel = " "
		rightHint = " "
	}

	leftW := ansi.PrintableRuneWidth(leftLabel)
	rightW := ansi.PrintableRuneWidth(rightHint)
	trackW = innerW - leftW - rightW
	if trackW < 4 {
		leftLabel = " "
		rightHint = " "
		leftW = 1
		rightW = 1
		trackW = innerW - 2
	}
	if trackW < 2 {
		return PaintBarLine("", innerW, ColorBackground, ColorMuted), -1, 0, -1, -1
	}

	// Outer X coordinate includes 1-col left box border.
	trackStartX = 1 + leftW

	// Compute full-history commit density per track cell from filtered AllCommits.
	density := make([]int, trackW)
	fullSpan := m.FullMaxTime.Sub(m.FullMinTime)
	if fullSpan > 0 {
		for _, c := range m.AllCommits {
			frac := float64(c.Timestamp.Sub(m.FullMinTime)) / float64(fullSpan)
			col := posToCol(frac, trackW)
			density[col]++
		}
	} else if len(m.AllCommits) > 0 {
		density[trackW/2] = len(m.AllCommits)
	}

	// Compute active window handle columns within [0..trackW-1].
	winStartCol := 0
	winEndCol := trackW - 1
	if fullSpan > 0 && !m.WindowStart.IsZero() && !m.WindowEnd.IsZero() {
		sFrac := float64(m.WindowStart.Sub(m.FullMinTime)) / float64(fullSpan)
		eFrac := float64(m.WindowEnd.Sub(m.FullMinTime)) / float64(fullSpan)
		winStartCol = posToCol(sFrac, trackW)
		winEndCol = posToCol(eFrac, trackW)
		if winEndCol <= winStartCol {
			if winStartCol < trackW-1 {
				winEndCol = winStartCol + 1
			} else if winStartCol > 0 {
				winStartCol = winEndCol - 1
			}
		}
	}

	leftHandleX = trackStartX + winStartCol
	rightHandleX = trackStartX + winEndCol

	leftStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted).Background(ColorBackground)
	if m.IsFocused {
		leftStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorBackground)
	}
	hintStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorBackground)

	handleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Background(ColorDiffHunkBg)
	activeHandleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorOnAccent).Background(ColorPrimary)
	inWinEmptyStyle := lipgloss.NewStyle().Foreground(ColorActive).Background(ColorDiffHunkBg)
	inWinDenseStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorText).Background(ColorDiffHunkBg)
	outEmptyStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(ColorBackground)
	outLightStyle := lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorBackground)
	outHeavyStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Background(ColorBackground)

	// Run-length group consecutive overview cells sharing the same style & rune.
	type ovCellKind int
	const (
		ovHandleLeft ovCellKind = iota
		ovHandleRight
		ovInDense
		ovInEmpty
		ovOutEmpty
		ovOutLight
		ovOutHeavy
	)
	cellKind := func(i int) ovCellKind {
		switch {
		case i == winStartCol:
			return ovHandleLeft
		case i == winEndCol:
			return ovHandleRight
		case i > winStartCol && i < winEndCol:
			if density[i] > 0 {
				return ovInDense
			}
			return ovInEmpty
		default:
			switch {
			case density[i] == 0:
				return ovOutEmpty
			case density[i] == 1:
				return ovOutLight
			default:
				return ovOutHeavy
			}
		}
	}

	var trackBuf strings.Builder
	for i := 0; i < trackW; {
		k := cellKind(i)
		start := i
		i++
		if k != ovHandleLeft && k != ovHandleRight {
			for i < trackW && cellKind(i) == k {
				i++
			}
		}
		n := i - start
		switch k {
		case ovHandleLeft:
			if m.DragMode == "left" {
				trackBuf.WriteString(activeHandleStyle.Render("◄"))
			} else {
				trackBuf.WriteString(handleStyle.Render("◄"))
			}
		case ovHandleRight:
			if m.DragMode == "right" {
				trackBuf.WriteString(activeHandleStyle.Render("►"))
			} else {
				trackBuf.WriteString(handleStyle.Render("►"))
			}
		case ovInDense:
			trackBuf.WriteString(inWinDenseStyle.Render(strings.Repeat("═", n)))
		case ovInEmpty:
			trackBuf.WriteString(inWinEmptyStyle.Render(strings.Repeat("═", n)))
		case ovOutEmpty:
			trackBuf.WriteString(outEmptyStyle.Render(strings.Repeat("░", n)))
		case ovOutLight:
			trackBuf.WriteString(outLightStyle.Render(strings.Repeat("▒", n)))
		case ovOutHeavy:
			trackBuf.WriteString(outHeavyStyle.Render(strings.Repeat("▓", n)))
		}
	}

	raw := leftStyle.Render(leftLabel) + trackBuf.String() + hintStyle.Render(rightHint)
	return PaintBarLine(raw, innerW, ColorBackground, ColorText), trackStartX, trackW, leftHandleX, rightHandleX
}

func (m TimelineRibbonModel) computeLayout() ribbonLayout {
	if m.Width <= 0 {
		return ribbonLayout{trackStartX: -1, leftHandleX: -1, rightHandleX: -1}
	}
	if m.Collapsed || m.Width < 20 {
		return ribbonLayout{
			lines:        []string{m.renderCollapsedLine()},
			trackStartX:  -1,
			leftHandleX:  -1,
			rightHandleX: -1,
		}
	}

	b := lipgloss.RoundedBorder()
	borderFg := ColorBorder
	if m.IsFocused {
		borderFg = ColorActive
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderFg).Background(ColorBackground)
	leftBar := borderStyle.Render(b.Left)
	rightBar := borderStyle.Render(b.Right)

	innerW := m.Width - 2
	topRow, topChips := m.buildTopBorder(borderFg)

	renderer := m.Renderer
	if renderer == nil {
		renderer = BlockPlotRenderer{}
	}
	spec := m.buildPlotSpec(innerW)
	plotRows, colToCommit := renderer.RenderPlot(spec)
	for len(plotRows) < 2 {
		plotRows = append(plotRows, PaintBarLine("", innerW, ColorBackground, ColorText))
	}

	row1 := leftBar + PaintBarLine(plotRows[0], innerW, ColorBackground, ColorText) + rightBar
	row2 := leftBar + PaintBarLine(plotRows[1], innerW, ColorBackground, ColorText) + rightBar

	axisInner := m.renderAxisRow(innerW)
	row3 := leftBar + axisInner + rightBar

	overviewInner, trackStartX, trackW, leftHandleX, rightHandleX := m.renderOverviewRow(innerW)
	row4 := leftBar + overviewInner + rightBar

	bottomRow := PaintBarLine(b.BottomLeft+strings.Repeat(b.Bottom, innerW)+b.BottomRight, m.Width, ColorBackground, borderFg)

	return ribbonLayout{
		lines:        []string{topRow, row1, row2, row3, row4, bottomRow},
		topChips:     topChips,
		colToCommit:  colToCommit,
		trackStartX:  trackStartX,
		trackW:       trackW,
		leftHandleX:  leftHandleX,
		rightHandleX: rightHandleX,
	}
}

// TopChipHitRanges returns the exact rendered horizontal column ranges [StartX, EndX)
// of clickable preset chips and collapse toggle on Row 0 (outer coordinates).
func (m TimelineRibbonModel) TopChipHitRanges() []HitChip {
	return m.computeLayout().topChips
}

const (
	ribbonDoubleClickWindow = 450 * time.Millisecond
	ribbonDoubleClickMaxDX  = 2
	ribbonBrushMinDragCols  = 2
)

func (m TimelineRibbonModel) now() time.Time {
	if m.nowFunc != nil {
		return m.nowFunc()
	}
	return time.Now()
}

func (m TimelineRibbonModel) activeBrushInnerCols(innerW int) (lo, hi int, active bool) {
	if m.DragMode != "brush" || !m.brushDragged || innerW <= 0 {
		return -1, -1, false
	}
	a := m.dragAnchorX - 1
	b := m.brushCurrentX - 1
	if a > b {
		a, b = b, a
	}
	if a < 0 {
		a = 0
	}
	if b >= innerW {
		b = innerW - 1
	}
	if b <= a {
		return -1, -1, false
	}
	return a, b, true
}

func (m TimelineRibbonModel) brushTimeBounds(innerW int) (startT, endT time.Time, ok bool) {
	lo, hi, active := m.activeBrushInnerCols(innerW)
	if !active || innerW <= 1 {
		return time.Time{}, time.Time{}, false
	}
	origStart, origEnd := m.dragOrigStart, m.dragOrigEnd
	if origStart.IsZero() || origEnd.IsZero() || !origEnd.After(origStart) {
		origStart, origEnd = m.WindowStart, m.WindowEnd
	}
	span := origEnd.Sub(origStart)
	if span <= 0 {
		return time.Time{}, time.Time{}, false
	}
	f0 := float64(lo) / float64(innerW-1)
	f1 := float64(hi) / float64(innerW-1)
	startT = origStart.Add(time.Duration(float64(span) * f0))
	endT = origStart.Add(time.Duration(float64(span) * f1))

	minSpan := m.minWindowSpan()
	if endT.Sub(startT) < minSpan {
		mid := startT.Add(endT.Sub(startT) / 2)
		startT = mid.Add(-minSpan / 2)
		endT = startT.Add(minSpan)
	}
	if !m.FullMinTime.IsZero() && startT.Before(m.FullMinTime) {
		startT = m.FullMinTime
		if endT.Sub(startT) < minSpan {
			endT = startT.Add(minSpan)
		}
	}
	if !m.FullMaxTime.IsZero() && endT.After(m.FullMaxTime) {
		endT = m.FullMaxTime
		if endT.Sub(startT) < minSpan {
			startT = endT.Add(-minSpan)
		}
	}
	if !m.FullMinTime.IsZero() && startT.Before(m.FullMinTime) {
		startT = m.FullMinTime
	}
	if !endT.After(startT) {
		return time.Time{}, time.Time{}, false
	}
	return startT, endT, true
}

// ZoomIntoSection progressively zooms the timeline window closer around the clicked column
// (and any changes clustered near that point). area is "plot" (rows 1..3) or "overview" (row 4).
// Returns true if WindowStart or WindowEnd changed.
func (m *TimelineRibbonModel) ZoomIntoSection(relX int, area string) bool {
	fullSpan := m.FullMaxTime.Sub(m.FullMinTime)
	curSpan := m.WindowEnd.Sub(m.WindowStart)
	if fullSpan <= 0 || curSpan <= 0 || m.Width < 20 {
		return false
	}

	var clickTime time.Time
	var refSpan time.Duration
	var candidates []models.TimelineCommit

	if area == "overview" {
		layout := m.computeLayout()
		if layout.trackStartX < 0 || layout.trackW <= 1 {
			return false
		}
		frac := float64(relX-layout.trackStartX) / float64(layout.trackW-1)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		clickTime = m.FullMinTime.Add(time.Duration(float64(fullSpan) * frac))
		refSpan = fullSpan
		candidates = m.AllCommits
	} else {
		innerW := m.Width - 2
		if innerW <= 1 {
			return false
		}
		frac := float64(relX-1) / float64(innerW-1)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		clickTime = m.WindowStart.Add(time.Duration(float64(curSpan) * frac))
		refSpan = curSpan
		candidates = m.WindowCommits
	}

	minSpan := m.minWindowSpan()
	nearRadius := time.Duration(float64(refSpan) * 0.06)
	if nearRadius < minSpan {
		nearRadius = minSpan
	}

	var nearCommits []models.TimelineCommit
	for _, c := range candidates {
		d := c.Timestamp.Sub(clickTime)
		if d < 0 {
			d = -d
		}
		if d <= nearRadius {
			nearCommits = append(nearCommits, c)
		}
	}
	// If no commit fell inside the tight +/-6% radius, snap anchor to nearest commit within +/-10%.
	if len(nearCommits) == 0 && len(candidates) > 0 {
		wideRadius := time.Duration(float64(refSpan) * 0.10)
		bestIdx := -1
		var bestDist time.Duration
		for i, c := range candidates {
			d := c.Timestamp.Sub(clickTime)
			if d < 0 {
				d = -d
			}
			if d <= wideRadius && (bestIdx < 0 || d < bestDist) {
				bestIdx = i
				bestDist = d
			}
		}
		if bestIdx >= 0 {
			nearCommits = append(nearCommits, candidates[bestIdx])
		}
	}

	anchorTime := clickTime
	targetSpan := time.Duration(float64(curSpan) * 0.35)
	if area == "overview" && float64(curSpan) >= float64(fullSpan)*0.80 {
		targetSpan = time.Duration(float64(fullSpan) * 0.30)
	}

	if len(nearCommits) >= 2 {
		minC, maxC := nearCommits[0].Timestamp, nearCommits[0].Timestamp
		for _, c := range nearCommits[1:] {
			if c.Timestamp.Before(minC) {
				minC = c.Timestamp
			}
			if c.Timestamp.After(maxC) {
				maxC = c.Timestamp
			}
		}
		clusterSpan := maxC.Sub(minC)
		anchorTime = minC.Add(clusterSpan / 2)

		// If the local cluster is tighter than 35% of curSpan and not vastly smaller than 8%,
		// frame the cluster cleanly with 15% breathing padding on each side; otherwise step down
		// smoothly by 30% centered on the cluster so repeated double-clicks zoom progressively closer.
		pad := time.Duration(float64(clusterSpan) * 0.15)
		if pad < minSpan/2 {
			pad = minSpan / 2
		}
		paddedCluster := clusterSpan + 2*pad
		if paddedCluster < targetSpan && float64(paddedCluster) >= float64(curSpan)*0.08 {
			targetSpan = paddedCluster
		} else if paddedCluster < targetSpan {
			targetSpan = time.Duration(float64(curSpan) * 0.30)
		}
	} else if len(nearCommits) == 1 {
		anchorTime = nearCommits[0].Timestamp
	}

	if targetSpan < minSpan {
		targetSpan = minSpan
	}
	if targetSpan > fullSpan {
		targetSpan = fullSpan
	}
	if curSpan > minSpan && targetSpan >= curSpan {
		targetSpan = curSpan / 2
		if targetSpan < minSpan {
			targetSpan = minSpan
		}
	}

	newStart := anchorTime.Add(-targetSpan / 2)
	newEnd := newStart.Add(targetSpan)
	if newStart.Before(m.FullMinTime) {
		newStart = m.FullMinTime
		newEnd = newStart.Add(targetSpan)
	}
	if newEnd.After(m.FullMaxTime) {
		newEnd = m.FullMaxTime
		newStart = newEnd.Add(-targetSpan)
	}
	if newStart.Before(m.FullMinTime) {
		newStart = m.FullMinTime
	}

	if !newStart.Equal(m.WindowStart) || !newEnd.Equal(m.WindowEnd) {
		m.WindowStart = newStart
		m.WindowEnd = newEnd
		m.ActivePreset = ""
		m.recomputeWindowCommits()
		return true
	}
	return false
}

// HandleClick processes a mouse click at coordinates (relX, relY) relative to the ribbon's
// top-left corner (0, 0).
//
// Returned (action, payload) pairs:
//   - Row 0 (or collapsed bar): ("preset:1h"|"preset:24h"|"preset:7d"|"preset:all"|"preset:fit", "")
//     or ("toggle_collapse", "") — parent handles preset/collapse transition.
//   - Rows 1..3 (plot/rail/axis):
//     - Double-click -> progressively zooms into that section/cluster and returns ("window_changed", "").
//     - Shift+Click on commit dot -> ("pin_left", fullSHA).
//     - Click/drag -> arms "brush" drag (and immediately returns ("select_commit", fullSHA) when
//       clicking a commit dot on rows 1..2, else ("drag_start:brush", "")). Dragging >=2 cols
//       paints a live brush window committed on EndDrag().
//   - Row 4 (overview scrubber):
//     - Double-click -> zooms into that section on the full history and returns ("window_changed", "").
//     - Drag ◄ ("drag_start:left"), ► ("drag_start:right"), inside window ("drag_start:pan"),
//       or click/drag outside window on track (jumps immediately and allows sweeping a new window).
func (m *TimelineRibbonModel) HandleClick(relX, relY int, shift bool) (action string, payload string) {
	if relX < 0 || relX >= m.Width || relY < 0 || relY >= m.OuterHeight() {
		return "", ""
	}
	if m.Collapsed || m.Width < 20 {
		if relY == 0 {
			return "toggle_collapse", ""
		}
		return "", ""
	}

	layout := m.computeLayout()

	switch relY {
	case 0:
		m.lastClickAt = time.Time{}
		for _, chip := range layout.topChips {
			if relX >= chip.StartX && relX < chip.EndX {
				return chip.Label, ""
			}
		}
		return "", ""

	case 1, 2, 3:
		innerX := relX - 1
		if innerX < 0 || innerX >= m.Width-2 {
			return "", ""
		}

		if !shift {
			now := m.now()
			if !m.lastClickAt.IsZero() &&
				m.lastClickArea == "plot" &&
				now.Sub(m.lastClickAt) <= ribbonDoubleClickWindow &&
				absInt(relX-m.lastClickX) <= ribbonDoubleClickMaxDX {
				m.lastClickAt = time.Time{}
				m.DragMode = ""
				m.brushDragged = false
				if m.ZoomIntoSection(relX, "plot") {
					return "window_changed", ""
				}
				return "", ""
			}
			m.lastClickAt = now
			m.lastClickX = relX
			m.lastClickArea = "plot"

			// Arm brush drag on any non-shift press across the timeline canvas.
			m.DragMode = "brush"
			m.dragAnchorX = relX
			m.brushCurrentX = relX
			m.brushDragged = false
			m.dragOrigStart = m.WindowStart
			m.dragOrigEnd = m.WindowEnd
		}

		// On histogram/rail rows (1..2), also resolve commit dot hits immediately so single-click
		// feels instant and Shift+Click split-compares against the selected commit.
		if relY == 1 || relY == 2 {
			tryCols := []int{innerX, innerX - 1, innerX + 1}
			if shift {
				tryCols = []int{innerX, innerX - 1, innerX + 1, innerX - 2, innerX + 2}
			}
			for _, col := range tryCols {
				if idx, ok := layout.colToCommit[col]; ok && idx >= 0 && idx < len(m.WindowCommits) {
					sha := m.WindowCommits[idx].FullSHA
					if shift {
						return "pin_left", sha
					}
					return "select_commit", sha
				}
			}
		}
		if !shift {
			return "drag_start:brush", ""
		}
		return "", ""

	case 4:
		if layout.trackStartX < 0 || layout.trackW < 2 {
			return "", ""
		}
		trackEndX := layout.trackStartX + layout.trackW
		if relX < layout.trackStartX || relX >= trackEndX {
			return "", ""
		}

		if !shift {
			now := m.now()
			if !m.lastClickAt.IsZero() &&
				m.lastClickArea == "overview" &&
				now.Sub(m.lastClickAt) <= ribbonDoubleClickWindow &&
				absInt(relX-m.lastClickX) <= ribbonDoubleClickMaxDX {
				m.lastClickAt = time.Time{}
				m.DragMode = ""
				m.brushDragged = false
				if m.ZoomIntoSection(relX, "overview") {
					return "window_changed", ""
				}
				return "", ""
			}
			m.lastClickAt = now
			m.lastClickX = relX
			m.lastClickArea = "overview"
		}

		if relX == layout.leftHandleX {
			m.DragMode = "left"
			m.dragAnchorX = relX
			m.brushCurrentX = relX
			m.brushDragged = false
			m.dragOrigStart = m.WindowStart
			m.dragOrigEnd = m.WindowEnd
			return "drag_start:left", ""
		}
		if relX == layout.rightHandleX {
			m.DragMode = "right"
			m.dragAnchorX = relX
			m.brushCurrentX = relX
			m.brushDragged = false
			m.dragOrigStart = m.WindowStart
			m.dragOrigEnd = m.WindowEnd
			return "drag_start:right", ""
		}
		if relX > layout.leftHandleX && relX < layout.rightHandleX {
			m.DragMode = "pan"
			m.dragAnchorX = relX
			m.brushCurrentX = relX
			m.brushDragged = false
			m.dragOrigStart = m.WindowStart
			m.dragOrigEnd = m.WindowEnd
			return "drag_start:pan", ""
		}

		// Clicked outside active window on the overview track: jump & center window immediately,
		// and arm overview_brush so sweeping across the track directly defines a new window.
		m.DragMode = "overview_brush"
		m.dragAnchorX = relX
		m.brushCurrentX = relX
		m.brushDragged = false
		m.dragOrigStart = m.WindowStart
		m.dragOrigEnd = m.WindowEnd

		fullSpan := m.FullMaxTime.Sub(m.FullMinTime)
		winSpan := m.WindowEnd.Sub(m.WindowStart)
		if fullSpan > 0 && winSpan > 0 {
			frac := float64(relX-layout.trackStartX) / float64(layout.trackW-1)
			if frac < 0 {
				frac = 0
			}
			if frac > 1 {
				frac = 1
			}
			center := m.FullMinTime.Add(time.Duration(float64(fullSpan) * frac))
			newStart := center.Add(-winSpan / 2)
			newEnd := newStart.Add(winSpan)
			if newStart.Before(m.FullMinTime) {
				newStart = m.FullMinTime
				newEnd = newStart.Add(winSpan)
			}
			if newEnd.After(m.FullMaxTime) {
				newEnd = m.FullMaxTime
				newStart = newEnd.Add(-winSpan)
			}
			if newStart.Before(m.FullMinTime) {
				newStart = m.FullMinTime
			}
			m.WindowStart = newStart
			m.WindowEnd = newEnd
			m.ActivePreset = ""
			m.recomputeWindowCommits()
			return "window_changed", ""
		}
		return "", ""
	}

	return "", ""
}

// HandleDrag smoothly updates the active window (or live brush selection) while DragMode != "".
// Returns true if WindowStart or WindowEnd changed immediately.
func (m *TimelineRibbonModel) HandleDrag(relX int) bool {
	if m.DragMode == "" || m.Collapsed || m.Width < 20 {
		return false
	}

	// Direct timeline canvas brush: highlight live during drag, commit on EndDrag().
	if m.DragMode == "brush" {
		clampedX := relX
		if clampedX < 1 {
			clampedX = 1
		}
		if clampedX > m.Width-2 {
			clampedX = m.Width - 2
		}
		m.brushCurrentX = clampedX
		if absInt(clampedX-m.dragAnchorX) >= ribbonBrushMinDragCols {
			m.brushDragged = true
		}
		return false
	}

	layout := m.computeLayout()
	if layout.trackW <= 1 {
		return false
	}
	fullSpan := m.FullMaxTime.Sub(m.FullMinTime)
	if fullSpan <= 0 {
		return false
	}

	minSpan := m.minWindowSpan()
	oldStart, oldEnd := m.WindowStart, m.WindowEnd

	if m.DragMode == "overview_brush" {
		clampedX := relX
		if clampedX < layout.trackStartX {
			clampedX = layout.trackStartX
		}
		maxTrackX := layout.trackStartX + layout.trackW - 1
		if clampedX > maxTrackX {
			clampedX = maxTrackX
		}
		m.brushCurrentX = clampedX
		if absInt(clampedX-m.dragAnchorX) < ribbonBrushMinDragCols {
			return false
		}
		m.brushDragged = true
		lo, hi := m.dragAnchorX, clampedX
		if lo > hi {
			lo, hi = hi, lo
		}
		f0 := float64(lo-layout.trackStartX) / float64(layout.trackW-1)
		f1 := float64(hi-layout.trackStartX) / float64(layout.trackW-1)
		newStart := m.FullMinTime.Add(time.Duration(float64(fullSpan) * f0))
		newEnd := m.FullMinTime.Add(time.Duration(float64(fullSpan) * f1))
		if newEnd.Sub(newStart) < minSpan {
			newEnd = newStart.Add(minSpan)
			if newEnd.After(m.FullMaxTime) {
				newEnd = m.FullMaxTime
				newStart = newEnd.Add(-minSpan)
			}
		}
		if newStart.Before(m.FullMinTime) {
			newStart = m.FullMinTime
		}
		m.WindowStart = newStart
		m.WindowEnd = newEnd
		if !m.WindowStart.Equal(oldStart) || !m.WindowEnd.Equal(oldEnd) {
			m.ActivePreset = ""
			m.recomputeWindowCommits()
			return true
		}
		return false
	}

	dx := relX - m.dragAnchorX
	dt := time.Duration(float64(dx) / float64(layout.trackW-1) * float64(fullSpan))

	switch m.DragMode {
	case "left":
		newStart := m.dragOrigStart.Add(dt)
		if newStart.Before(m.FullMinTime) {
			newStart = m.FullMinTime
		}
		maxStart := m.dragOrigEnd.Add(-minSpan)
		if newStart.After(maxStart) {
			newStart = maxStart
		}
		m.WindowStart = newStart
		m.WindowEnd = m.dragOrigEnd

	case "right":
		newEnd := m.dragOrigEnd.Add(dt)
		if newEnd.After(m.FullMaxTime) {
			newEnd = m.FullMaxTime
		}
		minEnd := m.dragOrigStart.Add(minSpan)
		if newEnd.Before(minEnd) {
			newEnd = minEnd
		}
		m.WindowStart = m.dragOrigStart
		m.WindowEnd = newEnd

	case "pan":
		span := m.dragOrigEnd.Sub(m.dragOrigStart)
		newStart := m.dragOrigStart.Add(dt)
		newEnd := m.dragOrigEnd.Add(dt)
		if newStart.Before(m.FullMinTime) {
			newStart = m.FullMinTime
			newEnd = newStart.Add(span)
		}
		if newEnd.After(m.FullMaxTime) {
			newEnd = m.FullMaxTime
			newStart = newEnd.Add(-span)
		}
		if newStart.Before(m.FullMinTime) {
			newStart = m.FullMinTime
		}
		m.WindowStart = newStart
		m.WindowEnd = newEnd
	default:
		return false
	}

	if !m.WindowStart.Equal(oldStart) || !m.WindowEnd.Equal(oldEnd) {
		m.brushDragged = true
		m.ActivePreset = ""
		m.recomputeWindowCommits()
		return true
	}
	return false
}

// EndDrag completes any active scrubber or brush drag interaction.
// Returns true if releasing a timeline brush drag committed a new time window.
func (m *TimelineRibbonModel) EndDrag() bool {
	if m.DragMode == "brush" && m.brushDragged {
		startT, endT, ok := m.brushTimeBounds(m.Width - 2)
		m.DragMode = ""
		m.dragAnchorX = 0
		m.brushCurrentX = 0
		m.brushDragged = false
		m.lastClickAt = time.Time{}
		if ok && (!startT.Equal(m.WindowStart) || !endT.Equal(m.WindowEnd)) {
			m.WindowStart = startT
			m.WindowEnd = endT
			m.ActivePreset = ""
			m.recomputeWindowCommits()
			return true
		}
		return false
	}
	if m.brushDragged {
		m.lastClickAt = time.Time{}
	}
	m.DragMode = ""
	m.dragAnchorX = 0
	m.brushCurrentX = 0
	m.brushDragged = false
	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// View renders the Focus+Context Visual Timeline Ribbon (6 rows expanded, 1 row collapsed),
// painted full-bleed to exact width m.Width across all built-in themes.
func (m TimelineRibbonModel) View() string {
	layout := m.computeLayout()
	return strings.Join(layout.lines, "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
