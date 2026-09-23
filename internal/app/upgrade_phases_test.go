package app

// Unit tests for the runUpgrade phase helpers. These run fully in-process:
// no helper binary is spawned and no test asserts via process exit —
// outcomes are checked through returned values and ExitError codes.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

// phaseTestAdapter is a self-contained stub for the single-tool upgrade path.
// Probe results are canned so no host subprocess ever runs.
type phaseTestAdapter struct {
	available   bool
	installed   bool
	canRemove   bool
	observation *plan.Observation
	calls       []string
}

func (a *phaseTestAdapter) Kind() string { return "go" }
func (a *phaseTestAdapter) Available(context.Context, run.Runner) bool {
	a.calls = append(a.calls, "available")
	return a.available
}
func (a *phaseTestAdapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.calls = append(a.calls, "check")
	return a.installed
}
func (a *phaseTestAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.calls = append(a.calls, "check-available")
	return true
}
func (a *phaseTestAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}
func (a *phaseTestAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	a.calls = append(a.calls, "resolve-plan")
	resolved := intent.Clone()
	return &resolved, nil
}
func (a *phaseTestAdapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	a.calls = append(a.calls, "observe")
	if a.observation != nil {
		return *a.observation, nil
	}
	presence := plan.PresenceAbsent
	if a.installed {
		presence = plan.PresencePresent
	}
	return plan.Observation{Presence: presence, Identity: plan.ObservedIdentity{Package: "example.test/demo", Version: "v0.1.0"}, KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion}}, nil
}
func (a *phaseTestAdapter) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	a.calls = append(a.calls, "install-resolved")
	return nil
}

var _ exec.AdapterV2 = (*phaseTestAdapter)(nil)

func (a *phaseTestAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	a.calls = append(a.calls, "install")
	return nil
}
func (a *phaseTestAdapter) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	a.calls = append(a.calls, "remove")
	return nil
}
func (a *phaseTestAdapter) CanRemove() bool { return a.canRemove }

func phaseTestGoTool(pkg string) (*config.Tool, *config.MethodCandidate) {
	method := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": pkg, "version": "v0.2.0"}}
	return &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}, method
}

func phaseTestRunner() *run.LoggingRunner {
	return run.NewLoggingRunner(&run.FakeRunner{}, log.Default)
}

func TestResolveUpgradeManifestPathPassthrough(t *testing.T) {
	cases := []struct {
		name       string
		noManifest bool
		flag       string
		wantPath   string
		wantAuto   bool
	}{
		{name: "explicit flag wins", flag: "/x/manifest.toml", wantPath: "/x/manifest.toml"},
		{name: "no-manifest disables lookup", noManifest: true, wantPath: ""},
		{name: "no-manifest keeps explicit flag", noManifest: true, flag: "/x/manifest.toml", wantPath: "/x/manifest.toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, auto := resolveUpgradeManifestPath(tc.noManifest, tc.flag)
			if got != tc.wantPath || auto != tc.wantAuto {
				t.Fatalf("resolveUpgradeManifestPath(%v, %q) = (%q, %v), want (%q, %v)",
					tc.noManifest, tc.flag, got, auto, tc.wantPath, tc.wantAuto)
			}
		})
	}
}

func TestResolveUpgradeManifestPathDefaultConsistent(t *testing.T) {
	got, auto := resolveUpgradeManifestPath(false, "")
	want := config.DefaultManifestPath()
	if got != want || auto != (want != "") {
		t.Fatalf("resolveUpgradeManifestPath(false, \"\") = (%q, %v), want (%q, %v)",
			got, auto, want, want != "")
	}
}

