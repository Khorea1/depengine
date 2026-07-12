// Package lock provides a schema.lock mechanism for reproducible installations.
//
// schema.toml declares intent ("install the latest release of tool X"), while
// schema.lock pins the resolved versions so that repeated installs produce the
// same result — akin to Cargo.lock or package-lock.json.
//
// The lock captures resolved {latest} tags for every tool method that uses a
// GitHub release URL. On subsequent installs the lockfile is read and the pinned
// tags are substituted directly into the schema's URL fields before the adapters
// see them. Running `depengine update` force-re-resolves and updates the lock.
//
// Pipeline:
//
//	schema.toml → ParseSchema → resolve {latest} → patch schema → execute
//	                               ↓
//	                           schema.lock
package lock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"depengine/pkg/ghrelease"
	"depengine/pkg/schema"

	"github.com/pelletier/go-toml/v2"
)

// Lock pins resolved placeholder values for reproducible installs.
type Lock struct {
	Version int                `toml:"version"`
	Tools   map[string]ToolPin `toml:"tools"`
}

// ToolPin captures resolved values for one tool's {latest} placeholder.
// The key in Lock.Tools is "<toolName>/<methodKind>" so that the same tool
// with both git and http methods can pin each independently.
type ToolPin struct {
	Latest string `toml:"latest,omitempty"`
}

// DefaultPath returns the default schema.lock path alongside schema.toml.
// If path points to a file, it returns <dir>/schema.lock.
func DefaultPath(schemaPath string) string {
	dir := filepath.Dir(schemaPath)
	return filepath.Join(dir, "schema.lock")
}

// Load reads a lock file. A missing file is NOT an error — returns nil, nil.
func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
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
	return &l, nil
}

// Save writes l to path, creating parent directories as needed.
func Save(path string, l *Lock) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("lock: mkdir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("lock: create %s: %w", path, err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(l); err != nil {
		return fmt.Errorf("lock: encode: %w", err)
	}
	return nil
}

// toolKey builds a stable key for Lock.Tools: "<toolName>/<methodKind>".
func toolKey(toolName, methodKind string) string {
	return toolName + "/" + methodKind
}

// ResolveAll scans every tool method in the schema for {latest} in URL fields,
// resolves them via the GitHub releases API, and returns a Lock with the pinned
// values. Empty lock (no tools needing resolution) is still valid.
func ResolveAll(ctx context.Context, s *schema.Schema) (*Lock, error) {
	l := &Lock{
		Version: 1,
		Tools:   make(map[string]ToolPin),
	}

	for name, tool := range s.Tools {
		for _, method := range tool.Methods {
			if method.Kind != "git" && method.Kind != "http" {
				continue
			}
			urlRaw, ok := method.Config["url"].(string)
			if !ok || !strings.Contains(urlRaw, "{latest}") {
				continue
			}

			resolved, err := ghrelease.ResolveLatest(ctx, urlRaw)
			if err != nil {
				return nil, fmt.Errorf("lock: resolve %s/%s: %w", name, method.Kind, err)
			}

			key := toolKey(name, method.Kind)
			l.Tools[key] = ToolPin{Latest: resolved}
		}
	}

	return l, nil
}

// Apply substitutes pinned values from the lock into the schema's method
// Config maps. When a method has a pin, its entire "url" field is replaced with
// the pinned URL. Methods not present in the lock are left untouched.
func Apply(s *schema.Schema, l *Lock) {
	if l == nil {
		return
	}
	for name, tool := range s.Tools {
		for _, method := range tool.Methods {
			key := toolKey(name, method.Kind)
			pin, ok := l.Tools[key]
			if !ok || pin.Latest == "" {
				continue
			}
			if _, ok := method.Config["url"]; ok {
				method.Config["url"] = pin.Latest
			}
		}
	}
}
