package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"

	"github.com/pelletier/go-toml/v2"
)

func TestFindMethodCandidateRespectsLabelSelection(t *testing.T) {
	tool := &config.Tool{
		MethodOnly: []string{"gh_linux"},
		Methods: []*config.MethodCandidate{
			{Kind: "github", Label: "gh_apk"},
			{Kind: "github", Label: "gh_linux"},
		},
	}
	got := findMethodCandidate(tool, "github", []string{"github"}, "")
	if got == nil || got.Label != "gh_linux" {
		t.Fatalf("findMethodCandidate() = %+v, want gh_linux", got)
	}
}

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
	content := "schema_version = 1\n\n[defaults]\nmethod_order = [\"go\", \"native\"]\n\n"
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
	upgradeEnv := append(append([]string(nil), extraEnv...), "DEPENGINE_TEST_ARGS="+strings.Join(flags, "\x1f"))
	return runCommand(t, "upgrade", upgradeEnv)
}

// writeFakeUpgradeBinaries creates executable stubs for the named binaries and
// returns a PATH assignment exposing them to helper subprocesses. Upgrade
// preflight requires the tracked installation to be present on the host;
// without the stub the fixture would describe an absent tool and upgrade
// must fail closed ("run install/repair") instead of destroying state.
func writeFakeUpgradeBinaries(t *testing.T, names ...string) string {
	t.Helper()
	binDir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
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
		"gostr/go/0": {Latest: "v0.1.0"},
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
	// The tracked tool must be present on the host: upgrade preflight fails
	// closed for absent installations instead of a destructive Remove+Install.
	pathEnv := writeFakeUpgradeBinaries(t, "gostr")

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go/0": {Latest: "v0.2.0"},
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
			pathEnv,
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

	// State must be unchanged and dry-run must not create the state lock file.
	st := loadTestState(t, stateHome)
	if ts, ok := st.Tools["gostr"]; !ok || ts.Version != "v0.1.0" {
		t.Fatalf("state changed after dry-run: %+v", st.Tools)
	}
	if _, err := os.Stat(filepath.Join(stateHome, "depengine", "state.json.lock")); !os.IsNotExist(err) {
		t.Fatalf("upgrade --dry-run created state lock file (err=%v)", err)
	}
}

// TestUpgradeFailsClosedWhenTrackedToolAbsent pins the preflight boundary:
// state may claim an installation the host no longer has, and upgrade must
// refuse the destructive Remove+Install with an actionable message.
func TestUpgradeFailsClosedWhenTrackedToolAbsent(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go/0": {Latest: "v0.2.0"},
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

	if code == 0 {
		t.Fatalf("upgrade of absent tool exit = %d, want non-zero (output: %s)", code, out)
	}
	if !strings.Contains(out, "run install/repair") {
		t.Fatalf("output should direct to install/repair, got: %s", out)
	}
}

