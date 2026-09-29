package app

import (
	"bufio"
	"bytes"
	"context"
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

// defaultSchemaPath returns the default schema file path, trying common names
// in schemaCandidateNames order. If none exist, returns "schema.toml" so the
// caller gets the original "file not found" error instead of a confusing one.
//
// If MORE THAN ONE candidate exists simultaneously, this is almost always a
// mistake (e.g. a leftover file from migrating between naming conventions,
// or a merge that landed two of them side by side) rather than intentional —
// silently picking the first one by priority means the user can edit the
// "wrong" file and see their changes never take effect, with no indication
// why. So instead of guessing quietly, we print a loud, explicit warning to
// stderr naming every candidate found and which one was selected, so the
// ambiguity is visible instead of silent. This only fires for the *default*
// (auto-detected) path — passing --schema explicitly bypasses this function
// entirely and is never second-guessed.
func defaultSchemaPath() string {
	var found []string
	for _, c := range schemaCandidateNames {
		if _, err := os.Stat(c); err == nil {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return "schema.toml"
	}
	if len(found) > 1 {
		fmt.Fprintf(os.Stderr,
			"warning: multiple schema files found (%s) — using %q. "+
				"This is ambiguous: pass --schema explicitly to silence this warning, "+
				"or remove the file(s) you don't intend to use.\n",
			strings.Join(found, ", "), found[0])
	}
	return found[0]
}

// loadSchema reads and validates a schema.toml from path, gathering OS facts.
// Returns the parsed Schema, clan name, Facts, or an error for exitCodeForError.
func loadSchema(path string) (*config.Schema, string, *engine.Facts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", nil, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "schema.toml")
	}
	log.Default.Debug("loading schema", "path", path)

	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		return nil, "", nil, err
	}
	clan := engine.ResolveFamily(facts)
	s, err := config.ParseProjectSchema(path, config.BuildMap(facts, clan))
	if err != nil {
		return nil, "", nil, err
	}
	vr := validate.ValidateSchema(s, exec.RegisteredKinds())
	if vr.HasErrors() {
		for _, e := range vr.Errors {
			log.Default.Error(e.Error())
		}
		return nil, "", nil, &config.ParseSchemaError{Err: errors.New("schema validation failed")}
	}
	for _, w := range vr.Warnings {
		log.Default.Warn(w.Error())
	}
	return s, clan, facts, nil
}

// loadSchemaWithManifest reads and resolves a schema, merging methods from
// the personal manifest at manifestPath. If manifestPath is empty, calls
// loadSchema directly. Returns the resolved schema, clan, facts, number of
// manifest tools that contributed (0 when no manifest), and any error.
// On manifest parse errors the function returns the error (caller decides exit).
func loadSchemaWithManifest(schemaPath, manifestPath string) (*config.Schema, string, *engine.Facts, int, error) {
	s, clan, facts, err := loadSchema(schemaPath)
	if err != nil {
		return nil, "", nil, 0, err
	}
	if manifestPath == "" {
		return s, clan, facts, 0, nil
	}

	merged, count, err := mergeManifest(s, manifestPath, true)
	if err != nil {
		return nil, "", nil, 0, err
	}
	if count > 0 {
		s = merged
		vr := validate.ValidateSchema(merged, exec.RegisteredKinds())
		if vr.HasErrors() {
			for _, e := range vr.Errors {
				log.Default.Error(e.Error())
			}
			return nil, "", nil, 0, &config.ParseSchemaError{Err: errors.New("schema validation failed after manifest merge")}
		}
	}
	return s, clan, facts, count, nil
}

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

// loadLockfile reads, frozen-validates, then applies the lockfile for a schema.
// Non-frozen installs tolerate a corrupt lock and continue without it. Frozen
// installs fail closed for a missing, unreadable, detectably stale, or
// incomplete lock.
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
		lock.Apply(s, lk)
	}
	return lk, nil
}

// saveLockfile resolves version pins, merges with any existing lock, and persists.
func saveLockfile(ctx context.Context, s *config.Schema, lockPath string, oldLock *lock.Lock, lg *slog.Logger, diagnose bool, rn run.Runner) {
	newLock, err := lock.ResolveAll(ctx, s, rn)
	if err != nil {
		lg.Warn("resolve lock", "error", err)
		return
	}
	if newLock == nil {
		return
	}
	merged, err := mergeInstallLock(oldLock, newLock)
	if err != nil {
		lg.Warn("refusing to rewrite lock identity during install", "error", err, "hint", "run 'depengine update' to accept the change")
		return
	}
	newLock = merged
	if err := lock.Save(lockPath, newLock); err != nil {
		lg.Warn("save lock", "error", err)
		return
	}
	if diagnose {
		lg.Debug("lock saved", "path", lockPath, "pinned", len(newLock.Tools))
	}
}

// validateInstallPackageLockIdentity rejects stale mutable package identity before any remote
// resolution. This closes the failure path where resolving the new selector
// itself fails and install might otherwise continue without a concrete pin.
func validateInstallPackageLockIdentity(s *config.Schema, l *lock.Lock) error {
	if s == nil || l == nil {
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
			if method.Kind != "npm" && method.Kind != "pnpm" {
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
// Cargo Git branch/tag selectors, container tags, and plain unversioned npm or pnpm packages.
func hasLockableMutableSelectors(s *config.Schema) bool {
	for _, tool := range s.Tools {
		for _, method := range tool.Methods {
			if method == nil {
				continue
			}
			if method.Kind == "npm" || method.Kind == "pnpm" {
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
