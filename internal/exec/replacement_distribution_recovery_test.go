package exec

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type effectiveDistributionRecoveryAdapter struct {
	*executorAdapterV2Double
	observedConfigs []map[string]any
	installedConfig map[string]any
}

func (a *effectiveDistributionRecoveryAdapter) Observe(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	index := len(a.observedConfigs)
	a.observedConfigs = append(a.observedConfigs, cloneMethodConfig(method.Config))
	if index != 3 {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	identity := a.installed.Identity
	known := []plan.IdentityField{plan.FieldVersion}
	if identity.Package != "" {
		known = append(known, plan.FieldPackage)
	}
	return plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: identity.Package, Version: identity.Version}, KnownFields: known}, nil
}

func (a *effectiveDistributionRecoveryAdapter) InstallResolved(ctx context.Context, runner run.Runner, tool *config.Tool, method *config.MethodCandidate, desired *plan.ResolvedInstallPlan) error {
	a.installedConfig = cloneMethodConfig(method.Config)
	return a.executorAdapterV2Double.InstallResolved(ctx, runner, tool, method, desired)
}
func effectiveDistributionConfigMatches(got map[string]any) bool {
	if got["repo"] != "owner/repo" || got["asset"] != "linux.tar.gz" || got["binary"] != "linux-bin" || got["extract_to"] != "/opt/demo-linux" {
		return false
	}
	switch entrypoints := got["entrypoints"].(type) {
	case map[string]string:
		return entrypoints["demo"] == "bin/demo-linux"
	case map[string]any:
		return entrypoints["demo"] == "bin/demo-linux"
	default:
		return false
	}
}
func TestReplacementRecoveryUsesEffectiveGitHubDistribution(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "tool"
	const specializedExtractTo = "/opt/demo-linux"
	effectiveEntrypoints := map[string]string{"demo": "bin/demo-linux"}
	method := &config.MethodCandidate{
		Kind: "github",
		Config: map[string]any{
			"repo":        "owner/repo",
			"asset":       "base.tar.gz",
			"binary":      "base-bin",
			"extract_to":  "/opt/demo-base",
			"entrypoints": map[string]string{"demo": "bin/demo-base"},
		},
		Distributions: []config.Distribution{{
			When:         &config.Condition{OS: []string{"linux"}},
			Asset:        config.AssetSelector{Template: "linux.tar.gz"},
			HasAsset:     true,
			Binary:       "linux-bin",
			HasBinary:    true,
			ExtractTo:    specializedExtractTo,
			HasExtractTo: true,
			Entrypoints:  effectiveEntrypoints,
		}},
	}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	facts := &platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"}
	effective, err := config.SpecializeDistribution(method, facts)
	if err != nil {
		t.Fatal(err)
	}
	if effective == nil {
		t.Fatal("test method did not specialize for Linux")
	}
	desired, err := candidatePlanIntentErr(tool, effective)
	if err != nil || desired == nil {
		t.Fatalf("candidate intent = %v, %v", desired, err)
	}
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(*desired)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{
		Method: "github", MethodKind: "github", Version: "1.0.0",
		Config: cloneMethodConfig(effective.Config),
	}
	setup, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	setup.State().Tools[name] = previous
	candidate := state.ReplacementCandidate{Identity: projection.Candidate}
	if err := setup.BeginReplacement(name, "github", candidate, candidate, previous, projection, "", nil); err != nil {
		_ = setup.Close()
		t.Fatal(err)
	}
	if err := setup.PlanReplacementRemoval(name); err != nil {
		_ = setup.Close()
		t.Fatal(err)
	}
	if err := setup.RecordReplacementRemoved(name); err != nil {
		_ = setup.Close()
		t.Fatal(err)
	}
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &effectiveDistributionRecoveryAdapter{executorAdapterV2Double: &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "github"},
		presence:        plan.PresenceAbsent,
	}}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithFacts(facts)(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "linux")
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locked.Close() }()
	tx, err := locked.ReplacementTransaction(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.recoverReplacementTransaction(context.Background(), rc, locked, name); err != nil {
		t.Fatalf("recover replacement: %v", err)
	}

	want := map[string]any{
		"repo":        "owner/repo",
		"asset":       "linux.tar.gz",
		"binary":      "linux-bin",
		"extract_to":  specializedExtractTo,
		"entrypoints": effectiveEntrypoints,
	}
	if len(adapter.observedConfigs) != 4 {
		t.Fatalf("observations = %d, want old and desired candidate before and after install", len(adapter.observedConfigs))
	}
	for i, observed := range adapter.observedConfigs {
		if !effectiveDistributionConfigMatches(observed) {
			t.Fatalf("observation %d config = %v, want effective distribution %v", i, observed, want)
		}
	}
	if !effectiveDistributionConfigMatches(adapter.installedConfig) {
		t.Fatalf("installed config = %v, want effective distribution %v", adapter.installedConfig, want)
	}
	if adapter.resolveCall != 0 {
		t.Fatalf("ResolvePlan calls = %d, want zero during recovery", adapter.resolveCall)
	}
	if adapter.installed == nil {
		t.Fatal("recovery did not install a resolved plan")
	}
	foundAsset := false
	for _, operation := range adapter.installed.Operations {
		if operation.Kind == "resolve-artifact" && strings.Contains(operation.Description, "linux.tar.gz") {
			foundAsset = true
		}
	}
	if !foundAsset {
		t.Fatalf("installed plan operations = %+v, want specialized asset", adapter.installed.Operations)
	}
	installedPin, err := plan.ProjectLock(*adapter.installed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(installedPin, tx.Desired) {
		t.Fatalf("installed plan pin = %+v, want journaled lock pin %+v", installedPin, tx.Desired)
	}
	persisted, err := locked.ReplacementTransaction(name)
	if err == nil {
		t.Fatalf("replacement transaction remained after successful recovery: %+v", persisted)
	}
	if got := locked.State().Tools[name].Config; !effectiveDistributionConfigMatches(got) {
		t.Fatalf("persisted tool config = %v, want effective distribution %v", got, want)
	}
}
