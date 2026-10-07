package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/validate"
)

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

func exitWithCode(code int) error { return &ExitError{Code: code} }

// confirmationAccepted centralizes the non-interactive part of confirmation
// prompts so command workflows can test it without spawning the CLI binary.
func confirmationAccepted(input io.Reader) bool {
	line, _ := bufio.NewReader(input).ReadBytes('\n')
	answer := bytes.TrimSpace(line)
	return bytes.Equal(bytes.ToLower(answer), []byte("y")) ||
		bytes.Equal(bytes.ToLower(answer), []byte("yes"))
}

// schemaCandidateNames are the filenames auto-detected as a project schema,
// in priority order. Keep this in sync with docs/*.md mentions of
// auto-detection.
var schemaCandidateNames = []string{"schema.toml", "depengine.toml", "depends.toml"}

type schemaDiscovery struct {
	Selected string
	Found    []string
}

// discoverDefaultSchema is deliberately side-effect-free. Command
// construction calls it to choose a default value; presentation of ambiguity
// happens only when a schema-consuming command is actually invoked.
func discoverDefaultSchema() schemaDiscovery {
	var found []string
	for _, c := range schemaCandidateNames {
		if _, err := os.Stat(c); err == nil {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return schemaDiscovery{Selected: "schema.toml"}
	}
	return schemaDiscovery{Selected: found[0], Found: found}
}

func defaultSchemaPath() string {
	return discoverDefaultSchema().Selected
}

type loadedProject struct {
	Schema        *config.Schema
	Facts         *platform.Facts
	Clan          string
	SchemaPath    string
	ManifestPath  string
	ManifestCount int
	ManifestAuto  bool
}

type projectLoadOptions struct {
	ManifestPath string
	ManifestAuto bool
	Provenance   bool
}

// resolveSchemaFilePath resolves the exact schema file consumed by project
// commands. Directory inputs retain the historical <dir>/schema.toml rule,
// while every successful result is clean and absolute for state/lock identity.
func resolveSchemaFilePath(input string) (string, error) {
	if input == "" {
		return "", os.ErrNotExist
	}
	candidate := input
	info, err := os.Stat(candidate)
	if err == nil && info.IsDir() {
		candidate = filepath.Join(candidate, "schema.toml")
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err = os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("schema path %s is a directory", abs)
	}
	return abs, nil
}

// loadProject is the single fact-aware project loading pipeline. Project and
// manifest are expanded with the same host map and validation runs exactly
// once against the final merged schema.
func loadProject(schemaPath string, opts projectLoadOptions) (*loadedProject, error) {
	resolvedSchema, err := resolveSchemaFilePath(schemaPath)
	if err != nil {
		return nil, err
	}
	log.Default.Debug("loading schema", "path", resolvedSchema)

	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		return nil, err
	}
	clan := platform.ResolveFamily(facts)
	factMap := config.BuildMap(facts, clan)

	schema, err := config.ParseProjectSchema(resolvedSchema, factMap)
	if err != nil {
		return nil, err
	}

	count := 0
	if opts.ManifestPath != "" {
		manifest, err := config.ParseManifest(opts.ManifestPath, factMap)
		if err != nil {
			return nil, err
		}
		config.FilterManifestTools(schema, manifest)
		if err := config.ValidateManifestLayer(manifest); err != nil {
			return nil, err
		}
		if err := config.ValidateManifestNewTools(schema, manifest); err != nil {
			return nil, err
		}
		count = len(manifest.Tools)
		if count > 0 {
			if opts.Provenance {
				schema = config.MergeLayersWithProvenance(manifest, schema)
			} else {
				schema = config.MergeLayers(manifest, schema)
			}
		}
	}

	vr := validate.ValidateSchema(schema, exec.RegisteredKinds())
	if vr.HasErrors() {
		for _, validationErr := range vr.Errors {
			log.Default.Error(validationErr.Error())
		}
		return nil, &config.ParseSchemaError{Err: errors.New("schema validation failed")}
	}
	for _, warning := range vr.Warnings {
		log.Default.Warn(warning.Error())
	}

	return &loadedProject{
		Schema:        schema,
		Facts:         facts,
		Clan:          clan,
		SchemaPath:    resolvedSchema,
		ManifestPath:  opts.ManifestPath,
		ManifestCount: count,
		ManifestAuto:  opts.ManifestAuto,
	}, nil
}

