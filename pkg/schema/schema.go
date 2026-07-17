package schema

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"depengine/pkg/native"

	"depengine/pkg/log"
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
	IsSimple    bool
	Tags        []string // profile tags for --profile filtering (e.g. "desktop", "server")
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
// get replaced. Pass an empty-ish map for tooling that needs the raw text
// (validation can flag unknown placeholders via Expand leaving them in place).
//
// Behavior notes:
//   - placeholder expansion runs AFTER TOML decoding and BEFORE ordering,
//     so method_order, when.distro_family and every pkg/url/build field get
//     the same treatment uniformly.
//   - placeholders unknown to m are left untouched; validation layer is
//     responsible for flagging them, not the parser.
//   - the `simple` list is processed first; an inline table redeclaring a
//     simple tool is an error (TODO.md accepts this as E_DUPE_TOOL).
func ParseSchema(path string, m map[string]string) (*Schema, error) {
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

	// Extract rawTools from expanded raw (ExpandAll returns a new map).
	rawTools, _ := raw["tools"].(map[string]any)
	if rawTools == nil {
		rawTools = map[string]any{}
	}

	defaults := extractDefaults(raw["defaults"])

	tools, err := normalizeTools(rawTools, defaults)
	if err != nil {
		return nil, &ParseSchemaError{Err: err}
	}

	return &Schema{Defaults: defaults, Tools: tools}, nil
}

// ParseSchemaNoFacts loads and normalizes a schema.toml without gathering
// distro facts. Placeholders are left unexpanded (passed an empty map).
// Use this for read-only operations (check, validate) that don't need
// the ~50-100ms overhead of running detect_os.sh.
func ParseSchemaNoFacts(path string) (*Schema, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseSchemaError{Err: fmt.Errorf("reading schema %s: %w", path, err)}
	}

	var raw map[string]any
	if err := toml.Unmarshal(rawBytes, &raw); err != nil {
		return nil, &ParseSchemaError{Err: fmt.Errorf("parse TOML %s: %w", path, err)}
	}
	// Expand with empty map — no placeholders will be substituted.
	raw = ExpandAll(raw, map[string]string{}).(map[string]any)
	rawTools, _ := raw["tools"].(map[string]any)
	if rawTools == nil {
		rawTools = map[string]any{}
	}
	defaults := extractDefaults(raw["defaults"])
	tools, err := normalizeTools(rawTools, defaults)
	if err != nil {
		return nil, &ParseSchemaError{Err: err}
	}
	return &Schema{Defaults: defaults, Tools: tools}, nil
}
func extractDefaults(raw any) Defaults {
	d := Defaults{
		Manager:   "native",
		AurHelper: "paru",
		MethodOrder: []string{
			"native", "cargo", "go", "pipx", "uv", "pip",
			"npm", "pnpm", "bun", "gem", "yarn", "yarn-berry",
			"composer", "apm", "vscode", "vscodium", "flatpak",
			"snap", "cask", "mas", "sdkman", "steamcmd",
			"pacstall", "aur", "conda", "asdf", "git", "http",
		},
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
			d.MethodOrder = order
		}
	}
	return d
}

