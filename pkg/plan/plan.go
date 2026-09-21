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
	"path"
	"reflect"
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
	detail := "<nil>"
	if e.Err != nil {
		detail = run.RedactSensitiveText(e.Err.Error())
	}
	op := run.RedactSensitiveText(e.Op)
	if op == "" {
		return fmt.Sprintf("%s: %s", e.Class, detail)
	}
	return fmt.Sprintf("%s (%s): %s", e.Class, op, detail)
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
	Package          string             `json:"package,omitempty"`
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

// Validate checks adapter-neutral desired identity invariants. Keeping this on
// ResolvedIdentity itself lets planning, locking, and direct reconciliation use
// exactly the same structural contract instead of trusting callers to have run
// ResolvedInstallPlan.Validate first.
func (i ResolvedIdentity) Validate() error {
	if strings.TrimSpace(i.Package) != i.Package {
		return errors.New("package must not contain surrounding whitespace")
	}
	if strings.ContainsRune(i.Package, '\x00') {
		return errors.New("package contains NUL")
	}
	for label, value := range map[string]string{
		"version": i.Version, "revision": i.Revision,
		"architecture": i.Architecture, "platform": i.Platform,
	} {
		if strings.TrimSpace(value) != value {
			return fmt.Errorf("%s must not contain surrounding whitespace", label)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%s contains NUL", label)
		}
	}
	if i.Digest != "" {
		if err := validateConcreteDigestSyntax(i.Digest); err != nil {
			return fmt.Errorf("digest: %w", err)
		}
	}
	if i.Source != "" {
		if err := validateIdentityReference(i.Source); err != nil {
			return fmt.Errorf("source: %w", err)
		}
	}
	if i.Registry != "" {
		if err := validateIdentityReference(i.Registry); err != nil {
			return fmt.Errorf("registry: %w", err)
		}
	}
	if i.RequestedVersion != nil {
		if err := i.RequestedVersion.Validate(); err != nil {
			return fmt.Errorf("requested version: %w", err)
		}
		switch i.RequestedVersion.Mode {
		case VersionExact:
			if i.Version != "" && i.Version != i.RequestedVersion.Value {
				return errors.New("requested exact version does not match resolved identity version")
			}
		case VersionDigest:
			if i.Digest != "" && canonicalDigest(i.RequestedVersion.Value) != canonicalDigest(i.Digest) {
				return errors.New("requested digest does not match resolved identity digest")
			}
		}
	}
	if i.Scope != "" {
		if _, err := ParseScope(i.Scope); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
	}
	if i.Environment != nil {
		if err := i.Environment.Validate(); err != nil {
			return fmt.Errorf("environment: %w", err)
		}
	}
	return nil
}

// Artifact describes a resolved install artifact without embedding credentials.
type Artifact struct {
	Kind               ArtifactKind `json:"kind,omitempty"`
	URL                string       `json:"url,omitempty"`
	LocalPath          string       `json:"local_path,omitempty"`
	Checksum           string       `json:"checksum,omitempty"`
	ChecksumURL        string       `json:"checksum_url,omitempty"`
	ChecksumFileFormat string       `json:"checksum_file_format,omitempty"`
	SignatureURL       string       `json:"signature_url,omitempty"`
	SigningKey         string       `json:"signing_key,omitempty"`
}

// Prerequisite describes a dependency required by the selected plan.
type Prerequisite struct {
	Name   string `json:"name"`
	Method string `json:"method,omitempty"`
}

