package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/plan"
	"github.com/Khorea1/depengine/pkg/run"
)

func TestGitAdapterAvailable(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()

	if !adapter.Available(context.Background(), fr) {
		t.Fatal("Available should be true when git is on PATH")
	}

	if len(fr.Calls) != 1 || fr.Calls[0].Name != "which" {
		t.Fatalf("expected LookPath for git, got %v", fr.Calls)
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
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	fr := &run.FakeRunner{}
	adapter := NewGitAdapter()
	mc := &config.MethodCandidate{Config: map[string]any{"extract_to": dir}}

	if !adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be true when extract_to/.git exists")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("filesystem check executed commands: %+v", fr.Calls)
	}
}

func TestGitAdapterCheckRejectsFileAsGitDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if NewGitAdapter().Check(context.Background(), &run.FakeRunner{}, nil, &config.MethodCandidate{Config: map[string]any{"extract_to": dir}}) {
		t.Fatal("Check should be false when .git is not a directory")
	}
}

func TestGitAdapterCheckViaBinary(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	// extract_to not present, binary present.
	mc := &config.MethodCandidate{Config: map[string]any{"binary": "somebin"}}

	if !adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be true when binary is on PATH")
	}
	if len(fr.Calls) != 1 || fr.Calls[0].Name != "which" {
		t.Fatalf("expected LookPath for binary, got %+v", fr.Calls)
	}
}

func TestGitAdapterCheckNotFound(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewGitAdapter()
	mc := &config.MethodCandidate{Config: map[string]any{}}

	if adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be false when nothing found")
	}
}

func TestGitAdapterInstallGeneratesCloneCommand(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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

func TestGitAdapterRejectsEmbeddedCredentialsBeforeClone(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://secret-token@example.com/repo.git",
	}}

	err := adapter.Install(context.Background(), fr, tool, mc)
	if err == nil {
		t.Fatal("expected embedded credentials to be rejected")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked credential: %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("credential-bearing URL reached subprocess argv: %+v", fr.Calls)
	}
}

func TestGitAdapterInstallWithoutURL(t *testing.T) {
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{Config: map[string]any{}}

	err := adapter.Install(context.Background(), nil, tool, mc)
	if err == nil {
		t.Fatal("expected error when no url configured, got nil")
	}
}

func TestGitAdapterInstallWithBuild(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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

	// Second call: sh -c with build command in the clone directory.
	buildCall := fr.Calls[1]
	if buildCall.Name != "sh" {
		t.Fatalf("call 1: expected 'sh', got %q", buildCall.Name)
	}
	if len(buildCall.Args) != 2 || buildCall.Args[0] != "-c" {
		t.Fatalf("call 1: expected args ['-c', '...'], got %v", buildCall.Args)
	}
	if buildCall.Args[1] != "make install" {
		t.Fatalf("build command = %q, want %q", buildCall.Args[1], "make install")
	}
	if buildCall.Dir == "" {
		t.Fatal("build command should run in the clone directory")
	}
}

func TestGitAdapterInstallWithPortableBuild(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":   "https://github.com/user/repo.git",
		"build": map[string]any{"run": []any{"go", "build", "./cmd/tool"}},
	}}

	if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := fr.Calls[1]
	if call.Name != "go" || strings.Join(call.Args, " ") != "build ./cmd/tool" || call.Dir == "" {
		t.Fatalf("portable build call = %+v", call)
	}
}

func TestGitAdapterInstallWithPortableBuildSteps(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://github.com/user/repo.git",
		"build": []any{
			map[string]any{"run": []any{"make"}},
			map[string]any{"run": []any{"make", "install"}},
		},
	}}

	if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "mytool"}, mc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fr.Calls) != 3 || fr.Calls[1].Name != "make" || fr.Calls[2].Name != "make" {
		t.Fatalf("build calls = %+v", fr.Calls)
	}
	if fr.Calls[1].Dir == "" || fr.Calls[1].Dir != fr.Calls[2].Dir {
		t.Fatalf("build steps should share clone directory: %+v", fr.Calls[1:])
	}
}

