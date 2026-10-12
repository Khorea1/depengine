package ghrelease

import (
	"fmt"
	"regexp"
	"strings"
)

// archSynonyms maps a canonical arch value (as reported by uname -m / the
// engine's {arch} placeholder) to every spelling GitHub release assets are
// commonly published under. This is the piece that a single {arch}
// placeholder can't provide: {arch} always expands to exactly one string
// (the machine's own uname value), but upstream projects each pick their
// own asset-naming convention independently, so a schema author matching
// against real assets needs the *set* of spellings, not one fixed string.
var archSynonyms = map[string][]string{
	"x86_64":  {"x86_64", "amd64", "x64"},
	"aarch64": {"aarch64", "arm64"},
	"armv7l":  {"armv7l", "armv7", "armhf", "arm"},
	"i686":    {"i686", "i386", "x86", "386"},
}

// osSynonyms maps a canonical OS value (GOOS-style, as exposed by the
// engine's {os} placeholder) to spellings seen in release asset names.
var osSynonyms = map[string][]string{
	"darwin":  {"darwin", "macos", "osx", "mac"},
	"windows": {"windows", "win"},
	"linux":   {"linux"},
}

// synonymGroup returns a non-capturing regex alternation for every known
// spelling of value in table, falling back to just value itself (escaped)
// when the table has no entry — so an unrecognized arch/OS still matches
// literally instead of failing outright.
func synonymGroup(table map[string][]string, value string) string {
	syns, ok := table[strings.ToLower(value)]
	if !ok || len(syns) == 0 {
		syns = []string{value}
	}
	escaped := make([]string, len(syns))
	for i, s := range syns {
		escaped[i] = regexp.QuoteMeta(s)
	}
	return "(?:" + strings.Join(escaped, "|") + ")"
}

// CompileLiteralPattern compiles a filename template (the plain string
// AssetSelector form) into an anchored, case-insensitive regexp. Exported
// alongside CompileExplicitRegex so resolution can be tested against
// in-memory release catalogs without GitHub API access.
func CompileLiteralPattern(assetPattern, tag, arch, osName string) (*regexp.Regexp, error) {
	return assetPatternRegexp(assetPattern, tag, arch, osName)
}

// CompileExplicitRegex compiles an explicit asset regex (the { regex = "..." }
// AssetSelector form) into an anchored, case-insensitive regexp matched
// against real release asset names. Unlike assetPatternRegexp, the expression
// itself is regex: only the three known placeholders are substituted (with
// the same escaped groups), and every other character passes through raw, so
// regex operators, character classes, and quantifiers keep working. Unknown
// {...} tokens are left untouched.
//
// Anchoring wraps the whole expression in a non-capturing group:
// ^(?:EXPR)$. A bare "^" + expr + "$" would let a top-level alternation
// escape the anchors (^a|b$ matches "xa" via the second branch and "bx"
// via the first), silently turning an exact selector into a partial match.
func CompileExplicitRegex(expr, tag, arch, osName string) (*regexp.Regexp, error) {
	versionGroup := "(?:" + regexp.QuoteMeta(tag) + "|" + regexp.QuoteMeta(strings.TrimPrefix(tag, "v")) + ")"
	replacer := strings.NewReplacer(
		"{version}", versionGroup,
		"{arch_any}", synonymGroup(archSynonyms, arch),
		"{os_any}", synonymGroup(osSynonyms, osName),
	)
	pattern := "^(?:" + replacer.Replace(expr) + ")$"

	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid asset regex %q: %w", expr, err)
	}
	return re, nil
}

// MatchAssetNames returns the subset of names matched by re. Pure: no
// network, so resolution can be tested against in-memory release catalogs.
func MatchAssetNames(re *regexp.Regexp, names []string) []string {
	matched := make([]string, 0, 1)
	for _, name := range names {
		if re.MatchString(name) {
			matched = append(matched, name)
		}
	}
	return matched
}

// assetPatternRegexp compiles assetPattern (a filename template understood
// by ResolveAssetURL — see its doc comment for the placeholder list) into an
// anchored, case-insensitive regexp matched against real release asset
// names.
func assetPatternRegexp(assetPattern, tag, arch, osName string) (*regexp.Regexp, error) {
	// Escape everything first so literal filename characters (dots,
	// plus signs, etc.) are matched literally; QuoteMeta also escapes the
	// braces around our placeholders, e.g. "{arch_any}" -> `\{arch_any\}`,
	// so the literal replacements below target that escaped form.
	escaped := regexp.QuoteMeta(assetPattern)

	versionGroup := "(?:" + regexp.QuoteMeta(tag) + "|" + regexp.QuoteMeta(strings.TrimPrefix(tag, "v")) + ")"
	replacer := strings.NewReplacer(
		`\{version\}`, versionGroup,
		`\{arch_any\}`, synonymGroup(archSynonyms, arch),
		`\{os_any\}`, synonymGroup(osSynonyms, osName),
	)
	pattern := "^" + replacer.Replace(escaped) + "$"

	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid asset pattern %q: %w", assetPattern, err)
	}
	return re, nil
}
