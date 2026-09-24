package app

import (
	"context"
	"fmt"
	"sort"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/graph"
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

func projectGraphView(ctx context.Context, declared graph.Graph, schema *config.Schema, view graph.GraphView) (graph.Graph, error) {
	if view == graph.DeclaredView {
		return declared.Project(view, graph.ProjectionContext{})
	}

	needsGuards, candidateTools := graphProjectionRequirements(declared, view)
	if !needsGuards && len(candidateTools) == 0 {
		return declared.Project(view, graph.ProjectionContext{})
	}

	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		return graph.Graph{}, fmt.Errorf("gather host facts: %w", err)
	}

	projection := graph.ProjectionContext{}
	if needsGuards {
		projection.GuardActive = func(guard graph.Guard) (bool, error) {
			return matchGraphGuard(guard, facts)
		}
	}

	if len(candidateTools) > 0 {
		selected := resolvedGraphCandidates(ctx, schema, facts, candidateTools)
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

func matchGraphGuard(guard graph.Guard, facts *engine.Facts) (bool, error) {
	condition, ok := guard.(*config.Condition)
	if !ok {
		return false, fmt.Errorf("unsupported graph guard %T", guard)
	}
	return condition.Match(facts), nil
}

func resolvedGraphCandidates(ctx context.Context, schema *config.Schema, facts *engine.Facts, candidateTools map[string]struct{}) map[string]int {
	selected := make(map[string]int)
	if schema == nil || len(candidateTools) == 0 {
		return selected
	}

	clan := engine.ResolveFamily(facts)
	if helper := schema.Defaults.AurHelper; helper != "" {
		ecosystem.ReconfigureAUR(helper)
	}

	executor := exec.New()
	exec.WithRunner(run.OSExecRunner{})(executor)
	exec.WithFacts(facts)(executor)
	exec.WithDefaultMethodOrder(schema.Defaults.MethodOrder)(executor)

	names := make([]string, 0, len(candidateTools))
	for name := range candidateTools {
		if schema.Tools[name] != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		candidate, ok := selectedGraphCandidate(executor.ExplainTool(ctx, schema.Tools[name], clan))
		if ok {
			selected[name] = candidate
		}
	}

	return selected
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
