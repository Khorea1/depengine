package plan

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/Khorea1/depengine/pkg/formatversion"
	"github.com/Khorea1/depengine/pkg/run"
)

// CurrentLockVersion is the schema version of the adapter-neutral lock
// projection. Breaking changes must increment this value; readers must reject
// unknown versions until an explicit migration is implemented.
const CurrentLockVersion = formatversion.CurrentLockVersion

// LockStability describes whether a resolved plan contains enough concrete
// identity to prevent mutable intent from being re-resolved on reinstall.
type LockStability string

const (
	LockImmutable   LockStability = "immutable"
	LockUnavailable LockStability = "unavailable"
)

// ErrLockUnavailable is returned when callers require an immutable pin but the
// selected manager/resolver could not provide one.
var ErrLockUnavailable = errors.New("immutable lock identity unavailable")

// ErrLockMismatch is returned when a newly resolved plan does not reproduce an
// immutable lock identity. Callers must require an explicit lock update rather
// than silently accepting the new mutable resolution.
var ErrLockMismatch = errors.New("resolved plan does not match immutable lock")

// LockedArtifact is the reproducibility identity of one resolved artifact.
// Credentials are never part of this identity.
type LockedArtifact struct {
	Kind               ArtifactKind `json:"kind,omitempty"`
	URL                string       `json:"url,omitempty"`
	LocalPath          string       `json:"local_path,omitempty"`
	Checksum           string       `json:"checksum,omitempty"`
	ChecksumURL        string       `json:"checksum_url,omitempty"`
	ChecksumFileFormat string       `json:"checksum_file_format,omitempty"`
	SignatureURL       string       `json:"signature_url,omitempty"`
	SigningKey         string       `json:"signing_key,omitempty"`
}

// LockedSource is the persistence-safe identity of a source used during
// resolution. Secret references are deliberately omitted: authentication
// remains external to committed lockfiles.
type LockedSource struct {
	Role  SourceRole   `json:"role"`
	Kind  string       `json:"kind,omitempty"`
	Name  string       `json:"name,omitempty"`
	URL   string       `json:"url,omitempty"`
	Owned bool         `json:"owned,omitempty"`
	Trust *SourceTrust `json:"trust,omitempty"`
}

// LockIdentity is the immutable-resolution projection of ResolvedIdentity.
// RequestedVersion is intentionally excluded: locks consume concrete identity,
// not the mutable intent that produced it.
type LockIdentity struct {
	Package      string             `json:"package,omitempty"`
	Version      string             `json:"version,omitempty"`
	Revision     string             `json:"revision,omitempty"`
	Digest       string             `json:"digest,omitempty"`
	Source       string             `json:"source,omitempty"`
	Registry     string             `json:"registry,omitempty"`
	Scope        string             `json:"scope,omitempty"`
	Environment  *EnvironmentTarget `json:"environment,omitempty"`
	Architecture string             `json:"architecture,omitempty"`
	Platform     string             `json:"platform,omitempty"`
	Artifacts    []LockedArtifact   `json:"artifacts,omitempty"`
	Sources      []LockedSource     `json:"sources,omitempty"`
}

func (a LockedArtifact) validate() error {
	if err := a.Kind.Validate(); err != nil {
		return err
	}
	if a.URL != "" && a.LocalPath != "" {
		return errors.New("artifact cannot specify both url and local_path")
	}
	if a.URL == "" && a.LocalPath == "" {
		return errors.New("artifact requires url or local_path")
	}
	if a.URL != "" {
		if err := validateCredentialFreeURL(a.URL); err != nil {
			return fmt.Errorf("artifact URL: %w", err)
		}
		if canonical := sanitizeLockReference(a.URL); canonical != a.URL {
			return fmt.Errorf("artifact URL is not canonical; use %q", canonical)
		}
	}
	if a.ChecksumURL != "" {
		if a.LocalPath != "" {
			return errors.New("local artifact cannot persist a remote checksum URL")
		}
		if err := validateCredentialFreeURL(a.ChecksumURL); err != nil {
			return fmt.Errorf("artifact checksum URL: %w", err)
		}
		if canonical := sanitizeLockReference(a.ChecksumURL); canonical != a.ChecksumURL {
			return fmt.Errorf("artifact checksum URL is not canonical; use %q", canonical)
		}
	}
	switch a.ChecksumFileFormat {
	case "", "sha256sum", "bsd", "raw":
	default:
		return fmt.Errorf("unsupported checksum file format %q", a.ChecksumFileFormat)
	}
	if a.SignatureURL != "" {
		if a.LocalPath != "" {
			return errors.New("local artifact cannot persist a remote signature URL")
		}
		if err := validateCredentialFreeURL(a.SignatureURL); err != nil {
			return fmt.Errorf("artifact signature URL: %w", err)
		}
		if canonical := sanitizeLockReference(a.SignatureURL); canonical != a.SignatureURL {
			return fmt.Errorf("artifact signature URL is not canonical; use %q", canonical)
		}
	}
	if strings.TrimSpace(a.SigningKey) != a.SigningKey || strings.ContainsRune(a.SigningKey, '\x00') {
		return errors.New("artifact signing key must not contain surrounding whitespace or NUL")
	}
	if strings.Contains(a.SigningKey, "://") {
		if err := validateCredentialFreeURL(a.SigningKey); err != nil {
			return fmt.Errorf("artifact signing key URL: %w", err)
		}
		if canonical := sanitizeLockReference(a.SigningKey); canonical != a.SigningKey {
			return fmt.Errorf("artifact signing key URL is not canonical; use %q", canonical)
		}
	}
	if a.LocalPath != "" {
		clean, err := NormalizeProjectPath(a.LocalPath)
		if err != nil {
			return err
		}
		if clean != a.LocalPath {
			return fmt.Errorf("local path %q is not canonical; use %q", a.LocalPath, clean)
		}
	}
	if err := validateLockedChecksum(a.Checksum); err != nil {
		return err
	}
	return nil
}

