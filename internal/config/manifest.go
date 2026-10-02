package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Khorea1/depengine/internal/methodkind"
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
	if _, err := os.Stat(p); err != nil { // #nosec G703 -- XDG/operator configuration intentionally selects the manifest path being probed.
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
// Each manifest path is parsed with ParseManifest(path, nil) and
// validated with ValidateManifestLayer. An empty manifest path is skipped.
// The result merges all layers: manifest files (earlier = lower priority),
// then the local schema (highest priority).
func ResolveSchemaFromFiles(schemaPath string, manifestPaths ...string) (*Schema, error) {
	s, err := ParseProjectSchema(schemaPath, nil)
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
		mt, err := ParseManifest(mp, nil)
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
//   - Defaults merge field-by-field by declared presence; omitted fields inherit.
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
			Version: 1,
			Defaults: Defaults{
				Manager:     "native",
				AurHelper:   "paru",
				MethodOrder: append([]string(nil), methodkind.DefaultMethodOrder...),
			},
			Tools: make(map[string]*Tool),
		}
	}

	// Project metadata belongs to the most-specific (project schema) layer.
	var mostSpecific *Schema
	for i := len(layers) - 1; i >= 0; i-- {
		if layers[i] != nil {
			mostSpecific = layers[i]
			break
		}
	}
	if mostSpecific == nil {
		return MergeLayersWithOpts(opts)
	}

	result := &Schema{
		Version:       mostSpecific.Version,
		Defaults:      defaultEngineDefaults(),
		Tools:         make(map[string]*Tool),
		AllowNewTools: mostSpecific.AllowNewTools,
		Provenance:    make(map[string][]FieldSource),
		ProjectRoot:   mostSpecific.ProjectRoot,
	}

	for _, layer := range layers {
		if layer == nil {
			continue
		}
		mergeDefaultsInto(result, layer)
		for name, tool := range layer.Tools {
			existing, exists := result.Tools[name]
			if !exists {
				result.Tools[name] = cloneTool(tool)
				continue
			}
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

	// Local/project-relative resources always resolve against the project
	// schema directory, even when a lower-priority personal manifest supplied
	// the field.
	bindProjectRoot(result.Tools, result.ProjectRoot)
	return result
}

func defaultEngineDefaults() Defaults {
	return Defaults{
		Manager:     "native",
		AurHelper:   "paru",
		MethodOrder: append([]string(nil), methodkind.DefaultMethodOrder...),
	}
}

func mergeDefaultsInto(result, layer *Schema) {
	if result == nil || layer == nil {
		return
	}
	if result.defaultsPresence == nil {
		result.defaultsPresence = fieldPresence{}
	}
	for field := range layer.defaultsPresence {
		switch field {
		case "Manager":
			result.Defaults.Manager = layer.Defaults.Manager
		case "AurHelper":
			result.Defaults.AurHelper = layer.Defaults.AurHelper
		case "MethodOrder":
			result.Defaults.MethodOrder = append([]string(nil), layer.Defaults.MethodOrder...)
		case "ArchMap":
			result.Defaults.ArchMap = cloneStringMap(layer.Defaults.ArchMap)
		case "OSMap":
			result.Defaults.OSMap = cloneStringMap(layer.Defaults.OSMap)
		}
		result.defaultsPresence[field] = true
	}
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
		case "map":
			strategy = MergeMapMerge
		case "methods":
			strategy = MergeMethods
		default:
			panic(fmt.Sprintf("config: Tool field %q has unknown merge tag %q", sf.Name, tag))
		}
		fields = append(fields, toolMergeField{index: i, name: sf.Name, strategy: strategy})
	}
	return fields
}

func fieldDeclared(tool *Tool, name string) bool {
	return tool != nil && tool.presence != nil && tool.presence[name]
}

