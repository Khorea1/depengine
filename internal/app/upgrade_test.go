package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"

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

// writeFakeUpgradeBinaries creates Go binaries whose build metadata matches a
// real versioned module install. Upgrade preflight now inspects `go version -m`
// instead of executing the installed program, so a command-line-arguments stub
// would correctly be treated as an unknown installation identity.
func writeFakeUpgradeBinaries(t *testing.T, names ...string) []string {
	t.Helper()
	// The Go command defaults telemetry to local mode and may keep its mapped
	// counter file active briefly after the command exits on BSD. Redirect its
	// test-only telemetry directory and pin the mode to off so TempDir cleanup
	// cannot race a late write under HOME/.config/go/telemetry.
	telemetryDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(telemetryDir, "mode"), []byte("off"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_TELEMETRY_DIR", telemetryDir)
	binDir := t.TempDir()

	moduleDir := t.TempDir()
	cmdDir := filepath.Join(moduleDir, "cmd", "stringer")
	if err := os.MkdirAll(cmdDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module golang.org/x/tools\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const body = `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	buildDir := t.TempDir()
	buildMod := "module depengine.test/upgradefixture\n\ngo 1.27\n\nrequire golang.org/x/tools v0.1.0\n\nreplace golang.org/x/tools => " + filepath.ToSlash(moduleDir) + "\n"
	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte(buildMod), 0600); err != nil {
		t.Fatal(err)
	}

	for _, name := range names {
		// Presence is resolved through PATH lookup, which needs the .exe
		// suffix on Windows.
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(binDir, name)
		res := (run.OSExecRunner{}).RunInDir(context.Background(), buildDir, "go", "build", "-o", path, "golang.org/x/tools/cmd/stringer")
		if err := run.CheckResult(res, "go build fake upgrade binary"); err != nil {
			t.Fatalf("build fake upgrade binary %s: %v", name, err)
		}
	}
	return []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GOBIN=" + binDir,
		"TEST_TELEMETRY_DIR=" + telemetryDir,
	}
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
	goEnv := writeFakeUpgradeBinaries(t, "gostr", "stringer")

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

	upgradeEnv := append([]string{
		"XDG_STATE_HOME=" + stateHome,
		"HOME=" + homeDir,
	}, goEnv...)
	code, out := runUpgradeCommand(t,
		upgradeEnv,
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
	goEnv := writeFakeUpgradeBinaries(t, "gostr", "stringer")

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

	upgradeEnv := append([]string{
		"XDG_STATE_HOME=" + stateHome,
		"HOME=" + homeDir,
	}, goEnv...)
	code, out := runUpgradeCommand(t,
		upgradeEnv,
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
	goEnv := writeFakeUpgradeBinaries(t, "gostr", "stringer")

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

	upgradeEnv := append([]string{
		"XDG_STATE_HOME=" + stateHome,
		"HOME=" + homeDir,
	}, goEnv...)
	code, out := runUpgradeCommand(t,
		upgradeEnv,
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

// TestUpgradeHTTPToolFailsOnDownload tests that an HTTP download failure aborts
// the upgrade after removal. A local server keeps the failure deterministic.

func TestUpgradeHTTPToolFailsOnDownload(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	schemaDir := t.TempDir()
	downloadRequests := make(chan struct{}, 4)
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloadRequests <- struct{}{}
		http.Error(w, "download unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(downloadServer.Close)

	// Keep the target outside shared system directories to avoid real elevation.
	installDir := filepath.Join(homeDir, "tools")
	if err := os.MkdirAll(installDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "httptool"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	content := "schema_version = 1\n\n[defaults]\nmethod_order = [\"http\", \"native\"]\n\n" +
		"[tools.httptool]\n" +
		// Literal string (single quotes): Windows paths carry backslashes,
		// which are escapes in TOML basic strings.
		"http = {url = \"" + downloadServer.URL + "/tool.bin\", checksum = \"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\", extract_to = '" + installDir + "'}\n"
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
			Config:      map[string]any{"url": "https://example.invalid/old-tool.bin", "extract_to": installDir},
		},
	})

	code, out := runUpgradeCommand(t,
		[]string{
			"XDG_STATE_HOME=" + stateHome,
			"HOME=" + homeDir,
			"USERPROFILE=" + homeDir,
		},
		"-schema", filepath.Join(schemaDir, "schema.toml"),
		"-force",
	)
	select {
	case <-downloadRequests:
	default:
		t.Fatal("upgrade did not request the failing download")
	}

	// Upgrade fails (non-zero exit) because the local server rejects the download.
	if code == 0 {
		t.Fatalf("upgrade exit = 0, want non-zero (output: %s)", out)
	}
	if !strings.Contains(out, "failed") {
		t.Fatalf("output should mention failed tool, got: %s", out)
	}
}

func upgradeExecutorForMethodOrder(order []string) *exec.Executor {
	ex := exec.New()
	exec.WithDefaultMethodOrder(order)(ex)
	return ex
}

func TestLockPinForRejectsAmbiguousKind(t *testing.T) {
	lk := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}
	if _, ok := legacyV1PinFor(lk, "demo", "http"); ok {
		t.Fatal("lockPinFor accepted ambiguous same-kind pins")
	}
}

func TestLockPinForCandidateUsesKindOrdinal(t *testing.T) {
	first := &config.MethodCandidate{Kind: "http", Label: "primary"}
	other := &config.MethodCandidate{Kind: "git", Label: "source"}
	second := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{first, other, second}}
	lk := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}

	pin, ok := legacyV1PinForCandidate(lk, "demo", tool, second)
	if !ok || pin.Latest != "v2" {
		t.Fatalf("legacyV1PinForCandidate(second) = %+v, %v; want v2, true", pin, ok)
	}
}

func TestFindTrackedMethodCandidateUsesPersistedLabel(t *testing.T) {
	primary := &config.MethodCandidate{Kind: "http", Label: "primary"}
	mirror := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{primary, mirror}}

	got, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "mirror", MethodKind: "http"}, config.SelectMethods(tool, []string{"http"}, ""))
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

	_, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "http", MethodKind: "http"}, config.SelectMethods(tool, []string{"http"}, ""))
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("findTrackedMethodCandidate error = %v, want ambiguous", err)
	}
}

func TestFindTrackedMethodCandidateLegacyKindResolvesSingleLabeledCandidate(t *testing.T) {
	candidate := &config.MethodCandidate{Kind: "http", Label: "primary"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{candidate}}

	got, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "http", MethodKind: "http"}, config.SelectMethods(tool, []string{"http"}, ""))
	if err != nil {
		t.Fatalf("findTrackedMethodCandidate: %v", err)
	}
	if got != candidate {
		t.Fatalf("findTrackedMethodCandidate = %+v, want candidate", got)
	}
}

func TestFindTrackedMethodCandidateRejectsCandidateExcludedByCurrentPolicy(t *testing.T) {
	primary := &config.MethodCandidate{Kind: "http", Label: "primary"}
	mirror := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{
		Methods:    []*config.MethodCandidate{primary, mirror},
		MethodOnly: []string{"primary"},
	}

	_, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "mirror", MethodKind: "http"}, config.SelectMethods(tool, []string{"http"}, ""))
	if err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("findTrackedMethodCandidate error = %v, want current-policy rejection", err)
	}
}

// TestBuildUpgradeExecutorUsesSchemaAURHelper verifies configured helper
// selection is executor-local for install and upgrade paths.
func TestBuildUpgradeExecutorUsesSchemaAURHelper(t *testing.T) {
	build := func(helper string) *exec.Executor {
		t.Helper()
		schemaPath := filepath.Join(t.TempDir(), "schema.toml")
		if err := os.WriteFile(schemaPath, []byte("schema_version = 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		schema := &config.Schema{Defaults: config.Defaults{AurHelper: helper}}
		ex, err := buildUpgradeExecutor(schema, "arch", &platform.Facts{}, schemaPath, upgradeOptions{}, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("buildUpgradeExecutor() error = %v", err)
		}
		return ex
	}

	before := exec.Lookup("aur")
	installSchema := &config.Schema{Defaults: config.Defaults{AurHelper: "yay"}}
	installExecutor := newInstallExecutor(installPlan{schema: "schema.toml"}, installSchema, "arch", &platform.Facts{}, time.Time{}, slog.New(slog.DiscardHandler))
	yayExecutor := build("yay")
	paruExecutor := build("")
	if got := exec.Lookup("aur"); got != before {
		t.Fatal("building a schema executor changed the process AUR registry")
	}

	for _, ex := range []*exec.Executor{installExecutor, yayExecutor, paruExecutor} {
		for _, kind := range exec.RegisteredKinds() {
			if ex.LookupAdapter(kind) == nil {
				t.Errorf("executor lost registered adapter %q", kind)
			}
		}
	}

	installWithFake := func(ex *exec.Executor) run.FakeCall {
		t.Helper()
		fake := &run.FakeRunner{}
		exec.WithRunner(fake)(ex)
		aur, ok := ex.LookupAdapter("aur").(interface {
			Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error
		})
		if !ok {
			t.Fatal("aur adapter does not implement Install")
		}
		method := &config.MethodCandidate{Kind: "aur", Config: map[string]any{"pkg": "demo"}}
		if err := aur.Install(context.Background(), fake, &config.Tool{Name: "demo"}, method); err != nil {
			t.Fatalf("aur Install() error = %v", err)
		}
		if len(fake.Calls) != 1 {
			t.Fatalf("runner calls = %v, want one call", fake.Calls)
		}
		return fake.Calls[0]
	}

	if got := installWithFake(installExecutor); got.Name != "yay" || !reflect.DeepEqual(got.Args, []string{"-S", "--noconfirm", "demo"}) {
		t.Errorf("install runner call = %#v", got)
	}
	if got := installWithFake(yayExecutor); got.Name != "yay" || !reflect.DeepEqual(got.Args, []string{"-S", "--noconfirm", "demo"}) {
		t.Errorf("yay runner call = %#v", got)
	}
	if got := installWithFake(paruExecutor); got.Name != "paru" || !reflect.DeepEqual(got.Args, []string{"-S", "--noconfirm", "demo"}) {
		t.Errorf("paru runner call = %#v", got)
	}
}
func TestFindTrackedMethodCandidateRejectsDuplicatePersistedLabel(t *testing.T) {
	tool := &config.Tool{Methods: []*config.MethodCandidate{
		{Kind: "http", Label: "mirror"},
		{Kind: "http", Label: "mirror"},
	}}
	_, err := findTrackedMethodCandidate(tool, state.ToolState{Method: "mirror", MethodKind: "http"}, config.SelectMethods(tool, []string{"http"}, ""))
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("findTrackedMethodCandidate error = %v, want duplicate-label ambiguity", err)
	}
}
