package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"depengine/pkg/engine"
	"depengine/pkg/exec"
	"depengine/pkg/lock"
	"depengine/pkg/log"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/validate"
)

// defaultSchemaPath returns the default schema file path, trying common names.
// If none exist, returns "schema.toml" so the caller gets the original error.
func defaultSchemaPath() string {
	candidates := []string{"schema.toml", "depengine.toml", "depends.toml"}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "schema.toml"
}

// loadSchema reads and validates a schema.toml from path, gathering OS facts.
// Returns the parsed Schema, clan name, Facts, or an error for exitCodeForError.
func loadSchema(path string) (*schema.Schema, string, *engine.Facts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", nil, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "schema.toml")
	}

	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		return nil, "", nil, err
	}
	clan := engine.ResolveFamily(facts)
	s, err := schema.ParseSchema(path, schema.BuildMap(facts, clan))
	if err != nil {
		return nil, "", nil, err
	}
	vr := validate.ValidateSchema(s, exec.RegisteredKinds())
	if vr.HasErrors() {
		for _, e := range vr.Errors {
			log.Default.Error(e.Error())
		}
		return nil, "", nil, &schema.ParseSchemaError{Err: errors.New("schema validation failed")}
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
func loadSchemaWithManifest(schemaPath, manifestPath string) (*schema.Schema, string, *engine.Facts, int, error) {
	s, clan, facts, err := loadSchema(schemaPath)
	if err != nil {
		return nil, "", nil, 0, err
	}
	if manifestPath == "" {
		return s, clan, facts, 0, nil
	}

	manifestTools, merr := schema.ParseManifest(manifestPath)
	if merr != nil {
		return nil, "", nil, 0, merr
	}

	count := 0
	if manifestTools != nil {
		var resolved *schema.Schema
		resolved, count = schema.ResolveSchema(s, manifestTools)
		s = resolved
	}
	return s, clan, facts, count, nil
}




func exitCodeForError(err error) int {
	var schemaErr *schema.ParseSchemaError
	if errors.As(err, &schemaErr) {
		return 2
	}
	return 3
}

// filteredByTags applies profile filtering: if profile is non-empty,
// only include tools that have no tags (universal) OR have the
// specified profile tag in their Tags slice.
func filteredByTags(tools map[string]*schema.Tool, profile string) map[string]*schema.Tool {
	if profile == "" {
		return tools
	}
	result := make(map[string]*schema.Tool, len(tools))
	for name, tool := range tools {
		if len(tool.Tags) == 0 {
			result[name] = tool
			continue
		}
		for _, tag := range tool.Tags {
			if strings.EqualFold(tag, profile) {
				result[name] = tool
				break
			}
		}
	}
	return result
}

// filterTools applies --only, --skip, and --profile filters to the tool map.
func filterTools(tools map[string]*schema.Tool, only, skip, profile string) map[string]*schema.Tool {
	if only == "" && skip == "" && profile == "" {
		return tools
	}
	skipSet := make(map[string]bool)
	for _, name := range strings.Split(skip, ",") {
		skipSet[strings.TrimSpace(name)] = true
	}
	filtered := make(map[string]*schema.Tool, len(tools))
	for name, tool := range tools {
		if skipSet[name] {
			continue
		}
		if only != "" && name != only {
			continue
		}
		filtered[name] = tool
	}
	if only != "" {
		queue := []string{only}
		visited := map[string]bool{only: true}
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if skipSet[name] {
				// The skipped tool is a required dependency of --only; still add it
				// to filtered so graph.Sort doesn't reject the partial graph.
				if t, ok := tools[name]; ok {
					filtered[name] = t
				}
				continue
			}
			if t, ok := tools[name]; ok {
				filtered[name] = t
				for _, req := range t.Requires {
					if !visited[req] {
						visited[req] = true
						queue = append(queue, req)
					}
				}
			}
		}
	}
	filtered = filteredByTags(filtered, profile)
	return filtered
}

// loadLockfile reads the lockfile for a given schema. Returns nil if no
// lockfile exists or it's corrupted (logs a warning).
// Exits with code 2 if --frozen-lockfile is set and no lock exists.
func loadLockfile(schemaPath string, s *schema.Schema, frozen bool, lg *slog.Logger) *lock.Lock {
	lockPath := lock.DefaultPath(schemaPath)
	lk, err := lock.Load(lockPath)
	if err != nil {
		lg.Warn("lockfile corrupted, continuing without lock", "path", lockPath, "error", err)
	}
	if frozen && lk == nil {
		lg.Error("--frozen-lockfile requires lockfile — run 'depengine update' first", "path", lockPath)
		os.Exit(2)
	}
	if lk != nil {
		lock.Apply(s, lk)
	}
	return lk
}

// saveLockfile resolves version pins, merges with any existing lock, and persists.
func saveLockfile(ctx context.Context, s *schema.Schema, lockPath string, oldLock *lock.Lock, lg *slog.Logger, diagnose bool) {
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

// hasLatestPlaceholders checks whether any tool method in the schema uses
// a {latest} placeholder in its URL. Used by install to decide whether
// auto-resolution is needed when no lockfile exists.
func hasLatestPlaceholders(s *schema.Schema) bool {
	for _, tool := range s.Tools {
		for _, method := range tool.Methods {
			if url, ok := method.Config["url"].(string); ok && strings.Contains(url, "{latest}") {
				return true
			}
		}
	}
	return false
}
