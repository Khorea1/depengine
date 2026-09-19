// Package plan defines depengine's canonical resolved installation semantics.
//
// The model is deliberately adapter-neutral and contains only resolved intent;
// it must not contain literal credentials. JSON serialization is additionally
// redacted as a defense-in-depth diagnostic boundary.
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/run"
)

// CurrentVersion is the internal ResolvedInstallPlan schema version.
const CurrentVersion = 1

// OperationEffect classifies whether a planned operation is observational or
// may mutate host state.
type OperationEffect string

const (
	EffectReadOnly OperationEffect = "read_only"
	EffectMutation OperationEffect = "mutation"
)

// ErrorClass is a stable category for planner failures.
type ErrorClass string

const (
	ErrorInvalidManifest       ErrorClass = "invalid_manifest"
	ErrorUnsupportedCapability ErrorClass = "unsupported_capability"
	ErrorUnavailableCandidate  ErrorClass = "unavailable_candidate"
	ErrorResolutionFailure     ErrorClass = "resolution_failure"
	ErrorAuthRequirement       ErrorClass = "auth_requirement"
	ErrorHostIncompatibility   ErrorClass = "host_incompatibility"
)

// PlannerError preserves a machine-readable error class while wrapping the
// underlying cause for errors.Is/errors.As.
type PlannerError struct {
	Class ErrorClass
	Op    string
	Err   error
}

func (e *PlannerError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Op == "" {
		return fmt.Sprintf("%s: %v", e.Class, e.Err)
	}
	return fmt.Sprintf("%s (%s): %v", e.Class, e.Op, e.Err)
}

func (e *PlannerError) Unwrap() error { return e.Err }

// IsClass reports whether err contains a PlannerError of the requested class.
func IsClass(err error, class ErrorClass) bool {
	var pe *PlannerError
	return errors.As(err, &pe) && pe.Class == class
}

// ToolIdentity identifies the manifest tool being planned.
type ToolIdentity struct {
	Name string `json:"name"`
}

// CandidateIdentity records the selected method and whether it was explicitly
// declared rather than inferred by shorthand policy.
type CandidateIdentity struct {
	Method   string `json:"method"`
	Explicit bool   `json:"explicit"`
}

// ResolvedIdentity is the normalized desired-state identity shared across
// method implementations. Empty fields mean that dimension is not applicable.
type ResolvedIdentity struct {
	RequestedVersion *VersionIntent     `json:"requested_version,omitempty"`
	Version          string             `json:"version,omitempty"`
	Revision         string             `json:"revision,omitempty"`
	Digest           string             `json:"digest,omitempty"`
	Source           string             `json:"source,omitempty"`
	Registry         string             `json:"registry,omitempty"`
	Scope            string             `json:"scope,omitempty"`
	Environment      *EnvironmentTarget `json:"environment,omitempty"`
	Architecture     string             `json:"architecture,omitempty"`
	Platform         string             `json:"platform,omitempty"`
}

// Artifact describes a resolved install artifact without embedding credentials.
type Artifact struct {
	Kind         ArtifactKind `json:"kind,omitempty"`
	URL          string       `json:"url,omitempty"`
	LocalPath    string       `json:"local_path,omitempty"`
	Checksum     string       `json:"checksum,omitempty"`
	SignatureURL string       `json:"signature_url,omitempty"`
}

// Prerequisite describes a dependency required by the selected plan.
type Prerequisite struct {
	Name   string `json:"name"`
	Method string `json:"method,omitempty"`
}

// SecretReference identifies external secret material by reference only. Value
// material must never be copied into the plan.
type SecretReference struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

// Operation is one ordered action in resolution/execution/removal.
type Operation struct {
	Kind          string          `json:"kind"`
	Description   string          `json:"description,omitempty"`
	Effect        OperationEffect `json:"effect"`
	Command       []string        `json:"command,omitempty"`
	ArbitraryCode bool            `json:"arbitrary_code,omitempty"`
}

