package git

import (
	"context"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func TestGitAdapterAvailable(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()

	if !adapter.Available(context.Background(), fr) {
		t.Fatal("Available should be true when which git returns 0")
	}

	if len(fr.Calls) != 1 || fr.Calls[0].Name != "which" {
		t.Fatalf("expected 'which git', got %v", fr.Calls)
	}
}

func TestGitAdapterAvailableMissing(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewGitAdapter()

	if adapter.Available(context.Background(), fr) {
		t.Fatal("Available should be false when which returns non-zero")
	}
}

func TestGitAdapterKind(t *testing.T) {
	if NewGitAdapter().Kind() != "git" {
		t.Fatalf("Kind() = %q, want 'git'", NewGitAdapter().Kind())
	}
}

func TestGitAdapterCheckViaExtractTo(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	mc := &schema.MethodCandidate{Config: map[string]any{"extract_to": "/tmp/test"}}

	if !adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be true when extract_to/.git exists")
	}
}

func TestGitAdapterCheckViaBinary(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	// extract_to not present, binary present.
	mc := &schema.MethodCandidate{Config: map[string]any{"binary": "somebin"}}

	if !adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be true when binary is on PATH")
	}
}

func TestGitAdapterCheckNotFound(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewGitAdapter()
	mc := &schema.MethodCandidate{Config: map[string]any{}}

	if adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be false when nothing found")
	}
}

func TestGitAdapterInstallGeneratesCloneCommand(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"url": "https://github.com/user/repo.git",
		},
	}

	err := adapter.Install(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that git clone was called with depth and url.
	if len(fr.Calls) < 1 {
		t.Fatal("expected at least 1 call to git")
	}
	call := fr.Calls[0]
	if call.Name != "git" {
		t.Fatalf("expected 'git', got %q", call.Name)
	}
	if len(call.Args) < 4 || call.Args[0] != "clone" || call.Args[1] != "--depth" {
		t.Fatalf("expected 'git clone --depth ...', got %v", call.Args)
	}
}

func TestGitAdapterInstallWithoutURL(t *testing.T) {
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{Config: map[string]any{}}

	err := adapter.Install(context.Background(), nil, tool, mc)
	if err == nil {
		t.Fatal("expected error when no url configured, got nil")
	}
}
