package schema

import (
	"fmt"
	"os"
	"path/filepath"

	"depengine/pkg/log"
	"github.com/pelletier/go-toml/v2"
)

// ParseManifest reads a personal manifest file from path and returns the
// package installation knowledge as a map of tool name → *Tool.
//
// Returns nil, nil if the file does not exist or has no [packages] section.
// Errors on invalid TOML or parse failures within [packages].
func ParseManifest(path string) (map[string]*Tool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var decoded map[string]any
	if err := toml.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	raw, ok := decoded["packages"]
	if !ok {
		return nil, nil
	}

	rawMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("manifest [packages]: expected table, got %T", raw)
	}

	tools, err := normalizeTools(rawMap, Defaults{Manager: "native"})
	if err != nil {
		return nil, fmt.Errorf("manifest [packages]: %w", err)
	}

	// Strip intent-only fields (requires, post_install, pre_install, tags).
	// The manifest only encodes installation knowledge; intent belongs in the schema.
	for name, tool := range tools {
		if tool.Requires != nil {
			log.Default.Debug("manifest: ignoring requires in [packages]", "tool", name)
			tool.Requires = nil
		}
		if tool.PostInstall != "" {
			log.Default.Debug("manifest: ignoring post_install in [packages]", "tool", name)
			tool.PostInstall = ""
		}
		if tool.PreInstall != "" {
			log.Default.Debug("manifest: ignoring pre_install in [packages]", "tool", name)
			tool.PreInstall = ""
		}
		if len(tool.Tags) > 0 {
			log.Default.Debug("manifest: ignoring tags in [packages]", "tool", name)
			tool.Tags = nil
		}
	}

	return tools, nil
}

// ResolveSchema returns a new *Schema with methods merged from manifest
// and the number of manifest tools that contributed to the result.
// The original schema is not mutated.
//
// Merge rules (per tool):
//
//  1. Tool only in schema → copied as-is.
//  2. Tool in schema AND manifest:
//     a. Native method:
//     - Config["pkg"]: schema wins, EXCEPT when schema.IsSimple == true AND
//     schema's pkg == tool.Name (auto-injected default). In that case,
//     manifest's pkg wins.
//     - Config["pkg_overrides"]: map merge. Schema keys win; manifest keys
//     not present in schema are added.
//     - Other Config keys: schema wins.
//     - When: schema's When if non-nil, otherwise manifest's.
//     b. Non-native methods: if schema has the same Kind → schema wins
//     (no config merge). If schema lacks that Kind → manifest method added.
//     c. Manifest methods with Err != nil → skipped (logged at debug).
//     d. Requires, PreInstall, PostInstall, Tags: always from schema.
//     e. IsSimple: preserved from schema.
//  3. Methods are re-ordered by schema.Defaults.MethodOrder.
//  4. Tools only in manifest → ignored (manifest doesn't add new tools).
func ResolveSchema(s *Schema, manifest map[string]*Tool) (*Schema, int) {
	result := &Schema{
		Defaults: s.Defaults,
		Tools:    make(map[string]*Tool, len(s.Tools)),
	}
	mergedCount := 0

	for name, st := range s.Tools {
		mt, inManifest := manifest[name]
		if !inManifest {
			result.Tools[name] = deepCopyTool(st)
			continue
		}

		merged := deepCopyTool(st)
		merged.Methods = mergeMethods(name, st, mt, s.Defaults.MethodOrder)
		result.Tools[name] = merged
		mergedCount++
	}

	return result, mergedCount
}

// ResolveSchemaFromFiles is a convenience that calls ParseSchemaNoFacts,
// ParseManifest for each manifest path (in order), and ResolveSchema.
// If a manifest path is empty, it is skipped.
func ResolveSchemaFromFiles(schemaPath string, manifestPaths ...string) (*Schema, error) {
	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}

	for _, mp := range manifestPaths {
		if mp == "" {
			continue
		}
		mt, err := ParseManifest(mp)
		if err != nil {
			return nil, fmt.Errorf("parse manifest %s: %w", mp, err)
		}
		if mt != nil {
			s, _ = ResolveSchema(s, mt)
		}
	}

	return s, nil
}

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

// -- merge helpers --

