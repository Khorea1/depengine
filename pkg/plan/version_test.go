package plan_test

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestVersionIntentValidation(t *testing.T) {
	tests := []struct {
		name    string
		intent  plan.VersionIntent
		wantErr bool
	}{
		{name: "latest", intent: plan.VersionIntent{Mode: plan.VersionLatest}},
		{name: "exact", intent: plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}},
		{name: "constraint", intent: plan.VersionIntent{Mode: plan.VersionConstraint, Value: ">=1.2,<2"}},
		{name: "channel", intent: plan.VersionIntent{Mode: plan.VersionChannel, Channel: &plan.ChannelSelector{Track: "22", Risk: "stable"}}},
		{name: "git tag", intent: plan.VersionIntent{Mode: plan.VersionGitTag, Value: "v1.2.3"}},
		{name: "git branch", intent: plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "release/1.x"}},
		{name: "git revision", intent: plan.VersionIntent{Mode: plan.VersionGitRevision, Value: "abc123"}},
		{name: "container tag", intent: plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "1.2-alpine"}},
		{name: "digest", intent: plan.VersionIntent{Mode: plan.VersionDigest, Value: "sha256:abcdef"}},
		{name: "exact missing value", intent: plan.VersionIntent{Mode: plan.VersionExact}, wantErr: true},
		{name: "latest with value", intent: plan.VersionIntent{Mode: plan.VersionLatest, Value: "1.2.3"}, wantErr: true},
		{name: "channel missing selector", intent: plan.VersionIntent{Mode: plan.VersionChannel}, wantErr: true},
		{name: "channel with value", intent: plan.VersionIntent{Mode: plan.VersionChannel, Value: "stable", Channel: &plan.ChannelSelector{Name: "stable"}}, wantErr: true},
		{name: "whitespace is ambiguous", intent: plan.VersionIntent{Mode: plan.VersionExact, Value: " 1.2.3"}, wantErr: true},
		{name: "unknown", intent: plan.VersionIntent{Mode: "magic", Value: "x"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.intent.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestVersionIntentPortability(t *testing.T) {
	for _, mode := range []plan.VersionMode{plan.VersionLatest, plan.VersionExact, plan.VersionConstraint} {
		if got := (plan.VersionIntent{Mode: mode}).Portability(); got != plan.VersionPortable {
			t.Fatalf("Portability(%s) = %s, want portable", mode, got)
		}
	}
	for _, mode := range []plan.VersionMode{plan.VersionChannel, plan.VersionGitTag, plan.VersionGitBranch, plan.VersionGitRevision, plan.VersionContainerTag, plan.VersionDigest} {
		if got := (plan.VersionIntent{Mode: mode}).Portability(); got != plan.VersionMethodSpecific {
			t.Fatalf("Portability(%s) = %s, want method_specific", mode, got)
		}
	}
}

func TestPlanValidatesRequestedVersion(t *testing.T) {
	p := plan.New("tool", "native", true)
	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted an invalid requested version")
	}

	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}
	p.Identity.Version = "1.2.3"
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}
