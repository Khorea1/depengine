package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/lock"
	deplog "github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
)

func TestLegacyNativeLockFallbackOnlyAcceptsNativeUnpinnableTools(t *testing.T) {
	nativeErr := &plan.LockUnavailableToolsError{Tools: []plan.LockUnavailableTool{
		{Tool: "zsh", Method: "native", Reason: "manager did not expose a stable resolved identity"},
	}}

	tests := []struct {
		name     string
		previous *lock.Lock
		err      error
		want     []string
		ok       bool
	}{
		{name: "missing previous lock", err: nativeErr, want: []string{"zsh"}, ok: true},
		{name: "v1 previous lock", previous: &lock.Lock{Version: 1}, err: nativeErr, want: []string{"zsh"}, ok: true},
		{name: "v2 previous lock is never downgraded", previous: &lock.Lock{Version: lock.CurrentVersion}, err: nativeErr},
		{name: "non-native method stays hard failure", err: &plan.LockUnavailableToolsError{Tools: []plan.LockUnavailableTool{
			{Tool: "demo", Method: "http", Reason: "no checksum"},
		}}},
		{name: "mixed native and non-native stays hard failure", err: &plan.LockUnavailableToolsError{Tools: []plan.LockUnavailableTool{
			{Tool: "zsh", Method: "native", Reason: "no identity"},
			{Tool: "demo", Method: "http", Reason: "no checksum"},
		}}},
		{name: "manager alias is native", err: &plan.LockUnavailableToolsError{Tools: []plan.LockUnavailableTool{
			{Tool: "zsh", Method: "apt", Reason: "no identity"},
			{Tool: "zsh-extra", Method: "port", Reason: "no identity"},
		}}, want: []string{"zsh", "zsh-extra"}, ok: true},
		{name: "winget has its own pinning contract", err: &plan.LockUnavailableToolsError{Tools: []plan.LockUnavailableTool{
			{Tool: "demo", Method: "winget", Reason: "no version"},
		}}},
		{name: "untyped error", err: errors.New("boom")},
		{name: "empty unavailable set", err: &plan.LockUnavailableToolsError{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := legacyNativeLockFallback(tt.previous, tt.err)
			if ok != tt.ok {
				t.Fatalf("legacyNativeLockFallback() ok = %v, want %v", ok, tt.ok)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("legacyNativeLockFallback() names = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func writeExecutableFixture(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // executable test fixture, not production data
		t.Fatal(err)
	}
}

func TestRunUpdateFallsBackToV1ForNativeOnlySchema(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake native manager fixture is a POSIX shell script")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pacman := filepath.Join(binDir, "pacman")
	writeExecutableFixture(t, pacman, `#!/bin/sh
case "$1" in
  -Si) exit 0 ;;
  -Qi) printf 'Name            : zsh
Version         : 5.9
' ; exit 0 ;;
  *) exit 0 ;;
esac
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	detector := filepath.Join(dir, "detect.sh")
	writeExecutableFixture(t, detector, `#!/bin/sh
cat <<'EOF'
{"target_arch":"x86_64","distro_id":"arch","distro_name":"Arch Linux","distro_version":"2026.09","distro_id_like":"","target_family":"unix","detection_method":"os-release","confidence":"high","is_wsl":false,"is_container":false,"is_android":false}
EOF
`)
	t.Setenv("DEPENGINE_DETECT_SCRIPT", detector)

	schemaPath := writeUpdateTestSchema(t, dir, `schema_version = 1

[tools.zsh]
method_only = ["native"]

[tools.zsh.native]
pkg = "zsh"
`)

	var logs bytes.Buffer
	previousLogger := deplog.Default
	deplog.Default = deplog.New(&logs, deplog.LevelDebug)
	defer func() { deplog.Default = previousLogger }()

	if err := runTestUpdateResult(t, schemaPath, ""); err != nil {
		t.Fatalf("runUpdate native fallback: %v", err)
	}
	if text := logs.String(); !strings.Contains(text, "using legacy v1 lock") || !strings.Contains(text, "zsh") {
		t.Fatalf("update logs = %q, want native fallback warning naming zsh", text)
	}

	got, err := lock.Load(lock.DefaultPath(schemaPath))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("runUpdate did not write a lock")
	}
	if got.Version != 1 {
		t.Fatalf("lock version = %d, want 1", got.Version)
	}
	if got.UniversalProjection != "" {
		t.Fatalf("universal projection = %q, want none", got.UniversalProjection)
	}
}
