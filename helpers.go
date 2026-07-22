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
)

// defaultSchemaPath returns the default schema file path, trying common names.
// If none exist, returns "schema.toml" so the caller gets the original error.
func defaultSchemaPath() string {
	candidates := []string{"depengine.toml", "schema.toml", "depends.toml"}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "depengine.toml"
}

// loadSchema reads and validates a schema.toml from path, gathering OS facts.
// Returns the parsed Schema, clan name, Facts, or an error for exitCodeForError.
func loadSchema(path string) (*schema.Schema, string, *engine.Facts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", nil, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "depengine.toml")
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
	if verr, warnings := schema.Validate(s, exec.RegisteredKinds()); verr != nil {
		return nil, "", nil, verr
	} else if len(warnings) > 0 {
		for _, w := range warnings {
			log.Default.Warn(w)
		}
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


// resolveManifestPath returns the manifest path to use and whether it was
// auto-discovered. If flagValue is non-empty it is used directly. If empty,
// DefaultManifestPath() is tried. A non-empty flagValue that was explicitly
// set to "" (via --manifest "") returns ("", false) — the caller must check
// the explicitEmpty flag from flag.Visit to enforce that.
//
// intended usage:
//
//	flagSet.String("manifest", "", ...)
//	flagSet.Parse(args)
//	explicitEmpty := false
//	flagSet.Visit(func(f *flag.Flag) {
//	    if f.Name == "manifest" && f.Value.String() == "" {
//	        explicitEmpty = true
//	    }
//	})
//	manifestPath, autoDisc := resolveManifestPath(*manifestFlag, explicitEmpty)
func resolveManifestPath(flagValue string, explicitEmpty bool) (manifestPath string, autoDiscovered bool) {
	if explicitEmpty {
		return "", false
	}
	if flagValue != "" {
		return flagValue, false
	}
	mp := schema.DefaultManifestPath()
	if mp != "" {
		return mp, true
	}
	return "", false
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
		lg.Error("--frozen-lockfile requires schema.lock — run 'depengine update' first")
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
