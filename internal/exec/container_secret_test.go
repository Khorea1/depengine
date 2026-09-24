package exec

import (
	"context"
	"encoding/json"
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

type containerCredentialResolver struct {
	value string
	err   error
	calls int
}

func (r *containerCredentialResolver) Resolve(_ context.Context, ref plan.SecretReference) (string, error) {
	r.calls++
	if ref.Provider != "env" || ref.Name != "REGISTRY_PASSWORD" {
		return "", secret.ErrInvalidReference
	}
	if r.err != nil {
		return "", r.err
	}
	return r.value, nil
}

func TestContainerCredentialContextResolvesOnlyForExplicitReference(t *testing.T) {
	resolver := &containerCredentialResolver{value: "registry-secret"}
	ex := New()
	WithSecretResolver(resolver)(ex)
	method := &config.MethodCandidate{
		Kind:      "container",
		SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_PASSWORD"},
		Config:    map[string]any{"auth_username": "ci-user"},
	}
	ctx, err := ex.executionCredentialContext(context.Background(), method)
	if err != nil {
		t.Fatal(err)
	}
	username, credential, ok := ContainerRegistryCredential(ctx)
	if !ok || username != "ci-user" || credential != "registry-secret" {
		t.Fatalf("credential = (%q, %q, %t)", username, credential, ok)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
}

func TestContainerCredentialContextFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
		value    string
		want     string
	}{
		{name: "missing username", value: "registry-secret", want: "auth_username"},
		{name: "colon username", username: "ci:user", value: "registry-secret", want: "auth_username"},
		{name: "control username", username: "ci\nuser", value: "registry-secret", want: "auth_username"},
		{name: "empty secret", username: "ci-user", want: "container registry secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &containerCredentialResolver{value: tc.value}
			ex := New()
			WithSecretResolver(resolver)(ex)
			method := &config.MethodCandidate{
				Kind:      "container",
				SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_PASSWORD"},
				Config:    map[string]any{"auth_username": tc.username},
			}
			_, err := ex.executionCredentialContext(context.Background(), method)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

type containerCredentialCaptureAdapter struct {
	*testMockAdapter
	username string
	secret   string
	ok       bool
}

func (a *containerCredentialCaptureAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.username, a.secret, a.ok = ContainerRegistryCredential(ctx)
	return a.testMockAdapter.InstallResolved(ctx, rn, tool, mc, resolved)
}

func TestContainerCredentialIsResolvedOnlyAtExecutionAndOmittedFromReport(t *testing.T) {
	const credential = "registry-secret-sentinel"
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	resolver := &containerCredentialResolver{value: credential}
	adapter := &containerCredentialCaptureAdapter{testMockAdapter: &testMockAdapter{kindValue: "container"}}
	ex := New()
	WithRunner(&sequenceRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"container"}},
		Tools: map[string]*config.Tool{"demo": {
			Name: "demo", Methods: []*config.MethodCandidate{{
				Kind: "container", SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_PASSWORD"},
				Config: map[string]any{"manager": "docker", "source": "registry.example.test/team/demo", "auth_username": "ci-user"},
			}},
		}},
	}
	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || !adapter.ok || adapter.username != "ci-user" || adapter.secret != credential {
		t.Fatalf("resolver calls = %d, adapter credential = (%q, %q, %t)", resolver.calls, adapter.username, adapter.secret, adapter.ok)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), credential) || strings.Contains(string(encoded), "REGISTRY_PASSWORD") {
		t.Fatalf("execution report contains registry secret material or reference: %s", encoded)
	}
	// #nosec G304 -- the path is rooted in the test-owned t.TempDir().
	data, err := os.ReadFile(filepath.Join(stateDir, "depengine", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), credential) || strings.Contains(string(data), "REGISTRY_PASSWORD") || strings.Contains(string(data), "secret_ref") {
		t.Fatalf("persisted state contains registry secret material or reference: %s", data)
	}
	var persisted depstate.State
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := depstate.ValidateNoSecrets(&persisted); err != nil {
		t.Fatalf("persisted state failed secret validation: %v", err)
	}
}