// TestUpgradeSkipsUnknownVersion skips tools with empty version.
func TestUpgradeSkipsUnknownVersion(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go/0": {Latest: "v0.2.0"},
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
	pathEnv := writeFakeUpgradeBinaries(t, "gostr")

	writeTestSchema(t, schemaDir, map[string]string{
		"gostr":   "golang.org/x/tools/cmd/stringer",
		"gotool2": "golang.org/x/tools/cmd/guru",
	})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go/0":   {Latest: "v0.2.0"},
		"gotool2/go/0": {Latest: "v0.3.0"},
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
			pathEnv,
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
	pathEnv := writeFakeUpgradeBinaries(t, "gostr")

	writeTestSchema(t, schemaDir, map[string]string{"gostr": "golang.org/x/tools/cmd/stringer"})
	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"gostr/go/0": {Latest: "v0.2.0"},
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
			pathEnv,
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

	content := "schema_version = 1\n\n[defaults]\nmethod_order = [\"http\", \"native\"]\n\n" +
		"[tools.httptool]\n" +
		"http = {url = \"https://example.invalid/tool.tar.gz\", extract_to = \"" + sharedDir + "\"}\n"
	if err := os.WriteFile(filepath.Join(schemaDir, "schema.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	writeTestLock(t, schemaDir, map[string]lock.ToolPin{
		"httptool/http/0": {Latest: "v2.0.0"},
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

type upgradePreflightAdapter struct {
	available       bool
	installed       bool
	targetAvailable bool
	canRemove       bool
	calls           []string
}

func (a *upgradePreflightAdapter) Kind() string { return "native" }
func (a *upgradePreflightAdapter) Available(context.Context, run.Runner) bool {
	a.calls = append(a.calls, "available")
	return a.available
}
func (a *upgradePreflightAdapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.calls = append(a.calls, "check")
	return a.installed
}
func (a *upgradePreflightAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.calls = append(a.calls, "check-available")
	return a.targetAvailable
}
func (a *upgradePreflightAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	a.calls = append(a.calls, "install")
	return nil
}
func (a *upgradePreflightAdapter) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	a.calls = append(a.calls, "remove")
	return nil
}
func (a *upgradePreflightAdapter) CanRemove() bool { return a.canRemove }

func TestLockPinForRejectsAmbiguousKind(t *testing.T) {
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}
	if _, ok := lockPinFor(lk, "demo", "http"); ok {
		t.Fatal("lockPinFor accepted ambiguous same-kind pins")
	}
}

func TestLockPinForCandidateUsesKindOrdinal(t *testing.T) {
	first := &config.MethodCandidate{Kind: "http", Label: "primary"}
	other := &config.MethodCandidate{Kind: "git", Label: "source"}
	second := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{first, other, second}}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}

	pin, ok := lockPinForCandidate(lk, "demo", tool, second)
	if !ok || pin.Latest != "v2" {
		t.Fatalf("lockPinForCandidate(second) = %+v, %v; want v2, true", pin, ok)
	}
}

func TestFindTrackedMethodCandidateUsesPersistedLabel(t *testing.T) {
	primary := &config.MethodCandidate{Kind: "http", Label: "primary"}
	mirror := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{primary, mirror}}

	got, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "mirror", MethodKind: "http"}, []string{"http"}, "")
	if err != nil {
		t.Fatalf("findTrackedMethodCandidate: %v", err)
	}
	if got != mirror {
		t.Fatalf("findTrackedMethodCandidate = %+v, want mirror", got)
	}
}

func TestFindTrackedMethodCandidateLegacyKindFailsClosedWhenAmbiguous(t *testing.T) {
	tool := &config.Tool{Methods: []*config.MethodCandidate{
		{Kind: "http", Label: "primary"},
		{Kind: "http", Label: "mirror"},
	}}

	_, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "http", MethodKind: "http"}, []string{"http"}, "")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("findTrackedMethodCandidate error = %v, want ambiguous", err)
	}
}

func TestFindTrackedMethodCandidateLegacyKindResolvesSingleLabeledCandidate(t *testing.T) {
	candidate := &config.MethodCandidate{Kind: "http", Label: "primary"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{candidate}}

	got, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "http", MethodKind: "http"}, []string{"http"}, "")
	if err != nil {
		t.Fatalf("findTrackedMethodCandidate: %v", err)
	}
	if got != candidate {
		t.Fatalf("findTrackedMethodCandidate = %+v, want candidate", got)
	}
}

func TestPreflightDirectUpgradeRejectsPreparationBeforeProbes(t *testing.T) {
	adapter := &upgradePreflightAdapter{available: true, installed: true, targetAvailable: true, canRemove: true}
	method := &config.MethodCandidate{
		Kind:    "native",
		Config:  map[string]any{"pkg": "demo"},
		Sources: []config.Source{{Kind: "apt", Name: "demo"}},
	}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}

	err := preflightDirectUpgrade(context.Background(), &run.FakeRunner{}, &engine.Facts{}, tool, method, adapter, false)
	if err == nil || !strings.Contains(err.Error(), "transactional upgrade preparation") {
		t.Fatalf("preflightDirectUpgrade error = %v, want preparation rejection", err)
	}
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter calls = %v, want no host probes after static rejection", adapter.calls)
	}
}

