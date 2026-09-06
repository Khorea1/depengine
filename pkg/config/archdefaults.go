package config

import "strings"

// defaultArchMap and defaultOSMap are the engine's built-in fallback
// spelling tables for the {arch}/{os} placeholders. They are consulted by
// ParseSchema when expanding {arch}/{os} in a method's Config (url, pkg,
// build, git, ...), used whenever neither the method's own arch_map/os_map
// nor [defaults].arch_map/os_map has an entry for the current value.
//
// This is the http-method sibling of pkg/ghrelease's archSynonyms/
// osSynonyms (see assetmatch.go): same rationale — the engine's canonical
// spelling (uname/GOOS-style) doesn't always match what upstream release
// artifacts use — but 1:1 instead of 1:N. The http method builds a URL
// rather than matching a real asset list, so unlike github's {arch_any}/
// {os_any} (which tries every known synonym via regex), it can only ever
// pick one spelling. Keep the two tables' canonical keys in sync by hand;
// they document each other.
var defaultArchMap = map[string]string{
	"aarch64": "arm64",
}

var defaultOSMap = map[string]string{
	"darwin": "macos",
}

// resolveAlias returns the effective spelling for raw (a canonical value
// straight from engine.Facts, e.g. "aarch64" or "darwin"), consulting the
// layered alias maps in priority order: method-level, then schema
// [defaults]-level, then the engine builtin above. A layer with no entry
// for raw is skipped silently — same "unknown key -> literal fallback"
// behavior as ghrelease.synonymGroup, so a value none of the layers know
// about still resolves to itself instead of failing outright.
func resolveAlias(raw string, methodMap, defaultsMap, builtinMap map[string]string) string {
	key := strings.ToLower(raw)
	if v, ok := methodMap[key]; ok {
		return v
	}
	if v, ok := defaultsMap[key]; ok {
		return v
	}
	if v, ok := builtinMap[key]; ok {
		return v
	}
	return raw
}
