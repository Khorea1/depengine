package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/native"

	"github.com/Khorea1/depengine/pkg/log"
	"github.com/pelletier/go-toml/v2"
)

// MergeStrategy defines how a field is merged across layers.
type MergeStrategy int

const (
	MergeOverwrite  MergeStrategy = iota // most specific wins entirely
	MergeMapMerge                        // key-by-key within map, most specific wins per key
	MergeUnionSlice                      // union without duplicates
	MergeLocalOnly                       // only schema layer may set; manifest presence is error
	MergeMethods                         // special: merge method slices by Kind, then MapMerge Config
)

// MethodConfigFieldStrategy defines merge policy per Config key of MethodCandidate.
var MethodConfigFieldStrategy = map[string]MergeStrategy{
	"pkg":           MergeOverwrite,
	"pkg_overrides": MergeMapMerge,
	"url":           MergeOverwrite,
	"build":         MergeOverwrite,
	"checksum":      MergeOverwrite,
	"checksum_url":  MergeOverwrite,
	"extract_to":    MergeOverwrite,
	"git":           MergeOverwrite,
	"binary":        MergeOverwrite,
}

// FieldSource describes which layer contributed to a field's value.
type FieldSource struct {
	Field    string `json:"field"`
	Source   string `json:"source"` // "schema", "manifest", "both"
	Schema   any    `json:"schema,omitempty"`
	Manifest any    `json:"manifest,omitempty"`
	Merged   any    `json:"merged"`
}

// provenanceCollector accumulates FieldSource entries during a merge.
type provenanceCollector struct {
	toolName string
	sources  []FieldSource
}

func (pc *provenanceCollector) record(field, source string, schemaVal, manifestVal, mergedVal any) {
	if pc == nil {
		return
	}
	pc.sources = append(pc.sources, FieldSource{
		Field:    field,
		Source:   source,
		Schema:   schemaVal,
		Manifest: manifestVal,
		Merged:   mergedVal,
	})
}

// mergeConfig holds options for MergeLayers.
type mergeConfig struct {
	collectProvenance bool
}

// ErrorCode is a stable identifier for a class of validation or schema error.
// This duplicates pkg/validate.ErrorCode to avoid an import cycle.
type ErrorCode string

// ParseSchemaError is returned when a project schema or manifest is invalid
// (invalid TOML, validation errors, redeclared tools, etc.),
// as opposed to an I/O or runtime error. Callers use errors.As to distinguish
// schema errors (exit code 2) from runtime errors (exit code 3).
type ParseSchemaError struct {
	Err error
}

func (e *ParseSchemaError) Error() string { return e.Err.Error() }
func (e *ParseSchemaError) Unwrap() error { return e.Err }

// SchemaCodeError is a typed error carrying a stable ErrorCode, used when the
// schema has a well-defined validation problem (e.g. duplicate tool declaration).
// Callers can use errors.As to extract the code programmatically.
type SchemaCodeError struct {
	Code ErrorCode
	Path string
	Line int
	Msg  string
}

func (e *SchemaCodeError) Error() string {
	return fmt.Sprintf("%s:%d: [%s] %s", e.Path, e.Line, e.Code, e.Msg)
}

// ParseProjectSchema loads and normalizes a project schema.toml.
func ParseProjectSchema(path string, m map[string]string) (*Schema, error) {
	return parseDocument(path, m, "tools")
}

// ParseManifest loads and normalizes a personal manifest.toml.
func ParseManifest(path string, m map[string]string) (*Schema, error) {
	return parseDocument(path, m, "packages")
}

