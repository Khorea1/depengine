// Package methodkind defines the schema contract for installation methods.
package methodkind

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/artifact"
	"github.com/Khorea1/depengine/pkg/native"
	"github.com/Khorea1/depengine/pkg/plan"
)

// FieldType is the runtime and JSON-Schema type of a method config field.
type FieldType string

const (
	String          FieldType = "string"
	Boolean         FieldType = "boolean"
	Integer         FieldType = "integer"
	IntegerOrString FieldType = "integer_or_string"
	StringMap       FieldType = "string_map"
	Command         FieldType = "command"
	StringList      FieldType = "string_list"
	StringStringMap FieldType = "string_string_map"
)

// FieldEffect identifies the runtime phase in which a declared field must
// have observable semantics. Effects are intentionally coarse: they are a
// conformance contract, not an execution plan.
type FieldEffect uint8

const (
	EffectResolve FieldEffect = 1 << iota
	EffectValidate
	EffectExecute
	EffectVerify
)

// Capability describes a semantic feature a method can honor. Capabilities
// are planner-facing metadata: they answer whether a candidate can represent
// requested intent without weakening it or branching on method names.
type Capability uint64

const (
	CapabilityExactVersion Capability = 1 << iota
	CapabilityChannel
	CapabilityImmutableIdentity
	CapabilityArbitraryCode
	CapabilitySourceSelection
	CapabilityArchitecture
	CapabilityScope
	CapabilityRevision
	CapabilityEnvironmentTarget
	CapabilityVersionConstraint
	CapabilityMutableTag
	CapabilitySourceMutation
	CapabilitySourceTrust
	CapabilityAuth
	CapabilityCheck
	CapabilityRemove
	CapabilityUpgrade
	CapabilityImmutableLock
	CapabilityLocalArtifact
)

// LifecycleCapabilities translates a requested lifecycle transition into the
// capability a method must advertise. Install is universal for a method
// contract; repair requires observation/check support, while remove and
// upgrade are explicit because not every adapter can safely reverse or replace
// an installation.
func LifecycleCapabilities(transition plan.TransitionKind) (Capability, error) {
	switch transition {
	case plan.TransitionInstall:
		return 0, nil
	case plan.TransitionRepair:
		return CapabilityCheck, nil
	case plan.TransitionRemove:
		return CapabilityRemove, nil
	case plan.TransitionUpgrade:
		return CapabilityUpgrade, nil
	default:
		return 0, fmt.Errorf("unsupported lifecycle transition %q", transition)
	}
}

// LockCapabilities returns the capability required when a caller needs the
// selected method to resolve mutable install intent into an immutable lock
// identity. This is distinct from CapabilityImmutableIdentity: the latter
// means the user supplied an immutable identity (for example a digest), while
// this capability means the adapter can produce/persist a reproducible pin.
func LockCapabilities(requireImmutable bool) Capability {
	if requireImmutable {
		return CapabilityImmutableLock
	}
	return 0
}

// VersionIntentCapabilities translates shared version intent into the semantic
// capabilities a candidate must declare. A zero capability means the intent
// is universal (currently only latest). Invalid intent is rejected before
// capability filtering so it cannot be silently weakened.
func VersionIntentCapabilities(intent plan.VersionIntent) (Capability, error) {
	if err := intent.Validate(); err != nil {
		return 0, err
	}
	switch intent.Mode {
	case plan.VersionLatest:
		return 0, nil
	case plan.VersionExact:
		return CapabilityExactVersion, nil
	case plan.VersionConstraint:
		return CapabilityVersionConstraint, nil
	case plan.VersionChannel:
		return CapabilityChannel, nil
	case plan.VersionGitTag, plan.VersionGitBranch, plan.VersionGitRevision:
		return CapabilityRevision, nil
	case plan.VersionContainerTag:
		return CapabilityMutableTag, nil
	case plan.VersionDigest:
		return CapabilityImmutableIdentity, nil
	default:
		// Validate currently makes this unreachable; retain fail-closed behavior
		// if VersionMode grows without updating this switch.
		return 0, fmt.Errorf("unsupported version mode %q", intent.Mode)
	}
}

// SourceCapabilities derives planner requirements from typed source identity.
// Authentication, trust, and host configuration mutation are deliberately
// independent so a method cannot claim generic source selection and thereby
// silently inherit stronger semantics.
func SourceCapabilities(sources []plan.SourceReference) (Capability, error) {
	var required Capability
	for i, source := range sources {
		if err := source.Validate(); err != nil {
			return 0, fmt.Errorf("source %d: %w", i, err)
		}
		required |= CapabilitySourceSelection
		if source.Role == plan.SourceHostConfiguration {
			required |= CapabilitySourceMutation
		}
		if source.Trust != nil {
			required |= CapabilitySourceTrust
		}
		if source.SecretRef != nil {
			required |= CapabilityAuth
		}
	}
	return required, nil
}

