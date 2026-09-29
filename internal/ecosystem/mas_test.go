package ecosystem

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func withSudoElevation(t *testing.T) {
	t.Helper()
	run.OverrideElevation("sudo")
	t.Cleanup(func() { run.OverrideElevation("") })
}

func TestMasAdapterRemoveElevatesWithNumericID(t *testing.T) {
	withSudoElevation(t)
	fr := &run.FakeRunner{LookPaths: map[string]bool{"mas": true}}
	tl, mc := tool("xcode", "497799835")

	a := NewMasAdapter()
	if !a.CanRemove() {
		t.Fatal("CanRemove() = false, want true")
	}
	if err := a.Remove(context.Background(), fr, tl, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	got := fr.Calls[len(fr.Calls)-1]
	if got.Name != "sudo" || strings.Join(got.Args, " ") != "mas uninstall 497799835" {
		t.Fatalf("Remove() call = %s %v, want sudo mas uninstall 497799835", got.Name, got.Args)
	}
}

func TestMasAdapterRemoveAcceptsBundleID(t *testing.T) {
	withSudoElevation(t)
	fr := &run.FakeRunner{LookPaths: map[string]bool{"mas": true}}
	tl, mc := tool("xcode", "com.apple.dt.Xcode")
	if err := NewMasAdapter().Remove(context.Background(), fr, tl, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	got := fr.Calls[len(fr.Calls)-1]
	if got.Name != "sudo" || strings.Join(got.Args, " ") != "mas uninstall com.apple.dt.Xcode" {
		t.Fatalf("Remove() call = %s %v, want sudo mas uninstall com.apple.dt.Xcode", got.Name, got.Args)
	}
}

func TestMasAdapterRemoveRejectsInvalidIdentifierBeforeRunning(t *testing.T) {
	for _, pkg := range []string{"497799835; rm -rf /", "-1", " ", "12 34", ".com.apple.Xcode"} {
		t.Run(pkg, func(t *testing.T) {
			withSudoElevation(t)
			fr := &run.FakeRunner{LookPaths: map[string]bool{"mas": true}}
			tl, mc := tool("app", pkg)
			if err := NewMasAdapter().Remove(context.Background(), fr, tl, mc); err == nil {
				t.Fatal("Remove() must reject an invalid app identifier")
			}
			if len(fr.Calls) != 0 {
				t.Fatalf("no command may run for %q, got %v", pkg, fr.Calls)
			}
		})
	}
}

func TestMasAdapterRemoveRequiresMasBinary(t *testing.T) {
	withSudoElevation(t)
	fr := &run.FakeRunner{LookPaths: map[string]bool{"mas": false}}
	tl, mc := tool("xcode", "497799835")
	if err := NewMasAdapter().Remove(context.Background(), fr, tl, mc); err == nil {
		t.Fatal("Remove() must fail when mas is unavailable")
	}
}

func TestMasAdapterRemoveSurfacesFailure(t *testing.T) {
	withSudoElevation(t)
	fr := &run.FakeRunner{LookPaths: map[string]bool{"mas": true}, ExitCode: 1}
	tl, mc := tool("xcode", "497799835")
	if err := NewMasAdapter().Remove(context.Background(), fr, tl, mc); err == nil {
		t.Fatal("Remove() must surface a non-zero exit")
	}
}

func TestMasAdapterKeepsBaseInstallAndKind(t *testing.T) {
	if got := NewMasAdapter().Kind(); got != "mas" {
		t.Fatalf("Kind() = %q, want mas", got)
	}
}
