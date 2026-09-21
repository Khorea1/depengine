package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// runCommand executes a depengine command in a child process of the test
// binary. The child env is scrubbed of the
// state/Go/home variables the tests control and re-set from `extraEnv`, so
// every scenario is hermetic: temp XDG_STATE_HOME, fake GOBIN dir, no
// network, no real package managers.
func runCommand(t *testing.T, cmd string, extraEnv []string, args ...string) (exitCode int, output string) {
	t.Helper()
	c := osexec.Command(os.Args[0], "-test.run=^TestCommandHelperSubprocess$")

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
	env = append(env, "DEPENGINE_TEST_ARGS="+strings.Join(args, "\x1f"))
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
	var args []string
	if encoded := os.Getenv("DEPENGINE_TEST_ARGS"); encoded != "" {
		args = strings.Split(encoded, "\x1f")
	}
	switch cmd := os.Getenv("DEPENGINE_TEST_HELPER_CMD"); cmd {
	case "remove":
		runViaCobra(newRemoveCmd(), args)
	case "undo":
		runViaCobra(newUndoCmd(), args)
	case "forget":
		runViaCobra(newForgetCmd(), args)
	case "upgrade":
		runViaCobra(newUpgradeCmd(), args)
	default:
		os.Exit(99)
	}
	// A successful command returns here.
	os.Exit(0)
}

// runViaCobra executes a single cobra.Command the same way the real CLI
// does (parse args, then RunE), so these tests exercise the exact code path
// `depengine remove|undo|forget|upgrade ...` runs, flag parsing included.
func runViaCobra(cmd *cobra.Command, args []string) {
	cmd.SetArgs(normalizeArgs(args))
	if err := cmd.Execute(); err != nil {
		var exitErr *ExitError
		if !errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(exitErr.Code)
	}
}

func writeTestState(t *testing.T, stateHome string, tools map[string]state.ToolState) {
	t.Helper()
	writeTestFullState(t, stateHome, state.State{Version: state.CurrentVersion, Tools: tools})
}

func writeTestFullState(t *testing.T, stateHome string, st state.State) {
	t.Helper()
	dir := filepath.Join(stateHome, "depengine")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data := marshalTestFullState(t, st)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func marshalTestState(t *testing.T, tools map[string]state.ToolState) []byte {
	t.Helper()
	return marshalTestFullState(t, state.State{Version: state.CurrentVersion, Tools: tools})
}

func marshalTestFullState(t *testing.T, st state.State) []byte {
	t.Helper()
	canonical, err := json.Marshal(&st)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	st.Checksum = hex.EncodeToString(sum[:])
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return data
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
	data := marshalTestState(t, tools)
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

func TestRemoveDryRunDoesNotWriteStateOrCreateLock(t *testing.T) {
	stateHome := t.TempDir()
	homeDir := t.TempDir()
	writeTestState(t, stateHome, map[string]state.ToolState{"gostr": goToolState()})

	statePath := filepath.Join(stateHome, "depengine", "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "HOME=" + homeDir},
		"--dry-run", "gostr",
	)
	if code != 0 {
		t.Fatalf("remove dry-run exit = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "would remove") {
		t.Fatalf("output should describe planned removal, got: %s", out)
	}

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("remove --dry-run rewrote state.json")
	}
	if _, err := os.Stat(statePath + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("remove --dry-run created state lock file (err=%v)", err)
	}
}

func prerequisiteGoToolState(pkg string, rootRequested bool) state.ToolState {
	return state.ToolState{
		Method:        "go",
		MethodKind:    "go",
		InstalledAt:   "2026-08-01T00:00:00Z",
		RootRequested: rootRequested,
		Config:        map[string]any{"pkg": pkg},
	}
}

func TestRemoveLastOwnerCleansUnreferencedPrerequisite(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	ownerBin := fakeBinary(t, binDir, "owner")
	helperBin := fakeBinary(t, binDir, "helper")
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"owner":  prerequisiteGoToolState("example.test/cmd/owner", true),
			"helper": prerequisiteGoToolState("example.test/cmd/helper", false),
		},
		OwnedResources: []plan.OwnedResourceState{{
			Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"owner"},
		}},
	})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"owner",
	)
	if code != 0 {
		t.Fatalf("remove owner exit = %d, want 0 (output: %s)", code, out)
	}
	for _, binary := range []string{ownerBin, helperBin} {
		if _, err := os.Stat(binary); !os.IsNotExist(err) {
			t.Fatalf("binary %s still present after prerequisite cleanup (err=%v)", binary, err)
		}
	}
	st := loadTestState(t, stateHome)
	if len(st.Tools) != 0 || len(st.OwnedResources) != 0 {
		t.Fatalf("state after prerequisite cleanup = tools:%#v resources:%#v, want empty", st.Tools, st.OwnedResources)
	}
	if !strings.Contains(out, "removed unreferenced prerequisite") {
		t.Fatalf("output should report prerequisite cleanup, got: %s", out)
	}
}

