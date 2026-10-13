package exec

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

type verificationAdapter struct {
	executorAdapterV2Double
	seen        *config.MethodCandidate
	observation plan.Observation
}

func (a *verificationAdapter) Observe(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	a.seen = method
	return a.observation, a.observeErr
}

func TestVerifyResolvedCandidateReconcilesResolvedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation plan.Observation
		want        plan.VerificationState
	}{
		{"satisfied", plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "demo"}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, plan.StateSatisfied},
		{"drifted", plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "other"}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, plan.StateDrifted},
		{"absent", plan.Observation{Presence: plan.PresenceAbsent}, plan.StateAbsent},
		{"unknown", plan.Observation{Presence: plan.PresenceUnknown}, plan.StateUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &verificationAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}}}
			adapter.observation = tc.observation
			ex := New()
			WithAdapters(adapter)(ex)
			tool := &config.Tool{Name: "demo"}
			method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo"}}
			resolved := plan.New("demo", "cargo", true)
			resolved.Identity.Package = "demo"
			got, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
			if err != nil || got.State != tc.want {
				t.Fatalf("verification=%+v err=%v, want %s", got, err, tc.want)
			}
		})
	}
}

func TestVerifyResolvedCandidateIgnoresExecuteOnlyIdentityDimensions(t *testing.T) {
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
		observation: plan.Observation{
			Presence:    plan.PresencePresent,
			Identity:    plan.ObservedIdentity{Package: "demo"},
			KnownFields: []plan.IdentityField{plan.FieldPackage},
		},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo", "git": "https://example.test/demo.git"}}
	resolved := plan.New("demo", "cargo", true)
	resolved.Identity.Package = "demo"
	resolved.Identity.Source = "https://example.test/demo.git"

	verification, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
	if err != nil {
		t.Fatalf("VerifyResolvedCandidate: %v", err)
	}
	if verification.State != plan.StateSatisfied {
		t.Fatalf("verification = %+v, want satisfied without unverifiable cargo.git identity", verification)
	}
}

func TestVerifyResolvedCandidateKeepsDeclaredVerificationDimensions(t *testing.T) {
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "winget"}},
		observation: plan.Observation{
			Presence:    plan.PresencePresent,
			Identity:    plan.ObservedIdentity{Package: "Demo.Tool"},
			KnownFields: []plan.IdentityField{plan.FieldPackage},
		},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "winget", Config: map[string]any{"pkg": "Demo.Tool", "source": "winget"}}
	resolved := plan.New("demo", "winget", true)
	resolved.Identity.Package = "Demo.Tool"
	resolved.Identity.Source = "winget"

	verification, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
	if err != nil {
		t.Fatalf("VerifyResolvedCandidate: %v", err)
	}
	if verification.State != plan.StateUnknown || len(verification.Unverifiable) != 1 || verification.Unverifiable[0] != plan.FieldSource {
		t.Fatalf("verification = %+v, want unverifiable declared source identity", verification)
	}
}

func TestResolveAndVerifyCandidateAtVersionProjectsDesiredIdentity(t *testing.T) {
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{
			testMockAdapter: testMockAdapter{kindValue: "cargo"},
		},
		observation: plan.Observation{
			Presence:    plan.PresencePresent,
			Identity:    plan.ObservedIdentity{Package: "demo", Version: "v2.0.0"},
			KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
		},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	WithRunner(&run.FakeRunner{})(ex)
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo"}}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}

	resolved, verification, err := ex.ResolveAndVerifyCandidateAtVersion(context.Background(), tool, method, "v2.0.0", "")
	if err != nil {
		t.Fatalf("ResolveAndVerifyCandidateAtVersion: %v", err)
	}
	if resolved == nil || resolved.Identity.Version != "v2.0.0" {
		t.Fatalf("resolved=%+v, want projected version v2.0.0", resolved)
	}
	if verification.State != plan.StateSatisfied {
		t.Fatalf("verification=%+v, want satisfied", verification)
	}
}