func TestGitAdapterInstallWithShellSyntaxBuild(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGitAdapter()
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}

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

	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}

	tempDir := t.TempDir()
	sharedDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("failed to create temp bin dir: %v", err)
	}

	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}

	tempDir := t.TempDir()
	privateDir := filepath.Join(tempDir, "mytool-private-dir")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("failed to create private dir: %v", err)
	}

	binaryPath := filepath.Join(privateDir, "mytool")
	if err := os.WriteFile(binaryPath, []byte("data"), 0o755); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{
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

type clonePopulatingRunner struct {
	calls []run.FakeCall
}

func (r *clonePopulatingRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: append([]string(nil), args...)})
	if name == "git" && len(args) > 0 && args[0] == "clone" {
		cloneDir := args[len(args)-1]
		artifactDir := filepath.Join(cloneDir, "dist")
		if err := os.MkdirAll(artifactDir, 0o755); err != nil {
			return run.Result{Err: err}
		}
		if err := os.WriteFile(filepath.Join(artifactDir, "tool"), []byte("artifact"), 0o755); err != nil {
			return run.Result{Err: err}
		}
	}
	return run.Result{ExitCode: 0}
}

func TestGitAdapterDeclaredCloneFieldsGovernRuntimeCommand(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":    "https://example.test/team/tool.git",
		"branch": "release-1",
		"depth":  int64(7),
	}}

	if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "different-name"}, mc); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if len(fr.Calls) == 0 {
		t.Fatal("expected git clone call")
	}
	call := fr.Calls[0]
	if call.Name != "git" {
		t.Fatalf("clone executable = %q, want git", call.Name)
	}
	wantPrefix := []string{"clone", "--depth", "7", "--branch", "release-1", "https://example.test/team/tool.git"}
	if len(call.Args) < len(wantPrefix)+1 {
		t.Fatalf("clone args = %v, want prefix %v plus destination", call.Args, wantPrefix)
	}
	for i, want := range wantPrefix {
		if call.Args[i] != want {
			t.Fatalf("clone args[%d] = %q, want %q; full args: %v", i, call.Args[i], want, call.Args)
		}
	}
}

func TestGitAdapterArtifactAndExtractToGovernCopiedOutput(t *testing.T) {
	rn := &clonePopulatingRunner{}
	dst := t.TempDir()
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":        "https://example.test/team/tool.git",
		"artifact":   "dist/tool",
		"extract_to": dst,
	}}

	if err := NewGitAdapter().Install(context.Background(), rn, &config.Tool{Name: "tool"}, mc); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "tool"))
	if err != nil {
		t.Fatalf("configured artifact was not copied to extract_to: %v", err)
	}
	if string(got) != "artifact" {
		t.Fatalf("copied artifact = %q, want %q", got, "artifact")
	}
}

func TestGitAdapterManagedPathsGovernCheckAndRemove(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "owned-a")
	second := filepath.Join(root, "owned-b")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"managed_paths": []any{first, second},
	}}
	adapter := NewGitAdapter()
	if !adapter.Check(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc) {
		t.Fatal("Check should require all configured managed_paths to exist")
	}
	if err := adapter.Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("managed path %q still exists after Remove", path)
		}
	}
}

func TestGitAdapterExplicitTagUsesCloneBranch(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.test/repo.git",
		"tag": "v1.2.3",
	}}
	if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "tool"}, mc); err != nil {
		t.Fatal(err)
	}
	call := fr.Calls[0]
	wantPrefix := []string{"clone", "--depth", "1", "--branch", "v1.2.3", "https://example.test/repo.git"}
	if len(call.Args) < len(wantPrefix) || !reflect.DeepEqual(call.Args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("clone argv=%v want prefix=%v", call.Args, wantPrefix)
	}
}

func TestGitAdapterExactRevisionFetchesAndDetaches(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.test/repo.git",
		"rev": "0123456789abcdef",
	}}
	if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "tool"}, mc); err != nil {
		t.Fatal(err)
	}
	if len(fr.Calls) != 3 {
		t.Fatalf("calls=%d want 3: %+v", len(fr.Calls), fr.Calls)
	}
	clone := fr.Calls[0]
	if clone.Name != "git" || !containsArg(clone.Args, "--no-checkout") {
		t.Fatalf("clone must use --no-checkout: %+v", clone)
	}
	cloneDir := clone.Args[len(clone.Args)-1]
	fetchWant := []string{"-C", cloneDir, "fetch", "--depth", "1", "origin", "0123456789abcdef"}
	if got := fr.Calls[1]; got.Name != "git" || !reflect.DeepEqual(got.Args, fetchWant) {
		t.Fatalf("fetch=%+v want git %v", got, fetchWant)
	}
	checkoutWant := []string{"-C", cloneDir, "checkout", "--detach", "FETCH_HEAD"}
	if got := fr.Calls[2]; got.Name != "git" || !reflect.DeepEqual(got.Args, checkoutWant) {
		t.Fatalf("checkout=%+v want git %v", got, checkoutWant)
	}
}

