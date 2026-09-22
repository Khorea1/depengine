package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// TestHTTPAdapterV2ObserveAgreesWithCheck pins the V2 strangler invariant:
// Observe must reach the same installed/not-installed verdict as the legacy
// Check on every detection strategy, so migration never changes skip
// behavior. Present observations additionally carry the tool name as known
// package identity; absent observations carry no identity.
func TestHTTPAdapterV2ObserveAgreesWithCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewHTTPAdapter()

	extractDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(extractDir, "present-tool"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Directory created by an unrelated operation: the target FILE is what
	// counts, never the bare directory.
	dirOnly := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirOnly, "dir-only-tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	entryRoot := t.TempDir()
	payload := filepath.Join(entryRoot, "opt", "entry-tool")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "bin", "entry-tool"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	links := filepath.Join(entryRoot, "bin")
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	entryMC := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"extract_to": payload,
		"entrypoints": map[string]any{
			"entry-tool": "bin/entry-tool",
		},
		"link_dir": links,
	}}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(payload, "bin", "entry-tool"), filepath.Join(links, "entry-tool")); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name        string
		tool        *config.Tool
		mc          *config.MethodCandidate
		runner      *run.FakeRunner
		wantPresent bool
	}{
		{
			name:        "extract_to file present",
			tool:        &config.Tool{Name: "present-tool"},
			mc:          &config.MethodCandidate{Kind: "http", Config: map[string]any{"extract_to": extractDir}},
			runner:      &run.FakeRunner{},
			wantPresent: true,
		},
		{
			name:        "extract_to file absent",
			tool:        &config.Tool{Name: "missing-tool"},
			mc:          &config.MethodCandidate{Kind: "http", Config: map[string]any{"extract_to": extractDir}},
			runner:      &run.FakeRunner{},
			wantPresent: false,
		},
		{
			name:        "directory only is not installed",
			tool:        &config.Tool{Name: "dir-only-tool"},
			mc:          &config.MethodCandidate{Kind: "http", Config: map[string]any{"extract_to": dirOnly, "binary": "dir-only-tool"}},
			runner:      &run.FakeRunner{},
			wantPresent: false,
		},
		{
			name:        "binary on PATH",
			tool:        &config.Tool{Name: "pathtool"},
			mc:          &config.MethodCandidate{Kind: "http", Config: map[string]any{"binary": "pathtool"}},
			runner:      &run.FakeRunner{ExitCode: 0},
			wantPresent: true,
		},
		{
			name:        "binary not on PATH",
			tool:        &config.Tool{Name: "ghosttool"},
			mc:          &config.MethodCandidate{Kind: "http", Config: map[string]any{"binary": "ghosttool"}},
			runner:      &run.FakeRunner{ExitCode: 1},
			wantPresent: false,
		},
		{
			name:        "no evidence anywhere",
			tool:        &config.Tool{Name: "nothing"},
			mc:          &config.MethodCandidate{Kind: "http", Config: map[string]any{}},
			runner:      &run.FakeRunner{ExitCode: 1},
			wantPresent: false,
		},
	}

	if runtime.GOOS != "windows" {
		tests = append(tests,
			struct {
				name        string
				tool        *config.Tool
				mc          *config.MethodCandidate
				runner      *run.FakeRunner
				wantPresent bool
			}{
				name:        "entrypoints payload and launcher valid",
				tool:        &config.Tool{Name: "entry-tool"},
				mc:          entryMC,
				runner:      &run.FakeRunner{},
				wantPresent: true,
			},
			struct {
				name        string
				tool        *config.Tool
				mc          *config.MethodCandidate
				runner      *run.FakeRunner
				wantPresent bool
			}{
				name: "entrypoints payload missing",
				tool: &config.Tool{Name: "entry-tool"},
				mc: &config.MethodCandidate{Kind: "http", Config: map[string]any{
					"extract_to":  filepath.Join(entryRoot, "opt", "absent"),
					"entrypoints": map[string]any{"entry-tool": "bin/entry-tool"},
					"link_dir":    links,
				}},
				runner:      &run.FakeRunner{},
				wantPresent: false,
			},
		)
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
		})
	}
}

