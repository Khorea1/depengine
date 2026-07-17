package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depengine/pkg/exec"
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

	// Verify {latest} was resolved to the tag "latest" and passed as --branch.
	var foundBranch bool
	for i, arg := range call.Args {
		if arg == "--branch" && i+1 < len(call.Args) {
			if call.Args[i+1] != "latest" {
				t.Fatalf("expected --branch 'latest', got %q", call.Args[i+1])
			}
			foundBranch = true
		}
	}
	if !foundBranch {
		t.Fatal("expected --branch argument in clone args")
	}

	// Verify the clone URL does not contain {latest}.
	urlFound := false
	for _, arg := range call.Args {
		if strings.Contains(arg, "gitlab.com") {
			if strings.Contains(arg, "{latest}") {
				t.Fatalf("URL still contains unresolved {latest}: %q", arg)
			}
			urlFound = true
		}
	}
	if !urlFound {
		t.Fatal("no URL argument found in clone args")
	}
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

func TestGitAdapterCanRemove(t *testing.T) {
	adapter := NewGitAdapter()
	if !exec.CanRemove(adapter) {
		t.Fatal("GitAdapter should implement Remover and CanRemove should return true")
	}
}

func TestGitAdapterRemoveWithoutExtractTo(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{
		Config: map[string]any{},
	}

	err := adapter.Remove(context.Background(), fr, tool, mc)
	if err == nil {
		t.Fatal("expected error removing without extract_to")
	}
	if !strings.Contains(err.Error(), "remove not supported without extract_to") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestGitAdapterRemoveSharedDirWithBinary(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}

	// Setup temporary shared-like directory
	tempDir := t.TempDir()
	sharedDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("failed to create temp bin dir: %v", err)
	}

	binaryName := "mytool"
	binaryPath := filepath.Join(sharedDir, binaryName)
	if err := os.WriteFile(binaryPath, []byte("binary data"), 0o755); err != nil {
		t.Fatalf("failed to write dummy binary: %v", err)
	}

	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"extract_to": sharedDir,
			"binary":     binaryName,
		},
	}

	err := adapter.Remove(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The binary should be deleted
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Fatal("binary file should have been deleted")
	}

	// The shared directory should NOT be deleted
	if _, err := os.Stat(sharedDir); os.IsNotExist(err) {
		t.Fatal("shared directory itself should NOT have been deleted")
	}
}

func TestGitAdapterRemoveSharedDirWithoutBinary(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}

	tempDir := t.TempDir()
	sharedDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("failed to create temp bin dir: %v", err)
	}

	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"extract_to": sharedDir,
		},
	}

	err := adapter.Remove(context.Background(), fr, tool, mc)
	if err == nil {
		t.Fatal("expected error removing shared dir without binary")
	}
	if !strings.Contains(err.Error(), "shared directory and binary is not configured") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestGitAdapterRemovePrivateDir(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}

	tempDir := t.TempDir()
	privateDir := filepath.Join(tempDir, "mytool-private-dir")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("failed to create private dir: %v", err)
	}

	binaryPath := filepath.Join(privateDir, "mytool")
	if err := os.WriteFile(binaryPath, []byte("data"), 0o755); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	mc := &schema.MethodCandidate{
		Config: map[string]any{
			"extract_to": privateDir,
		},
	}

	err := adapter.Remove(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The entire directory should be deleted
	if _, err := os.Stat(privateDir); !os.IsNotExist(err) {
		t.Fatal("private directory should have been deleted")
	}
}

func TestIsSharedDir(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// Root and current directory are always shared.
		{"/", true},
		{".", true},
		// Common shared directories.
		{"/bin", true},
		{"/usr/bin", true},
		{"/usr/local/bin", true},
		{"/sbin", true},
		{"/usr/sbin", true},
		{"/usr/local/sbin", true},
		{"/opt", true},
		{"/usr", true},
		{"/usr/local", true},
		{"/lib", true},
		{"/usr/lib", true},
		{"/usr/local/lib", true},
		// Windows shared directories.
		{"C:\\Windows", true},
		{"C:\\Program Files", true},
		{"C:\\Program Files (x86)", true},
		// Directories ending in /bin or /sbin.
		{"/some/other/bin", true},
		{"/custom/sbin", true},
		{"/any/path/to/bin", true},
		// Non-shared directories.
		{"/home/user", false},
		{"/tmp", false},
		{"/var/lib", false},
		{"/usr/local/foo", false},
		// Cleaned versions should also match.
		{"/../", true},        // Cleaned to "/"
		{"/usr/../bin", true}, // Cleaned to "/bin"
	}
	for _, tt := range tests {
		got := isSharedDir(tt.path)
		if got != tt.want {
			t.Errorf("isSharedDir(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestGitAdapterInstallResolvesLatestTagInBranch(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{
		Config: map[string]any{
			// GitHub-archive-style URL where {latest} is embedded in the path.
			"url": "https://example.com/repo/archive/refs/tags/{latest}.tar.gz",
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

	// Verify --branch contains just the tag, not the full URL.
	var branchTag string
	for i, arg := range call.Args {
		if arg == "--branch" && i+1 < len(call.Args) {
			branchTag = call.Args[i+1]
			break
		}
	}
	if branchTag == "" {
		t.Fatal("expected --branch argument in clone args")
	}
	if strings.Contains(branchTag, "example.com") || strings.Contains(branchTag, "/") {
		t.Fatalf("--branch should contain only the resolved tag, not a URL, got %q", branchTag)
	}
	if branchTag != "latest" {
		t.Fatalf("expected --branch 'latest', got %q", branchTag)
	}

	// Verify the clone URL is the base URL without {latest}.
	var cloneURL string
	for _, arg := range call.Args {
		if strings.Contains(arg, "example.com") {
			cloneURL = arg
			break
		}
	}
	if cloneURL == "" {
		t.Fatal("expected clone URL in args")
	}
	if strings.Contains(cloneURL, "{latest}") {
		t.Fatalf("clone URL should not contain {latest}: %q", cloneURL)
	}
}
