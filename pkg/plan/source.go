package plan

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// SourceRole describes how a named source participates in resolution. Keeping
// these roles distinct prevents host configuration mutation from being
// conflated with a per-install registry/index/channel selection.
type SourceRole string

const (
	SourceHostConfiguration SourceRole = "host_configuration"
	SourceSelection         SourceRole = "selection"
	SourceRegistry          SourceRole = "registry"
	SourceIndex             SourceRole = "index"
	SourceChannel           SourceRole = "channel"
	SourceRemote            SourceRole = "remote"
)

// SourceTrust records declarative trust identity. KeyReference names public
// key material or a keyring entry; Fingerprint pins the expected key identity.
// Neither field may contain secret material.
type SourceTrust struct {
	KeyReference string `json:"key_reference,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
}

// SourceReference is the adapter-neutral identity of a package source,
// registry, index, channel, remote, or host source configuration. Literal
// credentials are forbidden; authenticated sources refer to external secret
// material through SecretRef.
type SourceReference struct {
	Role      SourceRole       `json:"role"`
	Name      string           `json:"name,omitempty"`
	URL       string           `json:"url,omitempty"`
	Owned     bool             `json:"owned,omitempty"`
	Trust     *SourceTrust     `json:"trust,omitempty"`
	SecretRef *SecretReference `json:"secret_ref,omitempty"`
}

// Validate enforces source identity and secret-safety independently of any
// adapter. A source may be identified by name, URL, or both.
func (s SourceReference) Validate() error {
	switch s.Role {
	case SourceHostConfiguration, SourceSelection, SourceRegistry, SourceIndex, SourceChannel, SourceRemote:
	default:
		return fmt.Errorf("invalid source role %q", s.Role)
	}
	if strings.TrimSpace(s.Name) != s.Name {
		return errors.New("source name must not contain leading or trailing whitespace")
	}
	if s.Name == "" && s.URL == "" {
		return errors.New("source requires name or URL")
	}
	if s.URL != "" {
		if err := validateCredentialFreeReference(s.URL); err != nil {
			return fmt.Errorf("source URL: %w", err)
		}
	}
	if s.SecretRef != nil {
		if err := s.SecretRef.Validate(); err != nil {
			return fmt.Errorf("source secret reference: %w", err)
		}
	}
	if s.Trust != nil {
		if err := s.Trust.Validate(); err != nil {
			return fmt.Errorf("source trust: %w", err)
		}
	}
	return nil
}

// Validate checks that trust metadata is useful and does not look like a
// credential-bearing URL.
func (t SourceTrust) Validate() error {
	if strings.TrimSpace(t.KeyReference) != t.KeyReference || strings.TrimSpace(t.Fingerprint) != t.Fingerprint {
		return errors.New("trust fields must not contain leading or trailing whitespace")
	}
	if t.KeyReference == "" && t.Fingerprint == "" {
		return errors.New("trust metadata requires key reference or fingerprint")
	}
	if t.KeyReference != "" {
		if err := validateCredentialFreeReference(t.KeyReference); err != nil {
			return fmt.Errorf("key reference: %w", err)
		}
	}
	return nil
}

// Validate checks a secret reference without dereferencing it. Providers are
// deliberately opaque but must be stable, non-empty identifiers.
func (s SecretReference) Validate() error {
	if strings.TrimSpace(s.Provider) != s.Provider || strings.TrimSpace(s.Name) != s.Name {
		return errors.New("secret reference fields must not contain leading or trailing whitespace")
	}
	if s.Provider == "" {
		return errors.New("secret reference provider is required")
	}
	if s.Name == "" {
		return errors.New("secret reference name is required")
	}
	return nil
}

// CanonicalSources returns a deterministic copy suitable for snapshots and
// lock projection. Secret references are intentionally preserved here because
// this is still an in-memory plan object; lock projection drops them.
func CanonicalSources(in []SourceReference) ([]SourceReference, error) {
	out := append([]SourceReference(nil), in...)
	for i := range out {
		if err := out[i].Validate(); err != nil {
			return nil, fmt.Errorf("source %d: %w", i, err)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return sourceSortKey(out[i]) < sourceSortKey(out[j])
	})
	for i := 1; i < len(out); i++ {
		if sourceSortKey(out[i-1]) == sourceSortKey(out[i]) {
			return nil, fmt.Errorf("duplicate source identity %q", sourceSortKey(out[i]))
		}
	}
	return out, nil
}

func sourceSortKey(s SourceReference) string {
	return string(s.Role) + "\x00" + strings.ToLower(s.Name) + "\x00" + sanitizeLockReference(s.URL)
}

func validateCredentialFreeReference(raw string) error {
	if strings.TrimSpace(raw) != raw {
		return errors.New("reference must not contain leading or trailing whitespace")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}
	// Plain symbolic references such as "conda-forge" or "crates-io" are
	// valid. URL-specific credential checks apply only when a host is present.
	if u.Host == "" {
		return nil
	}
	if u.User != nil {
		return errors.New("literal URL credentials are forbidden; use secret_ref")
	}
	for key := range u.Query() {
		if _, sensitive := sensitiveLockQueryKeys[strings.ToLower(key)]; sensitive {
			return fmt.Errorf("sensitive query parameter %q is forbidden; use secret_ref", key)
		}
	}
	return nil
}
