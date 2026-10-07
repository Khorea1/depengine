package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/graph"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

func parseGraphView(value string) (graph.GraphView, error) {
	switch value {
	case "declared":
		return graph.DeclaredView, nil
	case "effective":
		return graph.EffectiveView, nil
	case "resolved":
		return graph.ResolvedView, nil
	default:
		return graph.DeclaredView, fmt.Errorf("unknown graph view %q (valid: declared, effective, resolved)", value)
	}
}

// graphSliceRequested reports whether the graph command must slice the typed
// IR. The default (dependency direction, unbounded depth) is the historical
// `--only` closure and keeps using schema-level filtering.
func graphSliceRequested(direction graph.Direction, depth int) bool {
	return direction != graph.DirectionDeps || depth != graph.Unbounded
}

// validateGraphSliceOptions rejects slicing flags that have no root to slice
// from, instead of silently ignoring them.
func validateGraphSliceOptions(only string, direction graph.Direction, depth int) error {
	if depth < graph.Unbounded {
		return fmt.Errorf("--depth must be >= 0, or %d for unbounded", graph.Unbounded)
	}
	if graphSliceRequested(direction, depth) && only == "" {
		return fmt.Errorf("--direction and --depth require --only")
	}
	return nil
}

// sliceDeclaredGraph builds the declared IR from the complete tool set and
// slices it around the --only root.
//
// --skip and --profile keep their existing meaning: they decide whether the
// root itself is eligible and never remove nodes reached by the traversal.
// ok is false when the root is missing or filtered out.
func sliceDeclaredGraph(tools map[string]*config.Tool, only, skip, profile string, direction graph.Direction, depth int) (sliced graph.Graph, ok bool, err error) {
	if len(filterTools(tools, only, skip, profile)) == 0 {
		return graph.Graph{}, false, nil
	}
	sliced, err = graph.BuildDeclaredGraph(tools).Slice([]string{only}, direction, depth)
	if err != nil {
		return graph.Graph{}, false, err
	}
	return sliced, true, nil
}

func validateGraphProjectionOptions(view graph.GraphView, includeInactive bool) error {
	if includeInactive && view == graph.DeclaredView {
		return fmt.Errorf("--show-inactive requires --view effective or --view resolved")
	}
	return nil
}

func projectGraphView(ctx context.Context, declared graph.Graph, schema *config.Schema, facts *platform.Facts, view graph.GraphView, includeInactive bool) (graph.Graph, error) {
	if view == graph.DeclaredView {
		return declared.Project(view, graph.ProjectionContext{IncludeInactive: includeInactive})
	}

	needsGuards, candidateTools := graphProjectionRequirements(declared, view)
	if !needsGuards && len(candidateTools) == 0 {
		return declared.Project(view, graph.ProjectionContext{IncludeInactive: includeInactive})
	}

	if facts == nil {
		return graph.Graph{}, fmt.Errorf("host facts are required for %s graph projection", view)
	}

	projection := graph.ProjectionContext{IncludeInactive: includeInactive}
	if needsGuards {
		projection.GuardActive = func(guard graph.Guard) (bool, error) {
			return matchGraphGuard(guard, facts)
		}
	}

	if len(candidateTools) > 0 {
		selected, err := resolvedGraphCandidates(ctx, schema, facts, candidateTools)
		if err != nil {
			return graph.Graph{}, fmt.Errorf("resolve graph candidates: %w", err)
		}
		projection.SelectedCandidate = func(toolID string, candidate int) bool {
			selectedCandidate, ok := selected[toolID]
			return ok && selectedCandidate == candidate
		}
	}

	projected, err := declared.Project(view, projection)
	if err != nil {
		return graph.Graph{}, err
	}
	return projected, nil
}

func graphProjectionRequirements(declared graph.Graph, view graph.GraphView) (bool, map[string]struct{}) {
	candidateTools := make(map[string]struct{})
	needsGuards := false
	for _, edge := range declared.Edges {
		if edge.Guard != nil {
			needsGuards = true
		}
		if view == graph.ResolvedView && edge.Kind == graph.MethodRequire {
			candidateTools[edge.To] = struct{}{}
		}
	}
	return needsGuards, candidateTools
}

func matchGraphGuard(guard graph.Guard, facts *platform.Facts) (bool, error) {
	condition, ok := guard.(*config.Condition)
	if !ok {
		return false, fmt.Errorf("unsupported graph guard %T", guard)
	}
	return condition.Match(facts), nil
}

func resolvedGraphCandidates(ctx context.Context, schema *config.Schema, facts *platform.Facts, candidateTools map[string]struct{}) (map[string]int, error) {
	selected := make(map[string]int)
	if schema == nil || len(candidateTools) == 0 {
		return selected, nil
	}

	clan := platform.ResolveFamily(facts)

	executor := newProjectExecutor(schema, clan, facts, run.OSExecRunner{})

	names := make([]string, 0, len(candidateTools))
	for name := range candidateTools {
		if schema.Tools[name] != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate, ok := selectedGraphCandidate(executor.ExplainTool(ctx, schema.Tools[name], clan))
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ok {
			selected[name] = candidate
		}
	}

	return selected, nil
}

func selectedGraphCandidate(attempts []exec.MethodAttempt) (int, bool) {
	for _, attempt := range attempts {
		switch attempt.Status {
		case "already_installed", "would_install":
			// A synthesized winning candidate has no declared ordinal. It is still
			// the selected plan, so stop here rather than incorrectly selecting a
			// later declared candidate and retaining its activation edges.
			if !attempt.CandidateKnown {
				return 0, false
			}
			return attempt.Candidate, true
		}
	}
	return 0, false
}
