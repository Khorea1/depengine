package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/exec"
)

func TestHostMutationLockPrecedesRemoveAndForgetStateAccess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	release, err := holdHostMutations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	for name, action := range map[string]func(context.Context) error{
		"forget": func(ctx context.Context) error { return runForgetContext(ctx, "not-tracked", false) },
		"remove": func(ctx context.Context) error {
			yes, no, empty := true, false, ""
			return runRemoveWithExecutor(ctx, []string{"not-tracked"}, &no, &no, &empty, &empty, &yes, &no, exec.New(), nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := action(ctx); err == nil {
				t.Fatal("mutating command succeeded while host mutation lock was held")
			}
			if ctx.Err() != context.DeadlineExceeded {
				t.Fatalf("operation did not wait for mutation lock: %v", ctx.Err())
			}
			if _, err := os.Stat(filepath.Join(dir, "depengine", "state.json")); !os.IsNotExist(err) {
				t.Fatalf("blocked operation accessed or created state: %v", err)
			}
		})
	}
}

func TestHostMutationLockDryRunSkipsAcquisition(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	release, err := holdHostMutations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	releaseReadOnly, err := holdHostMutations(canceled, true)
	if err != nil {
		t.Fatalf("read-only operation unnecessarily acquired mutation lock: %v", err)
	}
	releaseReadOnly()
}
