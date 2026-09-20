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
