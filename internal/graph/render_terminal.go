package graph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Terminal rendering turns a visible graph into a layered diagram drawn with
// box-drawing characters.
//
// The pipeline follows the research proposal: project -> weak components ->
// ranks (from the scheduling projection) -> coordinates -> orthogonal routes ->
// painted grid. Every component is laid out on its own, so one oversized
// component degrades to a compact edge list instead of forcing the whole graph
// out of its diagram form.
//
// The renderer never reads the environment. The caller passes the available
// width explicitly, which keeps snapshot tests possible at fixed widths.
const (
	// A routing gap keeps free cells around its lanes and arrowhead, so lines
	// never start or end flush against a label: one before the lanes, one
	// between the last lane and the arrowhead, and one after the arrowhead.
	terminalGapFront = 1
	terminalGapPad   = 1
	terminalGapAfter = 1

	terminalArrow = '▶'
)

// RenderTerminalGraph renders the visible graph as a terminal dependency
// diagram.
//
// width is the available terminal width in columns. A component whose layout
// is wider than width falls back to a compact dependency-edge list; other
// components keep their diagram. The returned string is empty for an empty
// graph.
func RenderTerminalGraph(g Graph, width int) (string, error) {
	if width <= 0 {
		return "", fmt.Errorf("graph: terminal width must be positive, got %d", width)
	}

	analysis, err := Analyze(g)
	if err != nil {
		return "", err
	}
	if len(analysis.Connected) == 0 && len(analysis.Isolated) == 0 {
		return "", nil
	}

	sections := make([]string, 0, len(analysis.Connected)+2)
	annotations := []string{}
	for _, component := range analysis.Connected {
		layout, err := layoutTerminalComponent(component, analysis.Ranks)
		if err != nil {
			return "", err
		}
		if layout.width > width {
			sections = append(sections, renderTerminalCompact(layout, width))
			continue
		}
		sections = append(sections, renderTerminalDiagram(layout))
		annotations = append(annotations, terminalAnnotations(layout)...)
	}

	// Line style alone says whether an edge is guarded or method-scoped, not
	// which guard or method it carries. Diagram-rendered components list those
	// annotations once; compact components carry them inline.
	if len(annotations) > 0 {
		sections = append(sections, "annotations:\n  "+strings.Join(annotations, "\n  "))
	}
	if len(analysis.Isolated) > 0 {
		sections = append(sections, renderTerminalIsolated(analysis.Isolated, width))
	}

	return strings.Join(sections, "\n\n") + "\n", nil
}

// terminalStyle carries edge semantics without relying on color. The zero
// value is the weakest style so a cell keeps the strongest relation painted
// over it, matching the bundle rule from the research: unconditional
// scheduling beats guarded scheduling beats activation-only.
type terminalStyle uint8

const (
	terminalStyleDotted terminalStyle = iota
	terminalStyleDashed
	terminalStyleSolid
)

func terminalEdgeStyle(edge Edge) terminalStyle {
	switch {
	case edge.Kind == MethodRequire:
		return terminalStyleDotted
	case edge.Guard != nil:
		return terminalStyleDashed
	default:
		return terminalStyleSolid
	}
}

func strongerStyle(a, b terminalStyle) terminalStyle {
	if a > b {
		return a
	}
	return b
}

// horizontal and vertical return the straight-run glyph for a style. Corners
// and junctions always use the shared solid box-drawing set because terminal
// fonts have no dashed or dotted corner variants; style therefore rides on
// straight runs, not on every cell.
func (s terminalStyle) horizontal() rune {
	switch s {
	case terminalStyleDashed:
		return '╌'
	case terminalStyleDotted:
		return '┄'
	default:
		return '─'
	}
}

func (s terminalStyle) vertical() rune {
	switch s {
	case terminalStyleDashed:
		return '╎'
	case terminalStyleDotted:
		return '┇'
	default:
		return '│'
	}
}

// terminalCell records which neighbours a painted cell connects to. Glyphs are
// derived from those connections so crossings and merges render as real
// junctions instead of overwriting each other.
type terminalCell struct {
	left  bool
	right bool
	up    bool
	down  bool
	style terminalStyle
	glyph rune
}

