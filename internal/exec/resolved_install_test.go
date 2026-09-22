package exec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// resolvingMock is a PlanResolver + ResolvedInstaller test double. ResolvePlan
// returns a deterministic concrete URL (A on the first call, B afterwards to
// detect double resolution); InstallResolved records the plan it received;
// legacy Install fails the test when called.
type resolvingMock struct {
	testMockAdapter
	calls               int
	urlA, urlB          string
	gotURL              string
	legacyInstallCalled bool
}

func newResolvingMock(kind, urlA, urlB string) *resolvingMock {
	m := &resolvingMock{urlA: urlA, urlB: urlB}
	m.kindValue = kind
	return m
}

func (m *resolvingMock) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	m.calls++
	url := m.urlA
	if m.calls > 1 {
		url = m.urlB
	}
	resolved := intent.Clone()
	if len(resolved.Artifacts) == 0 {
		resolved.Artifacts = []plan.Artifact{{URL: url}}
	} else {
		resolved.Artifacts[0].URL = url
	}
	return &resolved, nil
}

func (m *resolvingMock) InstallResolved(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if len(resolved.Artifacts) > 0 {
		m.gotURL = resolved.Artifacts[0].URL
	}
	return nil
}

func (m *resolvingMock) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	m.legacyInstallCalled = true
	return errors.New("legacy Install must not be called for resolving adapters")
}

func resolvingSchema(kind string) *config.Schema {
	return &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{kind}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name:       "demo",
				MethodOnly: []string{kind},
				Methods: []*config.MethodCandidate{
					{Kind: kind, Config: map[string]any{"pkg": "demo"}},
				},
			},
		},
	}
}

func TestExecutorRealInstallExecutesResolvedPlan(t *testing.T) {
	const urlA = "https://example.test/releases/demo-A.tar.gz"
	m := newResolvingMock("cargo", urlA, urlA)
	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(m)(ex)

	report, err := ex.Execute(context.Background(), resolvingSchema("cargo"), "")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled {
		t.Fatalf("report = %+v, want one installed tool", report.Tools)
	}
	if m.legacyInstallCalled {
		t.Fatal("legacy Install() was called; real install must use InstallResolved()")
	}
	if m.gotURL != urlA {
		t.Fatalf("InstallResolved URL = %q, want %q", m.gotURL, urlA)
	}
	if got := report.Tools[0].PlanIntent; got == nil || len(got.Artifacts) == 0 || got.Artifacts[0].URL != urlA {
		t.Fatalf("result PlanIntent = %+v, want concrete URL %q", got, urlA)
	}
}

func TestExecutorResolvesCandidateOnlyOnce(t *testing.T) {
	const urlA = "https://example.test/releases/demo-A.tar.gz"
	const urlB = "https://example.test/releases/demo-B.tar.gz"
	m := newResolvingMock("cargo", urlA, urlB)
	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(m)(ex)

	report, err := ex.Execute(context.Background(), resolvingSchema("cargo"), "")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if m.calls != 1 {
		t.Fatalf("ResolvePlan calls = %d, want exactly 1", m.calls)
	}
	if m.gotURL != urlA {
		t.Fatalf("installed URL = %q, want first resolution %q (second would be %q)", m.gotURL, urlA, urlB)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled {
		t.Fatalf("report = %+v, want installed", report.Tools)
	}
}

func TestExecutorDryRunAndInstallUseEquivalentResolvedPlan(t *testing.T) {
	const urlA = "https://example.test/releases/demo-1.2.3.tar.gz"
	runOnce := func(dryRun bool) *plan.ResolvedInstallPlan {
		m := newResolvingMock("cargo", urlA, urlA)
		ex := New()
		WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
		WithAdapters(m)(ex)
		if dryRun {
			WithDryRun()(ex)
		}
		report, err := ex.Execute(context.Background(), resolvingSchema("cargo"), "")
		if err != nil {
			t.Fatalf("Execute(dryRun=%v) error = %v", dryRun, err)
		}
		if len(report.Tools) != 1 {
			t.Fatalf("tools = %+v", report.Tools)
		}
		return report.Tools[0].PlanIntent
	}
	dry := runOnce(true)
	real := runOnce(false)
	if dry == nil || real == nil {
		t.Fatalf("plans = %+v / %+v, want both concrete", dry, real)
	}
	if len(dry.Artifacts) == 0 || len(real.Artifacts) == 0 || dry.Artifacts[0].URL != real.Artifacts[0].URL {
		t.Fatalf("dry-run URL = %+v, install URL = %+v, want identical", dry.Artifacts, real.Artifacts)
	}
	if dry.Artifacts[0].URL != urlA {
		t.Fatalf("resolved URL = %q, want %q", dry.Artifacts[0].URL, urlA)
	}
}

// compatRejectingMock resolves to a .deb and rejects it for the host. It
// records every mutation surface to prove rejection happens before any of
// them.
type compatRejectingMock struct {
	testMockAdapter
	resolveCalls  int
	installCalls  int
	prereqInstall *int
}

func (m *compatRejectingMock) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	m.resolveCalls++
	resolved := intent.Clone()
	if len(resolved.Artifacts) == 0 {
		resolved.Artifacts = []plan.Artifact{{URL: "https://example.test/releases/tool.deb"}}
	} else {
		resolved.Artifacts[0].URL = "https://example.test/releases/tool.deb"
	}
	return &resolved, nil
}

