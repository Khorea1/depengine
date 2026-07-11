package git

import (
	"context"
	"strings"
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

func TestGitAdapterInstallWithBuild(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"url":   "https://github.com/user/repo.git",
			"build": "make install",
		},
	}

	err := adapter.Install(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First call: git clone.
	if len(fr.Calls) < 2 {
		t.Fatalf("expected 2 calls (git clone + sh -c), got %d", len(fr.Calls))
	}
	cloneCall := fr.Calls[0]
	if cloneCall.Name != "git" || cloneCall.Args[0] != "clone" {
		t.Fatalf("call 0: expected 'git clone', got %q %v", cloneCall.Name, cloneCall.Args)
	}

	// Second call: sh -c with build command.
	buildCall := fr.Calls[1]
	if buildCall.Name != "sh" {
		t.Fatalf("call 1: expected 'sh', got %q", buildCall.Name)
	}
	if len(buildCall.Args) != 2 || buildCall.Args[0] != "-c" {
		t.Fatalf("call 1: expected args ['-c', '...'], got %v", buildCall.Args)
	}
	expectedSuffix := "make install"
	if !strings.HasSuffix(buildCall.Args[1], expectedSuffix) {
		t.Fatalf("build command %q does not end with %q", buildCall.Args[1], expectedSuffix)
	}
}

func TestGitAdapterInstallWithShellSyntaxBuild(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"url":   "https://github.com/user/repo.git",
			"build": "make && sudo make install",
		},
	}

	err := adapter.Install(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fr.Calls) < 2 {
		t.Fatalf("expected 2 calls, got %d", len(fr.Calls))
	}

	buildCall := fr.Calls[1]
	if buildCall.Name != "sh" || buildCall.Args[0] != "-c" {
		t.Fatalf("expected 'sh -c ...', got %q %v", buildCall.Name, buildCall.Args)
	}

	// Verify shell operators are preserved inside the sh -c string.
	cmd := buildCall.Args[1]
	if !strings.Contains(cmd, "&&") {
		t.Fatalf("build command should contain '&&', got: %q", cmd)
	}
	if !strings.Contains(cmd, "sudo") {
		t.Fatalf("build command should contain 'sudo', got: %q", cmd)
	}
}

func TestGitAdapterInstallWithBuildFails(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "compilation error"}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"url":   "https://github.com/user/repo.git",
			"build": "make",
		},
	}

	err := adapter.Install(context.Background(), fr, tool, mc)
	if err == nil {
		t.Fatal("expected error on build failure, got nil")
	}
	if !strings.Contains(err.Error(), "compilation error") {
		t.Fatalf("error should contain stderr, got: %v", err)
	}
}

func TestGitAdapterInstallResolvesLatestNonGitHub(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"url": "https://gitlab.com/user/repo/-/archive/{latest}/archive.tar.gz",
		},
	}

	err := adapter.Install(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fr.Calls) < 1 {
		t.Fatal("expected at least 1 call to git")
	}
	call := fr.Calls[0]
	if call.Name != "git" || call.Args[0] != "clone" {
		t.Fatalf("expected 'git clone', got %q %v", call.Name, call.Args)
	}

	// Find the URL in clone args and verify {latest} was resolved to "latest".
	for _, arg := range call.Args {
		if strings.Contains(arg, "gitlab.com") {
			if strings.Contains(arg, "{latest}") {
				t.Fatalf("URL still contains unresolved {latest}: %q", arg)
			}
			if !strings.Contains(arg, "/latest/") {
				t.Fatalf("URL should contain '/latest/', got: %q", arg)
			}
			return
		}
	}
	t.Fatal("no URL argument found in clone args")
}

func TestGitAdapterInstallPreservesURLWithoutLatest(t *testing.T) {
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

	if len(fr.Calls) < 1 {
		t.Fatal("expected at least 1 call to git")
	}
	call := fr.Calls[0]
	// Verify the original URL is passed through unchanged.
	urlFound := false
	for _, arg := range call.Args {
		if arg == "https://github.com/user/repo.git" {
			urlFound = true
			break
		}
	}
	if !urlFound {
		t.Fatalf("expected original URL in clone args, got %v", call.Args)
	}
}
