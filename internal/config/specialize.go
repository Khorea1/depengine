package config

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/platform"
)

// AmbiguousDistributionsError reports that more than one distribution of a
// method matches the host. It is terminal: callers must abort the tool
// instead of falling back silently. Use errors.As to detect it.
type AmbiguousDistributionsError struct {
	Kind       string
	Conditions []string
}

func (e *AmbiguousDistributionsError) Error() string {
	return fmt.Sprintf("specialize: %d distributions match this host for method kind %q, want exactly one: %s",
		len(e.Conditions), e.Kind, strings.Join(e.Conditions, "; "))
}

// SpecializeDistribution selects the single host-compatible distribution of
// a method candidate and materializes its effective fields (see
// docs/specs/github-distributions.md).
//
// Pure: no network, no host mutation, never modifies method. It returns a
// conventional candidate copy that downstream (planner, adapters, why,
// state, lock) consumes exactly like a hand-written one:
//
//   - no distributions → method unchanged (same pointer, zero behavior delta);
//   - method-level when mismatch, or zero matching distributions → nil, nil
//     (ineligible: the caller tries the next method, like skip_when);
//   - more than one match → hard error (ambiguous manifest, abort the tool);
//   - exactly one match → specialized copy.
//
// Scalar overrides replace the base value; entrypoints replaces the whole
// map (no deep merge). Config values keep their decoded TOML shapes
// (map[string]any for entrypoints, int64 for strip_components) so existing
// readers keep working unchanged.
func SpecializeDistribution(method *MethodCandidate, facts *platform.Facts) (*MethodCandidate, error) {
	if method == nil {
		return nil, fmt.Errorf("specialize: method candidate is required")
	}
	if len(method.Distributions) == 0 {
		return method, nil
	}
	if method.When != nil && !method.When.Match(facts) {
		return nil, nil
	}
	var matched []int
	for i := range method.Distributions {
		if method.Distributions[i].When.Match(facts) {
			matched = append(matched, i)
		}
	}
	switch len(matched) {
	case 0:
		return nil, nil
	case 1:
		return applyDistribution(method, &method.Distributions[matched[0]]), nil
	default:
		conditions := make([]string, 0, len(matched))
		for _, i := range matched {
			conditions = append(conditions, fmt.Sprintf("distributions[%d] (%s)", i, method.Distributions[i].When))
		}
		return nil, &AmbiguousDistributionsError{Kind: method.Kind, Conditions: conditions}
	}
}

// applyDistribution returns a copy of method with one distribution's
// overrides materialized into Config. Absent fields inherit the base.
// The copy carries no Distributions: downstream sees exactly what a
// hand-written conventional candidate looks like, and re-specialization
// stays a no-op (nothing left to select).
func applyDistribution(method *MethodCandidate, d *Distribution) *MethodCandidate {
	out := cloneMethod(method)
	out.Distributions = nil
	set := func(key string, value any) {
		if out.Config == nil {
			out.Config = map[string]any{}
		}
		out.Config[key] = value
	}
	if d.HasAsset {
		if d.Asset.Regex != "" {
			set("asset", map[string]any{"regex": d.Asset.Regex})
		} else {
			set("asset", d.Asset.Template)
		}
	}
	if d.HasChecksum {
		set("checksum", d.Checksum)
	}
	if d.HasChecksumURL {
		set("checksum_url", d.ChecksumURL)
	}
	if d.HasChecksumFileFormat {
		set("checksum_file_format", d.ChecksumFileFormat)
	}
	if d.HasSignatureURL {
		set("signature_url", d.SignatureURL)
	}
	if d.HasBinary {
		set("binary", d.Binary)
	}
	if d.HasExtractTo {
		set("extract_to", d.ExtractTo)
	}
	if d.HasStripComponents {
		set("strip_components", d.StripComponents)
	}
	if d.Entrypoints != nil {
		replaced := make(map[string]any, len(d.Entrypoints))
		for name, target := range d.Entrypoints {
			replaced[name] = target
		}
		set("entrypoints", replaced)
	}
	return out
}
