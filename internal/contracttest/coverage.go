package contracttest

import "fmt"

// Phase identifies a behavioral boundary proven by a field probe.
type Phase string

const (
	PhaseResolveRuntime Phase = "resolve-runtime"
	PhaseExecute        Phase = "execute"
	PhaseVerify         Phase = "verify"
)

// Coverage describes where a field behavior is proven.
type Coverage struct {
	Consumer  string
	Rationale string
}

var coverage = map[Phase]map[string]Coverage{}

// RegisterCoverage records test-only evidence for exact kind.field keys.
// Invalid or duplicate registrations panic so evidence metadata cannot be
// silently ignored during package initialization.
func RegisterCoverage(phase Phase, entries map[string]Coverage) {
	switch phase {
	case PhaseResolveRuntime, PhaseExecute, PhaseVerify:
	default:
		panic(fmt.Sprintf("contracttest: unknown coverage phase %q", phase))
	}
	if coverage[phase] == nil {
		coverage[phase] = make(map[string]Coverage)
	}
	for key, item := range entries {
		if key == "" || item.Consumer == "" || item.Rationale == "" {
			panic(fmt.Sprintf("contracttest: incomplete %s coverage entry %q", phase, key))
		}
		if _, exists := coverage[phase][key]; exists {
			panic(fmt.Sprintf("contracttest: duplicate %s coverage entry %q", phase, key))
		}
		coverage[phase][key] = item
	}
}

// CoverageFor returns evidence for one exact kind.field key.
func CoverageFor(phase Phase, key string) (Coverage, bool) {
	item, ok := coverage[phase][key]
	return item, ok
}

// CoverageKeys returns a copy of registered keys for orphan detection.
func CoverageKeys(phase Phase) map[string]Coverage {
	out := make(map[string]Coverage, len(coverage[phase]))
	for key, item := range coverage[phase] {
		out[key] = item
	}
	return out
}
