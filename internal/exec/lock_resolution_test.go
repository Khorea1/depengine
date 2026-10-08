package exec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

type lockCandidateAdapter struct {
	executorAdapterV2Double
	unavailable  bool
	resolveError map[*config.MethodCandidate]error
	incompatible map[*config.MethodCandidate]error
}

func (a *lockCandidateAdapter) Available(context.Context, run.Runner) bool { return !a.unavailable }

func (a *lockCandidateAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	a.resolveCall++
	if err := a.resolveError[method]; err != nil {
		return nil, err
	}
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *lockCandidateAdapter) CheckHostCompatibility(_ *config.Tool, method *config.MethodCandidate, _ *plan.ResolvedInstallPlan, _ *platform.Facts, _ string) error {
	return a.incompatible[method]
}

func newLockCandidateExecutor(runner run.Runner, adapters ...AdapterV2) *Executor {
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapters...)(ex)
	return ex
}

func lockCandidate(kind, label, pkg string) *config.MethodCandidate {
	return &config.MethodCandidate{Kind: kind, Label: label, Config: map[string]any{"pkg": pkg}}
}

func TestResolveLockCandidateSelectsFirstApplicablePlan(t *testing.T) {
	first := lockCandidate("cargo", "preferred", "first-pkg")
	second := lockCandidate("cargo", "fallback", "second-pkg")
	adapter := &lockCandidateAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}}}
	ex := newLockCandidateExecutor(&run.FakeRunner{}, adapter)
	tool := &config.Tool{Name: "demo", MethodPrefer: []string{"preferred"}, Methods: []*config.MethodCandidate{first, second}}

	got, err := ex.ResolveLockCandidate(context.Background(), tool, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != first || got.Method.Label != "preferred" {
		t.Fatalf("selected method = %#v, want first labeled candidate %#v", got.Method, first)
	}
	if got.Plan.Identity.Package != "first-pkg" {
		t.Fatalf("resolved package = %q, want first-pkg", got.Plan.Identity.Package)
	}
	if adapter.resolveCall != 1 {
		t.Fatalf("ResolvePlan calls = %d, want only the first candidate", adapter.resolveCall)
	}
}

func TestResolveLockCandidateSkipsWhenUnavailableAndFailedCandidates(t *testing.T) {
	whenMismatch := lockCandidate("cargo", "when-mismatch", "wrong-when")
	whenMismatch.When = &config.Condition{DistroID: []string{"definitely-not-this-distro"}}
	unavailable := lockCandidate("unavailable-kind", "unavailable", "unavailable")
	failed := lockCandidate("cargo", "failed-resolution", "failed")
	selected := lockCandidate("cargo", "selected", "selected-pkg")
	adapter := &lockCandidateAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
		resolveError:            map[*config.MethodCandidate]error{failed: errors.New("resolution failed")},
	}
	unavailableAdapter := &lockCandidateAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "unavailable-kind"}},
		unavailable:             true,
	}
	ex := newLockCandidateExecutor(&run.FakeRunner{}, adapter, unavailableAdapter)
	tool := &config.Tool{Name: "demo", MethodPrefer: []string{"when-mismatch", "unavailable", "failed-resolution", "selected"}, Methods: []*config.MethodCandidate{whenMismatch, unavailable, failed, selected}}
	got, err := ex.ResolveLockCandidate(context.Background(), tool, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != selected || got.Plan.Identity.Package != "selected-pkg" {
		t.Fatalf("selection = (%p, %q), want selected candidate %p", got.Method, got.Plan.Identity.Package, selected)
	}
	if adapter.resolveCall != 2 {
		t.Fatalf("ResolvePlan calls = %d, want failed candidate then selected candidate", adapter.resolveCall)
	}
}

func TestResolveLockCandidateRejectsHostIncompatibilityAndPreservesSameKindLabels(t *testing.T) {
	incompatible := lockCandidate("cargo", "host-incompatible", "incompatible")
	selected := lockCandidate("cargo", "portable", "portable")
	adapter := &lockCandidateAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
		incompatible:            map[*config.MethodCandidate]error{incompatible: errors.New("unsupported host")},
	}
	ex := newLockCandidateExecutor(&run.FakeRunner{}, adapter)
	tool := &config.Tool{Name: "demo", MethodPrefer: []string{"host-incompatible", "portable"}, Methods: []*config.MethodCandidate{incompatible, selected}}

	got, err := ex.ResolveLockCandidate(context.Background(), tool, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != selected || got.Method.Label != "portable" {
		t.Fatalf("selected method = %#v, want exact portable candidate %#v", got.Method, selected)
	}
}

func TestResolveLockCandidateReturnsErrorWhenCandidatesUnavailableOrFail(t *testing.T) {
	failed := lockCandidate("cargo", "failed", "failed")
	adapter := &lockCandidateAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
		resolveError:            map[*config.MethodCandidate]error{failed: errors.New("cannot resolve")},
	}
	ex := newLockCandidateExecutor(&run.FakeRunner{}, adapter)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{lockCandidate("missing", "unavailable", "x"), failed}}
	if got, err := ex.ResolveLockCandidate(context.Background(), tool, ""); err == nil || !strings.Contains(err.Error(), "cannot resolve") {
		t.Fatalf("ResolveLockCandidate() = (%+v, %v), want final resolution failure", got, err)
	}
}

func TestResolveLockCandidateProjectsSourceRevisionAndDoesNotMutateRunner(t *testing.T) {
	const revision = "0123456789abcdef0123456789abcdef01234567"
	runner := &explainSourceRunner{FakeRunner: &run.FakeRunner{}, heads: map[string]string{"vendor/alpha": revision}}
	method := lockCandidate("cargo", "source-backed", "demo-package")
	method.Sources = []config.Source{{Kind: "brew-tap", Name: "vendor/alpha", URL: "https://github.com/vendor/alpha.git"}}
	adapter := &lockCandidateAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}}}
	ex := newLockCandidateExecutor(runner, adapter)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}

	got, err := ex.ResolveLockCandidate(context.Background(), tool, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != method || len(got.Plan.Sources) != 1 || got.Plan.Sources[0].Revision != revision {
		t.Fatalf("resolved candidate source projection = method %p sources %+v, want method %p at %s", got.Method, got.Plan.Sources, method, revision)
	}
	if len(runner.Calls) == 0 {
		t.Fatal("expected read-only source/host probes")
	}
	for _, call := range runner.Calls {
		if call.Name == "install" || call.Name == "remove" || call.Name == "sudo" {
			t.Fatalf("mutating runner operation occurred: %+v", call)
		}
	}
	if adapter.installCall != 0 {
		t.Fatalf("InstallResolved calls = %d, want 0", adapter.installCall)
	}
}
