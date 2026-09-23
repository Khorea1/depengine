package exec

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type countingAdapter struct {
	v2TestStub
	mu     sync.Mutex
	checks map[string]int
}

type plannedCountingAdapter struct{ *countingAdapter }

func (*plannedCountingAdapter) Kind() string { return "go" }

func (a *countingAdapter) Kind() string                               { return "counting" }
func (a *countingAdapter) Available(context.Context, run.Runner) bool { return true }
func (a *countingAdapter) Check(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checks[tool.Name]++
	return false
}
func (a *countingAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if a.Check(ctx, rn, tool, mc) {
		return plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "x"}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, nil
	}
	return plan.Observation{Presence: plan.PresenceAbsent}, nil
}
func (a *countingAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, _ *plan.ResolvedInstallPlan) error {
	return a.Install(ctx, rn, tool, mc)
}

func (a *countingAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}

func TestLazyDependencyRunsOnceUnderConcurrency(t *testing.T) {
	adapter := &plannedCountingAdapter{countingAdapter: &countingAdapter{checks: map[string]int{}}}
	method := func(requires ...string) []*config.MethodCandidate {
		return []*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": "x"}, Requires: requires}}
	}
	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"go"}}, Tools: map[string]*config.Tool{
		"helper": {Name: "helper", DependencyOnly: true, Methods: method()},
		"a":      {Name: "a", Methods: method("helper")},
		"b":      {Name: "b", Methods: method("helper")},
	}}
	executor := New()
	WithAdapters(adapter)(executor)
	WithDryRun()(executor)
	WithMaxJobs(2)(executor)
	if _, err := executor.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}
	if got := adapter.checks["helper"]; got != 1 {
		t.Fatalf("helper checks=%d want=1", got)
	}
}

func TestUnavailableCandidateDoesNotRunLazyDependencies(t *testing.T) {
	checks := map[string]int{}
	installs := map[string]int{}
	adapter := &availabilityMockAdapter{
		testMockAdapter: testMockAdapter{
			kindValue: "go",
			checkFunc: func(name string) bool {
				checks[name]++
				return false
			},
			installFunc: func(name string) error {
				installs[name]++
				return nil
			},
		},
		checkAvailableFunc: func(name string) bool { return name != "owner" },
	}
	method := func(requires ...string) []*config.MethodCandidate {
		return []*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": "example.test/tool"}, Requires: requires}}
	}
	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"go"}}, Tools: map[string]*config.Tool{
		"helper": {Name: "helper", DependencyOnly: true, Methods: method()},
		"owner":  {Name: "owner", Methods: method("helper")},
	}}
	executor := New()
	WithAdapters(adapter)(executor)

	report, err := executor.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Tool != "owner" || report.Tools[0].Status != StatusSkippedUnavailable {
		t.Fatalf("report=%+v, want owner skipped as unavailable", report.Tools)
	}
	if checks["helper"] != 0 || installs["helper"] != 0 {
		t.Fatalf("unavailable owner prepared lazy dependency: checks=%v installs=%v", checks, installs)
	}
}

func TestFailedCandidateRetainsLazyDependencyExplicitlyInReportAndState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	primary := &testMockAdapter{
		kindValue: "go",
		checkFunc: func(string) bool { return false },
		installFunc: func(name string) error {
			if name == "owner" {
				return errors.New("primary install failed")
			}
			return nil
		},
	}
	fallback := &testMockAdapter{
		kindValue:   "http",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return nil },
	}
	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"go", "http"}}, Tools: map[string]*config.Tool{
		"helper": {
			Name:           "helper",
			DependencyOnly: true,
			Methods: []*config.MethodCandidate{{
				Kind: "go", Config: map[string]any{"pkg": "example.test/helper"},
			}},
		},
		"owner": {
			Name: "owner",
			Methods: []*config.MethodCandidate{
				{Kind: "go", Config: map[string]any{"pkg": "example.test/owner"}, Requires: []string{"helper"}},
				{Kind: "http", Config: map[string]any{"url": "https://example.test/owner"}},
			},
		},
	}}
	executor := New()
	WithAdapters(primary, fallback)(executor)
	WithSchemaInfo("/test/schema.toml", time.Now())(executor)

	report, err := executor.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	var helper, owner *ToolResult
	for i := range report.Tools {
		switch report.Tools[i].Tool {
		case "helper":
			helper = &report.Tools[i]
		case "owner":
			owner = &report.Tools[i]
		}
	}
	if helper == nil || helper.Status != StatusInstalled {
		t.Fatalf("helper result = %#v, want explicit installed lazy dependency", helper)
	}
	if owner == nil || owner.Status != StatusInstalled || owner.MethodKind != "http" {
		t.Fatalf("owner result = %#v, want successful fallback", owner)
	}
	if len(owner.Methods) < 1 || owner.Methods[0].Status != "failed" {
		t.Fatalf("owner attempts = %#v, want failed primary candidate before fallback", owner.Methods)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Tools["helper"]; !ok {
		t.Fatalf("retained prerequisite is absent from state: %#v", st.Tools)
	}
	if _, ok := st.Tools["owner"]; !ok {
		t.Fatalf("fallback owner is absent from state: %#v", st.Tools)
	}
}

