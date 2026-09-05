package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

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
// *Schema. Field-level merge strategies are applied per each field's `merge`
// struct tag on Tool (see schema.go).
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

// toolMergeField pairs a Tool struct field (by index) with the strategy
// declared in its `merge` tag.
type toolMergeField struct {
	index    int
	name     string
	strategy MergeStrategy
}

// toolMergeFields is computed once from Tool's `merge` struct tags: the
// single source of truth for how each field is merged across layers. This
// replaces the old ToolFieldStrategy map (a second place every field name
// had to be listed) and the four hand-written switch statements that used
// to read it (schemaValue/isFieldSet/setField/setFieldZero) — one generic,
// reflection-driven implementation now serves every field kind Tool has.
var toolMergeFields = buildToolMergeFields()

func buildToolMergeFields() []toolMergeField {
	t := reflect.TypeOf(Tool{})
	fields := make([]toolMergeField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("merge")
		if tag == "" {
			continue
		}
		var strategy MergeStrategy
		switch tag {
		case "overwrite":
			strategy = MergeOverwrite
		case "local_only":
			strategy = MergeLocalOnly
		case "union":
			strategy = MergeUnionSlice
		case "methods":
			strategy = MergeMethods
		default:
			panic(fmt.Sprintf("config: Tool field %q has unknown merge tag %q", sf.Name, tag))
		}
		fields = append(fields, toolMergeField{index: i, name: sf.Name, strategy: strategy})
	}
	return fields
}

// fieldIsSet reports whether v (a field of *Tool) holds a non-default value,
// using the same per-kind zero test the old field-name switch used to encode
// one case at a time: strings compare to "", bools to false, slices/maps to
// length 0, pointers to nil.
func fieldIsSet(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() != ""
	case reflect.Bool:
		return v.Bool()
	case reflect.Slice, reflect.Map:
		return v.Len() > 0
	case reflect.Ptr, reflect.Interface:
		return !v.IsNil()
	default:
		return !v.IsZero()
	}
}

// assignField copies src into dst. String-slice fields are copied
// element-by-element (matching the defensive copies the old setField made
// for Requires/MethodPrefer/MethodOnly/Tags); every other kind is assigned
// directly, which is exactly what the old switch did for the rest (Name,
// PreInstall, PostInstall, PostInstallWhen, RequiresWhen, IsSimple, Ecosystem).
func assignField(dst, src reflect.Value) {
	if src.Kind() == reflect.Slice && src.Type().Elem().Kind() == reflect.String {
		cp := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		reflect.Copy(cp, src)
		dst.Set(cp)
		return
	}
	dst.Set(src)
}

// unionStringSlice appends elements of upper onto dst that aren't already
// present, preserving dst's existing order. Works for any []string field,
// so a new field tagged `merge:"union"` is handled automatically instead of
// needing its own case (the old mergeSlices panicked until one was added).
func unionStringSlice(dst, upper reflect.Value) {
	seen := make(map[string]bool, dst.Len())
	for i := 0; i < dst.Len(); i++ {
		seen[dst.Index(i).String()] = true
	}
	for i := 0; i < upper.Len(); i++ {
		s := upper.Index(i).String()
		if !seen[s] {
			dst.Set(reflect.Append(dst, upper.Index(i)))
			seen[s] = true
		}
	}
}

// mergeTools merges two Tool values using the per-field strategy declared in
// each field's `merge` struct tag (see toolMergeFields). lower is the
// lower-priority (less specific) layer, upper is the higher-priority one.
// The result is a new *Tool (cloned).
func mergeTools(lower, upper *Tool, pc *provenanceCollector) *Tool {
	// Start with the lower layer as base.
	result := cloneTool(lower)

	rv := reflect.ValueOf(result).Elem()
	uv := reflect.ValueOf(upper).Elem()
	lv := reflect.ValueOf(lower).Elem()

	for _, tf := range toolMergeFields {
		dstField := rv.Field(tf.index)
		upperField := uv.Field(tf.index)
		lowerField := lv.Field(tf.index)
		schemeVal, manifestVal := upperField.Interface(), lowerField.Interface()

		switch tf.strategy {
		case MergeOverwrite:
			// Use upper (more specific) value if it's set.
			if fieldIsSet(upperField) {
				assignField(dstField, upperField)
				pc.record(tf.name, "schema", schemeVal, manifestVal, dstField.Interface())
			} else {
				pc.record(tf.name, "manifest", schemeVal, manifestVal, lowerField.Interface())
			}

		case MergeLocalOnly:
			// Only set from upper (schema) layer; ignore lower layer values.
			if fieldIsSet(upperField) {
				assignField(dstField, upperField)
				pc.record(tf.name, "schema", schemeVal, manifestVal, dstField.Interface())
			} else if fieldIsSet(lowerField) {
				// Lower had it but MergeLocalOnly means propagate only if upper also has it.
				// But we clear it because lower shouldn't have set it.
				dstField.Set(reflect.Zero(dstField.Type()))
				pc.record(tf.name, "schema", schemeVal, manifestVal, nil)
			} else {
				pc.record(tf.name, "manifest", schemeVal, manifestVal, "-")
			}

		case MergeUnionSlice:
			// Union of lower's and upper's elements without duplicates.
			if fieldIsSet(lowerField) || fieldIsSet(upperField) {
				unionStringSlice(dstField, upperField)
				pc.record(tf.name, "both", schemeVal, manifestVal, dstField.Interface())
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

// ValidateManifestLayer previously rejected manifest-layer tools that set
// fields tagged `merge:"local_only"` ("schema layer only"). No Tool field
// has used that tag since intent fields (pre_install, post_install,
// requires, method_prefer, ...) were deliberately opened up to the manifest
// layer — see TestValidateManifestLayer_AcceptsIntentFields. That made the
// per-field loop this function used to run permanently unreachable (it could
// never find a local_only field to reject), so it always returned nil while
// its doc comment implied an active check. The dead loop has been removed;
// the function is now an explicit no-op, kept only so its five call sites
// (helpers.go, status_remove_forget.go, validate_check.go, graph_why.go)
// don't need to change. If a future field needs manifest-layer rejection,
// tag it `merge:"local_only"` on Tool and reinstate a check here.
func ValidateManifestLayer(s *Schema) error {
	return nil
}