// parseDocument loads, decodes and normalizes a depengine TOML document. It produces a
// flat Schema where the three declaration shapes (simple list, inline table,
// full [tools.X] block) all collapse into Tool + MethodCandidate pairs. The
// substitution map m is applied to every string leaf during normalization:
// this is where {arch}/{distro_family}/{os}/{kernel}/{libc}/{init_system}/...
// get replaced. Pass nil for m to skip placeholder expansion (use for
// read-only operations like check/validate).
//
// Behavior notes:
//   - placeholder expansion runs AFTER TOML decoding and BEFORE ordering,
//     so method_order, when.distro_family and every pkg/url/build field get
//     the same treatment uniformly.
//   - placeholders unknown to m are left untouched; validation layer is
//     responsible for flagging them, not the parser.
//   - the `simple` list is processed first; an inline table redeclaring a
//     simple tool is an error (SchemaCodeError with Code "E_DUPE_TOOL").
func parseDocument(path string, m map[string]string, sectionName string) (*Schema, error) {
	if m == nil {
		m = map[string]string{}
	}
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseSchemaError{Err: fmt.Errorf("reading schema %s: %w", path, err)}
	}

	var raw map[string]any
	if err := toml.Unmarshal(rawBytes, &raw); err != nil {
		return nil, &ParseSchemaError{Err: fmt.Errorf("parse TOML %s: %w", path, err)}
	}

	// {arch}/{os} are deliberately withheld from this first, whole-tree
	// expansion pass and resolved afterwards, once per method (see the
	// arch_map/os_map resolution loop below): arch_map/os_map layering
	// (method > [defaults] > engine builtin) picks the *effective*
	// spelling before {arch}/{os} gets substituted, and that choice can
	// differ per method, so it can't be folded into this single global
	// map. Every other placeholder is expanded here exactly as before.
	// hasArch/hasOS false means m carries no facts at all (validate/check-
	// style callers pass nil or {}), in which case {arch}/{os} are left
	// untouched below too, exactly like the pre-existing behavior for
	// every other unknown-to-m placeholder.
	rawArch, hasArch := m["arch"]
	rawOS, hasOS := m["os"]
	mWithoutArchOS := make(map[string]string, len(m))
	for k, v := range m {
		if k == "arch" || k == "os" {
			continue
		}
		mWithoutArchOS[k] = v
	}
	raw = ExpandAll(raw, mWithoutArchOS).(map[string]any)

	if err := validateRawSchema(raw, sectionName); err != nil {
		return nil, &ParseSchemaError{Err: fmt.Errorf("%s: %w", path, err)}
	}

	// Extract rawTools from the specified section.
	rawTools, _ := raw[sectionName].(map[string]any)
	if rawTools == nil {
		rawTools = map[string]any{}
	}

	defaults := extractDefaults(raw["defaults"])

	tools, err := normalizeTools(path, rawTools, defaults)
	if err != nil {
		return nil, &ParseSchemaError{Err: err}
	}

	allowNewTools := false
	if sectionName == "packages" {
		if manifestRaw, ok := raw["manifest"]; ok {
			if manifestMap, ok := manifestRaw.(map[string]any); ok {
				if allow, ok := manifestMap["allow_new_tools"]; ok {
					allowNewTools, _ = allow.(bool)
				}
			}
		}
	}

	// {arch}/{os} were withheld above; now that arch_map/os_map layering is
	// known (defaults parsed, tools/methods built with their own ArchMap/
	// OSMap hoisted out), resolve them per-recipient:
	//
	//   - "github" method candidates: unchanged from before this feature.
	//     The adapter needs the machine's raw arch/os facts (uname-style
	//     "x86_64", GOOS-style "linux"/"darwin"/...) to resolve
	//     {arch_any}/{os_any} against the real release-asset list at
	//     install time — those two tokens are adapter-owned and were never
	//     in m's key set to begin with, so Expand always left them
	//     untouched. Adapters have no other way to reach engine.Facts, so
	//     we stash the two raw values it needs directly on its own method
	//     candidates here. github does its own arch/os resolution via
	//     regex over every known synonym (ghrelease.archSynonyms/
	//     osSynonyms), so it is deliberately excluded from the arch_map/
	//     os_map mechanism below, which only ever picks one spelling.
	//   - tool.PreInstall/PostInstall: plain shell command strings, not
	//     part of any method's Config, so arch_map/os_map (scoped to
	//     [defaults] and method blocks) doesn't apply to them — they just
	//     get the raw fact value, same as every other placeholder.
	//   - every other method candidate's Config: {arch}/{os} resolve
	//     through resolveAlias's method > defaults > builtin layering
	//     before substitution.
	if hasArch || hasOS {
		backfill := map[string]string{}
		if hasArch {
			backfill["arch"] = rawArch
		}
		if hasOS {
			backfill["os"] = rawOS
		}
		for _, tool := range tools {
			tool.PreInstall = Expand(tool.PreInstall, backfill)
			tool.PostInstall = Expand(tool.PostInstall, backfill)
		}
	}

	for _, tool := range tools {
		for _, mc := range tool.Methods {
			if mc.Kind == "github" {
				mc.Config["_current_arch"] = m["arch"]
				mc.Config["_current_os"] = m["os"]
				continue
			}
			if !hasArch && !hasOS {
				continue
			}
			localMap := map[string]string{}
			if hasArch {
				localMap["arch"] = resolveAlias(rawArch, mc.ArchMap, defaults.ArchMap, defaultArchMap)
			}
			if hasOS {
				localMap["os"] = resolveAlias(rawOS, mc.OSMap, defaults.OSMap, defaultOSMap)
			}
			mc.Config = ExpandAll(mc.Config, localMap).(map[string]any)
		}
	}

	return &Schema{Version: 1, Defaults: defaults, Tools: tools, AllowNewTools: allowNewTools}, nil
}