func phaseTestDriftFixture() (*state.State, *config.Schema, *lock.Lock) {
	oldMethod := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/old"}}
	curMethod := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/current"}}
	unkMethod := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/unknown"}}
	s := &config.Schema{Tools: map[string]*config.Tool{
		"old":     {Name: "old", Methods: []*config.MethodCandidate{oldMethod}},
		"current": {Name: "current", Methods: []*config.MethodCandidate{curMethod}},
		"unknown": {Name: "unknown", Methods: []*config.MethodCandidate{unkMethod}},
	}}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"old/go/0":     {Latest: "v0.2.0"},
		"current/go/0": {Latest: "v0.1.0"},
		"unknown/go/0": {Latest: "v0.2.0"},
	}}
	st := &state.State{Tools: map[string]state.ToolState{
		"old":     {Method: "go", MethodKind: "go", Version: "v0.1.0"},
		"current": {Method: "go", MethodKind: "go", Version: "v0.1.0"},
		"unknown": {Method: "go", MethodKind: "go", Version: ""},
		"ghost":   {Method: "go", MethodKind: "go", Version: "v0.0.1"},
	}}
	return st, s, lk
}

func TestCollectOutdatedToolsFindsDriftOnly(t *testing.T) {
	st, s, lk := phaseTestDriftFixture()
	outdated, failures := collectOutdatedTools(st, s, lk, "", []string{"go"}, "")
	if len(failures) != 0 {
		t.Fatalf("failures = %+v, want none", failures)
	}
	if len(outdated) != 1 || outdated[0].name != "old" {
		t.Fatalf("outdated = %+v, want exactly [old]", outdated)
	}
	ot := outdated[0]
	if ot.pinnedVer != "v0.2.0" || ot.methodKind != "go" || ot.method == nil {
		t.Fatalf("outdated[0] = %+v, want pinned v0.2.0 with resolved candidate", ot)
	}
}

func TestCollectOutdatedToolsOnlyFilter(t *testing.T) {
	st, s, lk := phaseTestDriftFixture()
	outdated, failures := collectOutdatedTools(st, s, lk, "current", []string{"go"}, "")
	if len(outdated) != 0 || len(failures) != 0 {
		t.Fatalf("outdated = %+v, failures = %+v, want both empty", outdated, failures)
	}
}

func TestCollectOutdatedToolsDiscoveryFailure(t *testing.T) {
	tool := &config.Tool{Name: "amb", Methods: []*config.MethodCandidate{
		{Kind: "go", Label: "a", Config: map[string]any{"pkg": "example.test/a"}},
		{Kind: "go", Label: "b", Config: map[string]any{"pkg": "example.test/b"}},
	}}
	s := &config.Schema{Tools: map[string]*config.Tool{"amb": tool}}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{"amb/go/0": {Latest: "v0.9.0"}}}
	st := &state.State{Tools: map[string]state.ToolState{
		"amb": {Method: "go", MethodKind: "go", Version: "v0.1.0"},
	}}
	outdated, failures := collectOutdatedTools(st, s, lk, "", []string{"go"}, "")
	if len(outdated) != 0 {
		t.Fatalf("outdated = %+v, want none", outdated)
	}
	if len(failures) != 1 || failures[0].Tool != "amb" {
		t.Fatalf("failures = %+v, want exactly [amb]", failures)
	}
	if failures[0].Status != "failed" || !strings.Contains(failures[0].Error, "cannot resolve tracked candidate") {
		t.Fatalf("failure = %+v, want failed/cannot-resolve", failures[0])
	}
}

