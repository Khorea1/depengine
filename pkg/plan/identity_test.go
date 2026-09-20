package plan

import (
	"strings"
	"testing"
)

func TestResolvedPlanRejectsCredentialBearingIdentityReferences(t *testing.T) {
	for name, edit := range map[string]func(*ResolvedInstallPlan){
		"source":   func(p *ResolvedInstallPlan) { p.Identity.Source = "https://user:secret@example.test/repo" },
		"registry": func(p *ResolvedInstallPlan) { p.Identity.Registry = "https://example.test/index?token=secret" },
	} {
		t.Run(name, func(t *testing.T) {
			p := New("demo", "native", true)
			edit(&p)
			if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "forbidden") && !strings.Contains(err.Error(), "credentials") {
				t.Fatalf("Validate() error = %v, want credential rejection", err)
			}
		})
	}
}

func TestPlanIdentityRejectsNUL(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*ResolvedInstallPlan)
	}{
		{name: "tool", mut: func(p *ResolvedInstallPlan) { p.Tool.Name = "tool\x00name" }},
		{name: "method", mut: func(p *ResolvedInstallPlan) { p.Candidate.Method = "native\x00kind" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := New("tool", "native", true)
			tc.mut(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate() accepted NUL-bearing identity")
			}
		})
	}
}

func TestResolvedIdentityRejectsMalformedConcreteFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*ResolvedInstallPlan)
	}{
		{name: "package NUL", edit: func(p *ResolvedInstallPlan) { p.Identity.Package = "pkg\x00bad" }},
		{name: "version whitespace", edit: func(p *ResolvedInstallPlan) { p.Identity.Version = " 1.2.3" }},
		{name: "revision NUL", edit: func(p *ResolvedInstallPlan) { p.Identity.Revision = "abc\x00def" }},
		{name: "digest whitespace", edit: func(p *ResolvedInstallPlan) { p.Identity.Digest = "sha256:abc " }},
		{name: "architecture NUL", edit: func(p *ResolvedInstallPlan) { p.Identity.Architecture = "amd64\x00bad" }},
		{name: "platform whitespace", edit: func(p *ResolvedInstallPlan) { p.Identity.Platform = " linux" }},
		{name: "removal identity NUL", edit: func(p *ResolvedInstallPlan) { p.Removal.Identity = "pkg\x00bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := New("tool", "native", true)
			tc.edit(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate() accepted malformed concrete identity")
			}
		})
	}
}

func TestPlanValidateRequiresConcreteResolvedDigest(t *testing.T) {
	for _, digest := range []string{
		"garbage",
		"sha256:",
		"sha256:auto",
		"sha256:not-hex",
		"sha256:abcd",
	} {
		t.Run(digest, func(t *testing.T) {
			p := New("image", "container", true)
			p.Identity.Digest = digest
			if err := p.Validate(); err == nil {
				t.Fatalf("Validate() accepted unresolved digest %q", digest)
			}
		})
	}

	p := New("image", "container", true)
	p.Identity.Digest = "sha256:" + strings.Repeat("a", 64)
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() rejected concrete digest: %v", err)
	}
}

func TestResolvedIdentityRejectsMalformedURLLikeSourceReferences(t *testing.T) {
	for _, edit := range []func(*ResolvedInstallPlan){
		func(p *ResolvedInstallPlan) { p.Identity.Source = "https://" },
		func(p *ResolvedInstallPlan) { p.Identity.Registry = "https:///registry" },
	} {
		p := New("tool", "native", true)
		p.Identity.Version = "1.0.0"
		edit(&p)
		if err := p.Validate(); err == nil {
			t.Fatal("Validate() accepted malformed URL-like identity reference")
		}
	}
	p := New("tool", "native", true)
	p.Identity.Version = "1.0.0"
	p.Identity.Source = "stable"
	p.Identity.Registry = "crates-io"
	if err := p.Validate(); err != nil {
		t.Fatalf("symbolic identity reference rejected: %v", err)
	}
}
