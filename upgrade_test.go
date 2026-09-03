package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/lock"
	"github.com/Khorea1/depengine/pkg/state"

	"github.com/pelletier/go-toml/v2"
)

// writeTestLock writes a depengine.lock file in the schema directory.
func writeTestLock(t *testing.T, schemaDir string, tools map[string]lock.ToolPin) {
	t.Helper()
	lk := &lock.Lock{
		Version: 1,
		Tools:   tools,
	}
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(lk); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(schemaDir, "depengine.lock")
	if err := os.WriteFile(path, []byte(buf.String()), 0600); err != nil {
		t.Fatal(err)
	}
}

// writeTestSchema writes a minimal schema.toml with go tools.
func writeTestSchema(t *testing.T, dir string, tools map[string]string) {
	t.Helper()
	content := "[defaults]\nmethod_order = [\"go\", \"native\"]\n\n"
	for name, pkg := range tools {
		content += "[tools." + name + "]\n" +
			"go = \"" + pkg + "\"\n"
	}
	path := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// runUpgradeCommand is a wrapper around runCommand that passes flags via
// the DEPENGINE_TEST_ARGS env var (US-separated, \x1f) to avoid collision with
// the Go test binary's own flag parser.
func runUpgradeCommand(t *testing.T, extraEnv []string, flags ...string) (int, string) {
	t.Helper()
	upgradeEnv := append(extraEnv, "DEPENGINE_TEST_ARGS="+strings.Join(flags, "\x1f"))
	return runCommand(t, "upgrade", upgradeEnv)
}

// TestUpgradeNoLockfile exits 1 with a clear message when no lock exists.
func TestUpgradeNoLockfile(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestState(t, stateHome, map[string]state.ToolState{
		"gostr": {
			Method:      "go",
			MethodKind:  "go",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "v0.1.0",
			Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-force",
	)

	if code != 1 {
		t.Fatalf("upgrade exit = %d, want 1 (output: %s)", code, out)
	}
	if !strings.Contains(out, "No lockfile found") {
		t.Fatalf("output should mention missing lockfile, got: %s", out)
	}
}

// TestUpgradeNothingOutdated exits 0 with "up to date" when versions match.
func TestUpgradeNothingOutdated(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go": {Latest: "v0.1.0"},
	})
	writeTestState(t, stateHome, map[string]state.ToolState{
		"gostr": {
			Method:      "go",
			MethodKind:  "go",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "v0.1.0",
			Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-force",
	)

	if code != 0 {
		t.Fatalf("upgrade exit = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "up to date") {
		t.Fatalf("output should say 'up to date', got: %s", out)
	}
}

// TestUpgradeDryRun shows what would be upgraded without touching state.
func TestUpgradeDryRun(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go": {Latest: "v0.2.0"},
	})
	writeTestState(t, stateHome, map[string]state.ToolState{
		"gostr": {
			Method:      "go",
			MethodKind:  "go",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "v0.1.0",
			Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-dry-run",
	)

	if code != 0 {
		t.Fatalf("upgrade dry-run exit = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "gostr") {
		t.Fatalf("output should mention gostr, got: %s", out)
	}
	if !strings.Contains(out, "dry-run") && !strings.Contains(out, "would") {
		t.Fatalf("output should mention dry-run/would_upgrade, got: %s", out)
	}

	// State must be unchanged.
	st := loadTestState(t, stateHome)
	if ts, ok := st.Tools["gostr"]; !ok || ts.Version != "v0.1.0" {
		t.Fatalf("state changed after dry-run: %+v", st.Tools)
	}
}

// TestUpgradeSkipsUnknownVersion skips tools with empty version.
func TestUpgradeSkipsUnknownVersion(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go": {Latest: "v0.2.0"},
	})
	writeTestState(t, stateHome, map[string]state.ToolState{
		"gostr": {
			Method:      "go",
			MethodKind:  "go",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "",
			Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-force",
	)

	if code != 0 {
		t.Fatalf("upgrade exit = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "up to date") {
		t.Fatalf("output should say 'up to date' (unknown version skipped), got: %s", out)
	}
}

// TestUpgradeOnlyFlag filters to a single tool.
func TestUpgradeOnlyFlag(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{
		"gostr":   "golang.org/x/tools/cmd/stringer",
		"gotool2": "golang.org/x/tools/cmd/guru",
	})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go":   {Latest: "v0.2.0"},
		"gotool2/go": {Latest: "v0.3.0"},
	})
	writeTestState(t, stateHome, map[string]state.ToolState{
		"gostr": {
			Method:      "go",
			MethodKind:  "go",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "v0.1.0",
			Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"},
		},
		"gotool2": {
			Method:      "go",
			MethodKind:  "go",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "v0.2.0",
			Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/guru"},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-only", "gostr",
		"-dry-run",
	)

	if code != 0 {
		t.Fatalf("upgrade --only exit = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "gostr") {
		t.Fatalf("output should mention gostr, got: %s", out)
	}
	if strings.Contains(out, "gotool2") {
		t.Fatalf("output should NOT mention gotool2, got: %s", out)
	}
}

// TestUpgradeJSONOutput produces valid JSON with correct counts.
func TestUpgradeJSONOutput(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go": {Latest: "v0.2.0"},
	})
	writeTestState(t, stateHome, map[string]state.ToolState{
		"gostr": {
			Method:      "go",
			MethodKind:  "go",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "v0.1.0",
			Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-dry-run",
		"-json",
	)

	if code != 0 {
		t.Fatalf("upgrade --json exit = %d, want 0 (output: %s)", code, out)
	}
	// JSON output may have a human-readable header line before the JSON
	// block. Extract the JSON portion (starts at first '{').
	jsonStart := strings.Index(out, "{")
	if jsonStart < 0 {
		t.Fatalf("no JSON in output: %s", out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out[jsonStart:]), &result); err != nil {
		t.Fatalf("invalid JSON output: %v (raw: %s)", err, out)
	}
	if result["upgraded"].(float64) != 0 {
		t.Fatalf("upgraded = %v, want 0 (dry-run)", result["upgraded"])
	}
	results := result["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	r := results[0].(map[string]any)
	if r["status"] != "would_upgrade" {
		t.Fatalf("status = %v, want would_upgrade", r["status"])
	}
}

// TestUpgradeHTTPToolFailsOnDownload tests that an HTTP tool with an
// unreachable URL fails during upgrade (Remove succeeds, Install fails
// because the download URL is invalid).
func TestUpgradeHTTPToolFailsOnDownload(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	// Create a /bin-suffixed dir so isSharedDir returns true.
	sharedDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "httptool"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	content := "[defaults]\nmethod_order = [\"http\", \"native\"]\n\n" +
		"[tools.httptool]\n" +
		"http = {url = \"https://example.invalid/tool.tar.gz\", extract_to = \"" + sharedDir + "\"}\n"
	if err := os.WriteFile(filepath.Join(schemaDir, "schema.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"httptool/http": {Latest: "v2.0.0"},
	})
	writeTestState(t, stateHome, map[string]state.ToolState{
		"httptool": {
			Method:      "http",
			MethodKind:  "http",
			InstalledAt: "2026-08-01T00:00:00Z",
			Version:     "v1.0.0",
			Config:      map[string]any{"url": "https://example.invalid/tool.tar.gz", "extract_to": sharedDir},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-force",
	)

	// Upgrade fails (non-zero exit) because the download URL is unreachable.
	if code == 0 {
		t.Fatalf("upgrade exit = 0, want non-zero (output: %s)", out)
	}
	if !strings.Contains(out, "failed") {
		t.Fatalf("output should mention failed tool, got: %s", out)
	}
}
