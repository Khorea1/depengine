package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

// TestAllRegisteredAdaptersConformance runs basic conformance checks against
// every adapter registered in the global registry. This catches adapters that
// panic on empty input, return inconsistent Kind values, or fail to return an
// error when given a clearly invalid configuration.
func TestAllRegisteredAdaptersConformance(t *testing.T) {
	for _, kind := range RegisteredKinds() {
		adapter := Lookup(kind)
		if adapter == nil {
			t.Errorf("RegisteredKinds returned %q but Lookup returns nil", kind)
			continue
		}
		t.Run(adapter.Kind(), func(t *testing.T) {
			ctx := context.Background()
			fr := &run.FakeRunner{}
			tool := &config.Tool{Name: "conformance-test"}
			mc := &config.MethodCandidate{Kind: adapter.Kind(), Config: map[string]any{}}

			// Kind
			if k := adapter.Kind(); k == "" {
				t.Error("Kind() must not be empty")
			}

			// Available — must never panic
			_ = adapter.Available(ctx, fr)

			// Check — must never panic with unknown tool and empty config
			_ = adapter.Check(ctx, fr, tool, mc)

			// Install with empty config must not panic (adapters that fall
			// back to tool.Name may succeed here — SubstitutePkg always
			// resolves a package name via tool.Name).
			_ = adapter.Install(ctx, fr, tool, mc)

			// Install with nil runner — should return error, never panic
			if err := adapter.Install(ctx, nil, tool, mc); err == nil {
				t.Error("Install with nil runner should return an error")
			}
		})
	}
}

// perToolNativeAdapter is a mock adapter registered as "native" that
// returns different Install results per tool name. Used to test that
// batch failure does not consume the native method attempt.
type perToolNativeAdapter struct {
	testMockAdapter
}

func (a *perToolNativeAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if tool.Name == "fd" {
		return &installError{"fd native install failed"}
	}
	return nil // bat succeeds
}

func TestBatchDoesNotChangeMethodSelection(t *testing.T) {
	// Two tools: fd (native fails -> falls back to git), bat (native succeeds).
	// Batch should attempt a combined native install, fail, then transparently
	// fall back per-tool. The method selection must be identical to serial.
	nativeAdp := &perToolNativeAdapter{}
	nativeAdp.kindValue = "native"
	nativeAdp.availableFunc = func() bool { return true }
	nativeAdp.checkFunc = func(string) bool { return false }

	gitMock := &testMockAdapter{kindValue: "git"}
	gitMock.installFunc = func(string) error { return nil }
	gitMock.availableFunc = func() bool { return true }
	gitMock.checkFunc = func(string) bool { return false }

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 1})(ex) // batch fails
	WithAdapters(nativeAdp, gitMock)(ex)

	s := &config.Schema{
		Defaults: config.Defaults{
			Manager:     "native",
			MethodOrder: []string{"native", "git"},
		},
		Tools: map[string]*config.Tool{
			"fd": {
				Name: "fd",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "fd"}},
					{Kind: "git", Config: map[string]any{"pkg": "fd"}},
				},
			},
			"bat": {
				Name: "bat",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "bat"}},
					{Kind: "git", Config: map[string]any{"pkg": "bat"}},
				},
			},
		},
	}

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 2 {
		t.Fatalf("expected 2 successes, got %d. Tools: %+v", report.Success, report.Tools)
	}

	results := make(map[string]ToolResult)
	for _, r := range report.Tools {
		results[r.Tool] = r
	}

	// fd: native failed in batch AND in serial fallback -> installed via git
	fd, ok := results["fd"]
	if !ok {
		t.Fatal("fd not found in results")
	}
	if fd.Status != StatusInstalled {
		t.Fatalf("fd expected StatusInstalled, got %v", fd.Status)
	}
	if fd.Method != "git" {
		t.Fatalf("fd expected method 'git' (native failed, fell back), got %q — batch failure consumed native attempt!", fd.Method)
	}

	// bat: native failed in batch but succeeded in serial fallback -> installed via native
	bat, ok := results["bat"]
	if !ok {
		t.Fatal("bat not found in results")
	}
	if bat.Status != StatusInstalled {
		t.Fatalf("bat expected StatusInstalled, got %v", bat.Status)
	}
	if bat.Method != "native" {
		t.Fatalf("bat expected method 'native', got %q — batch failure consumed native attempt!", bat.Method)
	}
}

func TestBatchHappyPath(t *testing.T) {
	// Two tools both with native-only methods. Batch succeeds and both are
	// installed in a single elevated command.
	nativeAdp := &testMockAdapter{kindValue: "native"}
	nativeAdp.installFunc = func(string) error { return nil }
	nativeAdp.availableFunc = func() bool { return true }
	nativeAdp.checkFunc = func(string) bool { return false }

	ex := New()
	WithRunner(&run.FakeRunner{})(ex) // ExitCode 0, batch succeeds
	WithAdapters(nativeAdp)(ex)

	s := &config.Schema{
		Defaults: config.Defaults{
			Manager:     "native",
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*config.Tool{
			"fd": {
				Name: "fd",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "fd"}},
				},
			},
			"bat": {
				Name: "bat",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "bat"}},
				},
			},
		},
	}

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 2 {
		t.Fatalf("expected 2 successes, got %d. Tools: %+v", report.Success, report.Tools)
	}

	results := make(map[string]ToolResult)
	for _, r := range report.Tools {
		results[r.Tool] = r
	}

	for _, name := range []string{"fd", "bat"} {
		tr := results[name]
		if tr.Status != StatusInstalled {
			t.Fatalf("%s expected StatusInstalled, got %v", name, tr.Status)
		}
		if tr.Method != "native" {
			t.Fatalf("%s expected method 'native', got %q", name, tr.Method)
		}
	}
}