// loadSchema preserves the older tuple API for call sites that only need a
// schema, while delegating to the canonical project loader.
func loadSchema(path string) (*config.Schema, string, *platform.Facts, error) {
	project, err := loadProject(path, projectLoadOptions{})
	if err != nil {
		return nil, "", nil, err
	}
	return project.Schema, project.Clan, project.Facts, nil
}

// mergeManifest remains for the syntax-only validate command. Host-aware
// commands use loadProject so project and manifest never receive different
// placeholder maps.
func mergeManifest(schema *config.Schema, path string, provenance bool) (*config.Schema, int, error) {
	manifest, err := config.ParseManifest(path, nil)
	if err != nil {
		return nil, 0, err
	}
	config.FilterManifestTools(schema, manifest)
	if err := config.ValidateManifestLayer(manifest); err != nil {
		return nil, 0, err
	}
	if err := config.ValidateManifestNewTools(schema, manifest); err != nil {
		return nil, 0, err
	}
	count := len(manifest.Tools)
	if count == 0 {
		return schema, 0, nil
	}
	if provenance {
		return config.MergeLayersWithProvenance(manifest, schema), count, nil
	}
	return config.MergeLayers(manifest, schema), count, nil
}

// newProjectExecutor wires the host-specific adapters and selection policy
// consistently for install-adjacent read-only operations.
func newProjectExecutor(schema *config.Schema, clan string, facts *platform.Facts, rn run.Runner) *exec.Executor {
	executor := exec.New()
	exec.WithRunner(rn)(executor)
	exec.WithFacts(facts)(executor)
	if schema != nil {
		exec.WithDefaultMethodOrder(schema.Defaults.MethodOrder)(executor)
		adapters := []exec.AdapterV2{exec.NewNativeAdapter(clan)}
		if helper := schema.Defaults.AurHelper; helper != "" {
			adapters = append(adapters, ecosystem.NewAURAdapter(helper))
		}
		exec.WithAdapters(adapters...)(executor)
	} else {
		exec.WithAdapters(exec.NewNativeAdapter(clan))(executor)
	}
	return executor
}

func exitCodeForError(err error) int {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	var schemaErr *config.ParseSchemaError
	if errors.As(err, &schemaErr) {
		return 2
	}
	return 3
}

