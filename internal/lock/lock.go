// Package lock persists depengine.lock: the resolved values of the mutable
// references a schema pins.
//
// schema.toml declares intent ("install the latest release of tool X");
// depengine.lock records the concrete values resolved for that intent so
// later installs substitute them instead of re-resolving them. Lock coverage
// is method-specific; docs/support-boundary.md is the authoritative boundary.
// The lock pins:
//
//   - {latest} placeholders in URL templates (the bare version tag, not a
//     baked URL);
//   - repo-backed latest GitHub releases (repo present, release empty or
//     "latest", no branch), resolved through the GitHub releases API;
//   - checksums, including `:auto` checksums once a non-frozen install has
//     materialized them (update alone never downloads a payload to compute
//     one); and
//   - local artifact content digests;
//   - mutable direct-Git and cargo --git branch/tag selectors, resolved to a
//     concrete commit;
//   - mutable container tags, resolved to immutable registry digests;
//   - unversioned npm, pnpm, or Yarn Classic packages, resolved to the registry's latest concrete
//     package version; and
//   - candidate-scoped host package-source declarations (kind/name/url and any
//     declared Brew tap revision) as identity hashes. Source contents remain
//     unpinned unless a Brew revision is separately declared and preflighted.
//
// It does NOT pin native package versions, channels, or most ecosystem
// package versions. Direct Git, cargo --git, containers, and unversioned npm
// packages are the mutable-selector classes pinned inside legacy lock v1. On
// subsequent installs the lockfile is read and the pinned values are applied
// before adapters resolve plans: artifact pins may patch method config, while
// Git/container pins remain transient immutable state so requested selectors
// stay visible for drift reporting. Running `depengine update` re-resolves and merges into the existing
// lock, so pins the resolution skipped (e.g. a materialized `:auto` checksum)
// or did not cover (e.g. tools outside --profile) are kept.
//
// Pipeline:
//
//	schema.toml → ParseProjectSchema → resolve {latest} → patch schema → execute
//	                               ↓
//	                           depengine.lock
package lock

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/containerref"
	"github.com/Khorea1/depengine/internal/containerregistry"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/ghrelease"
	gitadapter "github.com/Khorea1/depengine/internal/git"
	"github.com/Khorea1/depengine/internal/localartifact"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/secret"

	"github.com/pelletier/go-toml/v2"
)

var (
	resolveLatestReleaseTag   = ghrelease.ResolveLatestReleaseTag
	resolveContainerTagDigest = containerregistry.ResolveTagDigest
)

// A v2 lock uses UniversalProjection as its operational resolved identity.
// Tools remains readable compatibility payload; v2 consumers must not let it
// override the universal projection. Version 1 uses Tools for legacy pins.
// V2 locks persist the canonical resolved identity in UniversalProjection.
type Lock struct {
	Version     int                `toml:"version"`
	Tools       map[string]ToolPin `toml:"tools"`
	MethodsHash map[string]string  `toml:"methods_hash,omitempty" json:"methods_hash,omitempty"`
	SourceHash  map[string]string  `toml:"source_hash,omitempty" json:"source_hash,omitempty"`
	// UniversalProjection is the adapter-neutral plan.LockDocument encoded as JSON.
	// It is present in v2 lockfiles; v1 remains readable for migration.
	UniversalProjection string `toml:"universal_projection,omitempty"`

	// clearGitRevision/clearContainerDigest are transient merge policy populated
	// by ResolveLegacyV1 when a previously lockable mutable selector has been removed.
	// They are never persisted.
	clearGitRevision     map[string]struct{} `toml:"-" json:"-"`
	clearContainerDigest map[string]struct{} `toml:"-" json:"-"`
	clearPackageVersion  map[string]struct{} `toml:"-" json:"-"`
}

// ToolPin captures resolved values for legacy lock v1 compatibility. If a v2
// universal projection is present, Tools is compatibility data and cannot
// override the resolved identity in operational paths. Keys in Lock.Tools use
// the form "<toolName>/<methodKind>/<idx>" so same-kind candidates remain distinct.
type ToolPin struct {
	Latest          string `toml:"latest,omitempty"`
	Checksum        string `toml:"checksum,omitempty"` // pinned concrete checksum (e.g. "sha256:abc123...")
	Revision        string `toml:"revision,omitempty"` // immutable Git commit for branch/tag selectors
	Selector        string `toml:"selector,omitempty"` // requested Git selector, e.g. "branch:main" or "tag:v1.2.3"
	ContainerTag    string `toml:"container_tag,omitempty"`
	ContainerDigest string `toml:"container_digest,omitempty"`
	PackageVersion  string `toml:"package_version,omitempty"`  // concrete npm-registry package version
	PackageSelector string `toml:"package_selector,omitempty"` // hash of requested package and registry
}

// DefaultPath returns the default lockfile path for a given schema file.
// Always produces a lockfile named "depengine.lock" in the same directory
// as the schema file — matches Cargo.lock and package-lock.json conventions.
//
//	schema.toml   → depengine.lock
//	depengine.toml → depengine.lock
//	depends.toml  → depengine.lock
func DefaultPath(schemaPath string) string {
	dir := filepath.Dir(schemaPath)
	return filepath.Join(dir, "depengine.lock")
}