// PlanCapabilities derives cross-cutting semantic requirements from a
// ResolvedInstallPlan. This is the adapter-neutral counterpart to
// RequestedCapabilities, which exists for legacy raw method config.
func PlanCapabilities(p plan.ResolvedInstallPlan) (Capability, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	var required Capability
	if p.Identity.RequestedVersion != nil {
		versionCaps, err := VersionIntentCapabilities(*p.Identity.RequestedVersion)
		if err != nil {
			return 0, err
		}
		required |= versionCaps
	}
	if p.Identity.Scope != "" {
		required |= CapabilityScope
	}
	if p.Identity.Environment != nil {
		required |= CapabilityEnvironmentTarget
	}
	if p.Identity.Architecture != "" || p.Identity.Platform != "" {
		required |= CapabilityArchitecture
	}
	for _, artifact := range p.Artifacts {
		if artifact.LocalPath != "" {
			required |= CapabilityLocalArtifact
		}
	}
	sourceCaps, err := SourceCapabilities(p.Sources)
	if err != nil {
		return 0, err
	}
	required |= sourceCaps
	groups := [][]plan.Operation{p.SourceMutations, p.Operations}
	if p.Preparation != nil {
		groups = append(groups, p.Preparation.Probe, p.Preparation.Commit)
		for _, mutation := range p.Preparation.Prepare {
			groups = append(groups, []plan.Operation{mutation.Apply})
			if mutation.Rollback != nil {
				groups = append(groups, []plan.Operation{*mutation.Rollback})
			}
		}
	}
	for _, hook := range p.Hooks {
		groups = append(groups, []plan.Operation{hook.Operation})
	}
	for _, ensure := range p.Ensures {
		groups = append(groups, []plan.Operation{ensure.Check, ensure.Apply})
	}
	for _, group := range groups {
		for _, op := range group {
			if op.ArbitraryCode {
				required |= CapabilityArbitraryCode
			}
		}
	}
	return required, nil
}

// CandidateRequirements carries cross-cutting requirements that are not
// intrinsic fields of ResolvedInstallPlan. Keeping them typed prevents callers
// from growing method-name switches for lifecycle and reproducibility policy.
type CandidateRequirements struct {
	Transition           plan.TransitionKind
	RequireImmutableLock bool
}

// RequiredCapabilities combines semantic plan intent with operation-level
// requirements. An empty transition means the caller only wants intrinsic plan
// requirements (useful during parse/validation before a lifecycle command is
// selected).
func RequiredCapabilities(p plan.ResolvedInstallPlan, requirements CandidateRequirements) (Capability, error) {
	required, err := PlanCapabilities(p)
	if err != nil {
		return 0, err
	}
	if requirements.Transition != "" {
		lifecycle, err := LifecycleCapabilities(requirements.Transition)
		if err != nil {
			return 0, err
		}
		required |= lifecycle
	}
	required |= LockCapabilities(requirements.RequireImmutableLock)
	return required, nil
}

// MissingRequirements is the single adapter-neutral capability boundary for a
// resolved candidate plus the operation the caller intends to perform.
func (c Contract) MissingRequirements(p plan.ResolvedInstallPlan, requirements CandidateRequirements) (Capability, error) {
	required, err := RequiredCapabilities(p, requirements)
	if err != nil {
		return 0, err
	}
	return required &^ c.Capabilities, nil
}

// MissingPlanCapabilities reports capabilities required by an already-resolved
// semantic plan that the method contract does not declare.
func (c Contract) MissingPlanCapabilities(p plan.ResolvedInstallPlan) (Capability, error) {
	required, err := PlanCapabilities(p)
	if err != nil {
		return 0, err
	}
	return required &^ c.Capabilities, nil
}

// MissingVersionCapabilities reports why this contract cannot represent the
// requested version intent without weakening it.
func (c Contract) MissingVersionCapabilities(intent plan.VersionIntent) (Capability, error) {
	required, err := VersionIntentCapabilities(intent)
	if err != nil {
		return 0, err
	}
	return required &^ c.Capabilities, nil
}

// Supports reports whether every requested capability is declared by the
// contract. A zero request is always satisfied.
func (c Contract) Supports(requested Capability) bool {
	return c.Capabilities&requested == requested
}

// SupportsScope reports whether the method can represent the requested portable
// scope without weakening it to a manager default.
func (c Contract) SupportsScope(scope plan.Scope) bool {
	if !c.Supports(CapabilityScope) || c.Scopes == nil {
		return false
	}
	_, ok := c.Scopes.AdapterValues[scope]
	return ok
}

