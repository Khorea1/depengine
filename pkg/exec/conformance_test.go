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
