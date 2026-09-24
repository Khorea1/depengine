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
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/secret"
	depstate "github.com/Khorea1/depengine/internal/state"
)

const (
	githubTestRefName  = "PRIVATE_GITHUB_RELEASE_TOKEN"
	githubTestSentinel = "github-runtime-secret-sentinel"
)

type githubSecretResult struct {
	value string
	err   error
}

type githubSecretTestResolver struct {
	calls   int
	refs    []plan.SecretReference
	results []githubSecretResult
}

func (r *githubSecretTestResolver) Resolve(_ context.Context, ref plan.SecretReference) (string, error) {
	r.calls++
	r.refs = append(r.refs, ref)
	if len(r.results) == 0 {
		return "", secret.ErrSecretMissing
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result.value, result.err
}

type githubSecretTestAdapter struct {
	testMockAdapter
	resolveTokens []string
	installTokens []string
	installCalls  int
}

func newGithubSecretTestAdapter() *githubSecretTestAdapter {
	return &githubSecretTestAdapter{testMockAdapter: testMockAdapter{kindValue: "github"}}
}

func (a *githubSecretTestAdapter) ResolvePlan(ctx context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	a.resolveTokens = append(a.resolveTokens, ghrelease.NewResolver().GithubToken(ctx, nil))
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *githubSecretTestAdapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	return plan.Observation{Presence: plan.PresenceAbsent}, nil
}

func (a *githubSecretTestAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.installCalls++
	a.installTokens = append(a.installTokens, ghrelease.NewResolver().GithubToken(ctx, nil))
	return a.testMockAdapter.InstallResolved(ctx, rn, tool, method, resolved)
}

func githubSecretTestMethod() *config.MethodCandidate {
	return &config.MethodCandidate{
		Kind:      "github",
		SecretRef: &config.SecretReference{Provider: "env", Name: githubTestRefName},
		Config:    map[string]any{"repo": "owner/private", "asset": "tool.tar.gz"},
	}
}

func githubSecretTestTool(methods ...*config.MethodCandidate) *config.Tool {
	return &config.Tool{Name: "demo", Methods: methods}
}

func TestResolveCandidatePlanPassesResolvedGitHubSecretContext(t *testing.T) {
	resolver := &githubSecretTestResolver{results: []githubSecretResult{{value: githubTestSentinel}}}
	adapter := newGithubSecretTestAdapter()
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)

	_, err := ex.ResolveCandidatePlan(context.Background(), githubSecretTestTool(githubSecretTestMethod()), githubSecretTestMethod())
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || len(adapter.resolveTokens) != 1 || adapter.resolveTokens[0] != githubTestSentinel {
		t.Fatalf("resolver calls = %d, ResolvePlan tokens = %q; want one explicit token", resolver.calls, adapter.resolveTokens)
	}
	if got := resolver.refs; len(got) != 1 || got[0] != (plan.SecretReference{Provider: "env", Name: githubTestRefName}) {
		t.Fatalf("resolved refs = %+v, want the declared GitHub ref", got)
	}
}

func TestInstallResolvedCandidatePassesResolvedGitHubSecretContext(t *testing.T) {
	resolver := &githubSecretTestResolver{results: []githubSecretResult{{value: githubTestSentinel}}}
	adapter := newGithubSecretTestAdapter()
	ex := New()
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)
	method := githubSecretTestMethod()
	resolved := plan.New("demo", "github", true)

	if err := ex.InstallResolvedCandidate(context.Background(), &run.FakeRunner{}, githubSecretTestTool(method), method, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || adapter.installCalls != 1 || len(adapter.installTokens) != 1 || adapter.installTokens[0] != githubTestSentinel {
		t.Fatalf("resolver calls = %d, install calls = %d, install tokens = %q; want one explicit token", resolver.calls, adapter.installCalls, adapter.installTokens)
	}
}