// AdapterScope resolves canonical planner scope into the value expected by the
// adapter. This is the only place portable scope aliases should be translated.
func (c Contract) AdapterScope(scope plan.Scope) (string, error) {
	if !c.Supports(CapabilityScope) {
		return "", fmt.Errorf("method %q does not support installation scope", c.Kind)
	}
	value, err := c.Scopes.AdapterValue(scope)
	if err != nil {
		return "", fmt.Errorf("method %q: %w", c.Kind, err)
	}
	return value, nil
}

// NormalizeScope maps an existing adapter-native spelling to canonical planner
// identity. It intentionally rejects manager-specific values with no portable
// equivalent.
func (c Contract) NormalizeScope(raw string) (plan.Scope, error) {
	if !c.Supports(CapabilityScope) {
		return "", fmt.Errorf("method %q does not support installation scope", c.Kind)
	}
	scope, err := c.Scopes.Normalize(raw)
	if err != nil {
		return "", fmt.Errorf("method %q: %w", c.Kind, err)
	}
	return scope, nil
}

// RequestedCapabilities derives the semantic capabilities exercised by one
// concrete method configuration. This deliberately keys off contract fields,
// not method names, so candidate filtering can stay generic. Validation owns
// value-shape checks; this function only records configured intent and fails
// closed for command-bearing fields.
func (c Contract) RequestedCapabilities(config map[string]any) Capability {
	var requested Capability
	if configuredNonEmpty(config, "version") {
		requested |= CapabilityExactVersion
	}
	branchConfigured := configuredNonEmpty(config, "branch")
	if configuredNonEmpty(config, "channel") || (c.Supports(CapabilityChannel) && (configuredNonEmpty(config, "track") || configuredNonEmpty(config, "risk") || branchConfigured)) {
		requested |= CapabilityChannel
	}
	if configuredNonEmpty(config, "digest") {
		requested |= CapabilityImmutableIdentity
	}
	if configuredNonEmpty(config, "source") || configuredNonEmpty(config, "registry") || configuredNonEmpty(config, "remote") || configuredNonEmpty(config, "channels") || configuredNonEmpty(config, "bucket") || configuredNonEmpty(config, "index") || configuredNonEmpty(config, "index_url") {
		requested |= CapabilitySourceSelection
	}
	if configuredNonEmpty(config, "architecture") || configuredNonEmpty(config, "target") || configuredNonEmpty(config, "platform") {
		requested |= CapabilityArchitecture
	}
	if configuredNonEmpty(config, "scope") {
		requested |= CapabilityScope
	}
	// branch is a channel selector for channel-oriented contracts (currently
	// snap), otherwise it is revision intent. tag/rev are always revisions.
	if configuredNonEmpty(config, "tag") || configuredNonEmpty(config, "rev") || (branchConfigured && !c.Supports(CapabilityChannel)) {
		requested |= CapabilityRevision
	}
	if configuredNonEmpty(config, "environment") || configuredNonEmpty(config, "prefix") || configuredNonEmpty(config, "root") {
		requested |= CapabilityEnvironmentTarget
	}
	for name, field := range c.Fields {
		if field.Type != Command {
			continue
		}
		if _, configured := config[name]; configured {
			requested |= CapabilityArbitraryCode
		}
	}
	return requested
}

// MissingCapabilities reports semantic intent present in config that the
// method contract cannot honor. It is primarily a planner safety net: normal
// schema validation should reject unsupported fields earlier, but execution
// must not silently weaken intent when handed a programmatically constructed
// candidate.
func (c Contract) MissingCapabilities(config map[string]any) Capability {
	return c.RequestedCapabilities(config) &^ c.Capabilities
}

// CapabilityNames returns stable human-readable names for a capability mask.
func CapabilityNames(capabilities Capability) []string {
	known := []struct {
		cap  Capability
		name string
	}{
		{CapabilityExactVersion, "exact-version"},
		{CapabilityChannel, "channel"},
		{CapabilityImmutableIdentity, "immutable-identity"},
		{CapabilityArbitraryCode, "arbitrary-code"},
		{CapabilitySourceSelection, "source-selection"},
		{CapabilityArchitecture, "architecture"},
		{CapabilityScope, "scope"},
		{CapabilityRevision, "revision"},
		{CapabilityEnvironmentTarget, "environment-profile"},
		{CapabilityVersionConstraint, "version-constraint"},
		{CapabilityMutableTag, "mutable-tag"},
		{CapabilitySourceMutation, "source-mutation"},
		{CapabilitySourceTrust, "source-trust"},
		{CapabilityAuth, "auth"},
		{CapabilityCheck, "check"},
		{CapabilityRemove, "remove"},
		{CapabilityUpgrade, "upgrade"},
		{CapabilityImmutableLock, "immutable-lock"},
		{CapabilityLocalArtifact, "local-artifact"},
	}
	out := make([]string, 0, len(known))
	for _, item := range known {
		if capabilities&item.cap != 0 {
			out = append(out, item.name)
		}
	}
	return out
}