func validateLockedChecksum(value string) error {
	if value == "" {
		return nil
	}
	if err := validateConcreteDigestSyntax(value); err != nil {
		return fmt.Errorf("artifact checksum: %w", err)
	}
	return nil
}

func validateConcreteDigestSyntax(value string) error {
	if strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
		return errors.New("must not contain surrounding whitespace or NUL")
	}
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("must use algorithm:hex syntax")
	}
	if strings.EqualFold(parts[1], "auto") {
		return errors.New("must be concrete in a lock; :auto is unresolved")
	}
	if strings.TrimSpace(parts[0]) != parts[0] {
		return errors.New("algorithm is malformed")
	}
	if !validDigestAlgorithm(parts[0]) {
		return errors.New("algorithm is malformed")
	}
	decoded, err := hex.DecodeString(parts[1])
	if err != nil {
		return errors.New("value must be hexadecimal")
	}
	wantBytes := map[string]int{
		"md5":    16,
		"sha1":   20,
		"sha256": 32,
		"sha512": 64,
	}[strings.ToLower(parts[0])]
	if wantBytes != 0 && len(decoded) != wantBytes {
		return fmt.Errorf("%s digest must contain %d hexadecimal bytes", strings.ToLower(parts[0]), wantBytes)
	}
	return nil
}

func validDigestAlgorithm(value string) bool {
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.' || r == '+') {
			continue
		}
		return false
	}
	return value != ""
}

func (s LockedSource) validate() error {
	source := SourceReference{
		Role:  s.Role,
		Kind:  s.Kind,
		Name:  s.Name,
		URL:   s.URL,
		Owned: s.Owned,
		Trust: s.Trust,
	}
	if err := source.Validate(); err != nil {
		return err
	}
	if s.URL != "" {
		if canonical := sanitizeLockReference(s.URL); canonical != s.URL {
			return fmt.Errorf("source URL is not canonical; use %q", canonical)
		}
	}
	return nil
}

func (i LockIdentity) validate() error {
	if strings.TrimSpace(i.Package) != i.Package || strings.ContainsRune(i.Package, '\x00') {
		return errors.New("lock package identity must not contain surrounding whitespace or NUL")
	}
	for label, raw := range map[string]string{"source": i.Source, "registry": i.Registry} {
		if raw == "" {
			continue
		}
		if err := validateIdentityReference(raw); err != nil {
			return fmt.Errorf("lock %s: %w", label, err)
		}
		if strings.Contains(raw, "://") {
			if canonical := sanitizeLockReference(raw); canonical != raw {
				return fmt.Errorf("lock %s is not canonical; use %q", label, canonical)
			}
		}
	}
	if i.Scope != "" {
		if _, err := ParseScope(i.Scope); err != nil {
			return fmt.Errorf("lock scope: %w", err)
		}
	}
	if i.Environment != nil {
		if err := i.Environment.Validate(); err != nil {
			return fmt.Errorf("lock environment: %w", err)
		}
	}
	for label, value := range map[string]string{"version": i.Version, "revision": i.Revision, "digest": i.Digest, "architecture": i.Architecture, "platform": i.Platform} {
		if strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("lock %s must not contain surrounding whitespace or NUL", label)
		}
	}
	if i.Digest != "" {
		if err := validateConcreteDigestSyntax(i.Digest); err != nil {
			return fmt.Errorf("lock digest: %w", err)
		}
	}
	previousArtifactKey := ""
	for idx, artifact := range i.Artifacts {
		if err := artifact.validate(); err != nil {
			return fmt.Errorf("lock artifact %d: %w", idx, err)
		}
		key := artifactIdentityKey(artifact.Kind, artifact.URL, artifact.LocalPath)
		if idx > 0 && key <= previousArtifactKey {
			if key == previousArtifactKey {
				return fmt.Errorf("duplicate lock artifact location %q", key)
			}
			return errors.New("lock artifacts are not in canonical order")
		}
		previousArtifactKey = key
	}
	previousSourceKey := ""
	for idx, source := range i.Sources {
		if err := source.validate(); err != nil {
			return fmt.Errorf("lock source %d: %w", idx, err)
		}
		key := sourceSortKey(SourceReference{Role: source.Role, Kind: source.Kind, Name: source.Name, URL: source.URL})
		if idx > 0 && key <= previousSourceKey {
			if key == previousSourceKey {
				return fmt.Errorf("duplicate lock source identity %q", key)
			}
			return fmt.Errorf("lock sources are not in canonical order")
		}
		previousSourceKey = key
	}
	return nil
}