// DefaultMethodOrder is the engine-wide canonical preference order for
// install methods. This is delegated to methodkind.DefaultMethodOrder — the
// single source of truth.
var DefaultMethodOrder = methodkind.DefaultMethodOrder

func extractDefaults(raw any) Defaults {
	d := Defaults{
		Manager:     "native",
		AurHelper:   "paru",
		MethodOrder: DefaultMethodOrder,
	}
	if raw == nil {
		return d
	}
	rm, ok := raw.(map[string]any)
	if !ok {
		return d
	}
	if v, ok := rm["manager"].(string); ok && v != "" {
		d.Manager = v
	}
	if v, ok := rm["aur_helper"].(string); ok && v != "" {
		d.AurHelper = v
	}
	if v, ok := rm["arch_map"]; ok {
		d.ArchMap = normalizeAliasMap(v)
	}
	if v, ok := rm["os_map"]; ok {
		d.OSMap = normalizeAliasMap(v)
	}
	if v, ok := rm["method_order"].([]any); ok {
		order := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				order = append(order, s)
			} else {
				log.Default.Debug("extractDefaults: ignoring non-string method_order item", "value", item)
			}
		}
		if len(order) > 0 {
			seen := make(map[string]bool, len(order))
			merged := make([]string, 0, len(DefaultMethodOrder))
			merged = append(merged, order...)
			for _, k := range order {
				seen[k] = true
			}
			for _, k := range DefaultMethodOrder {
				if !seen[k] {
					merged = append(merged, k)
				}
			}
			d.MethodOrder = merged
		}
	}
	return d
}

