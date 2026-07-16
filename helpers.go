package main

import (
	"context"
	"errors"
	"fmt"
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
	if verr, warnings := schema.Validate(s, exec.RegisteredKinds()); verr != nil {
		return nil, "", nil, verr
	} else if len(warnings) > 0 {
		for _, w := range warnings {
			log.Default.Warn(w)
		}
	}
	return s, clan, facts, nil
}

// exitCodeForError maps common bootstrap errors to exit codes.
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
		fmt.Fprintf(os.Stderr, "warning: lockfile %q is corrupted, continuing without lock: %v\n", lockPath, err)
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
