package methodkind

// FieldSemantic classifies why an adapter-facing field exists. The category is
// deliberately independent from execution phases (Field.Effects): Effects say
// when a field matters, while Semantic says what part of the resolved contract
// it influences. New public fields must be classified here before they can be
// accepted by a method contract.
type FieldSemantic uint8

const (
	SemanticUnknown FieldSemantic = iota
	SemanticPackageIdentity
	SemanticVersionIdentity
	SemanticSourceIdentity
	SemanticArtifactIdentity
	SemanticIntegrity
	SemanticPlacement
	SemanticExecution
	SemanticVerificationIdentity
	SemanticPolicy
	SemanticAuthentication
)

var fieldSemantics = map[string]FieldSemantic{
	"architecture":         SemanticPolicy,
	"artifact":             SemanticArtifactIdentity,
	"asset":                SemanticArtifactIdentity,
	"binary":               SemanticPlacement,
	"bins":                 SemanticPlacement,
	"branch":               SemanticVersionIdentity,
	"bucket":               SemanticSourceIdentity,
	"build":                SemanticExecution,
	"channel":              SemanticVersionIdentity,
	"channels":             SemanticSourceIdentity,
	"checksum":             SemanticIntegrity,
	"checksum_file_format": SemanticIntegrity,
	"checksum_url":         SemanticIntegrity,
	"confinement":          SemanticPolicy,
	"depth":                SemanticPolicy,
	"desktop":              SemanticPlacement,
	"digest":               SemanticVersionIdentity,
	"entrypoints":          SemanticPlacement,
	"environment":          SemanticPlacement,
	"extract_to":           SemanticPlacement,
	"features":             SemanticExecution,
	"git":                  SemanticSourceIdentity,
	"index":                SemanticSourceIdentity,
	"index_url":            SemanticSourceIdentity,
	"install_dir":          SemanticPlacement,
	"installer_type":       SemanticPolicy,
	"link_dir":             SemanticPlacement,
	"local_path":           SemanticArtifactIdentity,
	"managed_paths":        SemanticPlacement,
	"manager":              SemanticPolicy,
	"no_default_features":  SemanticExecution,
	"pkg":                  SemanticPackageIdentity,
	"pkg_overrides":        SemanticPackageIdentity,
	"platform":             SemanticPolicy,
	"prefix":               SemanticPlacement,
	"prerelease":           SemanticVersionIdentity,
	"product_name":         SemanticVerificationIdentity,
	"publisher":            SemanticVerificationIdentity,
	"registry":             SemanticSourceIdentity,
	"release":              SemanticVersionIdentity,
	"remote":               SemanticSourceIdentity,
	"repo":                 SemanticSourceIdentity,
	"rev":                  SemanticVersionIdentity,
	"risk":                 SemanticPolicy,
	"root":                 SemanticPlacement,
	"scope":                SemanticPolicy,
	"secret_ref":           SemanticAuthentication,
	"checksum_secret_ref":  SemanticAuthentication,
	"signature_secret_ref": SemanticAuthentication,
	"signature_url":        SemanticIntegrity,
	"signing_key":          SemanticIntegrity,
	"source":               SemanticSourceIdentity,
	"strip_components":     SemanticPlacement,
	"submodules":           SemanticExecution,
	"sudo_required":        SemanticPolicy,
	"tag":                  SemanticVersionIdentity,
	"target":               SemanticPolicy,
	"track":                SemanticVersionIdentity,
	"url":                  SemanticSourceIdentity,
	"version":              SemanticVersionIdentity,
}

func semanticForField(name string) FieldSemantic {
	return fieldSemantics[name]
}
