// Package exec implements the core installation orchestrator and the Adapter
// interface that every method (native, cargo, git, http, …) must satisfy.
//
// Architecture
//
//	Executor.orquestrar(schema, facts, clan)
//	  │
//	  ├─ 1. Resolver ordem topológica das tools (pkg/graph)
//	  │
//	  ├─ 2. Para cada tool (na ordem do grafo):
//	  │     │
//	  │     ├─ 2a. Para cada método (na ordem de method_order):
//	  │     │      ├─ when bate?           → PULA
//	  │     │      ├─ adapter.Available()? → não → PULA
//	  │     │      ├─ adapter.Check() ok?  → sim → PULA (já instalado)
//	  │     │      ├─ adapter.Install()    → ok  → SUCESSO
//	  │     │      └─ Install() falhou     → tenta próximo método
//	  │     │
//	  │     ├─ 2b. Se algum método sucedeu → executa postinstall (se houver)
//	  │     └─ 2c. Se todos falharam       → log erro, CONTINUA
//	  │
//	  └─ 3. Reportar resumo (sucessos, falhas, pulados)
//
// Adding a new adapter: implement Adapter, call Register in an init().
// The executor is generic — it only knows this interface.
package exec

import (
	"context"
	"strings"

	"depengine/pkg/run"
	"depengine/pkg/schema"
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
	Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool

	// Install runs the installation command for this method. Returns
	// nil on success, an error describing what went wrong on failure.
	// The executor handles fallback when Install fails.
	Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error
}

// Remover is an optional interface that adapters can implement to support
// automated uninstallation. The executor's `remove` command checks for this
// interface and calls Remove when available. When an adapter does not implement
// Remover, the executor falls back to a manual-removal instruction.
type Remover interface {
	Adapter
	Remove(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error
	// CanRemove reports whether this adapter actually supports removal.
	// Adapters may implement Remover but have no remove template configured.
	CanRemove() bool
}

func CanRemove(adapter Adapter) bool {
	r, ok := adapter.(Remover)
	return ok && r.CanRemove()
}

// SubstitutePkg replaces "{pkg}" in cmd with the package name from
// mc.Config["pkg"], falling back to tool.Name. Shared by all adapters.
func SubstitutePkg(cmd []string, tool *schema.Tool, mc *schema.MethodCandidate) []string {
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