func TestVerifyResolvedCandidateUsesResolvedTargetAndCanonicalizesObserveError(t *testing.T) {
	adapter := &verificationAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "conda"}, observeErr: errors.New("probe failed at https://user:pass@example.test/?token=secret")}}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "demo", "environment": "old"}}
	resolved := plan.New("demo", "conda", true)
	resolved.Identity.Package = "demo"
	resolved.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "new"}
	got, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
	if err != nil {
		t.Fatalf("VerifyResolvedCandidate: %v", err)
	}
	if got.State != plan.StateBroken || !strings.Contains(got.Detail, "probe failed") {
		t.Fatalf("verification=%+v, want redacted broken observation", got)
	}
	if strings.Contains(got.Detail, "pass") || strings.Contains(got.Detail, "secret") {
		t.Fatalf("verification detail leaked probe credentials: %q", got.Detail)
	}
	if got := adapter.seen.Config["environment"]; got != "new" {
		t.Fatalf("observed environment=%v, want new", got)
	}
}

func TestResolveCandidatePlanUsesExplicitClanOverridesOnSameExecutor(t *testing.T) {
	method := &config.MethodCandidate{
		Kind: "native",
		Config: map[string]any{
			"pkg":           "fd",
			"pkg_overrides": map[string]any{"apt": "fd-find", "pacman": "fd"},
		},
	}
	tool := &config.Tool{Name: "fd", Methods: []*config.MethodCandidate{method}}
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "native"})(ex)

	for _, tc := range []struct {
		clan string
		pkg  string
	}{
		{clan: "debian", pkg: "fd-find"},
		{clan: "arch", pkg: "fd"},
		{clan: "debian", pkg: "fd-find"},
	} {
		resolved, err := ex.ResolveCandidatePlan(context.Background(), tool, method, tc.clan)
		if err != nil {
			t.Fatalf("ResolveCandidatePlan(%q): %v", tc.clan, err)
		}
		if resolved.Identity.Package != tc.pkg {
			t.Errorf("ResolveCandidatePlan(%q) package = %q, want %q", tc.clan, resolved.Identity.Package, tc.pkg)
		}
	}
}

func TestResolveAndVerifyUsesEffectiveCandidate(t *testing.T) {
	// br on Windows: the plan resolves br.exe from the Windows override,
	// so verification must observe br.exe, never the inherited br entrypoint.
	newWindowsTool := func() *config.Tool {
		method := &config.MethodCandidate{
			Kind:   "github",
			Config: map[string]any{"repo": "o/r", "asset": "base.tar.gz", "entrypoints": map[string]any{"br": "br"}},
			Distributions: []config.Distribution{
				{
					When:        &config.Condition{OS: []string{"windows"}, Arch: []string{"x86_64"}},
					Asset:       config.AssetSelector{Template: "br-win.zip"},
					HasAsset:    true,
					Entrypoints: map[string]string{"br": "br.exe"},
				},
			},
		}
		return &config.Tool{Name: "br", Methods: []*config.MethodCandidate{method}}
	}
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "github"}},
		observation:             plan.Observation{Presence: plan.PresencePresent},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	WithRunner(&run.FakeRunner{})(ex)
	WithFacts(&platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"})(ex)
	tool := newWindowsTool()
	resolved, _, err := ex.ResolveAndVerifyCandidateAtVersion(context.Background(), tool, tool.Methods[0], "", "")
	if err != nil {
		t.Fatalf("ResolveAndVerifyCandidateAtVersion: %v", err)
	}
	if adapter.seen == nil {
		t.Fatal("verification never observed the candidate")
	}
	entrypoints, _ := adapter.seen.Config["entrypoints"].(map[string]any)
	if entrypoints["br"] != "br.exe" {
		t.Fatalf("verified entrypoints = %v, want br.exe from the Windows override", adapter.seen.Config["entrypoints"])
	}
	if adapter.seen.Config["asset"] != "br-win.zip" {
		t.Fatalf("verified asset = %v, want br-win.zip", adapter.seen.Config["asset"])
	}
	if tool.Methods[0].Config["entrypoints"].(map[string]any)["br"] != "br" {
		t.Fatalf("declared candidate mutated: %v", tool.Methods[0].Config["entrypoints"])
	}
	found := false
	for _, op := range resolved.Operations {
		if op.Kind == "resolve-artifact" && strings.Contains(op.Description, "br-win.zip") {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolved operations = %+v, want the effective Windows payload", resolved.Operations)
	}
}

func TestResolveAndVerifyReceivesLibcOverride(t *testing.T) {
	newTool := func() *config.Tool {
		method := &config.MethodCandidate{
			Kind:   "github",
			Config: map[string]any{"repo": "o/r", "asset": "base.tar.gz"},
			Distributions: []config.Distribution{
				{When: &config.Condition{OS: []string{"linux"}, Libc: []string{"glibc"}}, Asset: config.AssetSelector{Template: "glibc.tar.gz"}, HasAsset: true},
				{When: &config.Condition{OS: []string{"linux"}, Libc: []string{"musl"}}, Asset: config.AssetSelector{Template: "musl.tar.gz"}, HasAsset: true},
			},
		}
		return &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	}
	for _, tc := range []struct {
		libc  string
		asset string
	}{
		{libc: "glibc", asset: "glibc.tar.gz"},
		{libc: "musl", asset: "musl.tar.gz"},
	} {
		t.Run(tc.libc, func(t *testing.T) {
			adapter := &verificationAdapter{
				executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "github"}},
				observation:             plan.Observation{Presence: plan.PresencePresent},
			}
			ex := New()
			WithAdapters(adapter)(ex)
			WithRunner(&run.FakeRunner{})(ex)
			WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix", Libc: tc.libc})(ex)
			tool := newTool()
			if _, _, err := ex.ResolveAndVerifyCandidateAtVersion(context.Background(), tool, tool.Methods[0], "", ""); err != nil {
				t.Fatalf("ResolveAndVerifyCandidateAtVersion: %v", err)
			}
			if adapter.seen == nil || adapter.seen.Config["asset"] != tc.asset {
				t.Fatalf("verified asset = %v, want %s", adapter.seen.Config["asset"], tc.asset)
			}
		})
	}
}

