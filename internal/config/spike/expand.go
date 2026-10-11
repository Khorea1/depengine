package spike

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Allowlisted per-case override keys. Everything else in a case (besides
// label/when) is rejected: repo, checksum, scope, credentials, and
// independent strategies keep deserving their own explicit candidates.
var allowedCaseOverrides = map[string]bool{
	"asset":            true,
	"entrypoints":      true,
	"binary":           true,
	"strip_components": true,
	"extract_to":       true,
}

// caseRef is one emitted case for overlap reporting.
type caseRef struct {
	label string
	when  map[string]any
}

// ExpandCompact reads a compact base+cases document (testdata/c_*.toml) and
// returns conventional schema/manifest TOML with one
// [tools|packages.<tool>.<label>] candidate per case (kind="github").
// It understands both [tools] (project schemas) and [packages] (personal
// manifests) and preserves every declaration that does not participate in
// the expansion — including simple lists (simple = [...]) and non-github
// candidates. Expansion is total (all cases, every host) and order-preserving.
func ExpandCompact(data []byte) (expanded []byte, overlapNotes []string, err error) {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("spike: compact TOML decode: %w", err)
	}
	out := map[string]any{}
	for _, section := range []string{"tools", "packages"} {
		raw, ok := doc[section]
		if !ok {
			continue
		}
		sectionMap, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("spike: [%s] is not a table", section)
		}
		expandedSection, notes, err := expandSection(section, sectionMap)
		if err != nil {
			return nil, nil, err
		}
		overlapNotes = append(overlapNotes, notes...)
		out[section] = expandedSection
	}
	for k, v := range doc {
		if k != "tools" && k != "packages" {
			out[k] = v
		}
	}
	buf, err := toml.Marshal(out)
	if err != nil {
		return nil, nil, fmt.Errorf("spike: marshal expanded document: %w", err)
	}
	return buf, overlapNotes, nil
}

func expandSection(section string, sectionMap map[string]any) (map[string]any, []string, error) {
	var notes []string
	out := map[string]any{}
	for _, toolName := range sortedMapKeys(sectionMap) {
		toolRaw, ok := sectionMap[toolName].(map[string]any)
		if !ok {
			// Simple lists (simple = [...]), strings, and any other
			// non-table declaration pass through verbatim.
			out[toolName] = sectionMap[toolName]
			continue
		}
		expandedTool, toolNotes, err := expandTool(section, toolName, toolRaw)
		if err != nil {
			return nil, nil, err
		}
		notes = append(notes, toolNotes...)
		out[toolName] = expandedTool
	}
	return out, notes, nil
}

// expandTool expands one tool table. Tool-level keys (method_only,
// method_prefer, requires, post_install, ...) pass through untouched; the
// single github base table carrying a cases array becomes N labeled
// kind="github" candidates. Tables without cases pass through untouched.
func expandTool(section, toolName string, toolRaw map[string]any) (map[string]any, []string, error) {
	var notes []string
	out := map[string]any{}
	var baseName string
	var base map[string]any
	for _, k := range sortedMapKeys(toolRaw) {
		m, ok := toolRaw[k].(map[string]any)
		if !ok {
			out[k] = toolRaw[k]
			continue
		}
		if _, hasCases := m["cases"]; hasCases {
			if base != nil {
				return nil, nil, fmt.Errorf("spike: [%s.%s] has more than one cases base (%q and %q)", section, toolName, baseName, k)
			}
			baseName, base = k, m
			continue
		}
		out[k] = toolRaw[k]
	}
	if base == nil {
		return toolRaw, nil, nil // no cases here; pass through
	}
	// The macro is github-only: any other table carrying cases is rejected
	// instead of being reinterpreted as a github recipe.
	kind, _ := base["kind"].(string)
	if baseName != "github" && kind != "github" {
		return nil, nil, fmt.Errorf("spike: [%s.%s.%s] carries cases but is not a github method (name %q, kind %q): cases are github-only", section, toolName, baseName, baseName, kind)
	}
	casesRaw, ok := base["cases"].([]any)
	if !ok || len(casesRaw) == 0 {
		return nil, nil, fmt.Errorf("spike: [%s.%s.%s] cases must be a non-empty array", section, toolName, baseName)
	}
	baseFields := map[string]any{}
	for k, v := range base {
		if k == "cases" {
			continue
		}
		if k == "when" || k == "label" || k == "kind" {
			return nil, nil, fmt.Errorf("spike: [%s.%s.%s] base must not carry %q", section, toolName, baseName, k)
		}
		baseFields[k] = v
	}
	// Every other declared key in this tool is reserved (explicit
	// candidates and tool-level keys alike): a case must never silently
	// replace an existing declaration. Case-vs-case duplicates get their
	// own error so tests can tell the two apart.
	declared := map[string]bool{}
	for k := range toolRaw {
		if k == baseName {
			continue
		}
		declared[k] = true
	}
	seenCases := map[string]bool{}
	var emittedCases []caseRef
	for i, c := range casesRaw {
		cm, ok := c.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("spike: [%s.%s.%s] cases[%d] must be a table", section, toolName, baseName, i)
		}
		label, _ := cm["label"].(string)
		if label == "" {
			return nil, nil, fmt.Errorf("spike: [%s.%s.%s] cases[%d] missing explicit label", section, toolName, baseName, i)
		}
		if declared[label] {
			return nil, nil, fmt.Errorf("spike: [%s.%s.%s] case label %q collides with an explicitly declared method; rename the case or the method", section, toolName, baseName, label)
		}
		if seenCases[label] {
			return nil, nil, fmt.Errorf("spike: [%s.%s.%s] duplicate case label %q", section, toolName, baseName, label)
		}
		seenCases[label] = true
		whenRaw, ok := cm["when"].(map[string]any)
		if !ok || len(whenRaw) == 0 {
			return nil, nil, fmt.Errorf("spike: [%s.%s.%s] case %q missing non-empty when table", section, toolName, baseName, label)
		}
		cand := map[string]any{"kind": "github"}
		for k, v := range baseFields {
			cand[k] = deepCopy(v)
		}
		for _, k := range sortedMapKeys(cm) {
			if k == "label" || k == "when" {
				continue
			}
			if !allowedCaseOverrides[k] {
				return nil, nil, fmt.Errorf("spike: [%s.%s.%s] case %q forbids override %q (allowlist: asset, entrypoints, binary, strip_components, extract_to)", section, toolName, baseName, label, k)
			}
			cand[k] = deepCopy(cm[k])
		}
		cand["when"] = deepCopy(whenRaw)
		out[label] = cand
		emittedCases = append(emittedCases, caseRef{label: label, when: whenRaw})
	}
	notes = reportOverlapList(toolName, emittedCases)
	return out, notes, nil
}

