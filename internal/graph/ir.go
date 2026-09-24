package graph

import "sort"

// Guard is an opaque dependency condition retained by the graph IR.
//
// Graph analysis does not need to know the config representation. The stable
// String form is used for deterministic ordering and presentation; future
// effective projections can receive an evaluator for the concrete guard value.
type Guard interface {
	String() string
}

// EdgeKind describes the semantic source of a dependency relation.
//
// The graph IR intentionally separates relation kind from guards. A guarded
// ToolRequire edge is still a tool requirement; the guard is additional
// metadata, not a different relation type.
type EdgeKind uint8

const (
	ToolRequire EdgeKind = iota
	MethodRequire
)

// EdgeRole describes whether an edge participates in ordering analysis.
type EdgeRole uint8

const (
	Scheduling EdgeRole = iota
	Activation
)

// Graph is the typed dependency graph intermediate representation.
//
// Edges are kept as semantic multiedges. Renderers may collapse them visually,
// but analysis must not lose their meaning.
type Graph struct {
	Nodes map[string]Node
	Edges []Edge
}

// Node is a dependency graph vertex.
type Node struct {
	ID             string
	Tags           []string
	DependencyOnly bool
}

// Edge is directed from dependency to dependent tool.
type Edge struct {
	From   string
	To     string
	Kind   EdgeKind
	Role   EdgeRole
	Guard  Guard
	Method string
}

// NewGraph creates an empty typed graph.
func NewGraph() Graph {
	return Graph{Nodes: map[string]Node{}}
}

// AddNode inserts or replaces a graph node. Slice metadata is copied so later
// mutations of the source model cannot mutate the graph implicitly.
func (g *Graph) AddNode(node Node) {
	if g.Nodes == nil {
		g.Nodes = map[string]Node{}
	}
	node.Tags = append([]string(nil), node.Tags...)
	g.Nodes[node.ID] = node
}

// AddEdge preserves every semantic relation, including parallel edges.
func (g *Graph) AddEdge(edge Edge) {
	g.Edges = append(g.Edges, edge)
}

// SchedulingProjection returns the subset used by installation ordering.
func (g Graph) SchedulingProjection() Graph {
	out := NewGraph()
	for _, node := range g.Nodes {
		out.AddNode(node)
	}
	for _, edge := range g.Edges {
		if edge.Role == Scheduling {
			out.AddEdge(edge)
		}
	}
	return out.Canonicalize()
}

// Canonicalize returns an independent graph value with deterministic edge
// ordering. Nodes remain addressable by ID; their slice metadata is copied.
func (g Graph) Canonicalize() Graph {
	nodes := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)

	ordered := NewGraph()
	for _, id := range nodes {
		ordered.AddNode(g.Nodes[id])
	}
	ordered.Edges = append(ordered.Edges, g.Edges...)
	sort.Slice(ordered.Edges, func(i, j int) bool {
		a, b := ordered.Edges[i], ordered.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		return guardLabel(a.Guard) < guardLabel(b.Guard)
	})
	return ordered
}

func guardLabel(guard Guard) string {
	if guard == nil {
		return ""
	}
	return guard.String()
}