type terminalCanvas struct {
	cells  []terminalCell
	width  int
	height int
}

func newTerminalCanvas(width, height int) *terminalCanvas {
	return &terminalCanvas{
		cells:  make([]terminalCell, width*height),
		width:  width,
		height: height,
	}
}

func (c *terminalCanvas) cell(x, y int) *terminalCell {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return nil
	}
	return &c.cells[y*c.width+x]
}

// paintHorizontal paints cells x0..x1 on row y. Only interior cells connect
// in both directions automatically: an endpoint connects outward when the
// caller says so, because an endpoint sits next to a label, an arrowhead, or a
// vertical segment of the same route.
func (c *terminalCanvas) paintHorizontal(x0, x1, y int, style terminalStyle, connectLeft, connectRight bool) {
	if x0 > x1 {
		x0, x1 = x1, x0
		connectLeft, connectRight = connectRight, connectLeft
	}
	for x := x0; x <= x1; x++ {
		cell := c.cell(x, y)
		if cell == nil {
			continue
		}
		if x > x0 {
			cell.left = true
		}
		if x < x1 {
			cell.right = true
		}
		if x == x0 {
			cell.left = cell.left || connectLeft
		}
		if x == x1 {
			cell.right = cell.right || connectRight
		}
		cell.style = strongerStyle(cell.style, style)
	}
}

// paintVertical paints cells y0..y1 on column x. Its endpoints connect to the
// horizontal runs of the same route, which the caller paints separately.
func (c *terminalCanvas) paintVertical(y0, y1, x int, style terminalStyle) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		cell := c.cell(x, y)
		if cell == nil {
			continue
		}
		if y > y0 {
			cell.up = true
		}
		if y < y1 {
			cell.down = true
		}
		cell.style = strongerStyle(cell.style, style)
	}
}

func (c *terminalCanvas) paintGlyph(x, y int, glyph rune) {
	if cell := c.cell(x, y); cell != nil {
		cell.glyph = glyph
	}
}

func (c *terminalCanvas) paintText(x, y int, text string) {
	for i, r := range []rune(text) {
		c.paintGlyph(x+i, y, r)
	}
}

func (c terminalCanvas) String() string {
	rows := make([]string, 0, c.height)
	for y := 0; y < c.height; y++ {
		line := make([]rune, c.width)
		for x := 0; x < c.width; x++ {
			line[x] = c.cells[y*c.width+x].render()
		}
		rows = append(rows, strings.TrimRight(string(line), " "))
	}
	for len(rows) > 1 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return strings.Join(rows, "\n")
}

func (cell terminalCell) render() rune {
	if cell.glyph != 0 {
		return cell.glyph
	}
	switch {
	case cell.left && cell.right && cell.up && cell.down:
		return '┼'
	case cell.left && cell.right && cell.up:
		return '┴'
	case cell.left && cell.right && cell.down:
		return '┬'
	case cell.left && cell.up && cell.down:
		return '┤'
	case cell.right && cell.up && cell.down:
		return '├'
	case cell.left && cell.up:
		return '┘'
	case cell.left && cell.down:
		return '┐'
	case cell.right && cell.up:
		return '└'
	case cell.right && cell.down:
		return '┌'
	case cell.left, cell.right:
		return cell.style.horizontal()
	case cell.up, cell.down:
		return cell.style.vertical()
	default:
		return ' '
	}
}

// terminalLabel converts node IDs to single-cell ASCII when terminal display
// width would otherwise differ from rune count (wide, combining, or control
// characters). ASCII IDs remain unchanged; escaped IDs stay unambiguous and
// make the renderer's column arithmetic exact without depending on locale.
func terminalLabel(id string) string {
	for _, r := range id {
		if r < 0x20 || r > 0x7e {
			return strconv.QuoteToASCII(id)
		}
	}
	return id
}

type terminalNode struct {
	id    string
	label string
	row   int
}

type terminalColumn struct {
	start int
	width int
	nodes []terminalNode
}

