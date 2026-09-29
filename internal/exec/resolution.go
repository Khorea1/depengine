package exec

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// resolveCandidatePlan is the single read-only resolution point shared by
// dry-run, why, and real install. It performs no source, prerequisite, or
// package mutations.
//
// Contract:
//
//	intent estático
//	    ↓
//	AdapterV2.ResolvePlan()
//	    ↓
//	plan.ValidateResolution (intent preservation + resolved.Validate)
//
// The returned plan is the concrete executable identity. No layer below the
// executor may resolve GitHub, {latest}, tags, or assets again after this
// point. Compatibility against the concrete plan stays with the caller so
// resolve failures (failed) and host incompatibility (skip_unavailable) keep
// distinct attempt statuses.
func (ex *Executor) resolveCandidatePlan(
	ctx context.Context,
	tool *config.Tool,
	method *config.MethodCandidate,
	adapter AdapterV2,
	intent *plan.ResolvedInstallPlan,
	displayKind string,
) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, nil
	}
	var expected *plan.LockProjection
	if ex.lockDocument != nil {
		entry, err := ex.lockDocument.EntryForPlan(*intent)
		if err != nil {
			return intent, fmt.Errorf("%s: frozen lock: %w", displayKind, err)
		}
		pinned, err := ex.lockDocument.PinnedPlanFor(*intent)
		if err != nil {
			return intent, fmt.Errorf("%s: frozen lock: %w", displayKind, err)
		}
		intent = &pinned
		expected = &entry
	}
	resolveCtx, err := ex.githubCredentialContext(ctx, method)
	if err != nil {
		return intent, fmt.Errorf("%s: %w", displayKind, err)
	}
	resolved, err := adapter.ResolvePlan(resolveCtx, ex.probeRunner(tool.Name, displayKind), tool, method, intent)
	if err != nil {
		return intent, fmt.Errorf("%s: resolve plan: %w", displayKind, err)
	}
	if resolved == nil {
		return intent, fmt.Errorf("%s: resolve plan: resolver returned nil plan", displayKind)
	}
	if err := plan.ValidateResolution(*intent, *resolved); err != nil {
		return intent, fmt.Errorf("%s: resolve plan: %w", displayKind, err)
	}
	if expected != nil {
		if err := plan.VerifyResolvedPlanAgainstLock(*expected, *resolved); err != nil {
			return intent, fmt.Errorf("%s: frozen lock: %w", displayKind, err)
		}
	}
	return resolved, nil
}

// sourcesForResolvedPlan overlays immutable source revisions from a locked plan
// onto runtime source preparation without mutating the parsed schema.
func sourcesForResolvedPlan(configured []config.Source, resolved *plan.ResolvedInstallPlan) ([]config.Source, error) {
	if resolved == nil || len(configured) == 0 || len(resolved.Sources) == 0 {
		return configured, nil
	}
	result := configured
	cloned := false
	for i := range configured {
		for _, locked := range resolved.Sources {
			if locked.Kind != configured[i].Kind || locked.Name != configured[i].Name || locked.Revision == "" {
				continue
			}
			if configured[i].Revision != "" && configured[i].Revision != locked.Revision {
				return nil, fmt.Errorf("source %s:%s revision changed from locked %s", configured[i].Kind, configured[i].Name, locked.Revision)
			}
			if configured[i].Revision == locked.Revision {
				break
			}
			if !cloned {
				result = append([]config.Source(nil), configured...)
				cloned = true
			}
			result[i].Revision = locked.Revision
			break
		}
	}
	return result, nil
}

// observeResolvedCandidate is the single read-only desired-state observation
// point shared by install, dry-run, check/status, and explanation after plan
// resolution. It always observes the concrete resolved target rather than the
// unresolved method spelling.
//
// Adapter probe errors and invalid presence values are canonicalized to
// PresenceBroken. Diagnostic detail is redacted here so every caller gets the
// same fail-closed semantics without independently handling sensitive output.
func (ex *Executor) observeResolvedCandidate(
	ctx context.Context,
	tool *config.Tool,
	method *config.MethodCandidate,
	adapter AdapterV2,
	resolved *plan.ResolvedInstallPlan,
	displayKind string,
) plan.Observation {
	observation, err := adapter.Observe(
		ctx,
		ex.probeRunner(tool.Name, displayKind),
		tool,
		methodForResolvedTarget(method, resolved),
	)
	if err != nil {
		return plan.Observation{
			Presence: plan.PresenceBroken,
			Detail:   "observe presence failed: " + run.RedactSensitiveText(err.Error()),
		}
	}

	observation.Detail = run.RedactSensitiveText(observation.Detail)
	switch observation.Presence {
	case plan.PresencePresent, plan.PresenceAbsent, plan.PresenceUnknown:
		return observation
	case plan.PresenceBroken:
		if observation.Detail == "" {
			observation.Detail = "presence observation is broken"
		}
		return observation
	default:
		return plan.Observation{
			Presence: plan.PresenceBroken,
			Detail:   fmt.Sprintf("invalid presence observation %q", observation.Presence),
		}
	}
}
