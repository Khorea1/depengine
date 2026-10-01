package exec

import (
	"fmt"
	"sync"
)

// Registry is an injectable adapter registry: a Kind-keyed adapter set.
// The package-level Register/Lookup/RegisteredKinds functions delegate to
// defaultRegistry for composition-root bootstrap and lookups. New Executor
// instances snapshot that registry at construction and can apply per-instance
// overrides with WithAdapters.
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

// Replace inserts or replaces an adapter in this registry. Unlike Register,
// it does not panic if the kind is already registered — it overwrites the
// existing entry silently. This affects only this Registry; Executors seeded
// from defaultRegistry are unaffected.
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

// defaultRegistry backs package-level bootstrap registration and lookup. The
// composition root populates it before constructing commands or executors;
// each Executor then keeps its own snapshot and optional instance overrides.
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
