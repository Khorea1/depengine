package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// FilterManifestTools removes tools from the manifest that are not present in
// the schema when AllowNewTools is false. When AllowNewTools is true, all tools
// are kept (they may introduce new capabilities).
//
// Manifest-only tools are "rejected" (silently excluded) by default — they
// don't participate in validation or merging unless AllowNewTools is set.
func FilterManifestTools(schema, manifest *Schema) {
	if manifest.AllowNewTools {
		return
	}
	for name := range manifest.Tools {
		if _, exists := schema.Tools[name]; !exists {
			delete(manifest.Tools, name)
		}
	}
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

	// Collect layers from least to most specific: every manifest in the
	// order given, then the schema last (highest priority).
	layers := make([]*Schema, 0, len(manifestPaths)+1)

	for _, mp := range manifestPaths {
		if mp == "" {
			continue
		}
		mt, err := ParseSchema(mp, nil, "packages")
		if err != nil {
			return nil, fmt.Errorf("parse manifest %s: %w", mp, err)
		}
		// Strip manifest-only tools before validation+merge.
		// When AllowNewTools=false, they're "rejected" (silently excluded).
		FilterManifestTools(s, mt)
		if err := ValidateManifestLayer(mt); err != nil {
			return nil, fmt.Errorf("manifest %s: %w", mp, err)
		}
		if err := ValidateManifestNewTools(s, mt); err != nil {
			return nil, fmt.Errorf("manifest %s: %w", mp, err)
		}
		layers = append(layers, mt)
	}
	layers = append(layers, s)

	return MergeLayers(layers...), nil
}

// MergeLayers merges an ordered list of Schema pointers, from least specific
// (lowest priority) to most specific (highest priority), and returns a new
// *Schema. Field-level merge strategies are applied per ToolFieldStrategy.
//
// Rules:
//   - If a tool exists in multiple layers, fields are merged per their
//     MergeStrategy (Overwrite, LocalOnly, UnionSlice, or Methods).
//   - If a tool only exists in one layer, that version is used.
//   - Defaults come from the most specific layer that has them.
//   - Method ordering is preserved from the merged schema's defaults.
func MergeLayers(layers ...*Schema) *Schema {
	return MergeLayersWithOpts(nil, layers...)
}

// MergeLayersWithProvenance is like MergeLayers but also collects provenance
// information describing which layer contributed to each field.
func MergeLayersWithProvenance(layers ...*Schema) *Schema {
	return MergeLayersWithOpts(&mergeConfig{collectProvenance: true}, layers...)
}

// MergeLayersWithOpts is like MergeLayers but accepts merge options.
func MergeLayersWithOpts(opts *mergeConfig, layers ...*Schema) *Schema {
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
		Defaults:   layers[len(layers)-1].Defaults,
		Tools:      make(map[string]*Tool),
		Provenance: make(map[string][]FieldSource),
	}

	// Iterate layers in order; later layers overwrite earlier ones field-by-field.
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		for name, tool := range layer.Tools {
			existing, exists := result.Tools[name]
			if !exists {
				// First occurrence: clone and use as-is.
				result.Tools[name] = cloneTool(tool)
				continue
			}
			// Merge tool: existing is lower priority, tool is higher priority.
			var pc *provenanceCollector
			if opts != nil && opts.collectProvenance {
				pc = &provenanceCollector{toolName: name}
			}
			result.Tools[name] = mergeTools(existing, tool, pc)
			if pc != nil {
				result.Provenance[name] = append(result.Provenance[name], pc.sources...)
			}
		}
	}

	return result
}