func normalizeTools(rawTools map[string]any, defaults Defaults) (map[string]*Tool, error) {
	tools := map[string]*Tool{}

	// 1. Process `simple` list first.
	if sim, ok := rawTools["simple"].([]any); ok {
		for _, item := range sim {
			name, ok := item.(string)
			if !ok {
				continue
			}
			if _, dup := tools[name]; dup {
				return nil, fmt.Errorf("tool %q redeclarada (lista simple)", name)
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
			return nil, fmt.Errorf("tool %q redeclarada (simple + tabela)", name)
		}
		tool := &Tool{Name: name}
		valMap, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool %q: esperava tabela, got %T", name, val)
		}

		if r, ok := valMap["requires"].([]any); ok {
			tool.Requires = anySliceToStrings(r)
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

		methods := buildMethods(name, valMap)
		tool.Methods = orderByMethodOrder(methods, defaults.MethodOrder)
		tools[name] = tool
	}
	return tools, nil
}

func parseMethod(kind string, val any) (*MethodCandidate, error) {
	mc := &MethodCandidate{Kind: kind, Config: map[string]any{}}

	switch t := val.(type) {
	case string:
		// inline scalar: `apt = "fd-find"` → pkg
		mc.Config["pkg"] = t
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
	return mc, nil
}

// buildMethods processes the keys of an inline-table tool declaration and
// collapses native-manager overrides (CASO 2) into a single "native" method
// with a pkg_overrides map, while keeping non-native keys as separate methods.
//
// When a tool declares only non-native methods (go, cargo, pip, etc.) without
// any native manager overrides, a "native" method is automatically injected
// with the tool name as the default package name. This ensures method_order
// (where "native" comes first) is respected even when the schema only mentions
// language-specific installers.
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

	for _, k := range sortedKeys(valMap, "requires", "pre_install", "preinstall", "post_install", "postinstall", "tags") {
		if _, isStr := valMap[k].(string); isStr && native.IsNativeManagerName(k) {
			nativeOverrides[k] = valMap[k]
		} else {
			nonNativeKeys = append(nonNativeKeys, k)
		}
	}

	// Always inject a native method when there are any relevant keys.
	// With overrides if native manager names are present, plain otherwise.
	if len(nativeOverrides) > 0 || len(nonNativeKeys) > 0 {
		cfg := map[string]any{"pkg": name}
		if len(nativeOverrides) > 0 {
			cfg["pkg_overrides"] = nativeOverrides
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
			if df, ok := v.([]any); ok {
				cond.DistroFamily = anySliceToStrings(df)
			}
		default:
			invalidKeys = append(invalidKeys, k)
		}
	}
	if len(invalidKeys) > 0 {
		// Logged at debug level — unknown keys in `when` are not an error
		// (they may be from future schema versions).
		log.Default.Debug("parseCondition: ignoring unknown keys", "keys", invalidKeys)
	}
	if len(cond.DistroFamily) == 0 {
		return nil
	}
	return cond
}
func orderByMethodOrder(methods []*MethodCandidate, order []string) []*MethodCandidate {
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

// Validate checks a parsed Schema for references to method kinds that no
// adapter will ever satisfy. Unknown kinds in Defaults.MethodOrder or any
// MethodCandidate.Kind cause a tool whose only candidates are unknown to
// be silently skipped by the executor at runtime — Validate surfaces that
// at parse time instead. knownKinds is typically exec.RegisteredKinds();
// it is a parameter rather than an import so pkg/schema stays free of a
// circular dependency on pkg/exec.
//
// An unknown kind that appears as some tool's ONLY candidate is a hard
// error: the tool is unreachable. An unknown kind that appears alongside
// at least one known kind is skipped with a logged warning: the tool may
// still install via the known fallback. Defaults.MethodOrder entries that
// are unknown are always warned, never errored — they are a hint about
// preference, not a per-tool contract.
func Validate(s *Schema, knownKinds []string) (error, []string) {
	set := make(map[string]struct{}, len(knownKinds))
	for _, k := range knownKinds {
		set[k] = struct{}{}
	}

	var hardErrors []string
	var warnings []string

	// Check Defaults.MethodOrder entries first.
	for _, kind := range s.Defaults.MethodOrder {
		if _, ok := set[kind]; !ok {
			warnings = append(warnings, fmt.Sprintf(
				"warning: defaults.method_order lists unknown kind %q (no adapter registered for this name)",
				kind,
			))
		}
	}
	// Check each tool's method candidates. Sort tool names for deterministic output.
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, toolName := range names {
		tool := s.Tools[toolName]
		// Zero-method tools are unreachable regardless of known kinds.
		if len(tool.Methods) == 0 {
			hardErrors = append(hardErrors, fmt.Sprintf(
				"tool %q has no methods declared — no adapter can install it",
				toolName,
			))
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
		if len(unknownKinds) == 0 {
			continue
		}
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

	if len(hardErrors) > 0 {
		return &ParseSchemaError{Err: errors.New(strings.Join(hardErrors, "\n"))}, warnings
	}
	return nil, warnings
}
