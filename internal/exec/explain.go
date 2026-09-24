package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/source"
)

func explainIntent(method *config.MethodCandidate) map[string]string {
	if method == nil {
		return nil
	}
	// Only declarative identity/target fields are surfaced. Command-bearing
	// fields and arbitrary config are deliberately excluded. Values are still
	// passed through the shared redactor for defensive programmatic callers.
	keys := []string{"pkg", "version", "registry", "git", "branch", "tag", "rev", "channel", "track", "risk", "digest", "source", "remote", "scope", "environment", "prefix", "architecture", "target", "root", "manager"}
	intent := make(map[string]string)
	for _, key := range keys {
		if value, ok := method.Config[key].(string); ok && value != "" {
			intent[key] = run.RedactSensitiveText(value)
		}
	}
	if raw, ok := method.Config["channels"]; ok {
		var channels []string
		switch values := raw.(type) {
		case []string:
			channels = append(channels, values...)
		case []any:
			for _, value := range values {
				if channel, ok := value.(string); ok && channel != "" {
					channels = append(channels, channel)
				}
			}
		}
		if len(channels) > 0 {
			intent["channels"] = run.RedactSensitiveText(strings.Join(channels, ","))
		}
	}
	if len(intent) == 0 {
		return nil
	}
	return intent
}

