package exec

// Unit coverage for the terminal classification hub and the integrity
// boundary (dep-1n0.10, plan section 4). These exercise the new T4 API, so
// they fail to build before the fix and pass after; the end-to-end fallback
// behavior they guard is proven red-before/green-after by
// terminal_behavior_test.go, which compiles on the base tree.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

func TestIsTerminalMethodErrorMatrix(t *testing.T) {
	terminal := []error{
		&config.AmbiguousDistributionsError{Kind: "github", Conditions: []string{"a", "b"}},
		fmt.Errorf("outer: %w", &config.AmbiguousDistributionsError{Kind: "github"}),
		&terminalError{err: errors.New("specialize: invalid effective configuration")},
		&ghrelease.AssetResolutionError{},
		fmt.Errorf("github: resolve plan: %w", &ghrelease.AssetResolutionError{}),
		fmt.Errorf("artifact: %w", ghrelease.ErrNoAssetMatch),
		fmt.Errorf("artifact: %w", ghrelease.ErrAmbiguousAssetMatch),
		fmt.Errorf("resolve asset: %w", ghrelease.ErrInvalidAssetSelector),
		&IntegrityError{Method: "github", Reason: "checksum mismatch"},
		fmt.Errorf("http: checksum: %w", &IntegrityError{Method: "github"}),
		fmt.Errorf("verify: %w", ErrIntegrityMismatch),
	}
	for i, err := range terminal {
		if !IsTerminalMethodError(err) {
			t.Errorf("terminal[%d] = %v, want terminal", i, err)
		}
	}

	recoverable := []error{
		nil,
		errors.New("connection reset by peer"),
		errors.New("resolve latest: GitHub API returned 500"),
		errors.New("github: resolve plan: connection reset by peer"),
		errors.New("no adapter for kind"),
	}
	for i, err := range recoverable {
		if IsTerminalMethodError(err) {
			t.Errorf("recoverable[%d] = %v, want fallback preserved", i, err)
		}
	}
}

func TestIntegrityErrorCarriesSentinel(t *testing.T) {
	err := &IntegrityError{Method: "github", Reason: "checksum mismatch for tool.tar.gz"}
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("errors.Is(%v, ErrIntegrityMismatch) = false", err)
	}
	var integrity *IntegrityError
	if !errors.As(fmt.Errorf("http: checksum: %w", err), &integrity) {
		t.Fatalf("wrapped %v lost its type", err)
	}
	if !strings.Contains(err.Error(), "github") || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Error() = %q, want method and reason", err.Error())
	}
}

func TestInstallIntegrityFailureAbortsWithoutFallback(t *testing.T) {
	github := &terminalResolveAdapter{
		testMockAdapter: testMockAdapter{
			kindValue: "github",
			// Production shape: the transport wraps the integrity failure.
			installFunc: func(string) error {
				return fmt.Errorf("http: checksum: %w", &IntegrityError{Method: "github", Reason: "checksum mismatch for tool.tar.gz"})
			},
		},
	}
	fallback := &terminalResolveAdapter{testMockAdapter: testMockAdapter{kindValue: "fallback"}}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(github, fallback)(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)

	// Real (non-dry-run) mode: dry-run never reaches InstallResolved, so an
	// integrity failure is only observable on the commit path.
	schema := terminalTwoMethodSchema("github", "fallback")
	report, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Status != StatusFailed {
		t.Fatalf("result = %+v, want terminal failure on bad checksum", got)
	}
	if !strings.Contains(got.Error, "checksum mismatch") {
		t.Fatalf("error = %q, want the integrity detail, not a generic failure", got.Error)
	}
	if len(got.Methods) != 1 {
		t.Fatalf("attempts = %+v, want no source switch after a bad checksum", got.Methods)
	}
	if fallback.installs != 0 {
		t.Fatalf("fallback installs = %d, want 0: integrity failure must not switch source", fallback.installs)
	}
}

func TestGPGRefetchIntegrityFailureAbortsWithoutFallback(t *testing.T) {
	http := &terminalResolveAdapter{
		testMockAdapter: testMockAdapter{
			kindValue: "http",
			installFunc: func(string) error {
				return fmt.Errorf("http: cached checksum refetch: %w", &IntegrityError{
					Method: "http", Reason: "gpg signature verification failed", Err: errors.New("signature verification failed after cached checksum refetch"),
				})
			},
		},
	}
	fallback := &terminalResolveAdapter{testMockAdapter: testMockAdapter{kindValue: "fallback"}}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(http, fallback)(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)

	schema := terminalTwoMethodSchema("http", "fallback")
	schema.Tools["tool"].Methods[0].Config = map[string]any{"url": "https://example.test/tool.tar.gz"}
	report, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Status != StatusFailed {
		t.Fatalf("result = %+v, want terminal GPG failure", got)
	}
	if len(got.Methods) != 1 {
		t.Fatalf("attempts = %+v, want no second method after GPG failure", got.Methods)
	}
	if fallback.installs != 0 {
		t.Fatalf("fallback installs = %d, want 0 while cached checksum refetch still lacks a valid signature", fallback.installs)
	}
}

func TestInstallAvailabilityFailureKeepsFallback(t *testing.T) {
	github := &terminalResolveAdapter{
		testMockAdapter: testMockAdapter{
			kindValue:   "github",
			installFunc: func(string) error { return errors.New("connection reset by peer") },
		},
	}
	fallback := &terminalResolveAdapter{testMockAdapter: testMockAdapter{kindValue: "fallback"}}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(github, fallback)(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)

	schema := terminalTwoMethodSchema("github", "fallback")
	report, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Method != "fallback" {
		t.Fatalf("result = %+v, want availability fallback to keep working", got)
	}
}
