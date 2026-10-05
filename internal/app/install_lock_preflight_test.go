package app

import (
	"context"
	"errors"
	"github.com/Khorea1/depengine/internal/log"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/run"
)

type changingGitRunner struct {
	outputs []string
	calls   int
}

func (r *changingGitRunner) Run(_ context.Context, _ string, _ ...string) run.Result {
	out := r.outputs[r.calls]
	r.calls++
	return run.Result{Stdout: []byte(out)}
}

func resolvedLegacyForSave(t *testing.T, schema *config.Schema, old *lock.Lock) *lock.Lock {
	t.Helper()
	fresh, err := lock.ResolveLegacyV1(context.Background(), schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := mergeInstallLock(old, fresh)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestResolveInstallLockFailsBeforeExecutionWhenLegacyResolutionFails(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	method := &config.MethodCandidate{
		Kind:   "git",
		Config: map[string]any{"url": "https://example.test/tool.git", "branch": "main"},
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}},
	}}
	runner := &run.FakeRunner{Err: errors.New("remote unavailable")}

	resolved, err := resolveInstallLock(context.Background(), installPlan{schema: schemaPath}, schema, log.Default, runner)
	if resolved != nil {
		t.Fatalf("resolved lock = %#v, want nil on resolution failure", resolved)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("resolveInstallLock() error = %v, want exit code 2", err)
	}
	if len(runner.Calls) == 0 {
		t.Fatal("resolver failure did not attempt the mutable selector")
	}
	if method.LockedRevision != "" {
		t.Fatalf("LockedRevision = %q after failed preflight, want empty", method.LockedRevision)
	}
	if _, err := os.Stat(lock.DefaultPath(schemaPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock after failed resolution: error = %v, want not-exist", err)
	}
}

func TestInstallPersistsOnlyPreExecutionLegacyIdentity(t *testing.T) {
	const first = "0123456789abcdef0123456789abcdef01234567"
	const second = "abcdef0123456789abcdef0123456789abcdef01"
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	method := &config.MethodCandidate{
		Kind:   "git",
		Config: map[string]any{"url": "https://example.test/tool.git", "branch": "main"},
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}},
	}}
	lockPath := lock.DefaultPath(schemaPath)
	legacy := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{}, MethodsHash: map[string]string{"tool": "legacy-method-hash"}}
	if err := lock.Save(lockPath, legacy); err != nil {
		t.Fatal(err)
	}
	runner := &changingGitRunner{outputs: []string{first + "\trefs/heads/main\n", second + "\trefs/heads/main\n"}}

	resolved, err := resolveInstallLock(context.Background(), installPlan{schema: schemaPath}, schema, log.Default, runner)
	if err != nil {
		t.Fatal(err)
	}
	if method.LockedRevision != first {
		t.Fatalf("pre-execution LockedRevision = %q, want %q", method.LockedRevision, first)
	}
	saveResolvedLegacyInstallLock(lockPath, resolved, log.Default, false)
	if runner.calls != 1 {
		t.Fatalf("legacy resolver calls = %d, want one pre-execution resolution", runner.calls)
	}
	persisted, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if pin := persisted.Tools["tool/git/0"]; pin.Revision != first {
		t.Fatalf("persisted revision = %q, want pre-execution identity %q (not second resolution %q)", pin.Revision, first, second)
	}
}
