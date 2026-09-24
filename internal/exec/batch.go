package exec

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type batchCandidate struct {
	toolName     string
	tool         *config.Tool
	method       *config.MethodCandidate
	resolvedPlan *plan.ResolvedInstallPlan
}

func (ex *Executor) identifyBatchCandidates(ctx context.Context, level []string, s *config.Schema, report *ExecReport) (candidates []batchCandidate, remaining []string, resolutions map[string]*candidateResolutionSeed) {
	remaining = make([]string, 0, len(level))
	resolutions = make(map[string]*candidateResolutionSeed)
	for _, toolName := range level {
		tool, ok := s.Tools[toolName]
		if !ok {
			continue
		}
		// Pre-install hooks are transition-scoped. Keep those tools on the
		// serial candidate pipeline so the hook runs only after a concrete
		// candidate survives resolution and the already-installed gate.
		if len(tool.PreInstall) > 0 {
			remaining = append(remaining, toolName)
			continue
		}
		toolCtx := omitToolSecretEnvironment(ctx, tool)
		if len(tool.Methods) == 0 {
			remaining = append(remaining, toolName)
			continue
		}

		foundNative := false
		for _, method := range config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName) {
			if method.When != nil && !method.When.Match(ex.facts) {
				continue
			}
			planIntent, mismatch := candidatePlanIntent(tool, method)
			planIntent = ex.hostResolvedPlanIntent(method, planIntent)
			if mismatch != "" {
				// Batch is only an optimization. A candidate rejected by the static
				// planning boundary must fall back to the serial path, which records
				// the capability failure and may try later methods in order.
				break
			}
			adapter := ex.LookupAdapter(method.Kind)
			if adapter == nil || !adapter.Available(toolCtx, ex.probeRunner(toolName, method.Kind)) {
				continue
			}
			if method.Kind != "native" && !native.IsNativeManagerName(method.Kind) {
				break
			}
			if len(method.Requires) > 0 || len(method.Sources) > 0 {
				break
			}
			if !native.IsBatchCapable(ex.clan) {
				break
			}

			// Batch planning crosses the same read-only resolution boundary as
			// serial execution. Preserve the result for any later serial fallback
			// so a dynamic release/tag/asset lookup is never repeated.
			resolvedPlan, resolveErr := ex.resolveCandidatePlan(toolCtx, tool, method, adapter, planIntent, displayMethodKind(method))
			resolution := &candidateResolutionSeed{method: method, resolved: resolvedPlan, err: resolveErr}
			if resolveErr != nil {
				resolutions[toolName] = resolution
				break
			}
			if resolvedPlan == nil || resolvedPlan.Identity.Package == "" || !validBatchPkgName(resolvedPlan.Identity.Package) {
				resolutions[toolName] = resolution
				break
			}
			if err := adapter.CheckHostCompatibility(tool, method, resolvedPlan, ex.facts, ex.clan); err != nil {
				resolutions[toolName] = resolution
				break
			}
			presence, ok := ex.batchPresence(toolCtx, adapter, toolName, tool, method)
			if !ok {
				// A failed or broken V2 observation is not evidence that the
				// package is absent. Leave the candidate to the serial path,
				// which records the probe failure using normal method semantics.
				resolutions[toolName] = resolution
				break
			}
			if presence == plan.PresencePresent {
				ex.recordToolResult(toolCtx, &ToolResult{
					Tool: toolName, Status: StatusAlready, Method: displayMethodKind(method),
					MethodKind: method.Kind, Config: method.Config, PlanIntent: resolvedPlan,
				}, report)
				foundNative = true
				break
			}
			if !checkAvailable(toolCtx, ex.probeRunner(toolName, method.Kind), adapter, tool, method) {
				resolutions[toolName] = resolution
				break
			}
			candidates = append(candidates, batchCandidate{toolName: toolName, tool: tool, method: method, resolvedPlan: resolvedPlan})
			foundNative = true
			break
		}
		if !foundNative {
			remaining = append(remaining, toolName)
		}
	}
	return candidates, remaining, resolutions
}

// batchPresence returns false for a failed, broken, or invalid observation,
// making the caller fall back to serial execution.
func (ex *Executor) batchPresence(ctx context.Context, adapter AdapterV2, toolName string, tool *config.Tool, method *config.MethodCandidate) (plan.PresenceState, bool) {
	if adapter == nil {
		return "", false
	}
	runner := ex.probeRunner(toolName, method.Kind)
	observation, err := adapter.Observe(ctx, runner, tool, method)
	if err != nil {
		return "", false
	}
	switch observation.Presence {
	case plan.PresencePresent, plan.PresenceAbsent, plan.PresenceUnknown:
		return observation.Presence, true
	default:
		return "", false
	}
}

func displayMethodKind(method *config.MethodCandidate) string {
	if method != nil && method.Label != "" {
		return method.Label
	}
	if method == nil {
		return ""
	}
	return method.Kind
}

func (ex *Executor) providerForMethodKind(kind string) string {
	if kind == "native" {
		return ex.nativeManagerName
	}
	if native.IsNativeManagerName(kind) || methodkind.IsKnownKind(kind) {
		return kind
	}
	return ""
}

func (ex *Executor) hostResolvedPlanIntent(method *config.MethodCandidate, intent *plan.ResolvedInstallPlan) *plan.ResolvedInstallPlan {
	if intent == nil || method == nil {
		return intent
	}
	if method.Kind != "native" && !native.IsNativeManagerName(method.Kind) {
		return intent
	}
	resolved := *intent
	resolved.Identity = intent.Identity
	if pkg := pkgFromConfig(method, ex.clan); pkg != "" {
		resolved.Identity.Package = pkg
	}
	return &resolved
}

var pkgNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+-_]*$`)

func validBatchPkgName(name string) bool {
	return pkgNameRegexp.MatchString(name)
}

func (ex *Executor) batchNativeInstall(ctx context.Context, candidates []batchCandidate) bool {
	pkgs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.resolvedPlan == nil || candidate.resolvedPlan.Identity.Package == "" || !validBatchPkgName(candidate.resolvedPlan.Identity.Package) {
			return false
		}
		pkgs = append(pkgs, candidate.resolvedPlan.Identity.Package)
	}
	cmd := native.BuildBatchInstallCmd(ex.clan, pkgs)
	if cmd == nil {
		return false
	}

	timeout := ex.methodTimeout * time.Duration(max(1, len(pkgs)))
	if ex.batchTimeout > 0 {
		timeout = ex.batchTimeout
	}
	if ex.toolTimeout > 0 && timeout > ex.toolTimeout {
		timeout = ex.toolTimeout
	}
	batchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runner := ex.mutationRunner("batch", "native")
	ex.outputf("  ⚡  batch installing %d packages via %s (1 elevation)\n", len(pkgs), ex.nativeManagerName)
	ex.logDebug(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "started")
	result := runner.Run(batchCtx, cmd[0], cmd[1:]...)
	if result.Err != nil || result.ExitCode != 0 {
		ex.outputf("  ⚡  batch install failed (%s), falling back to per-tool install\n", formatBatchError(result))
		ex.logWarn(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "failed", "error", result.Err, "exit_code", result.ExitCode)
		return false
	}
	ex.logDebug(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "succeeded")
	return true
}

func formatBatchError(result run.Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	return fmt.Sprintf("exit code %d", result.ExitCode)
}
