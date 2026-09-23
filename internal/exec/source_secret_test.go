package exec

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/secret"
)

type countingSecretResolver struct {
	calls int
	err   error
}

func (r *countingSecretResolver) Resolve(context.Context, plan.SecretReference) (string, error) {
	r.calls++
	return "", r.err
}

func authenticatedCandidate() *config.MethodCandidate {
	return &config.MethodCandidate{
		Kind: "cargo", Config: map[string]any{"pkg": "demo"},
		Sources: []config.Source{{
			Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git",
			SecretRef: &config.SecretReference{Provider: "env", Name: "CORP_TOKEN"},
		}},
	}
}

func TestSecretResolutionFailureFallsBackBeforeSourceMutation(t *testing.T) {
	resolver := &countingSecretResolver{err: secret.ErrSecretMissing}
	runner := &sequenceRunner{results: []run.Result{{}}}
	ex := New()
	WithRunner(runner)(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(&testMockAdapter{kindValue: "cargo"}, &testMockAdapter{kindValue: "http"})(ex)
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"cargo", "http"}},
		Tools: map[string]*config.Tool{"demo": {
			Name: "demo", Methods: []*config.MethodCandidate{
				authenticatedCandidate(),
				{Kind: "http", Config: map[string]any{"url": "https://example.test/demo.tar.gz"}},
			},
		}},
	}
	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || report.Tools[0].MethodKind != "http" {
		t.Fatalf("report = %+v, want fallback http install", report.Tools)
	}
	if resolver.calls != 1 || len(runner.calls) != 1 || runner.calls[0].Name != "brew" {
		t.Fatalf("resolver calls = %d, source commands = %+v", resolver.calls, runner.calls)
	}
	if !strings.Contains(report.Tools[0].Methods[0].Error, "secret missing") {
		t.Fatalf("primary attempt error = %q, want missing secret", report.Tools[0].Methods[0].Error)
	}
}

func TestSecretResolverIsNotCalledForUnreachedCandidate(t *testing.T) {
	resolver := &countingSecretResolver{err: secret.ErrSecretMissing}
	ex := New()
	WithRunner(&sequenceRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(&testMockAdapter{kindValue: "http"}, &testMockAdapter{kindValue: "cargo"})(ex)
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"http", "cargo"}},
		Tools: map[string]*config.Tool{"demo": {
			Name: "demo", Methods: []*config.MethodCandidate{
				{Kind: "http", Config: map[string]any{"url": "https://example.test/demo.tar.gz"}},
				authenticatedCandidate(),
			},
		}},
	}
	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil || len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled {
		t.Fatalf("Execute() = (%+v, %v)", report, err)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver called %d times for an unreached candidate", resolver.calls)
	}
}
