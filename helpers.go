package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
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
			for _, req := range t.Requires {
				queue = append(queue, req)
			}
			for _, method := range t.Methods {
				for _, req := range method.Requires {
					queue = append(queue, req)
				}
			}
		}
	}
	return filtered
}

// loadLockfile reads the lockfile for a given schema. Returns nil if no
// lockfile exists or it's corrupted (logs a warning).
// Returns an exit-coded error if --frozen-lockfile is set and no lock exists.
func loadLockfile(schemaPath string, s *config.Schema, frozen bool, lg *slog.Logger) (*lock.Lock, error) {
	lockPath := lock.DefaultPath(schemaPath)
	lk, err := lock.Load(lockPath)
	if err != nil {
		lg.Warn("lockfile corrupted, continuing without lock", "path", lockPath, "error", err)
	}
	if frozen && lk == nil {
		lg.Error("--frozen-lockfile requires lockfile — run 'depengine update' first", "path", lockPath)
		return nil, exitWithCode(2)
	}
	if lk != nil {
		lock.Apply(s, lk)
	}
	return lk, nil
}

// saveLockfile resolves version pins, merges with any existing lock, and persists.
func saveLockfile(ctx context.Context, s *config.Schema, lockPath string, oldLock *lock.Lock, lg *slog.Logger, diagnose bool) {
	newLock, err := lock.ResolveAll(ctx, s, run.OSExecRunner{})
	if err != nil {
		lg.Warn("resolve lock", "error", err)
		return
	}
	if newLock == nil {
		return
	}
	if oldLock != nil {
		for k, v := range oldLock.Tools {
			if _, exists := newLock.Tools[k]; !exists {
				newLock.Tools[k] = v
			}
		}
	}
	if err := lock.Save(lockPath, newLock); err != nil {
		lg.Warn("save lock", "error", err)
		return
	}
	if diagnose {
		lg.Debug("lock saved", "path", lockPath, "pinned", len(newLock.Tools))
	}
}

// hasLatestPlaceholders checks whether any method needs a latest release
// resolved. Used by install to decide whether auto-resolution is needed when
// no lockfile exists.
func hasLatestPlaceholders(s *config.Schema) bool {
	for _, tool := range s.Tools {
		for _, method := range tool.Methods {
			if _, hasRepo := method.Config["repo"]; method.Kind == "github" || hasRepo {
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
