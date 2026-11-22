package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	engineexec "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

// gatedRemovalAdapter represents the physical phase of a mutation whose state
// lock has already been released (as with upgrade). The adapter is invoked
// through the actual executor removal boundary, not directly by the test.
type gatedRemovalAdapter struct {
	*recordingRemoveAdapter
	root string
	mode string
}

func (a *gatedRemovalAdapter) Remove(ctx context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate) error {
	switch a.mode {
	case "interleaving":
		if err := os.WriteFile(filepath.Join(a.root, "in-adapter"), nil, 0600); err != nil {
			return err
		}
		if err := waitForMutationTestSignal(ctx, filepath.Join(a.root, "finish-adapter")); err != nil {
			return err
		}
	case "crash-after-effect":
		// No readiness signal until the externally visible side effect exists.
	default:
		return fmt.Errorf("unexpected adapter mode %q", a.mode)
	}
	if err := os.Remove(filepath.Join(a.root, "physical-tool")); err != nil {
		return err
	}
	if a.mode == "crash-after-effect" {
		if err := os.WriteFile(filepath.Join(a.root, "effect-done"), nil, 0600); err != nil {
			return err
		}
		// Parent terminates the process at this exact uncommitted boundary.
		return waitForMutationTestSignal(ctx, filepath.Join(a.root, "never-commit"))
	}
	return nil
}

func waitForMutationTestSignal(ctx context.Context, path string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// This is intentionally a separate process: a mutex, a held state lock, or a
// fake executor running in another goroutine would not test the real invariant.
func TestHostMutationProcessHelper(t *testing.T) {
	mode := os.Getenv("DEPENGINE_HOST_MUTATION_TEST_CHILD")
	if mode == "" {
		return
	}
	root := os.Getenv("DEPENGINE_HOST_MUTATION_TEST_DIR")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch mode {
	case "forget-blocked":
		short, stop := context.WithTimeout(ctx, 600*time.Millisecond)
		defer stop()
		if err := runForgetContext(short, "beta", false); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("competing forget while physical mutation in progress: got %v, want deadline exceeded", err)
		}
	case "forget-success":
		if err := runForgetContext(ctx, "beta", false); err != nil {
			t.Fatalf("forget after release: %v", err)
		}
	case "interleaving", "crash-after-effect":
		unlock, err := holdHostMutations(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		ls, err := state.LoadLockedContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ls.State().Tools["alpha"]; !ok {
			t.Fatal("tracked tool missing before physical mutation")
		}
		if err := ls.Close(); err != nil {
			t.Fatal(err)
		}
		// The state lock is now FREE; only the long-lived mutation lock
		// prevents a second command from changing actionable state.
		adapter := &gatedRemovalAdapter{recordingRemoveAdapter: &recordingRemoveAdapter{}, root: root, mode: mode}
		engine := engineexec.New()
		engineexec.WithAdapters(adapter)(engine)
		method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "test/pkg"}}
		resolved := plan.New("alpha", "cargo", true)
		resolved.Identity.Package = "test/pkg"
		if err := engine.RemoveResolvedCandidate(ctx, &run.FakeRunner{}, &config.Tool{Name: "alpha"}, method, &resolved); err != nil {
			t.Fatalf("controlled physical mutation: %v", err)
		}
		ls, err = state.LoadLockedContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		delete(ls.State().Tools, "alpha")
		if err := ls.Save(); err != nil {
			t.Fatal(err)
		}
		if err := ls.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown subprocess mode %q", mode)
	}
}

func mutationTestProcess(t *testing.T, mode, root string) (*osexec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := osexec.Command(os.Args[0], "-test.run=^TestHostMutationProcessHelper$") // #nosec G204,G702 -- fixed test helper entry point.
	cmd.Env = append(os.Environ(), "DEPENGINE_HOST_MUTATION_TEST_CHILD="+mode, "DEPENGINE_HOST_MUTATION_TEST_DIR="+root)
	output := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = output, output
	return cmd, output
}

func mutationTestFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	if err := state.Save(&state.State{Version: state.CurrentVersion, Tools: map[string]state.ToolState{
		"alpha": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "test/pkg"}},
		"beta":  {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "other/pkg"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "physical-tool"), []byte("installed"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func mutationTestWaitMarker(t *testing.T, root, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	if err := waitForMutationTestSignal(ctx, filepath.Join(root, name)); err != nil {
		t.Fatalf("subprocess did not signal %s: %v", name, err)
	}
}

func mutationTestAssertTracked(t *testing.T, wantAlpha, wantBeta bool) {
	t.Helper()
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	_, alpha := st.Tools["alpha"]
	_, beta := st.Tools["beta"]
	if alpha != wantAlpha || beta != wantBeta {
		t.Fatalf("tracked tools alpha=%t beta=%t, want alpha=%t beta=%t", alpha, beta, wantAlpha, wantBeta)
	}
}

func TestMutationLockSerializesPhysicalPhaseAcrossRealProcesses(t *testing.T) {
	root := mutationTestFixture(t)
	holder, output := mutationTestProcess(t, "interleaving", root)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if holder.ProcessState == nil {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	}()
	mutationTestWaitMarker(t, root, "in-adapter")

	// The simulated upgrade has released the ordinary state lock while its
	// adapter is still performing an externally visible operation.
	blocked, blockedOut := mutationTestProcess(t, "forget-blocked", root)
	if err := blocked.Run(); err != nil {
		t.Fatalf("competing command: %v\n%s", err, blockedOut)
	}
	// A different XDG state directory does not isolate physical package
	// managers. Competing CLI mutations still share one user-level lease.
	otherRoot := filepath.Join(root, "other-state")
	blockedOther, blockedOtherOut := mutationTestProcess(t, "forget-blocked", root)
	blockedOther.Env = append(blockedOther.Env, "XDG_STATE_HOME="+otherRoot)
	if err := blockedOther.Run(); err != nil {
		t.Fatalf("competing command with different state root: %v\n%s", err, blockedOtherOut)
	}
	if _, err := os.Stat(filepath.Join(otherRoot, "depengine", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("blocked cross-root mutation wrote state: %v", err)
	}
	mutationTestAssertTracked(t, true, true)
	if _, err := os.Stat(filepath.Join(root, "physical-tool")); err != nil {
		t.Fatalf("physical effect happened before controlled release: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "finish-adapter"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err != nil {
		t.Fatalf("controlled operation: %v\n%s", err, output)
	}
	mutationTestAssertTracked(t, false, true)
	if _, err := os.Stat(filepath.Join(root, "physical-tool")); !os.IsNotExist(err) {
		t.Fatalf("adapter did not remove physical fixture: %v", err)
	}
	retry, retryOut := mutationTestProcess(t, "forget-success", root)
	if err := retry.Run(); err != nil {
		t.Fatalf("retry after mutation lock released: %v\n%s", err, retryOut)
	}
	mutationTestAssertTracked(t, false, false)
}

func TestMutationLockCrashReleasesLeaseWithoutInventingStateCommit(t *testing.T) {
	root := mutationTestFixture(t)
	holder, output := mutationTestProcess(t, "crash-after-effect", root)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if holder.ProcessState == nil {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	}()
	mutationTestWaitMarker(t, root, "effect-done")
	mutationTestAssertTracked(t, true, true)
	if _, err := os.Stat(filepath.Join(root, "physical-tool")); !os.IsNotExist(err) {
		t.Fatalf("controlled physical effect did not happen: %v", err)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err == nil {
		t.Fatalf("killed process exited successfully; output: %s", output)
	}

	// A crash cannot be interpreted as a successful state commit. The new
	// process must still acquire the OS lock and retain unrelated entries.
	mutationTestAssertTracked(t, true, true)
	retry, retryOut := mutationTestProcess(t, "forget-success", root)
	if err := retry.Run(); err != nil {
		t.Fatalf("mutation after crashed owner: %v\n%s", err, retryOut)
	}
	mutationTestAssertTracked(t, true, false)
}
