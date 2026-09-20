package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/Khorea1/depengine/pkg/run"
)

// VerificationState describes whether observed host state reconciles with a
// resolved installation plan. It deliberately has more states than a boolean
// installed/not-installed check so callers can preserve drift and uncertainty.
type VerificationState string

const (
	StateSatisfied VerificationState = "satisfied"
	StateAbsent    VerificationState = "absent"
	StateDrifted   VerificationState = "drifted"
	StateUnknown   VerificationState = "unknown"
	StateBroken    VerificationState = "broken"
)

// PresenceState is the adapter's observation about whether the target exists.
// PresenceUnknown is for managers that cannot reliably establish presence;
// PresenceBroken means verification itself failed and should not be treated as
// a normal absence or drift.
type PresenceState string

const (
	PresencePresent PresenceState = "present"
	PresenceAbsent  PresenceState = "absent"
	PresenceUnknown PresenceState = "unknown"
	PresenceBroken  PresenceState = "broken"
)

// IdentityField names one comparable dimension of desired/observed state.
type IdentityField string

const (
	FieldPackage      IdentityField = "package"
	FieldVersion      IdentityField = "version"
	FieldRevision     IdentityField = "revision"
	FieldDigest       IdentityField = "digest"
	FieldSource       IdentityField = "source"
	FieldRegistry     IdentityField = "registry"
	FieldScope        IdentityField = "scope"
	FieldEnvironment  IdentityField = "environment"
	FieldArchitecture IdentityField = "architecture"
	FieldPlatform     IdentityField = "platform"
)

var identityFields = []IdentityField{
	FieldPackage,
	FieldVersion,
	FieldRevision,
	FieldDigest,
	FieldSource,
	FieldRegistry,
	FieldScope,
	FieldEnvironment,
	FieldArchitecture,
	FieldPlatform,
}

// ObservedIdentity contains identity reported from the host. A field is only
// authoritative when it also appears in Observation.KnownFields; an empty
// value can itself be a known value for dimensions where that is meaningful.
type ObservedIdentity struct {
	Package      string             `json:"package,omitempty"`
	Version      string             `json:"version,omitempty"`
	Revision     string             `json:"revision,omitempty"`
	Digest       string             `json:"digest,omitempty"`
	Source       string             `json:"source,omitempty"`
	Registry     string             `json:"registry,omitempty"`
	Scope        string             `json:"scope,omitempty"`
	Environment  *EnvironmentTarget `json:"environment,omitempty"`
	Architecture string             `json:"architecture,omitempty"`
	Platform     string             `json:"platform,omitempty"`
}

// Observation is the adapter-neutral output of a host-state probe.
type Observation struct {
	Presence    PresenceState    `json:"presence"`
	Identity    ObservedIdentity `json:"identity,omitempty"`
	KnownFields []IdentityField  `json:"known_fields,omitempty"`
	Detail      string           `json:"detail,omitempty"`
}

// IdentityDrift records one desired-state dimension that is known and differs
// from the resolved plan.
type IdentityDrift struct {
	Field    IdentityField `json:"field"`
	Desired  string        `json:"desired,omitempty"`
	Observed string        `json:"observed,omitempty"`
}

// VerificationResult is the shared reconciliation result consumed by status,
// install idempotency and upgrade planning.
type VerificationResult struct {
	State        VerificationState `json:"state"`
	Observed     ObservedIdentity  `json:"observed,omitempty"`
	KnownFields  []IdentityField   `json:"known_fields,omitempty"`
	Drift        []IdentityDrift   `json:"drift,omitempty"`
	Unverifiable []IdentityField   `json:"unverifiable,omitempty"`
	Detail       string            `json:"detail,omitempty"`
}

// Reconcile compares an observation with the concrete identity resolved in the
// plan. Comparisons are exact by design: ecosystem-specific version constraint
// satisfaction belongs in the resolver, which must place the concrete desired
// identity in ResolvedIdentity before verification.
func Reconcile(desired ResolvedIdentity, observation Observation) VerificationResult {
	result := VerificationResult{
		Observed: observation.Identity,
		Detail:   observation.Detail,
	}
	if err := validateFieldSet("observation known", observation.KnownFields); err != nil {
		result.State = StateBroken
		result.Detail = err.Error()
		return result
	}
	result.KnownFields = normalizedFields(observation.KnownFields)

	switch observation.Presence {
	case PresenceAbsent:
		result.State = StateAbsent
		return result
	case PresenceUnknown:
		result.State = StateUnknown
		result.Unverifiable = desiredIdentityFields(desired)
		return result
	case PresenceBroken:
		result.State = StateBroken
		return result
	case PresencePresent:
		// Continue with identity reconciliation.
	default:
		result.State = StateBroken
		result.Detail = fmt.Sprintf("invalid presence state %q", observation.Presence)
		return result
	}

	known := make(map[IdentityField]struct{}, len(result.KnownFields))
	for _, field := range result.KnownFields {
		known[field] = struct{}{}
	}
	for _, field := range desiredIdentityFields(desired) {
		if _, ok := known[field]; !ok {
			result.Unverifiable = append(result.Unverifiable, field)
			continue
		}
		want := desiredField(desired, field)
		got := observedField(observation.Identity, field)
		if want != got {
			result.Drift = append(result.Drift, IdentityDrift{Field: field, Desired: want, Observed: got})
		}
	}

	switch {
	case len(result.Drift) > 0:
		result.State = StateDrifted
	case len(result.Unverifiable) > 0:
		result.State = StateUnknown
	default:
		result.State = StateSatisfied
	}
	return result
}