// LockProjection is the versioned, adapter-neutral lock representation for one
// resolved installation candidate. An unavailable projection is diagnostic and
// must not be consumed as if it pinned the install.
type LockProjection struct {
	Version         int               `json:"lock_version"`
	Tool            ToolIdentity      `json:"tool"`
	Candidate       CandidateIdentity `json:"candidate"`
	RequestedMode   VersionMode       `json:"requested_mode,omitempty"`
	RequestedIntent *VersionIntent    `json:"requested_intent,omitempty"`
	Stability       LockStability     `json:"stability"`
	Reason          string            `json:"reason,omitempty"`
	Identity        LockIdentity      `json:"identity"`
}

// MarshalJSON is the persistence/debugging safety boundary for lock projections.
// It redacts credential-bearing strings even when a caller constructs a
// projection directly instead of using ProjectLock.
func (p LockProjection) MarshalJSON() ([]byte, error) {
	type plain LockProjection
	safe := p.redacted()
	return json.Marshal(plain(redactAllStrings(safe)))
}

func (p LockProjection) redacted() LockProjection {
	p.Identity.Artifacts = append([]LockedArtifact(nil), p.Identity.Artifacts...)
	p.Identity.Sources = append([]LockedSource(nil), p.Identity.Sources...)
	p.Reason = run.RedactSensitiveText(p.Reason)
	p.Identity.Source = sanitizeLockReference(p.Identity.Source)
	p.Identity.Registry = sanitizeLockReference(p.Identity.Registry)
	for i := range p.Identity.Artifacts {
		p.Identity.Artifacts[i].URL = sanitizeLockReference(p.Identity.Artifacts[i].URL)
		p.Identity.Artifacts[i].ChecksumURL = sanitizeLockReference(p.Identity.Artifacts[i].ChecksumURL)
		p.Identity.Artifacts[i].SignatureURL = sanitizeLockReference(p.Identity.Artifacts[i].SignatureURL)
		p.Identity.Artifacts[i].SigningKey = sanitizeSigningKeyReference(p.Identity.Artifacts[i].SigningKey)
		p.Identity.Artifacts[i].Checksum = run.RedactSensitiveText(p.Identity.Artifacts[i].Checksum)
	}
	for i := range p.Identity.Sources {
		p.Identity.Sources[i].URL = sanitizeLockReference(p.Identity.Sources[i].URL)
		if p.Identity.Sources[i].Trust != nil {
			trust := *p.Identity.Sources[i].Trust
			trust.KeyReference = sanitizeLockReference(trust.KeyReference)
			p.Identity.Sources[i].Trust = &trust
		}
	}
	return p
}