func TestPreflightDirectUpgradeRequiresInstalledRemovableAvailableTarget(t *testing.T) {
	method := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo"}}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}

	tests := []struct {
		name      string
		adapter   upgradePreflightAdapter
		wantErr   string
		wantCalls []string
	}{
		{name: "adapter unavailable", adapter: upgradePreflightAdapter{}, wantErr: "unavailable", wantCalls: []string{"available"}},
		{name: "installation missing", adapter: upgradePreflightAdapter{available: true}, wantErr: "not present", wantCalls: []string{"available", "check"}},
		{name: "target unavailable", adapter: upgradePreflightAdapter{available: true, installed: true}, wantErr: "not available", wantCalls: []string{"available", "check", "check-available"}},
		{name: "removal unsupported", adapter: upgradePreflightAdapter{available: true, installed: true, targetAvailable: true}, wantErr: "does not support removal", wantCalls: []string{"available", "check", "check-available"}},
		{name: "valid", adapter: upgradePreflightAdapter{available: true, installed: true, targetAvailable: true, canRemove: true}, wantCalls: []string{"available", "check", "check-available"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := preflightDirectUpgrade(context.Background(), &run.FakeRunner{}, &engine.Facts{}, tool, method, &tt.adapter, false)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("preflightDirectUpgrade: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("preflightDirectUpgrade error = %v, want substring %q", err, tt.wantErr)
			}
			if strings.Join(tt.adapter.calls, ",") != strings.Join(tt.wantCalls, ",") {
				t.Fatalf("adapter calls = %v, want %v", tt.adapter.calls, tt.wantCalls)
			}
		})
	}
}

func TestFindTrackedMethodCandidateRejectsCandidateExcludedByCurrentPolicy(t *testing.T) {
	primary := &config.MethodCandidate{Kind: "http", Label: "primary"}
	mirror := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{
		Methods:    []*config.MethodCandidate{primary, mirror},
		MethodOnly: []string{"primary"},
	}

	_, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "mirror", MethodKind: "http"}, []string{"http"}, "")
	if err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("findTrackedMethodCandidate error = %v, want current-policy rejection", err)
	}
}

func TestUpgradedToolStatePreservesRootIntent(t *testing.T) {
	previous := state.ToolState{
		Method: "primary", MethodKind: "go", PostinstallDone: true,
		Version: "v1", RootRequested: true, Config: map[string]any{"pkg": "old"},
	}
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/cmd/demo"}}
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	got := upgradedToolState(previous, "go", tool, method, "", "v2", at)
	if !got.RootRequested {
		t.Fatal("upgraded state cleared root_requested")
	}
	if got.Version != "v2" || got.Method != "primary" || got.MethodKind != "go" || !got.PostinstallDone {
		t.Fatalf("upgraded state = %#v, want preserved metadata and pinned fallback version", got)
	}
	if !reflect.DeepEqual(got.Config, method.Config) {
		t.Fatalf("upgraded config = %#v, want %#v", got.Config, method.Config)
	}
}

func TestRecordFailedUpgradeRemovalReleasesClaimsWithoutCleaningResources(t *testing.T) {
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	st := &state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"owner":  {Method: "go", RootRequested: true},
			"helper": {Method: "go"},
		},
		OwnedResources: []plan.OwnedResourceState{{
			Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"owner"},
		}},
	}
	if err := recordFailedUpgradeRemoval(st, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Tools["owner"]; ok {
		t.Fatalf("failed-upgrade owner still tracked: %#v", st.Tools)
	}
	if _, ok := st.Tools["helper"]; !ok {
		t.Fatalf("helper unexpectedly removed: %#v", st.Tools)
	}
	if len(st.OwnedResources) != 1 || st.OwnedResources[0].RefCount() != 0 || st.OwnedResources[0].Resource != resource {
		t.Fatalf("owned resources = %#v, want retained zero-ref helper", st.OwnedResources)
	}
}

func TestPreflightDirectUpgradeRejectsStaticRequiresBeforeProbes(t *testing.T) {
	adapter := &upgradePreflightAdapter{available: true, installed: true, targetAvailable: true, canRemove: true}
	method := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo"}}
	tool := &config.Tool{Name: "demo", Requires: []string{"helper"}, Methods: []*config.MethodCandidate{method}}

	err := preflightDirectUpgrade(context.Background(), &run.FakeRunner{}, &engine.Facts{}, tool, method, adapter, false)
	if err == nil || !strings.Contains(err.Error(), "transactional upgrade dependency handling") {
		t.Fatalf("preflightDirectUpgrade error = %v, want static requires rejection", err)
	}
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter calls = %v, want no host probes after static rejection", adapter.calls)
	}
}
