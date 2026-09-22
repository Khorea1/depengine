package plan

import (
	"fmt"
	"reflect"
)

// ValidateResolution enforces the planner/executor invariant for runtime
// resolution: a resolver may enrich identity dimensions (Version, Revision,
// Digest, Source, Artifacts) but must never rewrite the original candidate
// intent into something else (tool foo -> bar, github -> http, swapped
// RequestedVersion, ...).
//
// This keeps PlanResolver from becoming a second hidden planning language
// inside adapters.
func ValidateResolution(intent, resolved ResolvedInstallPlan) error {
	if err := resolved.Validate(); err != nil {
		return fmt.Errorf("resolved plan: %w", err)
	}
	if resolved.Tool.Name != intent.Tool.Name {
		return fmt.Errorf("resolver changed tool %q to %q", intent.Tool.Name, resolved.Tool.Name)
	}
	if resolved.Candidate.Method != intent.Candidate.Method {
		return fmt.Errorf("resolver changed method %q to %q", intent.Candidate.Method, resolved.Candidate.Method)
	}
	if resolved.Candidate.Explicit != intent.Candidate.Explicit {
		return fmt.Errorf("resolver changed candidate explicit flag")
	}
	if !reflect.DeepEqual(intent.Identity.RequestedVersion, resolved.Identity.RequestedVersion) {
		return fmt.Errorf("resolver must not rewrite requested version intent")
	}
	// Structural identity dimensions other than the enrichable set must be
	// preserved exactly. Enrichable: Version, Revision, Digest, Source,
	// Artifacts.
	stable := func(i ResolvedIdentity) ResolvedIdentity {
		out := i
		out.Version = ""
		out.Revision = ""
		out.Digest = ""
		out.Source = ""
		return out
	}
	if !reflect.DeepEqual(stable(intent.Identity), stable(resolved.Identity)) {
		return fmt.Errorf("resolver rewrote stable identity fields (only version, revision, digest, source, and artifacts may be enriched)")
	}
	return nil
}
