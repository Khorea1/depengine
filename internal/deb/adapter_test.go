package deb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestObserveReportsExactPackageIdentity(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "dpkg\tinstalled\t1.22.6\tamd64\ndelta\tinstalled\t0.18.2\tamd64\n"}
	tool := &config.Tool{Name: "delta"}
	method := &config.MethodCandidate{Config: map[string]any{"architecture": "amd64"}}

	observation, err := NewAdapter().Observe(context.Background(), runner, tool, method)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("presence = %v, want present", observation.Presence)
	}
	if observation.Identity.Package != "delta" || observation.Identity.Version != "0.18.2" || observation.Identity.Architecture != "amd64" {
		t.Fatalf("identity = %#v", observation.Identity)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "dpkg-query" || len(runner.Calls[0].Args) != 2 || runner.Calls[0].Args[0] != "-W" || !strings.HasPrefix(runner.Calls[0].Args[1], "-f=") {
		t.Fatalf("calls = %#v", runner.Calls)
	}
}

func TestObserveDoesNotTreatQueryFailureAsAbsent(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 2, Stderr: "database is unavailable"}
	observation, err := NewAdapter().Observe(context.Background(), runner, &config.Tool{Name: "delta"}, &config.MethodCandidate{Config: map[string]any{"architecture": "amd64"}})
	if err == nil || observation.Presence != plan.PresenceUnknown {
		t.Fatalf("observation = %#v, err = %v; want unknown with error", observation, err)
	}
}

func TestObserveExitOneWithQueryErrorIsUnknown(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 1, Stderr: "dpkg-query: unable to read package database"}
	observation, err := NewAdapter().Observe(context.Background(), runner, &config.Tool{Name: "delta"}, &config.MethodCandidate{Config: map[string]any{"architecture": "amd64"}})
	if err == nil || observation.Presence != plan.PresenceUnknown {
		t.Fatalf("observation = %#v, err = %v; want unknown with error", observation, err)
	}
}

func TestRemoveRefusesChangedInstalledVersion(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "delta\tinstalled\t0.19.0\tamd64\n"}
	method := &config.MethodCandidate{Config: map[string]any{
		"pkg":          "delta",
		"version":      "0.18.2",
		"architecture": "amd64",
	}}

	err := NewAdapter().Remove(context.Background(), runner, &config.Tool{Name: "delta"}, method)
	if err == nil || !strings.Contains(err.Error(), "differs from recorded version") {
		t.Fatalf("Remove error = %v, want version mismatch", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "dpkg-query" {
		t.Fatalf("calls = %#v; removal must stop before dpkg", runner.Calls)
	}
}

func TestRemoveRefusesMissingRecordedVersion(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "delta\tinstalled\t0.18.2\tamd64\n"}
	method := &config.MethodCandidate{Config: map[string]any{
		"pkg":          "delta",
		"architecture": "amd64",
	}}

	err := NewAdapter().Remove(context.Background(), runner, &config.Tool{Name: "delta"}, method)
	if err == nil || !strings.Contains(err.Error(), "without recorded package version") {
		t.Fatalf("Remove error = %v, want missing-version refusal", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "dpkg-query" {
		t.Fatalf("calls = %#v; removal must stop before dpkg", runner.Calls)
	}
}

func TestReadMetadataRequiresValidPackageArchitectureAndVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		want bool
	}{
		{name: "valid epoch version", out: "Package: delta\nVersion: 1:0.18.2-1\nArchitecture: amd64\n", want: true},
		{name: "invalid package", out: "Package: ../delta\nVersion: 0.18.2\nArchitecture: amd64\n"},
		{name: "missing version", out: "Package: delta\nArchitecture: amd64\n"},
		{name: "invalid architecture", out: "Package: delta\nVersion: 0.18.2\nArchitecture: ../amd64\n"},
		{name: "missing architecture", out: "Package: delta\nVersion: 0.18.2\n"},
		{name: "duplicate field", out: "Package: delta\nVersion: 0.18.2\nArchitecture: amd64\nPackage: delta\n"},
		{name: "malformed field", out: "Package delta\nVersion: 0.18.2\nArchitecture: amd64\n"},
		{name: "whitespace in version", out: "Package: delta\nVersion: 0.18.2 invalid\nArchitecture: amd64\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &run.FakeRunner{Stdout: tc.out}
			got, err := readMetadata(context.Background(), runner, "/tmp/package.deb")
			if (err == nil) != tc.want {
				t.Fatalf("readMetadata() = %#v, %v; want success=%t", got, err, tc.want)
			}
			if tc.want && (got.pkg != "delta" || got.version != "1:0.18.2-1" || got.arch != "amd64") {
				t.Fatalf("metadata = %#v", got)
			}
		})
	}
}