// ProjectLock derives lock identity from an already-resolved plan. It is pure:
// it performs no resolution, subprocess execution, network access, or host
// mutation. A plan that cannot be pinned is returned with LockUnavailable so a
// caller can explain the limitation instead of silently re-resolving later.
func ProjectLock(p ResolvedInstallPlan) (LockProjection, error) {
	if err := p.Validate(); err != nil {
		return LockProjection{}, fmt.Errorf("project lock: invalid plan: %w", err)
	}

	projection := LockProjection{
		Version:   CurrentLockVersion,
		Tool:      p.Tool,
		Candidate: p.Candidate,
		Identity: LockIdentity{
			Package:      p.Identity.Package,
			Version:      p.Identity.Version,
			Revision:     p.Identity.Revision,
			Digest:       canonicalDigest(p.Identity.Digest),
			Source:       sanitizeLockReference(p.Identity.Source),
			Registry:     sanitizeLockReference(p.Identity.Registry),
			Scope:        p.Identity.Scope,
			Architecture: p.Identity.Architecture,
			Platform:     p.Identity.Platform,
		},
	}
	if p.Identity.Environment != nil {
		environment := *p.Identity.Environment
		projection.Identity.Environment = &environment
	}
	if p.Identity.RequestedVersion != nil {
		projection.RequestedMode = p.Identity.RequestedVersion.Mode
		intent := *p.Identity.RequestedVersion
		if intent.Mode == VersionDigest {
			intent.Value = canonicalDigest(intent.Value)
		}
		if intent.Channel != nil {
			channel := *intent.Channel
			intent.Channel = &channel
		}
		projection.RequestedIntent = &intent
	}
	for _, artifact := range p.Artifacts {
		projection.Identity.Artifacts = append(projection.Identity.Artifacts, LockedArtifact{
			Kind:               artifact.Kind,
			URL:                sanitizeLockReference(artifact.URL),
			LocalPath:          artifact.LocalPath,
			Checksum:           canonicalDigest(run.RedactSensitiveText(artifact.Checksum)),
			ChecksumURL:        sanitizeLockReference(artifact.ChecksumURL),
			ChecksumFileFormat: artifact.ChecksumFileFormat,
			SignatureURL:       sanitizeLockReference(artifact.SignatureURL),
			SigningKey:         sanitizeSigningKeyReference(artifact.SigningKey),
		})
	}
	slices.SortFunc(projection.Identity.Artifacts, func(a, b LockedArtifact) int {
		return strings.Compare(
			artifactIdentityKey(a.Kind, a.URL, a.LocalPath),
			artifactIdentityKey(b.Kind, b.URL, b.LocalPath),
		)
	})
	canonicalSources, err := CanonicalSources(p.Sources)
	if err != nil {
		return LockProjection{}, fmt.Errorf("project lock: %w", err)
	}
	for _, source := range canonicalSources {
		locked := LockedSource{
			Role:  source.Role,
			Kind:  strings.ToLower(source.Kind),
			Name:  strings.ToLower(source.Name),
			URL:   sanitizeLockReference(source.URL),
			Owned: source.Owned,
		}
		if source.Trust != nil {
			trust := *source.Trust
			trust.KeyReference = sanitizeLockReference(trust.KeyReference)
			trust.Fingerprint = canonicalTrustFingerprint(trust.Fingerprint)
			locked.Trust = &trust
		}
		projection.Identity.Sources = append(projection.Identity.Sources, locked)
	}

	if reason := lockUnavailabilityReason(p, projection.Identity); reason != "" {
		projection.Stability = LockUnavailable
		projection.Reason = reason
	} else {
		projection.Stability = LockImmutable
	}
	if err := projection.Validate(); err != nil {
		return LockProjection{}, fmt.Errorf("project lock: %w", err)
	}
	return projection, nil
}

// RequireImmutable rejects diagnostic/unpinnable projections. Install/update
// paths that promise reproducibility should call this before persisting or
// consuming a lock entry.
func (p LockProjection) RequireImmutable() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Stability != LockImmutable {
		return fmt.Errorf("%w: %s", ErrLockUnavailable, p.Reason)
	}
	return nil
}

// VerifyResolvedPlanAgainstLock projects an already-resolved plan and proves
// that it reproduces the supplied immutable lock identity. It never performs
// resolution itself. A mismatch requires an explicit lock update; mutable
// intent must not silently replace the pinned revision/digest/version.
func VerifyResolvedPlanAgainstLock(expected LockProjection, resolved ResolvedInstallPlan) error {
	if err := expected.RequireImmutable(); err != nil {
		return fmt.Errorf("expected lock: %w", err)
	}
	actual, err := ProjectLock(resolved)
	if err != nil {
		return err
	}
	if err := actual.RequireImmutable(); err != nil {
		return fmt.Errorf("resolved plan: %w", err)
	}
	if expected.Tool != actual.Tool {
		return fmt.Errorf("%w: tool identity changed", ErrLockMismatch)
	}
	if expected.Candidate != actual.Candidate {
		return fmt.Errorf("%w: selected candidate changed", ErrLockMismatch)
	}
	if expected.RequestedMode != actual.RequestedMode {
		return fmt.Errorf("%w: requested version mode changed", ErrLockMismatch)
	}
	if expected.RequestedIntent != nil && !versionIntentEqual(expected.RequestedIntent, actual.RequestedIntent) {
		return fmt.Errorf("%w: requested version intent changed", ErrLockMismatch)
	}
	if field := lockIdentityMismatchField(expected.Identity, actual.Identity); field != "" {
		return fmt.Errorf("%w: %s identity changed", ErrLockMismatch, field)
	}
	return nil
}

func versionIntentEqual(expected, actual *VersionIntent) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}
	if expected.Mode != actual.Mode {
		return false
	}
	if expected.Mode == VersionDigest {
		return canonicalDigest(expected.Value) == canonicalDigest(actual.Value) && reflect.DeepEqual(expected.Channel, actual.Channel)
	}
	return reflect.DeepEqual(expected, actual)
}

func lockIdentityMismatchField(expected, actual LockIdentity) string {
	switch {
	case expected.Package != actual.Package:
		return "package"
	case expected.Version != actual.Version:
		return "version"
	case expected.Revision != actual.Revision:
		return "revision"
	case canonicalDigest(expected.Digest) != canonicalDigest(actual.Digest):
		return "digest"
	case sanitizeLockReference(expected.Source) != sanitizeLockReference(actual.Source):
		return "source"
	case sanitizeLockReference(expected.Registry) != sanitizeLockReference(actual.Registry):
		return "registry"
	case expected.Scope != actual.Scope:
		return "scope"
	case !reflect.DeepEqual(expected.Environment, actual.Environment):
		return "environment"
	case expected.Architecture != actual.Architecture:
		return "architecture"
	case expected.Platform != actual.Platform:
		return "platform"
	case !lockedArtifactsEqual(expected.Artifacts, actual.Artifacts):
		return "artifact"
	case !lockedSourcesEqual(expected.Sources, actual.Sources):
		return "source"
	default:
		return ""
	}
}