// filterTools applies --only, --skip, and --profile filters to the tool map.
func filterTools(tools map[string]*config.Tool, only, skip, profile string) map[string]*config.Tool {
	skipSet := make(map[string]bool)
	for _, name := range strings.Split(skip, ",") {
		skipSet[strings.TrimSpace(name)] = true
	}
	roots := make(map[string]bool, len(tools))
	for name, tool := range tools {
		if skipSet[name] {
			continue
		}
		if tool.DependencyOnly && only != name {
			continue
		}
		if only != "" && name != only {
			continue
		}
		if profile != "" {
			matched := false
			for _, tag := range tool.Tags {
				if strings.EqualFold(tag, profile) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		roots[name] = true
	}
	filtered := make(map[string]*config.Tool, len(tools))
	queue := make([]string, 0, len(roots))
	for name := range roots {
		queue = append(queue, name)
	}
	visited := map[string]bool{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true
		if t, ok := tools[name]; ok {
			if only == name && t.DependencyOnly {
				clone := *t
				clone.DependencyOnly = false
				t = &clone
			}
			filtered[name] = t
			queue = append(queue, t.Requires...)
			for _, method := range t.Methods {
				queue = append(queue, method.Requires...)
			}
		}
	}
	return filtered
}

// loadLockfile reads, frozen-validates, and applies legacy v1 pins. V2 locks
// are decoded by universal-lock consumers and never mutate the schema through
// their compatibility ToolPin payload.
func loadLockfile(schemaPath string, s *config.Schema, frozen bool, lg *slog.Logger) (*lock.Lock, error) {
	lockPath := lock.DefaultPath(schemaPath)
	lk, err := lock.Load(lockPath)
	if err != nil {
		if frozen {
			lg.Error("--frozen-lockfile requires a readable lockfile", "path", lockPath, "error", err)
			return nil, exitWithCode(2)
		}
		lg.Warn("lockfile corrupted, continuing without lock", "path", lockPath, "error", err)
		lk = nil
	}
	if frozen && lk == nil {
		lg.Error("--frozen-lockfile requires lockfile — run 'depengine update' first", "path", lockPath)
		return nil, exitWithCode(2)
	}
	if frozen {
		if err := lock.ValidateFrozen(s, lk); err != nil {
			lg.Error("--frozen-lockfile requires an up-to-date supported lock", "error", err)
			return nil, exitWithCode(2)
		}
	}
	if lk != nil {
		if lk.Version == lock.CurrentVersion {
			if _, err := lk.ProjectionDocument(); err != nil {
				return nil, fmt.Errorf("load universal lock projection: %w", err)
			}
		} else {
			lock.ApplyLegacyV1(s, lk)
		}
	}
	return lk, nil
}

// saveResolvedLegacyInstallLock persists the identity resolved before execution.
func saveResolvedLegacyInstallLock(lockPath string, resolved *lock.Lock, lg *slog.Logger, diagnose bool) {
	if resolved == nil {
		return
	}
	if err := lock.Save(lockPath, resolved); err != nil {
		lg.Warn("save lock", "error", err)
		return
	}
	if diagnose {
		lg.Debug("lock saved", "path", lockPath, "pinned", len(resolved.Tools))
	}
}

// validateInstallPackageLockIdentity rejects stale mutable package identity before any remote
// resolution. This closes the failure path where resolving the new selector
// itself fails and install might otherwise continue without a concrete pin.
func validateInstallPackageLockIdentity(s *config.Schema, l *lock.Lock) error {
	if s == nil || l == nil {
		return nil
	}
	if l.Version == lock.CurrentVersion {
		return nil
	}
	for name, tool := range s.Tools {
		if tool == nil {
			continue
		}
		kindCount := make(map[string]int)
		for _, method := range tool.Methods {
			if method == nil {
				continue
			}
			idx := kindCount[method.Kind]
			kindCount[method.Kind] = idx + 1
			if method.Kind != "npm" && method.Kind != "pnpm" && method.Kind != "yarn" {
				continue
			}
			key := fmt.Sprintf("%s/%s/%d", name, method.Kind, idx)
			pin, ok := l.Tools[key]
			if !ok || pin.PackageSelector == "" {
				continue
			}
			if !lock.MatchesPackagePin(name, method, pin) {
				return fmt.Errorf("lock: package, registry, or version request changed for %q; run 'depengine update' to accept the change", key)
			}
		}
	}
	return nil
}

// mergeInstallLock preserves the existing lock's identity hashes while adding
// newly resolved pin fields. Only an explicit update may accept identity drift.
// A changed package identity is rejected before merge so install cannot
// silently execute an unpinned request while retaining the stale lock identity.
// A v2 universal projection is preserved verbatim: install consumes it but
// never regenerates it — update is the operation that accepts/recalculates
// identity. Partial/profile installs therefore keep projection entries for
// tools outside the current resolution scope.
func mergeInstallLock(oldLock, newLock *lock.Lock) (*lock.Lock, error) {
	if oldLock != nil && newLock != nil {
		for key, oldPin := range oldLock.Tools {
			if oldPin.PackageSelector == "" {
				continue
			}
			freshPin, ok := newLock.Tools[key]
			if !ok || freshPin.PackageSelector == "" || freshPin.PackageSelector == oldPin.PackageSelector {
				continue
			}
			return nil, fmt.Errorf("lock: package or registry changed for %q; run 'depengine update' to accept the change", key)
		}
	}
	newLock = lock.Merge(oldLock, newLock)
	if oldLock == nil {
		return newLock, nil
	}
	// Preserve the v2 universal projection verbatim. lock.Merge already does
	// this when fresh carries no projection — the only shape ResolveLegacyV1
	// produces today, so install always keeps the existing projection here.
	// A projection carried by fresh would win instead (as in lock.Merge);
	// install never regenerates one either way.
	if oldLock.Version == lock.CurrentVersion && oldLock.UniversalProjection != "" {
		if newLock.UniversalProjection == "" {
			newLock.UniversalProjection = oldLock.UniversalProjection
		}
		newLock.Version = lock.CurrentVersion
	}
	for name, oldHash := range oldLock.MethodsHash {
		newHash, exists := newLock.MethodsHash[name]
		if !exists || newHash != oldHash {
			newLock.MethodsHash[name] = oldHash
		}
	}
	if newLock.SourceHash == nil {
		newLock.SourceHash = make(map[string]string, len(oldLock.SourceHash))
	}
	for key, oldHash := range oldLock.SourceHash {
		newHash, exists := newLock.SourceHash[key]
		if !exists || newHash != oldHash {
			newLock.SourceHash[key] = oldHash
		}
	}
	for key := range newLock.SourceHash {
		if _, existed := oldLock.SourceHash[key]; existed {
			continue
		}
		if _, toolWasLocked := oldLock.MethodsHash[lockCandidateToolName(key)]; toolWasLocked {
			delete(newLock.SourceHash, key)
		}
	}
	return newLock, nil
}

func lockCandidateToolName(key string) string {
	last := strings.LastIndexByte(key, '/')
	if last <= 0 {
		return ""
	}
	previous := strings.LastIndexByte(key[:last], '/')
	if previous <= 0 {
		return ""
	}
	return key[:previous]
}

// hasLatestPlaceholders checks whether any validated method needs a latest
// release resolved. Repository-backed methods are identified by their resolved
// config shape rather than by method kind. Used by install to decide whether
// auto-resolution is needed when no lockfile exists.
// hasLockableMutableSelectors reports whether first install must create a lock
// for a mutable selector that legacy lock v1 can make immutable: direct Git or
// Cargo Git branch/tag selectors, container tags, and plain unversioned npm, pnpm, or Yarn Classic packages.
func hasLockableMutableSelectors(s *config.Schema) bool {
	for _, tool := range s.Tools {
		for _, method := range tool.Methods {
			if method == nil {
				continue
			}
			if method.Kind == "npm" || method.Kind == "pnpm" || method.Kind == "yarn" {
				if version, _ := method.Config["version"].(string); version == "" {
					pkg, _ := method.Config["pkg"].(string)
					if pkg == "" {
						pkg = tool.Name
					}
					if ecosystem.IsNPMRegistryPackage(pkg) {
						return true
					}
				}
				continue
			}
			if method.Kind == "container" {
				if digest, _ := method.Config["digest"].(string); digest == "" {
					return true
				}
				continue
			}
			if method.Kind != "git" && method.Kind != "cargo" {
				continue
			}
			if method.Kind == "cargo" {
				if source, _ := method.Config["git"].(string); source == "" {
					continue
				}
			}
			branch, _ := method.Config["branch"].(string)
			tag, _ := method.Config["tag"].(string)
			if branch != "" || tag != "" {
				return true
			}
		}
	}
	return false
}

func hasLatestPlaceholders(s *config.Schema) bool {
	for _, tool := range s.Tools {
		for _, method := range tool.Methods {
			if _, hasRepo := method.Config["repo"]; hasRepo {
				branch, _ := method.Config["branch"].(string)
				release, _ := method.Config["release"].(string)
				if branch == "" && (release == "" || release == "latest") {
					return true
				}
			}
			if url, ok := method.Config["url"].(string); ok && strings.Contains(url, "{latest}") {
				return true
			}
		}
	}
	return false
}