// Load reads a lock file. A missing file is NOT an error — returns nil, nil.
func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- Lock paths are explicitly selected or derived next to the project schema.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("lock: read %s: %w", path, err)
	}
	var l Lock
	if err := toml.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("lock: parse %s: %w", path, err)
	}
	if l.Version != 1 && l.Version != CurrentVersion {
		return nil, fmt.Errorf("lock: unsupported version %d (supported: 1, %d)", l.Version, CurrentVersion)
	}
	if l.Version == 1 && l.UniversalProjection != "" {
		return nil, fmt.Errorf("lock: version 1 cannot contain a universal projection")
	}
	if l.Version == CurrentVersion {
		if _, err := l.ProjectionDocument(); err != nil {
			return nil, err
		}
	}
	for key, pin := range l.Tools {
		if !isCanonicalKey(key) {
			return nil, fmt.Errorf("lock: invalid tool key %q; expected <tool>/<method>/<index>", key)
		}
		if err := validateGitPin(pin); err != nil {
			return nil, fmt.Errorf("lock: invalid tool pin %q: %w", key, err)
		}
		if err := validateContainerPin(pin); err != nil {
			return nil, fmt.Errorf("lock: invalid tool pin %q: %w", key, err)
		}
		if err := validatePackagePin(pin); err != nil {
			return nil, fmt.Errorf("lock: invalid tool pin %q: %w", key, err)
		}
	}
	for key := range l.SourceHash {
		if !isCanonicalKey(key) {
			return nil, fmt.Errorf("lock: invalid source identity key %q; expected <tool>/<method>/<index>", key)
		}
	}
	return &l, nil
}

// Save writes l to path, creating parent directories as needed.
func Save(path string, l *Lock) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil { // #nosec G301 -- Project lockfile parent directories are intentionally traversable.
		return fmt.Errorf("lock: mkdir: %w", err)
	}

	// Write to a temp file in the same directory (ensures same-filesystem rename).
	var f *os.File
	var tmpPath string
	var err error
	for range 10 {
		var suffix [16]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return fmt.Errorf("lock: generate temp name: %w", err)
		}
		tmpPath = filepath.Join(dir, ".depengine-lock-"+hex.EncodeToString(suffix[:]))
		f, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666) // #nosec G304,G302 -- Random exclusive temp file; preserve project lockfile sharing under umask for non-secret metadata.
		if err == nil || !os.IsExist(err) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("lock: create tmp: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(l); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lock: encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lock: sync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lock: close tmp: %w", err)
	}

	// Atomic rename — the target is never left in a partially-written state.
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lock: rename: %w", err)
	}
	if runtime.GOOS != "windows" {
		d, err := os.Open(dir) // #nosec G304 -- Directory is derived from the selected project lockfile path.
		if err != nil {
			return fmt.Errorf("lock: open directory for sync: %w", err)
		}
		if err := d.Sync(); err != nil {
			_ = d.Close()
			return fmt.Errorf("lock: sync directory: %w", err)
		}
		if err := d.Close(); err != nil {
			return fmt.Errorf("lock: close directory: %w", err)
		}
	}

	return nil
}

// toolKey builds a stable key for Lock.Tools: "<toolName>/<methodKind>/<idx>".
// This is the canonical key shape that writers emit.
func toolKey(toolName, methodKind string, idx int) string {
	return fmt.Sprintf("%s/%s/%d", toolName, methodKind, idx)
}

