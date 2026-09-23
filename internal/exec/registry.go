package exec

import (
	"fmt"
	"sync"
)

// Registry is an injectable adapter registry: a Kind-keyed adapter set.
// It replaces the former package-global map so the registry can be
// constructed and passed explicitly instead of living in process-wide
// mutable state. The package-level Register/Lookup/Replace/RegisteredKinds
// functions below delegate to defaultRegistry, preserving behavior for the
// composition root and existing callers; New Executor instances snapshot it
// at construction (overridable per instance via WithAdapters).
//
// A Registry must not be copied after first use.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]AdapterV2
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{adapters: map[string]AdapterV2{}}
}

// Register inserts an adapter. Panics if a different adapter with the same
// Kind is already registered (fail-fast on composition conflicts before
// command execution).
func (r *Registry) Register(a AdapterV2) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.adapters[a.Kind()]; ok {
		panic(fmt.Sprintf(
			"exec: adapter %q already registered by %T", a.Kind(), existing,
		))
	}
	r.adapters[a.Kind()] = a
}

// Lookup returns the adapter for the given kind, or nil if none is
// registered. Callers always check for nil before calling methods:
//
//	if ad := exec.Lookup(kind); ad != nil {
//	    ad.Kind()
//	}
func (r *Registry) Lookup(kind string) AdapterV2 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[kind]
}

// Kinds returns the names of all registered adapters (for debug logging
// and schema validation).
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		out = append(out, k)
	}
	return out
}

// Replace inserts or replaces an adapter. Unlike Register, it does not
// panic if the kind is already registered — it overwrites the existing
// entry silently. Use for runtime reconfiguration (e.g. swapping the AUR
// adapter's helper binary).
func (r *Registry) Replace(a AdapterV2) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Kind()] = a
}

// snapshot returns a copy of the registry contents for Executor seeding.
func (r *Registry) snapshot() map[string]AdapterV2 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]AdapterV2, len(r.adapters))
	for k, a := range r.adapters {
		out[k] = a
	}
	return out
}

// defaultRegistry backs the package-level functions. The binary's
// composition root populates it before constructing commands or executors.
//
// INTENTIONAL process-global boundary (not tech debt): the registry needs
// exactly one populated instance per process, and threading it through
// every adapter call site adds no isolation (adapters are stateless
// w.r.t. the registry). Per-instance registries remain available via the
// Registry type and WithAdapters for tests; see "Process-global shims"
// in docs/architecture.md.
var defaultRegistry = NewRegistry()

// Register inserts an adapter into the default registry. Panics if a
// different adapter with the same Kind is already registered (fail-fast
// on composition conflicts before command execution).
func Register(a AdapterV2) {
	defaultRegistry.Register(a)
}

// Lookup returns the adapter for the given kind from the default registry,
// or nil if none is registered. Callers always check for nil before
// calling methods:
//
//	if ad := exec.Lookup(kind); ad != nil {
//	    ad.Kind()
//	}
func Lookup(kind string) AdapterV2 {
	return defaultRegistry.Lookup(kind)
}

// RegisteredKinds returns the names of all adapters in the default
// registry (for debug logging and schema validation).
func RegisteredKinds() []string {
	return defaultRegistry.Kinds()
}

// Replace inserts or replaces an adapter in the default registry.
// Unlike Register, it does not panic if the kind is already registered —
// it overwrites the existing entry silently. Use for runtime reconfiguration
// (e.g. swapping the AUR adapter's helper binary).
func Replace(a AdapterV2) {
	defaultRegistry.Replace(a)
}
