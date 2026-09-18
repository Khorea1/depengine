package ecosystem

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

func TestSDKManCheckExactVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDKMAN layout is Unix-specific")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

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
	t.Setenv("HOME", home)

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
	t.Setenv("HOME", home)

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
		t.Fatal("expected sdk install command")
	}
	got := fr.Calls[len(fr.Calls)-1]
	if got.Name != "sdk" || len(got.Args) != 3 || got.Args[0] != "install" || got.Args[1] != "java" || got.Args[2] != "21.0.4-tem" {
		t.Fatalf("install command = %s %v, want sdk install java 21.0.4-tem", got.Name, got.Args)
	}
}