func canonicalDigest(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return value
	}
	return strings.ToLower(parts[0]) + ":" + strings.ToLower(parts[1])
}

func lockedArtifactsEqual(expected, actual []LockedArtifact) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		a, b := expected[i], actual[i]
		if a.Kind != b.Kind || sanitizeLockReference(a.URL) != sanitizeLockReference(b.URL) || a.LocalPath != b.LocalPath || canonicalDigest(a.Checksum) != canonicalDigest(b.Checksum) || sanitizeLockReference(a.ChecksumURL) != sanitizeLockReference(b.ChecksumURL) || a.ChecksumFileFormat != b.ChecksumFileFormat || sanitizeLockReference(a.SignatureURL) != sanitizeLockReference(b.SignatureURL) || sanitizeSigningKeyReference(a.SigningKey) != sanitizeSigningKeyReference(b.SigningKey) {
			return false
		}
	}
	return true
}

func lockedSourcesEqual(expected, actual []LockedSource) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		a, b := expected[i], actual[i]
		if a.Role != b.Role || !strings.EqualFold(a.Kind, b.Kind) || !strings.EqualFold(a.Name, b.Name) || sanitizeLockReference(a.URL) != sanitizeLockReference(b.URL) || a.Owned != b.Owned || !sourceTrustEqual(a.Trust, b.Trust) {
			return false
		}
	}
	return true
}

func sourceTrustEqual(a, b *SourceTrust) bool {
	if a == nil || b == nil {
		return a == b
	}
	return sanitizeLockReference(a.KeyReference) == sanitizeLockReference(b.KeyReference) && canonicalTrustFingerprint(a.Fingerprint) == canonicalTrustFingerprint(b.Fingerprint)
}

func canonicalTrustFingerprint(value string) string {
	return strings.ToUpper(strings.ReplaceAll(value, " ", ""))
}

// Validate checks lock schema and stability invariants without consulting an
// adapter or the host.
func (p LockProjection) Validate() error {
	if err := formatversion.ValidateReadVersion(formatversion.Lock, p.Version); err != nil {
		return err
	}
	if strings.TrimSpace(p.Tool.Name) != p.Tool.Name || p.Tool.Name == "" || strings.ContainsRune(p.Tool.Name, '\x00') {
		return errors.New("lock tool name is required and must not contain surrounding whitespace or NUL")
	}
	if strings.TrimSpace(p.Candidate.Method) != p.Candidate.Method || p.Candidate.Method == "" || strings.ContainsRune(p.Candidate.Method, '\x00') {
		return errors.New("lock candidate method is required and must not contain surrounding whitespace or NUL")
	}
	switch p.Stability {
	case LockImmutable:
		if p.Reason != "" {
			return errors.New("immutable lock projection cannot contain an unavailability reason")
		}
	case LockUnavailable:
		if strings.TrimSpace(p.Reason) == "" {
			return errors.New("unavailable lock projection requires a reason")
		}
	default:
		return fmt.Errorf("invalid lock stability %q", p.Stability)
	}
	if err := p.Identity.validate(); err != nil {
		return err
	}
	if p.RequestedMode != "" {
		intent := VersionIntent{Mode: p.RequestedMode}
		switch p.RequestedMode {
		case VersionLatest:
			// latest is the only mode whose shape validates without a value.
			if err := intent.Validate(); err != nil {
				return fmt.Errorf("requested lock mode: %w", err)
			}
		case VersionExact, VersionConstraint, VersionGitTag, VersionGitBranch, VersionGitRevision, VersionContainerTag, VersionDigest:
			// Mode membership is sufficient here; the concrete value belongs to
			// the resolved identity, not the lock's requested-mode audit field.
		case VersionChannel:
		default:
			return fmt.Errorf("invalid requested lock mode %q", p.RequestedMode)
		}
	}
	if p.RequestedIntent != nil {
		if err := p.RequestedIntent.Validate(); err != nil {
			return fmt.Errorf("requested lock intent: %w", err)
		}
		if p.RequestedMode == "" {
			return errors.New("requested lock intent requires requested_mode")
		}
		if p.RequestedMode != p.RequestedIntent.Mode {
			return fmt.Errorf("requested lock mode %q does not match requested intent mode %q", p.RequestedMode, p.RequestedIntent.Mode)
		}
		if p.RequestedIntent.Mode == VersionDigest && p.Identity.Digest != "" &&
			canonicalDigest(p.RequestedIntent.Value) != canonicalDigest(p.Identity.Digest) {
			return errors.New("requested lock digest does not match resolved identity digest")
		}
	}
	if p.Stability == LockImmutable {
		requested := p.RequestedIntent
		if requested == nil && p.RequestedMode != "" {
			requested = &VersionIntent{Mode: p.RequestedMode}
		}
		if reason := lockIdentityUnavailabilityReason(requested, p.Identity); reason != "" {
			return fmt.Errorf("immutable lock identity is incomplete: %s", reason)
		}
	}
	return nil
}