func TestRemoveOwnerRetainsPrerequisiteRequestedAsRoot(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	ownerBin := fakeBinary(t, binDir, "owner")
	helperBin := fakeBinary(t, binDir, "helper")
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"owner":  prerequisiteGoToolState("example.test/cmd/owner", true),
			"helper": prerequisiteGoToolState("example.test/cmd/helper", true),
		},
		OwnedResources: []plan.OwnedResourceState{{
			Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"owner"},
		}},
	})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"owner",
	)
	if code != 0 {
		t.Fatalf("remove owner exit = %d, want 0 (output: %s)", code, out)
	}
	if _, err := os.Stat(ownerBin); !os.IsNotExist(err) {
		t.Fatalf("owner binary still present (err=%v)", err)
	}
	if _, err := os.Stat(helperBin); err != nil {
		t.Fatalf("root-requested helper should remain installed: %v", err)
	}
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["helper"]; !ok {
		t.Fatalf("root-requested helper missing from state: %#v", st.Tools)
	}
	if len(st.OwnedResources) != 1 || st.OwnedResources[0].Resource != resource || st.OwnedResources[0].RefCount() != 0 {
		t.Fatalf("root-requested prerequisite state = %#v, want retained zero-ref resource", st.OwnedResources)
	}
}

func TestRemoveTrackedPrerequisiteWithLiveOwnerFailsClosed(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	helperBin := fakeBinary(t, binDir, "helper")
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"owner":  prerequisiteGoToolState("example.test/cmd/owner", true),
			"helper": prerequisiteGoToolState("example.test/cmd/helper", false),
		},
		OwnedResources: []plan.OwnedResourceState{{
			Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"owner"},
		}},
	})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"helper",
	)
	if code != 1 {
		t.Fatalf("remove live prerequisite exit = %d, want 1 (output: %s)", code, out)
	}
	if _, err := os.Stat(helperBin); err != nil {
		t.Fatalf("live prerequisite binary should remain: %v", err)
	}
	if !strings.Contains(out, "still required by tracked tools") {
		t.Fatalf("output should explain dependency blocker, got: %s", out)
	}
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["helper"]; !ok || len(st.OwnedResources) != 1 || st.OwnedResources[0].RefCount() != 1 {
		t.Fatalf("failed removal mutated prerequisite state: tools=%#v resources=%#v", st.Tools, st.OwnedResources)
	}
}

func TestExplicitRemoveOfRetainedRootPrerequisiteFinalizesOwnership(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	helperBin := fakeBinary(t, binDir, "helper")
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"helper": prerequisiteGoToolState("example.test/cmd/helper", true),
		},
		OwnedResources: []plan.OwnedResourceState{{Resource: resource, Ownership: plan.OwnershipDepengine}},
	})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"helper",
	)
	if code != 0 {
		t.Fatalf("remove retained root prerequisite exit = %d, want 0 (output: %s)", code, out)
	}
	if _, err := os.Stat(helperBin); !os.IsNotExist(err) {
		t.Fatalf("helper binary still present (err=%v)", err)
	}
	st := loadTestState(t, stateHome)
	if len(st.Tools) != 0 || len(st.OwnedResources) != 0 {
		t.Fatalf("state after explicit helper removal = tools:%#v resources:%#v, want empty", st.Tools, st.OwnedResources)
	}
}

func TestSharedPrerequisiteCleansOnlyAfterLastOwner(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	fakeBinary(t, binDir, "a")
	fakeBinary(t, binDir, "b")
	helperBin := fakeBinary(t, binDir, "helper")
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"a":      prerequisiteGoToolState("example.test/cmd/a", true),
			"b":      prerequisiteGoToolState("example.test/cmd/b", true),
			"helper": prerequisiteGoToolState("example.test/cmd/helper", false),
		},
		OwnedResources: []plan.OwnedResourceState{{
			Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"a", "b"},
		}},
	})
	env := []string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir}

	if code, out := runCommand(t, "remove", env, "a"); code != 0 {
		t.Fatalf("remove a exit = %d, want 0 (output: %s)", code, out)
	}
	if _, err := os.Stat(helperBin); err != nil {
		t.Fatalf("shared helper removed before last owner: %v", err)
	}
	st := loadTestState(t, stateHome)
	if len(st.OwnedResources) != 1 || !reflect.DeepEqual(st.OwnedResources[0].Dependents, []string{"b"}) {
		t.Fatalf("after first owner resources = %#v, want helper claimed by b", st.OwnedResources)
	}

	if code, out := runCommand(t, "remove", env, "b"); code != 0 {
		t.Fatalf("remove b exit = %d, want 0 (output: %s)", code, out)
	}
	if _, err := os.Stat(helperBin); !os.IsNotExist(err) {
		t.Fatalf("helper still present after last owner removal (err=%v)", err)
	}
	st = loadTestState(t, stateHome)
	if len(st.Tools) != 0 || len(st.OwnedResources) != 0 {
		t.Fatalf("after last owner state = tools:%#v resources:%#v, want empty", st.Tools, st.OwnedResources)
	}
}

