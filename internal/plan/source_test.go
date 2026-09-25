package plan_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestSourceReferenceRejectsLiteralCredentials(t *testing.T) {
	cases := []string{
		"https://user:password@example.test/simple",
		"https://example.test/simple?token=secret",
		"https://example.test/simple?X-Amz-Signature=secret",
		"https://example.test/simple?X-Amz-Credential=credential",
		"https://example.test/simple?X-Amz-Security-Token=session",
		"https://example.test/simple?X-Goog-Signature=signature",
		"https://example.test/simple?client_secret=secret",
		"https://example.test/simple?refresh_token=secret",
	}
	for _, raw := range cases {
		s := plan.SourceReference{Role: plan.SourceIndex, URL: raw}
		if err := s.Validate(); err == nil {
			t.Fatalf("Validate(%q) accepted literal credentials", raw)
		}
	}
}

func TestSourceReferenceMalformedURLDoesNotLeakCredentials(t *testing.T) {
	const secret = "redaction-sentinel"
	source := plan.SourceReference{
		Role: plan.SourceRemote,
		URL:  "https://user:" + secret + "@%zz.example/repo.git",
	}
	err := source.Validate()
	if err == nil {
		t.Fatal("Validate() accepted malformed credential-bearing URL")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Validate() leaked credential in error: %v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Fatalf("Validate() error = %q, want redacted userinfo marker", err)
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

func TestHostSourceKindsRemainDistinct(t *testing.T) {
	got, err := plan.CanonicalSources([]plan.SourceReference{
		{Role: plan.SourceHostConfiguration, Kind: "brew-tap", Name: "corp/tools", Owned: true},
		{Role: plan.SourceHostConfiguration, Kind: "scoop-bucket", Name: "corp/tools", Owned: true},
	})
	if err != nil {
		t.Fatalf("CanonicalSources() error: %v", err)
	}
	if len(got) != 2 || got[0].Kind == got[1].Kind {
		t.Fatalf("host source kinds collapsed: %+v", got)
	}
}

func TestHostSourceURLRequiresSupportedKind(t *testing.T) {
	for _, kind := range []string{"apt-ppa", "dnf-copr"} {
		source := plan.SourceReference{Role: plan.SourceHostConfiguration, Kind: kind, Name: "vendor/tools", URL: "https://example.test/tools"}
		if err := source.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("Validate(%s) = %v, want unsupported URL", kind, err)
		}
	}
	for _, kind := range []string{"scoop-bucket", "brew-tap"} {
		source := plan.SourceReference{Role: plan.SourceHostConfiguration, Kind: kind, Name: "vendor/tools", URL: "https://example.test/tools"}
		if err := source.Validate(); err != nil {
			t.Fatalf("Validate(%s) = %v, want supported URL", kind, err)
		}
	}
}

func TestSourceReferenceRejectsMalformedKind(t *testing.T) {
	for _, kind := range []string{" brew-tap", "brew-tap ", "brew\x00tap"} {
		if err := (plan.SourceReference{Role: plan.SourceHostConfiguration, Kind: kind, Name: "corp/tools"}).Validate(); err == nil {
			t.Fatalf("Validate() accepted malformed source kind %q", kind)
		}
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

func TestSourceMetadataRejectsNUL(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "source name", err: (plan.SourceReference{Role: plan.SourceSelection, Name: "stable\x00repo"}).Validate()},
		{name: "trust key", err: (plan.SourceTrust{KeyReference: "key\x00ref"}).Validate()},
		{name: "trust fingerprint", err: (plan.SourceTrust{Fingerprint: "ABCD\x00EF"}).Validate()},
		{name: "secret provider", err: (plan.SecretReference{Provider: "env\x00bad", Name: "TOKEN"}).Validate()},
		{name: "secret name", err: (plan.SecretReference{Provider: "env", Name: "TOKEN\x00BAD"}).Validate()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("Validate() accepted NUL-bearing metadata")
			}
		})
	}
}

func TestCanonicalSourcesReturnsIndependentCopy(t *testing.T) {
	trust := &plan.SourceTrust{KeyReference: "keyring:vendor", Fingerprint: "ABCD"}
	secret := &plan.SecretReference{Provider: "env", Name: "TOKEN"}
	input := []plan.SourceReference{{
		Role:      plan.SourceRegistry,
		Name:      "registry",
		URL:       "https://registry.example.test",
		Trust:     trust,
		SecretRef: secret,
	}}
	got, err := plan.CanonicalSources(input)
	if err != nil {
		t.Fatal(err)
	}
	got[0].Trust.KeyReference = "mutated"
	got[0].SecretRef.Name = "MUTATED"
	if input[0].Trust.KeyReference != "keyring:vendor" {
		t.Fatal("CanonicalSources output aliases input trust metadata")
	}
	if input[0].SecretRef.Name != "TOKEN" {
		t.Fatal("CanonicalSources output aliases input secret reference")
	}
}

func TestSourceReferenceURLRequiresRemoteURLShape(t *testing.T) {
	for _, raw := range []string{
		"registry-name",
		"https://",
		"https:///missing-host",
		"ssh:///missing-host/repo",
		" source://example.test/repo",
		"https://example.test/repo\x00bad",
	} {
		t.Run(strings.ReplaceAll(raw, "/", "_"), func(t *testing.T) {
			s := plan.SourceReference{Role: plan.SourceRemote, URL: raw}
			if err := s.Validate(); err == nil {
				t.Fatalf("SourceReference.URL %q unexpectedly accepted", raw)
			}
		})
	}

	for _, raw := range []string{
		"https://example.test/org/repo.git",
		"ssh://git@example.test/org/repo.git",
		"git@example.test:org/repo.git",
	} {
		s := plan.SourceReference{Role: plan.SourceRemote, URL: raw}
		if err := s.Validate(); err != nil {
			t.Fatalf("valid remote source URL %q rejected: %v", raw, err)
		}
	}

	// Symbolic manager/source identities belong in Name rather than URL.
	if err := (plan.SourceReference{Role: plan.SourceRegistry, Name: "crates-io"}).Validate(); err != nil {
		t.Fatalf("symbolic source name rejected: %v", err)
	}
}