// Validate checks the stable prerequisite identity carried by a resolved
// plan. Method may be empty when candidate selection for the prerequisite is
// intentionally deferred, but an explicitly supplied method must itself be a
// canonical identifier.
func (p Prerequisite) Validate() error {
	if strings.TrimSpace(p.Name) != p.Name || p.Name == "" {
		return errors.New("prerequisite name is required and must not contain surrounding whitespace")
	}
	if strings.ContainsRune(p.Name, '\x00') {
		return errors.New("prerequisite name contains NUL")
	}
	if p.Method != "" {
		if strings.TrimSpace(p.Method) != p.Method {
			return errors.New("prerequisite method must not contain surrounding whitespace")
		}
		if strings.ContainsRune(p.Method, '\x00') {
			return errors.New("prerequisite method contains NUL")
		}
	}
	return nil
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

// Validate enforces operation-level safety invariants independently of the
// adapter that produced the plan. Any explicit argv is executable user intent
// and therefore must be classified as arbitrary code so capability gating
// cannot be bypassed by a malformed or future planner path.
func (o Operation) Validate() error {
	if strings.TrimSpace(o.Kind) != o.Kind || o.Kind == "" {
		return errors.New("operation kind is required and must not contain surrounding whitespace")
	}
	switch o.Effect {
	case EffectReadOnly, EffectMutation:
	default:
		return fmt.Errorf("operation %q has invalid effect %q", o.Kind, o.Effect)
	}
	if len(o.Command) > 0 && !o.ArbitraryCode {
		return fmt.Errorf("operation %q has a command but is not marked arbitrary code", o.Kind)
	}
	if len(o.Command) > 0 {
		if strings.TrimSpace(o.Command[0]) == "" {
			return fmt.Errorf("operation %q command executable is empty", o.Kind)
		}
		for i, arg := range o.Command {
			if strings.ContainsRune(arg, '\x00') {
				return fmt.Errorf("operation %q command argument %d contains NUL", o.Kind, i)
			}
		}
	}
	return nil
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

// Clone returns a deep copy safe for adapter-specific resolution.
func (p ResolvedInstallPlan) Clone() ResolvedInstallPlan {
	return cloneResolvedInstallPlan(p)
}

// Validate checks invariants that are independent of any adapter.
func (p ResolvedInstallPlan) Validate() error {
	if p.Version != CurrentVersion {
		return fmt.Errorf("unsupported plan version %d (want %d)", p.Version, CurrentVersion)
	}
	if strings.TrimSpace(p.Tool.Name) != p.Tool.Name || p.Tool.Name == "" {
		return errors.New("plan tool name is required and must not contain surrounding whitespace")
	}
	if strings.ContainsRune(p.Tool.Name, '\x00') {
		return errors.New("plan tool name contains NUL")
	}
	if strings.TrimSpace(p.Candidate.Method) != p.Candidate.Method || p.Candidate.Method == "" {
		return errors.New("plan candidate method is required and must not contain surrounding whitespace")
	}
	if strings.ContainsRune(p.Candidate.Method, '\x00') {
		return errors.New("plan candidate method contains NUL")
	}
	if err := p.Identity.Validate(); err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	seenArtifacts := make(map[string]struct{}, len(p.Artifacts))
	for i, artifact := range p.Artifacts {
		if err := artifact.Validate(); err != nil {
			return fmt.Errorf("artifact %d: %w", i, err)
		}
		key := artifactIdentityKey(artifact.Kind, artifact.URL, artifact.LocalPath)
		if _, duplicate := seenArtifacts[key]; duplicate {
			return fmt.Errorf("duplicate artifact location %q", key)
		}
		seenArtifacts[key] = struct{}{}
	}
	seenPrerequisites := make(map[Prerequisite]struct{}, len(p.Prerequisites))
	for i, prerequisite := range p.Prerequisites {
		if err := prerequisite.Validate(); err != nil {
			return fmt.Errorf("prerequisite %d: %w", i, err)
		}
		if _, duplicate := seenPrerequisites[prerequisite]; duplicate {
			return fmt.Errorf("duplicate prerequisite %q via method %q", prerequisite.Name, prerequisite.Method)
		}
		seenPrerequisites[prerequisite] = struct{}{}
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
	for name, target := range p.Entrypoints {
		if strings.TrimSpace(name) != name || name == "" {
			return errors.New("entrypoint name is required and must not contain surrounding whitespace")
		}
		if strings.ContainsRune(name, '\x00') {
			return fmt.Errorf("entrypoint %q name contains NUL", name)
		}
		if target == "" {
			return fmt.Errorf("entrypoint %q target is required", name)
		}
		if strings.ContainsRune(target, '\x00') {
			return fmt.Errorf("entrypoint %q target contains NUL", name)
		}
	}
	seenSecrets := make(map[SecretReference]struct{}, len(p.Secrets))
	for i := range p.Secrets {
		if err := p.Secrets[i].Validate(); err != nil {
			return fmt.Errorf("secret reference %d: %w", i, err)
		}
		if _, duplicate := seenSecrets[p.Secrets[i]]; duplicate {
			return fmt.Errorf("duplicate secret reference %q/%q", p.Secrets[i].Provider, p.Secrets[i].Name)
		}
		seenSecrets[p.Secrets[i]] = struct{}{}
	}
	for _, group := range [][]Operation{p.SourceMutations, p.Operations} {
		for _, op := range group {
			if err := op.Validate(); err != nil {
				return err
			}
		}
	}
	for _, op := range p.SourceMutations {
		if op.Effect != EffectMutation {
			return fmt.Errorf("source mutation %q must be classified as mutation", op.Kind)
		}
	}
	if err := validateRemovalOwnership(p.OwnedPaths, p.Removal); err != nil {
		return err
	}
	return nil
}

func artifactIdentityKey(kind ArtifactKind, rawURL, localPath string) string {
	location := rawURL
	if localPath != "" {
		location = localPath
	} else if rawURL != "" {
		// Artifact URL identity is transport-semantic, not spelling-semantic.
		// Use the same credential-free canonical form persisted by locks so the
		// plan rejects aliases before execution rather than discovering the
		// duplicate only during lock projection.
		location = sanitizeLockReference(rawURL)
	}
	return string(kind) + "\x00" + location
}

func validateRemovalOwnership(ownedPaths []string, removal RemovalMetadata) error {
	if !removal.Supported && (removal.Identity != "" || len(removal.OwnedPaths) != 0) {
		return errors.New("unsupported removal must not declare removal identity or owned paths")
	}
	if strings.TrimSpace(removal.Identity) != removal.Identity {
		return errors.New("removal identity must not contain surrounding whitespace")
	}
	if strings.ContainsRune(removal.Identity, '\x00') {
		return errors.New("removal identity contains NUL")
	}
	owned := make(map[string]struct{}, len(ownedPaths))
	for i, ownedPath := range ownedPaths {
		key, err := ownershipPathKey(ownedPath)
		if err != nil {
			return fmt.Errorf("owned path %d: %w", i, err)
		}
		if _, duplicate := owned[key]; duplicate {
			return fmt.Errorf("duplicate owned path %q", ownedPath)
		}
		owned[key] = struct{}{}
	}
	seenRemoval := make(map[string]struct{}, len(removal.OwnedPaths))
	for i, removalPath := range removal.OwnedPaths {
		key, err := ownershipPathKey(removalPath)
		if err != nil {
			return fmt.Errorf("removal owned path %d: %w", i, err)
		}
		if _, duplicate := seenRemoval[key]; duplicate {
			return fmt.Errorf("duplicate removal owned path %q", removalPath)
		}
		seenRemoval[key] = struct{}{}
		if _, ok := owned[key]; !ok {
			return fmt.Errorf("removal path %q is not declared as owned by the plan", removalPath)
		}
	}
	return nil
}

func ownershipPathKey(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
		return "", errors.New("path must be non-empty and must not contain surrounding whitespace or NUL")
	}
	if validWindowsAbsolutePath(value) {
		// Windows path spelling has multiple aliases for the same destination.
		// Normalize separators and case so ownership/removal cannot bypass the
		// declared set by switching drive/path spelling.
		return "windows\x00" + strings.ToLower(strings.ReplaceAll(value, "/", `\`)), nil
	}
	if validUnixAbsolutePath(value) {
		clean := path.Clean(value)
		if clean != value {
			return "", fmt.Errorf("Unix path is not canonical; use %q", clean)
		}
		return "unix\x00" + value, nil
	}
	return "", errors.New("path must be an absolute canonical Unix or Windows path")
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
	// Final defensive boundary: redact every string in a deep typed copy so a
	// future plan field cannot bypass credential filtering. Keeping the concrete
	// struct type preserves the stable JSON field order used by golden tests.
	return json.Marshal(plain(redactAllStrings(safe)))
}

func redactAllStrings[T any](value T) T {
	return redactStrings(reflect.ValueOf(value)).Interface().(T)
}

func redactStrings(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.String:
		out := reflect.New(value.Type()).Elem()
		out.SetString(run.RedactSensitiveText(value.String()))
		return out
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(redactStrings(value.Elem()))
		return out
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		item := redactStrings(value.Elem())
		out := reflect.New(value.Type()).Elem()
		out.Set(item)
		return out
	case reflect.Struct:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.NumField(); i++ {
			out.Field(i).Set(redactStrings(value.Field(i)))
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(redactStrings(value.Index(i)))
		}
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			out.SetMapIndex(redactStrings(iter.Key()), redactStrings(iter.Value()))
		}
		return out
	default:
		return value
	}
}

func (p ResolvedInstallPlan) redacted() ResolvedInstallPlan {
	p.Artifacts = append([]Artifact(nil), p.Artifacts...)
	p.Sources = append([]SourceReference(nil), p.Sources...)
	p.Identity.Source = run.RedactSensitiveText(p.Identity.Source)
	p.Identity.Registry = run.RedactSensitiveText(p.Identity.Registry)
	for i := range p.Artifacts {
		p.Artifacts[i].URL = run.RedactSensitiveText(p.Artifacts[i].URL)
		p.Artifacts[i].ChecksumURL = run.RedactSensitiveText(p.Artifacts[i].ChecksumURL)
		p.Artifacts[i].SignatureURL = run.RedactSensitiveText(p.Artifacts[i].SignatureURL)
		p.Artifacts[i].SigningKey = run.RedactSensitiveText(p.Artifacts[i].SigningKey)
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

func cloneOperation(op Operation) Operation {
	op.Command = append([]string(nil), op.Command...)
	return op
}

// cloneResolvedInstallPlan returns a deep copy of the mutable composite state
// carried by a resolved plan. Plans cross lifecycle/lock boundaries and are
// routinely handed to code that may adapt them for execution; sharing slices,
// maps, or pointer-backed identity with the caller would let those adaptations
// silently rewrite the original intent.
func cloneResolvedInstallPlan(in ResolvedInstallPlan) ResolvedInstallPlan {
	out := in
	if in.Identity.RequestedVersion != nil {
		requested := *in.Identity.RequestedVersion
		if in.Identity.RequestedVersion.Channel != nil {
			channel := *in.Identity.RequestedVersion.Channel
			requested.Channel = &channel
		}
		out.Identity.RequestedVersion = &requested
	}
	if in.Identity.Environment != nil {
		environment := *in.Identity.Environment
		out.Identity.Environment = &environment
	}

	out.Artifacts = append([]Artifact(nil), in.Artifacts...)
	out.Prerequisites = append([]Prerequisite(nil), in.Prerequisites...)
	out.Sources = append([]SourceReference(nil), in.Sources...)
	for i := range out.Sources {
		if in.Sources[i].Trust != nil {
			trust := *in.Sources[i].Trust
			out.Sources[i].Trust = &trust
		}
		if in.Sources[i].SecretRef != nil {
			secret := *in.Sources[i].SecretRef
			out.Sources[i].SecretRef = &secret
		}
	}

	if in.Preparation != nil {
		preparation := *in.Preparation
		preparation.Probe = cloneOperations(in.Preparation.Probe)
		preparation.Commit = cloneOperations(in.Preparation.Commit)
		preparation.Prepare = append([]PreparationMutation(nil), in.Preparation.Prepare...)
		for i := range preparation.Prepare {
			preparation.Prepare[i].Apply = cloneOperation(in.Preparation.Prepare[i].Apply)
			if in.Preparation.Prepare[i].Rollback != nil {
				rollback := cloneOperation(*in.Preparation.Prepare[i].Rollback)
				preparation.Prepare[i].Rollback = &rollback
			}
		}
		out.Preparation = &preparation
	}

	out.Hooks = append([]LifecycleHook(nil), in.Hooks...)
	for i := range out.Hooks {
		out.Hooks[i].Operation = cloneOperation(in.Hooks[i].Operation)
	}
	out.Ensures = append([]EnsureAction(nil), in.Ensures...)
	for i := range out.Ensures {
		out.Ensures[i].Check = cloneOperation(in.Ensures[i].Check)
		out.Ensures[i].Apply = cloneOperation(in.Ensures[i].Apply)
	}
	out.SourceMutations = cloneOperations(in.SourceMutations)
	out.OwnedPaths = append([]string(nil), in.OwnedPaths...)
	if in.Entrypoints != nil {
		out.Entrypoints = make(map[string]string, len(in.Entrypoints))
		for name, target := range in.Entrypoints {
			out.Entrypoints[name] = target
		}
	}
	out.Operations = cloneOperations(in.Operations)
	out.Removal.OwnedPaths = append([]string(nil), in.Removal.OwnedPaths...)
	out.Secrets = append([]SecretReference(nil), in.Secrets...)
	return out
}

func cloneOperations(in []Operation) []Operation {
	out := append([]Operation(nil), in...)
	for i := range out {
		out[i] = cloneOperation(out[i])
	}
	return out
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
		if run.IsSensitiveFlag(out[i]) {
			if i+1 < len(out) {
				out[i+1] = "***"
			}
		}
		out[i] = run.RedactSensitiveText(out[i])
	}
	return out
}
