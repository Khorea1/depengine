package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/methodkind"
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
		out := applyDistribution(method, &method.Distributions[matched[0]])
		// Fail closed before execution: layering (schema+manifest) can pin
		// a fixed checksum under an inherited sidecar, or add a sidecar
		// under a fixed checksum, after every layer validated clean on its
		// own. The host-wide ValidateEffectiveIntegrity pass covers all
		// distributions; this guards the matched one even when callers
		// specialize a merged schema they never re-validated.
		if errs := effectiveIntegrityErrors(out); len(errs) > 0 {
			return nil, fmt.Errorf("specialize: invalid effective configuration for method kind %q: %s",
				method.Kind, strings.Join(errs, "; "))
		}
		return out, nil
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

// integrityKeys lists the Config keys whose combination must stay coherent
// across base inheritance, distribution overrides, and layer merges.
var integrityKeys = []string{"checksum", "checksum_url", "checksum_file_format", "signature_url", "signing_key"}

// integrityView projects the integrity keys of a materialized Config into
// the raw-map shape validateArtifactIntegrityOptions enforces, so merged
// schemas and specialized candidates reuse the exact authoring-time rule
// set instead of restating it.
func integrityView(config map[string]any) map[string]any {
	view := make(map[string]any, len(integrityKeys))
	for _, key := range integrityKeys {
		if raw, ok := config[key]; ok {
			view[key] = raw
		}
	}
	return view
}

// effectiveIntegrityErrors reports the integrity combination faults of one
// materialized candidate Config (already base+override merged). It returns
// nil when the combination is coherent.
func effectiveIntegrityErrors(mc *MethodCandidate) []string {
	if mc == nil {
		return nil
	}
	var errs []string
	view := integrityView(mc.Config)
	validateArtifactIntegrityOptions(view, "candidate", &errs)
	if contract, known := methodkind.Lookup(mc.Kind); known {
		checkEffectiveRequires(view, contract, "candidate", &errs)
	}
	return errs
}

// ValidateEffectiveIntegrity reports invalid checksum/sidecar combinations
// and distribution binary names on an already-merged schema. Each layer
// validates clean on its own; only the merge can pin a fixed checksum under
// an inherited sidecar (upper layer) or add a sidecar under a fixed checksum
// (lower layer). It runs host-independently over ALL distributions —
// including ones that match no current host — so any invalid effective
// configuration is rejected during validation, before execution. Pure: no
// network, no mutation.
func ValidateEffectiveIntegrity(s *Schema) error {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		tool := s.Tools[name]
		for _, mc := range tool.Methods {
			validateMergedMethodIntegrity(name, mc, &errs)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid effective configuration:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// validateMergedMethodIntegrity checks one merged method's base combination
// plus every distribution's effective combination (base overlaid with that
// distribution's overrides), reusing the authoring-time helpers.
func validateMergedMethodIntegrity(toolName string, mc *MethodCandidate, errs *[]string) {
	if mc == nil {
		return
	}
	base := toolName + "." + mc.Kind
	if mc.Label != "" {
		base = toolName + "." + mc.Label
	}
	baseView := integrityView(mc.Config)
	validateArtifactIntegrityOptions(baseView, base, errs)
	contract, known := methodkind.Lookup(mc.Kind)
	if known {
		checkEffectiveRequires(baseView, contract, base, errs)
	}
	for i := range mc.Distributions {
		d := &mc.Distributions[i]
		path := fmt.Sprintf("%s.distributions[%d]", base, i)
		if d.HasBinary && binaryRuleApplies(contract, known) {
			validateBinaryNameValue(d.Binary, path+".binary", errs)
		}
		effective := make(map[string]any, len(integrityKeys))
		for key, raw := range baseView {
			effective[key] = raw
		}
		if d.HasChecksum {
			effective["checksum"] = d.Checksum
		}
		if d.HasChecksumURL {
			effective["checksum_url"] = d.ChecksumURL
		}
		if d.HasChecksumFileFormat {
			effective["checksum_file_format"] = d.ChecksumFileFormat
		}
		if d.HasSignatureURL {
			effective["signature_url"] = d.SignatureURL
		}
		// signing_key is never distributable: the base value always stands.
		validateArtifactIntegrityOptions(effective, path, errs)
		if known {
			checkEffectiveRequires(effective, contract, path, errs)
		}
	}
}