func normalizeTools(path string, rawTools map[string]any, defaults Defaults) (map[string]*Tool, error) {
	tools := map[string]*Tool{}

	// 1. Process `simple` list first.
	if sim, ok := rawTools["simple"].([]any); ok {
		for _, item := range sim {
			name, ok := item.(string)
			if !ok {
				continue
			}
			if _, dup := tools[name]; dup {
				line, _ := findLineInFile(path, name)
				return nil, &SchemaCodeError{
					Code: "E_DUPE_TOOL",
					Path: path,
					Line: line,
					Msg:  fmt.Sprintf("tool %q redeclared (simple list)", name),
				}
			}
			tools[name] = &Tool{
				Name:     name,
				IsSimple: true,
				Methods: []*MethodCandidate{{
					Kind:   defaults.Manager,
					Config: map[string]any{"pkg": name},
				}},
			}
		}
	}

	// 2. Process remaining entries. Sort keys for deterministic error messages.
	for _, name := range sortedKeys(rawTools, "simple") {
		val := rawTools[name]
		if _, dup := tools[name]; dup {
			line, _ := findLineInFile(path, name)
			return nil, &SchemaCodeError{
				Code: "E_DUPE_TOOL",
				Path: path,
				Line: line,
				Msg:  fmt.Sprintf("tool %q redeclared (simple + inline table)", name),
			}
		}
		tool := &Tool{Name: name}
		valMap, ok := val.(map[string]any)
		if !ok {
			line, _ := findLineInFile(path, name)
			return nil, fmt.Errorf("%s:%d: tool %q: expected inline table, got %T", path, line, name, val)
		}

		if r, ok := valMap["requires"].([]any); ok {
			tool.Requires = anySliceToStrings(r)
			if len(tool.Requires) == 0 {
				tool.Requires = nil
			}
		}
		// requires_when gates individual deps by platform facts:
		// `requires_when = { fontconfig = { target_family = ["unix"] } }`.
		if rw, ok := valMap["requires_when"].(map[string]any); ok {
			tool.RequiresWhen = make(map[string]*Condition, len(rw))
			for dep, v := range rw {
				wm, ok := v.(map[string]any)
				if !ok {
					line, _ := findLineInFile(path, name)
					return nil, fmt.Errorf("%s:%d: tool %q: requires_when.%s: expected inline table, got %T", path, line, name, dep, v)
				}
				tool.RequiresWhen[dep] = parseCondition(wm)
			}
		}
		if pi, ok := valMap["pre_install"].(string); ok {
			tool.PreInstall = pi
		}
		// post_install accepts a plain string (unconditional) or
		// the table form { cmd = "...", when = {...} } gating the hook.
		parsePostInstall := func(v any) {
			switch pi := v.(type) {
			case string:
				tool.PostInstall = pi
			case map[string]any:
				if c, ok := pi["cmd"].(string); ok {
					tool.PostInstall = c
				}
				if wm, ok := pi["when"].(map[string]any); ok {
					tool.PostInstallWhen = parseCondition(wm)
				}
			}
		}
		if v, ok := valMap["post_install"]; ok {
			parsePostInstall(v)
		}
		if t, ok := valMap["tags"].([]any); ok {
			tool.Tags = anySliceToStrings(t)
		}

		// Expand bucket names into their corresponding method kinds.
		// Ex: `ruff = { python = true }` → `ruff = { pipx = true, uv = true }`
		// Supports three shapes:
		//   bool:   python = true       → each method gets true
		//   string: python = "pkgname"  → each method gets the package name
		//   map:    python = { pkg = …, when = … } → each method gets a clone of the config
		for k, v := range valMap {
			if methods, ok := DefaultBuckets[k]; ok {
				switch tv := v.(type) {
				case bool:
					if tv {
						for _, m := range methods {
							if _, exists := valMap[m]; !exists {
								valMap[m] = true
							}
						}
						tool.Ecosystem = k
						delete(valMap, k)
					}
				case string:
					for _, m := range methods {
						if _, exists := valMap[m]; !exists {
							valMap[m] = tv
						}
					}
					tool.Ecosystem = k
					delete(valMap, k)
				case map[string]any:
					shared := tv
					for _, m := range methods {
						if _, exists := valMap[m]; !exists {
							cloned := make(map[string]any, len(shared))
							for mk, mv := range shared {
								cloned[mk] = mv
							}
							valMap[m] = cloned
						}
					}
					tool.Ecosystem = k
					delete(valMap, k)
				}
			}
		}
		// Read per-tool method preference keys. The per-tool `method_order`
		// alias (deprecated spelling of method_prefer) has been removed; a
		// schema still using it now gets its value treated like any other
		// unrecognized key — see buildMethods/Validate for the resulting
		// diagnostics.
		if mp, ok := valMap["method_prefer"].([]any); ok {
			order := anySliceToStrings(mp)
			if len(order) > 0 {
				tool.MethodPrefer = order
			}
		}
		if mo, ok := valMap["method_only"].([]any); ok {
			order := anySliceToStrings(mo)
			if len(order) > 0 {
				tool.MethodOnly = order
			}
		}

		tool.Methods = OrderMethods(buildMethods(name, valMap), defaults.MethodOrder)
		tools[name] = tool
	}
	return tools, nil
}

// platformMethodConditions maps method kinds that are inherently bound to
// a single OS/distro family to their implicit when condition. Applied
// during parseMethod when the user hasn't set an explicit when.
var platformMethodConditions = buildPlatformMethodConditions()

func buildPlatformMethodConditions() map[string]Condition {
	out := make(map[string]Condition)
	for _, contract := range methodkind.Contracts {
		if len(contract.ImplicitDistroFamily) > 0 {
			condition := Condition{DistroFamily: append([]string(nil), contract.ImplicitDistroFamily...)}
			out[contract.Kind] = condition
			for _, alias := range contract.Aliases {
				out[alias] = condition
			}
		}
	}
	return out
}