// isCanonicalKey reports whether key has the canonical
// "<toolName>/<methodKind>/<idx>" shape, i.e. its last "/"-separated segment
// is the numeric method index.
func isCanonicalKey(key string) bool {
	i := strings.LastIndex(key, "/")
	if i < 0 || i == len(key)-1 {
		return false
	}
	if i == 0 || !strings.Contains(key[:i], "/") {
		return false
	}
	for _, digit := range key[i+1:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// computeMethodsHash returns a SHA-256 hash of the tool's method kinds and
// labels, in order. Used to detect reordering of same-kind methods between
// lock and apply.
//
// Label is included (not just Kind) because Apply keys pins by
// "<toolName>/<kind>/<idx-within-kind>": swapping the position of two
// methods that share a Kind changes which pin each one receives, but the
// kind sequence alone is unchanged by such a swap and would not move the
// hash. Label (the TOML section key, e.g. "http-musl") is the field that
// actually distinguishes same-kind mirrors, so hashing it too makes the
// swap detectable whenever the methods are labeled.
func computeMethodsHash(methods []*config.MethodCandidate) string {
	h := sha256.New()
	for _, m := range methods {
		h.Write([]byte(m.Kind))
		h.Write([]byte{0})
		h.Write([]byte(m.Label))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// computeSourceHash returns the requested host-source identity for one method
// candidate. Secret references are deliberately excluded: authentication is
// external to the lock contract, while kind/name/url and declared revision
// determine the host repository identity expected by the candidate.
func computeSourceHash(method *config.MethodCandidate) string {
	if method == nil || len(method.Sources) == 0 {
		return ""
	}
	h := sha256.New()
	for _, source := range method.Sources {
		h.Write([]byte(strings.ToLower(source.Kind)))
		h.Write([]byte{0})
		h.Write([]byte(strings.ToLower(source.Name)))
		h.Write([]byte{0})
		h.Write([]byte(source.URL))
		h.Write([]byte{0})
		if source.Revision != "" {
			h.Write([]byte("revision="))
			h.Write([]byte(source.Revision))
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SnapshotIntentMetadata derives requested method/source identity hashes without
// resolving mutable selectors or mutating the schema.
func SnapshotIntentMetadata(s *config.Schema) (map[string]string, map[string]string, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("lock: intent metadata requires schema")
	}
	methods := make(map[string]string)
	sources := make(map[string]string)
	for name, tool := range s.Tools {
		if tool == nil {
			return nil, nil, fmt.Errorf("lock: intent metadata requires valid tool %q", name)
		}
		if len(tool.Methods) == 0 {
			continue
		}
		for _, method := range tool.Methods {
			if method == nil {
				return nil, nil, fmt.Errorf("lock: intent metadata requires valid method for tool %q", name)
			}
		}
		methods[name] = computeMethodsHash(tool.Methods)
		kindCount := make(map[string]int)
		for _, method := range tool.Methods {
			if method == nil {
				return nil, nil, fmt.Errorf("lock: intent metadata requires valid method for tool %q", name)
			}
			idx := kindCount[method.Kind]
			kindCount[method.Kind] = idx + 1
			if hash := computeSourceHash(method); hash != "" {
				sources[toolKey(name, method.Kind, idx)] = hash
			}
		}
	}
	return methods, sources, nil
}

// ResolveLegacyV1 resolves every selector class supported by legacy lock v1 and
// returns their immutable pins plus method/source identity hashes. Empty lock
// (no tools needing resolution) is still valid.
// This compatibility resolver is for lock envelope v1 only. New v2 code must
// resolve through internal/exec AdapterV2.ResolvePlan.
func ResolveLegacyV1(ctx context.Context, s *config.Schema, rn run.Runner) (*Lock, error) {
	methodsHash, sourceHash, err := SnapshotIntentMetadata(s)
	if err != nil {
		return nil, err
	}
	l := &Lock{
		Version:              1,
		Tools:                make(map[string]ToolPin),
		MethodsHash:          methodsHash,
		SourceHash:           sourceHash,
		clearGitRevision:     make(map[string]struct{}),
		clearContainerDigest: make(map[string]struct{}),
		clearPackageVersion:  make(map[string]struct{}),
	}

	for name, tool := range s.Tools {
		kindCount := make(map[string]int)
		for _, method := range tool.Methods {
			idx := kindCount[method.Kind]
			kindCount[method.Kind] = idx + 1
			key := toolKey(name, method.Kind, idx)
			pin := ToolPin{}

			// Resolve {latest} in URL fields (git and http methods only).
			// Pin the bare version tag, not a fully-baked URL — see the
			// ToolPin.Latest doc comment for why.
			if urlRaw, ok := method.Config["url"].(string); ok && strings.Contains(urlRaw, "{latest}") {
				tag, err := ghrelease.ResolveLatestTag(ctx, urlRaw, rn)
				if err != nil {
					return nil, fmt.Errorf("lock: resolve %s/%s: %w", name, method.Kind, err)
				}
				pin.Latest = tag
			}
			if usesGitHubReleasePin(method) && githubUsesLatest(method.Config) {
				repo, _ := method.Config["repo"].(string)
				tag, err := resolveLatestReleaseTag(ctx, repo, rn)
				if err != nil {
					return nil, fmt.Errorf("lock: resolve %s/%s release: %w", name, method.Kind, err)
				}
				pin.Latest = tag
			}

			// Capture concrete checksum (prefer adapter-resolved hash over manual pin).
			if checksum, ok := method.Config["_checksum_resolved"].(string); ok && checksum != "" {
				pin.Checksum = checksum
			} else if checksum, ok := method.Config["checksum"].(string); ok && checksum != "" && !strings.HasSuffix(checksum, ":auto") {
				pin.Checksum = checksum
			}

			// Local artifacts are content-addressed during lock resolution even when
			// the schema omitted an explicit checksum. The absolute project root is
			// execution-local and never enters the lock; only the resulting digest
			// is persisted.
			if localPath, ok := method.Config["local_path"].(string); ok && localPath != "" {
				expected, _ := method.Config["checksum"].(string)
				resolved, err := localartifact.Resolve(method.ProjectRoot, localPath, expected)
				if err != nil {
					return nil, fmt.Errorf("lock: resolve %s/local artifact: %w", name, err)
				}
				pin.Checksum = resolved.Artifact.Checksum
			}

			if selector, mutable := gitMutableSelector(method); mutable {
				pin.Selector = selector
				if method.LockedRevision != "" {
					pin.Revision = method.LockedRevision
				} else {
					resolveCtx, err := gitLockResolutionContext(ctx, method)
					if err != nil {
						return nil, fmt.Errorf("lock: resolve %s/%s credential: %w", name, method.Kind, err)
					}
					revision, err := resolveMutableGitRevision(resolveCtx, rn, tool, method)
					if err != nil {
						return nil, fmt.Errorf("lock: resolve %s/%s selector: %w", name, method.Kind, err)
					}
					pin.Revision = revision
				}
			} else if method.Kind == "git" || method.Kind == "cargo" {
				l.clearGitRevision[key] = struct{}{}
			}

			if tag, mutable := containerMutableTag(method); mutable {
				pin.ContainerTag = tag
				if method.LockedDigest != "" {
					pin.ContainerDigest = strings.ToLower(method.LockedDigest)
				} else {
					credentials, err := containerLockCredentials(ctx, method)
					if err != nil {
						return nil, fmt.Errorf("lock: resolve %s/container credential: %w", name, err)
					}
					source, _ := method.Config["source"].(string)
					digest, err := resolveContainerTagDigest(ctx, source, tag, credentials)
					if err != nil {
						return nil, fmt.Errorf("lock: resolve %s/container tag: %w", name, err)
					}
					pin.ContainerDigest = digest
				}
			} else if method.Kind == "container" {
				l.clearContainerDigest[key] = struct{}{}
			}

			if selector, mutable := packageMutableSelector(name, method); mutable {
				pin.PackageSelector = selector
				if method.LockedVersion != "" {
					pin.PackageVersion = method.LockedVersion
				} else {
					pkg, _ := method.Config["pkg"].(string)
					if pkg == "" {
						pkg = name
					}
					registry, _ := method.Config["registry"].(string)
					var version string
					var err error
					switch method.Kind {
					case "pnpm":
						version, err = ecosystem.ResolveLatestPNPMVersion(ctx, rn, pkg)
					case "yarn":
						version, err = ecosystem.ResolveLatestYarnVersion(ctx, rn, pkg)
					default:
						version, err = ecosystem.ResolveLatestNPMVersion(ctx, rn, pkg, registry)
					}
					if err != nil {
						return nil, fmt.Errorf("lock: resolve %s/%s latest: %w", name, method.Kind, err)
					}
					pin.PackageVersion = version
				}
			} else if method.Kind == "npm" || method.Kind == "pnpm" || method.Kind == "yarn" {
				l.clearPackageVersion[key] = struct{}{}
			}

			if !toolPinEmpty(pin) {
				l.Tools[key] = pin
			}
		}
	}

	return l, nil
}

// Merge folds the pins of an existing lock (typically the lock already on
// disk) into fresh (typically a lock just produced by ResolveLegacyV1) and returns
// the result that callers save.
//
// Tool pins merge field by field. For a pin key present in both locks, a
// non-empty field in fresh wins and an empty field in fresh keeps the value
// from existing. This matters for composite pins: ResolveLegacyV1 may rediscover
// only one field of an existing identity (lock.ApplyLegacyV1 concretizes release and
// checksum selectors before execution, and `:auto` checksums are skipped
// until materialized), so replacing a pin wholesale would silently drop the
// half it did not re-resolve. Pin keys only present in existing are carried
// over wholesale — they cover pins ResolveLegacyV1 deliberately skips and tools
// the fresh resolution did not cover (e.g. filtered out by --profile). Pin
// keys only present in fresh are kept as resolved.
//
// Ownership and nil behavior: when fresh is non-nil, Merge mutates fresh's
// Tools map and returns fresh; existing is never modified, and its pin values
// are copied rather than aliased. When fresh is nil, existing is returned
// unchanged (nil when both are nil).
//
// MethodsHash and SourceHash are intentionally not merged — fresh's maps pass
// through untouched (or existing's, in the fresh-nil case above). Callers
// apply their own identity policy: a regular install must not bless changed
// method/source identity, while `depengine update` accepts freshly resolved
// identity. That policy belongs to each caller, not to the pin merge.
//
// Version and UniversalProjection are preserved: when the existing lock is v2
// and the fresh resolution carries no projection (ResolveLegacyV1 only produces
// legacy v1 pins; promotion to v2 happens via SetProjection in update), the
// merged result keeps the existing v2 version and projection verbatim.
// Install must not regenerate or drop the universal projection — update is
// the operation that accepts/recalculates identity. When fresh already
// carries a projection, it wins.
func Merge(existing, fresh *Lock) *Lock {
	if fresh == nil {
		return existing
	}
	if existing == nil {
		return fresh
	}
	if fresh.Tools == nil {
		fresh.Tools = make(map[string]ToolPin, len(existing.Tools))
	}
	for key, oldPin := range existing.Tools {
		newPin, ok := fresh.Tools[key]
		_, clearRevision := fresh.clearGitRevision[key]
		_, clearContainer := fresh.clearContainerDigest[key]
		_, clearPackage := fresh.clearPackageVersion[key]
		if !ok && !clearRevision && !clearContainer && !clearPackage {
			fresh.Tools[key] = oldPin
			continue
		}
		if newPin.Latest == "" {
			newPin.Latest = oldPin.Latest
		}
		if newPin.Checksum == "" {
			newPin.Checksum = oldPin.Checksum
		}
		if !clearRevision {
			if newPin.Revision == "" {
				newPin.Revision = oldPin.Revision
			}
			if newPin.Selector == "" {
				newPin.Selector = oldPin.Selector
			}
		}
		if !clearContainer {
			if newPin.ContainerTag == "" {
				newPin.ContainerTag = oldPin.ContainerTag
			}
			if newPin.ContainerDigest == "" {
				newPin.ContainerDigest = oldPin.ContainerDigest
			}
		}
		if !clearPackage {
			if newPin.PackageVersion == "" {
				newPin.PackageVersion = oldPin.PackageVersion
			}
			if newPin.PackageSelector == "" {
				newPin.PackageSelector = oldPin.PackageSelector
			}
		}
		if toolPinEmpty(newPin) {
			delete(fresh.Tools, key)
		} else {
			fresh.Tools[key] = newPin
		}
	}
	if existing.Version == CurrentVersion && existing.UniversalProjection != "" && fresh.UniversalProjection == "" {
		fresh.Version = CurrentVersion
		fresh.UniversalProjection = existing.UniversalProjection
	}
	return fresh
}

// ValidateFrozen dispatches frozen validation by lock envelope version.
// Version 1 retains legacy ToolPin requirements; version 2 trusts the universal
// projection for resolved identity and checks requested method/source hashes.
//
// V1 method hashes cover candidate kind/label ordering, not every requested
// field inside a candidate; selectors outside legacy lock coverage remain
// documented as unsupported.
func ValidateFrozen(s *config.Schema, l *Lock) error {
	if s == nil {
		return fmt.Errorf("lock: frozen validation requires schema")
	}
	if l == nil {
		return fmt.Errorf("lock: frozen validation requires lockfile")
	}
	if l.Version != 1 && l.Version != CurrentVersion {
		return fmt.Errorf("lock: unsupported version %d (supported: 1, %d)", l.Version, CurrentVersion)
	}
	if l.Version == 1 {
		return validateFrozenV1(s, l)
	}
	return validateFrozenV2(s, l)
}

func validateFrozenV1(s *config.Schema, l *Lock) error {
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tool := s.Tools[name]
		if tool == nil {
			return fmt.Errorf("lock: frozen validation requires a valid tool %q", name)
		}
		for _, method := range tool.Methods {
			if method == nil {
				return fmt.Errorf("lock: frozen validation requires a valid method for tool %q", name)
			}
		}

		if len(tool.Methods) > 0 {
			stored, ok := l.MethodsHash[name]
			if !ok || stored == "" {
				return fmt.Errorf("lock: frozen lock needs update: missing method identity for tool %q", name)
			}
			if current := computeMethodsHash(tool.Methods); stored != current {
				return fmt.Errorf("lock: frozen lock needs update: methods changed for tool %q", name)
			}
		}

		kindCount := make(map[string]int)
		for _, method := range tool.Methods {
			idx := kindCount[method.Kind]
			kindCount[method.Kind] = idx + 1
			key := toolKey(name, method.Kind, idx)
			currentSourceHash := computeSourceHash(method)
			storedSourceHash, hasStoredSourceHash := l.SourceHash[key]
			if currentSourceHash != "" {
				if !hasStoredSourceHash || storedSourceHash == "" {
					return fmt.Errorf("lock: frozen lock needs update: missing package-source identity for %q", key)
				}
				if storedSourceHash != currentSourceHash {
					return fmt.Errorf("lock: frozen lock needs update: package sources changed for %q", key)
				}
			} else if hasStoredSourceHash && storedSourceHash != "" {
				return fmt.Errorf("lock: frozen lock needs update: package sources changed for %q", key)
			}
			pin := l.Tools[key]

			if requiresLatestPin(method) && pin.Latest == "" {
				return fmt.Errorf("lock: frozen lock needs update: missing resolved release pin for %q", key)
			}
			if requiresChecksumPin(method) && pin.Checksum == "" {
				checksum, _ := method.Config["checksum"].(string)
				if strings.HasSuffix(checksum, ":auto") {
					return fmt.Errorf("lock: frozen lock needs update: missing resolved checksum pin for %q (materialize the auto checksum with a non-frozen install first)", key)
				}
				return fmt.Errorf("lock: frozen lock needs update: missing resolved checksum pin for %q", key)
			}
			if selector, mutable := gitMutableSelector(method); mutable {
				if pin.Revision == "" || pin.Selector == "" {
					return fmt.Errorf("lock: frozen lock needs update: missing resolved Git revision pin for %q", key)
				}
				if err := validateGitPin(pin); err != nil {
					return fmt.Errorf("lock: frozen lock needs update: invalid Git revision pin for %q: %w", key, err)
				}
				if pin.Selector != selector {
					return fmt.Errorf("lock: frozen lock needs update: Git selector changed for %q", key)
				}
			}
			if tag, mutable := containerMutableTag(method); mutable {
				if pin.ContainerTag == "" || pin.ContainerDigest == "" {
					return fmt.Errorf("lock: frozen lock needs update: missing resolved container digest pin for %q", key)
				}
				if err := validateContainerPin(pin); err != nil {
					return fmt.Errorf("lock: frozen lock needs update: invalid container digest pin for %q: %w", key, err)
				}
				if pin.ContainerTag != tag {
					return fmt.Errorf("lock: frozen lock needs update: container tag changed for %q", key)
				}
			}
			if selector, mutable := packageMutableSelector(name, method); mutable {
				if pin.PackageVersion == "" || pin.PackageSelector == "" {
					return fmt.Errorf("lock: frozen lock needs update: missing resolved package version for %q", key)
				}
				if err := validatePackagePin(pin); err != nil {
					return fmt.Errorf("lock: frozen lock needs update: invalid package pin for %q: %w", key, err)
				}
				if pin.PackageSelector != selector {
					return fmt.Errorf("lock: frozen lock needs update: package or registry changed for %q", key)
				}
			} else if (method.Kind == "npm" || method.Kind == "pnpm" || method.Kind == "yarn") && (pin.PackageVersion != "" || pin.PackageSelector != "") {
				return fmt.Errorf("lock: frozen lock needs update: package version request changed for %q", key)
			}
		}
	}
	return nil
}

func validateFrozenV2(s *config.Schema, l *Lock) error {
	doc, err := l.ProjectionDocument()
	if err != nil {
		return err
	}
	currentMethods, currentSources, err := SnapshotIntentMetadata(s)
	if err != nil {
		return err
	}

	projected := make(map[string]struct{}, len(doc.Entries))
	for _, entry := range doc.Entries {
		projected[entry.Tool.Name] = struct{}{}
	}
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tool := s.Tools[name]
		if len(tool.Methods) == 0 {
			continue
		}
		storedMethodHash, ok := l.MethodsHash[name]
		if !ok || storedMethodHash == "" {
			return fmt.Errorf("lock: frozen lock needs update: missing method identity for tool %q", name)
		}
		if storedMethodHash != currentMethods[name] {
			return fmt.Errorf("lock: frozen lock needs update: methods changed for tool %q", name)
		}
		if _, ok := projected[name]; !ok {
			return fmt.Errorf("lock: frozen lock needs update: tool %q is not present in universal projection", name)
		}

		kindCount := make(map[string]int)
		for _, method := range tool.Methods {
			idx := kindCount[method.Kind]
			kindCount[method.Kind] = idx + 1
			key := toolKey(name, method.Kind, idx)
			currentSourceHash := currentSources[key]
			storedSourceHash, hasStoredSourceHash := l.SourceHash[key]
			if currentSourceHash != "" {
				if !hasStoredSourceHash || storedSourceHash == "" {
					return fmt.Errorf("lock: frozen lock needs update: missing package-source identity for %q", key)
				}
				if storedSourceHash != currentSourceHash {
					return fmt.Errorf("lock: frozen lock needs update: package sources changed for %q", key)
				}
			} else if hasStoredSourceHash && storedSourceHash != "" {
				return fmt.Errorf("lock: frozen lock needs update: package sources changed for %q", key)
			}
		}
	}
	return nil
}

func requiresLatestPin(method *config.MethodCandidate) bool {
	if method == nil {
		return false
	}
	if raw, ok := method.Config["url"].(string); ok && strings.Contains(raw, "{latest}") {
		return true
	}
	return usesGitHubReleasePin(method) && githubUsesLatest(method.Config)
}

func requiresChecksumPin(method *config.MethodCandidate) bool {
	if method == nil {
		return false
	}
	checksum, _ := method.Config["checksum"].(string)
	if strings.HasSuffix(checksum, ":auto") {
		return true
	}
	localPath, _ := method.Config["local_path"].(string)
	return localPath != "" && checksum == ""
}

// ApplyLegacyV1 projects lock-envelope v1 pins onto the parsed schema before
// planning. It is a compatibility operation only; non-v1 envelopes are left
// unchanged. Artifact pins patch Config, while Git revisions and container
// digests stay transient so mutable intent remains visible for drift reporting.
func ApplyLegacyV1(s *config.Schema, l *Lock) {
	if l == nil {
		return
	}
	if l.Version != 1 {
		return
	}
	for name, tool := range s.Tools {
		kindCount := make(map[string]int)
		// Warn if method ordering has changed since lock was created.
		if l.MethodsHash != nil {
			currentHash := computeMethodsHash(tool.Methods)
			if storedHash, ok := l.MethodsHash[name]; ok && storedHash != currentHash {
				log.Default.Warn("method ordering changed since lock was created",
					"tool", name,
					"action", "run 'depengine update' to refresh pins")
			}
		}

		for _, method := range tool.Methods {
			idx := kindCount[method.Kind]
			kindCount[method.Kind] = idx + 1
			key := toolKey(name, method.Kind, idx)
			if l.SourceHash != nil {
				if storedHash, ok := l.SourceHash[key]; ok {
					if currentHash := computeSourceHash(method); currentHash != storedHash {
						log.Default.Warn("package sources changed since lock was created",
							"tool", name,
							"method", method.Kind,
							"action", "run 'depengine update' to accept the source change")
					}
				}
			}
			pin, ok := l.Tools[key]
			if !ok {
				continue
			}

			if selector, mutable := gitMutableSelector(method); mutable && pin.Revision != "" && pin.Selector != "" {
				if err := validateGitPin(pin); err == nil && pin.Selector == selector {
					method.LockedRevision = pin.Revision
				} else if pin.Selector != selector {
					log.Default.Warn("Git selector changed since lock was created",
						"tool", name, "method", method.Kind,
						"action", "run 'depengine update' to refresh the revision pin")
				}
			}

			if tag, mutable := containerMutableTag(method); mutable && pin.ContainerTag != "" && pin.ContainerDigest != "" {
				if err := validateContainerPin(pin); err == nil && pin.ContainerTag == tag {
					method.LockedDigest = strings.ToLower(pin.ContainerDigest)
				} else if pin.ContainerTag != tag {
					log.Default.Warn("container tag changed since lock was created",
						"tool", name, "method", method.Kind,
						"action", "run 'depengine update' to refresh the digest pin")
				}
			}

			if selector, mutable := packageMutableSelector(name, method); mutable && pin.PackageVersion != "" && pin.PackageSelector != "" {
				if err := validatePackagePin(pin); err == nil && pin.PackageSelector == selector {
					method.LockedVersion = pin.PackageVersion
				} else if pin.PackageSelector != selector {
					log.Default.Warn("package or registry changed since lock was created",
						"tool", name, "method", method.Kind,
						"action", "run 'depengine update' to refresh the package version pin")
				}
			}

			// Substitute {latest} in the current URL template with the
			// pinned version tag.
			if pin.Latest != "" {
				// Preserve the concrete version as internal resolved metadata so
				// dry-run/reporting can expose the pin even after {latest} has been
				// substituted out of the URL template.
				method.Config["_resolved_version"] = pin.Latest
				if usesGitHubReleasePin(method) && githubUsesLatest(method.Config) {
					method.Config["release"] = pin.Latest
				}
				if urlRaw, ok := method.Config["url"].(string); ok && strings.Contains(urlRaw, "{latest}") {
					method.Config["url"] = strings.ReplaceAll(urlRaw, "{latest}", pin.Latest)
				}
			}

			// Apply pinned checksum. Remote :auto values are replaced as before;
			// an unpinned local declaration receives the lock's content digest so
			// later install/check resolution verifies exactly the locked bytes.
			if pin.Checksum != "" {
				if v, ok := method.Config["checksum"].(string); ok && strings.HasSuffix(v, ":auto") {
					method.Config["checksum"] = pin.Checksum
				} else if _, local := method.Config["local_path"]; local {
					if v, _ := method.Config["checksum"].(string); v == "" {
						method.Config["checksum"] = pin.Checksum
					}
				}
			}
		}
	}
}

func containerMutableTag(method *config.MethodCandidate) (string, bool) {
	if method == nil || method.Kind != "container" {
		return "", false
	}
	if digest, _ := method.Config["digest"].(string); digest != "" {
		return "", false
	}
	tag, _ := method.Config["tag"].(string)
	if tag == "" {
		tag = "latest"
	}
	return tag, true
}

func packageMutableSelector(toolName string, method *config.MethodCandidate) (string, bool) {
	if method == nil || (method.Kind != "npm" && method.Kind != "pnpm" && method.Kind != "yarn") {
		return "", false
	}
	if version, _ := method.Config["version"].(string); version != "" {
		return "", false
	}
	pkg, _ := method.Config["pkg"].(string)
	if pkg == "" {
		pkg = toolName
	}
	if !ecosystem.IsNPMRegistryPackage(pkg) {
		return "", false
	}
	registry, _ := method.Config["registry"].(string)
	hash := sha256.Sum256([]byte(pkg + "\x00" + registry))
	return hex.EncodeToString(hash[:]), true
}

// MatchesPackagePin checks the requested npm-registry package and registry against a
// persisted pin before callers use that pin for status or upgrade decisions.
func MatchesPackagePin(toolName string, method *config.MethodCandidate, pin ToolPin) bool {
	selector, ok := packageMutableSelector(toolName, method)
	return ok && pin.PackageVersion != "" && pin.PackageSelector == selector
}

func containerLockCredentials(ctx context.Context, method *config.MethodCandidate) (*containerregistry.Credentials, error) {
	if method == nil || method.SecretRef == nil {
		return nil, nil
	}
	username, _ := method.Config["auth_username"].(string)
	if username == "" || username != strings.TrimSpace(username) || strings.ContainsRune(username, ':') || strings.IndexFunc(username, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("container secret_ref requires a valid auth_username without colons or control characters")
	}
	ref := plan.SecretReference{Provider: method.SecretRef.Provider, Name: method.SecretRef.Name}
	credential, err := (secret.EnvResolver{}).Resolve(ctx, ref)
	if err != nil || credential == "" {
		return nil, fmt.Errorf("declared registry credential is unavailable")
	}
	return &containerregistry.Credentials{Username: username, Secret: credential}, nil
}

func gitMutableSelector(method *config.MethodCandidate) (string, bool) {
	if method == nil || (method.Kind != "git" && method.Kind != "cargo") {
		return "", false
	}
	if method.Kind == "cargo" {
		source, _ := method.Config["git"].(string)
		if source == "" {
			return "", false
		}
	}
	if branch, _ := method.Config["branch"].(string); branch != "" {
		return "branch:" + branch, true
	}
	if tag, _ := method.Config["tag"].(string); tag != "" {
		return "tag:" + tag, true
	}
	return "", false
}

func resolveMutableGitRevision(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate) (string, error) {
	if method.Kind == "git" {
		return gitadapter.ResolveMutableRevision(ctx, rn, tool, method)
	}
	if method.Kind != "cargo" {
		return "", fmt.Errorf("method %q is not Git-backed", method.Kind)
	}
	source, _ := method.Config["git"].(string)
	if source == "" {
		return "", fmt.Errorf("cargo git source is required")
	}
	gitMethod := &config.MethodCandidate{
		Kind:           "git",
		LockedRevision: method.LockedRevision,
		SecretRef:      method.SecretRef,
		Config:         map[string]any{"url": source},
	}
	if branch, _ := method.Config["branch"].(string); branch != "" {
		gitMethod.Config["branch"] = branch
	}
	if tag, _ := method.Config["tag"].(string); tag != "" {
		gitMethod.Config["tag"] = tag
	}
	return gitadapter.ResolveMutableRevision(ctx, rn, tool, gitMethod)
}

func gitLockResolutionContext(ctx context.Context, method *config.MethodCandidate) (context.Context, error) {
	if method == nil || method.SecretRef == nil {
		return ctx, nil
	}
	ref := plan.SecretReference{Provider: method.SecretRef.Provider, Name: method.SecretRef.Name}
	credential, err := (secret.EnvResolver{}).Resolve(ctx, ref)
	if err != nil || credential == "" {
		return nil, fmt.Errorf("declared git credential is unavailable")
	}
	if ref.Provider == "env" && ref.Name != "" {
		ctx = run.WithOmittedEnv(ctx, ref.Name)
	}
	return exec.WithGitCredential(ctx, credential), nil
}

func validateContainerPin(pin ToolPin) error {
	if pin.ContainerTag == "" && pin.ContainerDigest == "" {
		return nil
	}
	if pin.ContainerTag == "" || pin.ContainerDigest == "" {
		return fmt.Errorf("container tag and digest must be present together")
	}
	if err := containerref.ValidateTag(pin.ContainerTag); err != nil {
		return fmt.Errorf("invalid container tag: %w", err)
	}
	if err := containerref.ValidateDigest(pin.ContainerDigest); err != nil {
		return fmt.Errorf("invalid container digest: %w", err)
	}
	if pin.Revision != "" || pin.Selector != "" {
		return fmt.Errorf("container and Git selector pins cannot be combined")
	}
	return nil
}

func validatePackagePin(pin ToolPin) error {
	if pin.PackageVersion == "" && pin.PackageSelector == "" {
		return nil
	}
	if !ecosystem.ValidNPMVersion(pin.PackageVersion) {
		return fmt.Errorf("package version must be concrete")
	}
	if len(pin.PackageSelector) != 64 {
		return fmt.Errorf("package selector must be a SHA-256 hash")
	}
	if _, err := hex.DecodeString(pin.PackageSelector); err != nil {
		return fmt.Errorf("package selector must be hexadecimal")
	}
	return nil
}

func validateGitPin(pin ToolPin) error {
	if pin.Revision == "" && pin.Selector == "" {
		return nil
	}
	if pin.Revision == "" || pin.Selector == "" {
		return fmt.Errorf("git revision and selector must be present together")
	}
	if !strings.HasPrefix(pin.Selector, "branch:") && !strings.HasPrefix(pin.Selector, "tag:") {
		return fmt.Errorf("unsupported git selector %q", pin.Selector)
	}
	_, value, _ := strings.Cut(pin.Selector, ":")
	if value == "" || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("git selector must be non-empty and contain no NUL")
	}
	if len(pin.Revision) != 40 && len(pin.Revision) != 64 {
		return fmt.Errorf("git revision must be a 40- or 64-hex commit")
	}
	if _, err := hex.DecodeString(pin.Revision); err != nil {
		return fmt.Errorf("git revision must be hexadecimal")
	}
	return nil
}

func toolPinEmpty(pin ToolPin) bool {
	return pin.Latest == "" && pin.Checksum == "" && pin.Revision == "" && pin.Selector == "" && pin.ContainerTag == "" && pin.ContainerDigest == "" && pin.PackageVersion == "" && pin.PackageSelector == ""
}

func githubUsesLatest(cfg map[string]any) bool {
	if branch, _ := cfg["branch"].(string); branch != "" {
		return false
	}
	release, _ := cfg["release"].(string)
	return release == "" || release == "latest"
}

// usesGitHubReleasePin reports whether a method resolves its release through
// the GitHub releases API. Every method carrying a repo reference does: the
// github method requires repo, and url-less http/msi/appimage/android methods
// address their asset the same way. Dispatching on the repo reference instead
// of the method kind keeps release resolution open to every current and
// future repo-backed method without a kind switch.
func usesGitHubReleasePin(method *config.MethodCandidate) bool {
	if method == nil {
		return false
	}
	_, hasRepo := method.Config["repo"]
	return hasRepo
}