func TestObserveDistinguishesKnownAbsenceFromUncertainOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stdout  string
		want    plan.PresenceState
		wantErr bool
	}{
		{name: "not installed", stdout: "delta\tnot-installed\t\tamd64\n", want: plan.PresenceAbsent},
		{name: "unrelated inventory proves absence", stdout: "dpkg\tinstalled\t1.22.6\tamd64\n", want: plan.PresenceAbsent},
		{name: "package-name prefix is not exact match", stdout: "delta-tools\tinstalled\t0.18.2\tamd64\n", want: plan.PresenceAbsent},
		{name: "config files", stdout: "delta\tconfig-files\t0.18.2\tamd64\n", want: plan.PresenceAbsent},
		{name: "half installed", stdout: "delta\thalf-installed\t0.18.2\tamd64\n", want: plan.PresenceUnknown, wantErr: true},
		{name: "unpacked", stdout: "delta\tunpacked\t0.18.2\tamd64\n", want: plan.PresenceUnknown, wantErr: true},
		{name: "empty inventory", stdout: "", want: plan.PresenceAbsent},
		{name: "missing architecture field", stdout: "delta\tinstalled\t0.18.2\n", want: plan.PresenceUnknown, wantErr: true},
		{name: "missing version", stdout: "delta\tinstalled\t\tamd64\n", want: plan.PresenceUnknown, wantErr: true},
		{name: "extra field", stdout: "delta\tinstalled\t0.18.2\tamd64\textra\n", want: plan.PresenceUnknown, wantErr: true},
		{name: "duplicate matching records", stdout: "delta\tinstalled\t0.18.2\tamd64\ndelta\tinstalled\t0.18.2\tamd64\n", want: plan.PresenceUnknown, wantErr: true},
		{name: "unterminated record", stdout: "delta\tinstalled\t0.18.2\tamd64", want: plan.PresenceUnknown, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &run.FakeRunner{Stdout: tc.stdout}
			got, err := NewAdapter().Observe(context.Background(), runner, &config.Tool{Name: "delta"}, &config.MethodCandidate{Config: map[string]any{"architecture": "amd64"}})
			if got.Presence != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("Observe() = %#v, %v; want presence=%v error=%t", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestObserveOtherArchitectureIsUncertain(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "delta\tinstalled\t0.18.2\tarm64\n"}
	got, err := NewAdapter().Observe(context.Background(), runner, &config.Tool{Name: "delta"}, &config.MethodCandidate{Config: map[string]any{"architecture": "amd64"}})
	if err == nil || got.Presence != plan.PresenceUnknown {
		t.Fatalf("Observe() = %#v, %v; want unknown with error", got, err)
	}
}

func TestObserveWithoutArchitectureRejectsMultiplePackageRecords(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "delta\tinstalled\t0.18.2\tamd64\ndelta\tconfig-files\t0.18.2\tarm64\n"}
	got, err := NewAdapter().Observe(context.Background(), runner, &config.Tool{Name: "delta"}, &config.MethodCandidate{})
	if err == nil || got.Presence != plan.PresenceUnknown {
		t.Fatalf("Observe() = %#v, %v; want unknown with error", got, err)
	}
}

func TestRemoveRefusesUncertainObservedState(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "delta\thalf-configured\t0.18.2\tamd64\n"}
	method := &config.MethodCandidate{Config: map[string]any{"pkg": "delta", "version": "0.18.2", "architecture": "amd64"}}
	err := NewAdapter().Remove(context.Background(), runner, &config.Tool{Name: "delta"}, method)
	if err == nil {
		t.Fatal("Remove() succeeded for an uncertain dpkg state")
	}
	for _, call := range runner.Calls {
		if call.Name == "dpkg" {
			t.Fatalf("uncertain observation authorized removal: %#v", runner.Calls)
		}
	}
}

func TestRemoveRefusesDifferentArchitectureInventory(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "delta\tinstalled\t0.18.2\tarm64\n"}
	method := &config.MethodCandidate{Config: map[string]any{"pkg": "delta", "version": "0.18.2", "architecture": "amd64"}}
	err := NewAdapter().Remove(context.Background(), runner, &config.Tool{Name: "delta"}, method)
	if err == nil {
		t.Fatal("Remove() succeeded when only another architecture was observed")
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "dpkg-query" {
		t.Fatalf("calls = %#v; architecture contradiction must stop before dpkg", runner.Calls)
	}
}

