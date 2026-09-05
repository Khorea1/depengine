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

// Schema is the fully-normalized in-memory form of schema.toml after parsing.
// It is the engine's working set: defaults + a flat map of tools, each with
// its method candidates ordered by defaults.method_order.
type Schema struct {
	Defaults      Defaults
	Tools         map[string]*Tool
	AllowNewTools bool                     `json:"-"`
	Provenance    map[string][]FieldSource `json:"-"` // tool name → field sources
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
	Name        string `merge:"overwrite"`
	PreInstall  string `merge:"overwrite"` // shell command run before install; failure aborts install
	PostInstall string `merge:"overwrite"` // shell command run after successful install
	// PostInstallWhen gates PostInstall by platform facts. Set only by the
	// table form `postinstall = { cmd = "...", when = {...} }`; nil = always run.
	PostInstallWhen *Condition `merge:"overwrite"`
	// RequiresWhen gates individual Requires entries by platform facts:
	// `requires_when = { fontconfig = { target_family = ["unix"] } }`.
	// A dep with no entry always applies. Conditions are immutable post-parse.
	RequiresWhen map[string]*Condition `merge:"overwrite"`
	Requires     []string               `merge:"overwrite"`
	Methods      []*MethodCandidate     `merge:"methods"`
	MethodPrefer []string               `merge:"overwrite"` // prefix: try these first, then fall back to defaults
	MethodOnly   []string               `merge:"overwrite"` // exclusive: use ONLY these methods, in this order
	IsSimple     bool                   `merge:"overwrite"`
	Tags         []string               `merge:"union"` // profile tags for --profile filtering (e.g. "desktop", "server")
	Ecosystem    string                 `merge:"overwrite"` // "python", "node", etc — empty if not from bucket
}

// cloneTool returns a deep copy of t.
// EffectiveRequires returns the deps of t that apply under the given facts:
// FilteredTools clones tools with Requires reduced to what applies under
// facts — pass it to graph.Sort and blockedBy checks so a unix-only dep
// (e.g. fontconfig for a font on Windows) never blocks installation.
func FilteredTools(tools map[string]*Tool, facts *engine.Facts) map[string]*Tool {
	if tools == nil || facts == nil {
		return tools
	}
	out := make(map[string]*Tool, len(tools))
	for name, t := range tools {
		if len(t.RequiresWhen) == 0 {
			out[name] = t
			continue
		}
		c := cloneTool(t)
		c.Requires = t.EffectiveRequires(facts)
		out[name] = c
	}
	return out
}

// entries gated by RequiresWhen only match when their condition is met.
// Facts nil means no filtering (match everything).
func (t *Tool) EffectiveRequires(facts *engine.Facts) []string {
	if facts == nil || len(t.RequiresWhen) == 0 {
		return t.Requires
	}
	out := make([]string, 0, len(t.Requires))
	for _, dep := range t.Requires {
		if c, gated := t.RequiresWhen[dep]; gated && !c.Match(facts) {
			continue
		}
		out = append(out, dep)
	}
	return out
}

func cloneTool(t *Tool) *Tool {
	if t == nil {
		return nil
	}
	out := *t
	out.Requires = append([]string{}, t.Requires...)
	if t.RequiresWhen != nil {
		out.RequiresWhen = make(map[string]*Condition, len(t.RequiresWhen))
		for k, v := range t.RequiresWhen {
			out.RequiresWhen[k] = v
		}
	}
	out.Tags = append([]string{}, t.Tags...)
	out.MethodPrefer = append([]string{}, t.MethodPrefer...)
	out.MethodOnly = append([]string{}, t.MethodOnly...)
	out.Methods = cloneMethods(t.Methods)
	return &out
}

// cloneMethods returns a deep copy of a MethodCandidate slice.
func cloneMethods(methods []*MethodCandidate) []*MethodCandidate {
	out := make([]*MethodCandidate, len(methods))
	for i, m := range methods {
		out[i] = cloneMethod(m)
	}
	return out
}

// cloneMethod returns a deep copy of m.
func cloneMethod(m *MethodCandidate) *MethodCandidate {
	if m == nil {
		return nil
	}
	out := *m
	out.Config = make(map[string]any, len(m.Config))
	for k, v := range m.Config {
		out.Config[k] = v // shallow copy; map values are not deeply cloned
	}
	if m.When != nil {
		w := *m.When
		out.When = &w
	}
	return &out
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
	Kind   string // adapter dispatch key (e.g. "http")
	Label  string // TOML section key (e.g. "http-musl"), empty = use Kind
	When   *Condition
	Config map[string]any
	Err    error
}

// Condition is the parsed form of `when = { ... }`. All fields (distro_family,
// distro_id, arch, os, kernel, libc, init_system, is_wsl, is_container, target_family) are
// honored by Match().
type Condition struct {
	DistroFamily []string `cfg:"distro_family"`
	TargetFamily []string `cfg:"target_family"`
	DistroID     []string `cfg:"distro_id"`
	Arch         []string `cfg:"arch"`
	OS           []string `cfg:"os"`
	Kernel       []string `cfg:"kernel"`
	Libc         []string `cfg:"libc"`
	InitSystem   []string `cfg:"init_system"`
	IsWSL        *bool    `cfg:"is_wsl"`
	IsContainer  *bool    `cfg:"is_container"`
}