// mergeTools merges two Tool values using the field strategies in ToolFieldStrategy.
// lower is the lower-priority (less specific) layer, upper is the higher-priority one.
// The result is a new *Tool (cloned).
func mergeTools(lower, upper *Tool, pc *provenanceCollector) *Tool {
	// Start with the lower layer as base.
	result := cloneTool(lower)

	for field, strategy := range ToolFieldStrategy {
		schemeVal, manifestVal := schemaValue(upper, lower, field)
		switch strategy {
		case MergeOverwrite:
			// Use upper (more specific) value if it's set.
			if isFieldSet(upper, field) {
				_ = setField(result, upper, field)
				pc.record(field, "schema", schemeVal, manifestVal, upperFieldValue(upper, field))
			} else {
				pc.record(field, "manifest", schemeVal, manifestVal, lowerFieldValue(lower, field))
			}

		case MergeLocalOnly:
			// Only set from upper (schema) layer; ignore lower layer values.
			if isFieldSet(upper, field) {
				_ = setField(result, upper, field)
				pc.record(field, "schema", schemeVal, manifestVal, upperFieldValue(upper, field))
			} else if isFieldSet(lower, field) {
				// Lower had it but MergeLocalOnly means propagate only if upper also has it.
				// But we clear it because lower shouldn't have set it.
				_ = setFieldZero(result, field)
				pc.record(field, "schema", schemeVal, manifestVal, nil)
			} else {
				pc.record(field, "manifest", schemeVal, manifestVal, "-")
			}

		case MergeUnionSlice:
			// Union of lower's and upper's elements without duplicates.
			if isFieldSet(lower, field) || isFieldSet(upper, field) {
				result = mergeSlices(result, lower, upper, field)
				pc.record(field, "both", schemeVal, manifestVal, upperFieldValue(upper, field))
			}

		case MergeMethods:
			merged := mergeMethodSlices(lower.Methods, upper.Methods, pc)
			result.Methods = merged
			pc.record("Methods", "both", lower.Methods, upper.Methods, merged)
		}
	}

	return result
}

// mergeMethodSlices merges two MethodCandidate slices by identity (Kind + Label).
// lower is the lower-priority layer, upper is higher-priority.
func mergeMethodSlices(lower, upper []*MethodCandidate, pc *provenanceCollector) []*MethodCandidate {
	// Index lower by identity key (Kind + NUL + Label).
	byKind := make(map[string]*MethodCandidate, len(lower))
	order := make([]string, 0, len(lower))
	for _, m := range lower {
		key := methodKey(m)
		byKind[key] = m
		order = append(order, key)
	}

	// Merge upper into lower.
	for _, upperMethod := range upper {
		key := methodKey(upperMethod)
		if lowerMethod, exists := byKind[key]; exists {
			merged := mergeMethodConfigs(lowerMethod, upperMethod, pc)
			byKind[key] = merged
		} else {
			byKind[key] = cloneMethod(upperMethod)
			order = append(order, key)
		}
	}

	// Build result preserving order (lower methods first, then new upper methods).
	result := make([]*MethodCandidate, 0, len(byKind))
	for _, k := range order {
		result = append(result, byKind[k])
	}
	return result
}

// methodKey returns an identity key for a MethodCandidate, combining Kind and
// Label with a NUL separator so that Kind="http"+Label="mirror" does not
// collide with Kind="http-mirror"+Label="".
func methodKey(m *MethodCandidate) string {
	if m.Label != "" {
		return m.Kind + "\x00" + m.Label
	}
	return m.Kind
}