func TestGitAdapterDepthZeroOmitsDepthFlag(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":   "https://example.test/repo.git",
		"depth": int64(0),
	}}
	if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "tool"}, mc); err != nil {
		t.Fatal(err)
	}
	if containsArg(fr.Calls[0].Args, "--depth") {
		t.Fatalf("depth=0 must omit --depth: %v", fr.Calls[0].Args)
	}
}

func TestGitAdapterRejectsLatestWithExplicitRevision(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.test/{latest}/repo.git",
		"tag": "v1.2.3",
	}}
	if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "tool"}, mc); err == nil {
		t.Fatal("expected {latest} plus explicit tag to be rejected")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("conflicting revision intent reached runner: %+v", fr.Calls)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestGitAdapterCheckVerifiesRequestedRevisionWhenRepoIsOwned(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"extract_to": dir,
		"rev":        "deadbeef",
	}}

	fr := &run.FakeRunner{Stdout: "0123456789abcdef\n0123456789abcdef\n"}
	if !NewGitAdapter().Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should accept matching HEAD and requested revision")
	}
	if len(fr.Calls) != 1 || fr.Calls[0].Name != "git" {
		t.Fatalf("unexpected calls: %+v", fr.Calls)
	}
	want := []string{"-C", dir, "rev-parse", "HEAD", "deadbeef"}
	if !reflect.DeepEqual(fr.Calls[0].Args, want) {
		t.Fatalf("argv=%v want=%v", fr.Calls[0].Args, want)
	}

	fr = &run.FakeRunner{Stdout: "0123456789abcdef\nffffffffffffffff\n"}
	if NewGitAdapter().Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should reject revision drift")
	}
}

func TestGitAdapterInstalledVersionReportsOwnedRepoHEAD(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":        "https://example.test/repo.git",
		"extract_to": dir,
		"branch":     "main",
	}}
	fr := &run.FakeRunner{Stdout: "0123456789abcdef\n"}
	got, err := NewGitAdapter().InstalledVersion(context.Background(), fr, nil, mc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0123456789abcdef" {
		t.Fatalf("InstalledVersion=%q want commit HEAD", got)
	}
}

func TestGitAdapterSubmodulesAreExplicitTypedStep(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":        "https://example.test/repo.git",
		"submodules": true,
	}}
	if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "tool"}, mc); err != nil {
		t.Fatal(err)
	}
	if len(fr.Calls) != 2 {
		t.Fatalf("calls=%d want clone + submodule: %+v", len(fr.Calls), fr.Calls)
	}
	cloneDir := fr.Calls[0].Args[len(fr.Calls[0].Args)-1]
	want := []string{"-C", cloneDir, "submodule", "update", "--init", "--recursive"}
	if got := fr.Calls[1]; got.Name != "git" || !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("submodule call=%+v want git %v", got, want)
	}
}

func TestGitAdapterRejectsInvalidDepthBeforeClone(t *testing.T) {
	for _, depth := range []any{int64(-1), "-1", "shallow"} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			fr := &run.FakeRunner{}
			mc := &config.MethodCandidate{Config: map[string]any{
				"url":   "https://example.test/repo.git",
				"depth": depth,
			}}
			if err := NewGitAdapter().Install(context.Background(), fr, &config.Tool{Name: "tool"}, mc); err == nil {
				t.Fatalf("expected invalid depth %v to fail", depth)
			}
			if len(fr.Calls) != 0 {
				t.Fatalf("invalid depth reached runner: %+v", fr.Calls)
			}
		})
	}
}

func TestGitAdapterResolvePlanProjectsSourceAndRevision(t *testing.T) {
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{
		"url": "https://example.test/repo.git",
		"rev": "deadbeef",
	}}
	intent := plan.New("demo", "git", true)
	got, err := NewGitAdapter().ResolvePlan(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity.Source != "https://example.test/repo.git" || got.Identity.Revision != "deadbeef" {
		t.Fatalf("identity = %+v", got.Identity)
	}
}