// RemovalMetadata records enough ownership identity for future removal logic.
type RemovalMetadata struct {
	Supported  bool     `json:"supported"`
	OwnedPaths []string `json:"owned_paths,omitempty"`
	Identity   string   `json:"identity,omitempty"`
}

// ResolvedInstallPlan is the canonical, adapter-neutral projection of one
// selected installation candidate.
type ResolvedInstallPlan struct {
	Version         int               `json:"plan_version"`
	Tool            ToolIdentity      `json:"tool"`
	Candidate       CandidateIdentity `json:"candidate"`
	Identity        ResolvedIdentity  `json:"identity,omitempty"`
	Artifacts       []Artifact        `json:"artifacts,omitempty"`
	Prerequisites   []Prerequisite    `json:"prerequisites,omitempty"`
	Sources         []SourceReference `json:"sources,omitempty"`
	Preparation     *PreparationPlan  `json:"preparation,omitempty"`
	Hooks           []LifecycleHook   `json:"hooks,omitempty"`
	Ensures         []EnsureAction    `json:"ensures,omitempty"`
	SourceMutations []Operation       `json:"source_mutations,omitempty"`
	OwnedPaths      []string          `json:"owned_paths,omitempty"`
	Entrypoints     map[string]string `json:"entrypoints,omitempty"`
	Operations      []Operation       `json:"operations,omitempty"`
	Removal         RemovalMetadata   `json:"removal"`
	Secrets         []SecretReference `json:"secret_refs,omitempty"`
}

// New creates a plan using the current internal version.
func New(tool, method string, explicit bool) ResolvedInstallPlan {
	return ResolvedInstallPlan{
		Version:   CurrentVersion,
		Tool:      ToolIdentity{Name: tool},
		Candidate: CandidateIdentity{Method: method, Explicit: explicit},
	}
}

// Validate checks invariants that are independent of any adapter.
func (p ResolvedInstallPlan) Validate() error {
	if p.Version != CurrentVersion {
		return fmt.Errorf("unsupported plan version %d (want %d)", p.Version, CurrentVersion)
	}
	if p.Tool.Name == "" {
		return errors.New("plan tool name is required")
	}
	if p.Candidate.Method == "" {
		return errors.New("plan candidate method is required")
	}
	if p.Identity.RequestedVersion != nil {
		if err := p.Identity.RequestedVersion.Validate(); err != nil {
			return fmt.Errorf("requested version: %w", err)
		}
	}
	if p.Identity.Scope != "" {
		if _, err := ParseScope(p.Identity.Scope); err != nil {
			return fmt.Errorf("identity scope: %w", err)
		}
	}
	if p.Identity.Environment != nil {
		if err := p.Identity.Environment.Validate(); err != nil {
			return fmt.Errorf("identity environment: %w", err)
		}
	}
	for i, artifact := range p.Artifacts {
		if err := artifact.Validate(); err != nil {
			return fmt.Errorf("artifact %d: %w", i, err)
		}
	}
	if _, err := CanonicalSources(p.Sources); err != nil {
		return err
	}
	if p.Preparation != nil {
		if err := p.Preparation.Validate(); err != nil {
			return fmt.Errorf("preparation: %w", err)
		}
	}
	if err := validateHooks(p.Hooks); err != nil {
		return err
	}
	if err := validateEnsures(p.Ensures); err != nil {
		return err
	}
	for i := range p.Secrets {
		if err := p.Secrets[i].Validate(); err != nil {
			return fmt.Errorf("secret reference %d: %w", i, err)
		}
	}
	for _, group := range [][]Operation{p.SourceMutations, p.Operations} {
		for _, op := range group {
			switch op.Effect {
			case EffectReadOnly, EffectMutation:
			default:
				return fmt.Errorf("operation %q has invalid effect %q", op.Kind, op.Effect)
			}
		}
	}
	for _, op := range p.SourceMutations {
		if op.Effect != EffectMutation {
			return fmt.Errorf("source mutation %q must be classified as mutation", op.Kind)
		}
	}
	return nil
}