// mergeMethodConfigs merges two MethodCandidate values for the same Kind.
// lower is lower priority, upper is higher priority.
func mergeMethodConfigs(lower, upper *MethodCandidate, pc *provenanceCollector) *MethodCandidate {
	// Start with upper as base (more specific).
	result := cloneMethod(upper)

	// For each key in lower.Config that upper doesn't have, copy it up.
	for key, lowerVal := range lower.Config {
		upperVal, inUpper := upper.Config[key]
		if !inUpper {
			result.Config[key] = lowerVal
			continue
		}

		// Both have the key. Check the merge strategy.
		strategy := MethodConfigFieldStrategy[key]
		if strategy == MergeMapMerge {
			// Merge inner maps.
			upperMap, upperOk := upperVal.(map[string]any)
			lowerMap, lowerOk := lowerVal.(map[string]any)
			if upperOk && lowerOk {
				merged := make(map[string]any, len(upperMap)+len(lowerMap))
				for k, v := range lowerMap {
					merged[k] = v
				}
				for k, v := range upperMap {
					merged[k] = v
				}
				result.Config[key] = merged
			}
			// If either isn't a map, fall back to Overwrite (upper wins)
		}
		// For MergeOverwrite or any other strategy: upper already in place.
	}

	return result
}
// mergeSlices applies MergeUnionSlice for a specific field. It only knows
// about "Tags" today — that is the only field registered with
// MergeUnionSlice in ToolFieldStrategy. If a new field is ever given that
// strategy, it MUST get a case here too, or its values will silently pass
// through unmerged (the clone from mergeTools' base layer wins with no
// union and no error).
func mergeSlices(result *Tool, lower, upper *Tool, field string) *Tool {
	switch field {
	case "Tags":
		seen := make(map[string]bool, len(result.Tags))
		for _, v := range result.Tags {
			seen[v] = true
		}
		for _, v := range upper.Tags {
			if !seen[v] {
				result.Tags = append(result.Tags, v)
				seen[v] = true
			}
		}
	default:
		panic(fmt.Sprintf("mergeSlices: field %q has MergeUnionSlice strategy but no merge case implemented", field))
	}
	return result
}

// field helpers for mergeTools

// schemaValue returns the values from upper and lower layers for a field,
// using "schema" to mean the more specific (upper) layer.
func schemaValue(upper, lower *Tool, field string) (schemaVal, manifestVal any) {
	return upperFieldValue(upper, field), lowerFieldValue(lower, field)
}

func upperFieldValue(t *Tool, field string) any {
	if t == nil {
		return nil
	}
	switch field {
	case "Name":
		return t.Name
	case "PreInstall":
		return t.PreInstall
	case "PostInstall":
		return t.PostInstall
	case "PostInstallWhen":
		return t.PostInstallWhen
	case "Requires":
		return t.Requires
	case "RequiresWhen":
		return t.RequiresWhen
	case "Methods":
		return t.Methods
	case "MethodOrder":
		return t.MethodOrder
	case "MethodPrefer":
		return t.MethodPrefer
	case "MethodOnly":
		return t.MethodOnly
	case "IsSimple":
		return t.IsSimple
	case "Tags":
		return t.Tags
	case "Ecosystem":
		return t.Ecosystem
	default:
		return nil
	}
}

func lowerFieldValue(t *Tool, field string) any {
	return upperFieldValue(t, field)
}

// isFieldSet reports whether a field on `upper` has been set.
func isFieldSet(upper *Tool, field string) bool {
	if upper == nil {
		return false
	}
	switch field {
	case "Name":
		return upper.Name != ""
	case "PreInstall":
		return upper.PreInstall != ""
	case "PostInstall":
		return upper.PostInstall != ""
	case "PostInstallWhen":
		return upper.PostInstallWhen != nil
	case "Requires":
		return len(upper.Requires) > 0
	case "RequiresWhen":
		return len(upper.RequiresWhen) > 0
	case "Methods":
		return len(upper.Methods) > 0
	case "MethodOrder":
		return len(upper.MethodOrder) > 0
	case "MethodPrefer":
		return len(upper.MethodPrefer) > 0
	case "MethodOnly":
		return len(upper.MethodOnly) > 0
	case "IsSimple":
		return upper.IsSimple
	case "Tags":
		return len(upper.Tags) > 0
	case "Ecosystem":
		return upper.Ecosystem != ""
	default:
		return false
	}
}