func configuredNonEmpty(config map[string]any, key string) bool {
	value, ok := config[key]
	if !ok {
		return false
	}
	if text, ok := value.(string); ok {
		return text != ""
	}
	return value != nil
}

// Field describes one adapter-facing config field. Effects must be non-zero
// for every field in Contracts; the conformance test enforces that invariant.
type Field struct {
	Type     FieldType
	Required bool
	NonEmpty bool
	Enum     []string
	Effects  FieldEffect
}

// ScopeContract maps depengine's portable scope vocabulary to the concrete
// spelling expected by one adapter. Only semantically equivalent mappings
// belong here: manager-specific values such as gem's "default" remain
// outside the portable vocabulary rather than being guessed as user/system.
type ScopeContract struct {
	AdapterValues map[plan.Scope]string
}

// PortableScopes returns supported canonical scopes in stable order.
func (s *ScopeContract) PortableScopes() []plan.Scope {
	if s == nil {
		return nil
	}
	out := make([]plan.Scope, 0, len(s.AdapterValues))
	for scope := range s.AdapterValues {
		out = append(out, scope)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AdapterValue resolves a portable scope to the adapter-native spelling.
func (s *ScopeContract) AdapterValue(scope plan.Scope) (string, error) {
	if s == nil {
		return "", fmt.Errorf("portable scope is unsupported")
	}
	if err := scope.Validate(); err != nil {
		return "", err
	}
	value, ok := s.AdapterValues[scope]
	if !ok {
		return "", fmt.Errorf("portable scope %q is unsupported", scope)
	}
	return value, nil
}

// Normalize maps an adapter-native scope spelling back to portable identity.
// Canonical portable spellings are also accepted when that scope is supported,
// allowing the planner/schema to migrate independently from adapter flags.
func (s *ScopeContract) Normalize(raw string) (plan.Scope, error) {
	if s == nil {
		return "", fmt.Errorf("portable scope is unsupported")
	}
	if strings.TrimSpace(raw) != raw || raw == "" {
		return "", fmt.Errorf("invalid scope %q", raw)
	}
	for portable, native := range s.AdapterValues {
		if raw == string(portable) || raw == native {
			return portable, nil
		}
	}
	return "", fmt.Errorf("scope %q has no portable mapping", raw)
}

// Contract is the declarative schema contract for one adapter kind.
type Contract struct {
	Kind                 string
	Aliases              []string
	DefaultOrder         int // zero means the kind is not a blind fallback
	Fields               map[string]Field
	SourceAlternatives   [][]string // exactly one alternative; every field in it is required
	MutuallyExclusive    [][]string
	Requires             map[string][]string // when key is configured, all listed fields must also be configured
	ImplicitDistroFamily []string
	AllowString          bool
	AllowTrue            bool
	CanRemove            bool
	Capabilities         Capability
	Scopes               *ScopeContract
	Artifact             *artifact.Contract
}

var pkgField = map[string]Field{"pkg": {Type: String, Effects: EffectExecute | EffectVerify}}

func packageContract(kind string, order int, canRemove bool) Contract {
	return Contract{Kind: kind, DefaultOrder: order, Fields: pkgField, AllowString: true, AllowTrue: true, CanRemove: canRemove}
}

func exactVersionPackageContract(kind string, order int, canRemove bool) Contract {
	return Contract{
		Kind:         kind,
		DefaultOrder: order,
		Capabilities: CapabilityExactVersion,
		Fields: fields(pkgField, map[string]Field{
			"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		}),
		AllowString: true,
		AllowTrue:   true,
		CanRemove:   canRemove,
	}
}

func fields(entries ...map[string]Field) map[string]Field {
	out := make(map[string]Field)
	for _, entry := range entries {
		for key, value := range entry {
			out[key] = value
		}
	}
	return out
}

func withoutField(entry map[string]Field, excluded string) map[string]Field {
	return withoutFields(entry, excluded)
}

func withoutFields(entry map[string]Field, excluded ...string) map[string]Field {
	skip := make(map[string]bool, len(excluded))
	for _, key := range excluded {
		skip[key] = true
	}
	out := make(map[string]Field, len(entry))
	for key, value := range entry {
		if !skip[key] {
			out[key] = value
		}
	}
	return out
}

var artifactFields = map[string]Field{
	"url":                  {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
	"repo":                 {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
	"asset":                {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
	"release":              {Type: String, Effects: EffectResolve},
	"branch":               {Type: String, NonEmpty: true, Effects: EffectResolve},
	"checksum":             {Type: String, Effects: EffectValidate | EffectExecute},
	"checksum_url":         {Type: String, Effects: EffectValidate | EffectExecute},
	"checksum_file_format": {Type: String, Enum: []string{"sha256sum", "bsd", "raw"}, Effects: EffectValidate | EffectExecute},
	"signature_url":        {Type: String, Effects: EffectValidate | EffectExecute},
	"signing_key":          {Type: String, Effects: EffectValidate | EffectExecute},
}

var downloadFields = fields(artifactFields, map[string]Field{
	"extract_to":       {Type: String, Effects: EffectExecute | EffectVerify},
	"binary":           {Type: String, Effects: EffectExecute | EffectVerify},
	"sudo_required":    {Type: Boolean, Effects: EffectExecute},
	"strip_components": {Type: Integer, Effects: EffectValidate | EffectExecute},
	"entrypoints":      {Type: StringStringMap, Effects: EffectExecute | EffectVerify},
	"link_dir":         {Type: String, Effects: EffectExecute | EffectVerify},
})

var artifactSourceAlternatives = [][]string{{"url"}, {"repo", "asset"}}

var downloadArtifactContract = &artifact.Contract{
	URLFields:                    []string{"url", "checksum_url", "signature_url"},
	AllowedSchemes:               []string{"http", "https"},
	ArtifactFields:               []string{"url", "asset"},
	ForbiddenExtensions:          artifact.PlatformInstallerExtensions,
	UnsupportedArchiveExtensions: artifact.UnsupportedArchiveExtensions,
}

var githubArtifactContract = &artifact.Contract{
	URLFields:                    []string{"checksum_url", "signature_url"},
	AllowedSchemes:               []string{"http", "https"},
	ArtifactFields:               []string{"asset"},
	ForbiddenExtensions:          artifact.PlatformInstallerExtensions,
	UnsupportedArchiveExtensions: artifact.UnsupportedArchiveExtensions,
}

var appImageArtifactContract = &artifact.Contract{
	URLFields:          []string{"url", "checksum_url", "signature_url"},
	AllowedSchemes:     []string{"http", "https"},
	ArtifactFields:     []string{"url", "asset"},
	RequiredExtensions: []string{".AppImage"},
}

var androidArtifactContract = &artifact.Contract{
	URLFields:          []string{"url", "checksum_url", "signature_url"},
	AllowedSchemes:     []string{"http", "https"},
	ArtifactFields:     []string{"url", "asset"},
	RequiredExtensions: []string{".apk"},
}

var msiArtifactContract = &artifact.Contract{
	URLFields:          []string{"url", "checksum_url", "signature_url"},
	AllowedSchemes:     []string{"http", "https"},
	ArtifactFields:     []string{"url", "asset"},
	RequiredExtensions: []string{".msi"},
}

// Contracts is the single source of truth for method kinds, ordering and
// adapter-facing schema fields. Keep entries in default preference order;
// kinds with DefaultOrder zero are valid but never injected as blind fallbacks.
var Contracts = finalizeContracts([]Contract{
	{Kind: "native", DefaultOrder: 1, Fields: fields(pkgField, map[string]Field{"pkg_overrides": {Type: StringMap, Effects: EffectResolve | EffectExecute | EffectVerify}}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "winget", Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityScope | CapabilityArchitecture, Scopes: &ScopeContract{AdapterValues: map[plan.Scope]string{plan.ScopeUser: "user", plan.ScopeSystem: "machine"}}, Fields: fields(pkgField, map[string]Field{
		"version":        {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"source":         {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute | EffectVerify},
		"scope":          {Type: String, Enum: []string{"user", "machine"}, Effects: EffectExecute},
		"architecture":   {Type: String, Enum: []string{"x86", "x64", "arm", "arm64"}, Effects: EffectExecute},
		"installer_type": {Type: String, Enum: []string{"appx", "burn", "exe", "font", "inno", "msi", "msix", "msstore", "nullsoft", "portable", "wix", "zip"}, Effects: EffectExecute},
	}), ImplicitDistroFamily: []string{"windows"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "scoop", DefaultOrder: 2, Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityScope | CapabilityArchitecture, Scopes: &ScopeContract{AdapterValues: map[plan.Scope]string{plan.ScopeUser: "user", plan.ScopeSystem: "global"}}, Fields: fields(pkgField, map[string]Field{
		"version":      {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"bucket":       {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"scope":        {Type: String, Enum: []string{"user", "global"}, Effects: EffectExecute | EffectVerify},
		"architecture": {Type: String, Enum: []string{"32bit", "64bit", "arm64"}, Effects: EffectExecute},
	}), ImplicitDistroFamily: []string{"windows"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "choco", DefaultOrder: 3, Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityArchitecture, Fields: fields(pkgField, map[string]Field{
		"version":      {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"prerelease":   {Type: Boolean, Effects: EffectExecute},
		"source":       {Type: String, NonEmpty: true, Effects: EffectExecute},
		"architecture": {Type: String, Enum: []string{"x86", "x64"}, Effects: EffectExecute},
	}), ImplicitDistroFamily: []string{"windows"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "cargo", DefaultOrder: 4, Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityRevision | CapabilityArchitecture | CapabilityEnvironmentTarget, Fields: fields(pkgField, map[string]Field{
		"git":                 {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"version":             {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"registry":            {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"branch":              {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"tag":                 {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"rev":                 {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"features":            {Type: StringList, Effects: EffectExecute},
		"no_default_features": {Type: Boolean, Effects: EffectExecute},
		"bins":                {Type: StringList, Effects: EffectExecute | EffectVerify},
		"target":              {Type: String, NonEmpty: true, Effects: EffectExecute},
		"root":                {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
	}), MutuallyExclusive: [][]string{{"git", "version"}, {"git", "registry"}, {"branch", "tag", "rev"}}, Requires: map[string][]string{
		"branch": {"git"},
		"tag":    {"git"},
		"rev":    {"git"},
	}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "go", DefaultOrder: 5, Capabilities: CapabilityExactVersion, Fields: fields(pkgField, map[string]Field{
		"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "pipx", DefaultOrder: 6, Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityScope, Scopes: &ScopeContract{AdapterValues: map[plan.Scope]string{plan.ScopeUser: "user", plan.ScopeSystem: "global"}}, Fields: fields(pkgField, map[string]Field{
		"version":   {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"index_url": {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"scope":     {Type: String, Enum: []string{"user", "global"}, Effects: EffectExecute | EffectVerify},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "uv", DefaultOrder: 7, Capabilities: CapabilityExactVersion | CapabilitySourceSelection, Fields: fields(pkgField, map[string]Field{
		"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"index":   {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "pip", DefaultOrder: 8, Capabilities: CapabilityExactVersion | CapabilitySourceSelection, Fields: fields(pkgField, map[string]Field{
		"version":   {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"index_url": {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "npm", DefaultOrder: 9, Capabilities: CapabilityExactVersion | CapabilitySourceSelection, Fields: fields(pkgField, map[string]Field{
		"version":  {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"registry": {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "pnpm", DefaultOrder: 10, Capabilities: CapabilityExactVersion, Fields: fields(pkgField, map[string]Field{
		"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "bun", DefaultOrder: 11, Capabilities: CapabilityExactVersion | CapabilitySourceSelection, Fields: fields(pkgField, map[string]Field{
		"version":  {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"registry": {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "gem", DefaultOrder: 12, Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityScope, Scopes: &ScopeContract{AdapterValues: map[plan.Scope]string{plan.ScopeUser: "user"}}, Fields: fields(pkgField, map[string]Field{
		"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"source":  {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"scope":   {Type: String, Enum: []string{"default", "user"}, Effects: EffectExecute},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "yarn", DefaultOrder: 13, Capabilities: CapabilityExactVersion, Fields: fields(pkgField, map[string]Field{
		"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	packageContract("yarn-berry", 14, false),
	{Kind: "composer", DefaultOrder: 15, Capabilities: CapabilityExactVersion, Fields: fields(pkgField, map[string]Field{
		"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	packageContract("apm", 16, false),
	packageContract("vscode", 17, false),
	packageContract("vscodium", 18, false),
	{Kind: "flatpak", DefaultOrder: 19, Capabilities: CapabilitySourceSelection | CapabilityScope | CapabilityRevision, Scopes: &ScopeContract{AdapterValues: map[plan.Scope]string{plan.ScopeUser: "user", plan.ScopeSystem: "system"}}, Fields: fields(pkgField, map[string]Field{
		"remote": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"branch": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"scope":  {Type: String, Enum: []string{"user", "system"}, Effects: EffectExecute | EffectVerify},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "snap", DefaultOrder: 20, Capabilities: CapabilityChannel, Fields: fields(pkgField, map[string]Field{
		"confinement": {Type: String, Enum: []string{"strict", "classic", "devmode"}, Effects: EffectExecute},
		"channel":     {Type: String, Enum: []string{"stable", "candidate", "beta", "edge"}, Effects: EffectExecute | EffectVerify},
		"track":       {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"risk":        {Type: String, Enum: []string{"stable", "candidate", "beta", "edge"}, Effects: EffectExecute | EffectVerify},
		"branch":      {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
	}), MutuallyExclusive: [][]string{{"channel", "track"}, {"channel", "risk"}, {"channel", "branch"}}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "cask", DefaultOrder: 21, Fields: pkgField, ImplicitDistroFamily: []string{"macos"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "mas", DefaultOrder: 22, Fields: pkgField, ImplicitDistroFamily: []string{"macos"}, AllowString: true, AllowTrue: true},
	packageContract("appman", 23, true),
	{Kind: "sdkman", DefaultOrder: 24, Capabilities: CapabilityExactVersion, Fields: fields(pkgField, map[string]Field{"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify}}), AllowString: true, AllowTrue: true},
	packageContract("steamcmd", 25, false),
	packageContract("pacstall", 26, false),
	{Kind: "aur", Aliases: []string{"paru", "yay"}, DefaultOrder: 27, Fields: pkgField, ImplicitDistroFamily: []string{"arch"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "conda", DefaultOrder: 28, Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityEnvironmentTarget, Fields: fields(pkgField, map[string]Field{
		"version":     {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"build":       {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"channels":    {Type: StringList, Effects: EffectResolve | EffectExecute},
		"environment": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"prefix":      {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
	}), MutuallyExclusive: [][]string{{"environment", "prefix"}}, Requires: map[string][]string{"build": {"version"}}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "asdf", DefaultOrder: 29, Capabilities: CapabilityExactVersion, Fields: fields(pkgField, map[string]Field{"version": {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify}}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "container", DefaultOrder: 30, Capabilities: CapabilityImmutableIdentity | CapabilityMutableTag | CapabilityArchitecture | CapabilitySourceSelection, Fields: map[string]Field{
		"manager":  {Type: String, Required: true, NonEmpty: true, Enum: []string{"docker", "podman"}, Effects: EffectExecute | EffectVerify},
		"source":   {Type: String, Required: true, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"tag":      {Type: String, Effects: EffectExecute | EffectVerify},
		"digest":   {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"platform": {Type: String, NonEmpty: true, Effects: EffectValidate | EffectExecute | EffectVerify},
	}, MutuallyExclusive: [][]string{{"tag", "digest"}}, CanRemove: true},
	{Kind: "appimage", DefaultOrder: 31, Fields: fields(withoutField(downloadFields, "extract_to"), map[string]Field{
		"install_dir": {Type: String, Effects: EffectExecute | EffectVerify},
		"desktop":     {Type: Boolean, Effects: EffectExecute},
	}), SourceAlternatives: artifactSourceAlternatives, CanRemove: true, Artifact: appImageArtifactContract},
	{Kind: "android", DefaultOrder: 32, Fields: withoutFields(downloadFields, "extract_to", "binary"), SourceAlternatives: artifactSourceAlternatives, Artifact: androidArtifactContract},
	{Kind: "git", DefaultOrder: 33, Capabilities: CapabilityArbitraryCode | CapabilityRevision, Fields: map[string]Field{
		"url":           {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"branch":        {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"tag":           {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"rev":           {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"depth":         {Type: IntegerOrString, Effects: EffectValidate | EffectExecute},
		"submodules":    {Type: Boolean, Effects: EffectExecute},
		"build":         {Type: Command, Effects: EffectExecute},
		"extract_to":    {Type: String, Effects: EffectExecute | EffectVerify},
		"artifact":      {Type: String, Effects: EffectExecute},
		"binary":        {Type: String, Effects: EffectExecute | EffectVerify},
		"managed_paths": {Type: StringList, Effects: EffectValidate | EffectExecute | EffectVerify},
	}, MutuallyExclusive: [][]string{{"branch", "tag", "rev"}}, CanRemove: true},
	{Kind: "github", DefaultOrder: 34, Fields: fields(withoutField(downloadFields, "url"), map[string]Field{
		"repo":    {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"asset":   {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"release": {Type: String, Effects: EffectResolve},
		"branch":  {Type: String, NonEmpty: true, Effects: EffectResolve},
	}), SourceAlternatives: [][]string{{"repo", "asset"}}, Artifact: githubArtifactContract, MutuallyExclusive: [][]string{{"release", "branch"}}, CanRemove: true},
	{Kind: "http", DefaultOrder: 35, Fields: downloadFields, SourceAlternatives: artifactSourceAlternatives, CanRemove: true, Artifact: downloadArtifactContract},
	{Kind: "msi", DefaultOrder: 36, Fields: fields(artifactFields, map[string]Field{
		"product_name": {Type: String, Required: true, NonEmpty: true, Effects: EffectVerify | EffectExecute},
		"publisher":    {Type: String, Effects: EffectVerify | EffectExecute},
	}), SourceAlternatives: artifactSourceAlternatives, Artifact: msiArtifactContract, MutuallyExclusive: [][]string{{"url", "repo"}, {"release", "branch"}}, ImplicitDistroFamily: []string{"windows"}, CanRemove: true},
})

// finalizeContracts adds capabilities implied by the adapter interface and by
// legacy contract facts in one place. This keeps lifecycle metadata complete
// while CanRemove remains temporarily available to older call sites. New
// cross-cutting behavior should consume capabilities rather than branching on
// method names or CanRemove.
func finalizeContracts(contracts []Contract) []Contract {
	for i := range contracts {
		// Check is mandatory on exec.Adapter, so every registered method can at
		// least observe whether its target is installed.
		contracts[i].Capabilities |= CapabilityCheck
		if contracts[i].CanRemove {
			contracts[i].Capabilities |= CapabilityRemove | CapabilityUpgrade
		}

		// Immutable locking is advertised only when the contract exposes a
		// concrete identity mechanism that the shared lock projection can pin:
		// exact versions, revisions/digests, or checksummed artifacts. Runtime
		// resolution may still return LockUnavailable for a specific candidate.
		if contracts[i].Capabilities&(CapabilityExactVersion|CapabilityRevision|CapabilityImmutableIdentity) != 0 {
			contracts[i].Capabilities |= CapabilityImmutableLock
		}
		if _, ok := contracts[i].Fields["checksum"]; ok {
			contracts[i].Capabilities |= CapabilityImmutableLock
		}
	}
	return contracts
}

var (
	contractByKind     = buildContractIndex()
	contractByAlias    = buildAliasIndex()
	DefaultMethodOrder = buildDefaultOrder()
	knownKinds         = buildKnownKinds()
	knownKindSet       = buildKnownKindSet()
)

func buildContractIndex() map[string]*Contract {
	out := make(map[string]*Contract, len(Contracts))
	for i := range Contracts {
		out[Contracts[i].Kind] = &Contracts[i]
	}
	return out
}

func buildAliasIndex() map[string]*Contract {
	out := make(map[string]*Contract)
	for i := range Contracts {
		for _, alias := range Contracts[i].Aliases {
			out[alias] = &Contracts[i]
		}
	}
	return out
}

func buildDefaultOrder() []string {
	ordered := make([]Contract, 0, len(Contracts))
	for _, contract := range Contracts {
		if contract.DefaultOrder > 0 {
			ordered = append(ordered, contract)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].DefaultOrder < ordered[j].DefaultOrder })
	out := make([]string, len(ordered))
	for i, contract := range ordered {
		out[i] = contract.Kind
	}
	return out
}

func buildKnownKinds() []string {
	set := make(map[string]bool, len(Contracts)+32)
	for _, contract := range Contracts {
		set[contract.Kind] = true
		for _, alias := range contract.Aliases {
			set[alias] = true
		}
	}
	for _, name := range native.ManagerNames() {
		set[name] = true
	}
	for _, name := range native.ManagerBinaryNames() {
		set[name] = true
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func buildKnownKindSet() map[string]bool {
	out := make(map[string]bool, len(knownKinds))
	for _, kind := range knownKinds {
		out[kind] = true
	}
	return out
}

// DefaultBuckets maps ecosystem shorthands to concrete method kinds.
var DefaultBuckets = map[string][]string{
	"python": {"pip", "pipx", "uv"},
	"node":   {"npm", "pnpm", "bun"},
}

// Lookup returns the contract for kind. Native package-manager aliases share
// the native contract.
func Lookup(kind string) (*Contract, bool) {
	if contract, ok := contractByKind[kind]; ok {
		return contract, true
	}
	if contract, ok := contractByAlias[kind]; ok {
		return contract, true
	}
	if native.IsNativeManagerName(kind) {
		return contractByKind["native"], true
	}
	return nil, false
}

// KnownKinds returns all adapter kinds and native manager aliases.
func KnownKinds() []string { return append([]string(nil), knownKinds...) }

// IsKnownKind reports whether k is a method kind or native manager alias.
func IsKnownKind(k string) bool { return knownKindSet[k] }

// ExpandBuckets replaces bucket names with their concrete method kinds.
func ExpandBuckets(order []string) []string {
	var expanded []string
	for _, kind := range order {
		methods, ok := DefaultBuckets[kind]
		if !ok {
			expanded = append(expanded, kind)
			continue
		}
		for _, method := range methods {
			if !contains(expanded, method) {
				expanded = append(expanded, method)
			}
		}
	}
	return expanded
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
