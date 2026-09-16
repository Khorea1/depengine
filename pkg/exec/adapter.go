// Package exec implements the core installation orchestrator and the Adapter
// interface that every method (native, cargo, git, http, …) must satisfy.
//
// Architecture
//
//	Executor.execute(schema, facts, clan)
//	  │
//	  ├─ 1. Resolve topological order of tools (pkg/graph)
//	  │
//	  ├─ 2. For each tool (in graph order):
//	  │     │
//	  │     ├─ 2a. For each method (in method_order):
//	  │     │      ├─ when matches?           → skip
//	  │     │      ├─ adapter.Available()?    → no → skip
//	  │     │      ├─ adapter.Check() ok?     → yes → skip (already installed)
//	  │     │      ├─ adapter.Install()       → ok  → SUCCESS
//	  │     │      └─ Install() failed        → try next method
//	  │     │
//	  │     ├─ 2b. If any method succeeded → run postinstall (if any)
//	  │     └─ 2c. If all failed            → log error, CONTINUE
//	  │
//	  └─ 3. Report summary (successes, failures, skips)
//
// Adding a new adapter: implement Adapter, call Register in an init().
// The executor is generic — it only knows this interface.
package exec

import (
	"context"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

// Adapter is the contract that every method backend (native, cargo, git,
// http, …) implements. The executor is generic — it only knows this
// interface and never imports adapter packages directly.
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

// Remover is an optional interface that adapters can implement to support
// automated uninstallation. The executor's `remove` command checks for this
// interface and calls Remove when available. When an adapter does not implement
// Remover, the executor falls back to a manual-removal instruction.
type Remover interface {
	Adapter
	Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error
	// CanRemove reports whether this adapter actually supports removal.
	// Adapters may implement Remover but have no remove template configured.
	CanRemove() bool
}

func CanRemove(adapter Adapter) bool {
	r, ok := adapter.(Remover)
	return ok && r.CanRemove()
}

// AvailabilityChecker is an optional interface for adapters that can
// distinguish "not installed yet" from "does not exist as an installable
// target at all" for a given method candidate — e.g. a native package
// manager where the resolved package name isn't in any configured repo.
//
// Without this, Check() == false is ambiguous: the executor cannot tell
// "go ahead and install this" apart from "this was never a real candidate
// in the first place" (the schema.go `simple = [...]` shortcut injects a
// native MethodCandidate for every simple tool with no such validation,
// so any simple tool whose name isn't an actual native package — AUR-only,
// cargo-only, or simply nonexistent — looks installable until this check
// runs).
//
// Adapters that don't implement this interface are assumed to always have
// the package available, preserving prior behavior for methods that have
// no concept of "not in any repo" (cargo, go, pip, git, http, ...).
type AvailabilityChecker interface {
	Adapter
	// CheckAvailable reports whether the package this method candidate
	// targets actually exists as an installable target (e.g. in the
	// manager's repo/index), independent of whether it is already
	// installed. Implementations that cannot cheaply determine this
	// should return true (assume available): failing open only risks a
	// wasted install attempt, whereas failing closed risks silently
	// skipping a tool that really was installable.
	CheckAvailable(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool
}

// ElevationRequirer is an optional interface for adapters whose need for
// privilege elevation depends on method configuration (for example, an HTTP
// install targeting /usr/local/bin). The executor uses it to establish an
// interactive elevation session before subprocess output is captured.
type ElevationRequirer interface {
	Adapter
	RequiresElevation(tool *config.Tool, mc *config.MethodCandidate) bool
}

// checkAvailable consults AvailabilityChecker if the adapter implements
// it; otherwise it assumes the package is available, which preserves
// existing behavior for adapters that have no notion of "not in any repo".
func checkAvailable(ctx context.Context, rn run.Runner, adapter Adapter, tool *config.Tool, mc *config.MethodCandidate) bool {
	if ac, ok := adapter.(AvailabilityChecker); ok {
		return ac.CheckAvailable(ctx, rn, tool, mc)
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
