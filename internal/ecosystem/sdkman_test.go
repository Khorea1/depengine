package ecosystem

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/run"
)

func sdkmanTestInit(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	exectest.SetHome(t, home)
	t.Setenv("SDKMAN_DIR", "")
	initScript := filepath.Join(home, ".sdkman", "bin", "sdkman-init.sh")
	if err := os.MkdirAll(filepath.Dir(initScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initScript, []byte("# test sdkman init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return initScript
}

func TestSDKManAvailableUsesInitScriptAndBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDKMAN layout is Unix-specific")
	}
	sdkmanTestInit(t)
	runner := &run.FakeRunner{LookPaths: map[string]bool{"bash": true}}
	if !NewSDKManAdapter().Available(context.Background(), runner) {
		t.Fatal("Available should accept SDKMAN init script with bash")
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "which" || len(runner.Calls[0].Args) != 1 || runner.Calls[0].Args[0] != "bash" {
		t.Fatalf("Available calls = %#v, want which bash", runner.Calls)
	}
}

func TestSDKManCheckExactVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDKMAN layout is Unix-specific")
	}
	home := t.TempDir()
	exectest.SetHome(t, home)

	versionDir := filepath.Join(home, ".sdkman", "candidates", "java", "21.0.4-tem")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewSDKManAdapter()
	tool := &config.Tool{Name: "java"}
	mc := &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{
		"pkg": "java", "version": "21.0.4-tem",
	}}
	if !adapter.Check(context.Background(), &run.FakeRunner{}, tool, mc) {
		t.Fatal("Check should accept the explicitly requested installed version")
	}
}

func TestSDKManCheckExactVersionRejectsDifferentInstalledVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDKMAN layout is Unix-specific")
	}
	home := t.TempDir()
	exectest.SetHome(t, home)

	otherVersionDir := filepath.Join(home, ".sdkman", "candidates", "java", "17.0.12-tem")
	if err := os.MkdirAll(otherVersionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(home, ".sdkman", "candidates", "java", "current")
	if err := os.Symlink(otherVersionDir, current); err != nil {
		t.Fatal(err)
	}

	adapter := NewSDKManAdapter()
	tool := &config.Tool{Name: "java"}
	mc := &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{
		"pkg": "java", "version": "21.0.4-tem",
	}}
	if adapter.Check(context.Background(), &run.FakeRunner{}, tool, mc) {
		t.Fatal("Check must not treat a different SDKMAN version as satisfying an exact request")
	}
}

func TestSDKManCheckWithoutVersionKeepsCurrentSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDKMAN layout is Unix-specific")
	}
	home := t.TempDir()
	exectest.SetHome(t, home)

	versionDir := filepath.Join(home, ".sdkman", "candidates", "java", "17.0.12-tem")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(home, ".sdkman", "candidates", "java", "current")
	if err := os.Symlink(versionDir, current); err != nil {
		t.Fatal(err)
	}

	adapter := NewSDKManAdapter()
	tool := &config.Tool{Name: "java"}
	mc := &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{"pkg": "java"}}
	if !adapter.Check(context.Background(), &run.FakeRunner{}, tool, mc) {
		t.Fatal("Check without version should continue to accept an existing current SDK")
	}
}

func TestSDKManInstallUsesExactVersion(t *testing.T) {
	initScript := sdkmanTestInit(t)
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewSDKManAdapter()
	tool := &config.Tool{Name: "java"}
	mc := &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{
		"pkg": "java", "version": "21.0.4-tem",
	}}

	if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatal(err)
	}
	if len(fr.Calls) == 0 {
		t.Fatal("expected SDKMAN bash invocation")
	}
	got := fr.Calls[len(fr.Calls)-1]
	want := []string{"-c", sdkmanShellCommand, "depengine-sdkman", initScript, "install", "java", "21.0.4-tem"}
	if got.Name != "bash" || !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("install command = %s %v, want bash %v", got.Name, got.Args, want)
	}
}

func TestSDKManInstalledVersionExact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDKMAN layout is Unix-specific")
	}
	home := t.TempDir()
	exectest.SetHome(t, home)
	version := "21.0.4-tem"
	if err := os.MkdirAll(filepath.Join(home, ".sdkman", "candidates", "java", version), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := NewSDKManAdapter().InstalledVersion(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "java"}, &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{"pkg": "java", "version": version}})
	if err != nil {
		t.Fatalf("InstalledVersion: %v", err)
	}
	if got != version {
		t.Fatalf("InstalledVersion = %q, want %q", got, version)
	}
}

func TestSDKManInstalledVersionCurrentSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDKMAN layout is Unix-specific")
	}
	home := t.TempDir()
	exectest.SetHome(t, home)
	version := "17.0.12-tem"
	versionDir := filepath.Join(home, ".sdkman", "candidates", "java", version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versionDir, filepath.Join(home, ".sdkman", "candidates", "java", "current")); err != nil {
		t.Fatal(err)
	}
	got, err := NewSDKManAdapter().InstalledVersion(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "java"}, &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{"pkg": "java"}})
	if err != nil {
		t.Fatalf("InstalledVersion: %v", err)
	}
	if got != version {
		t.Fatalf("InstalledVersion = %q, want %q", got, version)
	}
}
