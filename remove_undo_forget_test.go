package main

import (
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/ecosystem"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/state"
)

// init mirrors main.initAdapters: the test binary never runs main(), so the
// production adapter registry would otherwise be empty. These tests exercise
// remove/undo/forget end-to-end (including adapter dispatch), so the same
// adapters the CLI registers must be registered here.
func init() {
	exec.Register(exec.NewNativeAdapter(""))
	exec.RegisterNativeManagerAliases()
	ecosystem.RegisterAll("paru")
}

// runCommand executes a depengine command in a child process of the test
// binary. runRemove/runUndo/runForget call os.Exit on failure, so they can
// only be exercised out-of-process. The child env is scrubbed of the
// state/Go/home variables the tests control and re-set from `extraEnv`, so
// every scenario is hermetic: temp XDG_STATE_HOME, fake GOBIN dir, no
// network, no real package managers.
func runCommand(t *testing.T, cmd string, extraEnv []string, args ...string) (exitCode int, output string) {
	t.Helper()
	cmdArgs := append([]string{"-test.run=^TestCommandHelperSubprocess$"}, args...)
	c := osexec.Command(os.Args[0], cmdArgs...)

	scrubbed := map[string]bool{
		"XDG_STATE_HOME":     true,
		"XDG_CACHE_HOME":     true,
		"XDG_CONFIG_HOME":    true,
		"GOBIN":              true,
		"GOPATH":             true,
		"HOME":               true,
		"USERPROFILE":        true,
		"DEPENGINE_MANIFEST": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if !scrubbed[key] {
			env = append(env, kv)
		}
	}
	env = append(env, "DEPENGINE_TEST_HELPER=1")
	env = append(env, "DEPENGINE_TEST_HELPER_CMD="+cmd)
	env = append(env, extraEnv...)
	c.Env = env

	var out strings.Builder
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*osexec.ExitError)
		if !ok {
			t.Fatalf("helper failed to start: %v", err)
		}
		code = ee.ExitCode()
	}
	return code, out.String()
}

// TestCommandHelperSubprocess is not a real test — it is the entry point for
// the subprocess invocations issued by runCommand. It dispatches to the
// command named by DEPENGINE_TEST_HELPER_CMD with the positional arguments
// that follow the -test.run flag on the command line.
func TestCommandHelperSubprocess(t *testing.T) {
	if os.Getenv("DEPENGINE_TEST_HELPER") != "1" {
		t.Skip("subprocess entry point; run indirectly via runCommand")
	}
	args := os.Args[2:] // skip binary name and the -test.run flag
	switch os.Getenv("DEPENGINE_TEST_HELPER_CMD") {
	case "remove":
		runRemove(args)
	case "undo":
		runUndo(args)
	case "forget":
		runForget(args)
	case "upgrade":
		// Upgrade flags are passed via DEPENGINE_TEST_ARGS to avoid
		// collision with the Go test binary's own flag parser.
		var upgradeArgs []string
		if a := os.Getenv("DEPENGINE_TEST_ARGS"); a != "" {
			upgradeArgs = strings.Split(a, "\x1f")
		}
		runUpgrade(upgradeArgs)
	default:
		os.Exit(99)
	}
	// A successful command returns here (failure paths call os.Exit).
	os.Exit(0)
}