// MarshalJSON keeps verification/debug output credential-safe. Verification
// probes may echo configured registry/source URLs or command failures, so their
// diagnostic projection receives the same defense-in-depth redaction as plans.
func (r VerificationResult) MarshalJSON() ([]byte, error) {
	type plain VerificationResult
	safe := r
	safe.Observed.Source = run.RedactSensitiveText(safe.Observed.Source)
	safe.Observed.Registry = run.RedactSensitiveText(safe.Observed.Registry)
	safe.Detail = run.RedactSensitiveText(safe.Detail)
	for i := range safe.Drift {
		safe.Drift[i].Desired = run.RedactSensitiveText(safe.Drift[i].Desired)
		safe.Drift[i].Observed = run.RedactSensitiveText(safe.Drift[i].Observed)
	}
	return json.Marshal(plain(safe))
}

// Validate checks invariants on a verification result before it is persisted,
// rendered, or consumed by upgrade/install logic.
func (r VerificationResult) Validate() error {
	switch r.State {
	case StateSatisfied, StateAbsent, StateDrifted, StateUnknown, StateBroken:
	default:
		return fmt.Errorf("invalid verification state %q", r.State)
	}
	if err := validateFieldSet("known", r.KnownFields); err != nil {
		return err
	}
	if err := validateFieldSet("unverifiable", r.Unverifiable); err != nil {
		return err
	}
	for _, drift := range r.Drift {
		if !validIdentityField(drift.Field) {
			return fmt.Errorf("invalid drift field %q", drift.Field)
		}
		if drift.Desired == drift.Observed {
			return fmt.Errorf("drift field %q has identical desired and observed values", drift.Field)
		}
	}

	switch r.State {
	case StateSatisfied:
		if len(r.Drift) != 0 || len(r.Unverifiable) != 0 {
			return errors.New("satisfied verification cannot contain drift or unverifiable fields")
		}
	case StateDrifted:
		if len(r.Drift) == 0 {
			return errors.New("drifted verification requires at least one drift field")
		}
	case StateUnknown:
		if len(r.Unverifiable) == 0 && r.Detail == "" {
			return errors.New("unknown verification requires unverifiable fields or detail")
		}
	case StateAbsent:
		if len(r.Drift) != 0 {
			return errors.New("absent verification cannot contain identity drift")
		}
	case StateBroken:
		if r.Detail == "" {
			return errors.New("broken verification requires detail")
		}
	}
	return nil
}

func desiredIdentityFields(identity ResolvedIdentity) []IdentityField {
	fields := make([]IdentityField, 0, len(identityFields))
	for _, field := range identityFields {
		if desiredField(identity, field) != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

func normalizedFields(fields []IdentityField) []IdentityField {
	seen := make(map[IdentityField]struct{}, len(fields))
	out := make([]IdentityField, 0, len(fields))
	for _, canonical := range identityFields {
		if slices.Contains(fields, canonical) {
			if _, ok := seen[canonical]; !ok {
				out = append(out, canonical)
				seen[canonical] = struct{}{}
			}
		}
	}
	return out
}

func validateFieldSet(name string, fields []IdentityField) error {
	seen := make(map[IdentityField]struct{}, len(fields))
	for _, field := range fields {
		if !validIdentityField(field) {
			return fmt.Errorf("invalid %s identity field %q", name, field)
		}
		if _, ok := seen[field]; ok {
			return fmt.Errorf("duplicate %s identity field %q", name, field)
		}
		seen[field] = struct{}{}
	}
	return nil
}

func validIdentityField(field IdentityField) bool {
	return slices.Contains(identityFields, field)
}

func desiredField(identity ResolvedIdentity, field IdentityField) string {
	switch field {
	case FieldPackage:
		return identity.Package
	case FieldVersion:
		return identity.Version
	case FieldRevision:
		return identity.Revision
	case FieldDigest:
		return identity.Digest
	case FieldSource:
		return identity.Source
	case FieldRegistry:
		return identity.Registry
	case FieldScope:
		return identity.Scope
	case FieldEnvironment:
		if identity.Environment != nil {
			return identity.Environment.CanonicalKey()
		}
		return ""
	case FieldArchitecture:
		return identity.Architecture
	case FieldPlatform:
		return identity.Platform
	default:
		return ""
	}
}

func observedField(identity ObservedIdentity, field IdentityField) string {
	switch field {
	case FieldPackage:
		return identity.Package
	case FieldVersion:
		return identity.Version
	case FieldRevision:
		return identity.Revision
	case FieldDigest:
		return identity.Digest
	case FieldSource:
		return identity.Source
	case FieldRegistry:
		return identity.Registry
	case FieldScope:
		return identity.Scope
	case FieldEnvironment:
		if identity.Environment != nil {
			return identity.Environment.CanonicalKey()
		}
		return ""
	case FieldArchitecture:
		return identity.Architecture
	case FieldPlatform:
		return identity.Platform
	default:
		return ""
	}
}
