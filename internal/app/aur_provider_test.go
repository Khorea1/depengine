package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/state"
)

func TestRunRemoveAURWithoutSchemaUsesPersistedProvider(t *testing.T) {
	installedPath, callsPath := setupAURHelperFixture(t, "yay", true)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"demo": {
			Method: "yay", MethodKind: "aur", Provider: "yay",
			Config: map[string]any{"pkg": "demo-aur-fixture"},
		},
	})

	executor := exec.New()
	all, dryRun, force := false, false, false
	schema, only := "", "demo"
	if err := runRemoveWithExecutor(context.Background(), nil, &all, &dryRun, &schema, &only, &force, executor, nil); err != nil {
		t.Fatalf("runRemove without schema: %v", err)
	}
	if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
		t.Fatalf("AUR package marker still exists after remove, stat error = %v", err)
	}
	if _, ok := loadTestState(t, stateHome).Tools["demo"]; ok {
		t.Fatal("removed AUR tool remained in persisted state")
	}
	calls, err := os.ReadFile(callsPath) // #nosec G304 -- callsPath is created inside this test’s t.TempDir.
	if err != nil {
		t.Fatalf("read helper calls: %v", err)
	}
	if !strings.Contains(string(calls), "-Rns --noconfirm demo-aur-fixture") {
		t.Fatalf("helper calls = %q, want the saved yay helper to remove the package", calls)
	}
}

func TestUndoLegacyAURWithoutProviderDoesNotCallArbitraryHelper(t *testing.T) {
	installedPath, callsPath := setupAURHelperFixture(t, "arbitrary-helper", true)
	curState := &state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"demo": {
				Method: "aur", MethodKind: "aur",
				Config: map[string]any{"pkg": "demo-aur-fixture"},
			},
		},
	}
	executor := exec.New()
	exec.WithAdapters(ecosystem.NewAURAdapter("arbitrary-helper"))(executor)

	_, removed, hadFailure := removeUndoTools(context.Background(), []string{"demo"}, curState, executor, "")
	if !hadFailure {
		t.Fatal("undo of legacy AUR state without Provider succeeded; want fail-closed failure")
	}
	if removed["demo"] {
		t.Fatal("undo marked the AUR tool removed without a persisted Provider")
	}
	if _, err := os.Stat(installedPath); err != nil {
		t.Fatalf("legacy AUR tool was mutated despite missing Provider: %v", err)
	}
	if _, err := os.Stat(callsPath); !os.IsNotExist(err) {
		calls, _ := os.ReadFile(callsPath) // #nosec G304 -- callsPath is created inside this test’s t.TempDir.
		t.Fatalf("undo attempted arbitrary AUR helper: calls = %q, stat error = %v", calls, err)
	}
}

func setupAURHelperFixture(t *testing.T, helperName string, installed bool) (installedPath, callsPath string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("AUR helper lifecycle is Linux-specific")
	}

	helperDir := t.TempDir()
	installedPath = filepath.Join(t.TempDir(), "installed")
	callsPath = filepath.Join(t.TempDir(), "helper-calls")
	t.Setenv("AUR_TEST_INSTALLED", installedPath)
	t.Setenv("AUR_TEST_CALLS", callsPath)
	t.Setenv("PATH", helperDir)
	if installed {
		if err := os.WriteFile(installedPath, []byte("installed\n"), 0o600); err != nil {
			t.Fatalf("write installed marker: %v", err)
		}
	}

	helper := `#!/bin/sh
set -eu
printf '%s %s\n' "$0" "$*" >> "$AUR_TEST_CALLS"
case "$1" in
  --version) printf 'AUR helper fixture\n' ;;
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
	if err := os.WriteFile(filepath.Join(helperDir, helperName), []byte(helper), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatalf("write controlled %s helper: %v", helperName, err)
	}
	return installedPath, callsPath
}
