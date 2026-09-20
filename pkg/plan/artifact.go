package plan

import (
	"fmt"
	"path"
	"strings"
)

// ArtifactKind is the resolved payload shape. It is intentionally independent
// of transport: the same raw/archive semantics apply to HTTP and local files.
type ArtifactKind string

const (
	ArtifactRaw     ArtifactKind = "raw"
	ArtifactArchive ArtifactKind = "archive"
)

func (k ArtifactKind) Validate() error {
	switch k {
	case "", ArtifactRaw, ArtifactArchive:
		return nil
	default:
		return fmt.Errorf("unsupported artifact kind %q", k)
	}
}

// NormalizeProjectPath validates and canonicalizes a portable path relative to
// the manifest/project root. Canonical plan/lock identity never uses an
// absolute machine path for vendored artifacts.
func NormalizeProjectPath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("local artifact path is required")
	}
	if strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("local artifact path has surrounding whitespace")
	}
	if strings.ContainsRune(raw, '\x00') {
		return "", fmt.Errorf("local artifact path contains NUL")
	}
	if strings.Contains(raw, `\`) {
		return "", fmt.Errorf("local artifact path must use portable '/' separators")
	}
	if path.IsAbs(raw) || hasWindowsVolumePrefix(raw) {
		return "", fmt.Errorf("local artifact path must be relative to the project root")
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("local artifact path escapes the project root")
	}
	return clean, nil
}

// hasWindowsVolumePrefix rejects drive-qualified paths independently of the
// host running the planner. path.IsAbs intentionally follows slash semantics,
// so without this guard a Windows path such as C:/vendor/tool would be treated
// as portable when planning on Unix and could later resolve differently on a
// Windows host. Drive-relative forms such as C:vendor/tool are rejected for the
// same reason.
func hasWindowsVolumePrefix(raw string) bool {
	if len(raw) < 2 || raw[1] != ':' {
		return false
	}
	c := raw[0]
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func (a Artifact) Validate() error {
	if err := a.Kind.Validate(); err != nil {
		return err
	}
	if a.URL != "" {
		if err := validateCredentialFreeURL(a.URL); err != nil {
			return fmt.Errorf("artifact URL: %w", err)
		}
	}
	if a.SignatureURL != "" {
		if err := validateCredentialFreeURL(a.SignatureURL); err != nil {
			return fmt.Errorf("artifact signature URL: %w", err)
		}
	}
	if strings.TrimSpace(a.Checksum) != a.Checksum || strings.ContainsRune(a.Checksum, '\x00') {
		return fmt.Errorf("artifact checksum must not contain surrounding whitespace or NUL")
	}
	if a.URL != "" && a.LocalPath != "" {
		return fmt.Errorf("artifact cannot specify both url and local_path")
	}
	if a.URL == "" && a.LocalPath == "" {
		return fmt.Errorf("artifact requires url or local_path")
	}
	if a.LocalPath != "" {
		clean, err := NormalizeProjectPath(a.LocalPath)
		if err != nil {
			return err
		}
		if clean != a.LocalPath {
			return fmt.Errorf("local artifact path %q is not canonical; use %q", a.LocalPath, clean)
		}
		if a.SignatureURL != "" {
			return fmt.Errorf("local artifact cannot use signature_url; use a local signature reference")
		}
	}
	return nil
}