func (c *Condition) IsZero() bool {
	return len(c.DistroFamily) == 0 &&
		len(c.TargetFamily) == 0 &&
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
	if len(c.TargetFamily) > 0 && !matchExact(c.TargetFamily, facts.TargetFamily) {
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

// ErrorCode is a stable identifier for a class of validation or schema error.
// This duplicates pkg/validate.ErrorCode to avoid an import cycle.
type ErrorCode string

// ParseSchemaError is returned by ParseSchema when the problem is in the
// schema file itself (invalid TOML, validation errors, redeclared tools, etc.),
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
//     simple tool is an error (SchemaCodeError with Code "E_DUPE_TOOL").
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

	// The "github" adapter needs the machine's raw arch/os facts (uname-style
	// "x86_64", GOOS-style "linux"/"darwin"/...) to resolve {arch_any}/{os_any}
	// against the real release-asset list at install time. Those two tokens
	// are deliberately NOT expanded by ExpandAll above (they aren't in m's
	// key set, so Expand leaves them untouched) — the adapter needs the raw
	// facts, not a single pre-picked spelling, to try every known synonym.
	// Adapters have no other way to reach engine.Facts, so we stash the two
	// values it needs directly on its own method candidates here, the one
	// place ParseSchema still has both `tools` and `m` in scope.
	for _, tool := range tools {
		for _, mc := range tool.Methods {
			if mc.Kind == "github" {
				mc.Config["_current_arch"] = m["arch"]
				mc.Config["_current_os"] = m["os"]
			}
		}
	}

	return &Schema{Defaults: defaults, Tools: tools, AllowNewTools: allowNewTools}, nil
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
		} else if pi, ok := valMap["preinstall"].(string); ok {
			tool.PreInstall = pi
		}
		// post_install/postinstall accept a plain string (unconditional) or
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
		} else if v, ok := valMap["postinstall"]; ok {
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
// tool — UNLESS the tool declares method_only (an exclusive list) and native
// is not part of it. method_only filters the candidate set, not just the
// order: a tool restricted to http/go must never fall back to the native
// manager (which may require elevation). method_prefer is a prefix that
// still allows the native remainder, so it does not suppress the implicit
// native candidate.
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

	for _, k := range sortedKeys(valMap, "requires", "requires_when", "pre_install", "preinstall", "post_install", "postinstall", "tags", "method_prefer", "method_only", "when", "kind") {
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

	// method_only is an EXCLUSIVE list ("use ONLY these methods"): the
	// implicit native candidate must not be injected when native is not part
	// of the declared list. See methodOnlyAllowsNative for the match rules.
	injectNative := true
	if only, ok := valMap["method_only"].([]any); ok {
		if onlyList := anySliceToStrings(only); len(onlyList) > 0 {
			injectNative = methodOnlyAllowsNative(onlyList)
			if !injectNative && (len(nativeOverrides) > 0 || nativeBlockConfig != nil) {
				log.Default.Warn(fmt.Sprintf("tool %q: method_only %v excludes native; dropping the native manager method", name, onlyList))
			}
		}
	}

	// Inject a native method when there are any relevant keys.
	// With overrides if native manager names are present, plain otherwise.
	if injectNative && (len(nativeOverrides) > 0 || len(nonNativeKeys) > 0 || nativeBlockConfig != nil) {
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

// methodOnlyAllowsNative reports whether an exclusive method_only list
// permits the implicit native method. Native is allowed when "native"
// itself is listed, or when a native manager name (apt, pacman, …) is
// listed — ExpandMethodOrder rewrites a matching manager name to "native"
// at runtime, so the candidate must exist for the order to be satisfiable.
// Bucket names (python, node) never expand to native.
func methodOnlyAllowsNative(only []string) bool {
	for _, k := range ExpandBuckets(only) {
		if k == "native" || native.IsNativeManagerName(k) {
			return true
		}
	}
	return false
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
	if invalidKeys := decodeStructFields(cond, rm); len(invalidKeys) > 0 {
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
// or defaultOrder (fallback).
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
			if knownCount == 0 {
				// All kinds unknown — hard error: tool is unreachable.
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
			} else {
				// At least one known fallback — warn for each unknown kind.
				for _, uk := range unknownKinds {
					msg := fmt.Sprintf(
						"warning: tool %q declares method kind %q which is not a registered adapter (will be skipped at runtime) — if this is a variant of an existing method, add kind = \"<kind>\" to the method block",
						toolName, uk,
					)
					if hint := prefixHints[uk]; hint != "" {
						msg += hint
					}
					warnings = append(warnings, msg)
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

	// Validate per-tool method preference entries (method_prefer, method_only).
	for _, toolName := range names {
		tool := s.Tools[toolName]

		// Validate both method-ordering fields for unknown kinds.
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
