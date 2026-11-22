package exec

import (
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// methodForResolvedTarget projects the selected plan target into the legacy
// candidate consumed by Observe, Check, InstalledVersion, and Remove. Keep the
// original candidate untouched: InstallResolved already reads the plan itself.
func methodForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) *config.MethodCandidate {
	if method == nil || resolved == nil {
		return method
	}

	contract, ok := methodkind.Lookup(method.Kind)
	// Download installation uses the resolved artifact as the authority.
	// Observation and persisted metadata must see the very same URL and
	// checksum as InstallResolved, especially for v2 frozen lockfiles.
	transportArtifact := (method.Kind == "http" || method.Kind == "github") && len(resolved.Artifacts) > 0 && resolved.Artifacts[0].URL != ""
	if !ok || (contract.Environment == nil && !transportArtifact && (method.Kind != "git" || resolved.Identity.Revision == "") && ((method.Kind != "npm" && method.Kind != "pnpm" && method.Kind != "yarn") || resolved.Identity.Version == "")) {
		return method
	}

	methodCopy := *method
	methodCopy.Config = make(map[string]any, len(method.Config))
	for key, value := range method.Config {
		methodCopy.Config[key] = value
	}

	if transportArtifact {
		artifact := resolved.Artifacts[0]
		methodCopy.Config["url"] = artifact.URL
		for key, value := range map[string]string{
			"checksum":             artifact.Checksum,
			"checksum_url":         artifact.ChecksumURL,
			"checksum_file_format": artifact.ChecksumFileFormat,
			"signature_url":        artifact.SignatureURL,
			"signing_key":          artifact.SigningKey,
		} {
			if value == "" {
				delete(methodCopy.Config, key)
			} else {
				methodCopy.Config[key] = value
			}
		}
	}
	if contract.Environment != nil {
		for _, field := range contract.Environment.Fields() {
			delete(methodCopy.Config, field)
		}
		if target := resolved.Identity.Environment; target != nil {
			if field, ok := contract.Environment.FieldFor(target.Kind); ok {
				methodCopy.Config[field] = target.Value
			}
		}
	}
	if method.Kind == "git" && resolved.Identity.Revision != "" {
		delete(methodCopy.Config, "branch")
		delete(methodCopy.Config, "tag")
		methodCopy.Config["rev"] = resolved.Identity.Revision
	}
	if (method.Kind == "npm" || method.Kind == "pnpm" || method.Kind == "yarn") && resolved.Identity.Version != "" {
		methodCopy.Config["version"] = resolved.Identity.Version
	}

	return &methodCopy
}

func configForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) map[string]any {
	return methodForResolvedTarget(method, resolved).Config
}
