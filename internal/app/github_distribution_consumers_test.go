package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type githubDistributionSmokeAdapter struct {
	phaseTestAdapter
	seen map[string]any
}

func (*githubDistributionSmokeAdapter) Kind() string    { return "github" }
func (*githubDistributionSmokeAdapter) CanRemove() bool { return true }
func (a *githubDistributionSmokeAdapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	a.seen = mc.Config
	return a.phaseTestAdapter.ResolvePlan(ctx, rn, tool, mc, intent)
}
func (a *githubDistributionSmokeAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	a.seen = mc.Config
	return a.phaseTestAdapter.Observe(ctx, rn, tool, mc)
}

func TestStatusAndUpgradeResolveTrackedGitHubDistribution(t *testing.T) {
	root := t.TempDir()
	schemaPath := filepath.Join(root, "schema.toml")
	schema := []byte("schema_version = 1\n\n[tools.br.github]\nrepo = \"Dicklesworthstone/beads_rust\"\nasset = \"br-{version}-{os_any}_{arch_any}.tar.gz\"\nchecksum = \"sha256:auto\"\nscope = \"user\"\nbinary = \"br\"\nentrypoints = { br = \"base/br\" }\n\n[[tools.br.github.distributions]]\nwhen = { os = [\"linux\"], libc = [\"glibc\"] }\nbinary = \"br-linux\"\nentrypoints = { br = \"dist/linux/br\" }\n")
	if err := os.WriteFile(schemaPath, schema, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := config.ParseProjectSchema(schemaPath, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	tool := s.Tools["br"]
	adapter := &githubDistributionSmokeAdapter{phaseTestAdapter: phaseTestAdapter{available: true, observation: &plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "br", Version: "v0.9.0", Scope: "user"}, KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion, plan.FieldScope}}}}
	ex := exec.New()
	exec.WithAdapters(adapter)(ex)
	exec.WithRunner(&run.FakeRunner{})(ex)
	exec.WithDryRun()(ex)
	exec.WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", Libc: "glibc 2.35", TargetFamily: "unix"})(ex)
	lk := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{"br/github/0": {Latest: "v2.0.0"}}}
	lockPath := filepath.Join(root, "depengine.lock")
	if err := lock.Save(lockPath, lk); err != nil {
		t.Fatal(err)
	}
	loadedLock, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := state.ToolState{Method: "github", MethodKind: "github", Version: "v1.0.0", Config: map[string]any{"repo": "Dicklesworthstone/beads_rust"}, DefinitionHash: state.DefinitionHash(tool), DesiredStateHash: state.DesiredStateHash(tool)}
	installed := map[string]state.ToolState{"br": ts}
	rows := reconcileStatusTools(context.Background(), []toolStatus{{Name: "br", Status: "installed"}}, installed, s, loadedLock, ex, "unknown")
	if rows[0].Status != "installed" || rows[0].Verification == nil {
		t.Fatalf("status row=%+v verification=%+v", rows[0], *rows[0].Verification)
	}
	assertGitHubEffectiveDistribution(t, adapter.seen)
	st := &state.State{Version: state.CurrentVersion, Tools: installed}
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	if err := state.SaveLocked(st); err != nil {
		t.Fatal(err)
	}
	outdated, failures := collectOutdatedTools(context.Background(), st, s, loadedLock, "br", ex, "unknown")
	if len(failures) != 0 || len(outdated) != 1 {
		t.Fatalf("upgrade discovery outdated=%+v failures=%+v", outdated, failures)
	}
	assertGitHubEffectiveDistribution(t, adapter.seen)
	adapter.seen = nil
	result := upgradeSingleTool(context.Background(), ex, "unknown", outdated[0], upgradeOptions{dryRun: true, quiet: true}, nil)
	if result.Status != "would_upgrade" {
		t.Fatalf("upgrade dry-run result=%+v", result)
	}
	assertGitHubEffectiveDistribution(t, adapter.seen)
}
func assertGitHubEffectiveDistribution(t *testing.T, cfg map[string]any) {
	t.Helper()
	if cfg == nil {
		t.Fatal("github adapter did not receive effective config")
	}
	if cfg["binary"] != "br-linux" {
		t.Fatalf("effective binary=%#v", cfg["binary"])
	}
	eps, ok := cfg["entrypoints"].(map[string]any)
	if !ok || eps["br"] != "dist/linux/br" {
		t.Fatalf("effective entrypoints=%#v", cfg["entrypoints"])
	}
}
