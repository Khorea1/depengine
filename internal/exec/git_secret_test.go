package exec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/secret"
	depstate "github.com/Khorea1/depengine/internal/state"
)

const (
	gitTestRefName  = "PRIVATE_GIT_TOKEN"
	gitTestSentinel = "git-runtime-secret-sentinel"
)

type gitSecretTestAdapter struct {
	testMockAdapter
	installCredential string
	installOK         bool
}

func newGitSecretTestAdapter() *gitSecretTestAdapter {
	return &gitSecretTestAdapter{testMockAdapter: testMockAdapter{kindValue: "git"}}
}

func (a *gitSecretTestAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *gitSecretTestAdapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	return plan.Observation{Presence: plan.PresenceAbsent}, nil
}

func (a *gitSecretTestAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.installCredential, a.installOK = GitCredential(ctx)
	return a.testMockAdapter.InstallResolved(ctx, rn, tool, method, resolved)
}

func TestGitCredentialContextResolvesDeclaredRef(t *testing.T) {
	resolver := &githubSecretTestResolver{results: []githubSecretResult{{value: "runtime-git-secret"}}}
	ex := New()
	WithSecretResolver(resolver)(ex)
	method := &config.MethodCandidate{Kind: "git", SecretRef: &config.SecretReference{Provider: "env", Name: "PRIVATE_GIT_TOKEN"}}

	ctx, err := ex.executionCredentialContext(context.Background(), method)
	if err != nil {
		t.Fatalf("executionCredentialContext() error = %v", err)
	}
	if got, ok := GitCredential(ctx); !ok || got != "runtime-git-secret" {
		t.Fatalf("GitCredential() = %q, %v", got, ok)
	}
	if resolver.calls != 1 || len(resolver.refs) != 1 || resolver.refs[0].Name != "PRIVATE_GIT_TOKEN" {
		t.Fatalf("resolved refs = %+v", resolver.refs)
	}
}

func TestGitCredentialContextFailsClosedAndSanitizesErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result githubSecretResult
	}{
		{name: "resolver error", result: githubSecretResult{err: errors.New("private resolver detail")}},
		{name: "missing", result: githubSecretResult{err: secret.ErrSecretMissing}},
		{name: "empty", result: githubSecretResult{value: ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &githubSecretTestResolver{results: []githubSecretResult{tc.result}}
			ex := New()
			WithSecretResolver(resolver)(ex)
			method := &config.MethodCandidate{Kind: "git", SecretRef: &config.SecretReference{Provider: "env", Name: "PRIVATE_GIT_TOKEN"}}
			ctx, err := ex.executionCredentialContext(context.Background(), method)
			if err == nil || ctx != nil {
				t.Fatalf("executionCredentialContext() = (%v, %v), want sanitized failure", ctx, err)
			}
			for _, sensitive := range []string{"PRIVATE_GIT_TOKEN", "private resolver detail", "runtime-git-secret"} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatalf("error leaked %q: %v", sensitive, err)
				}
			}
		})
	}
}

func TestGitPlanIntentOmitsSecretReferenceFromExecutionReports(t *testing.T) {
	method := &config.MethodCandidate{
		Kind:      "git",
		SecretRef: &config.SecretReference{Provider: "env", Name: "PRIVATE_GIT_TOKEN"},
		Config:    map[string]any{"url": "https://example.test/private.git"},
	}
	intent, err := candidatePlanIntentErr(&config.Tool{Name: "private"}, method)
	if err != nil {
		t.Fatalf("candidatePlanIntentErr() error = %v", err)
	}
	if len(intent.Secrets) == 0 {
		t.Fatal("static intent unexpectedly lost typed git secret reference")
	}
	reported, mismatch := candidatePlanIntent(&config.Tool{Name: "private"}, method)
	if mismatch != "" {
		t.Fatalf("candidatePlanIntent() mismatch = %q", mismatch)
	}
	if reported == nil || len(reported.Secrets) != 0 {
		t.Fatalf("reported plan secrets = %+v, want no secret references", reported)
	}
}

func TestExecutorKeepsGitSecretOutOfReportAndState(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	resolver := &githubSecretTestResolver{results: []githubSecretResult{{value: gitTestSentinel}}}
	adapter := newGitSecretTestAdapter()
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)
	method := &config.MethodCandidate{
		Kind:      "git",
		SecretRef: &config.SecretReference{Provider: "env", Name: gitTestRefName},
		Config:    map[string]any{"url": "https://example.test/private.git"},
	}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"git"}},
		Tools: map[string]*config.Tool{"demo": {
			Name:    "demo",
			Methods: []*config.MethodCandidate{method},
		}},
	}

	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || !adapter.installOK || adapter.installCredential != gitTestSentinel {
		t.Fatalf("resolver calls = %d, install credential = %q (%v)", resolver.calls, adapter.installCredential, adapter.installOK)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{gitTestRefName, gitTestSentinel} {
		if strings.Contains(string(encoded), sensitive) {
			t.Fatalf("execution report leaked %q: %s", sensitive, encoded)
		}
	}
	if len(report.Tools) != 1 || report.Tools[0].PlanIntent == nil || len(report.Tools[0].PlanIntent.Secrets) != 0 {
		t.Fatalf("reported plan = %+v, want secret-free intent", report.Tools)
	}

	// #nosec G304 -- stateDir is created by t.TempDir for this test.
	stateBytes, err := os.ReadFile(filepath.Join(stateDir, "depengine", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{gitTestRefName, gitTestSentinel, "secret_ref"} {
		if strings.Contains(string(stateBytes), sensitive) {
			t.Fatalf("persisted state leaked %q: %s", sensitive, stateBytes)
		}
	}
	var persisted depstate.State
	if err := json.Unmarshal(stateBytes, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := depstate.ValidateNoSecrets(&persisted); err != nil {
		t.Fatalf("persisted state failed secret validation: %v", err)
	}
}
