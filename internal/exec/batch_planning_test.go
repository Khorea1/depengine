package exec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

func TestBatchDoesNotBypassCandidateCapabilityBoundary(t *testing.T) {
	nativeChecks := 0
	nativeAdapter := &testMockAdapter{
		kindValue: "native",
		checkFunc: func(string) bool {
			nativeChecks++
			return false
		},
	}
	gitAdapter := &testMockAdapter{
		kindValue: "git",
		checkFunc: func(string) bool { return false },
	}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"native", "git"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name: "demo",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}},
					{Kind: "git", Config: map[string]any{"url": "https://example.test/demo.git"}},
				},
			},
		},
	}
	ex := New()
	WithAdapters(nativeAdapter, gitAdapter)(ex)
	WithRunner(&run.FakeRunner{})(ex)
	WithDryRun()(ex)

	report, err := ex.Execute(context.Background(), schema, "arch")
	if err != nil {
		t.Fatal(err)
	}
	if nativeChecks != 0 {
		t.Fatalf("native adapter was probed %d times; static capability rejection must happen first", nativeChecks)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusWouldInstall || report.Tools[0].MethodKind != "git" {
		t.Fatalf("report = %+v, want git fallback planned", report.Tools)
	}
	if len(report.Tools[0].Methods) != 2 || report.Tools[0].Methods[0].Status != "skip_capability" || report.Tools[0].Methods[1].Status != "success" {
		t.Fatalf("method attempts = %+v, want native capability rejection then git success", report.Tools[0].Methods)
	}
	if !strings.Contains(report.Tools[0].Methods[0].Error, "exact-version") {
		t.Fatalf("capability rejection = %q, want exact_version", report.Tools[0].Methods[0].Error)
	}
}

func TestVerifiedBatchInstallPersistsCommitWhenPostInstallFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	checks := 0
	adapter := &testMockAdapter{
		kindValue: "native",
		checkFunc: func(string) bool {
			checks++
			return checks > 1
		},
	}
	runner := &sequenceRunner{results: []run.Result{
		{},            // batch install succeeds
		{ExitCode: 1}, // post-install hook fails
	}}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"native"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name:        "demo",
				PostInstall: []config.Hook{{Run: []string{"post-hook"}}},
				Methods: []*config.MethodCandidate{{
					Kind: "native", Config: map[string]any{"pkg": "demo"},
				}},
			},
		},
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithAllowArbitraryCode()(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	report, err := ex.Execute(context.Background(), schema, "arch")
	if err != nil {
		t.Fatal(err)
	}
	if checks != 2 {
		t.Fatalf("native checks = %d, want pre-batch and verification probes", checks)
	}
	if len(report.Tools) != 1 {
		t.Fatalf("report = %+v, want one result", report.Tools)
	}
	result := report.Tools[0]
	if result.Status != StatusFailed || !result.InstallCommitted {
		t.Fatalf("result = %+v, want failed post-install with committed install", result)
	}
	if result.Method != "native" || result.MethodKind != "native" || result.PlanIntent == nil {
		t.Fatalf("batch result lost method/plan metadata: %+v", result)
	}
	if !strings.Contains(result.Error, "post-install") {
		t.Fatalf("result error = %q, want post-install failure", result.Error)
	}

	persisted, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	toolState, ok := persisted.Tools["demo"]
	if !ok {
		t.Fatalf("committed batch install missing from state: %#v", persisted.Tools)
	}
	if toolState.MethodKind != "native" || toolState.Method != "native" {
		t.Fatalf("persisted batch method = %#v, want native metadata", toolState)
	}
}
