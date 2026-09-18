// Package methodkind defines the schema contract for installation methods.
package methodkind

import (
	"sort"

	"github.com/Khorea1/depengine/pkg/artifact"
	"github.com/Khorea1/depengine/pkg/native"
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

// Field describes one adapter-facing config field. Effects must be non-zero
// for every field in Contracts; the conformance test enforces that invariant.
type Field struct {
	Type     FieldType
	Required bool
	NonEmpty bool
	Enum     []string
	Effects  FieldEffect
}

// Contract is the declarative schema contract for one adapter kind.
type Contract struct {
	Kind                 string
	Aliases              []string
	DefaultOrder         int // zero means the kind is not a blind fallback
	Fields               map[string]Field
	SourceAlternatives   [][]string // exactly one alternative; every field in it is required
	MutuallyExclusive    [][]string
	ImplicitDistroFamily []string
	AllowString          bool
	AllowTrue            bool
	CanRemove            bool
	Artifact             *artifact.Contract
}

var pkgField = map[string]Field{"pkg": {Type: String, Effects: EffectExecute | EffectVerify}}

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

var artifactSourceAlternatives = [][]string{{"url"}, {"repo", "asset"}}

var downloadArtifactContract = &artifact.Contract{
	URLFields:           []string{"url", "checksum_url", "signature_url"},
	AllowedSchemes:      []string{"http", "https"},
	ArtifactFields:      []string{"url", "asset"},
	ForbiddenExtensions: artifact.PlatformInstallerExtensions,
}

var githubArtifactContract = &artifact.Contract{
	URLFields:           []string{"checksum_url", "signature_url"},
	AllowedSchemes:      []string{"http", "https"},
	ArtifactFields:      []string{"asset"},
	ForbiddenExtensions: artifact.PlatformInstallerExtensions,
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
var Contracts = []Contract{
	{Kind: "native", DefaultOrder: 1, Fields: fields(pkgField, map[string]Field{"pkg_overrides": {Type: StringMap, Effects: EffectResolve | EffectExecute | EffectVerify}}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "scoop", DefaultOrder: 2, Fields: pkgField, ImplicitDistroFamily: []string{"windows"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "choco", DefaultOrder: 3, Fields: fields(pkgField, map[string]Field{"prerelease": {Type: Boolean, Effects: EffectExecute}}), ImplicitDistroFamily: []string{"windows"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "cargo", DefaultOrder: 4, Fields: fields(pkgField, map[string]Field{"git": {Type: String, Effects: EffectExecute}}), AllowString: true, AllowTrue: true, CanRemove: true},
	packageContract("go", 5, true),
	packageContract("pipx", 6, true),
	packageContract("uv", 7, true),
	packageContract("pip", 8, true),
	packageContract("npm", 9, true),
	packageContract("pnpm", 10, true),
	packageContract("bun", 11, true),
	packageContract("gem", 12, true),
	packageContract("yarn", 13, true),
	packageContract("yarn-berry", 14, false),
	packageContract("composer", 15, true),
	packageContract("apm", 16, false),
	packageContract("vscode", 17, false),
	packageContract("vscodium", 18, false),
	packageContract("flatpak", 19, true),
	{Kind: "snap", DefaultOrder: 20, Fields: fields(pkgField, map[string]Field{
		"confinement": {Type: String, Enum: []string{"strict", "classic", "devmode"}, Effects: EffectExecute},
		"channel":     {Type: String, Enum: []string{"stable", "candidate", "beta", "edge"}, Effects: EffectExecute},
	}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "cask", DefaultOrder: 21, Fields: pkgField, ImplicitDistroFamily: []string{"macos"}, AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "mas", DefaultOrder: 22, Fields: pkgField, ImplicitDistroFamily: []string{"macos"}, AllowString: true, AllowTrue: true},
	packageContract("appman", 23, true),
	{Kind: "sdkman", DefaultOrder: 24, Fields: fields(pkgField, map[string]Field{"version": {Type: String, Effects: EffectExecute | EffectVerify}}), AllowString: true, AllowTrue: true},
	packageContract("steamcmd", 25, false),
	packageContract("pacstall", 26, false),
	{Kind: "aur", Aliases: []string{"paru", "yay"}, DefaultOrder: 27, Fields: pkgField, ImplicitDistroFamily: []string{"arch"}, AllowString: true, AllowTrue: true, CanRemove: true},
	packageContract("conda", 28, true),
	{Kind: "asdf", DefaultOrder: 29, Fields: fields(pkgField, map[string]Field{"version": {Type: String, Effects: EffectExecute | EffectVerify}}), AllowString: true, AllowTrue: true, CanRemove: true},
	{Kind: "container", DefaultOrder: 30, Fields: map[string]Field{
		"manager": {Type: String, Required: true, NonEmpty: true, Enum: []string{"docker", "podman"}, Effects: EffectExecute | EffectVerify},
		"source":  {Type: String, Required: true, NonEmpty: true, Effects: EffectExecute | EffectVerify},
		"tag":     {Type: String, Effects: EffectExecute | EffectVerify},
	}, CanRemove: true},
	{Kind: "appimage", DefaultOrder: 31, Fields: fields(withoutField(downloadFields, "extract_to"), map[string]Field{
		"install_dir": {Type: String, Effects: EffectExecute | EffectVerify},
		"desktop":     {Type: Boolean, Effects: EffectExecute},
	}), SourceAlternatives: artifactSourceAlternatives, CanRemove: true, Artifact: downloadArtifactContract},
	{Kind: "android", DefaultOrder: 32, Fields: withoutFields(downloadFields, "extract_to", "binary"), SourceAlternatives: artifactSourceAlternatives, Artifact: downloadArtifactContract},
	{Kind: "git", DefaultOrder: 33, Fields: map[string]Field{
		"url":           {Type: String, Required: true, NonEmpty: true, Effects: EffectResolve | EffectExecute},
		"branch":        {Type: String, Effects: EffectResolve | EffectExecute},
		"depth":         {Type: IntegerOrString, Effects: EffectExecute},
		"build":         {Type: Command, Effects: EffectExecute},
		"extract_to":    {Type: String, Effects: EffectExecute | EffectVerify},
		"artifact":      {Type: String, Effects: EffectExecute},
		"binary":        {Type: String, Effects: EffectExecute | EffectVerify},
		"managed_paths": {Type: StringList, Effects: EffectValidate | EffectExecute | EffectVerify},
	}, CanRemove: true},
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