// mergeMethods combines schema and manifest methods for a single tool.
func mergeMethods(name string, st, mt *Tool, methodOrder []string) []*MethodCandidate {
	// Index manifest methods by Kind, skipping errored ones.
	manifestByKind := make(map[string]*MethodCandidate, len(mt.Methods))
	for _, m := range mt.Methods {
		if m.Err != nil {
			log.Default.Debug("resolve: skipping manifest method with parse error",
				"tool", name, "kind", m.Kind, "error", m.Err)
			continue
		}
		manifestByKind[m.Kind] = m
	}

	// Collect present Kinds in schema for gap-fill detection.
	seenKinds := make(map[string]bool, len(st.Methods))
	for _, m := range st.Methods {
		seenKinds[m.Kind] = true
	}

	out := make([]*MethodCandidate, 0, len(st.Methods)+len(mt.Methods))

	// 1. Process each schema method.
	for _, sm := range st.Methods {
		switch sm.Kind {
		case "native":
			mm, inManifest := manifestByKind["native"]
			if !inManifest {
				out = append(out, sm)
				continue
			}
			out = append(out, mergeNativeMethods(st, sm, mm))
		default:
			// Non-native: schema method wins as-is.
			out = append(out, sm)
		}
	}

	// 2. Add manifest methods whose Kind is absent from schema.
	for kind, mm := range manifestByKind {
		if !seenKinds[kind] {
			out = append(out, deepCopyMethod(mm))
		}
	}

	// 3. Re-order.
	return orderByMethodOrder(out, methodOrder)
}

// mergeNativeMethods applies the native-specific merge rules.
// Returns a new *MethodCandidate (does not mutate inputs).
func mergeNativeMethods(st *Tool, schemaNative, manifestNative *MethodCandidate) *MethodCandidate {
	merged := deepCopyMethod(schemaNative)

	// Config["pkg"]: schema wins, except IsSimple auto-injected default.
	manifestPkg, manifestHasPkg := manifestNative.Config["pkg"]
	if manifestHasPkg {
		schemaPkg, schemaHasPkg := schemaNative.Config["pkg"]
		if schemaHasPkg && st.IsSimple {
			schemaPkgStr, _ := schemaPkg.(string)
			if schemaPkgStr == st.Name {
				merged.Config["pkg"] = manifestPkg
			}
		}
	}

	// Config["pkg_overrides"]: merge maps (schema keys win, missing added).
	schemaOverrides, _ := schemaNative.Config["pkg_overrides"].(map[string]any)
	manifestOverrides, _ := manifestNative.Config["pkg_overrides"].(map[string]any)
	if schemaOverrides != nil || manifestOverrides != nil {
		mergedOV := make(map[string]any, len(schemaOverrides)+len(manifestOverrides))
		for k, v := range schemaOverrides {
			mergedOV[k] = v
		}
		for k, v := range manifestOverrides {
			if _, exists := mergedOV[k]; !exists {
				mergedOV[k] = v
			}
		}
		merged.Config["pkg_overrides"] = mergedOV
	}

	// When: schema wins if non-nil.
	if merged.When == nil {
		merged.When = deepCopyCondition(manifestNative.When)
	}

	return merged
}

// -- deep copy helpers --

func deepCopyTool(t *Tool) *Tool {
	methods := make([]*MethodCandidate, len(t.Methods))
	for i, m := range t.Methods {
		methods[i] = deepCopyMethod(m)
	}
	var requires, tags []string
	if t.Requires != nil {
		requires = append([]string{}, t.Requires...)
	}
	if t.Tags != nil {
		tags = append([]string{}, t.Tags...)
	}
	return &Tool{
		Name:        t.Name,
		PreInstall:  t.PreInstall,
		PostInstall: t.PostInstall,
		Requires:    requires,
		Methods:     methods,
		IsSimple:    t.IsSimple,
		Tags:        tags,
	}
}

func deepCopyMethod(m *MethodCandidate) *MethodCandidate {
	cfg := make(map[string]any, len(m.Config))
	for k, v := range m.Config {
		cfg[k] = v
	}
	return &MethodCandidate{
		Kind:   m.Kind,
		When:   deepCopyCondition(m.When),
		Config: cfg,
		Err:    m.Err,
	}
}

func deepCopyCondition(c *Condition) *Condition {
	if c == nil {
		return nil
	}
	df := make([]string, len(c.DistroFamily))
	copy(df, c.DistroFamily)
	return &Condition{DistroFamily: df}
}
