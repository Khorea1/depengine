package exec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	calls    int
	value    string
	err      error
	refs     []plan.SecretReference
	failName string
	values   map[string]string
}

func (r *httpCredentialResolver) Resolve(_ context.Context, ref plan.SecretReference) (string, error) {
	r.calls++
	r.refs = append(r.refs, ref)
	if ref.Provider != "env" || (ref.Name != "ARTIFACT_TOKEN" && ref.Name != "CHECKSUM_TOKEN" && ref.Name != "SIGNATURE_TOKEN") {
		return "", secret.ErrInvalidReference
	}
	if r.err != nil && (r.failName == "" || ref.Name == r.failName) {
		return "", r.err
	}
	if value, ok := r.values[ref.Name]; ok {
		return value, nil
	}
	return r.value, nil
}

type bearerCaptureAdapter struct {
	*testMockAdapter
	credential   string
	credentialOK bool
	credentials  map[HTTPBearerPurpose]string
}

func (a *bearerCaptureAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.credential, a.credentialOK = HTTPArtifactBearer(ctx)
	a.credentials = make(map[HTTPBearerPurpose]string, 3)
	for _, purpose := range []HTTPBearerPurpose{HTTPBearerArtifact, HTTPBearerChecksum, HTTPBearerSignature} {
		a.credentials[purpose], _ = HTTPBearer(ctx, purpose)
	}
	return a.testMockAdapter.InstallResolved(ctx, rn, tool, mc, resolved)
}

func setHTTPSecretRef(t *testing.T, method *config.MethodCandidate, field string, ref *config.SecretReference) bool {
	t.Helper()
	value := reflect.ValueOf(method).Elem().FieldByName(field)
	if !value.IsValid() {
		return false
	}
	if value.Type() != reflect.TypeOf(ref) || !value.CanSet() {
		t.Fatalf("%s has unexpected type or is not settable", field)
	}
	value.Set(reflect.ValueOf(ref))
	return true
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
			want := "http artifact " + tt.want
			if len(report.Tools[0].Methods) < 1 || !strings.Contains(report.Tools[0].Methods[0].Error, want) {
				t.Fatalf("primary attempt = %+v, want %q", report.Tools[0].Methods, want)
			}
			if strings.Contains(report.Tools[0].Methods[0].Error, "should-never-appear") {
				t.Fatalf("primary attempt leaked resolver error: %+v", report.Tools[0].Methods)
			}
		})
	}
}

func TestHTTPSidecarSecretsResolveSeparatelyAndReachInstallByPurpose(t *testing.T) {
	resolver := &httpCredentialResolver{values: map[string]string{
		"ARTIFACT_TOKEN": "artifact-token", "CHECKSUM_TOKEN": "checksum-token", "SIGNATURE_TOKEN": "signature-token",
	}}
	adapter := &bearerCaptureAdapter{testMockAdapter: &testMockAdapter{kindValue: "http"}}
	ex := New()
	WithRunner(&sequenceRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(adapter)(ex)
	schema := httpSecretSchema()
	method := schema.Tools["demo"].Methods[0]
	if !setHTTPSecretRef(t, method, "ChecksumSecretRef", &config.SecretReference{Provider: "env", Name: "CHECKSUM_TOKEN"}) ||
		!setHTTPSecretRef(t, method, "SignatureSecretRef", &config.SecretReference{Provider: "env", Name: "SIGNATURE_TOKEN"}) {
		t.Skip("sidecar reference fields are not present in config.MethodCandidate yet")
	}
	schema.Defaults.MethodOrder = []string{"http"}
	schema.Tools["demo"].Methods = schema.Tools["demo"].Methods[:1]
	if _, err := ex.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}
	wantRefs := []plan.SecretReference{
		{Provider: "env", Name: "ARTIFACT_TOKEN"},
		{Provider: "env", Name: "CHECKSUM_TOKEN"},
		{Provider: "env", Name: "SIGNATURE_TOKEN"},
	}
	if !reflect.DeepEqual(resolver.refs, wantRefs) {
		t.Fatalf("resolved refs = %+v, want %+v", resolver.refs, wantRefs)
	}
	for purpose, want := range map[HTTPBearerPurpose]string{
		HTTPBearerArtifact: "artifact-token", HTTPBearerChecksum: "checksum-token", HTTPBearerSignature: "signature-token",
	} {
		if adapter.credentials[purpose] != want {
			t.Errorf("credential for %s = %q, want %q", purpose, adapter.credentials[purpose], want)
		}
	}
}

func TestHTTPSidecarSecretFailureFallsBackWithSanitizedDiagnostic(t *testing.T) {
	resolver := &httpCredentialResolver{
		values: map[string]string{"ARTIFACT_TOKEN": "artifact-token"},
		err:    errors.New("resolver leaked SIDE_SECRET reference-secret"), failName: "CHECKSUM_TOKEN",
	}
	ex := New()
	WithRunner(&sequenceRunner{})(ex)
	WithSecretResolver(resolver)(ex)
	WithAdapters(&testMockAdapter{kindValue: "http"}, &testMockAdapter{kindValue: "cargo"})(ex)
	schema := httpSecretSchema()
	if !setHTTPSecretRef(t, schema.Tools["demo"].Methods[0], "ChecksumSecretRef", &config.SecretReference{Provider: "env", Name: "CHECKSUM_TOKEN"}) {
		t.Skip("sidecar reference fields are not present in config.MethodCandidate yet")
	}
	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || report.Tools[0].MethodKind != "cargo" {
		t.Fatalf("report = %+v, want cargo fallback", report)
	}
	if resolver.calls != 2 {
		t.Fatalf("resolver calls = %d, want artifact then checksum", resolver.calls)
	}
	detail := report.Tools[0].Methods[0].Error
	for _, forbidden := range []string{"SIDE_SECRET", "reference-secret", "CHECKSUM_TOKEN"} {
		if strings.Contains(detail, forbidden) {
			t.Fatalf("sanitized diagnostic leaked %q: %q", forbidden, detail)
		}
	}
	if !strings.Contains(detail, "http checksum secret resolution failed") {
		t.Fatalf("diagnostic = %q, want checksum purpose and failure class", detail)
	}
}

func TestHTTPBearerPurposesAndLegacyArtifactWrappers(t *testing.T) {
	ctx := WithHTTPArtifactBearer(context.Background(), "artifact-token")
	ctx = WithHTTPBearer(ctx, HTTPBearerChecksum, "checksum-token")
	ctx = WithHTTPBearer(ctx, HTTPBearerSignature, "signature-token")
	for purpose, want := range map[HTTPBearerPurpose]string{
		HTTPBearerArtifact: "artifact-token", HTTPBearerChecksum: "checksum-token", HTTPBearerSignature: "signature-token",
	} {
		got, ok := HTTPBearer(ctx, purpose)
		if !ok || got != want {
			t.Errorf("HTTPBearer(%s) = (%q, %t), want %q", purpose, got, ok, want)
		}
	}
	if got, ok := HTTPArtifactBearer(ctx); !ok || got != "artifact-token" {
		t.Fatalf("legacy HTTPArtifactBearer() = (%q, %t)", got, ok)
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
			setHTTPSecretRef(t, schema.Tools["demo"].Methods[0], "ChecksumSecretRef", &config.SecretReference{Provider: "env", Name: "CHECKSUM_TOKEN"})
			setHTTPSecretRef(t, schema.Tools["demo"].Methods[0], "SignatureSecretRef", &config.SecretReference{Provider: "env", Name: "SIGNATURE_TOKEN"})
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
