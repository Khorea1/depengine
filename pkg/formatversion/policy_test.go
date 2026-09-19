package formatversion_test

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/formatversion"
)

func TestPoliciesAreIndependentAndPreFreeze(t *testing.T) {
	for _, tc := range []struct {
		kind    formatversion.Kind
		current int
	}{
		{formatversion.Manifest, formatversion.CurrentManifestVersion},
		{formatversion.Lock, formatversion.CurrentLockVersion},
		{formatversion.State, formatversion.CurrentStateVersion},
	} {
		p, err := formatversion.PolicyFor(tc.kind)
		if err != nil {
			t.Fatalf("PolicyFor(%q): %v", tc.kind, err)
		}
		if p.Current != tc.current {
			t.Fatalf("PolicyFor(%q).Current = %d, want %d", tc.kind, p.Current, tc.current)
		}
		if p.Stability != formatversion.PreFreeze {
			t.Fatalf("PolicyFor(%q).Stability = %q, want pre_freeze", tc.kind, p.Stability)
		}
	}
}

func TestValidateReadVersionFailClosed(t *testing.T) {
	for _, kind := range []formatversion.Kind{formatversion.Manifest, formatversion.Lock, formatversion.State} {
		p, err := formatversion.PolicyFor(kind)
		if err != nil {
			t.Fatal(err)
		}
		if err := formatversion.ValidateReadVersion(kind, p.Current); err != nil {
			t.Fatalf("ValidateReadVersion(%q, current): %v", kind, err)
		}
		for _, version := range []int{0, p.Current + 1, 99} {
			err := formatversion.ValidateReadVersion(kind, version)
			if err == nil {
				t.Fatalf("ValidateReadVersion(%q, %d) accepted unsupported version", kind, version)
			}
			if !strings.Contains(err.Error(), string(kind)) {
				t.Fatalf("error %q does not identify format %q", err, kind)
			}
		}
	}
}

func TestUnknownFormatKindFailsClosed(t *testing.T) {
	if _, err := formatversion.PolicyFor("future"); err == nil {
		t.Fatal("PolicyFor accepted unknown format kind")
	}
	if err := formatversion.ValidateReadVersion("future", 1); err == nil {
		t.Fatal("ValidateReadVersion accepted unknown format kind")
	}
}