// terminalGap is the routing space around one column. gap[c] sits left of
// column c, so a source in column c exits through gap[c+1] and a target in
// column c is approached through gap[c]. The last gap is the trailing gutter.
//
// A gap keeps two lane regions: exit lanes on the source side and entry lanes
// on the target side. Both are shared per row, so routes that leave the same
// tool or enter the same tool share one physical segment instead of fanning
// out into parallel columns.
type terminalGap struct {
	start int
	width int
	front int
	arrow bool

	exitLanes  int
	entryLanes int
	exitRows   map[int]int
	entryRows  map[int]int
}

func (g *terminalGap) exitLane(row int) int {
	if g.exitRows == nil {
		g.exitRows = map[int]int{}
	}
	if lane, ok := g.exitRows[row]; ok {
		return lane
	}
	lane := g.exitLanes
	g.exitLanes++
	g.exitRows[row] = lane
	return lane
}

func (g *terminalGap) entryLane(row int) int {
	if g.entryRows == nil {
		g.entryRows = map[int]int{}
	}
	if lane, ok := g.entryRows[row]; ok {
		return lane
	}
	lane := g.entryLanes
	g.entryLanes++
	g.entryRows[row] = lane
	return lane
}

func (g terminalGap) exitLaneX(lane int) int  { return g.start + g.front + lane }
func (g terminalGap) entryLaneX(lane int) int { return g.start + g.front + g.exitLanes + lane }

// arrowX is the cell holding the arrowhead of a gap that has one. It sits one
// cell before the next column starts, leaving room for "▶ label".
func (g terminalGap) arrowX() int {
	return g.start + g.front + g.exitLanes + g.entryLanes + terminalGapPad
}

// computeWidth sizes a gap from its lanes. A gap without lanes and without an
// arrowhead collapses to nothing, which is what lets a graph whose first rank
// has no incoming edge start at column zero.
func (g *terminalGap) computeWidth(trailing bool) {
	lanes := g.exitLanes + g.entryLanes
	g.front = 0
	if lanes > 0 {
		g.front = terminalGapFront
	}
	switch {
	case trailing:
		g.width = g.front + g.exitLanes
	case lanes == 0 && !g.arrow:
		g.width = 0
	case g.arrow:
		g.width = g.front + lanes + terminalGapPad + 1 + terminalGapAfter
	default:
		g.width = g.front + lanes
	}
}

type terminalRoute struct {
	edge  Edge
	style terminalStyle

	srcCol int
	dstCol int
	srcRow int
	dstRow int
	direct bool

	exitGap   int
	exitLane  int
	entryGap  int
	entryLane int

	exitX      int
	laneExitX  int
	laneEntryX int
	arrowX     int
	corridor   int // corridor row index; -1 when the route is adjacent
}

type terminalCorridor struct {
	spans [][2]int
}

func (c terminalCorridor) conflicts(span [2]int) bool {
	for _, other := range c.spans {
		if other[0] <= span[1] && span[0] <= other[1] {
			return true
		}
	}
	return false
}

type terminalLayout struct {
	component Component
	columns   []terminalColumn
	gaps      []terminalGap
	routes    []terminalRoute
	corridors []terminalCorridor
	nodeRows  int
	width     int
}