func lockUnavailabilityReason(p ResolvedInstallPlan, identity LockIdentity) string {
	return lockIdentityUnavailabilityReason(p.Identity.RequestedVersion, identity)
}

func lockIdentityUnavailabilityReason(requested *VersionIntent, identity LockIdentity) string {
	if requested != nil {
		switch requested.Mode {
		case VersionGitTag, VersionGitBranch, VersionGitRevision:
			if identity.Revision == "" {
				return "git version intent did not resolve to a concrete revision"
			}
		case VersionContainerTag, VersionDigest:
			if identity.Digest == "" {
				return "container identity did not resolve to an immutable digest"
			}
		case VersionLatest, VersionExact, VersionConstraint, VersionChannel:
			if identity.Version == "" && identity.Revision == "" && identity.Digest == "" {
				return "version intent did not resolve to a concrete version, revision, or digest"
			}
		default:
			return fmt.Sprintf("unsupported requested version mode %q", requested.Mode)
		}
	}

	for _, artifact := range identity.Artifacts {
		if artifact.URL == "" && artifact.LocalPath == "" {
			return "resolved artifact has no stable location identity"
		}
		if artifact.Checksum == "" {
			return "resolved artifact has no checksum"
		}
	}

	if requested == nil && identity.Version == "" && identity.Revision == "" && identity.Digest == "" && len(identity.Artifacts) == 0 {
		return "manager did not expose a stable resolved identity"
	}
	return ""
}

// sanitizeLockReference removes credential material rather than replacing it
// with display placeholders. The resulting value can serve as canonical
// persisted identity while authentication remains external to the lockfile.
func sanitizeSigningKeyReference(raw string) string {
	if strings.Contains(raw, "://") {
		return sanitizeLockReference(raw)
	}
	return run.RedactSensitiveText(raw)
}

