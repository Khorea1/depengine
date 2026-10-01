package exec

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/source"
)

// ResolvedCandidate identifies the exact configured method selected for a
// canonical resolved plan. Method is the original candidate pointer, including
// its label when multiple candidates share a kind.
type ResolvedCandidate struct {
	Method *config.MethodCandidate
	Plan   plan.ResolvedInstallPlan
}

type candidateSelection struct {
	intent   *plan.ResolvedInstallPlan
	resolved *plan.ResolvedInstallPlan
	adapter  AdapterV2
	status   string
	err      error
}

// resolveCandidateForSelection contains the static and plan-resolution gates
// shared by lock resolution and ExplainTool. Skippable candidate failures are
// returned as a status/error pair so each caller can project its own contract.
func (ex *Executor) resolveCandidateForSelection(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, clan string) candidateSelection {
	intent, mismatch := candidatePlanIntent(tool, method)
	intent = ex.hostResolvedPlanIntent(method, intent, clan)
	if mismatch != "" {
		return candidateSelection{intent: intent, status: "skip_capability", err: fmt.Errorf("%s", mismatch)}
	}
	if method.When != nil && !method.When.Match(ex.facts) {
		return candidateSelection{intent: intent, status: "skip_when", err: fmt.Errorf("when condition not met: %+v", method.When)}
	}
	displayKind := method.Kind
	if method.Label != "" {
		displayKind = method.Label
	}
	adapter := ex.LookupAdapter(method.Kind)
	if adapter == nil {
		return candidateSelection{intent: intent, status: "skip_unavailable", err: fmt.Errorf("no adapter registered for kind %q", displayKind)}
	}
	if !adapter.Available(ctx, ex.probeRunner(tool.Name, displayKind)) {
		return candidateSelection{intent: intent, adapter: adapter, status: "skip_unavailable", err: fmt.Errorf("adapter %q not available (binary not on PATH)", displayKind)}
	}
	resolved, err := ex.resolveCandidatePlan(ctx, tool, method, adapter, intent, displayKind)
	if err != nil {
		return candidateSelection{intent: intent, adapter: adapter, status: "failed", err: err}
	}
	if resolved == nil {
		return candidateSelection{intent: intent, adapter: adapter, status: "failed", err: fmt.Errorf("%s: resolver returned no plan", displayKind)}
	}
	if err := adapter.CheckHostCompatibility(tool, method, resolved, ex.facts, clan); err != nil {
		return candidateSelection{intent: intent, resolved: resolved, adapter: adapter, status: "skip_unavailable", err: err}
	}
	return candidateSelection{intent: intent, resolved: resolved, adapter: adapter}
}

// ResolveLockCandidate resolves the first candidate suitable for the v2 lock
// in executor order. It performs only adapter/source probes and never runs
// install, remove, lifecycle, prerequisite, or state mutation paths.
func (ex *Executor) ResolveLockCandidate(ctx context.Context, tool *config.Tool, clan string) (ResolvedCandidate, error) {
	if tool == nil {
		return ResolvedCandidate{}, fmt.Errorf("resolve lock candidate: nil tool")
	}
	ctx = omitToolSecretEnvironment(ctx, tool)
	manager := source.NewManager(ex.rn, true)
	var lastErr error
	for _, method := range ex.SelectedMethods(tool, clan) {
		selection := ex.resolveCandidateForSelection(ctx, tool, method, clan)
		if selection.err != nil {
			if selection.status == "failed" || lastErr == nil {
				lastErr = selection.err
			}
			continue
		}
		resolved := selection.resolved
		configuredSources, err := sourcesForResolvedPlan(method.Sources, resolved)
		if err != nil {
			lastErr = err
			continue
		}
		probe, err := probeCandidateSources(ctx, manager, configuredSources)
		if err != nil {
			lastErr = err
			continue
		}
		displayKind := method.Kind
		if method.Label != "" {
			displayKind = method.Label
		}
		if len(probe.missing) == 0 && !checkAvailable(ctx, ex.probeRunner(tool.Name, displayKind), selection.adapter, tool, method) {
			if lastErr == nil {
				lastErr = fmt.Errorf("%s: package not found in repo/index", displayKind)
			}
			continue
		}
		if probe.preparationPlan != nil && resolved != nil {
			projected := resolved.Clone()
			projected.Preparation = probe.preparationPlan
			resolved = &projected
		}
		for _, revision := range manager.SourceRevisions() {
			for _, reference := range resolved.Sources {
				if reference.Kind != revision.Kind || reference.Name != revision.Name {
					continue
				}
				if err := resolved.ApplyResolvedSourceRevision(revision.Kind, revision.Name, revision.Revision); err != nil {
					lastErr = err
					resolved = nil
				}
				break
			}
			if resolved == nil {
				break
			}
		}
		if resolved == nil {
			continue
		}
		return ResolvedCandidate{Method: method, Plan: resolved.Clone()}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no applicable method candidate")
	}
	return ResolvedCandidate{}, fmt.Errorf("resolve lock candidate for %q: %w", tool.Name, lastErr)
}