// layoutTerminalComponent assigns columns from scheduling ranks, rows within
// each column, routing lanes per gap, and corridor rows for routes that are
// not between adjacent columns.
func layoutTerminalComponent(component Component, ranks map[string]int) (*terminalLayout, error) {
	columns, rows, err := terminalColumns(component, ranks)
	if err != nil {
		return nil, err
	}

	gaps := make([]terminalGap, len(columns)+1)
	routes := make([]terminalRoute, 0, len(component.Edges))
	routeByEndpoints := make(map[[2]string]int, len(component.Edges))
	for _, edge := range component.Edges {
		endpoints := [2]string{edge.From, edge.To}
		if index, ok := routeByEndpoints[endpoints]; ok {
			// Semantic multiedges remain distinct in the IR and annotations, but
			// the terminal layout shares one physical route between identical
			// endpoints. The strongest visible relation determines line style.
			routes[index].style = strongerStyle(routes[index].style, terminalEdgeStyle(edge))
			continue
		}

		srcCol, ok := columnIndexOf(columns, edge.From)
		if !ok {
			return nil, fmt.Errorf("graph: edge %q -> %q starts at a node outside its component", edge.From, edge.To)
		}
		dstCol, ok := columnIndexOf(columns, edge.To)
		if !ok {
			return nil, fmt.Errorf("graph: edge %q -> %q targets a node outside its component", edge.From, edge.To)
		}

		route := terminalRoute{
			edge:     edge,
			style:    terminalEdgeStyle(edge),
			srcCol:   srcCol,
			dstCol:   dstCol,
			srcRow:   rows[edge.From],
			dstRow:   rows[edge.To],
			corridor: -1,
		}
		if dstCol == srcCol+1 {
			route.direct = true
			route.entryGap = dstCol
			route.entryLane = gaps[dstCol].entryLane(route.dstRow)
		} else {
			route.exitGap = srcCol + 1
			route.exitLane = gaps[srcCol+1].exitLane(route.srcRow)
			route.entryGap = dstCol
			route.entryLane = gaps[dstCol].entryLane(route.dstRow)
		}
		gaps[dstCol].arrow = true
		routeByEndpoints[endpoints] = len(routes)
		routes = append(routes, route)
	}

	x := 0
	for i := range gaps {
		gaps[i].computeWidth(i == len(columns))
	}
	for i := range columns {
		gaps[i].start = x
		x += gaps[i].width
		columns[i].start = x
		x += columns[i].width
	}
	gaps[len(columns)].start = x
	x += gaps[len(columns)].width

	layout := &terminalLayout{
		component: component,
		columns:   columns,
		gaps:      gaps,
		routes:    routes,
		nodeRows:  terminalNodeRows(columns),
		width:     x,
	}

	for i := range layout.routes {
		route := &layout.routes[i]
		exitGap := gaps[route.srcCol+1]
		route.exitX = exitGap.start + exitGap.front
		route.arrowX = gaps[route.dstCol].arrowX()
		if route.direct {
			laneX := gaps[route.entryGap].entryLaneX(route.entryLane)
			route.laneExitX, route.laneEntryX = laneX, laneX
		} else {
			route.laneExitX = gaps[route.exitGap].exitLaneX(route.exitLane)
			route.laneEntryX = gaps[route.entryGap].entryLaneX(route.entryLane)
		}
	}

	layout.assignCorridors()
	return layout, nil
}

// assignCorridors gives every non-adjacent route a routing row below the
// labels. Rows are reused when a new route's horizontal span does not overlap
// the spans already routed there, which keeps tall schemas from growing one
// row per long edge.
func (l *terminalLayout) assignCorridors() {
	for i := range l.routes {
		route := &l.routes[i]
		if route.direct {
			continue
		}
		span := [2]int{route.laneExitX, route.laneEntryX}
		if span[0] > span[1] {
			span[0], span[1] = span[1], span[0]
		}
		index := -1
		for candidate, corridor := range l.corridors {
			if !corridor.conflicts(span) {
				index = candidate
				break
			}
		}
		if index < 0 {
			l.corridors = append(l.corridors, terminalCorridor{})
			index = len(l.corridors) - 1
		}
		l.corridors[index].spans = append(l.corridors[index].spans, span)
		route.corridor = index
	}
}

