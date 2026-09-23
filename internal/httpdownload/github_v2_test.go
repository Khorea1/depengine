package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// TestGitHubAdapterV2ObserveAgreesWithCheck pins the V2 strangler invariant
// for the github kind: Observe delegates to HTTPAdapter.Observe, so it must
// reach the same verdict as Check (which delegates to HTTPAdapter.Check)
// without touching the release API — even though the candidate carries a
// repo/asset pair and no URL at all.
func TestGitHubAdapterV2ObserveAgreesWithCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewGitHubAdapter()

	extractDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(extractDir, "gh-tool"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		tool        *config.Tool
		mc          *config.MethodCandidate
		runner      *run.FakeRunner
		wantPresent bool
	}{
		{
			name: "extract_to file present",
			tool: &config.Tool{Name: "gh-tool"},
			mc: &config.MethodCandidate{Kind: "github", Config: map[string]any{
				"repo":       "owner/gh-tool",
				"asset":      "gh-tool-linux-amd64",
				"extract_to": extractDir,
			}},
			runner:      &run.FakeRunner{},
			wantPresent: true,
		},
		{
			name: "extract_to file absent",
			tool: &config.Tool{Name: "gh-absent"},
			mc: &config.MethodCandidate{Kind: "github", Config: map[string]any{
				"repo":       "owner/gh-absent",
				"asset":      "gh-absent-linux-amd64",
				"extract_to": extractDir,
				"binary":     "gh-absent",
			}},
			runner:      &run.FakeRunner{},
			wantPresent: false,
		},
		{
			name: "binary on PATH",
			tool: &config.Tool{Name: "gh-bin"},
			mc: &config.MethodCandidate{Kind: "github", Config: map[string]any{
				"repo":   "owner/gh-bin",
				"asset":  "gh-bin-linux-amd64",
				"binary": "gh-bin",
			}},
			runner:      &run.FakeRunner{ExitCode: 0},
			wantPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantCheck := adapter.Check(ctx, tt.runner, tt.tool, tt.mc)
			if wantCheck != tt.wantPresent {
				t.Fatalf("Check() = %v, want %v (test fixture broken)", wantCheck, tt.wantPresent)
			}
			observation, err := adapter.Observe(ctx, tt.runner, tt.tool, tt.mc)
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
			// Delegation must not have resolved anything: no release API
			// calls are possible through a FakeRunner with no scripted
			// commands, and observation carries no URL that could leak a
			// token-bearing asset URL.
			if observation.Detail != "" {
				t.Fatalf("Observe() detail = %q, want empty (no URLs in observations)", observation.Detail)
			}
		})
	}
}

func TestGitHubAdapterV2ObserveRequiresToolAndMethod(t *testing.T) {
	ctx := context.Background()
	adapter := NewGitHubAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "github", Config: map[string]any{
		"repo":  "owner/demo",
		"asset": "demo-linux-amd64",
	}}

	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

// TestGitHubAdapterV2ObservationReconciles proves the delegated observation
// is VerificationResult-compatible.
func TestGitHubAdapterV2ObservationReconciles(t *testing.T) {
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

func TestGitHubAdapterInstallResolvedClassifiesResolvedArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\necho hello\n"))
	}))
	defer server.Close()

	extractTo := t.TempDir()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "github", Config: map[string]any{
		"asset":         "stale.tar.gz",
		"extract_to":    extractTo,
		"sudo_required": false,
	}}
	resolved := plan.New("demo", "github", true)
	resolved.Artifacts = []plan.Artifact{{URL: server.URL + "/download"}}

	if err := NewGitHubAdapter().InstallResolved(context.Background(), run.OSExecRunner{}, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractTo, "demo")); err != nil {
		t.Fatalf("resolved raw artifact was not installed as tool name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractTo, "download")); !os.IsNotExist(err) {
		t.Fatalf("stale asset classification leaked into install path; download exists, err=%v", err)
	}
}