func sanitizeLockReference(raw string) string {
	if raw == "" {
		return ""
	}
	if scpLikeReference.MatchString(raw) {
		return canonicalSCPReference(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return run.RedactSensitiveText(raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	// DNS names and IPv6 hexadecimal digits are case-insensitive, but an IPv6
	// zone identifier (the %eth0 portion) may name a case-sensitive local
	// interface. Preserve zone-bearing hosts exactly rather than changing
	// transport identity while canonicalizing display spelling.
	if !strings.Contains(u.Host, "%") {
		u.Host = strings.ToLower(u.Host)
	}
	if u.User != nil {
		username := u.User.Username()
		_, hasPassword := u.User.Password()
		if isSSHScheme(u.Scheme) && !hasPassword && username != "" {
			// SSH usernames such as git@host are transport identity, not secret
			// material. Preserve them so the lock can distinguish remotes that use
			// different server-side principals. Password-bearing or non-SSH
			// userinfo is always removed defensively.
			u.User = url.User(username)
		} else {
			u.User = nil
		}
	}
	query := u.Query()
	for key := range query {
		if run.IsSensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()
	u.Fragment = sanitizeURLFragment(u.Fragment)
	return u.String()
}

func canonicalSCPReference(raw string) string {
	at := strings.IndexByte(raw, '@')
	if at <= 0 || at+1 >= len(raw) {
		return raw
	}
	rest := raw[at+1:]
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		return raw
	}
	return raw[:at+1] + strings.ToLower(rest[:colon]) + rest[colon:]
}

// LockDocument is the universal adapter-neutral lock model. Entries are sorted
// by tool name so serialization is deterministic and each tool has exactly one
// selected immutable candidate.
type LockDocument struct {
	Version int              `json:"version"`
	Entries []LockProjection `json:"entries"`
}

// BuildLockDocument projects resolved plans into one deterministic immutable
// lock document. It is deliberately all-or-nothing: if any selected plan cannot
// be pinned, no partial document is returned.
func BuildLockDocument(plans []ResolvedInstallPlan) (LockDocument, error) {
	entries := make([]LockProjection, 0, len(plans))
	seen := make(map[string]struct{}, len(plans))
	for _, resolved := range plans {
		if _, exists := seen[resolved.Tool.Name]; exists {
			return LockDocument{}, fmt.Errorf("lock document: duplicate tool %q", resolved.Tool.Name)
		}
		seen[resolved.Tool.Name] = struct{}{}

		entry, err := ProjectLock(resolved)
		if err != nil {
			return LockDocument{}, err
		}
		if err := entry.RequireImmutable(); err != nil {
			return LockDocument{}, fmt.Errorf("lock document: %s/%s: %w", entry.Tool.Name, entry.Candidate.Method, err)
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b LockProjection) int {
		if cmp := strings.Compare(a.Tool.Name, b.Tool.Name); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Candidate.Method, b.Candidate.Method)
	})

	doc := LockDocument{Version: CurrentLockVersion, Entries: entries}
	if err := doc.Validate(); err != nil {
		return LockDocument{}, fmt.Errorf("lock document: %w", err)
	}
	return doc, nil
}

// Validate enforces the persisted universal-lock invariants.
// EntryForPlan returns the immutable lock entry that a resolver may consume for
// an unresolved/static plan intent. It validates the document, selected
// candidate, requested version semantics, and every identity dimension already
// known by the plan. Concrete fields that are still empty are intentionally not
// compared: those are the values the resolver is expected to consume from the
// returned lock entry rather than re-resolve from a mutable source.
//
// The returned entry is a deep copy. Callers may adapt it for resolver-local
// use without mutating the persisted lock document.
func (d LockDocument) EntryForPlan(intent ResolvedInstallPlan) (LockProjection, error) {
	if err := d.Validate(); err != nil {
		return LockProjection{}, fmt.Errorf("lock document: %w", err)
	}
	if err := intent.Validate(); err != nil {
		return LockProjection{}, fmt.Errorf("plan intent: %w", err)
	}

	idx, found := slices.BinarySearchFunc(d.Entries, intent.Tool.Name, func(entry LockProjection, name string) int {
		return strings.Compare(entry.Tool.Name, name)
	})
	if !found {
		return LockProjection{}, fmt.Errorf("%w: tool %q is not present in lock", ErrLockMismatch, intent.Tool.Name)
	}
	expected := d.Entries[idx]
	if expected.Candidate != intent.Candidate {
		return LockProjection{}, fmt.Errorf("%w: selected candidate changed", ErrLockMismatch)
	}

	requestedMode := VersionMode("")
	var requestedIntent *VersionIntent
	if intent.Identity.RequestedVersion != nil {
		requestedMode = intent.Identity.RequestedVersion.Mode
		requestedIntent = intent.Identity.RequestedVersion
	}
	if expected.RequestedMode != requestedMode {
		return LockProjection{}, fmt.Errorf("%w: requested version mode changed", ErrLockMismatch)
	}
	if expected.RequestedIntent != nil && !versionIntentEqual(expected.RequestedIntent, requestedIntent) {
		return LockProjection{}, fmt.Errorf("%w: requested version intent changed", ErrLockMismatch)
	}
	if expected.RequestedIntent == nil && requestedIntent != nil {
		return LockProjection{}, fmt.Errorf("%w: requested version intent changed", ErrLockMismatch)
	}

	if field := knownLockIdentityMismatchField(intent.Identity, expected.Identity); field != "" {
		return LockProjection{}, fmt.Errorf("%w: known %s identity changed", ErrLockMismatch, field)
	}
	return cloneLockProjection(expected), nil
}

// knownLockIdentityMismatchField compares only identity dimensions already
// known in a static plan. Empty concrete values are unresolved, not wildcards
// for values that the manifest explicitly configured elsewhere.
func knownLockIdentityMismatchField(intent ResolvedIdentity, expected LockIdentity) string {
	switch {
	case intent.Package != "" && intent.Package != expected.Package:
		return "package"
	case intent.Version != "" && intent.Version != expected.Version:
		return "version"
	case intent.Revision != "" && intent.Revision != expected.Revision:
		return "revision"
	case intent.Digest != "" && canonicalDigest(intent.Digest) != canonicalDigest(expected.Digest):
		return "digest"
	case intent.Source != "" && sanitizeLockReference(intent.Source) != sanitizeLockReference(expected.Source):
		return "source"
	case intent.Registry != "" && sanitizeLockReference(intent.Registry) != sanitizeLockReference(expected.Registry):
		return "registry"
	case intent.Scope != "" && intent.Scope != expected.Scope:
		return "scope"
	case intent.Environment != nil && !reflect.DeepEqual(intent.Environment, expected.Environment):
		return "environment"
	case intent.Architecture != "" && intent.Architecture != expected.Architecture:
		return "architecture"
	case intent.Platform != "" && intent.Platform != expected.Platform:
		return "platform"
	default:
		return ""
	}
}

func cloneLockProjection(in LockProjection) LockProjection {
	out := in
	if in.RequestedIntent != nil {
		intent := *in.RequestedIntent
		if intent.Channel != nil {
			channel := *intent.Channel
			intent.Channel = &channel
		}
		out.RequestedIntent = &intent
	}
	if in.Identity.Environment != nil {
		environment := *in.Identity.Environment
		out.Identity.Environment = &environment
	}
	out.Identity.Artifacts = append([]LockedArtifact(nil), in.Identity.Artifacts...)
	out.Identity.Sources = append([]LockedSource(nil), in.Identity.Sources...)
	for i := range out.Identity.Sources {
		if in.Identity.Sources[i].Trust != nil {
			trust := *in.Identity.Sources[i].Trust
			out.Identity.Sources[i].Trust = &trust
		}
	}
	return out
}

// VerifyResolvedPlansAgainstLock verifies an exact resolved plan set against a
// persisted immutable lock document. Tool membership is part of the contract:
// missing, extra, or duplicate plans are mismatches rather than opportunities
// to resolve new mutable identity implicitly.
func VerifyResolvedPlansAgainstLock(doc LockDocument, plans []ResolvedInstallPlan) error {
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("lock document: %w", err)
	}
	if len(plans) != len(doc.Entries) {
		return fmt.Errorf("%w: resolved tool set size changed", ErrLockMismatch)
	}
	byTool := make(map[string]LockProjection, len(doc.Entries))
	for _, entry := range doc.Entries {
		byTool[entry.Tool.Name] = entry
	}
	seen := make(map[string]struct{}, len(plans))
	for _, resolved := range plans {
		name := resolved.Tool.Name
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("%w: duplicate resolved tool %q", ErrLockMismatch, name)
		}
		seen[name] = struct{}{}
		expected, ok := byTool[name]
		if !ok {
			return fmt.Errorf("%w: resolved tool %q is not present in lock", ErrLockMismatch, name)
		}
		if err := VerifyResolvedPlanAgainstLock(expected, resolved); err != nil {
			return fmt.Errorf("tool %q: %w", name, err)
		}
	}
	return nil
}