// TestHTTPAdapterV2ObserveStaleLauncherIsAbsent covers the entrypoints
// strategy's negative launcher leg: payload file exists but the launcher is
// missing, so Check is false and Observe must agree.
func TestHTTPAdapterV2ObserveStaleLauncherIsAbsent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher assertion")
	}
	ctx := context.Background()
	adapter := NewHTTPAdapter()

	root := t.TempDir()
	payload := filepath.Join(root, "opt", "stale-tool")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "bin", "stale-tool"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	links := filepath.Join(root, "bin")
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := &config.Tool{Name: "stale-tool"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"extract_to":  payload,
		"entrypoints": map[string]any{"stale-tool": "bin/stale-tool"},
		"link_dir":    links,
	}}

	if adapter.Check(ctx, &run.FakeRunner{}, tool, mc) {
		t.Fatal("Check() = true with missing launcher, want false")
	}
	observation, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", observation.Presence)
	}
}

func TestHTTPAdapterV2ObserveRequiresToolAndMethod(t *testing.T) {
	ctx := context.Background()
	adapter := NewHTTPAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{}}

	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

// TestHTTPAdapterV2ObservationReconciles proves the observation is
// VerificationResult-compatible: package-only desired state reconciles to
// satisfied, while a pinned version stays honestly unverifiable (a bare
// file on disk carries no version information).
func TestHTTPAdapterV2ObservationReconciles(t *testing.T) {
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "demo"},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}

	satisfied := plan.Reconcile(plan.ResolvedIdentity{Package: "demo"}, observation)
	if satisfied.State != plan.StateSatisfied {
		t.Fatalf("Reconcile(package-only) state = %q, want satisfied", satisfied.State)
	}
	if err := satisfied.Validate(); err != nil {
		t.Fatalf("satisfied result invalid: %v", err)
	}

	withVersion := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "v1.2.3"}, observation)
	if withVersion.State != plan.StateUnknown {
		t.Fatalf("Reconcile(pinned version) state = %q, want unknown", withVersion.State)
	}
	if len(withVersion.Unverifiable) != 1 || withVersion.Unverifiable[0] != plan.FieldVersion {
		t.Fatalf("Reconcile(pinned version) unverifiable = %v, want [version]", withVersion.Unverifiable)
	}
	if err := withVersion.Validate(); err != nil {
		t.Fatalf("unknown result invalid: %v", err)
	}

	absent := plan.Reconcile(
		plan.ResolvedIdentity{Package: "demo"},
		plan.Observation{Presence: plan.PresenceAbsent},
	)
	if absent.State != plan.StateAbsent {
		t.Fatalf("Reconcile(absent) state = %q, want absent", absent.State)
	}
	if err := absent.Validate(); err != nil {
		t.Fatalf("absent result invalid: %v", err)
	}
}

// TestHTTPAdapterV2ResolveInstallRoundTrip proves the V2 trio works end to
// end: ResolvePlan projects the concrete URL without downloading, and
// InstallResolved consumes exactly that URL.
func TestHTTPAdapterV2ResolveInstallRoundTrip(t *testing.T) {
	ctx := context.Background()
	adapter := NewHTTPAdapter()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\necho hello\n"))
	}))
	defer server.Close()

	extractTo := t.TempDir()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url":           server.URL + "/demo",
		"extract_to":    extractTo,
		"binary":        "demo",
		"sudo_required": false,
	}}
	intent := plan.New("demo", "http", true)

	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL != server.URL+"/demo" {
		t.Fatalf("resolved artifacts = %+v, want concrete server URL", resolved.Artifacts)
	}

	// The resolved plan is authoritative; the candidate URL is cleared so a
	// successful install can only have come from the plan.
	mc.Config["url"] = ""
	if err := adapter.InstallResolved(ctx, run.OSExecRunner{}, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(extractTo, "demo"))
	if err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("installed binary is empty")
	}

	observation, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe() presence = %q, want present after install", observation.Presence)
	}
}
