package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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

// LockedArtifact is the reproducibility identity of one resolved artifact.
// Credentials are never part of this identity.
type LockedArtifact struct {
	Kind      ArtifactKind `json:"kind,omitempty"`
	URL       string       `json:"url,omitempty"`
	LocalPath string       `json:"local_path,omitempty"`
	Checksum  string       `json:"checksum,omitempty"`
}

// LockedSource is the persistence-safe identity of a source used during
// resolution. Secret references are deliberately omitted: authentication
// remains external to committed lockfiles.
type LockedSource struct {
	Role  SourceRole   `json:"role"`
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

// LockProjection is the versioned, adapter-neutral lock representation for one
// resolved installation candidate. An unavailable projection is diagnostic and
// must not be consumed as if it pinned the install.
type LockProjection struct {
	Version       int               `json:"lock_version"`
	Tool          ToolIdentity      `json:"tool"`
	Candidate     CandidateIdentity `json:"candidate"`
	RequestedMode VersionMode       `json:"requested_mode,omitempty"`
	Stability     LockStability     `json:"stability"`
	Reason        string            `json:"reason,omitempty"`
	Identity      LockIdentity      `json:"identity"`
}

// MarshalJSON is the persistence/debugging safety boundary for lock projections.
// It redacts credential-bearing strings even when a caller constructs a
// projection directly instead of using ProjectLock.
func (p LockProjection) MarshalJSON() ([]byte, error) {
	type plain LockProjection
	safe := p.redacted()
	return json.Marshal(plain(safe))
}

func (p LockProjection) redacted() LockProjection {
	p.Reason = run.RedactSensitiveText(p.Reason)
	p.Identity.Source = sanitizeLockReference(p.Identity.Source)
	p.Identity.Registry = sanitizeLockReference(p.Identity.Registry)
	for i := range p.Identity.Artifacts {
		p.Identity.Artifacts[i].URL = sanitizeLockReference(p.Identity.Artifacts[i].URL)
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
			Digest:       p.Identity.Digest,
			Source:       sanitizeLockReference(p.Identity.Source),
			Registry:     sanitizeLockReference(p.Identity.Registry),
			Scope:        p.Identity.Scope,
			Environment:  p.Identity.Environment,
			Architecture: p.Identity.Architecture,
			Platform:     p.Identity.Platform,
		},
	}
	if p.Identity.RequestedVersion != nil {
		projection.RequestedMode = p.Identity.RequestedVersion.Mode
	}
	for _, artifact := range p.Artifacts {
		projection.Identity.Artifacts = append(projection.Identity.Artifacts, LockedArtifact{
			Kind:      artifact.Kind,
			URL:       sanitizeLockReference(artifact.URL),
			LocalPath: artifact.LocalPath,
			Checksum:  run.RedactSensitiveText(artifact.Checksum),
		})
	}
	canonicalSources, err := CanonicalSources(p.Sources)
	if err != nil {
		return LockProjection{}, fmt.Errorf("project lock: %w", err)
	}
	for _, source := range canonicalSources {
		locked := LockedSource{
			Role:  source.Role,
			Name:  source.Name,
			URL:   sanitizeLockReference(source.URL),
			Owned: source.Owned,
		}
		if source.Trust != nil {
			trust := *source.Trust
			trust.KeyReference = sanitizeLockReference(trust.KeyReference)
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

// Validate checks lock schema and stability invariants without consulting an
// adapter or the host.
func (p LockProjection) Validate() error {
	if err := formatversion.ValidateReadVersion(formatversion.Lock, p.Version); err != nil {
		return err
	}
	if p.Tool.Name == "" {
		return errors.New("lock tool name is required")
	}
	if p.Candidate.Method == "" {
		return errors.New("lock candidate method is required")
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
	for i, artifact := range p.Identity.Artifacts {
		if err := artifact.Kind.Validate(); err != nil {
			return fmt.Errorf("lock artifact %d: %w", i, err)
		}
		if artifact.URL != "" && artifact.LocalPath != "" {
			return fmt.Errorf("lock artifact %d cannot specify both url and local_path", i)
		}
		if artifact.URL == "" && artifact.LocalPath == "" {
			return fmt.Errorf("lock artifact %d requires url or local_path", i)
		}
		if artifact.LocalPath != "" {
			clean, err := NormalizeProjectPath(artifact.LocalPath)
			if err != nil {
				return fmt.Errorf("lock artifact %d: %w", i, err)
			}
			if clean != artifact.LocalPath {
				return fmt.Errorf("lock artifact %d local path %q is not canonical; use %q", i, artifact.LocalPath, clean)
			}
		}
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
	return nil
}

func lockUnavailabilityReason(p ResolvedInstallPlan, identity LockIdentity) string {
	if p.Identity.RequestedVersion != nil {
		switch p.Identity.RequestedVersion.Mode {
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
			return fmt.Sprintf("unsupported requested version mode %q", p.Identity.RequestedVersion.Mode)
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

	if p.Identity.RequestedVersion == nil && identity.Version == "" && identity.Revision == "" && identity.Digest == "" && len(identity.Artifacts) == 0 {
		return "manager did not expose a stable resolved identity"
	}
	return ""
}

var sensitiveLockQueryKeys = map[string]struct{}{
	"token": {}, "access_token": {}, "auth_token": {}, "api_key": {}, "apikey": {},
	"password": {}, "passwd": {}, "secret": {}, "signature": {}, "sig": {}, "x-amz-signature": {},
}

// sanitizeLockReference removes credential material rather than replacing it
// with display placeholders. The resulting value can serve as canonical
// persisted identity while authentication remains external to the lockfile.
func sanitizeLockReference(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return run.RedactSensitiveText(raw)
	}
	u.User = nil
	query := u.Query()
	for key := range query {
		if _, sensitive := sensitiveLockQueryKeys[strings.ToLower(key)]; sensitive {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
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
func (d LockDocument) Validate() error {
	if d.Version != CurrentLockVersion {
		return fmt.Errorf("unsupported lock document version %d (want %d)", d.Version, CurrentLockVersion)
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
