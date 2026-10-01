package app

// Unit tests for upgrade discovery, reporting, and orchestration phases.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

// phaseTestAdapter is a self-contained probe adapter shared by app phase tests.
type phaseTestAdapter struct {
	available   bool
	observation *plan.Observation
}

func (*phaseTestAdapter) Kind() string                                 { return "go" }
func (a *phaseTestAdapter) Available(context.Context, run.Runner) bool { return a.available }
func (*phaseTestAdapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return false
}
func (*phaseTestAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}
func (*phaseTestAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}
func (*phaseTestAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	resolved := intent.Clone()
	return &resolved, nil
}
func (a *phaseTestAdapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	if a.observation != nil {
		return *a.observation, nil
	}
	return plan.Observation{}, nil
}
func (*phaseTestAdapter) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	return nil
}

var _ exec.AdapterV2 = (*phaseTestAdapter)(nil)

func (*phaseTestAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}
func (*phaseTestAdapter) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}
func (*phaseTestAdapter) CanRemove() bool { return false }

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
	lk := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{
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
	outdated, failures := collectOutdatedTools(context.Background(), st, s, lk, "", upgradeExecutorForMethodOrder([]string{"go"}), "")
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
	outdated, failures := collectOutdatedTools(context.Background(), st, s, lk, "current", upgradeExecutorForMethodOrder([]string{"go"}), "")
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
	lk := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{"amb/go/0": {Latest: "v0.9.0"}}}
	st := &state.State{Tools: map[string]state.ToolState{
		"amb": {Method: "go", MethodKind: "go", Version: "v0.1.0"},
	}}
	outdated, failures := collectOutdatedTools(context.Background(), st, s, lk, "", upgradeExecutorForMethodOrder([]string{"go"}), "")
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

func TestRunUpgradeLoopTalliesDiscoveryFailures(t *testing.T) {
	ex := exec.New()
	failures := []upgradeResult{{
		Tool: "amb", Status: "failed", OldVer: "v0.1.0", Method: "go",
		Error: "cannot resolve tracked candidate: boom",
	}}
	results, counts := runUpgradeLoop(context.Background(), ex, "", nil, failures, upgradeOptions{quiet: true}, nil)
	if len(results) != 1 || results[0].Tool != "amb" {
		t.Fatalf("results = %+v, want the seeded failure", results)
	}
	if counts.failed != 1 || counts.upgraded != 0 || counts.skipped != 0 || counts.wouldUpgrade != 0 {
		t.Fatalf("counts = %+v, want exactly one failure", counts)
	}
}
func TestCollectOutdatedToolsUsesV2ReconciliationInsteadOfRecordedVersion(t *testing.T) {
	method := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/demo"}}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"demo": tool}}
	desired := plan.New("demo", "go", true)
	desired.Identity.Package = "example.test/demo"
	desired.Identity.Version = "1.0.0"
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{desired})
	if err != nil {
		t.Fatal(err)
	}
	lk, err := lock.NewUniversal(document, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "example.test/old", Version: "1.0.0"},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}
	ex := exec.New()
	exec.WithAdapters(&phaseTestAdapter{available: true, observation: &observation})(ex)
	exec.WithDefaultMethodOrder([]string{"go"})(ex)
	exec.WithLockDocument(document)(ex)
	st := &state.State{Tools: map[string]state.ToolState{
		"demo": {Method: "go", MethodKind: "go", Version: ""},
	}}
	outdated, failures := collectOutdatedTools(context.Background(), st, schema, lk, "", ex, "")
	if len(failures) != 0 {
		t.Fatalf("failures = %+v, want none", failures)
	}
	if len(outdated) != 1 || outdated[0].resolved == nil || outdated[0].pinnedVer != "1.0.0" {
		t.Fatalf("outdated = %+v, want the drifted locked target despite absent recorded version and equal version string", outdated)
	}
}

func TestCollectOutdatedToolsReportsV2TrackedCandidateFailureWithoutLegacyPins(t *testing.T) {
	tool := &config.Tool{Name: "amb", Methods: []*config.MethodCandidate{
		{Kind: "go", Label: "a", Config: map[string]any{"pkg": "example.test/a"}},
		{Kind: "go", Label: "b", Config: map[string]any{"pkg": "example.test/b"}},
	}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"amb": tool}}
	desired := plan.New("amb", "go", true)
	desired.Identity.Version = "1.0.0"
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{desired})
	if err != nil {
		t.Fatal(err)
	}
	lk, err := lock.NewUniversal(document, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := &state.State{Tools: map[string]state.ToolState{
		"amb": {Method: "go", MethodKind: "go", Version: "0.9.0"},
	}}
	outdated, failures := collectOutdatedTools(context.Background(), st, schema, lk, "", upgradeExecutorForMethodOrder([]string{"go"}), "")
	if len(outdated) != 0 || len(failures) != 1 || failures[0].Tool != "amb" {
		t.Fatalf("outdated = %+v, failures = %+v, want a terminal failure for the v2-tracked tool", outdated, failures)
	}
}

func TestCollectOutdatedToolsFailsClosedOnUnknownV2Observation(t *testing.T) {
	method := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/demo"}}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"demo": tool}}
	desired := plan.New("demo", "go", true)
	desired.Identity.Package = "example.test/demo"
	desired.Identity.Version = "1.0.0"
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{desired})
	if err != nil {
		t.Fatal(err)
	}
	lk, err := lock.NewUniversal(document, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	observation := plan.Observation{Presence: plan.PresenceUnknown, Detail: "probe failed"}
	ex := exec.New()
	exec.WithAdapters(&phaseTestAdapter{available: true, observation: &observation})(ex)
	exec.WithDefaultMethodOrder([]string{"go"})(ex)
	exec.WithLockDocument(document)(ex)
	st := &state.State{Tools: map[string]state.ToolState{
		"demo": {Method: "go", MethodKind: "go", Version: "0.9.0"},
	}}
	outdated, failures := collectOutdatedTools(context.Background(), st, schema, lk, "", ex, "")
	if len(outdated) != 0 || len(failures) != 1 || failures[0].Status != "failed" {
		t.Fatalf("outdated = %+v, failures = %+v, want unknown observation blocked as failure", outdated, failures)
	}
}