func (m *compatRejectingMock) CheckHostCompatibility(_ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan, _ *engine.Facts, _ string) error {
	if len(intent.Artifacts) > 0 && strings.HasSuffix(intent.Artifacts[0].URL, ".deb") {
		return &installError{msg: "resolved .deb is incompatible with this host"}
	}
	return nil
}

func (m *compatRejectingMock) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	m.installCalls++
	return nil
}

func (m *compatRejectingMock) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	m.installCalls++
	return errors.New("legacy Install must not be called")
}

func TestExecutorChecksCompatibilityAfterResolution(t *testing.T) {
	prereqInstalls := 0
	primary := &compatRejectingMock{}
	primary.kindValue = "cargo"
	prereq := &testMockAdapter{kindValue: "go", installFunc: func(string) error {
		prereqInstalls++
		return nil
	}}
	primary.prereqInstall = &prereqInstalls
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"cargo", "go"}},
		Tools: map[string]*config.Tool{
			"helper": {
				Name:       "helper",
				MethodOnly: []string{"go"},
				Methods:    []*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": "helper"}}},
			},
			"demo": {
				Name:       "demo",
				MethodOnly: []string{"cargo"},
				Methods: []*config.MethodCandidate{{
					Kind:     "cargo",
					Config:   map[string]any{"pkg": "demo"},
					Requires: []string{"helper"},
					Sources:  []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}},
				}},
			},
		},
	}
	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(primary, prereq)(ex)
	WithFacts(&engine.Facts{OS: "linux", DistroID: "arch"})(ex)

	tool := schema.Tools["demo"]
	result := &ToolResult{Tool: tool.Name}
	ex.tryMethods(context.Background(), tool, result, time.Now())

	if primary.resolveCalls != 1 {
		t.Fatalf("resolve calls = %d, want 1", primary.resolveCalls)
	}
	if len(result.Methods) != 1 || result.Methods[0].Status != "skip_unavailable" {
		t.Fatalf("methods = %+v, want one compat skip", result.Methods)
	}
	if primary.installCalls != 0 {
		t.Fatalf("install calls = %d, want 0 (rejected before mutation)", primary.installCalls)
	}
	if prereqInstalls != 0 {
		t.Fatalf("prerequisite installs = %d, want 0 (lazy requires run only after gates)", prereqInstalls)
	}
}

// resolverWithoutInstaller implements PlanResolver but deliberately omits
// ResolvedInstaller: real installs must fail closed before any mutation.
type resolverWithoutInstaller struct {
	testMockAdapter
	resolveCalls  int
	installCalls  int
	prereqInstall *int
}

func (m *resolverWithoutInstaller) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	m.resolveCalls++
	resolved := intent.Clone()
	if len(resolved.Artifacts) == 0 {
		resolved.Artifacts = []plan.Artifact{{URL: "https://example.test/releases/demo.tar.gz"}}
	} else {
		resolved.Artifacts[0].URL = "https://example.test/releases/demo.tar.gz"
	}
	return &resolved, nil
}

