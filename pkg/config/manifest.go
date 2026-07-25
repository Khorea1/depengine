package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/pkg/methodkind"
)

// DefaultManifestPath returns the path to the user's personal manifest,
// discovered via:
//
//  1. DEPENGINE_MANIFEST env var (if set and non-empty)
//  2. $XDG_CONFIG_HOME/depengine/manifest.toml (or ~/.config/… if unset)
//
// Returns "" if no manifest file exists at the resolved path.
func DefaultManifestPath() string {
	if env := os.Getenv("DEPENGINE_MANIFEST"); env != "" {
		return env
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		xdg = filepath.Join(home, ".config")
	}
	p := filepath.Join(xdg, "depengine", "manifest.toml")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// ResolveSchemaFromFiles is a convenience that parses a local schema and one
// or more manifest files (in order), validates the manifest layers.
//
// Each manifest path is parsed with ParseSchema(path, nil, "packages") and
// validated with ValidateManifestLayer. An empty manifest path is skipped.
// The result merges all layers: manifest files (earlier = lower priority),
// then the local schema (highest priority).
func ResolveSchemaFromFiles(schemaPath string, manifestPaths ...string) (*Schema, error) {
	s, err := ParseSchema(schemaPath, nil)
	if err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}

	// Collect layers from least to most specific.
	layers := []*Schema{s}

	for _, mp := range manifestPaths {
		if mp == "" {
			continue
		}
		mt, err := ParseSchema(mp, nil, "packages")
		if err != nil {
			return nil, fmt.Errorf("parse manifest %s: %w", mp, err)
		}
		if err := ValidateManifestLayer(mt); err != nil {
			return nil, fmt.Errorf("manifest %s: %w", mp, err)
		}
		// Insert manifest layers before the schema layer so schema wins.
		layers = append(layers[:len(layers)-1], mt, layers[len(layers)-1])
	}

	return MergeLayers(layers...), nil
}

// MergeLayers merges an ordered list of Schema pointers, from least specific
// (lowest priority) to most specific (highest priority), and returns a new
// *Schema. Later layers win entirely per-tool (not field-by-field).
//
// Rules:
//   - If a tool exists in multiple layers, the MOST SPECIFIC layer's version
//     wins entirely (whole Tool struct).
//   - If a tool only exists in one layer, that version is used.
//   - Defaults come from the most specific layer that has them.
//   - Method ordering is preserved from the merged schema's defaults.
func MergeLayers(layers ...*Schema) *Schema {
	if len(layers) == 0 {
		return &Schema{
			Defaults: Defaults{
				Manager:     "native",
				AurHelper:   "paru",
				MethodOrder: methodkind.DefaultMethodOrder,
			},
		}
	}

	// Defaults from the most specific layer.
	result := &Schema{
		Defaults: layers[len(layers)-1].Defaults,
		Tools:    make(map[string]*Tool),
	}

	// Iterate layers in order; later layers overwrite earlier ones.
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		for name, tool := range layer.Tools {
			result.Tools[name] = tool
		}
	}

	return result
}

// ValidateManifestLayer validates that a Schema parsed from the manifest
// layer does not contain fields that are security-sensitive or should only
// appear in the local schema. Returns a clear error listing all violations.
//
// Fields NOT allowed in the manifest layer:
//   - pre_install (arbitrary commands from a shared file)
//   - post_install / postinstall (arbitrary commands from a shared file)
//   - requires (dependency declarations)
//   - tags (profile filtering intent)
func ValidateManifestLayer(s *Schema) error {
	var errs []string
	for name, tool := range s.Tools {
		if tool.PreInstall != "" {
			errs = append(errs, fmt.Sprintf("tool %q: pre_install is not allowed in manifest layer", name))
		}
		if tool.PostInstall != "" {
			errs = append(errs, fmt.Sprintf("tool %q: post_install is not allowed in manifest layer", name))
		}
		if len(tool.Requires) > 0 {
			errs = append(errs, fmt.Sprintf("tool %q: requires is not allowed in manifest layer", name))
		}
		if len(tool.Tags) > 0 {
			errs = append(errs, fmt.Sprintf("tool %q: tags is not allowed in manifest layer", name))
		}
	}
	if len(errs) > 0 {
	return fmt.Errorf("manifest validation:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}