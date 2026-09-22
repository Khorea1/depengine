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

// TestAppImageAdapterV2ObserveAgreesWithCheck pins the V2 strangler
// invariant for the appimage kind: Observe resolves the same
// (install_dir, binary) pair as Check and delegates to HTTPAdapter, so both
// agree on every verdict. The default install_dir lives under the user home,
// which the test isolates.
func TestAppImageAdapterV2ObserveAgreesWithCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewAppImageAdapter()

	home := t.TempDir()
	exectest.SetHome(t, home)

	// Explicit install_dir fixture.
	installDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(installDir, "explicit-app"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Default install_dir fixture (~/.local/bin under the isolated home).
	defaultDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultDir, "default-app"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		tool        *config.Tool
		mc          *config.MethodCandidate
		wantPresent bool
	}{
		{
			name:        "explicit install_dir file present",
			tool:        &config.Tool{Name: "explicit-app"},
			mc:          &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"install_dir": installDir}},
			wantPresent: true,
		},
		{
			name:        "explicit install_dir file absent",
			tool:        &config.Tool{Name: "absent-app"},
			mc:          &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"install_dir": installDir, "binary": "absent-app"}},
			wantPresent: false,
		},
		{
			name:        "explicit binary name overrides tool name",
			tool:        &config.Tool{Name: "explicit-app"},
			mc:          &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"install_dir": installDir, "binary": "explicit-app"}},
			wantPresent: true,
		},
		{
			name:        "default install_dir file present",
			tool:        &config.Tool{Name: "default-app"},
			mc:          &config.MethodCandidate{Kind: "appimage", Config: map[string]any{}},
			wantPresent: true,
		},
		{
			name:        "default install_dir file absent",
			tool:        &config.Tool{Name: "ghost-app"},
			mc:          &config.MethodCandidate{Kind: "appimage", Config: map[string]any{}},
			wantPresent: false,
		},
		{
			name:        "no name anywhere is absent",
			tool:        &config.Tool{Name: ""},
			mc:          &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"install_dir": installDir}},
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

func TestAppImageAdapterV2ObserveRequiresToolAndMethod(t *testing.T) {
	ctx := context.Background()
	adapter := NewAppImageAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"url": "https://example.test/demo.AppImage"}}

	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

// TestAppImageAdapterV2ObservationReconciles proves the delegated
// observation is VerificationResult-compatible.
func TestAppImageAdapterV2ObservationReconciles(t *testing.T) {
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