func TestResolveEffectiveCandidatePlanPreservesDeclaredIdentity(t *testing.T) {
	// Without distributions the effective candidate is the declared one:
	// same pointer, same Kind/Label, identical behavior to before.
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
		observation:             plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "demo"}, KnownFields: []plan.IdentityField{plan.FieldPackage}},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	WithRunner(&run.FakeRunner{})(ex)
	method := &config.MethodCandidate{Kind: "cargo", Label: "cargo-main", Config: map[string]any{"pkg": "demo"}}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}
	effective, resolved, err := ex.resolveEffectiveCandidatePlan(context.Background(), tool, method, "")
	if err != nil {
		t.Fatalf("resolveEffectiveCandidatePlan: %v", err)
	}
	if effective != method {
		t.Fatal("effective candidate must be the declared pointer when no distributions apply")
	}
	if resolved == nil {
		t.Fatal("resolved plan is required")
	}
	if _, verification, err := ex.ResolveAndVerifyCandidateAtVersion(context.Background(), tool, method, "", ""); err != nil {
		t.Fatalf("ResolveAndVerifyCandidateAtVersion: %v", err)
	} else if verification.State != plan.StateSatisfied {
		t.Fatalf("verification = %+v, want satisfied", verification)
	}
	if adapter.seen == nil {
		t.Fatal("verification never observed the candidate")
	}
	// Observe receives the resolved-target projection; without distributions
	// it must carry the declared Kind/Label/Config unchanged.
	if adapter.seen.Kind != method.Kind || adapter.seen.Label != method.Label || !reflect.DeepEqual(adapter.seen.Config, method.Config) {
		t.Fatalf("observed method = %+v, want declared candidate content", adapter.seen)
	}
}

func TestResolveEffectiveCandidatePlanIsDeterministic(t *testing.T) {
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "github"}},
		observation:             plan.Observation{Presence: plan.PresencePresent},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	WithRunner(&run.FakeRunner{})(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix", Libc: "glibc"})(ex)
	method := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "o/r", "asset": "base.tar.gz", "entrypoints": map[string]any{"tool": "tool"}},
		Distributions: []config.Distribution{
			{When: &config.Condition{OS: []string{"linux"}, Libc: []string{"glibc"}}, Asset: config.AssetSelector{Template: "glibc.tar.gz"}, HasAsset: true},
			{When: &config.Condition{OS: []string{"linux"}, Libc: []string{"musl"}}, Asset: config.AssetSelector{Template: "musl.tar.gz"}, HasAsset: true},
		},
	}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	before := cloneMethodConfig(method.Config)
	first, _, err := ex.ResolveAndVerifyCandidateAtVersion(context.Background(), tool, method, "", "")
	if err != nil {
		t.Fatalf("first resolution: %v", err)
	}
	second, _, err := ex.ResolveAndVerifyCandidateAtVersion(context.Background(), tool, method, "", "")
	if err != nil {
		t.Fatalf("second resolution: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two resolutions with the same facts must produce the same result")
	}
	if !reflect.DeepEqual(method.Config, before) {
		t.Fatalf("declared config mutated: %v", method.Config)
	}
}
