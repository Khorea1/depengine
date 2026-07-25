package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/native"

	"github.com/Khorea1/depengine/pkg/log"
	"github.com/pelletier/go-toml/v2"
)

// Schema is the fully-normalized in-memory form of schema.toml after parsing.
// It is the engine's working set: defaults + a flat map of tools, each with
// its method candidates ordered by defaults.method_order.
type Schema struct {
	Defaults Defaults
	Tools    map[string]*Tool
}

// Defaults mirrors the [defaults] table. Omitted fields keep engine-safe
// defaults baked in here (not at use sites), so a single place documents the
// contract:manager defaults to native, aur_helper to paru, method_order to the
// engine-wide canonical order.
type Defaults struct {
	Manager     string
	AurHelper   string
	MethodOrder []string
}


// DefaultBuckets maps ecosystem names to lists of method kinds.
// This is delegated to methodkind.DefaultBuckets — the single source of truth.
var DefaultBuckets = methodkind.DefaultBuckets
// Tool is one entry under [tools]. IsSimple distinguishes names that came
// straight from the `simple = [...]` list (single native candidate whose
// pkg equals the tool's own name) from anything declared via inline table
// or full block.
type Tool struct {
	Name        string
	PreInstall  string // shell command run before install; failure aborts install
	PostInstall string // shell command run after successful install
	Requires    []string
	Methods     []*MethodCandidate
	MethodOrder []string // DEPRECATED per-tool: use MethodPrefer instead. Kept for backward compat.
	MethodPrefer []string // prefix: try these first, then fall back to defaults
	MethodOnly  []string // exclusive: use ONLY these methods, in this order
	IsSimple    bool
	Tags        []string // profile tags for --profile filtering (e.g. "desktop", "server")
	Ecosystem   string   // "python", "node", etc — empty if not from bucket
}

// MethodCandidate is one way to install the parent Tool. Kind is the
// manager/adapter name ("native", "cargo", "git", "http", ...). When is the
// optional applicability guard (today only distro_family). Config holds the
// method-specific fields verbatim (pkg, url, build, checksum, extract_to,
// git, ...) — strings here are subject to placeholder expansion.
// Err is non-nil when parseMethod encountered a value it could not process;
// callers should surface this with other diagnostics rather than silently
// dropping the method.
type MethodCandidate struct {
	Kind   string
	When   *Condition
	Config map[string]any
	Err    error
}

// Condition is the parsed form of `when = { ... }`. Today only
// distro_family is honored; the struct is exported so future fields
// (arch, libc, ...) can be added without changing call sites.
type Condition struct {
	DistroFamily []string
	DistroID     []string
	Arch         []string
	OS           []string
	Kernel       []string
	Libc         []string
	InitSystem   []string
	IsWSL        *bool
	IsContainer  *bool
}

func (c *Condition) IsZero() bool {
	return len(c.DistroFamily) == 0 &&
		len(c.DistroID) == 0 &&
		len(c.Arch) == 0 &&
		len(c.OS) == 0 &&
		len(c.Kernel) == 0 &&
		len(c.Libc) == 0 &&
		len(c.InitSystem) == 0 &&
		c.IsWSL == nil &&
		c.IsContainer == nil
}

// Match reports whether this condition is satisfied by the given system facts.
// All non-zero fields must match (AND semantics). Empty/nil fields are skipped.
// Returns true for a nil receiver (nil condition = always match).
func (c *Condition) Match(facts *engine.Facts) bool {
	if c == nil {
		return true
	}
	if facts == nil {
		// Conservative: can't verify without facts. A condition with only
		// DistroFamily matches if no other fields are set (empty condition).
		return c.IsZero()
	}

	// DistroFamily: resolved clan (ResolveFamily is a pure function)
	if len(c.DistroFamily) > 0 {
		clan := engine.ResolveFamily(facts)
		if !engine.MatchesDistroFamily(clan, c.DistroFamily) {
			return false
		}
	}

	// String-slice fields: case-insensitive OR-match (exact)
	if len(c.DistroID) > 0 && !matchExact(c.DistroID, facts.DistroID) {
		return false
	}
	if len(c.Arch) > 0 && !matchExact(c.Arch, facts.TargetArch) {
		return false
	}
	if len(c.OS) > 0 && !matchExact(c.OS, facts.OS) {
		return false
	}
	if len(c.Kernel) > 0 && !matchExact(c.Kernel, facts.Kernel) {
		return false
	}
	if len(c.InitSystem) > 0 && !matchExact(c.InitSystem, facts.InitSystem) {
		return false
	}

	// Libc: prefix match for version-suffixed values
	if len(c.Libc) > 0 && !matchPrefix(c.Libc, facts.Libc) {
		return false
	}

	// Three-state bools
	if c.IsWSL != nil && *c.IsWSL != facts.IsWSL {
		return false
	}
	if c.IsContainer != nil && *c.IsContainer != facts.IsContainer {
		return false
	}

	return true
}

