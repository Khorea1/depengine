package planner_test

import (
	"maps"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
)

// TestResolveEffectFieldsMoveStaticIntent is the P0.4 central invariant: a
// schema field accepted with EffectResolve must observably affect the static
// install plan. Two candidates differing only in that field must resolve to
// different intents (or the second value must fail validation). A field that
// builds two identical successful plans is accepted but ignored — the manifest
// looks precise while planning is blind to it.
//
// Deliberate exclusions (runtime-resolved dimensions whose static projection
// is P1.1 work, not ignored fields):
//   - release + branch on github/http/msi/appimage/android: consumed by the
//     runtime download resolver (internal/httpdownload/resolver.go), which enriches
//     the resolved plan after static planning.
//   - git.url: consumed by the git adapter at clone time
//     (internal/git/adapter.go); the static plan carries only version selectors.
//   - native.pkg_overrides: resolved per-clan by the executor
//     (internal/exec/native_adapter.go), invisible to host-independent planning.
var resolveEffectExclusions = map[string]bool{
	"github.release":       true,
	"http.release":         true,
	"msi.release":          true,
	"appimage.release":     true,
	"android.release":      true,
	"github.branch":        true,
	"http.branch":          true,
	"msi.branch":           true,
	"appimage.branch":      true,
	"android.branch":       true,
	"git.url":              true,
	"native.pkg_overrides": true,
}

// resolveEffectBases provides minimal valid configs with zero version
// selectors set, so each single-field probe is unambiguous.
var resolveEffectBases = map[string]map[string]any{
	"native":        {"pkg": "demo"},
	"winget":        {"pkg": "demo"},
	"cargo":         {"pkg": "demo"},
	"cargo+git":     {"pkg": "demo", "git": "https://example.test/demo.git"},
	"pipx":          {"pkg": "demo"},
	"uv":            {"pkg": "demo"},
	"pip":           {"pkg": "demo"},
	"npm":           {"pkg": "demo"},
	"bun":           {"pkg": "demo"},
	"gem":           {"pkg": "demo"},
	"conda":         {"pkg": "demo"},
	"git":           {"url": "https://example.test/demo.git"},
	"local":         {"local_path": "vendor/tool.tar.gz"},
	"github":        {"repo": "org/demo", "asset": "demo.tar.gz"},
	"appimage":      {"url": "https://example.test/tool.AppImage"},
	"appimage+repo": {"repo": "org/demo", "asset": "demo.tar.gz"},
	"android":       {"url": "https://example.test/tool.apk"},
	"android+repo":  {"repo": "org/demo", "asset": "demo.tar.gz"},
	"http":          {"url": "https://example.test/tool.tar.gz"},
	"http+repo":     {"repo": "org/demo", "asset": "demo.tar.gz"},
	"msi":           {"url": "https://example.test/tool.msi", "product_name": "Demo"},
	"msi+repo":      {"repo": "org/demo", "asset": "demo.tar.gz", "product_name": "Demo"},
}

func TestResolveEffectFieldsMoveStaticIntent(t *testing.T) {
	shaA := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	shaB := "sha256:ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"
	// Probe values keyed by "kind.field", falling back to bare field name.
	values := map[string][]any{
		"url":          {"https://example.test/a.tar.gz", "https://example.test/b.tar.gz"},
		"git.url":      {"https://example.test/a.git", "https://example.test/b.git"},
		"appimage.url": {"https://example.test/a.AppImage", "https://example.test/b.AppImage"},
		"android.url":  {"https://example.test/a.apk", "https://example.test/b.apk"},
		"cargo.git":    {"https://example.test/a.git", "https://example.test/b.git"},
		"repo":         {"org/a", "org/b"},
		"asset":        {"a.tar.gz", "b.tar.gz"},
		"branch":       {"edge-a", "edge-b"},
		"tag":          {"tag-a", "tag-b"},
		"rev":          {"rev-a", "rev-b"},
		"registry":     {"reg-a", "reg-b"},
		"source":       {"src-a", "src-b"},
		"index_url":    {"https://index-a.example/simple", "https://index-b.example/simple"},
		"index":        {"https://index-a.example/simple", "https://index-b.example/simple"},
		"channels":     {[]any{"chan-a"}, []any{"chan-b"}},
		"local_path":   {"vendor/a.tar.gz", "vendor/b.tar.gz"},
		"checksum":     {shaA, shaB},
		"cargo.branch": {"edge-a", "edge-b"},
		"cargo.tag":    {"tag-a", "tag-b"},
		"cargo.rev":    {"rev-a", "rev-b"},
	}
	// Base overrides for probes that need companion fields.
	bases := map[string]string{
		"cargo.branch":   "cargo+git",
		"cargo.tag":      "cargo+git",
		"cargo.rev":      "cargo+git",
		"http.asset":     "http+repo",
		"msi.asset":      "msi+repo",
		"appimage.asset": "appimage+repo",
		"android.asset":  "android+repo",
	}

	tool := &config.Tool{Name: "demo"}
	for _, contract := range methodkind.Contracts {
		for name, field := range contract.Fields {
			if field.Effects&methodkind.EffectResolve == 0 {
				continue
			}
			key := contract.Kind + "." + name
			if resolveEffectExclusions[key] {
				continue
			}
			pair, ok := values[key]
			if !ok {
				pair, ok = values[name]
			}
			if !ok {
				t.Errorf("%s declares EffectResolve but has no differential probe; add values or justify an exclusion", key)
				continue
			}
			baseKey := contract.Kind
			if override, ok := bases[key]; ok {
				baseKey = override
			}
			base, ok := resolveEffectBases[baseKey]
			if !ok {
				t.Errorf("%s has no probe base config", key)
				continue
			}
			build := func(value any) (any, error) {
				cfg := maps.Clone(base)
				cfg[name] = value
				return planner.BuildCandidateIntent(tool, &config.MethodCandidate{Kind: contract.Kind, Config: cfg})
			}
			a, err := build(pair[0])
			if err != nil {
				t.Fatalf("%s: probe value %v rejected, fix the probe: %v", key, pair[0], err)
			}
			b, err := build(pair[1])
			if err != nil {
				continue // Value-dependent validation is itself an observable effect.
			}
			if reflect.DeepEqual(a, b) {
				t.Errorf("%s is accepted but ignored: distinct values resolve to identical plans", key)
			}
		}
	}
}
