package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewGitAdapter())
}

func TestImportDoesNotRegisterAdapter(t *testing.T) {
	if exec.Lookup("git") != nil {
		t.Fatal("importing internal/git registered an adapter")
	}
}

func TestObserveReportsActualCheckoutHead(t *testing.T) {
	ctx := context.Background()
	if !(run.OSExecRunner{}).LookPath(ctx, "git") {
		t.Skip("git is required for checkout observation")
	}
	checkout := t.TempDir()
	gitTestCommand(t, "-C", checkout, "init", "--quiet")
	gitTestCommand(t, "-C", checkout, "config", "user.name", "depengine test")
	gitTestCommand(t, "-C", checkout, "config", "user.email", "depengine@example.test")
	if err := os.WriteFile(filepath.Join(checkout, "payload"), []byte("commit"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, "-C", checkout, "add", "payload")
	gitTestCommand(t, "-C", checkout, "commit", "--quiet", "-m", "initial")
	head := strings.TrimSpace(gitTestCommand(t, "-C", checkout, "rev-parse", "HEAD"))

	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "rev": "refs/heads/depengine-no-such-local-branch"}}
	got, err := NewGitAdapter().Observe(ctx, run.OSExecRunner{}, &config.Tool{Name: "demo"}, method)
	if err != nil {
		t.Fatalf("Observe(): %v", err)
	}
	want := plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Revision: head}, KnownFields: []plan.IdentityField{plan.FieldRevision}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Observe() = %#v, want actual HEAD %s", got, head)
	}
}

func TestObserveBreaksOnUnverifiableRetainedCheckout(t *testing.T) {
	ctx := context.Background()
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "rev": "main"}}
	for _, tc := range []struct {
		name   string
		result run.Result
	}{
		{name: "probe error", result: run.Result{Err: errors.New("probe failed"), ExitCode: 128}},
		{name: "malformed output", result: run.Result{Stdout: []byte("deadbeef\n"), ExitCode: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewGitAdapter().Observe(ctx, gitProbeRunner{result: tc.result}, &config.Tool{Name: "demo"}, method)
			if err != nil {
				t.Fatalf("Observe(): %v", err)
			}
			if got.Presence != plan.PresenceBroken || len(got.KnownFields) != 0 {
				t.Fatalf("Observe() = %#v, want broken without known identity", got)
			}
		})
	}
}

func TestObserveBreaksWhenRetainedCheckoutMetadataIsMissing(t *testing.T) {
	checkout := t.TempDir()
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "rev": "main"}}
	got, err := NewGitAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, method)
	if err != nil {
		t.Fatalf("Observe(): %v", err)
	}
	if got.Presence != plan.PresenceBroken || len(got.KnownFields) != 0 {
		t.Fatalf("Observe() = %#v, want broken without known identity", got)
	}
}

func TestIsFullCommitIDAcceptsSHA1AndSHA256(t *testing.T) {
	for _, length := range []int{40, 64} {
		if !isFullCommitID(strings.Repeat("a", length)) {
			t.Errorf("isFullCommitID length %d = false, want true", length)
		}
	}
	for _, value := range []string{"", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("g", 40)} {
		if isFullCommitID(value) {
			t.Errorf("isFullCommitID(%q) = true, want false", value)
		}
	}
}

func TestObserveKeepsOpaqueGitInstallPresenceOnly(t *testing.T) {
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "artifact": "build/output", "rev": "main"}}
	got, err := NewGitAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, method)
	if err != nil || got.Presence != plan.PresencePresent || len(got.KnownFields) != 0 {
		t.Fatalf("Observe() = %#v, err %v; want presence-only", got, err)
	}
}

func TestObserveRequiresToolAndMethod(t *testing.T) {
	ctx := context.Background()
	adapter := NewGitAdapter()
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{"url": "https://example.test/repo.git"}}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, &config.Tool{Name: "demo"}, nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

type gitProbeRunner struct {
	run.OSExecRunner
	result run.Result
}

func (r gitProbeRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	if name == "git" && len(args) > 0 && args[len(args)-1] == "HEAD^{commit}" {
		return r.result
	}
	return r.OSExecRunner.Run(ctx, name, args...)
}

func gitTestCommand(t *testing.T, args ...string) string {
	t.Helper()
	result := (run.OSExecRunner{}).Run(context.Background(), "git", args...)
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("git %v failed: exit=%d err=%v stderr=%s", args, result.ExitCode, result.Err, result.Stderr)
	}
	return string(result.Stdout)
}

// TestV2ResolveObserveInstallRevision exercises the full V2 path for a
// pinned revision: planner intent, read-only resolution, observation, and
// resolved install, all without network access.
func TestV2ResolveObserveInstallRevision(t *testing.T) {
	ctx := context.Background()
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"url": "https://example.test/repo.git",
		"rev": "deadbeef",
	}}

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Source != "https://example.test/repo.git" || resolved.Identity.Revision != "deadbeef" {
		t.Fatalf("resolved identity = %#v, want source URL and pinned revision", resolved.Identity)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}

	runner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(ctx, runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	foundClone := false
	for _, call := range runner.Calls {
		if call.Name == "git" && len(call.Args) > 0 && call.Args[0] == "clone" {
			foundClone = true
		}
	}
	if !foundClone {
		t.Fatalf("InstallResolved recorded no git clone: %+v", runner.Calls)
	}
}
