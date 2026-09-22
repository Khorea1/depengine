package plan

import (
	"testing"
)

func TestValidateResolutionAcceptsEnrichment(t *testing.T) {
	intent := New("demo", "http", true)
	intent.Identity.RequestedVersion = &VersionIntent{Mode: VersionExact, Value: "1.2.3"}
	intent.Identity.Version = "1.2.3"
	resolved := intent.Clone()
	resolved.Identity.Source = "https://example.test/releases/demo.tar.gz"
	resolved.Identity.Digest = "sha256:" + string(make([]byte, 0))
	resolved.Artifacts = []Artifact{{URL: "https://example.test/releases/demo.tar.gz"}}
	// Digest must be syntactically valid when set; use a real-length hex.
	resolved.Identity.Digest = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if err := ValidateResolution(intent, resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}
}

func TestValidateResolutionRejectsRewrites(t *testing.T) {
	base := New("demo", "http", true)
	base.Identity.RequestedVersion = &VersionIntent{Mode: VersionExact, Value: "1.2.3"}
	base.Identity.Version = "1.2.3"

	rewrite := func(mut func(*ResolvedInstallPlan)) ResolvedInstallPlan {
		out := base.Clone()
		mut(&out)
		return out
	}
	cases := map[string]ResolvedInstallPlan{
		"tool":     rewrite(func(p *ResolvedInstallPlan) { p.Tool.Name = "other" }),
		"method":   rewrite(func(p *ResolvedInstallPlan) { p.Candidate.Method = "github" }),
		"explicit": rewrite(func(p *ResolvedInstallPlan) { p.Candidate.Explicit = !p.Candidate.Explicit }),
		"requested": rewrite(func(p *ResolvedInstallPlan) {
			p.Identity.RequestedVersion = &VersionIntent{Mode: VersionExact, Value: "9.9.9"}
		}),
		"package": rewrite(func(p *ResolvedInstallPlan) { p.Identity.Package = "other" }),
	}
	for name, resolved := range cases {
		if err := ValidateResolution(base, resolved); err == nil {
			t.Fatalf("%s: ValidateResolution() succeeded, want rewrite rejection", name)
		}
	}
}
