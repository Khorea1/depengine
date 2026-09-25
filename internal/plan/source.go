package plan

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
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
	Kind      string           `json:"kind,omitempty"`
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
	if strings.TrimSpace(s.Kind) != s.Kind || strings.ContainsRune(s.Kind, '\x00') {
		return errors.New("source kind must not contain surrounding whitespace or NUL")
	}
	if strings.TrimSpace(s.Name) != s.Name {
		return errors.New("source name must not contain leading or trailing whitespace")
	}
	if strings.ContainsRune(s.Name, '\x00') {
		return errors.New("source name contains NUL")
	}
	if s.Name == "" && s.URL == "" {
		return errors.New("source requires name or URL")
	}
	if s.URL != "" {
		if s.Role == SourceHostConfiguration && (s.Kind == "apt-ppa" || s.Kind == "dnf-copr") {
			return fmt.Errorf("source URL is unsupported for host source kind %q", s.Kind)
		}
		if err := validateSourceURL(s.URL); err != nil {
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
	if strings.ContainsRune(t.KeyReference, '\x00') || strings.ContainsRune(t.Fingerprint, '\x00') {
		return errors.New("trust fields must not contain NUL")
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
	if strings.ContainsRune(s.Provider, '\x00') || strings.ContainsRune(s.Name, '\x00') {
		return errors.New("secret reference fields must not contain NUL")
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
		if out[i].Trust != nil {
			trust := *out[i].Trust
			out[i].Trust = &trust
		}
		if out[i].SecretRef != nil {
			secret := *out[i].SecretRef
			out[i].SecretRef = &secret
		}
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
	return string(s.Role) + "\x00" + strings.ToLower(s.Kind) + "\x00" + strings.ToLower(s.Name) + "\x00" + sanitizeLockReference(s.URL)
}

// scpLikeReference matches the scp-style git remote "user@host:path", which is
// not a URL and never carries a password.
// placeholderToken matches unexpanded {name} placeholders, which are legal
// anywhere in a manifest URL (including the host) before expansion.
var placeholderToken = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)

var scpLikeReference = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:[^/\\:]`)

// validateCredentialFreeReference rejects references that embed secrets.
// A password is always forbidden. A bare username is a login name, not a
// secret, for SSH transports (ssh://git@host); for every other scheme it can
// be a token (https://<token>@host) and is forbidden too.
func validateCredentialFreeReference(raw string) error {
	if strings.TrimSpace(raw) != raw {
		return errors.New("reference must not contain leading or trailing whitespace")
	}
	if !strings.Contains(raw, "://") && scpLikeReference.MatchString(raw) {
		return nil
	}
	u, err := url.Parse(placeholderToken.ReplaceAllString(raw, "x"))
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}
	// Credential checks run for every parseable reference, host or not:
	// "a://user:pass@" parses with an empty host, so gating on the host
	// accepted passwords the contract always forbids. Plain symbolic
	// references such as "conda-forge" or "crates-io" never carry userinfo
	// or query material and still pass unchanged.
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword || !isSSHScheme(u.Scheme) {
			return errors.New("literal URL credentials are forbidden; use secret_ref")
		}
	}
	for key := range u.Query() {
		if run.IsSensitiveQueryKey(key) {
			return fmt.Errorf("sensitive query parameter %q is forbidden; use secret_ref", key)
		}
	}
	if key, ok := firstSensitiveURLParameter(u.Fragment); ok {
		return fmt.Errorf("sensitive URL fragment parameter %q is forbidden; use secret_ref", key)
	}
	return nil
}

func firstSensitiveURLParameter(raw string) (string, bool) {
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '&' || r == ';' }) {
		key := part
		if i := strings.IndexByte(key, '='); i >= 0 {
			key = key[:i]
		}
		if decoded, err := url.QueryUnescape(key); err == nil {
			key = decoded
		}
		if run.IsSensitiveQueryKey(key) {
			return key, true
		}
	}
	return "", false
}

func sanitizeURLFragment(raw string) string {
	if _, ok := firstSensitiveURLParameter(raw); !ok {
		return raw
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '&' || r == ';' })
	out := parts[:0]
	for _, part := range parts {
		key := part
		if i := strings.IndexByte(key, '='); i >= 0 {
			key = key[:i]
		}
		if decoded, err := url.QueryUnescape(key); err == nil {
			key = decoded
		}
		if run.IsSensitiveQueryKey(key) {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "&")
}

func validateIdentityReference(raw string) error {
	if err := validateCredentialFreeReference(raw); err != nil {
		return err
	}
	if strings.Contains(raw, "://") {
		return validateCredentialFreeURL(raw)
	}
	return nil
}

func validateSourceURL(raw string) error {
	if strings.TrimSpace(raw) != raw || strings.ContainsRune(raw, '\x00') {
		return errors.New("URL must not contain surrounding whitespace or NUL")
	}
	if scpLikeReference.MatchString(raw) {
		return nil
	}
	return validateCredentialFreeURL(raw)
}

func validateCredentialFreeURL(raw string) error {
	if err := validateCredentialFreeReference(raw); err != nil {
		return err
	}
	u, err := url.Parse(placeholderToken.ReplaceAllString(raw, "x"))
	if err != nil || u.Scheme == "" {
		return errors.New("URL must be absolute")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ssh", "git":
		if u.Host == "" {
			return errors.New("network URL requires a host")
		}
	}
	return nil
}

func isSSHScheme(scheme string) bool {
	scheme = strings.ToLower(scheme)
	return scheme == "ssh" || strings.HasSuffix(scheme, "+ssh")
}
