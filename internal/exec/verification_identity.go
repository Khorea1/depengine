package exec

import (
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// projectVerificationIdentity narrows a resolved identity to dimensions the
// selected method contract promises to verify. Resolvers are allowed to carry
// execution identity that an adapter cannot recover from host state (for
// example cargo.git or npm.registry); treating those dimensions as mandatory
// verification evidence turns a successful install into StateUnknown on the
// next run.
func projectVerificationIdentity(kind string, identity plan.ResolvedIdentity) plan.ResolvedIdentity {
	contract, ok := methodkind.Lookup(kind)
	if !ok {
		return identity
	}

	var out plan.ResolvedIdentity
	if contractVerifiesSemantic(contract, methodkind.SemanticPackageIdentity) {
		out.Package = identity.Package
	}
	if contractVerifiesField(contract, "version") {
		out.Version = identity.Version
		if identity.RequestedVersion != nil && identity.RequestedVersion.Mode == plan.VersionExact {
			requested := *identity.RequestedVersion
			out.RequestedVersion = &requested
		}
	}
	if contractVerifiesAnyField(contract, "rev", "branch", "build") {
		out.Revision = identity.Revision
	}
	if contractVerifiesField(contract, "digest") {
		out.Digest = identity.Digest
		if identity.RequestedVersion != nil && identity.RequestedVersion.Mode == plan.VersionDigest {
			requested := *identity.RequestedVersion
			out.RequestedVersion = &requested
		}
	}
	if contractVerifiesSource(contract) {
		out.Source = identity.Source
	}
	if contractVerifiesField(contract, "registry") {
		out.Registry = identity.Registry
	}
	if contractVerifiesField(contract, "scope") {
		out.Scope = identity.Scope
	}
	if contractVerifiesEnvironment(contract) && identity.Environment != nil {
		environment := *identity.Environment
		out.Environment = &environment
	}
	if contractVerifiesAnyField(contract, "architecture", "target") {
		out.Architecture = identity.Architecture
	}
	if contractVerifiesField(contract, "platform") {
		out.Platform = identity.Platform
	}
	return out
}

func contractVerifiesField(contract *methodkind.Contract, name string) bool {
	if contract == nil {
		return false
	}
	field, ok := contract.Fields[name]
	return ok && field.Effects&methodkind.EffectVerify != 0
}

func contractVerifiesAnyField(contract *methodkind.Contract, names ...string) bool {
	for _, name := range names {
		if contractVerifiesField(contract, name) {
			return true
		}
	}
	return false
}

func contractVerifiesSemantic(contract *methodkind.Contract, semantic methodkind.FieldSemantic) bool {
	if contract == nil {
		return false
	}
	for _, field := range contract.Fields {
		if field.Semantic == semantic && field.Effects&methodkind.EffectVerify != 0 {
			return true
		}
	}
	return false
}

func contractVerifiesSource(contract *methodkind.Contract) bool {
	if contract == nil {
		return false
	}
	for name, field := range contract.Fields {
		if name == "registry" {
			continue
		}
		if field.Semantic == methodkind.SemanticSourceIdentity && field.Effects&methodkind.EffectVerify != 0 {
			return true
		}
	}
	return false
}

func contractVerifiesEnvironment(contract *methodkind.Contract) bool {
	if contract == nil || contract.Environment == nil {
		return false
	}
	for _, name := range contract.Environment.Fields() {
		if contractVerifiesField(contract, name) {
			return true
		}
	}
	return false
}