// setField copies a field value from src to dst.
func setField(dst, src *Tool, field string) error {
	switch field {
	case "Name":
		dst.Name = src.Name
	case "PreInstall":
		dst.PreInstall = src.PreInstall
	case "PostInstall":
		dst.PostInstall = src.PostInstall
	case "PostInstallWhen":
		dst.PostInstallWhen = src.PostInstallWhen
	case "Requires":
		dst.Requires = append([]string{}, src.Requires...)
	case "RequiresWhen":
		dst.RequiresWhen = src.RequiresWhen
	case "Methods":
		dst.Methods = cloneMethods(src.Methods)
	case "MethodOrder":
		dst.MethodOrder = append([]string{}, src.MethodOrder...)
	case "MethodPrefer":
		dst.MethodPrefer = append([]string{}, src.MethodPrefer...)
	case "MethodOnly":
		dst.MethodOnly = append([]string{}, src.MethodOnly...)
	case "IsSimple":
		dst.IsSimple = src.IsSimple
	case "Tags":
		dst.Tags = append([]string{}, src.Tags...)
	case "Ecosystem":
		dst.Ecosystem = src.Ecosystem
	}
	return nil
}

// setFieldZero resets a field to its zero value.
func setFieldZero(t *Tool, field string) error {
	switch field {
	case "Name":
		t.Name = ""
	case "PreInstall":
		t.PreInstall = ""
	case "PostInstall":
		t.PostInstall = ""
		t.PostInstallWhen = nil
	case "PostInstallWhen":
		t.PostInstallWhen = nil
	case "Requires":
		t.Requires = nil
	case "RequiresWhen":
		t.RequiresWhen = nil
	case "Methods":
		t.Methods = nil
	case "MethodOrder":
		t.MethodOrder = nil
	case "MethodPrefer":
		t.MethodPrefer = nil
	case "MethodOnly":
		t.MethodOnly = nil
	case "IsSimple":
		t.IsSimple = false
	case "Tags":
		t.Tags = nil
	case "Ecosystem":
		t.Ecosystem = ""
	}
	return nil
}

// ValidateManifestNewTools checks that the manifest does not introduce tools
// not present in the schema unless AllowNewTools is true in the manifest.
//
// When AllowNewTools is false, manifest-only tools are silently ignored
// ("rejected" = excluded from merge, not an error). Use FilterManifestTools
// before this function to strip them from the manifest, or simply rely on
// the fact that this function returns nil — the contract is that manifest-only
// tools are excluded by default, not errored.
//
// This is intentionally always nil today (kept for call-site/API stability
// across pkg/config and its callers: helpers.go, status_remove_forget.go,
// graph_why.go). Deleting it outright would require touching those files too,
// which is outside the pkg/config-only scope of this change.
func ValidateManifestNewTools(schema, manifest *Schema) error {
	_ = schema // kept for signature compatibility; filtering is handled by FilterManifestTools
	return nil
}

func ValidateManifestLayer(s *Schema) error {
	var errs []string
	// Sort tool names for deterministic error messages across runs
	toolNames := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	for _, name := range toolNames {
		tool := s.Tools[name]
		// Sort fields for deterministic error messages
		fields := make([]string, 0, len(ToolFieldStrategy))
		for field := range ToolFieldStrategy {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			strategy := ToolFieldStrategy[field]
			if strategy != MergeLocalOnly {
				continue
			}
			isSet, ok := ToolFieldIsSet[field]
			if !ok {
				continue
			}
			if isSet(tool) {
				// Use TOML-style field name for error messages.
				tomlName := toolFieldToTOML(field)
				errs = append(errs, fmt.Sprintf("tool %q: %s is not allowed in manifest layer", name, tomlName))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("manifest validation:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// toolFieldToTOML maps Go struct field names to their TOML equivalents for
// user-facing error messages.
func toolFieldToTOML(field string) string {
	switch field {
	case "PreInstall":
		return "pre_install"
	case "PostInstall":
		return "post_install"
	case "PostInstallWhen":
		return "post_install.when"
	case "Requires":
		return "requires"
	case "RequiresWhen":
		return "requires_when"
	case "Tags":
		return "tags"
	case "MethodOrder":
		return "method_order"
	case "MethodPrefer":
		return "method_prefer"
	case "MethodOnly":
		return "method_only"
	default:
		return field
	}
}

