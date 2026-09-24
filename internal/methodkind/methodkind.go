// Package methodkind defines the schema contract for installation methods.
package methodkind

import (
	"sort"

	"github.com/Khorea1/depengine/internal/artifact"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/plan"
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
	SecretRef       FieldType = "secret_ref"
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

// Field describes one adapter-facing config field. Effects must be non-zero
// for every field in Contracts; the conformance test enforces that invariant.
type Field struct {
	Type     FieldType
	Required bool
	NonEmpty bool
	Enum     []string
	Effects  FieldEffect
	Semantic FieldSemantic
}

// ScopeContract maps depengine's portable scope vocabulary to the concrete
// spelling expected by one adapter. Only semantically equivalent mappings
// belong here: manager-specific values such as gem's "default" remain
// outside the portable vocabulary rather than being guessed as user/system.
type ScopeContract struct {
	AdapterValues map[plan.Scope]string
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
	Checksum             *ChecksumContract
}

var pkgField = map[string]Field{"pkg": {Type: String, Effects: EffectExecute | EffectVerify}}

// artifactScopeContract declares that a download/artifact method resolves
// portable scope into platform-native placement defaults (see
// plan.ResolveScopePlacement and docs/design/adr-004-scope-placement.md).
// The adapter-native spelling of the portable vocabulary is the portable
// spelling itself: "user" and "system" are the placement defaults, not
// flags passed to a manager.
var artifactScopeContract = &ScopeContract{AdapterValues: map[plan.Scope]string{
	plan.ScopeUser:   "user",
	plan.ScopeSystem: "system",
}}

// artifactScopeField is the per-method scope declaration for artifact
// methods. It participates in resolution (identity + placement), execution
// (placement defaults), and verification (desired-state identity).
var artifactScopeField = map[string]Field{"scope": {Type: String, Enum: []string{"user", "system"}, Effects: EffectResolve | EffectExecute | EffectVerify}}

func packageContract(kind string, order int, canRemove bool) Contract {
	return Contract{Kind: kind, DefaultOrder: order, Fields: pkgField, AllowString: true, AllowTrue: true, CanRemove: canRemove}
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

var httpBearerSecretFields = map[string]Field{
	"secret_ref":           {Type: SecretRef, Effects: EffectResolve | EffectExecute},
	"checksum_secret_ref":  {Type: SecretRef, Effects: EffectResolve | EffectExecute},
	"signature_secret_ref": {Type: SecretRef, Effects: EffectResolve | EffectExecute},
}

var httpBearerSecretRequirements = map[string][]string{
	"checksum_secret_ref":  {"checksum_url"},
	"signature_secret_ref": {"signature_url"},
}

var artifactSourceAlternatives = [][]string{{"url"}, {"repo", "asset"}}

var remoteChecksumContract = &ChecksumContract{
	Algorithms: []string{"sha256", "sha512", "sha1", "md5"},
	AllowAuto:  true,
}

var localChecksumContract = &ChecksumContract{Algorithms: []string{"sha256"}}

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
	{Kind: "native", DefaultOrder: 1, Capabilities: CapabilitySourceSelection, Fields: fields(pkgField, map[string]Field{"pkg_overrides": {Type: StringMap, Effects: EffectResolve | EffectExecute | EffectVerify}}), AllowString: true, AllowTrue: true, CanRemove: true},
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
	{Kind: "cargo", DefaultOrder: 4, Capabilities: CapabilityExactVersion | CapabilitySourceSelection | CapabilityRevision | CapabilityArchitecture | CapabilityEnvironmentTarget | CapabilityAuth, Fields: fields(pkgField, map[string]Field{
		"git":                 {Type: String, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"secret_ref":          {Type: SecretRef, Effects: EffectResolve | EffectExecute},
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
		"branch":     {"git"},
		"tag":        {"git"},
		"rev":        {"git"},
		"secret_ref": {"git"},
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
	{Kind: "steamcmd", DefaultOrder: 25, Fields: map[string]Field{
		"pkg": {Type: String, Effects: EffectExecute},
	}, AllowString: true, AllowTrue: true},
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
	{Kind: "container", DefaultOrder: 30, Capabilities: CapabilityImmutableIdentity | CapabilityMutableTag | CapabilityArchitecture | CapabilitySourceSelection | CapabilityAuth, Fields: map[string]Field{
		"manager":       {Type: String, Required: true, NonEmpty: true, Enum: []string{"docker", "podman"}, Effects: EffectExecute | EffectVerify},
		"source":        {Type: String, Required: true, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"tag":           {Type: String, Effects: EffectExecute | EffectVerify},
		"digest":        {Type: String, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"platform":      {Type: String, NonEmpty: true, Effects: EffectValidate | EffectExecute | EffectVerify},
		"auth_username": {Type: String, NonEmpty: true, Effects: EffectExecute},
		"secret_ref":    {Type: SecretRef, Effects: EffectResolve | EffectExecute},
	}, Requires: map[string][]string{
		"auth_username": {"secret_ref"},
		"secret_ref":    {"auth_username"},
	}, MutuallyExclusive: [][]string{{"tag", "digest"}}, CanRemove: true},
	{Kind: "appimage", DefaultOrder: 31, Capabilities: CapabilityScope, Scopes: artifactScopeContract, Fields: fields(withoutField(downloadFields, "extract_to"), httpBearerSecretFields, map[string]Field{
		"install_dir": {Type: String, Effects: EffectExecute | EffectVerify},
		"desktop":     {Type: Boolean, Effects: EffectExecute},
		"scope":       artifactScopeField["scope"],
	}), Requires: httpBearerSecretRequirements, SourceAlternatives: artifactSourceAlternatives, CanRemove: true, Artifact: appImageArtifactContract, Checksum: remoteChecksumContract},
	{Kind: "android", DefaultOrder: 32, Fields: fields(withoutFields(downloadFields, "extract_to", "binary", "strip_components", "entrypoints", "link_dir"), httpBearerSecretFields), Requires: httpBearerSecretRequirements, SourceAlternatives: artifactSourceAlternatives, Artifact: androidArtifactContract, Checksum: remoteChecksumContract},
	{Kind: "git", DefaultOrder: 33, Capabilities: CapabilityArbitraryCode | CapabilityRevision | CapabilityAuth, Fields: map[string]Field{
		"url":           {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"secret_ref":    {Type: SecretRef, Effects: EffectResolve | EffectExecute},
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
	{Kind: "local", DefaultOrder: 34, Capabilities: CapabilityLocalArtifact, Fields: map[string]Field{
		"local_path":  {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"checksum":    {Type: String, Effects: EffectValidate | EffectResolve | EffectExecute | EffectVerify},
		"install_dir": {Type: String, Effects: EffectExecute | EffectVerify},
	}, CanRemove: true, Checksum: localChecksumContract},
	{Kind: "github", DefaultOrder: 35, Capabilities: CapabilityScope, Scopes: artifactScopeContract, Fields: fields(withoutField(downloadFields, "url"), map[string]Field{
		"repo":       {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"asset":      {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"release":    {Type: String, Effects: EffectResolve},
		"branch":     {Type: String, NonEmpty: true, Effects: EffectResolve},
		"scope":      artifactScopeField["scope"],
		"secret_ref": {Type: SecretRef, Effects: EffectResolve | EffectExecute},
	}), SourceAlternatives: [][]string{{"repo", "asset"}}, Artifact: githubArtifactContract, Checksum: remoteChecksumContract, MutuallyExclusive: [][]string{{"release", "branch"}}, CanRemove: true},
	{Kind: "http", DefaultOrder: 36, Capabilities: CapabilityScope, Scopes: artifactScopeContract, Fields: fields(downloadFields, artifactScopeField, httpBearerSecretFields), Requires: httpBearerSecretRequirements, SourceAlternatives: artifactSourceAlternatives, CanRemove: true, Artifact: downloadArtifactContract, Checksum: remoteChecksumContract},
	{Kind: "msi", DefaultOrder: 37, Fields: fields(artifactFields, httpBearerSecretFields, map[string]Field{
		"product_name": {Type: String, Required: true, NonEmpty: true, Effects: EffectVerify | EffectExecute},
		"publisher":    {Type: String, Effects: EffectVerify | EffectExecute},
	}), Requires: httpBearerSecretRequirements, SourceAlternatives: artifactSourceAlternatives, Artifact: msiArtifactContract, Checksum: remoteChecksumContract, MutuallyExclusive: [][]string{{"url", "repo"}, {"release", "branch"}}, ImplicitDistroFamily: []string{"windows"}, CanRemove: true},
})

// finalizeContracts adds capabilities implied by the adapter interface and by
// legacy contract facts in one place. This keeps lifecycle metadata complete
// while CanRemove remains temporarily available to older call sites. New
// cross-cutting behavior should consume capabilities rather than branching on
// method names or CanRemove.
func finalizeContracts(contracts []Contract) []Contract {
	for i := range contracts {
		for name, field := range contracts[i].Fields {
			semantic := semanticForField(name)
			if semantic == SemanticUnknown {
				panic("methodkind: field " + contracts[i].Kind + "." + name + " has no semantic classification")
			}
			field.Semantic = semantic
			contracts[i].Fields[name] = field
		}

		// Candidate-scoped host-source mutation is performed by the shared
		// executor source layer rather than by individual adapters. Any
		// method can therefore participate in a candidate that declares one of
		// the supported host source kinds without weakening intent.
		contracts[i].Capabilities |= CapabilitySourceMutation

		// Observe is mandatory on exec.AdapterV2, so every registered method can at
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

// IsNativeKind reports whether kind names the generic native method or a
// registered native package-manager alias.
func IsNativeKind(kind string) bool {
	return kind == "native" || native.IsNativeManagerName(kind)
}

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