func parseMethod(kind string, val any) (*MethodCandidate, error) {
	mc := &MethodCandidate{Kind: kind, Config: map[string]any{}}

	switch t := val.(type) {
	case string:
		// inline scalar: `apt = "fd-find"` → pkg
		mc.Config["pkg"] = t
	case bool:
		if t {
			// true → usa tool.Name como pkg (SubstitutePkg fallback)
			mc.Config["pkg"] = ""
		} else {
			return nil, fmt.Errorf("method %q: invalid value false (use true, a package name, or a config table)", kind)
		}
	case map[string]any:
		// `when` and `kind` are hoisted out; everything else stays in Config.
		if rawWhen, ok := t["when"]; ok {
			mc.When = parseCondition(rawWhen)
			delete(t, "when")
		}
		if rawKind, ok := t["kind"]; ok {
			if ks, ok := rawKind.(string); ok && ks != "" {
				mc.Kind = ks      // override adapter dispatch key
				mc.Label = kind   // store TOML section key as label
				delete(t, "kind") // don't pass to adapter
			}
		}
		if rawArchMap, ok := t["arch_map"]; ok {
			mc.ArchMap = normalizeAliasMap(rawArchMap)
			delete(t, "arch_map")
		}
		if rawOSMap, ok := t["os_map"]; ok {
			mc.OSMap = normalizeAliasMap(rawOSMap)
			delete(t, "os_map")
		}
		for k, v := range t {
			mc.Config[k] = v
		}
	default:
		return nil, fmt.Errorf("invalid method value type: %T", val)
	}

	// Apply implicit platform condition if user didn't set explicit when.
	if mc.When == nil {
		if cond, ok := platformMethodConditions[kind]; ok {
			mc.When = &cond
		}
	}

	return mc, nil
}

// buildMethods processes the keys of an inline-table tool declaration and
// collapses native-manager overrides into a single "native" method
// with a pkg_overrides map, while keeping non-native keys as separate methods.
//
// When a tool declares only non-native methods (go, cargo, pip, etc.) without
// any native manager overrides, a "native" method is automatically injected
// with the tool name as the default package name.
//
// A native candidate is injected so the native method is available for every
// tool. SelectMethods later removes it when method_only excludes native; the
// raw candidate set stays intact so layer merging cannot discard information.
//
// If native is in the effective method_order (user list or canonical
// remainder), it will be tried in that position. If the tool also declares
// non-native methods, those appear as separate candidates ordered by
// method_order.
//
// Example: fd = { apt = "fd-find" }
//
//	→ [{Kind:"native", Config:{"pkg":"fd", "pkg_overrides":{"apt":"fd-find"}}}]
//
// Example: fzf = { go = "github.com/junegunn/fzf" }
//
//	→ [{Kind:"native", Config:{"pkg":"fzf"}},
//	   {Kind:"go",    Config:{"pkg":"github.com/junegunn/fzf"}}]
//
// Example: organize = { pip = "organize-tool", pipx = "organize-tool" }
//
//	→ [{Kind:"native", Config:{"pkg":"organize"}},
//	   {Kind:"pip",  ...}, {Kind:"pipx", ...}]
//
// Example: nvim = { pacman = "neovim", apt = "neovim", brew = "neovim" }
//
//	→ [{Kind:"native", Config:{"pkg":"nvim",
//	    "pkg_overrides":{"pacman":"neovim","apt":"neovim","brew":"neovim"}}}]
func buildMethods(name string, valMap map[string]any) []*MethodCandidate {
	var methods []*MethodCandidate
	nativeOverrides := map[string]any{}
	var nonNativeKeys []string
	var nativeBlockConfig map[string]any

	for _, k := range sortedKeys(valMap, "requires", "requires_when", "pre_install", "post_install", "tags", "method_prefer", "method_only", "when", "kind") {
		if k == "native" {
			if m, ok := valMap[k].(map[string]any); ok {
				nativeBlockConfig = m
				continue
			}
			if s, ok := valMap[k].(string); ok {
				nativeBlockConfig = map[string]any{"pkg": s}
				continue
			}
		}
		if _, isStr := valMap[k].(string); isStr && native.IsNativeManagerName(k) {
			nativeOverrides[k] = valMap[k]
		} else {
			nonNativeKeys = append(nonNativeKeys, k)
		}
	}

	// Inject a native method when there are any relevant keys.
	// With overrides if native manager names are present, plain otherwise.
	if len(nativeOverrides) > 0 || len(nonNativeKeys) > 0 || nativeBlockConfig != nil {
		cfg := map[string]any{"pkg": name}
		if len(nativeOverrides) > 0 {
			cfg["pkg_overrides"] = nativeOverrides
		}
		for k, v := range nativeBlockConfig {
			cfg[k] = v
		}
		var when *Condition
		if rawWhen, ok := cfg["when"]; ok {
			when = parseCondition(rawWhen)
			delete(cfg, "when")
		}
		// Hoist arch_map/os_map the same way parseMethod does, in case a
		// native = { ... } block ever needs to override the {arch}/{os}
		// spelling (native pkg names rarely template on arch/os, but the
		// mechanism should be consistent across every method Config).
		archMap := normalizeAliasMap(cfg["arch_map"])
		osMap := normalizeAliasMap(cfg["os_map"])
		delete(cfg, "arch_map")
		delete(cfg, "os_map")
		methods = append(methods, &MethodCandidate{
			Kind:    "native",
			When:    when,
			Config:  cfg,
			ArchMap: archMap,
			OSMap:   osMap,
		})
	}

	for _, k := range nonNativeKeys {
		mc, err := parseMethod(k, valMap[k])
		if err != nil {
			// Attach the parse error to the method so callers can surface it.
			methods = append(methods, &MethodCandidate{
				Kind:   k,
				Config: map[string]any{},
				Err:    err,
			})
			continue
		}
		methods = append(methods, mc)
	}

	return methods
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		return anySliceToStrings(t)
	case string:
		return []string{t} // single-value sugar
	default:
		return nil
	}
}