func matchExact(allowed []string, actual string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, actual) {
			return true
		}
	}
	return false
}

func matchPrefix(allowed []string, actual string) bool {
	actualLower := strings.ToLower(actual)
	for _, a := range allowed {
		if strings.HasPrefix(actualLower, strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// ParseSchemaError is returned by ParseSchema when the problem is in the
// schema file itself (invalid TOML, validation errors, redeclared tools, etc.),
// as opposed to an I/O or runtime error. Callers use errors.As to distinguish
// schema errors (exit code 2) from runtime errors (exit code 3).
type ParseSchemaError struct {
	Err error
}

func (e *ParseSchemaError) Error() string { return e.Err.Error() }
func (e *ParseSchemaError) Unwrap() error { return e.Err }

// ParseSchema loads, decodes and normalizes a schema.toml file. It produces a
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
//     simple tool is an error (TODO.md accepts this as E_DUPE_TOOL).
//   - section is the TOML table name to read for tool declarations
//     (default "tools"; use "packages" for manifest files).
func ParseSchema(path string, m map[string]string, section ...string) (*Schema, error) {
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

	// Expand placeholders across the entire raw tree in one pass. Non-string
	// leaves are returned unchanged by ExpandAll, so booleans/ints survive.
	raw = ExpandAll(raw, m).(map[string]any)

	// Determine which section to read tool declarations from.
	sectionName := "tools"
	if len(section) > 0 && section[0] != "" {
		sectionName = section[0]
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

	return &Schema{Defaults: defaults, Tools: tools}, nil
}


// DefaultMethodOrder is the engine-wide canonical preference order for
// install methods. This is delegated to methodkind.DefaultMethodOrder — the
// single source of truth.
var DefaultMethodOrder = methodkind.DefaultMethodOrder
func extractDefaults(raw any) Defaults {
	d := Defaults{
		Manager:   "native",
		AurHelper: "paru",
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
				return nil, fmt.Errorf("%s:%d: tool %q redeclared (simple list)", path, line, name)
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
			return nil, fmt.Errorf("%s:%d: tool %q redeclared (simple + inline table)", path, line, name)
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
		if pi, ok := valMap["pre_install"].(string); ok {
			tool.PreInstall = pi
		} else if pi, ok := valMap["preinstall"].(string); ok {
			tool.PreInstall = pi
		}
		if pi, ok := valMap["post_install"].(string); ok {
			tool.PostInstall = pi
		} else if pi, ok := valMap["postinstall"].(string); ok {
			tool.PostInstall = pi
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
		// Read per-tool method preference keys.
		if mo, ok := valMap["method_order"].([]any); ok {
			order := anySliceToStrings(mo)
			if len(order) > 0 {
				tool.MethodPrefer = order
				tool.MethodOrder = order // backward compat
				log.Default.Warn(fmt.Sprintf("tool %q: [defaults].method_order is deprecated for per-tool use; use method_prefer = %v instead", name, order))
			}
		}
		if mp, ok := valMap["method_prefer"].([]any); ok {
			order := anySliceToStrings(mp)
			if len(order) > 0 {
				if tool.MethodPrefer != nil {
					// Both method_order and method_prefer specified — method_prefer wins
					log.Default.Warn(fmt.Sprintf("tool %q: both method_order and method_prefer specified; method_prefer takes precedence", name))
				}
				tool.MethodPrefer = order
			}
		}
		if mo, ok := valMap["method_only"].([]any); ok {
			order := anySliceToStrings(mo)
			if len(order) > 0 {
				tool.MethodOnly = order
			}
		}

		methods := buildMethods(name, valMap)
		effectiveOrder := EffectiveMethodOrder(tool, defaults.MethodOrder, defaults.Manager)
		tool.Methods = OrderMethods(methods, effectiveOrder)
		tools[name] = tool
	}
	return tools, nil
}

// platformMethodConditions maps method kinds that are inherently bound to
// a single OS/distro family to their implicit when condition. Applied
// during parseMethod when the user hasn't set an explicit when.
var platformMethodConditions = map[string]Condition{
	"aur":   {DistroFamily: []string{"arch"}},
	"cask":  {DistroFamily: []string{"macos"}},
	"mas":   {DistroFamily: []string{"macos"}},
	"scoop": {DistroFamily: []string{"windows"}},
	"choco": {DistroFamily: []string{"windows"}},
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
		// `when` is hoisted out; everything else stays in Config.
		if rawWhen, ok := t["when"]; ok {
			mc.When = parseCondition(rawWhen)
		}
		for k, v := range t {
			if k == "when" {
				continue
			}
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
// A native candidate is automatically injected so the native method
// is available for every tool. If native is in the effective method_order
// (user list or canonical remainder), it will be tried in that position.
// If the tool also declares non-native methods, those appear as separate
// candidates ordered by method_order.
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

	for _, k := range sortedKeys(valMap, "requires", "pre_install", "preinstall", "post_install", "postinstall", "tags", "method_order", "method_prefer", "method_only", "when") {
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

	// Always inject a native method when there are any relevant keys.
	// With overrides if native manager names are present, plain otherwise.
	if len(nativeOverrides) > 0 || len(nonNativeKeys) > 0 || nativeBlockConfig != nil {
		cfg := map[string]any{"pkg": name}
		if len(nativeOverrides) > 0 {
			cfg["pkg_overrides"] = nativeOverrides
		}
		for k, v := range nativeBlockConfig {
			cfg[k] = v
		}
		methods = append(methods, &MethodCandidate{
			Kind:   "native",
			Config: cfg,
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

func parseBoolPtr(v any) *bool {
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}

func parseCondition(raw any) *Condition {
	rm, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	cond := &Condition{}
	var invalidKeys []string
	for k, v := range rm {
		switch k {
		case "distro_family":
			cond.DistroFamily = toStringSlice(v)
		case "distro_id":
			cond.DistroID = toStringSlice(v)
		case "arch":
			cond.Arch = toStringSlice(v)
		case "os":
			cond.OS = toStringSlice(v)
		case "kernel":
			cond.Kernel = toStringSlice(v)
		case "libc":
			cond.Libc = toStringSlice(v)
		case "init_system":
			cond.InitSystem = toStringSlice(v)
		case "is_wsl":
			cond.IsWSL = parseBoolPtr(v)
		case "is_container":
			cond.IsContainer = parseBoolPtr(v)
		default:
			invalidKeys = append(invalidKeys, k)
		}
	}
	if len(invalidKeys) > 0 {
		log.Default.Debug("parseCondition: ignoring unknown keys", "keys", invalidKeys)
	}
	if cond.IsZero() {
		return nil
	}
	return cond
}
func OrderMethods(methods []*MethodCandidate, order []string) []*MethodCandidate {
	prio := map[string]int{}
	for i, k := range order {
		prio[k] = i
	}
	out := make([]*MethodCandidate, len(methods))
	copy(out, methods)
	sort.SliceStable(out, func(i, j int) bool {
		pi, okI := prio[out[i].Kind]
		pj, okJ := prio[out[j].Kind]
		switch {
		case okI && okJ:
			return pi < pj
		case okI:
			return true
		case okJ:
			return false
		default:
			return false
		}
	})
	return out
}

// ExpandMethodOrder resolves native manager references in a method_order
// list for the current machine's native manager.
//
// Rules:
//   - If the native manager name (e.g. "apt") appears explicitly in the
//     list, all "native" entries are removed (avoids duplicate attempts).
//   - The native manager name entry is replaced with "native" so it matches
//     the method kind used by schema parsing.
//   - All other entries pass through unchanged.
//   - If nativeManagerName is empty (unknown clan), the order is returned
//     unchanged.
func ExpandMethodOrder(order []string, nativeManagerName string) []string {
	if nativeManagerName == "" {
		return order
	}

	hasExplicitNativeMgr := false
	for _, k := range order {
		if k == nativeManagerName {
			hasExplicitNativeMgr = true
			break
		}
	}

	expanded := make([]string, 0, len(order))
	for _, k := range order {
		switch {
		case k == "native" && hasExplicitNativeMgr:
			// Skip: native manager name has its own explicit entry
		case k == nativeManagerName:
			expanded = append(expanded, "native")
		default:
			expanded = append(expanded, k)
		}
	}
	return expanded
}

// MergeMethodOrder merges a tool-specific method_order with the default
// order. The tool's list forms the prefix; entries from the default not
// already in the tool's list are appended as the remainder.
// toolOrder may be nil (returns defaultOrder unchanged).
func MergeMethodOrder(toolOrder, defaultOrder []string) []string {
	if toolOrder == nil {
		return defaultOrder
	}
	seen := make(map[string]bool, len(toolOrder))
	merged := make([]string, 0, len(defaultOrder))
	for _, k := range toolOrder {
		merged = append(merged, k)
		seen[k] = true
	}
	for _, k := range defaultOrder {
		if !seen[k] {
			merged = append(merged, k)
		}
	}
	return merged
}

// ExpandBuckets replaces bucket names in a method order list with their
// constituent concrete method kinds. Delegates to methodkind.ExpandBuckets.
func ExpandBuckets(order []string) []string {
	return methodkind.ExpandBuckets(order)
}

// EffectiveMethodOrder returns the method order effective for a given tool,
// considering per-tool MethodOnly (exclusive), MethodPrefer (preferred prefix),
// deprecated MethodOrder (alias for MethodPrefer), or defaultOrder (fallback).
// When nativeManagerName is a specific distro manager (e.g. "apt", "pacman"),
// native manager references in the order are expanded. Pass empty string or "native"
// to skip expansion (appropriate at schema-parse time).
func EffectiveMethodOrder(tool *Tool, defaultOrder []string, nativeManagerName string) []string {
	needsExpand := nativeManagerName != "" && nativeManagerName != "native"

	// Always expand bucket names in the default order first.
	defaultOrder = ExpandBuckets(defaultOrder)

	// method_only: exclusive list — no remainder from defaults.
	if len(tool.MethodOnly) > 0 {
		toolList := ExpandBuckets(tool.MethodOnly)
		if needsExpand {
			return ExpandMethodOrder(toolList, nativeManagerName)
		}
		return toolList
	}

	// method_prefer: prefix + remainder from defaults.
	if len(tool.MethodPrefer) > 0 {
		toolList := ExpandBuckets(tool.MethodPrefer)
		if needsExpand {
			expDefault := ExpandMethodOrder(defaultOrder, nativeManagerName)
			expTool := ExpandMethodOrder(toolList, nativeManagerName)
			return MergeMethodOrder(expTool, expDefault)
		}
		return MergeMethodOrder(toolList, defaultOrder)
	}

	// method_order: deprecated alias for method_prefer (backward compat).
	if len(tool.MethodOrder) > 0 {
		toolList := ExpandBuckets(tool.MethodOrder)
		if needsExpand {
			expDefault := ExpandMethodOrder(defaultOrder, nativeManagerName)
			expTool := ExpandMethodOrder(toolList, nativeManagerName)
			return MergeMethodOrder(expTool, expDefault)
		}
		return MergeMethodOrder(toolList, defaultOrder)
	}

	if needsExpand {
		return ExpandMethodOrder(defaultOrder, nativeManagerName)
	}
	return defaultOrder
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
// An unknown kind that appears as some tool's ONLY candidate is a hard
// error: the tool is unreachable. An unknown kind that appears alongside
// at least one known kind is skipped with a logged warning: the tool may
// still install via the known fallback. Defaults.MethodOrder entries that
// are unknown are always warned, never errored — they are a hint about
// preference, not a per-tool contract.
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
		knownCount := 0
		for _, mc := range tool.Methods {
			if _, ok := set[mc.Kind]; !ok {
				unknownKinds = append(unknownKinds, mc.Kind)
			} else {
				knownCount++
			}
		}
		if len(unknownKinds) > 0 {
			if knownCount == 0 {
				// All kinds unknown — hard error: tool is unreachable. List all.
				for _, uk := range unknownKinds {
					hardErrors = append(hardErrors, fmt.Sprintf(
						"unknown method kind %q for tool %q (no adapter is registered; hint: register the adapter in initAdapters or fix the typo)",
						uk, toolName,
					))
				}
			} else {
				// At least one known fallback — warn for each unknown kind.
				for _, uk := range unknownKinds {
					warnings = append(warnings, fmt.Sprintf(
						"warning: tool %q declares unknown method kind %q (will be skipped at runtime; keeps known fallbacks)",
						toolName, uk,
					))
				}
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

	// Validate per-tool method_order entries.
	for _, toolName := range names {
		tool := s.Tools[toolName]

		// Validate all three method-ordering fields for unknown kinds.
		checkOrderSlice := func(slice []string, fieldName string) {
			for _, kind := range slice {
				if native.IsNativeManagerName(kind) {
					continue
				}
				if _, isBucket := DefaultBuckets[kind]; isBucket {
					continue
				}
				if _, ok := set[kind]; !ok {
					hardErrors = append(hardErrors, fmt.Sprintf(
						"tool %q: %s entry %q is not a registered method kind",
						toolName, fieldName, kind,
					))
				}
			}
		}

		if len(tool.MethodOrder) > 0 {
			checkOrderSlice(tool.MethodOrder, "method_order")
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
