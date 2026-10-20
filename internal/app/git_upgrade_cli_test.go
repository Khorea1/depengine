package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
)

// TestGitLockV2UpgradeAndRemove verifies the exact-revision CLI lifecycle for
// a retained Git checkout: install at A, reconcile and upgrade to B, then
// remove only depengine-owned resources while leaving the parent directory
// intact.
func TestGitLockV2UpgradeAndRemove(t *testing.T) {
	if _, err := osexec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.email", "depengine-test@example.test")
	runGit(t, repoDir, "config", "user.name", "depengine test")
	writeFile(t, filepath.Join(repoDir, "payload.txt"), "revision A\n")
	runGit(t, repoDir, "add", "payload.txt")
	runGit(t, repoDir, "commit", "-m", "revision A")
	revisionA := runGit(t, repoDir, "rev-parse", "HEAD")
	repoURL, bareDir, stopServer := startGitHTTPServer(t, repoDir)
	defer stopServer()

	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	schemaPath := filepath.Join(root, "schema.toml")
	schema := fmt.Sprintf(`schema_version = 1

[defaults]
method_order = ["git"]

[tools.demo.git]
url = %s
branch = "main"
depth = 0
extract_to = %s
`, strconv.Quote(repoURL), strconv.Quote(checkout))
	writeFile(t, schemaPath, schema)

	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "git",
		Config: map[string]any{
			"url":        repoURL,
			"branch":     "main",
			"depth":      int64(0),
			"extract_to": checkout,
		},
	}}}
	method := tool.Methods[0]
	writeGitV2Lock(t, schemaPath, tool, method, revisionA)

	stateHome := t.TempDir()
	homeDir := t.TempDir()
	configHome := t.TempDir()
	cacheHome := t.TempDir()
	env := []string{
		"XDG_STATE_HOME=" + stateHome,
		"HOME=" + homeDir,
		"XDG_CONFIG_HOME=" + configHome,
		"XDG_CACHE_HOME=" + cacheHome,
	}

	code, out := runCommand(t, "install", env,
		"--schema", schemaPath, "--no-manifest", "--only", "demo", "--quiet")
	if code != 0 {
		t.Fatalf("install exit code %d, output:\n%s", code, out)
	}
	if got := runGit(t, checkout, "rev-parse", "HEAD"); got != revisionA {
		t.Fatalf("installed HEAD = %s, want %s", got, revisionA)
	}
	if got := readFile(t, filepath.Join(checkout, "payload.txt")); strings.TrimSpace(got) != "revision A" {
		t.Fatalf("installed payload = %q, want revision A", got)
	}
	st := loadTestState(t, stateHome)
	if got, ok := st.Tools["demo"]; !ok || got.Version != revisionA {
		t.Fatalf("state tool = %+v, want demo recorded at %s after install", st.Tools["demo"], revisionA)
	}

	writeFile(t, filepath.Join(repoDir, "payload.txt"), "revision B\n")
	runGit(t, repoDir, "add", "payload.txt")
	runGit(t, repoDir, "commit", "-m", "revision B")
	runGit(t, repoDir, "push", bareDir, "main")
	runGit(t, bareDir, "update-server-info")
	revisionB := runGit(t, repoDir, "rev-parse", "HEAD")
	writeGitV2Lock(t, schemaPath, tool, method, revisionB)

	code, out = runCommand(t, "upgrade", env,
		"--schema", schemaPath, "--no-manifest", "--force")
	if code != 0 {
		t.Fatalf("upgrade exit code %d, output:\n%s", code, out)
	}
	if got := runGit(t, checkout, "rev-parse", "HEAD"); got != revisionB {
		t.Fatalf("upgraded HEAD = %s, want %s", got, revisionB)
	}
	if got := readFile(t, filepath.Join(checkout, "payload.txt")); strings.TrimSpace(got) != "revision B" {
		t.Fatalf("upgraded payload = %q, want revision B", got)
	}
	st = loadTestState(t, stateHome)
	if got, ok := st.Tools["demo"]; !ok || got.Version != revisionB {
		t.Fatalf("state tool = %+v, want demo recorded at %s after upgrade", st.Tools["demo"], revisionB)
	}

	sibling := filepath.Join(root, "sibling.txt")
	writeFile(t, sibling, "keep me\n")
	code, out = runCommand(t, "remove", env, "--schema", schemaPath, "demo")
	if code != 0 {
		t.Fatalf("remove exit code %d, output:\n%s", code, out)
	}
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("checkout still exists after remove: %v", err)
	}
	if got := readFile(t, sibling); got != "keep me\n" {
		t.Fatalf("sibling payload = %q, want untouched", got)
	}
	st = loadTestState(t, stateHome)
	if _, ok := st.Tools["demo"]; ok {
		t.Fatalf("state still tracks demo after remove: %+v", st.Tools["demo"])
	}
}

func writeGitV2Lock(t *testing.T, schemaPath string, tool *config.Tool, method *config.MethodCandidate, revision string) {
	t.Helper()
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	intent.Identity.Revision = revision
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{intent})
	if err != nil {
		t.Fatalf("BuildLockDocument() error: %v", err)
	}
	lk, err := lock.NewUniversal(document, nil, nil)
	if err != nil {
		t.Fatalf("NewUniversal() error: %v", err)
	}
	if err := lock.Save(lock.DefaultPath(schemaPath), lk); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := osexec.Command("git", args...) // #nosec G204 -- test-owned git invocation with fixture-controlled arguments.
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=depengine test",
		"GIT_AUTHOR_EMAIL=depengine-test@example.test",
		"GIT_COMMITTER_NAME=depengine test",
		"GIT_COMMITTER_EMAIL=depengine-test@example.test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is a test-owned fixture under t.TempDir().
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func startGitHTTPServer(t *testing.T, repoDir string) (repoURL, bareDir string, stop func()) {
	t.Helper()
	bareDir = t.TempDir()
	runGit(t, filepath.Dir(repoDir), "clone", "--bare", repoDir, bareDir)
	runGit(t, bareDir, "update-server-info")
	server := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(bareDir))))
	repoURL = server.URL + "/" + filepath.Base(bareDir)
	return repoURL, bareDir, server.Close
}