func TestSuccessfulMethodRequiresClaimsSharedPrerequisiteOwnership(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	adapter := &countingAdapter{checks: map[string]int{}}
	method := func(requires ...string) []*config.MethodCandidate {
		return []*config.MethodCandidate{{Kind: "counting", Config: map[string]any{"pkg": "x"}, Requires: requires}}
	}
	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"counting"}}, Tools: map[string]*config.Tool{
		"helper": {Name: "helper", DependencyOnly: true, Methods: method()},
		"a":      {Name: "a", Methods: method("helper")},
		"b":      {Name: "b", Methods: method("helper")},
	}}
	executor := New()
	WithAdapters(adapter)(executor)
	WithSchemaInfo("/test/schema.toml", time.Now())(executor)

	report, err := executor.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 {
		t.Fatalf("report = %+v, want successful owners", report.Tools)
	}
	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.OwnedResources) != 1 {
		t.Fatalf("owned resources = %#v, want one prerequisite", st.OwnedResources)
	}
	got := st.OwnedResources[0]
	if got.Resource != resource || got.Ownership != plan.OwnershipDepengine {
		t.Fatalf("prerequisite ownership = %#v, want depengine-owned %#v", got, resource)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(got.Dependents, want) {
		t.Fatalf("prerequisite dependents = %#v, want %#v", got.Dependents, want)
	}
}

func TestPreexistingMethodPrerequisiteIsTrackedAsExternal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	adapter := &testMockAdapter{
		kindValue:   "go",
		checkFunc:   func(name string) bool { return name == "helper" },
		installFunc: func(string) error { return nil },
	}
	method := func(pkg string, requires ...string) []*config.MethodCandidate {
		return []*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": pkg}, Requires: requires}}
	}
	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"go"}}, Tools: map[string]*config.Tool{
		"helper": {Name: "helper", DependencyOnly: true, Methods: method("x")},
		"owner":  {Name: "owner", Methods: method("owner", "helper")},
	}}
	executor := New()
	WithAdapters(adapter)(executor)
	WithSchemaInfo("/test/schema.toml", time.Now())(executor)

	if _, err := executor.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}
	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.OwnedResources) != 1 || st.OwnedResources[0].Ownership != plan.OwnershipExternal {
		t.Fatalf("owned resources = %#v, want external prerequisite", st.OwnedResources)
	}
	if want := []string{"owner"}; !reflect.DeepEqual(st.OwnedResources[0].Dependents, want) {
		t.Fatalf("dependents = %#v, want %#v", st.OwnedResources[0].Dependents, want)
	}
}

func TestStaticRequiresClaimsPrerequisiteAndPersistsRootIntent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	adapter := &countingAdapter{checks: map[string]int{}}
	method := func() []*config.MethodCandidate {
		return []*config.MethodCandidate{{Kind: "counting", Config: map[string]any{"pkg": "x"}}}
	}
	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"counting"}}, Tools: map[string]*config.Tool{
		"helper": {Name: "helper", DependencyOnly: true, Methods: method()},
		"owner":  {Name: "owner", Requires: []string{"helper"}, Methods: method()},
	}}
	executor := New()
	WithAdapters(adapter)(executor)
	WithSchemaInfo("/test/schema.toml", time.Now())(executor)

	if _, err := executor.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}
	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if st.Tools["helper"].RootRequested {
		t.Fatalf("helper root_requested = true, want false for dependency-only install")
	}
	if !st.Tools["owner"].RootRequested {
		t.Fatalf("owner root_requested = false, want true for root tool")
	}
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.OwnedResources) != 1 {
		t.Fatalf("owned resources = %#v, want one static prerequisite", st.OwnedResources)
	}
	got := st.OwnedResources[0]
	if got.Resource != resource || got.Ownership != plan.OwnershipDepengine {
		t.Fatalf("prerequisite ownership = %#v, want depengine-owned %#v", got, resource)
	}
	if want := []string{"owner"}; !reflect.DeepEqual(got.Dependents, want) {
		t.Fatalf("dependents = %#v, want %#v", got.Dependents, want)
	}
}

func TestRootRequestedIntentIsStickyWhenToolLaterBecomesDependencyOnly(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	adapter := &testMockAdapter{
		kindValue:   "counting",
		checkFunc:   func(string) bool { return true },
		installFunc: func(string) error { return nil },
	}
	method := func() []*config.MethodCandidate {
		return []*config.MethodCandidate{{Kind: "counting", Config: map[string]any{"pkg": "x"}}}
	}
	if err := state.Save(&state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"helper": {
				Method:         "counting",
				MethodKind:     "counting",
				InstalledAt:    time.Now().UTC().Format(time.RFC3339),
				DefinitionHash: "previous",
				RootRequested:  true,
				Config:         map[string]any{"pkg": "x"},
			},
		},
		PreparationPlans:    map[string]plan.PreparationPlan{},
		PreparationJournals: map[string]plan.PreparationJournal{},
	}); err != nil {
		t.Fatal(err)
	}

	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"counting"}}, Tools: map[string]*config.Tool{
		"helper": {Name: "helper", DependencyOnly: true, Methods: method()},
		"owner":  {Name: "owner", Requires: []string{"helper"}, Methods: method()},
	}}
	executor := New()
	WithAdapters(adapter)(executor)
	WithSchemaInfo("/test/schema.toml", time.Now())(executor)
	if _, err := executor.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Tools["helper"].RootRequested {
		t.Fatalf("helper root_requested = false after dependency-only run, want sticky true")
	}
}