func TestExecutorReResolvesGitHubSecretForInstallContext(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	resolver := &githubSecretTestResolver{results: []githubSecretResult{
		{value: githubTestSentinel},
		{value: githubTestSentinel},
	}}
	adapter := newGithubSecretTestAdapter()
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"github"}},
		Tools:    map[string]*config.Tool{"demo": githubSecretTestTool(githubSecretTestMethod())},
	}

	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 2 {
		t.Fatalf("secret resolver calls = %d, want resolution plus install re-resolution", resolver.calls)
	}
	if len(adapter.resolveTokens) != 1 || adapter.resolveTokens[0] != githubTestSentinel {
		t.Fatalf("ResolvePlan tokens = %q, want explicit secret", adapter.resolveTokens)
	}
	if adapter.installCalls != 1 || len(adapter.installTokens) != 1 || adapter.installTokens[0] != githubTestSentinel {
		t.Fatalf("install calls = %d, install tokens = %q; want one install with explicit secret", adapter.installCalls, adapter.installTokens)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled {
		t.Fatalf("report = %+v, want successful GitHub install", report.Tools)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{githubTestRefName, githubTestSentinel} {
		if strings.Contains(string(encoded), sensitive) {
			t.Fatalf("execution report leaked %q: %s", sensitive, encoded)
		}
	}
	if got := report.Tools[0].PlanIntent; got == nil || len(got.Secrets) != 0 {
		t.Fatalf("reported plan secrets = %+v, want no secret references", got)
	}
	// #nosec G304 -- stateDir is created by t.TempDir for this test.
	stateBytes, err := os.ReadFile(filepath.Join(stateDir, "depengine", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{githubTestRefName, githubTestSentinel, "secret_ref"} {
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

func TestInstallResolvedCandidateResolvesGitHubSecretContext(t *testing.T) {
	resolver := &githubSecretTestResolver{results: []githubSecretResult{{value: githubTestSentinel}}}
	adapter := newGithubSecretTestAdapter()
	ex := New()
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)
	method := githubSecretTestMethod()
	resolved := plan.New("demo", "github", true)

	if err := ex.InstallResolvedCandidate(context.Background(), &run.FakeRunner{}, githubSecretTestTool(method), method, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || adapter.installCalls != 1 || len(adapter.installTokens) != 1 || adapter.installTokens[0] != githubTestSentinel {
		t.Fatalf("resolver calls = %d, install calls = %d, install tokens = %q; want one scoped credential", resolver.calls, adapter.installCalls, adapter.installTokens)
	}
}

func TestGitHubSecretResolutionFailureAndEmptyFailClosedWithFallback(t *testing.T) {
	tests := []struct {
		name    string
		results []githubSecretResult
		want    string
	}{
		{
			name:    "resolution error before ResolvePlan",
			results: []githubSecretResult{{err: errors.New("resolver detail must stay private")}},
			want:    "secret resolution failed",
		},
		{
			name:    "empty secret before ResolvePlan",
			results: []githubSecretResult{{value: ""}},
			want:    "secret empty",
		},
		{
			name: "empty secret when installing",
			results: []githubSecretResult{
				{value: githubTestSentinel},
				{value: ""},
			},
			want: "secret empty",
		},
		{
			name: "resolution error when installing",
			results: []githubSecretResult{
				{value: githubTestSentinel},
				{err: secret.ErrSecretMissing},
			},
			want: "secret missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "ambient-token-must-not-be-used")
			resolver := &githubSecretTestResolver{results: append([]githubSecretResult(nil), tt.results...)}
			githubAdapter := newGithubSecretTestAdapter()
			fallbackAdapter := &testMockAdapter{kindValue: "cargo"}
			ex := New()
			WithRunner(&run.FakeRunner{})(ex)
			WithSecretResolver(resolver)(ex)
			WithAdapters(githubAdapter, fallbackAdapter)(ex)
			schema := &config.Schema{
				Defaults: config.Defaults{MethodOrder: []string{"github", "cargo"}},
				Tools: map[string]*config.Tool{"demo": githubSecretTestTool(
					githubSecretTestMethod(),
					&config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo"}},
				)},
			}

			report, err := ex.Execute(context.Background(), schema, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || report.Tools[0].MethodKind != "cargo" {
				t.Fatalf("report = %+v, want successful cargo fallback", report.Tools)
			}
			if githubAdapter.installCalls != 0 {
				t.Fatalf("GitHub InstallResolved called %d times after secret failure", githubAdapter.installCalls)
			}
			if len(report.Tools[0].Methods) == 0 || !strings.Contains(report.Tools[0].Methods[0].Error, tt.want) {
				t.Fatalf("GitHub attempt = %+v, want sanitized %q diagnostic", report.Tools[0].Methods, tt.want)
			}
			if strings.Contains(report.Tools[0].Methods[0].Error, "resolver detail must stay private") {
				t.Fatalf("GitHub attempt leaked resolver error: %+v", report.Tools[0].Methods)
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			for _, sensitive := range []string{githubTestRefName, githubTestSentinel, "ambient-token-must-not-be-used"} {
				if strings.Contains(string(encoded), sensitive) {
					t.Fatalf("execution report leaked %q: %s", sensitive, encoded)
				}
			}
			if strings.Contains(tt.name, "before ResolvePlan") && len(githubAdapter.resolveTokens) != 0 {
				t.Fatalf("ResolvePlan called after declared secret failed: tokens = %q", githubAdapter.resolveTokens)
			}
		})
	}
}
