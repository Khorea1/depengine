package exec

import "fmt"

// Global adapter registry. Adapters register themselves in init().
// The executor looks up adapters by kind at runtime.
var adapters = map[string]Adapter{}

// Register inserts an adapter into the global registry. Panics if a
// different adapter with the same Kind is already registered (fail-fast
// on conflict at init time, never a runtime error).
func Register(a Adapter) {
	if existing, ok := adapters[a.Kind()]; ok {
		panic(fmt.Sprintf(
			"exec: adapter %q already registered by %T", a.Kind(), existing,
		))
	}
	adapters[a.Kind()] = a
}

// Lookup returns the adapter for the given kind, or nil if none is
// registered. Callers always check for nil before calling methods:
//
//	if ad := exec.Lookup(kind); ad != nil {
//	    ad.Install(...)
//	}
func Lookup(kind string) Adapter {
	return adapters[kind]
}

// RegisteredKinds returns the names of all registered adapters (for
// debug logging and schema validation).
func RegisteredKinds() []string {
	out := make([]string, 0, len(adapters))
	for k := range adapters {
		out = append(out, k)
	}
	return out
}
