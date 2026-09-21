package planner_test

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
)

// rejected reports whether the static planning boundary refuses a candidate,
// either while building the plan or because the contract lacks a capability.
func rejected(t *testing.T, kind string, cfg map[string]any) bool {
	t.Helper()
	tool, method := candidate("demo", kind, cfg)
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		return true
	}
	contract, ok := methodkind.Lookup(kind)
	if !ok {
		t.Fatalf("unknown kind %q", kind)
	}
	missing, err := contract.MissingPlanCapabilities(p)
	return err != nil || missing != 0
}

// TestUnsupportedSelectorsAreNeverDroppedSilently is the fail-closed
// invariant of the planning boundary: a version/selector field that a method
// contract cannot honor must be rejected, not ignored.
func TestUnsupportedSelectorsAreNeverDroppedSilently(t *testing.T) {
	need := map[string]func(methodkind.Contract) bool{
		"version": func(c methodkind.Contract) bool { return c.Supports(methodkind.CapabilityExactVersion) },
		"digest":  func(c methodkind.Contract) bool { return c.Supports(methodkind.CapabilityImmutableIdentity) },
		"rev":     func(c methodkind.Contract) bool { return c.Supports(methodkind.CapabilityRevision) },
		"channel": func(c methodkind.Contract) bool { return c.Supports(methodkind.CapabilityChannel) },
		"track":   func(c methodkind.Contract) bool { return c.Supports(methodkind.CapabilityChannel) },
		"risk":    func(c methodkind.Contract) bool { return c.Supports(methodkind.CapabilityChannel) },
		"tag": func(c methodkind.Contract) bool {
			return c.Supports(methodkind.CapabilityRevision) || c.Supports(methodkind.CapabilityMutableTag)
		},
	}
	samples := map[string]string{
		"version": "1.2.3", "digest": "sha256:" + strings.Repeat("a", 64), "rev": "abc123",
		"channel": "stable", "track": "latest", "risk": "stable", "tag": "v1",
	}
	for i := range methodkind.Contracts {
		c := methodkind.Contracts[i]
		for key, supported := range need {
			if _, declared := c.Fields[key]; declared {
				continue // declared fields are the adapter's own contract
			}
			got := rejected(t, c.Kind, map[string]any{key: samples[key]})
			if want := !supported(c); got != want {
				t.Errorf("%s + undeclared %q: rejected=%v, want %v", c.Kind, key, got, want)
			}
		}
	}
}

func TestConflictingVersionSelectorsAreRejected(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	for name, tc := range map[string]struct {
		kind string
		cfg  map[string]any
	}{
		"version+digest": {"go", map[string]any{"version": "1.0.0", "digest": digest}},
		"version+tag":    {"git", map[string]any{"url": "https://example.test/t.git", "version": "1.0.0", "tag": "v1"}},
		"tag+rev":        {"git", map[string]any{"url": "https://example.test/t.git", "tag": "v1", "rev": "abc123"}},
	} {
		t.Run(name, func(t *testing.T) {
			tool, method := candidate("demo", tc.kind, tc.cfg)
			if _, err := planner.BuildCandidateIntent(tool, method); err == nil || !strings.Contains(err.Error(), "conflicting version selectors") {
				t.Fatalf("error = %v, want conflicting version selectors", err)
			}
		})
	}
}

func TestInvalidScopeIsRejectedUnlessMethodOwnsScopeVocabulary(t *testing.T) {
	if !rejected(t, "go", map[string]any{"scope": "galaxy"}) {
		t.Fatal("scope on a method without scope support must be rejected")
	}
	// gem's "default" is manager-specific and intentionally non-portable.
	if rejected(t, "gem", map[string]any{"pkg": "x", "scope": "default"}) {
		t.Fatal("manager-specific scope on a scope-capable method must stay accepted")
	}
	tool, method := candidate("demo", "winget", map[string]any{"pkg": "x", "scope": "machine"})
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil || p.Identity.Scope != "system" {
		t.Fatalf("scope = %q err = %v, want canonical system", p.Identity.Scope, err)
	}
}

func TestSupportedSelectorsStillPlan(t *testing.T) {
	for name, tc := range map[string]struct {
		kind string
		cfg  map[string]any
	}{
		"snap channel":   {"snap", map[string]any{"pkg": "x", "channel": "edge"}},
		"cargo git tag":  {"cargo", map[string]any{"pkg": "x", "git": "https://example.test/x.git", "tag": "v1"}},
		"container tag":  {"container", map[string]any{"manager": "podman", "source": "org/x", "tag": "1"}},
		"github branch":  {"github", map[string]any{"repo": "org/x", "asset": "x.tar.gz", "branch": "edge"}},
		"flatpak branch": {"flatpak", map[string]any{"pkg": "org.x.Y", "branch": "stable"}},
		"pip exact":      {"pip", map[string]any{"pkg": "x", "version": "1.0"}},
	} {
		t.Run(name, func(t *testing.T) {
			if rejected(t, tc.kind, tc.cfg) {
				t.Fatalf("%s %v unexpectedly rejected", tc.kind, tc.cfg)
			}
		})
	}
}

func TestSSHSourcesArePlannable(t *testing.T) {
	for name, tc := range map[string]struct {
		kind string
		cfg  map[string]any
	}{
		"cargo scp git": {"cargo", map[string]any{"pkg": "x", "git": "git@github.com:org/x.git"}},
		"cargo ssh git": {"cargo", map[string]any{"pkg": "x", "git": "ssh://git@github.com/org/x.git"}},
		"pip git+ssh":   {"pip", map[string]any{"pkg": "x", "source": "git+ssh://git@github.com/org/x.git"}},
	} {
		t.Run(name, func(t *testing.T) {
			if rejected(t, tc.kind, tc.cfg) {
				t.Fatalf("%s %v rejected; SSH usernames are not credentials", tc.kind, tc.cfg)
			}
		})
	}
	if !rejected(t, "cargo", map[string]any{"pkg": "x", "git": "https://token@github.com/org/x.git"}) {
		t.Fatal("https userinfo must stay rejected")
	}
}
