package httpdownload

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/plan"
)

func debCandidate(when *config.Condition) *config.MethodCandidate {
	return &config.MethodCandidate{
		Kind: "http",
		When: when,
		Config: map[string]any{
			"url": "https://example.test/tool-linux-amd64.deb",
		},
	}
}

func TestDebHostCompatibilityAllowsDebianFamilies(t *testing.T) {
	for _, clan := range []string{"debian", "mint"} {
		t.Run(clan, func(t *testing.T) {
			if err := checkDebHostCompatibility(debCandidate(nil), &engine.Facts{DistroID: clan}, clan); err != nil {
				t.Fatalf("checkDebHostCompatibility() = %v", err)
			}
		})
	}
}

func TestDebHostCompatibilityRejectsUnscopedTermuxPackage(t *testing.T) {
	err := checkDebHostCompatibility(debCandidate(nil), &engine.Facts{DistroID: "termux", IsAndroid: true}, "termux")
	if err == nil {
		t.Fatal("checkDebHostCompatibility() accepted unscoped .deb on native Termux")
	}
	if got := err.Error(); !strings.Contains(got, "not assumed Debian-compatible on native Termux") {
		t.Fatalf("error = %q", got)
	}
}

func TestDebHostCompatibilityAllowsExplicitTermuxPackage(t *testing.T) {
	facts := &engine.Facts{DistroID: "termux", IsAndroid: true}
	for name, when := range map[string]*config.Condition{
		"family": {DistroFamily: []string{"termux"}},
		"id":     {DistroID: []string{"termux"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkDebHostCompatibility(debCandidate(when), facts, "termux"); err != nil {
				t.Fatalf("checkDebHostCompatibility() = %v", err)
			}
		})
	}
}

func TestDebHostCompatibilityRejectsUnrelatedLinuxFamily(t *testing.T) {
	err := checkDebHostCompatibility(debCandidate(nil), &engine.Facts{DistroID: "void", OS: "linux"}, "void")
	if err == nil {
		t.Fatal("checkDebHostCompatibility() accepted generic .deb on Void")
	}
	if got := err.Error(); !strings.Contains(got, `distro family "void"`) {
		t.Fatalf("error = %q", got)
	}
}

func TestDebHostCompatibilityAllowsExplicitNonDebianOverride(t *testing.T) {
	mc := debCandidate(&config.Condition{DistroFamily: []string{"void"}})
	if err := checkDebHostCompatibility(mc, &engine.Facts{DistroID: "void", OS: "linux"}, "void"); err != nil {
		t.Fatalf("explicit target override rejected: %v", err)
	}
}

func TestDownloadCompatibilityUsesResolvedPlanArtifact(t *testing.T) {
	mc := &config.MethodCandidate{Kind: "github", Config: map[string]any{"repo": "owner/repo", "asset": "tool"}}
	p := plan.New("tool", "github", true)
	p.Artifacts = []plan.Artifact{{URL: "https://github.com/owner/repo/releases/download/v1/tool.deb"}}
	err := checkDownloadHostCompatibility(mc, &p, &engine.Facts{DistroID: "arch", OS: "linux"}, "arch")
	if err == nil {
		t.Fatal("resolved .deb artifact was not rejected on Arch")
	}
}
