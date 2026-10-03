package exec_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

func TestExecuteAURInstallPersistsConfiguredProvider(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("AUR helper lifecycle is Linux-specific")
	}

	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	helperDir := t.TempDir()
	installedPath := filepath.Join(t.TempDir(), "installed")
	callsPath := filepath.Join(t.TempDir(), "helper-calls")
	t.Setenv("AUR_TEST_INSTALLED", installedPath)
	t.Setenv("AUR_TEST_CALLS", callsPath)
	t.Setenv("PATH", helperDir)

	helper := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$AUR_TEST_CALLS"
case "$1" in
  --version) printf 'yay fixture\n' ;;
  -Qi)
    if [ -f "$AUR_TEST_INSTALLED" ]; then
      printf 'Name : %s\n' "$2"
    else
      exit 1
    fi
    ;;
  -S)
    test "$2" = "--noconfirm"
    : > "$AUR_TEST_INSTALLED"
    ;;
  -Rns)
    test "$2" = "--noconfirm"
    /bin/rm -f "$AUR_TEST_INSTALLED"
    ;;
  *) printf 'unexpected helper arguments: %s\n' "$*" >&2; exit 64 ;;
esac
`
	if err := os.WriteFile(filepath.Join(helperDir, "yay"), []byte(helper), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatalf("write controlled yay helper: %v", err)
	}

	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(schemaPath, []byte("schema_version = 1\n"), 0o600); err != nil {
		t.Fatalf("write schema marker: %v", err)
	}
	schema := &config.Schema{
		Defaults: config.Defaults{AurHelper: "yay", MethodOrder: []string{"aur"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name:    "demo",
				Methods: []*config.MethodCandidate{{Kind: "aur", Config: map[string]any{"pkg": "demo-aur-fixture"}}},
			},
		},
	}
	executor := exec.New()
	exec.WithAdapters(ecosystem.NewAURAdapter("yay"))(executor)
	exec.WithRunner(run.OSExecRunner{})(executor)
	exec.WithSchemaInfo(schemaPath, time.Now())(executor)

	report, err := executor.Execute(context.Background(), schema, "arch")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.Failed != 0 || report.Success != 1 {
		t.Fatalf("Execute() report = %+v, want one successful install", report)
	}
	if _, err := os.Stat(installedPath); err != nil {
		t.Fatalf("controlled helper did not create install marker: %v", err)
	}

	installed, err := state.Load()
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}
	toolState, ok := installed.Tools["demo"]
	if !ok {
		t.Fatal("installed tool is missing from persisted state")
	}
	if toolState.MethodKind != "aur" || toolState.Provider != "yay" {
		t.Fatalf("persisted tool state = %+v, want MethodKind aur and Provider yay", toolState)
	}
	calls, err := os.ReadFile(callsPath) // #nosec G304 -- callsPath is created inside this test’s t.TempDir.
	if err != nil {
		t.Fatalf("read controlled helper calls: %v", err)
	}
	if !strings.Contains(string(calls), "-S --noconfirm demo-aur-fixture") {
		t.Fatalf("helper calls = %q, want actual yay install", calls)
	}
}
