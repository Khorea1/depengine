package config

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/platform"
)

// PlaceholderRe matches `{name}` tokens used throughout schema.toml string
// fields. Names are restricted to lowercase ascii letters, digits and
// underscore so `{pkg}` (handled by native managers) and `{latest}` (handled
// by the http/git adapters) keep working unchanged — those substitutions run
// later, on already-fact-expanded strings, and we never touch their tokens
// because they aren't present in the Facts map.
//
// Extensibility: any new host-fact placeholder only needs to be represented
// by platform.Facts and added to BuildMap below — every schema string field
// gets expanded for free, no parser change required.
var PlaceholderRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// Expand replaces every `{name}` placeholder in s with the corresponding value
// from m. Unknown placeholders are left untouched by design: a typo like
// `{archh}` should surface during validation rather than silently empty a
// field. `{pkg}` and `{latest}` are never in m, so they pass through
// unchanged for the downstream native/http stages that own them.
//
// Expand is pure and allocation-free when there is nothing to replace.
func Expand(s string, m map[string]string) string {
	if !PlaceholderRe.MatchString(s) {
		return s
	}
	return PlaceholderRe.ReplaceAllStringFunc(s, func(tok string) string {
		// tok includes the braces; strip them for the lookup
		key := tok[1 : len(tok)-1]
		if v, ok := m[key]; ok {
			return v
		}
		return tok // unknown -> leave as-is, validator will flag
	})
}

// BuildMap produces the substitution table fed to Expand. It is the single
// source of truth for which platform.Facts fields are exposed as placeholders.
//
// The clan (resolved distro family) is included as {distro_family} so
// `when = { distro_family = [...] }` style values and any URL/pkg field can
// reference it. Adding a new placeholder later means extending platform.Facts
// when needed and adding one line here.
func BuildMap(f *platform.Facts, clan string) map[string]string {
	m := map[string]string{
		"id":             f.DistroID,
		"distro_name":    f.DistroName,
		"distro_version": f.DistroVersion,
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

// KnownPlaceholders returns every {name} token that may appear in schema.toml
// without being flagged by validation. It derives the set from BuildMap (the
// platform.Facts surface plus adapter-owned tokens that pass through
// Expand untouched.
//
// Deriving from BuildMap ensures that adding a new Fact field automatically
// extends the known-placeholder set — the validate package never duplicates
// this list.
func KnownPlaceholders() []string {
	m := BuildMap(&platform.Facts{}, "")
	out := make([]string, 0, len(m)+5)
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	// "pkg" and "latest" are adapter-owned (native, and git/http
	// respectively); "version"/"arch_any"/"os_any" are owned by repo+asset
	// resolution — see ghrelease.ResolveAssetURL. All five are
	// deliberately left unexpanded by BuildMap/Expand (no entry in the map
	// this function derives from) so their owning adapter sees the literal
	// token, not a single pre-picked value.
	out = append(out, "pkg", "latest", "version", "arch_any", "os_any")
	return out
}

// normalizeAliasMap converts a raw arch_map/os_map inline table (as decoded
// by go-toml into map[string]any) into a map[string]string for use with
// resolveAlias. Keys are lowercased — they are matched against
// platform.Facts values case-insensitively, exactly like
// ghrelease.synonymGroup's strings.ToLower(value) lookup. Values are left
// byte-for-byte as written: they are opaque strings a schema author chose
// to match an upstream naming convention (release URLs are frequently
// case-sensitive), so they must never be case-normalized. Returns nil if
// raw isn't a non-empty table of string values (including when the key
// was absent entirely), so callers can treat a nil map as "no override at
// this layer" without a separate presence check.
func normalizeAliasMap(raw any) map[string]string {
	rm, ok := raw.(map[string]any)
	if !ok || len(rm) == 0 {
		return nil
	}
	out := make(map[string]string, len(rm))
	for k, v := range rm {
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[strings.ToLower(k)] = s
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// boolStr exposes booleans to placeholders with a stable "true"/"false"
// string form.
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
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = ExpandAll(val, m)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = ExpandAll(val, m)
		}
		return out
	default:
		return v
	}
}
