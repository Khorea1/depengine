package graph

import (
	"errors"
	"fmt"
)

// GraphView identifies a semantic projection of the dependency graph.
type GraphView uint8

const (
	// DeclaredView contains all declared semantic edges without evaluating
	// host-specific conditions.
	DeclaredView GraphView = iota

	// EffectiveView contains relations whose guards are active in a supplied
	// evaluation context.
	EffectiveView

	// ResolvedView contains active relations selected by an installation plan.
	ResolvedView
)

// ErrProjectionUnavailable reports that a graph view needs context that was not
// supplied by its caller.
var ErrProjectionUnavailable = errors.New("graph: projection context unavailable")

// ProjectionContext supplies domain decisions without coupling graph to config,
// platform facts, or install-plan types.
type ProjectionContext struct {
	// GuardActive evaluates an opaque declared guard. It is only required when
	// the input graph actually contains guarded edges.
	GuardActive func(Guard) (bool, error)

	// SelectedCandidate reports whether one declared candidate is the selected
	// candidate for a tool. Candidate is the zero-based ordinal retained from
	// the merged tool method list, so same-kind unlabeled candidates remain
	// distinguishable.
	//
	// It is only required by ResolvedView when method-require edges are present.
	SelectedCandidate func(toolID string, candidate int) bool
}

func (v GraphView) String() string {
	switch v {
	case DeclaredView:
		return "declared"
	case EffectiveView:
		return "effective"
	case ResolvedView:
		return "resolved"
	default:
		return fmt.Sprintf("unknown(%d)", v)
	}
}

// Project returns a semantic projection of the graph.
//
// DeclaredView is host-independent. EffectiveView evaluates guards and omits
// inactive relations. ResolvedView additionally keeps only method-require
// relations that belong to the selected candidate for their dependent tool.
func (g Graph) Project(view GraphView, context ProjectionContext) (Graph, error) {
	if view != DeclaredView && view != EffectiveView && view != ResolvedView {
		return Graph{}, fmt.Errorf("graph: unknown projection %d", view)
	}
	if view == DeclaredView {
		return g.Canonicalize(), nil
	}

	out := NewGraph()
	for _, node := range g.Nodes {
		out.AddNode(node)
	}

	for _, edge := range g.Edges {
		if view == ResolvedView && edge.Kind == MethodRequire {
			if !edge.CandidateKnown {
				return Graph{}, fmt.Errorf("%w: resolved view requires exact candidate identity for %q -> %q", ErrProjectionUnavailable, edge.From, edge.To)
			}
			if context.SelectedCandidate == nil {
				return Graph{}, fmt.Errorf("%w: resolved view requires selected candidates", ErrProjectionUnavailable)
			}
			if !context.SelectedCandidate(edge.To, edge.Candidate) {
				continue
			}
		}

		if edge.Guard != nil {
			if context.GuardActive == nil {
				return Graph{}, fmt.Errorf("%w: %s view requires guard evaluation", ErrProjectionUnavailable, view)
			}
			active, err := context.GuardActive(edge.Guard)
			if err != nil {
				return Graph{}, fmt.Errorf("graph: evaluate guard on %q -> %q: %w", edge.From, edge.To, err)
			}
			if !active {
				continue
			}
		}

		out.AddEdge(edge)
	}

	return out.Canonicalize(), nil
}
