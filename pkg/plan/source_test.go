package plan_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestSourceReferenceRejectsLiteralCredentials(t *testing.T) {
	cases := []string{
		"https://user:password@example.test/simple",
		"https://example.test/simple?token=secret",
		"https://example.test/simple?X-Amz-Signature=secret",
	}
	for _, raw := range cases {
		s := plan.SourceReference{Role: plan.SourceIndex, URL: raw}
		if err := s.Validate(); err == nil {
			t.Fatalf("Validate(%q) accepted literal credentials", raw)
		}
	}
}

func TestSourceReferenceAllowsExternalSecretReference(t *testing.T) {
	s := plan.SourceReference{
		Role: plan.SourceRegistry,
		Name: "corporate",
		URL:  "https://packages.example.test/index",
		SecretRef: &plan.SecretReference{
			Provider: "env",
			Name:     "DEPENGINE_CORP_REGISTRY_TOKEN",
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestSourceReferenceRolesRemainDistinct(t *testing.T) {
	host := plan.SourceReference{Role: plan.SourceHostConfiguration, Name: "corp"}
	selection := plan.SourceReference{Role: plan.SourceSelection, Name: "corp"}
	got, err := plan.CanonicalSources([]plan.SourceReference{selection, host})
	if err != nil {
		t.Fatalf("CanonicalSources() error: %v", err)
	}
	if len(got) != 2 || got[0].Role == got[1].Role {
		t.Fatalf("roles collapsed: %+v", got)
	}
}

func TestCanonicalSourcesDeterministicAndRejectsDuplicates(t *testing.T) {
	in := []plan.SourceReference{
		{Role: plan.SourceRegistry, Name: "zeta"},
		{Role: plan.SourceChannel, Name: "stable"},
		{Role: plan.SourceRegistry, Name: "alpha"},
	}
	got, err := plan.CanonicalSources(in)
	if err != nil {
		t.Fatalf("CanonicalSources() error: %v", err)
	}
	want := []string{"stable", "alpha", "zeta"}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if _, err := plan.CanonicalSources([]plan.SourceReference{
		{Role: plan.SourceRegistry, Name: "corp"},
		{Role: plan.SourceRegistry, Name: "corp"},
	}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate sources error = %v", err)
	}
}

func TestSourceTrustRequiresDeclarativeIdentity(t *testing.T) {
	if err := (plan.SourceTrust{}).Validate(); err == nil {
		t.Fatal("empty SourceTrust accepted")
	}
	if err := (plan.SourceTrust{Fingerprint: "SHA256:abc"}).Validate(); err != nil {
		t.Fatalf("fingerprint trust rejected: %v", err)
	}
}

func TestSecretReferenceValidation(t *testing.T) {
	for _, ref := range []plan.SecretReference{
		{},
		{Provider: "env"},
		{Provider: " env", Name: "TOKEN"},
	} {
		if err := ref.Validate(); err == nil {
			t.Fatalf("invalid secret reference accepted: %+v", ref)
		}
	}
	if err := (plan.SecretReference{Provider: "env", Name: "TOKEN"}).Validate(); err != nil {
		t.Fatalf("valid secret reference rejected: %v", err)
	}
}