// reportOverlapList conservatively flags case pairs whose os/arch/libc/
// is_android selectors intersect. Other when dimensions are out of scope for
// the spike check and are reported as-is; overlap here is informational —
// the expanded candidates keep normal fallback semantics, no silent change.
func reportOverlapList(toolName string, cases []caseRef) []string {
	var notes []string
	sets := func(w map[string]any, key string) (map[string]bool, bool) {
		raw, ok := w[key]
		if !ok {
			return nil, false // unconstrained on this dimension
		}
		arr, ok := raw.([]any)
		if !ok {
			return nil, false
		}
		s := map[string]bool{}
		for _, v := range arr {
			if str, ok := v.(string); ok {
				s[strings.ToLower(str)] = true
			}
		}
		return s, true
	}
	boolVal := func(w map[string]any, key string) (bool, bool) {
		raw, ok := w[key]
		if !ok {
			return false, false
		}
		b, ok := raw.(bool)
		return b, ok
	}
	intersects := func(a, b map[string]bool, aSet, bSet bool) bool {
		if !aSet || !bSet {
			return true // unconstrained dimension cannot rule overlap out
		}
		for k := range a {
			if b[k] {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(cases); i++ {
		for j := i + 1; j < len(cases); j++ {
			a, b := cases[i].when, cases[j].when
			aOS, aOSSet := sets(a, "os")
			bOS, bOSSet := sets(b, "os")
			aArch, aArchSet := sets(a, "arch")
			bArch, bArchSet := sets(b, "arch")
			aLibc, aLibcSet := sets(a, "libc")
			bLibc, bLibcSet := sets(b, "libc")
			aAnd, aAndSet := boolVal(a, "is_android")
			bAnd, bAndSet := boolVal(b, "is_android")
			androidOK := true
			if aAndSet && bAndSet {
				androidOK = aAnd == bAnd
			}
			if intersects(aOS, bOS, aOSSet, bOSSet) &&
				intersects(aArch, bArch, aArchSet, bArchSet) &&
				intersects(aLibc, bLibc, aLibcSet, bLibcSet) &&
				androidOK {
				extra := ""
				if hasOtherDims(a) || hasOtherDims(b) {
					extra = " (outras dimensões when fora do escopo do cheque; verificação conservadora)"
				}
				notes = append(notes, fmt.Sprintf("spike: %s cases %q × %q potencialmente sobrepostos em os/arch/libc/is_android%s — fallback normal preservado, sem garantia nova de exclusividade", toolName, cases[i].label, cases[j].label, extra))
			}
		}
	}
	return notes
}

func hasOtherDims(w map[string]any) bool {
	for k := range w {
		switch k {
		case "os", "arch", "libc", "is_android":
		default:
			return true
		}
	}
	return false
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopy(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopy(val)
		}
		return out
	default:
		return v
	}
}