func TestShouldPromptUpgrade(t *testing.T) {
	cases := []struct {
		name                             string
		force, dryRun, json, interactive bool
		want                             bool
	}{
		{name: "clear run prompts", interactive: true, want: true},
		{name: "force skips", force: true, interactive: true},
		{name: "dry-run skips", dryRun: true, interactive: true},
		{name: "json skips", json: true, interactive: true},
		{name: "non-interactive skips"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPromptUpgrade(tc.force, tc.dryRun, tc.json, tc.interactive); got != tc.want {
				t.Fatalf("shouldPromptUpgrade = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteUpgradeReportExitCodes(t *testing.T) {
	if err := writeUpgradeReport(false, false, upgradeCounts{}, nil); err != nil {
		t.Fatalf("clean report error = %v, want nil", err)
	}
	err := writeUpgradeReport(false, false,
		upgradeCounts{upgraded: 1, failed: 2},
		[]upgradeResult{{Tool: "demo", Status: "failed", Error: "boom"}})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("failed report error = %#v, want ExitError{1}", err)
	}
	if err := writeUpgradeReport(true, true,
		upgradeCounts{wouldUpgrade: 1},
		[]upgradeResult{{Tool: "demo", Status: "would_upgrade", NewVer: "v0.2.0"}}); err != nil {
		t.Fatalf("json dry-run report error = %v, want nil", err)
	}
}

func TestUpgradeSingleToolNoAdapter(t *testing.T) {
	tool, method := phaseTestGoTool("example.test/demo")
	ex := exec.New()
	st := &state.State{Tools: map[string]state.ToolState{}}
	ot := upgradeOutdatedTool{
		name: "demo", ts: state.ToolState{Method: "phase-test-no-such-kind", MethodKind: "phase-test-no-such-kind", Version: "v0.1.0"},
		pinnedVer: "v0.2.0", tool: tool, method: method, methodKind: "phase-test-no-such-kind",
	}
	res := upgradeSingleTool(context.Background(), ex, phaseTestRunner(), &engine.Facts{}, st, ot, upgradeOptions{quiet: true}, nil)
	if res.Status != "failed" || !strings.Contains(res.Error, "no adapter for method") {
		t.Fatalf("result = %+v, want failed/no-adapter", res)
	}
}

func TestUpgradeSingleToolDryRunSkipsMutation(t *testing.T) {
	tool, method := phaseTestGoTool("example.test/demo")
	adapter := &phaseTestAdapter{available: true, installed: true, canRemove: true}
	ex := exec.New()
	exec.WithAdapters(adapter)(ex)
	st := &state.State{Tools: map[string]state.ToolState{
		"demo": {Method: "go", MethodKind: "go", Version: "v0.1.0"},
	}}
	ot := upgradeOutdatedTool{
		name: "demo", ts: st.Tools["demo"],
		pinnedVer: "v0.2.0", tool: tool, method: method, methodKind: "go",
	}
	res := upgradeSingleTool(context.Background(), ex, phaseTestRunner(), &engine.Facts{}, st, ot,
		upgradeOptions{dryRun: true, quiet: true}, nil)
	if res.Status != "would_upgrade" || res.NewVer != "v0.2.0" {
		t.Fatalf("result = %+v, want would_upgrade/v0.2.0", res)
	}
	if strings.Join(adapter.calls, ",") != "available,resolve-plan,observe,check-available" {
		t.Fatalf("adapter calls = %v, want probes only (no remove/install)", adapter.calls)
	}
	if st.Tools["demo"].Version != "v0.1.0" {
		t.Fatalf("state mutated by dry-run: %+v", st.Tools["demo"])
	}
}

func TestRunUpgradeLoopTalliesDiscoveryFailures(t *testing.T) {
	ex := exec.New()
	st := &state.State{Tools: map[string]state.ToolState{}}
	failures := []upgradeResult{{
		Tool: "amb", Status: "failed", OldVer: "v0.1.0", Method: "go",
		Error: "cannot resolve tracked candidate: boom",
	}}
	results, counts := runUpgradeLoop(context.Background(), ex, phaseTestRunner(), &engine.Facts{},
		st, nil, failures, upgradeOptions{quiet: true}, nil)
	if len(results) != 1 || results[0].Tool != "amb" {
		t.Fatalf("results = %+v, want the seeded failure", results)
	}
	if counts.failed != 1 || counts.upgraded != 0 || counts.skipped != 0 || counts.wouldUpgrade != 0 {
		t.Fatalf("counts = %+v, want exactly one failure", counts)
	}
}