// ExplainTool evaluates all methods for a single tool WITHOUT installing.
// For each method it reports the status and reason: skip_when (when condition
// didn't match), skip_unavailable (no adapter or binary not on PATH),
// skip_policy (excluded by method_only), already_installed (Check passed), or
// would_install (ready to install). Inferred candidates are annotated in Reason.
//
// This is the engine behind `depengine why <tool>`.
func (ex *Executor) ExplainTool(ctx context.Context, tool *config.Tool, clan string) []MethodAttempt {
	ctx = omitToolSecretEnvironment(ctx, tool)
	orderedMethods := ex.selectedMethods(tool, clan)
	methods := orderedMethods
	if len(tool.Methods) == 0 {
		return []MethodAttempt{{Kind: "", Status: "virtual", Error: "dependency group (no methods declared)"}}
	}
	attempts := make([]MethodAttempt, 0, len(tool.Methods))
	appendAttempt := func(attempt MethodAttempt, method *config.MethodCandidate) {
		if method != nil {
			attempt.Intent = explainIntent(method)
		}
		if method != nil && method.Inferred {
			if attempt.Error != "" {
				attempt.Error += "; "
			}
			attempt.Error += "inferred candidate"
		}
		attempts = append(attempts, attempt)
	}

	for _, method := range methods {
		displayKind := method.Kind
		if method.Label != "" {
			displayKind = method.Label
		}
		attempt := MethodAttempt{Kind: method.Kind, Label: method.Label}
		planIntent, mismatch := candidatePlanIntent(tool, method)
		planIntent = ex.hostResolvedPlanIntent(method, planIntent)
		attempt.PlanIntent = planIntent

		// Reject semantic intent this method contract cannot honor before any
		// availability probe. Parsed schemas normally catch this earlier, but
		// ExplainTool also supports programmatically constructed candidates.
		if mismatch != "" {
			attempt.Status = "skip_capability"
			attempt.Error = mismatch
			appendAttempt(attempt, method)
			continue
		}

		// Check when condition.
		if method.When != nil && !method.When.Match(ex.facts) {
			attempt.Status = "skip_when"
			attempt.Error = fmt.Sprintf("when condition not met: %+v", method.When)
			appendAttempt(attempt, method)
			continue
		}
		adapter := ex.LookupAdapter(method.Kind)
		if adapter == nil {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("no adapter registered for kind %q", displayKind)
			appendAttempt(attempt, method)
			continue
		}

		// Check if the adapter is available on this system.
		if !adapter.Available(ctx, ex.rn) {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("adapter %q not available (binary not on PATH)", displayKind)
			appendAttempt(attempt, method)
			continue
		}

		// Same single read-only resolution point as Execute: dry-run, why,
		// and real install obtain the concrete identity from this function.
		resolvedPlan, resolveErr := ex.resolveCandidatePlan(ctx, tool, method, adapter, planIntent, displayKind)
		if resolveErr != nil {
			attempt.Status = "failed"
			attempt.Error = resolveErr.Error()
			appendAttempt(attempt, method)
			continue
		}
		attempt.PlanIntent = resolvedPlan

		if compatibilityErr := adapter.CheckHostCompatibility(tool, method, resolvedPlan, ex.facts, clan); compatibilityErr != nil {
			attempt.Status = "skip_unavailable"
			attempt.Error = compatibilityErr.Error()
			appendAttempt(attempt, method)
			continue
		}

		verification, verifyErr := ex.VerifyResolvedCandidate(ctx, tool, method, resolvedPlan)
		if verifyErr != nil {
			attempt.Status = "failed"
			attempt.Error = fmt.Sprintf("%s: verify desired state: %s", displayKind, run.RedactSensitiveText(verifyErr.Error()))
			appendAttempt(attempt, method)
			continue
		}
		switch verification.State {
		case plan.StateSatisfied:
			attempt.Status = "already_installed"
			appendAttempt(attempt, method)
			continue
		case plan.StateAbsent:
			// Continue to source and target availability checks.
		case plan.StateDrifted:
			attempt.Error = verificationDetail(verification)
		case plan.StateUnknown, plan.StateBroken:
			detail := verificationDetail(verification)
			if detail == "" {
				detail = string(verification.State)
			}
			attempt.Status = "failed"
			attempt.Error = fmt.Sprintf("%s: desired state %s: %s", displayKind, verification.State, run.RedactSensitiveText(detail))
			appendAttempt(attempt, method)
			continue
		default:
			attempt.Status = "failed"
			attempt.Error = fmt.Sprintf("%s: invalid verification state %q", displayKind, verification.State)
			appendAttempt(attempt, method)
			continue
		}

		// Source presence is read-only candidate selection, mirroring
		// Execute: a missing declared source makes the repository/index
		// answer inconclusive, so availability is deferred rather than
		// rejecting the candidate on a stale index.
		if ex.sources == nil {
			ex.sources = source.NewManager(ex.rn, true)
		}
		sourceProbe, probeErr := ex.probeCandidateSources(ctx, method.Sources)
		if probeErr != nil {
			attempt.Status = "failed"
			attempt.Error = probeErr.Error()
			appendAttempt(attempt, method)
			continue
		}
		if len(sourceProbe.missing) == 0 {
			// Match the real install planner's availability semantics. Check()==false
			// means only "not currently satisfied"; AvailabilityChecker can further
			// distinguish that from "this candidate does not exist in the configured
			// repo/index". Without this check, `why` can call a phantom native
			// candidate ready even though Execute will deterministically reject it.
			if !checkAvailable(ctx, ex.probeRunner(tool.Name, displayKind), adapter, tool, method) {
				attempt.Status = "skip_unavailable"
				attempt.Error = fmt.Sprintf("%s: package not found in repo/index", displayKind)
				appendAttempt(attempt, method)
				continue
			}
		}
		if sourceProbe.preparationPlan != nil && resolvedPlan != nil {
			projected := *resolvedPlan
			projected.Preparation = sourceProbe.preparationPlan
			resolvedPlan = &projected
			attempt.PlanIntent = resolvedPlan
		}

		// Method is ready and would be attempted.
		attempt.Status = "would_install"
		var prerequisites []string
		if len(method.Requires) > 0 {
			prerequisites = append(prerequisites, "requires "+strings.Join(method.Requires, ", "))
		}
		for _, item := range sourceProbe.missing {
			prerequisites = append(prerequisites, "missing source "+item.Kind+":"+item.Name)
		}
		if len(prerequisites) > 0 {
			attempt.Error = strings.Join(prerequisites, "; ")
		}
		appendAttempt(attempt, method)
	}

	if len(tool.MethodOnly) > 0 {
		selected := make(map[*config.MethodCandidate]bool, len(orderedMethods))
		for _, method := range orderedMethods {
			selected[method] = true
		}
		for _, method := range tool.Methods {
			if selected[method] {
				continue
			}
			attempt := MethodAttempt{
				Kind:   method.Kind,
				Label:  method.Label,
				Status: "skip_policy",
				Error:  "excluded by method_only",
			}
			attempt.PlanIntent, _ = candidatePlanIntent(tool, method)
			appendAttempt(attempt, method)
		}
	}

	return attempts
}