func writeTestState(t *testing.T, stateHome string, tools map[string]state.ToolState) {
	t.Helper()
	dir := filepath.Join(stateHome, "depengine")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(state.State{Version: 1, Tools: tools}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func loadTestState(t *testing.T, stateHome string) state.State {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateHome, "depengine", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st state.State
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func writeTestSnapshot(t *testing.T, stateHome string, tools map[string]state.ToolState) {
	t.Helper()
	dir := filepath.Join(stateHome, "depengine", "snapshots")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(state.State{Version: 1, Tools: tools}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot filenames embed a parseable timestamp; this one sorts as the
	// (only, therefore newest) snapshot.
	if err := os.WriteFile(filepath.Join(dir, "state-20260101T000000.000000000.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func fakeBinary(t *testing.T, binDir, name string) string {
	t.Helper()
	path := filepath.Join(binDir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func goToolState() state.ToolState {
	return state.ToolState{
		Method:      "go",
		MethodKind:  "go",
		InstalledAt: "2026-08-01T00:00:00Z",
		Config:      map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"},
	}
}

// TestRemoveGoTool removes a go-installed tool: the binary must be deleted
// from GOBIN, the state entry removed, and the command must exit 0.
func TestRemoveGoTool(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	bin := fakeBinary(t, binDir, "stringer")

	writeTestState(t, stateHome, map[string]state.ToolState{"gostr": goToolState()})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"gostr",
	)

	if code != 0 {
		t.Fatalf("remove gostr exit = %d, want 0 (output: %s)", code, out)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after remove (err=%v)", bin, err)
	}
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["gostr"]; ok {
		t.Fatalf("state still contains gostr after remove: %+v", st.Tools)
	}
}

// TestRemoveHTTPTool removes a tool installed via http: now that HTTPAdapter
// implements Remover, the command exits 0 and removes the state entry.
// The extract_to points to a /bin-suffixed temp dir, so Remove deletes
// only the target binary (extract_to/<tool>), not the dir itself.
func TestRemoveHTTPTool(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()

	// Create a /bin-suffixed dir so isSharedDir returns true.
	sharedDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Plant the target binary.
	binPath := filepath.Join(sharedDir, "httptool")
	if err := os.WriteFile(binPath, []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}

	httpState := state.ToolState{
		Method:      "http",
		MethodKind:  "http",
		InstalledAt: "2026-08-01T00:00:00Z",
		Config:      map[string]any{"url": "https://example.invalid/tool.tar.gz", "extract_to": sharedDir},
	}
	writeTestState(t, stateHome, map[string]state.ToolState{"httptool": httpState})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "HOME=" + homeDir},
		"httptool",
	)

	if code != 0 {
		t.Fatalf("remove httptool exit = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("output should mention removal, got: %s", out)
	}
	// Binary should be gone from shared dir.
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Fatalf("binary %s should be removed (err=%v)", binPath, err)
	}
	// State entry should be cleaned.
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["httptool"]; ok {
		t.Fatalf("state should not contain httptool after remove: %+v", st.Tools)
	}
}

// TestRemoveNonexistent removes a tool that was never installed: warning
// output and exit 1.
func TestRemoveNonexistent(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	writeTestState(t, stateHome, map[string]state.ToolState{})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "HOME=" + homeDir},
		"never-installed",
	)

	if code != 1 {
		t.Fatalf("remove nonexistent exit = %d, want 1 (output: %s)", code, out)
	}
	if !strings.Contains(out, "nothing to remove") {
		t.Fatalf("output should warn that the tool is not installed, got: %s", out)
	}
}

// TestUndoGoTool reverts to a snapshot taken before the go tool was installed:
// the binary must be removed, the state reverted, and the command must exit 0.
func TestUndoGoTool(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	bin := fakeBinary(t, binDir, "stringer")

	writeTestState(t, stateHome, map[string]state.ToolState{"gostr": goToolState()})
	writeTestSnapshot(t, stateHome, map[string]state.ToolState{})

	code, out := runCommand(t, "undo",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
	)

	if code != 0 {
		t.Fatalf("undo exit = %d, want 0 (output: %s)", code, out)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after undo (err=%v)", bin, err)
	}
	st := loadTestState(t, stateHome)
	if len(st.Tools) != 0 {
		t.Fatalf("state should be reverted to the empty snapshot, got: %+v", st.Tools)
	}
}

// TestUndoWithoutSnapshot runs undo with no snapshot available: exit 1 and a
// 'no snapshot available' message.
func TestUndoWithoutSnapshot(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	writeTestState(t, stateHome, map[string]state.ToolState{"gostr": goToolState()})

	code, out := runCommand(t, "undo",
		[]string{"XDG_STATE_HOME=" + stateHome, "HOME=" + homeDir},
	)

	if code != 1 {
		t.Fatalf("undo without snapshot exit = %d, want 1 (output: %s)", code, out)
	}
	if !strings.Contains(out, "no snapshot available") {
		t.Fatalf("output should mention missing snapshot, got: %s", out)
	}
}

// TestForgetGoTool forgets a go tool: state entry cleared but the binary
// stays on disk, exit 0.
func TestForgetGoTool(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	bin := fakeBinary(t, binDir, "stringer")

	writeTestState(t, stateHome, map[string]state.ToolState{"gostr": goToolState()})

	code, out := runCommand(t, "forget",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"gostr",
	)

	if code != 0 {
		t.Fatalf("forget exit = %d, want 0 (output: %s)", code, out)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("forget must keep the binary on disk, got stat error: %v", err)
	}
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["gostr"]; ok {
		t.Fatalf("state still contains gostr after forget: %+v", st.Tools)
	}
}
