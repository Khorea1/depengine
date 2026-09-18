// Package artifact defines shared semantic rules for downloadable artifacts.
package artifact

import (
	"fmt"
	"net/url"
	"strings"
)

// Contract describes URL and file-format invariants for a download-backed
// installation method. Method contracts embed this so validation and runtime
// adapters can share the same artifact semantics.
type Contract struct {
	URLFields           []string
	AllowedSchemes      []string
	ArtifactFields      []string
	ForbiddenExtensions []string
	RequiredExtensions  []string
}

// PlatformInstallerExtensions are installer formats that must never be treated
// as ordinary HTTP payloads. Only formats with a dedicated adapter may execute.
var PlatformInstallerExtensions = []string{".msi", ".exe", ".pkg", ".dmg", ".msix", ".appx"}

// ValidateURL verifies that raw is an absolute URL using one of the allowed
// schemes. Placeholder expansion belongs to the caller because placeholder
// syntax is part of the schema layer, not the artifact contract.
func ValidateURL(raw string, allowedSchemes []string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("malformed URL %q", raw)
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == "" {
		return fmt.Errorf("malformed URL %q", raw)
	}
	for _, scheme := range allowedSchemes {
		if strings.EqualFold(parsed.Scheme, scheme) {
			return nil
		}
	}
	return fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
}

// Extension returns a recognized extension from a URL or asset name while
// ignoring query strings and fragments.
func Extension(raw string, extensions []string) string {
	path := strings.ToLower(strings.Split(strings.Split(raw, "?")[0], "#")[0])
	for _, ext := range extensions {
		if strings.HasSuffix(path, strings.ToLower(ext)) {
			return strings.ToLower(ext)
		}
	}
	return ""
}

// InstallerExtension reports a platform installer extension, if any.
func InstallerExtension(raw string) string {
	return Extension(raw, PlatformInstallerExtensions)
}