// HasMutations reports whether execution of the plan can alter host state.
func (p ResolvedInstallPlan) HasMutations() bool {
	if p.Preparation != nil && (len(p.Preparation.Prepare) > 0 || len(p.Preparation.Commit) > 0) {
		return true
	}
	if len(p.Hooks) > 0 || len(p.Ensures) > 0 {
		return true
	}
	if len(p.SourceMutations) > 0 {
		return true
	}
	for _, op := range p.Operations {
		if op.Effect == EffectMutation {
			return true
		}
	}
	return false
}

// MarshalJSON makes diagnostic serialization credential-safe even if an
// adapter accidentally places a credential-bearing URL/argv fragment into a
// string field. Execution code must still never depend on this redaction.
func (p ResolvedInstallPlan) MarshalJSON() ([]byte, error) {
	type plain ResolvedInstallPlan
	safe := p.redacted()
	return json.Marshal(plain(safe))
}

func (p ResolvedInstallPlan) redacted() ResolvedInstallPlan {
	p.Identity.Source = run.RedactSensitiveText(p.Identity.Source)
	p.Identity.Registry = run.RedactSensitiveText(p.Identity.Registry)
	for i := range p.Artifacts {
		p.Artifacts[i].URL = run.RedactSensitiveText(p.Artifacts[i].URL)
		p.Artifacts[i].SignatureURL = run.RedactSensitiveText(p.Artifacts[i].SignatureURL)
	}
	for i := range p.Sources {
		p.Sources[i].URL = run.RedactSensitiveText(p.Sources[i].URL)
		if p.Sources[i].Trust != nil {
			trust := *p.Sources[i].Trust
			trust.KeyReference = run.RedactSensitiveText(trust.KeyReference)
			p.Sources[i].Trust = &trust
		}
	}
	if p.Preparation != nil {
		prep := *p.Preparation
		prep.Probe = redactOperations(prep.Probe)
		prep.Commit = redactOperations(prep.Commit)
		prep.Prepare = append([]PreparationMutation(nil), prep.Prepare...)
		for i := range prep.Prepare {
			prep.Prepare[i].Resource.Key = run.RedactSensitiveText(prep.Prepare[i].Resource.Key)
			prep.Prepare[i].Apply = redactOperations([]Operation{prep.Prepare[i].Apply})[0]
			if prep.Prepare[i].Rollback != nil {
				rollback := redactOperations([]Operation{*prep.Prepare[i].Rollback})[0]
				prep.Prepare[i].Rollback = &rollback
			}
		}
		p.Preparation = &prep
	}
	p.Hooks = append([]LifecycleHook(nil), p.Hooks...)
	for i := range p.Hooks {
		p.Hooks[i].Operation = redactOperations([]Operation{p.Hooks[i].Operation})[0]
	}
	p.Ensures = append([]EnsureAction(nil), p.Ensures...)
	for i := range p.Ensures {
		p.Ensures[i].Resource = run.RedactSensitiveText(p.Ensures[i].Resource)
		p.Ensures[i].Check = redactOperations([]Operation{p.Ensures[i].Check})[0]
		p.Ensures[i].Apply = redactOperations([]Operation{p.Ensures[i].Apply})[0]
	}
	p.SourceMutations = redactOperations(p.SourceMutations)
	p.Operations = redactOperations(p.Operations)
	return p
}

func redactOperations(in []Operation) []Operation {
	if len(in) == 0 {
		return in
	}
	out := make([]Operation, len(in))
	copy(out, in)
	for i := range out {
		out[i].Description = run.RedactSensitiveText(out[i].Description)
		if len(out[i].Command) > 0 {
			out[i].Command = redactCommand(out[i].Command)
		}
	}
	return out
}

func redactCommand(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		lower := strings.ToLower(out[i])
		switch lower {
		case "--token", "--password", "--passwd", "--secret", "--auth-token", "--access-token", "--api-key", "--apikey":
			if i+1 < len(out) {
				out[i+1] = "***"
			}
		}
		out[i] = run.RedactSensitiveText(out[i])
	}
	return out
}
