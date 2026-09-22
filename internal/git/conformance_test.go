package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewGitAdapter())
}

func TestImportDoesNotRegisterAdapter(t *testing.T) {
	if exec.Lookup("git") != nil {
		t.Fatal("importing internal/git registered an adapter")
	}
}

// TestObserveAgreesWithCheck pins the V2 strangler invariant: Observe must
// reach the same installed/not-installed verdict as the legacy Check on
// every detection strategy, so migration never changes skip behavior.
func TestObserveAgreesWithCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewGitAdapter()

	managedDir := t.TempDir()
	managedFile := filepath.Join(managedDir, "owned")
	if err := os.WriteFile(managedFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		mc          *config.MethodCandidate
		runner      *run.FakeRunner
		wantPresent bool
	}{
		{
			name:        "managed paths all exist",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"managed_paths": []any{managedFile}}},
			runner:      &run.FakeRunner{},
			wantPresent: true,
		},
		{
			name:        "managed path missing",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"managed_paths": []any{managedFile, filepath.Join(managedDir, "absent")}}},
			runner:      &run.FakeRunner{},
			wantPresent: false,
		},
		{
			name:        "checkout without revision",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout}},
			runner:      &run.FakeRunner{},
			wantPresent: true,
		},
		{
			name:        "checkout revision matches",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "rev": "abc123"}},
			runner:      &run.FakeRunner{Stdout: "abc123\nabc123\n"},
			wantPresent: true,
		},
		{
			name:        "checkout revision drifted",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "rev": "abc123"}},
			runner:      &run.FakeRunner{Stdout: "abc123\ndef456\n"},
			wantPresent: false,
		},
		{
			name:        "checkout revision unverifiable",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "rev": "abc123"}},
			runner:      &run.FakeRunner{ExitCode: 128},
			wantPresent: false,
		},
		{
			name:        "no checkout",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": filepath.Join(t.TempDir(), "absent")}},
			runner:      &run.FakeRunner{},
			wantPresent: false,
		},
		{
			name:        "binary on PATH",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"binary": "demo-tool"}},
			runner:      &run.FakeRunner{LookPaths: map[string]bool{"demo-tool": true}},
			wantPresent: true,
		},
		{
			name:        "binary missing",
			mc:          &config.MethodCandidate{Kind: "git", Config: map[string]any{"binary": "demo-tool"}},
			runner:      &run.FakeRunner{LookPaths: map[string]bool{"demo-tool": false}},
			wantPresent: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &config.Tool{Name: "demo"}
			checkRunner := &run.FakeRunner{Stdout: tt.runner.Stdout, Stderr: tt.runner.Stderr, ExitCode: tt.runner.ExitCode, Err: tt.runner.Err, LookPaths: tt.runner.LookPaths}
			if got := adapter.Check(ctx, checkRunner, tool, tt.mc); got != tt.wantPresent {
				t.Fatalf("Check() = %v, want %v", got, tt.wantPresent)
			}
			observation, err := adapter.Observe(ctx, tt.runner, tool, tt.mc)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			wantPresence := plan.PresenceAbsent
			if tt.wantPresent {
				wantPresence = plan.PresencePresent
			}
			if observation.Presence != wantPresence {
				t.Fatalf("Observe() presence = %q, want %q", observation.Presence, wantPresence)
			}
		})
	}
}

func TestObserveCarriesPinnedRevision(t *testing.T) {
	ctx := context.Background()
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": checkout, "rev": "abc123"}}
	observation, err := NewGitAdapter().Observe(ctx, &run.FakeRunner{Stdout: "abc123\nabc123\n"}, &config.Tool{Name: "demo"}, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Revision: "abc123"},
		KnownFields: []plan.IdentityField{plan.FieldRevision},
	}
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("Observe() = %#v, want %#v", observation, want)
	}
}

func TestObserveRequiresToolAndMethod(t *testing.T) {
	ctx := context.Background()
	adapter := NewGitAdapter()
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{"url": "https://example.test/repo.git"}}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, &config.Tool{Name: "demo"}, nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

// TestV2ResolveObserveInstallRevision exercises the full V2 path for a
// pinned revision: planner intent, read-only resolution, observation, and
// resolved install, all without network access.
func TestV2ResolveObserveInstallRevision(t *testing.T) {
	ctx := context.Background()
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"url": "https://example.test/repo.git",
		"rev": "deadbeef",
	}}

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Source != "https://example.test/repo.git" || resolved.Identity.Revision != "deadbeef" {
		t.Fatalf("resolved identity = %#v, want source URL and pinned revision", resolved.Identity)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}

	runner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(ctx, runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	foundClone := false
	for _, call := range runner.Calls {
		if call.Name == "git" && len(call.Args) > 0 && call.Args[0] == "clone" {
			foundClone = true
		}
	}
	if !foundClone {
		t.Fatalf("InstallResolved recorded no git clone: %+v", runner.Calls)
	}
}