func anySliceToStrings(in []any) []string {
	out := make([]string, 0, len(in))
	for i, v := range in {
		if s, ok := v.(string); ok {
			out = append(out, s)
		} else {
			log.Default.Warn("anySliceToStrings: discarding non-string element",
				"index", i,
				"type", fmt.Sprintf("%T", v),
				"value", v,
			)
		}
	}
	return out
}

// sortedKeys returns the keys of m in sorted order, excluding those in exclude.
func sortedKeys(m map[string]any, exclude ...string) []string {
	excludeSet := make(map[string]bool, len(exclude))
	for _, k := range exclude {
		excludeSet[k] = true
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if !excludeSet[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// findLineInFile returns the 1-based line number of the first occurrence of key in the file.
// Used to augment error messages with file positions when go-toml/v2 doesn't expose them.
func findLineInFile(path, key string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.Contains(line, key+" ") || strings.Contains(line, key+"=") || strings.Contains(line, key+".") {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("key %q not found", key)
}

// Validate checks a parsed Schema for references to method kinds that no
// adapter will ever satisfy. Unknown kinds in Defaults.MethodOrder or any
// MethodCandidate.Kind cause a tool whose only candidates are unknown to
// be silently skipped by the executor at runtime — Validate surfaces that
// at parse time instead. knownKinds is typically exec.RegisteredKinds();
// it is a parameter rather than an import so pkg/config stays free of a
// circular dependency on pkg/exec.
//
// Unknown declared method kinds are hard errors even when another candidate
// could succeed: silently skipping a typo makes the effective schema harder
// to audit. Defaults.MethodOrder entries remain warnings because they are a
// global preference and may intentionally name an adapter not registered by
// the current binary.
func Validate(s *Schema, knownKinds []string) ([]string, error) {
	set := make(map[string]struct{}, len(knownKinds))
	for _, k := range knownKinds {
		set[k] = struct{}{}
	}

	var hardErrors []string
	var warnings []string

	// Check Defaults.MethodOrder entries first.
	for _, kind := range s.Defaults.MethodOrder {
		if native.IsNativeManagerName(kind) {
			continue // valid: native manager name, resolved at execution time
		}
		if _, isBucket := DefaultBuckets[kind]; isBucket {
			warnings = append(warnings, fmt.Sprintf(
				"defaults.method_order entry %q is a bucket name → expands to %v",
				kind, DefaultBuckets[kind]))
			continue
		}
		if _, ok := set[kind]; !ok {
			warnings = append(warnings, fmt.Sprintf(
				"warning: defaults.method_order lists unknown kind %q (no adapter registered for this name)",
				kind,
			))
		}
	}

	// Build set from method_order for Part B check.
	orderSet := make(map[string]struct{}, len(s.Defaults.MethodOrder))
	for _, k := range s.Defaults.MethodOrder {
		orderSet[k] = struct{}{}
	}
	// Check each tool's method candidates. Sort tool names for deterministic output.
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, toolName := range names {
		tool := s.Tools[toolName]

		// Zero-method tools are valid (dependency groups — StatusVirtual). Skip method validation.
		if len(tool.Methods) == 0 {
			continue
		}

		var unknownKinds []string
		for _, mc := range tool.Methods {
			if _, ok := set[mc.Kind]; !ok {
				unknownKinds = append(unknownKinds, mc.Kind)
			}
		}
		if len(unknownKinds) > 0 {
			// Build prefix hints for variant detection (e.g. "http-musl" → "http").
			prefixHints := map[string]string{}
			for _, uk := range unknownKinds {
				for known := range set {
					if strings.HasPrefix(uk, known) {
						prefixHints[uk] = fmt.Sprintf(
							"\n  note: %q looks like a variant of %q — set kind = %q in the method block",
							uk, known, known,
						)
						break
					}
				}
			}
			for _, uk := range unknownKinds {
				msg := fmt.Sprintf(
					"method kind %q for tool %q is not a registered adapter — if this is a variant of an existing method kind (e.g. \"http-musl\" of \"http\"), add kind = \"<kind>\" to the method block",
					uk, toolName,
				)
				if hint := prefixHints[uk]; hint != "" {
					msg += hint
				}
				hardErrors = append(hardErrors, msg)
			}
		}

		// Check for parse errors on individual methods (e.g. pip = false).
		for _, mc := range tool.Methods {
			if mc.Err != nil {
				hardErrors = append(hardErrors, fmt.Sprintf(
					"tool %q: %v",
					toolName, mc.Err,
				))
			}
		}

		// Part C: warn if tool name matches a known method kind.
		if _, ok := set[toolName]; ok {
			warnings = append(warnings, fmt.Sprintf(
				"tool %q has the same name as method kind %q — ensure this is intentional",
				toolName, toolName,
			))
		}

		// Part B: warn if some method kinds are absent from method_order.
		var inOrder, notInOrder []string
		for _, mc := range tool.Methods {
			if native.IsNativeManagerName(mc.Kind) {
				continue // native manager aliases resolve to "native" at runtime
			}
			if _, ok := orderSet[mc.Kind]; ok {
				inOrder = append(inOrder, mc.Kind)
			} else {
				notInOrder = append(notInOrder, mc.Kind)
			}
		}
		if len(inOrder) > 0 && len(notInOrder) > 0 {
			for _, kind := range notInOrder {
				warnings = append(warnings, fmt.Sprintf(
					"tool %q has method %q which is not in method_order — it will be tried after all ordered methods",
					toolName, kind,
				))
			}
		}
	}

	// Validate per-tool method preference entries (method_prefer, method_only).
	for _, toolName := range names {
		tool := s.Tools[toolName]

		// Every selector must match a declared candidate by label or kind.
		checkOrderSlice := func(slice []string, fieldName string) {
			for _, selector := range ExpandBuckets(slice) {
				if native.IsNativeManagerName(selector) {
					selector = "native"
				}
				matched := false
				for _, method := range tool.Methods {
					if methodMatchesSelector(method, selector) {
						matched = true
						break
					}
				}
				if !matched {
					hardErrors = append(hardErrors, fmt.Sprintf(
						"tool %q: %s entry %q does not match a declared method label or kind",
						toolName, fieldName, selector,
					))
				}
			}
		}

		if len(tool.MethodPrefer) > 0 {
			checkOrderSlice(tool.MethodPrefer, "method_prefer")
		}
		if len(tool.MethodOnly) > 0 {
			checkOrderSlice(tool.MethodOnly, "method_only")
		}
	}

	if len(hardErrors) > 0 {
		return warnings, &ParseSchemaError{Err: errors.New(strings.Join(hardErrors, "\n"))}
	}
	return warnings, nil
}
