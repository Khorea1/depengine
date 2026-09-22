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
//	  │     │      ├─ adapter Observe()/Check() says present? → yes → skip
//	  │     │      ├─ adapter.Install()       → ok  → SUCCESS
//	  │     │      └─ Install() failed        → try next method
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

// Adapter is the legacy contract that every method backend (native, cargo,
// git, http, …) implements. It is superseded by AdapterV2, which folds in
// plan resolution, observation, resolved installation, removal,
// availability, and host compatibility. Adapter remains only until the
// cutover removes the legacy dispatch paths; new code must use AdapterV2.
type Adapter interface {
	// Kind returns the method identifier: "native", "cargo", "git", …
	// Must be a stable value; used as the registry key.
	Kind() string

	// Available reports whether the runtime/binary for this adapter
	// exists on the current system. For native: which apt/pacman/etc.
	// For cargo: which cargo. Must be cheap (one subprocess).
	Available(ctx context.Context, rn run.Runner) bool

	// Check reports whether the tool managed by this adapter is already
	// installed. Uses the adapter's check command (e.g. dpkg -s,
	// cargo install --list | grep). Exit 0 → installed.
	Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool

	// Install runs the installation command for this method. Returns
	// nil on success, an error describing what went wrong on failure.
	// The executor handles fallback when Install fails.
	Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error
}

// AdapterV2 is the plan-aware adapter seam: the single contract every method
// backend (native, cargo, git, http, …) implements. The executor is generic —
// it only knows this interface and never imports adapter packages directly.
//
// Besides the legacy Kind/Available/Check/Install entry points (retained as
// adapter-level primitives consumed by Observe and InstallResolved), every
// adapter provides plan resolution (ResolvePlan), presence observation
// (Observe), resolved-plan execution (InstallResolved), removal
// (Remove/CanRemove), repository availability (CheckAvailable), and host
// compatibility (CheckHostCompatibility). There are no optional capability
// interfaces: uniformity is what lets dry-run, why, and install agree on
// one resolution path per candidate.
type AdapterV2 interface {
	Adapter
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
// predate the AdapterV2 cutover are assumed available, preserving legacy
// behavior until the legacy dispatch paths are removed.
func checkAvailable(ctx context.Context, rn run.Runner, adapter Adapter, tool *config.Tool, mc *config.MethodCandidate) bool {
	if v2, ok := adapter.(AdapterV2); ok {
		return v2.CheckAvailable(ctx, rn, tool, mc)
	}
	return true
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
