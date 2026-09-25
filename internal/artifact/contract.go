// Package artifact defines shared semantic rules for downloadable artifacts.
package artifact

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

// Contract describes URL and file-format invariants for a download-backed
// installation method. Method contracts embed this so validation and runtime
// adapters can share the same artifact semantics.
type Contract struct {
	URLFields                    []string
	AllowedSchemes               []string
	ArtifactFields               []string
	ForbiddenExtensions          []string
	UnsupportedArchiveExtensions []string
	RequiredExtensions           []string
}

// PlatformInstallerExtensions are installer formats that must never be treated
// as ordinary HTTP payloads. Only formats with a dedicated adapter may execute.
var PlatformInstallerExtensions = []string{".msi", ".exe", ".pkg", ".dmg", ".msix", ".appx"}

// UnsupportedArchiveExtensions are common archive/compression formats that the
// HTTP installer cannot extract. Rejecting them is safer than the generic
// unknown-extension fallback, which intentionally treats unknown payloads as
// plain executables.
// SupportedArchiveExtensions are archive formats the HTTP installer extracts.
// This list also prevents a supported compound suffix such as .tar.xz from
// being mistaken for its unsupported standalone compression suffix .xz.
var SupportedArchiveExtensions = []string{
	".tar.gz", ".tgz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tar", ".zip", ".bz2",
}

var UnsupportedArchiveExtensions = []string{
	".tar.lz", ".tar.lzma", ".tar.lz4", ".tar.lzo",
	".7z", ".rar", ".gz", ".xz", ".zst",
}

// ValidateURL verifies that raw is an absolute URL using one of the allowed
// schemes. Placeholder expansion belongs to the caller because placeholder
// syntax is part of the schema layer, not the artifact contract.
func ValidateURL(raw string, allowedSchemes []string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("malformed URL %q", run.RedactSensitiveText(raw))
	}
	if requiresNetworkHost(parsed.Scheme) && parsed.Host == "" {
		return fmt.Errorf("malformed URL %q", run.RedactSensitiveText(raw))
	}
	if strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https") {
		// Credentials embedded in a URL are unsafe for a declarative artifact
		// contract: URLs are routinely surfaced in diagnostics, lock metadata,
		// and downloader command lines. Authentication must be supplied through
		// an out-of-band credential mechanism instead. Do not echo raw here,
		// because it may itself contain the secret we are rejecting.
		if parsed.User != nil {
			return fmt.Errorf("embedded URL credentials are not allowed")
		}
	}
	for _, scheme := range allowedSchemes {
		if strings.EqualFold(parsed.Scheme, scheme) {
			return nil
		}
	}
	return fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
}

// ValidateAuthenticatedURL verifies that an HTTP credential will only be sent
// over TLS. Plain HTTP is permitted only for the local loopback interface,
// which keeps local test/development registries usable without exposing a
// credential on the network.
func ValidateAuthenticatedURL(raw string) error {
	if err := ValidateURL(raw, []string{"http", "https"}); err != nil {
		return err
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed authenticated URL")
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("authenticated URL must use HTTPS; plain HTTP is allowed only for loopback")
}

func requiresNetworkHost(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "ssh", "git":
		return true
	default:
		return false
	}
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

// ForbiddenExtensionError reports an artifact format explicitly forbidden by
// a contract. Extension is normalized to lower-case.
type ForbiddenExtensionError struct {
	Extension string
}

func (e *ForbiddenExtensionError) Error() string {
	return fmt.Sprintf("artifact extension %s is not supported", e.Extension)
}

// RequiredExtensionError reports that an artifact does not match any format
// accepted by a method-specific contract.
// UnsupportedArchiveError reports an archive/compression format that is
// recognizable as such but not extractable by the current artifact backend.
type UnsupportedArchiveError struct {
	Extension string
}

func (e *UnsupportedArchiveError) Error() string {
	return fmt.Sprintf("archive extension %s is not supported", e.Extension)
}

type RequiredExtensionError struct {
	Required []string
}

func (e *RequiredExtensionError) Error() string {
	return fmt.Sprintf("artifact must end in %s", strings.Join(e.Required, " or "))
}

// ValidateArtifact enforces this contract's artifact-format restrictions.
// It is representation-agnostic: callers may pass a URL or an asset filename;
// query strings and fragments are ignored by Extension.
func (c *Contract) ValidateArtifact(raw string) error {
	if c == nil || raw == "" {
		return nil
	}
	if ext := Extension(raw, c.ForbiddenExtensions); ext != "" {
		return &ForbiddenExtensionError{Extension: ext}
	}
	if Extension(raw, SupportedArchiveExtensions) == "" {
		if ext := Extension(raw, c.UnsupportedArchiveExtensions); ext != "" {
			return &UnsupportedArchiveError{Extension: ext}
		}
	}
	if len(c.RequiredExtensions) > 0 && Extension(raw, c.RequiredExtensions) == "" {
		required := append([]string(nil), c.RequiredExtensions...)
		return &RequiredExtensionError{Required: required}
	}
	return nil
}