// CheckDesiredState probes method candidates in install order and returns the
// first candidate whose resolved identity satisfies desired state. It shares
// candidate selection and V2 plan resolution with ExplainTool, but stops after
// `check` never probes package sources or install availability. When live is
// false, unavailable adapters are skipped; live bypasses that gate.
type CheckResult struct {
	Method       string                  `json:"method,omitempty"`
	Verification plan.VerificationResult `json:"verification"`
}

func (ex *Executor) CheckDesiredState(ctx context.Context, tool *config.Tool, clan string, live bool) (CheckResult, error) {
	ctx = omitToolSecretEnvironment(ctx, tool)
	var first CheckResult
	haveFirst := false
	for _, method := range ex.selectedMethods(tool, clan) {
		if method.When != nil && !method.When.Match(ex.facts) {
			continue
		}
		adapter := ex.LookupAdapter(method.Kind)
		if adapter == nil {
			continue
		}
		probe := ex.probeRunner(tool.Name, method.Kind)
		if !live && !adapter.Available(ctx, probe) {
			continue
		}
		intent, mismatch := candidatePlanIntent(tool, method)
		if mismatch != "" {
			continue
		}
		intent = ex.hostResolvedPlanIntent(method, intent)
		resolved, err := ex.resolveCandidatePlan(ctx, tool, method, adapter, intent, method.Kind)
		if err != nil {
			continue
		}
		verification, err := ex.VerifyResolvedCandidate(ctx, tool, method, resolved)
		if err != nil {
			checked := CheckResult{
				Method: method.Kind,
				Verification: plan.VerificationResult{
					State:  plan.StateBroken,
					Detail: err.Error(),
				},
			}
			if !haveFirst || first.Verification.State == plan.StateAbsent {
				first, haveFirst = checked, true
			}
			continue
		}
		checked := CheckResult{Method: method.Kind, Verification: verification}
		if verification.State == plan.StateSatisfied {
			return checked, nil
		}
		if !haveFirst || first.Verification.State == plan.StateAbsent && verification.State != plan.StateAbsent {
			first, haveFirst = checked, true
		}
	}
	if haveFirst {
		return first, nil
	}
	return CheckResult{Verification: plan.VerificationResult{State: plan.StateAbsent}}, nil
}

// CheckInstalled remains a compatibility wrapper for internal callers.
func (ex *Executor) CheckInstalled(ctx context.Context, tool *config.Tool, clan string, live bool) (string, bool) {
	checked, err := ex.CheckDesiredState(ctx, tool, clan, live)
	return checked.Method, err == nil && checked.Verification.State == plan.StateSatisfied
}

func (ex *Executor) selectedMethods(tool *config.Tool, clan string) []*config.MethodCandidate {
	ex.SetHostContext(clan)
	return config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName)
}

// SetHostContext selects host-specific defaults used during candidate planning.
func (ex *Executor) SetHostContext(clan string) {
	ex.clan = clan
	ex.nativeManagerName = ""
	if mgr, ok := native.Lookup(clan); ok {
		ex.nativeManagerName = mgr.Name
	}
}