func (d LockDocument) Validate() error {
	if err := formatversion.ValidateReadVersion(formatversion.Lock, d.Version); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(d.Entries))
	previous := ""
	for i, entry := range d.Entries {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if err := entry.RequireImmutable(); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		name := entry.Tool.Name
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate lock tool %q", name)
		}
		seen[name] = struct{}{}
		if previous != "" && strings.Compare(previous, name) >= 0 {
			return fmt.Errorf("lock entries are not in canonical tool order: %q before %q", previous, name)
		}
		previous = name
	}
	return nil
}

// PinnedPlanFor materializes the immutable identity recorded in the lock into
// a static plan intent. It performs no resolution or host I/O. Operational
// semantics (operations, prerequisites, hooks, preparation, ownership) remain
// those of the current intent; only reproducibility identity comes from the
// validated lock entry. Source secret references are preserved from the
// in-memory intent and are never loaded from the persisted lock.
func (d LockDocument) PinnedPlanFor(intent ResolvedInstallPlan) (ResolvedInstallPlan, error) {
	entry, err := d.EntryForPlan(intent)
	if err != nil {
		return ResolvedInstallPlan{}, err
	}
	out := cloneResolvedInstallPlan(intent)
	out.Identity.Package = entry.Identity.Package
	out.Identity.Version = entry.Identity.Version
	out.Identity.Revision = entry.Identity.Revision
	out.Identity.Digest = entry.Identity.Digest
	out.Identity.Source = entry.Identity.Source
	out.Identity.Registry = entry.Identity.Registry
	out.Identity.Scope = entry.Identity.Scope
	out.Identity.Architecture = entry.Identity.Architecture
	out.Identity.Platform = entry.Identity.Platform
	if entry.Identity.Environment != nil {
		environment := *entry.Identity.Environment
		out.Identity.Environment = &environment
	} else {
		out.Identity.Environment = nil
	}

	out.Artifacts = make([]Artifact, len(entry.Identity.Artifacts))
	for i, artifact := range entry.Identity.Artifacts {
		out.Artifacts[i] = Artifact{
			Kind:               artifact.Kind,
			URL:                artifact.URL,
			LocalPath:          artifact.LocalPath,
			Checksum:           artifact.Checksum,
			ChecksumURL:        artifact.ChecksumURL,
			ChecksumFileFormat: artifact.ChecksumFileFormat,
			SignatureURL:       artifact.SignatureURL,
			SigningKey:         artifact.SigningKey,
		}
	}
	out.Sources = make([]SourceReference, len(entry.Identity.Sources))
	for i, source := range entry.Identity.Sources {
		out.Sources[i] = SourceReference{Role: source.Role, Kind: source.Kind, Name: source.Name, URL: source.URL, Owned: source.Owned}
		if source.Trust != nil {
			trust := *source.Trust
			out.Sources[i].Trust = &trust
		}
		out.Sources[i].SecretRef = secretRefForLockedSource(intent.Sources, source)
	}
	if err := out.Validate(); err != nil {
		return ResolvedInstallPlan{}, fmt.Errorf("pinned plan: %w", err)
	}
	if err := VerifyResolvedPlanAgainstLock(entry, out); err != nil {
		return ResolvedInstallPlan{}, fmt.Errorf("pinned plan: %w", err)
	}
	return out, nil
}

func secretRefForLockedSource(sources []SourceReference, locked LockedSource) *SecretReference {
	for _, source := range sources {
		if source.Role != locked.Role || !strings.EqualFold(source.Kind, locked.Kind) || !strings.EqualFold(source.Name, locked.Name) || sanitizeLockReference(source.URL) != sanitizeLockReference(locked.URL) || source.Owned != locked.Owned || !sourceTrustEqual(source.Trust, locked.Trust) {
			continue
		}
		if source.SecretRef == nil {
			return nil
		}
		secret := *source.SecretRef
		return &secret
	}
	return nil
}
