package plan_test

import (
	"net/url"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

// projectPathRejection encodes mandatory rejection properties without
// requiring NormalizeProjectPath to accept every string not covered here.
// That keeps the fuzz oracle from freezing an incomplete allow-list while still
// catching regressions in root containment and cross-platform path safety.
func projectPathRejection(raw string) (string, bool) {
	if reason, rejected := projectPathShapeRejection(raw); rejected {
		return reason, true
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "project root escape", true
	}
	if reason, rejected := projectPathShapeRejection(clean); rejected {
		return reason + " after cleaning", true
	}
	if reason, rejected := projectPathComponentRejection(clean); rejected {
		return reason + " after cleaning", true
	}
	return "", false
}

func projectPathShapeRejection(value string) (string, bool) {
	switch {
	case value == "":
		return "missing path", true
	case strings.TrimSpace(value) != value:
		return "surrounding whitespace", true
	case strings.ContainsRune(value, '\x00'):
		return "NUL byte", true
	case strings.Contains(value, `\`):
		return "backslash separator", true
	case path.IsAbs(value):
		return "absolute path", true
	case hasWindowsDrivePrefix(value):
		return "windows volume prefix", true
	}
	return "", false
}

func hasWindowsDrivePrefix(raw string) bool {
	if len(raw) < 2 || raw[1] != ':' {
		return false
	}
	c := raw[0]
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func projectPathComponentRejection(value string) (string, bool) {
	for _, component := range strings.Split(value, "/") {
		for _, r := range component {
			if r < 0x20 || strings.ContainsRune(`<>"|?*:`, r) {
				return "non-portable path character", true
			}
		}
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return "windows-ambiguous trailing dot or space", true
		}
		base := component
		if i := strings.IndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		switch strings.ToUpper(base) {
		case "CON", "PRN", "AUX", "NUL", "CLOCK$",
			"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return "reserved windows device name", true
		}
	}
	return "", false
}

// FuzzNormalizeProjectPathStaysInsideProjectRoot pins the plan-time path
// guard for vendored artifacts: every accepted path must already be canonical,
// re-normalizing it must be a no-op, and resolving it under the project root
// must never escape, so lock identity and extraction targets cannot disagree
// about where a local artifact lives.
func FuzzNormalizeProjectPathStaysInsideProjectRoot(f *testing.F) {
	for _, seed := range []string{
		"vendor/tool",
		"vendor/./tool",
		"vendor/tool/",
		"a//b",
		"a/b/../c",
		"../tool",
		"..",
		".",
		"/tmp/tool",
		"//server/share",
		"C:/vendor/tool",
		"c:vendor/tool",
		"C:",
		`vendor\tool`,
		" vendor/tool",
		"vendor/tool ",
		"vendor/tool\x00",
		"vendor/\x00tool",
		"vendor/tool:stream",
		"vendor/CON",
		"vendor/COM1.txt",
		"vendor/a?b",
		"vendor/name.",
		"vendor/name ",
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 512 {
			return
		}
		reason, rejected := projectPathRejection(raw)
		got, err := plan.NormalizeProjectPath(raw)
		if rejected && err == nil {
			t.Fatalf("NormalizeProjectPath(%q) = %q, want rejection for %s", raw, got, reason)
		}
		if err != nil {
			return
		}
		if cleaned := path.Clean(got); cleaned != got {
			t.Fatalf("NormalizeProjectPath(%q) = %q is not canonical (path.Clean gives %q)", raw, got, cleaned)
		}
		if again, err := plan.NormalizeProjectPath(got); err != nil || again != got {
			t.Fatalf("NormalizeProjectPath(%q) = %q is not a fixed point (second pass %q, %v)", raw, got, again, err)
		}
		rooted := path.Join("/project-root", got)
		if !strings.HasPrefix(rooted, "/project-root/") {
			t.Fatalf("NormalizeProjectPath(%q) = %q escapes the project root (joined path %q)", raw, got, rooted)
		}
	})
}

// urlWithLiteralPassword reports whether raw carries userinfo password
// material. Only authority-form references (scheme://...) can expose userinfo,
// so scp-style git remotes never trip this oracle.
func urlWithLiteralPassword(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return false
	}
	_, hasPassword := u.User.Password()
	return hasPassword
}

// FuzzCanonicalSourcesIsOrderIndependentAndIdempotent pins the canonical
// projection that snapshots and lock generation depend on: the result must
// not depend on declaration order, must be stable under a second pass, must
// leave the caller's slice untouched, must never hand back a source that
// fails its own validation, and must keep refusing literal URL passwords.
func FuzzCanonicalSourcesIsOrderIndependentAndIdempotent(f *testing.F) {
	for _, seed := range [][6]string{
		{"registry", "corp", "https://registry.example.test", "", "zeta", ""},
		{"", "corp", "", "", "corp", ""},
		{"", "Corp", "", "", "corp", ""},
		{"brew-tap", "corp/tools", "", "scoop-bucket", "corp/tools", ""},
		{"", "registry", "https://user:secret@example.test/index", "", "zeta", ""},
		{"", "repo", "ssh://git@example.test/org/repo.git", "", "repo", "https://example.test/org/repo.git"},
		{"", " registry", "", "", "zeta", ""},
		{"", "", "", "", "", "https://example.test/index"},
		{"", "a", "https://example.test/x", "", "a", "https://Example.test/x"},
	} {
		f.Add(seed[0], seed[1], seed[2], seed[3], seed[4], seed[5])
	}
	f.Fuzz(func(t *testing.T, kindA, nameA, urlA, kindB, nameB, urlB string) {
		if len(kindA)+len(nameA)+len(urlA)+len(kindB)+len(nameB)+len(urlB) > 1024 {
			return
		}
		sources := []plan.SourceReference{
			{Role: plan.SourceRegistry, Kind: kindA, Name: nameA, URL: urlA},
			{Role: plan.SourceRegistry, Kind: kindB, Name: nameB, URL: urlB},
		}
		snapshot := append([]plan.SourceReference(nil), sources...)
		swapped := []plan.SourceReference{snapshot[1], snapshot[0]}

		got, err := plan.CanonicalSources(sources)
		if !reflect.DeepEqual(sources, snapshot) {
			t.Fatalf("CanonicalSources mutated its input: %#v", sources)
		}
		other, otherErr := plan.CanonicalSources(swapped)
		if !reflect.DeepEqual(swapped, []plan.SourceReference{snapshot[1], snapshot[0]}) {
			t.Fatalf("CanonicalSources mutated its input: %#v", swapped)
		}
		if (err == nil) != (otherErr == nil) {
			t.Fatalf("canonical outcome depends on declaration order: err=%v, swapped err=%v", err, otherErr)
		}
		if err != nil {
			return
		}
		if !reflect.DeepEqual(got, other) {
			t.Fatalf("canonical output depends on declaration order:\n%#v\n%#v", got, other)
		}
		again, againErr := plan.CanonicalSources(got)
		if againErr != nil {
			t.Fatalf("canonical output is not re-canonical: %v", againErr)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("canonicalization is not idempotent:\n%#v\n%#v", got, again)
		}
		for i, source := range got {
			if sourceErr := source.Validate(); sourceErr != nil {
				t.Fatalf("canonical source %d is invalid: %v", i, sourceErr)
			}
		}
		for _, raw := range []string{urlA, urlB} {
			if urlWithLiteralPassword(raw) {
				t.Fatalf("CanonicalSources accepted %q, which carries a literal URL password", raw)
			}
		}
	})
}