func terminalColumns(component Component, ranks map[string]int) ([]terminalColumn, map[string]int, error) {
	byRank := map[int][]string{}
	minRank, maxRank := 0, -1
	for _, id := range component.Nodes {
		rank, ok := ranks[id]
		if !ok {
			return nil, nil, fmt.Errorf("graph: node %q has no scheduling rank", id)
		}
		byRank[rank] = append(byRank[rank], id)
		if maxRank < 0 || rank < minRank {
			minRank = rank
		}
		if rank > maxRank {
			maxRank = rank
		}
	}

	columns := make([]terminalColumn, 0, maxRank-minRank+1)
	for rank := minRank; rank <= maxRank; rank++ {
		member := byRank[rank]
		column := terminalColumn{nodes: make([]terminalNode, 0, len(member))}
		for _, id := range member {
			column.nodes = append(column.nodes, terminalNode{id: id, label: terminalLabel(id)})
		}
		columns = append(columns, column)
	}

	orderTerminalColumns(columns, component.Edges)

	rows := make(map[string]int, len(component.Nodes))
	nodeRows := terminalNodeRows(columns)
	for i := range columns {
		// Shorter columns are centered against the tallest one so a lone
		// dependent does not sit pinned to the top of its neighbours.
		offset := (nodeRows - len(columns[i].nodes)) / 2
		for j := range columns[i].nodes {
			columns[i].nodes[j].row = offset + j
			rows[columns[i].nodes[j].id] = offset + j
			if width := len(columns[i].nodes[j].label); width > columns[i].width {
				columns[i].width = width
			}
		}
	}
	return columns, rows, nil
}

const terminalCrossingSweeps = 4

// orderTerminalColumns applies a deterministic barycenter heuristic to the
// nodes inside each scheduling rank. Rank assignment remains untouched; only
// presentation order changes. Repeated semantic edges between the same pair do
// not overweight that relation during layout.
func orderTerminalColumns(columns []terminalColumn, edges []Edge) {
	if len(columns) < 2 {
		return
	}

	for sweep := 0; sweep < terminalCrossingSweeps; sweep++ {
		for column := 1; column < len(columns); column++ {
			orderTerminalColumn(columns, column, edges, true)
		}
		for column := len(columns) - 2; column >= 0; column-- {
			orderTerminalColumn(columns, column, edges, false)
		}
	}
}

type terminalBarycenter struct {
	sum   int64
	count int64
}

func orderTerminalColumn(columns []terminalColumn, columnIndex int, edges []Edge, predecessors bool) {
	column := &columns[columnIndex]
	if len(column.nodes) < 2 {
		return
	}

	positions := terminalColumnPositions(columns)
	members := make(map[string]struct{}, len(column.nodes))
	for _, node := range column.nodes {
		members[node.id] = struct{}{}
	}

	neighbors := make(map[string]map[string]struct{}, len(column.nodes))
	for _, edge := range edges {
		nodeID, neighborID := edge.To, edge.From
		if !predecessors {
			nodeID, neighborID = edge.From, edge.To
		}
		if _, ok := members[nodeID]; !ok {
			continue
		}
		if neighbors[nodeID] == nil {
			neighbors[nodeID] = map[string]struct{}{}
		}
		neighbors[nodeID][neighborID] = struct{}{}
	}

	scores := make(map[string]terminalBarycenter, len(column.nodes))
	for _, node := range column.nodes {
		score := terminalBarycenter{}
		for neighborID := range neighbors[node.id] {
			if position, ok := positions[neighborID]; ok {
				score.sum += position
				score.count++
			}
		}
		if score.count == 0 {
			// Nodes without a neighbor in this sweep stay anchored to their
			// current row instead of being pulled arbitrarily to an edge.
			score.sum = positions[node.id]
			score.count = 1
		}
		scores[node.id] = score
	}

	sort.SliceStable(column.nodes, func(i, j int) bool {
		left := scores[column.nodes[i].id]
		right := scores[column.nodes[j].id]
		leftScaled := left.sum * right.count
		rightScaled := right.sum * left.count
		if leftScaled != rightScaled {
			return leftScaled < rightScaled
		}
		return column.nodes[i].id < column.nodes[j].id
	})
}

func terminalColumnPositions(columns []terminalColumn) map[string]int64 {
	nodeRows := terminalNodeRows(columns)
	positions := make(map[string]int64)
	for _, column := range columns {
		offset := (nodeRows - len(column.nodes)) / 2
		for index, node := range column.nodes {
			positions[node.id] = int64(offset + index)
		}
	}
	return positions
}

func terminalNodeRows(columns []terminalColumn) int {
	nodeRows := 0
	for _, column := range columns {
		if len(column.nodes) > nodeRows {
			nodeRows = len(column.nodes)
		}
	}
	return nodeRows
}

