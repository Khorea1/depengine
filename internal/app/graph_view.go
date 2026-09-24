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

	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		return graph.Graph{}, fmt.Errorf("gather host facts: %w", err)
	}

	projection := graph.ProjectionContext{
		GuardActive: func(guard graph.Guard) (bool, error) {
			condition, ok := guard.(*config.Condition)
			if !ok {
				return false, fmt.Errorf("unsupported graph guard %T", guard)
			}
			return condition.Match(facts), nil
		},
	}

	if view == graph.ResolvedView {
		selected := resolvedGraphCandidates(ctx, schema, facts)
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

func resolvedGraphCandidates(ctx context.Context, schema *config.Schema, facts *engine.Facts) map[string]int {
	selected := make(map[string]int)
	if schema == nil {
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

	names := make([]string, 0, len(schema.Tools))
	for name := range schema.Tools {
		names = append(names, name)
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