func TestPrerequisiteCleanupRecursesTransitively(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	for _, binary := range []string{"owner", "helper", "subhelper"} {
		fakeBinary(t, binDir, binary)
	}
	helperResource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	subhelperResource, err := plan.PrerequisiteResource("subhelper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"owner":     prerequisiteGoToolState("example.test/cmd/owner", true),
			"helper":    prerequisiteGoToolState("example.test/cmd/helper", false),
			"subhelper": prerequisiteGoToolState("example.test/cmd/subhelper", false),
		},
		OwnedResources: []plan.OwnedResourceState{
			{Resource: helperResource, Ownership: plan.OwnershipDepengine, Dependents: []string{"owner"}},
			{Resource: subhelperResource, Ownership: plan.OwnershipDepengine, Dependents: []string{"helper"}},
		},
	})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"owner",
	)
	if code != 0 {
		t.Fatalf("remove owner exit = %d, want 0 (output: %s)", code, out)
	}
	for _, binary := range []string{"owner", "helper", "subhelper"} {
		if _, err := os.Stat(filepath.Join(binDir, binary)); !os.IsNotExist(err) {
			t.Fatalf("binary %s still present after transitive cleanup (err=%v)", binary, err)
		}
	}
	st := loadTestState(t, stateHome)
	if len(st.Tools) != 0 || len(st.OwnedResources) != 0 {
		t.Fatalf("transitive cleanup state = tools:%#v resources:%#v, want empty", st.Tools, st.OwnedResources)
	}
}

func TestExplicitOwnerAndPrerequisiteRemovalIsOrderIndependent(t *testing.T) {
	for _, order := range [][]string{{"owner", "helper"}, {"helper", "owner"}} {
		t.Run(strings.Join(order, "-then-"), func(t *testing.T) {
			stateHome := t.TempDir()
			binDir := t.TempDir()
			homeDir := t.TempDir()
			fakeBinary(t, binDir, "owner")
			fakeBinary(t, binDir, "helper")
			resource, err := plan.PrerequisiteResource("helper")
			if err != nil {
				t.Fatal(err)
			}
			writeTestFullState(t, stateHome, state.State{
				Version: state.CurrentVersion,
				Tools: map[string]state.ToolState{
					"owner":  prerequisiteGoToolState("example.test/cmd/owner", true),
					"helper": prerequisiteGoToolState("example.test/cmd/helper", false),
				},
				OwnedResources: []plan.OwnedResourceState{{
					Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"owner"},
				}},
			})

			code, out := runCommand(t, "remove",
				[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
				order...,
			)
			if code != 0 {
				t.Fatalf("remove %v exit = %d, want 0 (output: %s)", order, code, out)
			}
			st := loadTestState(t, stateHome)
			if len(st.Tools) != 0 || len(st.OwnedResources) != 0 {
				t.Fatalf("state after remove %v = tools:%#v resources:%#v, want empty", order, st.Tools, st.OwnedResources)
			}
		})
	}
}

func TestPrerequisiteCleanupFailureRetainsZeroRefOwnershipForRetry(t *testing.T) {
	stateHome := t.TempDir()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	ownerBin := fakeBinary(t, binDir, "owner")
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"owner": prerequisiteGoToolState("example.test/cmd/owner", true),
			"helper": {
				Method: "missing-adapter", MethodKind: "missing-adapter",
				InstalledAt: "2026-08-01T00:00:00Z", RootRequested: false,
				Config: map[string]any{"pkg": "helper"},
			},
		},
		OwnedResources: []plan.OwnedResourceState{{
			Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"owner"},
		}},
	})

	code, out := runCommand(t, "remove",
		[]string{"XDG_STATE_HOME=" + stateHome, "GOBIN=" + binDir, "HOME=" + homeDir},
		"owner",
	)
	if code != 1 {
		t.Fatalf("remove owner with failed helper cleanup exit = %d, want 1 (output: %s)", code, out)
	}
	if _, err := os.Stat(ownerBin); !os.IsNotExist(err) {
		t.Fatalf("owner binary should still be removed despite cleanup failure (err=%v)", err)
	}
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["owner"]; ok {
		t.Fatalf("removed owner still present in state: %#v", st.Tools)
	}
	if _, ok := st.Tools["helper"]; !ok {
		t.Fatalf("failed prerequisite cleanup lost helper state: %#v", st.Tools)
	}
	if len(st.OwnedResources) != 1 || st.OwnedResources[0].Resource != resource || st.OwnedResources[0].RefCount() != 0 {
		t.Fatalf("failed prerequisite cleanup state = %#v, want retained zero-ref resource", st.OwnedResources)
	}
	if !strings.Contains(out, "retaining state for retry") {
		t.Fatalf("output should report retained cleanup state, got: %s", out)
	}
}
