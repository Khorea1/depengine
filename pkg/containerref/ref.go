// Package containerref validates the normalized container image identity used
// by depengine's container method. The schema deliberately separates the
// repository (source) from the mutable tag so adapters never have to guess how
// to split a user-provided reference.
package containerref

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateRepository requires an untagged, undigested repository reference.
// A registry port is allowed because its colon appears before the final slash
// (for example registry.example:5000/team/tool).
func ValidateRepository(source string) error {
	if source == "" {
		return fmt.Errorf("container source is empty")
	}
	if strings.Contains(source, "://") {
		return fmt.Errorf("container source must be an image repository, not a URL")
	}
	if strings.Contains(source, "@") {
		return fmt.Errorf("container source must not include a digest; use an untagged repository")
	}
	if strings.HasPrefix(source, "/") || strings.HasSuffix(source, "/") || strings.Contains(source, "//") {
		return fmt.Errorf("container source has an invalid repository path")
	}
	for _, r := range source {
		if unicode.IsSpace(r) {
			return fmt.Errorf("container source must not contain whitespace")
		}
	}
	lastSlash := strings.LastIndexByte(source, '/')
	lastColon := strings.LastIndexByte(source, ':')
	if lastColon > lastSlash {
		return fmt.Errorf("container source must not include a tag; set tag separately")
	}
	return nil
}

// ValidateTag validates the subset of the OCI/Docker tag grammar depengine
// accepts. Tags are kept separate from source so a single canonical reference
// can be constructed as source+":"+tag.
func ValidateTag(tag string) error {
	if tag == "" {
		return nil // empty means the documented default: latest
	}
	if len(tag) > 128 {
		return fmt.Errorf("container tag is longer than 128 characters")
	}
	for i, r := range tag {
		valid := r == '_' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !valid {
			return fmt.Errorf("container tag contains invalid character %q", r)
		}
		if i == 0 && (r == '-' || r == '.') {
			return fmt.Errorf("container tag must start with a letter, digit, or underscore")
		}
	}
	return nil
}

// ValidateDigest accepts immutable OCI-style sha256 digests. depengine keeps
// digest support deliberately narrow until additional algorithms are needed;
// accepting only sha256 avoids pretending that an arbitrary algorithm label
// has been verified by the local container engine.
func ValidateDigest(digest string) error {
	if digest == "" {
		return nil
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(strings.ToLower(digest), prefix) {
		return fmt.Errorf("container digest must use sha256:<64 hex characters>")
	}
	hexPart := digest[len(prefix):]
	if len(hexPart) != 64 {
		return fmt.Errorf("container digest must contain exactly 64 hex characters")
	}
	for _, r := range hexPart {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return fmt.Errorf("container digest contains invalid hexadecimal character %q", r)
		}
	}
	return nil
}

// NormalizePlatform validates and canonicalizes an OCI platform selector in
// os/arch[/variant] form. Keeping this separate from repository identity lets
// callers request a non-host image without encoding host-specific defaults.
func NormalizePlatform(platform string) (string, error) {
	platform = strings.TrimSpace(strings.ToLower(platform))
	if platform == "" {
		return "", nil
	}
	parts := strings.Split(platform, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return "", fmt.Errorf("container platform must use os/arch[/variant]")
	}
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("container platform components must be non-empty")
		}
		for _, r := range part {
			valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.'
			if !valid {
				return "", fmt.Errorf("container platform component %q contains invalid character %q", part, r)
			}
		}
	}
	return strings.Join(parts, "/"), nil
}

// Reference builds the canonical image reference after validating repository,
// tag and digest. tag and digest are mutually exclusive; an empty pair uses
// the conventional latest tag.
func Reference(source, tag, digest string) (string, error) {
	if err := ValidateRepository(source); err != nil {
		return "", err
	}
	if tag != "" && digest != "" {
		return "", fmt.Errorf("container tag and digest are mutually exclusive")
	}
	if err := ValidateTag(tag); err != nil {
		return "", err
	}
	if err := ValidateDigest(digest); err != nil {
		return "", err
	}
	if digest != "" {
		return source + "@" + strings.ToLower(digest), nil
	}
	if tag == "" {
		tag = "latest"
	}
	return source + ":" + tag, nil
}
