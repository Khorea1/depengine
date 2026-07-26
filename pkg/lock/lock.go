// Package lock provides a depengine.lock mechanism for reproducible installations.
//
// schema.toml declares intent ("install the latest release of tool X"), while
// depengine.lock pins the resolved versions so that repeated installs produce the
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
//	                           depengine.lock
package lock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/pkg/ghrelease"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"

	"github.com/pelletier/go-toml/v2"
)

// Lock pins resolved placeholder values for reproducible installs.
type Lock struct {
	Version int                `toml:"version"`
	Tools   map[string]ToolPin `toml:"tools"`
}

// ToolPin captures resolved values for one tool's {latest} placeholder and/or
// checksum. The key in Lock.Tools is "<toolName>/<methodKind>/<idx>" so that methods
// of the same kind (e.g. two http methods as mirrors) each get their own pin.
type ToolPin struct {
	Latest   string `toml:"latest,omitempty"`
	Checksum string `toml:"checksum,omitempty"` // pinned concrete checksum (e.g. "sha256:abc123...")
}

// DefaultPath returns the default lockfile path for a given schema file.
// Always produces a lockfile named "depengine.lock" in the same directory
// as the schema file — matches Cargo.lock and package-lock.json conventions.
//   schema.toml   → depengine.lock
//   depengine.toml → depengine.lock
//   depends.toml  → depengine.lock
func DefaultPath(schemaPath string) string {
	dir := filepath.Dir(schemaPath)
	return filepath.Join(dir, "depengine.lock")
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

	// Write to a temp file in the same directory (ensures same-filesystem rename).
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
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

	return nil
}

// toolKey builds a stable key for Lock.Tools: "<toolName>/<methodKind>/<idx>".
func toolKey(toolName, methodKind string, idx int) string {
	return fmt.Sprintf("%s/%s/%d", toolName, methodKind, idx)
}

// ResolveAll scans every tool method in the schema for {latest} in URL fields,
// resolves them via the GitHub releases API, and returns a Lock with the pinned
// values. Empty lock (no tools needing resolution) is still valid.
func ResolveAll(ctx context.Context, s *config.Schema, rn run.Runner) (*Lock, error) {
	l := &Lock{
		Version: 1,
		Tools:   make(map[string]ToolPin),
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
			if method.Kind == "git" || method.Kind == "http" {
				if urlRaw, ok := method.Config["url"].(string); ok && strings.Contains(urlRaw, "{latest}") {
					tag, err := ghrelease.ResolveLatestTag(ctx, urlRaw, rn)
					if err != nil {
						return nil, fmt.Errorf("lock: resolve %s/%s: %w", name, method.Kind, err)
					}
					pin.Latest = tag
				}
			}

			// Capture concrete checksum (prefer adapter-resolved hash over manual pin).
			if checksum, ok := method.Config["_checksum_resolved"].(string); ok && checksum != "" {
				pin.Checksum = checksum
			} else if checksum, ok := method.Config["checksum"].(string); ok && checksum != "" && !strings.HasSuffix(checksum, ":auto") {
				pin.Checksum = checksum
			}

			if pin.Latest != "" || pin.Checksum != "" {
				l.Tools[key] = pin
			}
		}
	}

	return l, nil
}

// Apply substitutes pinned values from the lock into the schema's method
// Config maps. When a method has a pin with a Latest, every "{latest}"
// occurrence in its current "url" field is replaced with the pinned tag
// (the URL template itself always comes from the schema being applied to,
// so a template edited since the lock was last updated still takes effect).
func Apply(s *config.Schema, l *Lock) {
	if l == nil {
		return
	}
	for name, tool := range s.Tools {
		kindCount := make(map[string]int)
		for _, method := range tool.Methods {
			idx := kindCount[method.Kind]
			kindCount[method.Kind] = idx + 1
			key := toolKey(name, method.Kind, idx)
			pin, ok := l.Tools[key]
			if !ok {
				continue
			}

			// Substitute {latest} in the current URL template with the
			// pinned version tag.
			if pin.Latest != "" {
				if urlRaw, ok := method.Config["url"].(string); ok && strings.Contains(urlRaw, "{latest}") {
					method.Config["url"] = strings.ReplaceAll(urlRaw, "{latest}", pin.Latest)
				}
			}

			// Apply pinned checksum — replace :auto with concrete hash.
			if pin.Checksum != "" {
				if v, ok := method.Config["checksum"].(string); ok && strings.HasSuffix(v, ":auto") {
					method.Config["checksum"] = pin.Checksum
				}
			}
		}
	}
}
