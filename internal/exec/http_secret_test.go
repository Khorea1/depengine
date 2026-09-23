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

type httpCredentialResolver struct {
	calls int
	value string
	err   error
}

func (r *httpCredentialResolver) Resolve(_ context.Context, ref plan.SecretReference) (string, error) {
	r.calls++
	if ref != (plan.SecretReference{Provider: "env", Name: "ARTIFACT_TOKEN"}) {
		return "", secret.ErrInvalidReference
	}
	return r.value, r.err
}

type bearerCaptureAdapter struct {
	*testMockAdapter
	credential   string
	credentialOK bool
}

func (a *bearerCaptureAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.credential, a.credentialOK = HTTPArtifactBearer(ctx)
	return a.testMockAdapter.InstallResolved(ctx, rn, tool, mc, resolved)
}

func httpSecretSchema() *config.Schema {
	return &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"http", "cargo"}},
		Tools: map[string]*config.Tool{"demo": {
			Name: "demo", Methods: []*config.MethodCandidate{
				{Kind: "http", SecretRef: &config.SecretReference{Provider: "env", Name: "ARTIFACT_TOKEN"}, Config: map[string]any{"url": "https://example.test/private.tar.gz"}},
				{Kind: "cargo", Config: map[string]any{"pkg": "demo"}},
			},
		}},
	}
}

func TestHTTPSecretResolvedAtReachedInstallAndKeptRuntimeOnly(t *testing.T) {
	const credential = "runtime-secret-sentinel"
	resolver := &httpCredentialResolver{value: credential}
	adapter := &bearerCaptureAdapter{testMockAdapter: &testMockAdapter{kindValue: "http"}}
	ex := New()
	WithRunner(&sequenceRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter, &testMockAdapter{kindValue: "cargo"})(ex)
	report, err := ex.Execute(context.Background(), httpSecretSchema(), "")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || !adapter.credentialOK || adapter.credential != credential {
		t.Fatalf("resolver calls = %d, runtime credential received = %t", resolver.calls, adapter.credentialOK)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), credential) {
		t.Fatal("runtime credential appeared in execution report")
	}
}

func TestHTTPSecretResolutionFailuresFallBack(t *testing.T) {
	cases := []struct {
		name  string
		value string
		err   error
		want  string
	}{
		{name: "missing", err: secret.ErrSecretMissing, want: "secret missing"},
		{name: "empty", err: secret.ErrSecretEmpty, want: "secret empty"},
		{name: "empty value", want: "secret empty"},
		{name: "unsupported provider", err: secret.ErrUnsupportedProvider, want: "secret unsupported provider"},
		{name: "invalid reference", err: secret.ErrInvalidReference, want: "secret invalid reference"},
		{name: "unexpected", err: errors.New("resolver exposed should-never-appear"), want: "secret resolution failed"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &httpCredentialResolver{value: tt.value, err: tt.err}
			ex := New()
			WithRunner(&sequenceRunner{})(ex)
			WithSecretResolver(resolver)(ex)
			WithAdapters(&testMockAdapter{kindValue: "http"}, &testMockAdapter{kindValue: "cargo"})(ex)
			report, err := ex.Execute(context.Background(), httpSecretSchema(), "")
			if err != nil {
				t.Fatal(err)
			}
			if resolver.calls != 1 || len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || report.Tools[0].MethodKind != "cargo" {
				t.Fatalf("resolver calls = %d, report = %+v; want one call and cargo fallback", resolver.calls, report)
			}
			if len(report.Tools[0].Methods) < 1 || !strings.Contains(report.Tools[0].Methods[0].Error, tt.want) {
				t.Fatalf("primary attempt = %+v, want %q", report.Tools[0].Methods, tt.want)
			}
			if strings.Contains(report.Tools[0].Methods[0].Error, "should-never-appear") {
				t.Fatalf("primary attempt leaked resolver error: %+v", report.Tools[0].Methods)
			}
		})
	}
}

func TestHTTPSecretReferenceAndValueAreNotPersistedInState(t *testing.T) {
	const credential = "persist-secret-sentinel"
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	resolver := &httpCredentialResolver{value: credential}
	adapter := &bearerCaptureAdapter{testMockAdapter: &testMockAdapter{kindValue: "http"}}
	ex := New()
	WithRunner(&sequenceRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	schema := httpSecretSchema()
	schema.Defaults.MethodOrder = []string{"http"}
	schema.Tools["demo"].Methods = schema.Tools["demo"].Methods[:1]
	if _, err := ex.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}

	// #nosec G304 -- the path is rooted in the test-owned t.TempDir().
	data, err := os.ReadFile(filepath.Join(dir, "depengine", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), credential) || strings.Contains(string(data), "ARTIFACT_TOKEN") || strings.Contains(string(data), "secret_ref") {
		t.Fatalf("persisted state contains HTTP secret material or reference: %s", data)
	}
	var persisted depstate.State
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := depstate.ValidateNoSecrets(&persisted); err != nil {
		t.Fatalf("persisted state failed secret validation: %v", err)
	}
}

func TestHTTPSecretIsNotResolvedForDryRunOrUnreachedCandidate(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "unreached"
		if dryRun {
			name = "dry run"
		}
		t.Run(name, func(t *testing.T) {
			resolver := &httpCredentialResolver{value: "unused"}
			ex := New()
			WithRunner(&sequenceRunner{})(ex)
			WithSecretResolver(resolver)(ex)
			WithAdapters(&testMockAdapter{kindValue: "http"}, &testMockAdapter{kindValue: "cargo"})(ex)
			if dryRun {
				WithDryRun()(ex)
			}
			schema := httpSecretSchema()
			if !dryRun {
				schema.Defaults.MethodOrder = []string{"cargo", "http"}
			}
			if _, err := ex.Execute(context.Background(), schema, ""); err != nil {
				t.Fatal(err)
			}
			if resolver.calls != 0 {
				t.Fatalf("resolver called %d times, want zero", resolver.calls)
			}
		})
	}
}
