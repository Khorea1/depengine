package lock

import (
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestNewUniversalSetsVersionAndRoundTripsProjection(t *testing.T) {
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{universalTestPlan()})
	if err != nil {
		t.Fatalf("BuildLockDocument() error: %v", err)
	}

	got, err := NewUniversal(document, nil, nil)
	if err != nil {
		t.Fatalf("NewUniversal() error: %v", err)
	}
	if got.Version != CurrentVersion {
		t.Fatalf("Version = %d, want %d", got.Version, CurrentVersion)
	}
	decoded, err := got.ProjectionDocument()
	if err != nil {
		t.Fatalf("ProjectionDocument() error: %v", err)
	}
	if !reflect.DeepEqual(decoded, document) {
		t.Fatalf("projection round trip differs\n got: %#v\nwant: %#v", decoded, document)
	}
}

func TestNewUniversalCopiesMetadataAndCarriesLegacyPins(t *testing.T) {
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{universalTestPlan()})
	if err != nil {
		t.Fatalf("BuildLockDocument() error: %v", err)
	}
	methods := map[string]string{"tool": "method-hash"}
	sources := map[string]string{"tool/git/0": "source-hash"}
	legacy := map[string]ToolPin{"old/git/0": {Revision: "0123456789abcdef0123456789abcdef01234567"}}

	got, err := NewUniversal(document, methods, sources, legacy)
	if err != nil {
		t.Fatalf("NewUniversal() error: %v", err)
	}
	methods["tool"] = "changed"
	sources["tool/git/0"] = "changed"
	legacy["old/git/0"] = ToolPin{}

	if got.MethodsHash["tool"] != "method-hash" {
		t.Errorf("MethodsHash[tool] = %q, want method-hash", got.MethodsHash["tool"])
	}
	if got.SourceHash["tool/git/0"] != "source-hash" {
		t.Errorf("SourceHash[tool/git/0] = %q, want source-hash", got.SourceHash["tool/git/0"])
	}
	wantPin := ToolPin{Revision: "0123456789abcdef0123456789abcdef01234567"}
	if got.Tools["old/git/0"] != wantPin {
		t.Errorf("legacy pin = %+v, want %+v", got.Tools["old/git/0"], wantPin)
	}
	if _, exists := got.Tools["tool/native/0"]; exists {
		t.Error("constructor synthesized a legacy pin for the projected tool")
	}
	if len(got.Tools) != 1 {
		t.Errorf("legacy pins count = %d, want only the carried pin", len(got.Tools))
	}
}

func TestNewUniversalRejectsInvalidProjection(t *testing.T) {
	if _, err := NewUniversal(plan.LockDocument{}, nil, nil); err == nil {
		t.Fatal("NewUniversal() accepted an invalid projection")
	}
}
func universalTestPlan() plan.ResolvedInstallPlan {
	p := plan.New("tool", "native", true)
	p.Identity.Package = "tool"
	p.Identity.Version = "1.0.0"
	return p
}