func TestRemoveRefusesKnownAbsentPackage(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "dpkg\tinstalled\t1.22.6\tamd64\n"}
	method := &config.MethodCandidate{Config: map[string]any{"pkg": "delta", "version": "0.18.2", "architecture": "amd64"}}
	err := NewAdapter().Remove(context.Background(), runner, &config.Tool{Name: "delta"}, method)
	if err == nil {
		t.Fatal("Remove() succeeded for an absent package")
	}
	for _, call := range runner.Calls {
		if call.Name == "dpkg" {
			t.Fatalf("known absence authorized removal: %#v", runner.Calls)
		}
	}
}

func TestRemoveRefusesInventoryQueryExitOne(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 1, Stderr: "dpkg-query: package database error"}
	method := &config.MethodCandidate{Config: map[string]any{"pkg": "delta", "version": "0.18.2", "architecture": "amd64"}}
	err := NewAdapter().Remove(context.Background(), runner, &config.Tool{Name: "delta"}, method)
	if err == nil {
		t.Fatal("Remove() succeeded after an ambiguous inventory query failure")
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "dpkg-query" {
		t.Fatalf("calls = %#v; query failure must stop before dpkg", runner.Calls)
	}
}

func TestInstallResolvedInstallsMatchingArtifactAndPersistsIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("deb payload")) }))
	defer server.Close()
	runner := &run.FakeRunner{Stdout: "Package: delta\nVersion: 1:0.18.2-1\nArchitecture: amd64\n"}
	method := &config.MethodCandidate{Config: map[string]any{"pkg": "delta", "version": "1:0.18.2-1", "architecture": "amd64"}}
	resolved := plan.New("delta", "deb", true)
	resolved.Artifacts = []plan.Artifact{{URL: server.URL + "/delta.deb"}}
	if err := NewAdapter().InstallResolved(context.Background(), runner, &config.Tool{Name: "delta"}, method, &resolved); err != nil {
		t.Fatal(err)
	}
	if method.Config["pkg"] != "delta" || method.Config["version"] != "1:0.18.2-1" || method.Config["architecture"] != "amd64" {
		t.Fatalf("persisted method identity = %#v", method.Config)
	}
	if len(runner.Calls) != 2 || runner.Calls[0].Name != "dpkg-deb" {
		t.Fatalf("calls = %#v; want metadata inspection followed by install", runner.Calls)
	}
	stagedRead := false
	for _, arg := range runner.Calls[0].Args {
		if strings.HasSuffix(arg, "package.deb") {
			stagedRead = true
		}
	}
	if !stagedRead {
		t.Fatalf("metadata inspection did not read the staged package: %#v", runner.Calls[0])
	}
	install := effectiveArgv(runner.Calls[1])
	if len(install) != 4 || install[0] != "dpkg" || install[1] != "--install" || install[2] != "--" || !strings.HasSuffix(install[3], "package.deb") {
		t.Fatalf("unexpected dpkg invocation: %#v", runner.Calls[1])
	}
}

// effectiveArgv returns the command actually executed, dropping the elevation
// prefix that RunElevated adds only where the platform needs it.
func effectiveArgv(call run.FakeCall) []string {
	if call.Name == "sudo" {
		return call.Args
	}
	return append([]string{call.Name}, call.Args...)
}

func TestInstallResolvedRejectsArtifactIdentityMismatchBeforeDpkg(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured map[string]any
	}{
		{name: "package", configured: map[string]any{"pkg": "other", "version": "1:0.18.2-1", "architecture": "amd64"}},
		{name: "version", configured: map[string]any{"pkg": "delta", "version": "0.19.0", "architecture": "amd64"}},
		{name: "architecture", configured: map[string]any{"pkg": "delta", "version": "1:0.18.2-1", "architecture": "arm64"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("deb payload")) }))
			defer server.Close()
			runner := &run.FakeRunner{Stdout: "Package: delta\nVersion: 1:0.18.2-1\nArchitecture: amd64\n"}
			resolved := plan.New("delta", "deb", true)
			resolved.Artifacts = []plan.Artifact{{URL: server.URL + "/delta.deb"}}
			err := NewAdapter().InstallResolved(context.Background(), runner, &config.Tool{Name: "delta"}, &config.MethodCandidate{Config: tc.configured}, &resolved)
			if err == nil {
				t.Fatal("InstallResolved() succeeded for mismatched metadata")
			}
			if len(runner.Calls) != 1 || runner.Calls[0].Name != "dpkg-deb" {
				t.Fatalf("calls = %#v; dpkg must not be invoked", runner.Calls)
			}
		})
	}
}