func TestExecutorFailsClosedWhenResolverHasNoResolvedInstaller(t *testing.T) {
	prereqInstalls := 0
	primary := &resolverWithoutInstaller{}
	primary.kindValue = "cargo"
	prereq := &testMockAdapter{kindValue: "go", installFunc: func(string) error {
		prereqInstalls++
		return nil
	}}
	primary.prereqInstall = &prereqInstalls
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"cargo", "go"}},
		Tools: map[string]*config.Tool{
			"helper": {
				Name:       "helper",
				MethodOnly: []string{"go"},
				Methods:    []*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": "helper"}}},
			},
			"demo": {
				Name:       "demo",
				MethodOnly: []string{"cargo"},
				Methods: []*config.MethodCandidate{{
					Kind:     "cargo",
					Config:   map[string]any{"pkg": "demo"},
					Requires: []string{"helper"},
					Sources:  []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}},
				}},
			},
		},
	}
	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(primary, prereq)(ex)

	tool := schema.Tools["demo"]
	result := &ToolResult{Tool: tool.Name}
	ex.tryMethods(context.Background(), tool, result, time.Now())

	if primary.resolveCalls != 1 {
		t.Fatalf("resolve calls = %d, want 1", primary.resolveCalls)
	}
	if len(result.Methods) != 1 || result.Methods[0].Status != "failed" {
		t.Fatalf("methods = %+v, want one fail-closed failure", result.Methods)
	}
	if !strings.Contains(result.Methods[0].Error, "ResolvedInstaller") {
		t.Fatalf("error = %q, want fail-closed ResolvedInstaller message", result.Methods[0].Error)
	}
	if primary.installCalls != 0 {
		t.Fatalf("install calls = %d, want 0", primary.installCalls)
	}
	if prereqInstalls != 0 {
		t.Fatalf("prerequisite installs = %d, want 0 (fail before mutation)", prereqInstalls)
	}
}

func TestExplainToolUsesResolvedPlan(t *testing.T) {
	const urlA = "https://example.test/releases/demo-9.9.9.tar.gz"
	m := newResolvingMock("cargo", urlA, urlA)
	tool := resolvingSchema("cargo").Tools["demo"]

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(m)(ex)

	attempts := ex.ExplainTool(context.Background(), tool, "")
	if len(attempts) != 1 {
		t.Fatalf("attempts = %+v, want 1", attempts)
	}
	got := attempts[0].PlanIntent
	if got == nil || len(got.Artifacts) == 0 || got.Artifacts[0].URL != urlA {
		t.Fatalf("why PlanIntent = %+v, want concrete URL %q", got, urlA)
	}

	dry := New()
	dryMock := newResolvingMock("cargo", urlA, urlA)
	WithRunner(&run.FakeRunner{ExitCode: 0})(dry)
	WithAdapters(dryMock)(dry)
	WithDryRun()(dry)
	report, err := dry.Execute(context.Background(), resolvingSchema("cargo"), "")
	if err != nil {
		t.Fatalf("dry-run Execute() error = %v", err)
	}
	dryURL := report.Tools[0].PlanIntent.Artifacts[0].URL
	if dryURL != got.Artifacts[0].URL {
		t.Fatalf("why URL = %q, dry-run URL = %q, want identical", got.Artifacts[0].URL, dryURL)
	}
}

func TestExplainDefersAvailabilityForMissingSource(t *testing.T) {
	adapter := &availabilityMockAdapter{
		testMockAdapter: testMockAdapter{
			kindValue: "cargo",
			checkFunc: func(string) bool { return false },
		},
		checkAvailableFunc: func(string) bool { return false },
	}
	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(adapter)(ex)
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{"cargo"},
		Methods: []*config.MethodCandidate{{
			Kind:    "cargo",
			Config:  map[string]any{"pkg": "demo"},
			Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}},
		}},
	}
	attempts := ex.ExplainTool(context.Background(), tool, "")
	if len(attempts) != 1 {
		t.Fatalf("attempts = %+v, want 1", attempts)
	}
	if attempts[0].Status == "skip_unavailable" && strings.Contains(attempts[0].Error, "repo/index") {
		t.Fatalf("attempt = %+v, why must not reject on a stale index while a declared source is missing", attempts[0])
	}
	if attempts[0].Status != "would_install" {
		t.Fatalf("status = %q, want would_install (availability deferred)", attempts[0].Status)
	}
}
