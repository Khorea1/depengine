package git

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// TestGitAdapterInstallResolvedUsesPlanIdentity proves InstallResolved builds
// its clone identity solely from the resolved plan: the method candidate
// points at a decoy URL/branch (any network resolution would also fail on the
// {latest} placeholder), while the plan carries the concrete source. The
// captured git clone argv must reference the plan, never the candidate.
func TestGitAdapterInstallResolvedUsesPlanIdentity(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 0}
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"url":    "https://example.invalid/decoy-{latest}.git",
		"branch": "decoy-branch",
	}}
	resolved := plan.New("demo", "git", true)
	resolved.Identity.Source = "https://example.invalid/resolved.git"
	resolved.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "from-plan"}

	if err := NewGitAdapter().InstallResolved(context.Background(), runner, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	var clone []string
	for _, call := range runner.Calls {
		if call.Name == "git" && len(call.Args) > 0 && call.Args[0] == "clone" {
			clone = append([]string{call.Name}, call.Args...)
		}
	}
	if len(clone) == 0 {
		t.Fatalf("no git clone recorded: %+v", runner.Calls)
	}
	joined := strings.Join(clone, " ")
	if !strings.Contains(joined, "https://example.invalid/resolved.git") {
		t.Fatalf("clone = %q, want plan source URL", joined)
	}
	if !strings.Contains(joined, "from-plan") {
		t.Fatalf("clone = %q, want plan branch from-plan", joined)
	}
	if strings.Contains(joined, "decoy") {
		t.Fatalf("clone = %q, must not reference candidate decoy identity", joined)
	}
}

// TestGitAdapterInstallResolvedReproducesResolvedTag verifies the {latest}
// path: a resolved tag carried in Identity.Version becomes the clone branch.
func TestGitAdapterInstallResolvedReproducesResolvedTag(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 0}
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"url": "https://example.invalid/decoy.git",
	}}
	resolved := plan.New("demo", "git", true)
	resolved.Identity.Source = "https://example.invalid/stripped.git"
	resolved.Identity.Version = "v9.9.9"

	if err := NewGitAdapter().InstallResolved(context.Background(), runner, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	found := false
	for _, call := range runner.Calls {
		if call.Name == "git" && len(call.Args) > 0 && call.Args[0] == "clone" {
			joined := strings.Join(append([]string{call.Name}, call.Args...), " ")
			if strings.Contains(joined, "v9.9.9") && strings.Contains(joined, "https://example.invalid/stripped.git") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no clone of stripped URL at v9.9.9: %+v", runner.Calls)
	}
}

// TestGitAdapterInstallResolvedRequiresConcreteSource is the fail-closed half.
func TestGitAdapterInstallResolvedRequiresConcreteSource(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"url": "https://example.invalid/decoy.git",
	}}
	resolved := plan.New("demo", "git", true)
	if err := NewGitAdapter().InstallResolved(context.Background(), &run.FakeRunner{}, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved() with no source succeeded, want error")
	}
}
