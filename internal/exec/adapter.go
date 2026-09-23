// Package exec implements the core installation orchestrator and the AdapterV2
// interface that every method (native, cargo, git, http, …) must satisfy.
//
// Architecture
//
//	Executor.execute(schema, facts, clan)
//	  │
//	  ├─ 1. Resolve topological order of tools (internal/graph)
//	  │
//	  ├─ 2. For each tool (in graph order):
//	  │     │
//	  │     ├─ 2a. For each method (in method_order):
//	  │     │      ├─ when matches?           → skip
//	  │     │      ├─ adapter.Available()?    → no → skip
//	  │     │      ├─ adapter.Observe() says present? → yes → skip
//	  │     │      ├─ adapter.InstallResolved() → ok → SUCCESS
//	  │     │      └─ InstallResolved() failed  → try next method
//	  │     │
//	  │     ├─ 2b. If any method succeeded → run postinstall (if any)
//	  │     └─ 2c. If all failed            → log error, CONTINUE
//	  │
//	  └─ 3. Report summary (successes, failures, skips)
//
// Adding a new adapter: implement AdapterV2 and register it at the binary's
// composition root.
// The executor is generic — it only knows this interface.
package exec

import (
	"context"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// AdapterV2 is the plan-aware adapter seam: the single contract every method
// backend (native, cargo, git, http, …) implements. The executor is generic —
// it only knows this interface and never imports adapter packages directly.
//
// Every adapter provides plan resolution (ResolvePlan), presence observation
// (Observe), resolved-plan execution (InstallResolved), removal
// (Remove/CanRemove), repository availability (CheckAvailable), and host
// compatibility (CheckHostCompatibility). There are no optional capability
// interfaces: uniformity is what lets dry-run, why, and install agree on
// one resolution path per candidate.
type AdapterV2 interface {
	Kind() string
	Available(ctx context.Context, rn run.Runner) bool
	ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error)
	Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error)
	InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error
	Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error
	// CanRemove reports whether this adapter actually supports removal.
	// Adapters with no remove template configured return false, and the
	// executor falls back to a manual-removal instruction.
	CanRemove() bool
	// CheckAvailable reports whether the package this method candidate
	// targets actually exists as an installable target (e.g. in the
	// manager's repo/index), independent of whether it is already
	// installed. Adapters with no concept of "not in any repo" (cargo,
	// go, pip, git, http, ...) return true: failing open only risks a
	// wasted install attempt, whereas failing closed risks silently
	// skipping a tool that really was installable.
	CheckAvailable(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool
	// CheckHostCompatibility rejects candidates whose installability
	// depends on more than the presence of the adapter's runtime. A
	// download method, for example, may be available everywhere while
	// the resolved artifact is a distribution-specific installer such as
	// .deb. Returning an error rejects only this candidate and lets
	// normal method fallback continue. Implementations must be read-only
	// and deterministic for the supplied facts/plan; expensive source
	// resolution belongs in ResolvePlan instead. Adapters without host
	// constraints return nil.
	CheckHostCompatibility(tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan, facts *engine.Facts, clan string) error
}

// ElevationRequirer is an optional interface for adapters whose need for
// privilege elevation depends on method configuration (for example, an HTTP
// install targeting /usr/local/bin). The executor uses it to establish an
// interactive elevation session before subprocess output is captured.
type ElevationRequirer interface {
	AdapterV2
	RequiresElevation(tool *config.Tool, mc *config.MethodCandidate) bool
}

// checkAvailable consults the adapter's CheckAvailable. Adapters that
// Every registered adapter must implement this package-availability probe.
func checkAvailable(ctx context.Context, rn run.Runner, adapter AdapterV2, tool *config.Tool, mc *config.MethodCandidate) bool {
	return adapter.CheckAvailable(ctx, rn, tool, mc)
}

// SubstitutePkg replaces "{pkg}" in cmd with the package name from
// mc.Config["pkg"], falling back to tool.Name. Shared by all adapters.
func SubstitutePkg(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	pkg := tool.Name
	if p, ok := mc.Config["pkg"].(string); ok && p != "" {
		pkg = p
	}
	out := make([]string, len(cmd))
	for i, arg := range cmd {
		out[i] = strings.ReplaceAll(arg, "{pkg}", pkg)
	}
	return out
}
