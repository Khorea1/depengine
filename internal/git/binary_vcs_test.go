package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func sourceFixtureCommand(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...) // #nosec G204 -- test helper runs fixture commands from controlled arguments.
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", program, args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestGitBinaryOnlyInstallObservesGoBuildRevision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go compiler not available")
	}
	ctx := context.Background()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.invalid/git-vcs-fixture\n\ngo 1.27.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(src, "main.go")
	if err := os.WriteFile(main, []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceFixtureCommand(t, src, "git", "init", "-q")
	sourceFixtureCommand(t, src, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "add", ".")
	sourceFixtureCommand(t, src, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "initial")
	revision := sourceFixtureCommand(t, src, "git", "rev-parse", "HEAD")

	binary := "depengine"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	dir := t.TempDir()
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"url": src, "rev": revision,
		"build":    map[string]any{"run": []any{"go", "build", "-buildvcs=true", "-o", binary, "."}},
		"artifact": binary, "extract_to": dir, "binary": binary,
	}}
	tool := &config.Tool{Name: "depengine"}
	adapter := NewGitAdapter()
	if err := adapter.Install(ctx, run.OSExecRunner{}, tool, method); err != nil {
		t.Fatal(err)
	}
	observation, err := adapter.Observe(ctx, run.OSExecRunner{}, tool, method)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Revision != revision ||
		len(observation.KnownFields) != 1 || observation.KnownFields[0] != plan.FieldRevision {
		t.Fatalf("installed Go revision not verified: %+v; want %s", observation, revision)
	}

	// A changed build must never inherit the clean checkout's revision claim.
	if err := os.WriteFile(main, []byte("package main\nfunc main() { println(\"modified\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceFixtureCommand(t, src, "go", "build", "-buildvcs=true", "-o", filepath.Join(dir, binary), ".")
	observation, err = adapter.Observe(ctx, run.OSExecRunner{}, tool, method)
	if err != nil || observation.Presence != plan.PresencePresent || observation.Identity.Revision != "" {
		t.Fatalf("dirty Go build claimed revision: %+v, err=%v", observation, err)
	}

	// An arbitrary non-Go executable keeps legacy presence-only semantics.
	if err := os.WriteFile(filepath.Join(dir, binary), []byte("not a Go binary"), 0o755); err != nil { // #nosec G306 -- fixture executable requires owner execute permission.
		t.Fatal(err)
	}
	observation, err = adapter.Observe(ctx, run.OSExecRunner{}, tool, method)
	if err != nil || observation.Presence != plan.PresencePresent || observation.Identity.Revision != "" {
		t.Fatalf("non-Go binary observation = %+v, err=%v", observation, err)
	}

	if err := os.Remove(filepath.Join(dir, binary)); err != nil {
		t.Fatal(err)
	}
	observation, err = adapter.Observe(ctx, run.OSExecRunner{}, tool, method)
	if err != nil || observation.Presence != plan.PresenceAbsent {
		t.Fatalf("missing declared path observation = %+v, err=%v", observation, err)
	}
}

func TestGitBinaryObservationRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("elsewhere", filepath.Join(dir, "tool")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"extract_to": dir, "binary": "tool", "artifact": "tool",
	}}
	observation, err := NewGitAdapter().Observe(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "tool"}, method)
	if err != nil || observation.Presence != plan.PresenceBroken {
		t.Fatalf("symlink observation = %+v, err=%v", observation, err)
	}
}

func TestGitBinaryObservationPreservesValidSymlinkPresence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "actual"), []byte("not a Go binary"), 0o755); err != nil { // #nosec G306 -- fixture executable requires owner execute permission.
		t.Fatal(err)
	}
	if err := os.Symlink("actual", filepath.Join(dir, "tool")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"extract_to": dir, "binary": "tool", "artifact": "tool",
	}}
	adapter := NewGitAdapter()
	observation, err := adapter.Observe(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "tool"}, method)
	if err != nil || observation.Presence != plan.PresencePresent || observation.Identity.Revision != "" {
		t.Fatalf("symlinked entrypoint observation = %+v, err=%v", observation, err)
	}
	if !adapter.Check(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "tool"}, method) {
		t.Fatal("existing symlinked entrypoint should pass presence check")
	}
}