// assignField copies src into dst. String-slice fields are copied
// element-by-element (matching the defensive copies the old setField made
// for Requires/MethodPrefer/MethodOnly/Tags); every other kind is assigned
// directly, which is exactly what the old switch did for the rest.
func assignField(dst, src reflect.Value) {
	if hooks, ok := src.Interface().([]Hook); ok {
		dst.Set(reflect.ValueOf(cloneHooks(hooks)))
		return
	}
	if src.Kind() == reflect.Slice && src.Type().Elem().Kind() == reflect.String {
		cp := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		reflect.Copy(cp, src)
		dst.Set(cp)
		return
	}
	if conditions, ok := src.Interface().(map[string]*Condition); ok {
		cloned := make(map[string]*Condition, len(conditions))
		for key, condition := range conditions {
			if condition == nil {
				cloned[key] = nil
				continue
			}
			cloned[key] = cloneCondition(condition)
		}
		dst.Set(reflect.ValueOf(cloned))
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
	result := cloneTool(lower)
	if result.presence == nil {
		result.presence = fieldPresence{}
	}

	rv := reflect.ValueOf(result).Elem()
	uv := reflect.ValueOf(upper).Elem()
	lv := reflect.ValueOf(lower).Elem()

	for _, tf := range toolMergeFields {
		dstField := rv.Field(tf.index)
		upperField := uv.Field(tf.index)
		lowerField := lv.Field(tf.index)
		schemaVal, manifestVal := upperField.Interface(), lowerField.Interface()
		upperDeclared := fieldDeclared(upper, tf.name)
		lowerDeclared := fieldDeclared(lower, tf.name)

		switch tf.strategy {
		case MergeOverwrite:
			if upperDeclared {
				assignField(dstField, upperField)
				result.presence[tf.name] = true
				pc.record(tf.name, "schema", schemaVal, manifestVal, dstField.Interface())
			} else {
				pc.record(tf.name, "manifest", schemaVal, manifestVal, lowerField.Interface())
			}

		case MergeLocalOnly:
			if upperDeclared {
				assignField(dstField, upperField)
				result.presence[tf.name] = true
				pc.record(tf.name, "schema", schemaVal, manifestVal, dstField.Interface())
			} else if lowerDeclared {
				dstField.Set(reflect.Zero(dstField.Type()))
				delete(result.presence, tf.name)
				pc.record(tf.name, "schema", schemaVal, manifestVal, nil)
			}

		case MergeUnionSlice:
			if lowerDeclared || upperDeclared {
				unionStringSlice(dstField, upperField)
				result.presence[tf.name] = true
				pc.record(tf.name, "both", schemaVal, manifestVal, dstField.Interface())
			}

		case MergeMapMerge:
			if upperDeclared {
				merged := cloneConditionMap(lower.RequiresWhen)
				if merged == nil {
					merged = map[string]*Condition{}
				}
				for key, condition := range upper.RequiresWhen {
					if condition == nil {
						merged[key] = nil
						continue
					}
					merged[key] = cloneCondition(condition)
				}
				result.RequiresWhen = merged
				result.presence[tf.name] = true
			}
			if lowerDeclared || upperDeclared {
				source := "both"
				if upperDeclared && !lowerDeclared {
					source = "schema"
				} else if lowerDeclared && !upperDeclared {
					source = "manifest"
				}
				pc.record(tf.name, source, schemaVal, manifestVal, result.RequiresWhen)
			}

		case MergeMethods:
			merged := mergeMethodSlices(lower.Methods, upper.Methods, pc)
			result.Methods = merged
			pc.record("Methods", "both", upper.Methods, lower.Methods, merged)
		}
	}

	return result
}

func cloneConditionMap(in map[string]*Condition) map[string]*Condition {
	if in == nil {
		return nil
	}
	out := make(map[string]*Condition, len(in))
	for key, condition := range in {
		if condition == nil {
			out[key] = nil
			continue
		}
		out[key] = cloneCondition(condition)
	}
	return out
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
	result := cloneMethod(lower)
	if result.presence == nil {
		result.presence = fieldPresence{}
	}
	// Identity/runtime metadata is tied to the more-specific candidate.
	result.Kind = upper.Kind
	result.Label = upper.Label
	result.ProjectRoot = upper.ProjectRoot
	result.LockedRevision = upper.LockedRevision
	result.LockedDigest = upper.LockedDigest
	result.LockedVersion = upper.LockedVersion
	result.Err = upper.Err

	if upper.presence["Inferred"] {
		result.Inferred = upper.Inferred
		result.presence["Inferred"] = true
	}
	if upper.presence["When"] {
		if upper.When == nil {
			result.When = nil
		} else {
			result.When = cloneCondition(upper.When)
		}
		result.presence["When"] = true
	}
	if upper.presence["ArchMap"] {
		result.ArchMap = cloneStringMap(upper.ArchMap)
		result.presence["ArchMap"] = true
	}
	if upper.presence["OSMap"] {
		result.OSMap = cloneStringMap(upper.OSMap)
		result.presence["OSMap"] = true
	}
	if upper.presence["Requires"] {
		result.Requires = append([]string(nil), upper.Requires...)
		result.presence["Requires"] = true
	}
	if upper.presence["Sources"] {
		result.Sources = cloneSources(upper.Sources)
		result.presence["Sources"] = true
	}
	if upper.presence["PreInstall"] {
		result.PreInstall = cloneHooks(upper.PreInstall)
		result.presence["PreInstall"] = true
	}
	if upper.presence["PostInstall"] {
		result.PostInstall = cloneHooks(upper.PostInstall)
		result.presence["PostInstall"] = true
	}
	if upper.presence["SecretRef"] {
		result.SecretRef = cloneSecretReference(upper.SecretRef)
		result.presence["SecretRef"] = true
	}
	if upper.presence["ChecksumSecretRef"] {
		result.ChecksumSecretRef = cloneSecretReference(upper.ChecksumSecretRef)
		result.presence["ChecksumSecretRef"] = true
	}
	if upper.presence["SignatureSecretRef"] {
		result.SignatureSecretRef = cloneSecretReference(upper.SignatureSecretRef)
		result.presence["SignatureSecretRef"] = true
	}

	if result.Config == nil {
		result.Config = map[string]any{}
	}
	for key, upperVal := range upper.Config {
		lowerVal, inLower := result.Config[key]
		if inLower && MethodConfigFieldStrategy[key] == MergeMapMerge {
			upperMap, upperOK := upperVal.(map[string]any)
			lowerMap, lowerOK := lowerVal.(map[string]any)
			if upperOK && lowerOK {
				merged := cloneAnyMap(lowerMap)
				for k, v := range upperMap {
					merged[k] = cloneAny(v)
				}
				result.Config[key] = merged
				continue
			}
		}
		result.Config[key] = cloneAny(upperVal)
	}

	return result
}

func cloneSecretReference(in *SecretReference) *SecretReference {
	if in == nil {
		return nil
	}
	out := *in
	return &out
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
// across internal/config and its callers: helpers.go, status_remove_forget.go,
// graph_why.go). Deleting it outright would require touching those files too,
// which is outside the internal/config-only scope of this change.
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
