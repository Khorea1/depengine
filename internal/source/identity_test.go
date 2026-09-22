package source

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

func TestResourceIdentityRoundTrip(t *testing.T) {
	for _, source := range []config.Source{
		{Kind: "apt-ppa", Name: "ppa:Vendor/Stable", URL: "https://ignored.example/repo"},
		{Kind: "dnf-copr", Name: "Vendor/Tools"},
		{Kind: "scoop-bucket", Name: "Vendor Tools", URL: "https://example.invalid/bucket.git"},
		{Kind: "brew-tap", Name: "Vendor/Tools"},
	} {
		identity, err := ResourceIdentity(source)
		if err != nil {
			t.Fatalf("ResourceIdentity(%#v): %v", source, err)
		}
		if identity.Kind != plan.ResourceSource {
			t.Fatalf("kind = %q, want source", identity.Kind)
		}
		if source.URL != "" && identity.Key == source.URL {
			t.Fatalf("identity unexpectedly persisted source URL %q", source.URL)
		}
		decoded, err := FromResourceIdentity(identity)
		if err != nil {
			t.Fatalf("FromResourceIdentity(%#v): %v", identity, err)
		}
		if decoded.Kind != lower(source.Kind) || decoded.Name != lower(source.Name) || decoded.URL != "" {
			t.Fatalf("decoded = %#v, want normalized kind/name without URL", decoded)
		}
	}
}

func TestFromResourceIdentityRejectsAliasesAndWrongKinds(t *testing.T) {
	cases := []plan.ResourceIdentity{
		{Kind: plan.ResourcePrerequisite, Key: "v1?kind=brew-tap&name=vendor%2Ftools"},
		{Kind: plan.ResourceSource, Key: "kind=brew-tap&name=vendor%2Ftools"},
		{Kind: plan.ResourceSource, Key: "v1?name=vendor%2Ftools&kind=brew-tap"},
		{Kind: plan.ResourceSource, Key: "v1?kind=brew-tap&kind=brew-tap&name=vendor%2Ftools"},
		{Kind: plan.ResourceSource, Key: "v1?kind=unknown&name=vendor"},
	}
	for _, identity := range cases {
		if _, err := FromResourceIdentity(identity); err == nil {
			t.Fatalf("FromResourceIdentity(%#v) unexpectedly succeeded", identity)
		}
	}
}

func TestResourceIdentityRejectsInvalidSource(t *testing.T) {
	for _, source := range []config.Source{
		{},
		{Kind: "brew-tap"},
		{Kind: "unknown", Name: "repo"},
		{Kind: "brew-tap", Name: "repo\x00bad"},
	} {
		if _, err := ResourceIdentity(source); err == nil {
			t.Fatalf("ResourceIdentity(%#v) unexpectedly succeeded", source)
		}
	}
}

func lower(s string) string {
	out := []byte(s)
	for i, b := range out {
		if b >= 'A' && b <= 'Z' {
			out[i] = b + ('a' - 'A')
		}
	}
	return string(out)
}
