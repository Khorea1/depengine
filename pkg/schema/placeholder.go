package schema

import (
	"regexp"

	"depengine/pkg/engine"
)

// placeholderRe matches `{name}` tokens used throughout schema.toml string
// fields. Names are restricted to lowercase ascii letters, digits and
// underscore so `{pkg}` (handled by native managers) and `{latest}` (handled
// by the http/git adapters) keep working unchanged — those substitutions run
// later, on already-fact-expanded strings, and we never touch their tokens
// because they aren't present in the Facts map.
//
// Extensibility: any new key detect_os.sh emits in the future only needs to
// be added to Facts (facts.go) and to BuildMap below — every schema string
// field gets expanded for free, no parser change required.
var placeholderRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// Expand replaces every `{name}` placeholder in s with the corresponding value
// from m. Unknown placeholders are left untouched by design: a typo like
// `{archh}` should surface during validation rather than silently empty a
// field. `{pkg}` and `{latest}` are never in m, so they pass through
// unchanged for the downstream native/http stages that own them.
//
// Expand is pure and allocation-free when there is nothing to replace.
func Expand(s string, m map[string]string) string {
	if !placeholderRe.MatchString(s) {
		return s
	}
	return placeholderRe.ReplaceAllStringFunc(s, func(tok string) string {
		// tok includes the braces; strip them for the lookup
		key := tok[1 : len(tok)-1]
		if v, ok := m[key]; ok {
			return v
		}
		return tok // unknown -> leave as-is, validator will flag
	})
}

// BuildMap produces the substitution table fed to Expand. It is the single
// source of truth for which detect_os.sh Facts are exposed as placeholders.
//
// The clan (resolved distro family) is included as {distro_family} so
// `when = { distro_family = [...] }` style values and any URL/pkg field can
// reference it. Adding a new placeholder later means: (1) extend Facts in
// facts.go, (2) emit it from detect_os.sh, (3) add one line here. That's it.
func BuildMap(f *engine.Facts, clan string) map[string]string {
	m := map[string]string{
		"id":             f.DistroID,
		"distro_name":    f.DistroName,
		"distro_id_like": f.DistroIDLike,
		"distro_family":  clan,
		"target_family":  f.TargetFamily,
		"arch":           f.TargetArch,
		"detection":      f.DetectionMethod,
		"confidence":     f.Confidence,
		"kernel":         f.Kernel,
		"libc":           f.Libc,
		"init_system":    f.InitSystem,
		"os":             f.OS,
	}
	// Booleans render as their string form for URL/template ergonomics.
	m["is_wsl"] = boolStr(f.IsWSL)
	m["is_container"] = boolStr(f.IsContainer)
	m["is_android"] = boolStr(f.IsAndroid)
	return m
}

// boolStr mirrors detect_os.sh's bool_str so JSON booleans expose a stable
// "true"/"false" string form to placeholders.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ExpandAll recursively expands every string value in an arbitrary map. It
// walks nested maps and slices so that a TOML inline table like
// `foo = { url = "https://{arch}/x" }` is fully walked. Non-string leaves
// are returned unchanged. This is the entry point callers use after parsing
// schema.toml: pass them BuildMap(facts, clan) and every field is substituted
// before the engine resolves tools/methods.
//
// Keeping this generic (rather than enumerating fields) is what makes the new
// system extensible: adding a field to schema.toml requires zero parser
// changes — everything that is a string gets expanded, everything else is
// left alone.
func ExpandAll(v any, m map[string]string) any {
	switch t := v.(type) {
	case string:
		return Expand(t, m)
	case map[string]any:
		for k, val := range t {
			t[k] = ExpandAll(val, m)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = ExpandAll(val, m)
		}
		return t
	default:
		return v
	}
}
