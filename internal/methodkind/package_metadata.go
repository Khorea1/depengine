package methodkind

const (
	// PackageComponentApplication is the default CycloneDX component type for
	// installed tools that are not language-ecosystem packages.
	PackageComponentApplication = "application"

	// PackageComponentLibrary is used for language-ecosystem packages.
	PackageComponentLibrary = "library"
)

// PackageMetadata describes standardized package identity emitted by
// cross-cutting consumers such as SBOM exporters. Empty fields deliberately
// mean "use the generic fallback" so aliases and not-yet-specialized methods
// retain their existing identity.
type PackageMetadata struct {
	ComponentType string
	PURLType       string
}

// PackageMetadataFor resolves package metadata for a method kind without
// requiring consumers to branch on method names. ComponentType defaults to
// "application"; PURLType defaults to the exact input kind so aliases preserve
// their historical package-url identity unless a contract explicitly overrides
// it.
func PackageMetadataFor(kind string) PackageMetadata {
	metadata := PackageMetadata{
		ComponentType: PackageComponentApplication,
		PURLType:       kind,
	}
	contract, ok := Lookup(kind)
	if !ok {
		return metadata
	}
	if contract.Package.ComponentType != "" {
		metadata.ComponentType = contract.Package.ComponentType
	}
	if contract.Package.PURLType != "" {
		metadata.PURLType = contract.Package.PURLType
	}
	return metadata
}
