package httpdownload

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// TestAndroidAdapterV2ObserveAgreesWithCheck pins the V2 strangler invariant
// for the android kind: Observe targets the same fixed .apk path as Check
// and delegates to HTTPAdapter, so both agree on every verdict. The .apk
// dir is fixed under the user home, which the test isolates.
func TestAndroidAdapterV2ObserveAgreesWithCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewAndroidAdapter()

	home := t.TempDir()
	exectest.SetHome(t, home)

	apkDir := filepath.Join(home, ".cache", "depengine", "android-apks")
	if err := os.MkdirAll(apkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apkDir, "present-app.apk"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		tool        *config.Tool
		mc          *config.MethodCandidate
		wantPresent bool
	}{
		{
			name:        "apk downloaded",
			tool:        &config.Tool{Name: "present-app"},
			mc:          &config.MethodCandidate{Kind: "android", Config: map[string]any{"url": "https://example.test/present-app.apk"}},
			wantPresent: true,
		},
		{
			name:        "apk not downloaded",
			tool:        &config.Tool{Name: "ghost-app"},
			mc:          &config.MethodCandidate{Kind: "android", Config: map[string]any{"url": "https://example.test/ghost-app.apk"}},
			wantPresent: false,
		},
		{
			name:        "no tool name is absent",
			tool:        &config.Tool{Name: ""},
			mc:          &config.MethodCandidate{Kind: "android", Config: map[string]any{"url": "https://example.test/ghost-app.apk"}},
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &run.FakeRunner{}
			wantCheck := adapter.Check(ctx, runner, tt.tool, tt.mc)
			if wantCheck != tt.wantPresent {
				t.Fatalf("Check() = %v, want %v (test fixture broken)", wantCheck, tt.wantPresent)
			}
			observation, err := adapter.Observe(ctx, runner, tt.tool, tt.mc)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			if got := observation.Presence == plan.PresencePresent; got != tt.wantPresent {
				t.Fatalf("Observe() presence = %q, want present=%v", observation.Presence, tt.wantPresent)
			}
			if !tt.wantPresent {
				if len(observation.KnownFields) != 0 {
					t.Fatalf("absent observation must not carry identity state, got %#v", observation)
				}
				return
			}
			want := plan.Observation{
				Presence:    plan.PresencePresent,
				Identity:    plan.ObservedIdentity{Package: tt.tool.Name},
				KnownFields: []plan.IdentityField{plan.FieldPackage},
			}
			if !reflect.DeepEqual(observation, want) {
				t.Fatalf("Observe() = %#v, want %#v", observation, want)
			}
		})
	}
}

func TestAndroidAdapterV2ObserveRequiresToolAndMethod(t *testing.T) {
	ctx := context.Background()
	adapter := NewAndroidAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "android", Config: map[string]any{"url": "https://example.test/demo.apk"}}

	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

// TestAndroidAdapterV2ObservationReconciles proves the delegated observation
// is VerificationResult-compatible.
func TestAndroidAdapterV2ObservationReconciles(t *testing.T) {
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "demo"},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}
	result := plan.Reconcile(plan.ResolvedIdentity{Package: "demo"}, observation)
	if result.State != plan.StateSatisfied {
		t.Fatalf("Reconcile() state = %q, want satisfied", result.State)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}