func columnIndexOf(columns []terminalColumn, id string) (int, bool) {
	for i, column := range columns {
		for _, node := range column.nodes {
			if node.id == id {
				return i, true
			}
		}
	}
	return 0, false
}

// renderTerminalDiagram paints one component as a layered diagram.
func renderTerminalDiagram(layout *terminalLayout) string {
	canvas := newTerminalCanvas(layout.width, layout.nodeRows+len(layout.corridors))
	for _, route := range layout.routes {
		paintTerminalRoute(canvas, layout, route)
	}
	for _, column := range layout.columns {
		for _, node := range column.nodes {
			canvas.paintText(column.start, node.row, node.label)
		}
	}
	for _, route := range layout.routes {
		canvas.paintGlyph(route.arrowX, route.dstRow, terminalArrow)
	}
	return canvas.String()
}

// paintTerminalRoute draws one edge as orthogonal segments.
//
// Adjacent columns route straight across. Any other pair leaves the source to
// the right, drops into a corridor row below the labels, and rises into the
// target from the left, so multi-rank and backward routes never cross a label.
func paintTerminalRoute(canvas *terminalCanvas, layout *terminalLayout, route terminalRoute) {
	style := route.style
	if route.direct {
		if route.srcRow == route.dstRow {
			canvas.paintHorizontal(route.exitX, route.arrowX-1, route.srcRow, style, true, true)
			return
		}
		canvas.paintHorizontal(route.exitX, route.laneExitX, route.srcRow, style, true, false)
		canvas.paintVertical(route.srcRow, route.dstRow, route.laneExitX, style)
		canvas.paintHorizontal(route.laneExitX, route.arrowX-1, route.dstRow, style, false, true)
		return
	}

	corridorRow := layout.nodeRows + route.corridor
	canvas.paintHorizontal(route.exitX, route.laneExitX, route.srcRow, style, true, false)
	canvas.paintVertical(route.srcRow, corridorRow, route.laneExitX, style)
	canvas.paintHorizontal(route.laneExitX, route.laneEntryX, corridorRow, style, false, false)
	canvas.paintVertical(route.dstRow, corridorRow, route.laneEntryX, style)
	canvas.paintHorizontal(route.laneEntryX, route.arrowX-1, route.dstRow, style, false, true)
}

// terminalAnnotations lists the diagram-rendered edges that carry a guard or a
// method selector, so line style has a readable companion.
func terminalAnnotations(layout *terminalLayout) []string {
	annotations := make([]string, 0, len(layout.component.Edges))
	for _, edge := range layout.component.Edges {
		if label := edgeLabel(edge); label != "" {
			annotations = append(annotations, fmt.Sprintf("%s -> %s [%s]", terminalLabel(edge.From), terminalLabel(edge.To), label))
		}
	}
	return annotations
}

// renderTerminalCompact is the width fallback: a dependency-edge view, not the
// topological level list, so a component that does not fit stays a graph.
func renderTerminalCompact(layout *terminalLayout, width int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "component needs %d columns (%d available):", layout.width, width)
	for _, edge := range layout.component.Edges {
		b.WriteString("\n  ")
		b.WriteString(terminalLabel(edge.From))
		b.WriteString(" -> ")
		b.WriteString(terminalLabel(edge.To))
		if label := edgeLabel(edge); label != "" {
			b.WriteString(" [")
			b.WriteString(label)
			b.WriteString("]")
		}
	}
	return b.String()
}

// renderTerminalIsolated condenses nodes without relations into a wrapped
// sorted list instead of drawing each one as a diagram.
func renderTerminalIsolated(ids []string, width int) string {
	var b strings.Builder
	b.WriteString("isolated:")
	line := ""
	for _, id := range ids {
		label := terminalLabel(id)
		switch {
		case line == "":
			line = label
		case len(line)+2+len(label) <= width:
			line += ", " + label
		default:
			b.WriteString("\n")
			b.WriteString(line)
			line = label
		}
	}
	if line != "" {
		b.WriteString("\n")
		b.WriteString(line)
	}
	return b.String()
}
