package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func bootstrapCommand(t *testing.T, dir, tool string, args ...string) string {
	t.Helper()
	cmd := exec.Command(tool, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", tool, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestInstallSourceBootstrapsAndPreservesPreviousBinaryOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("source bootstrap is a POSIX shell installer")
	}
	for _, tool := range []string{"sh", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable: %v", tool, err)
		}
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.invalid/bootstrap-test\n\ngo 1.27.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(`package main
import "fmt"
func main() { fmt.Println("fixture-version") }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	bootstrapCommand(t, src, "git", "init", "-q")
	bootstrapCommand(t, src, "git", "add", ".")
	bootstrapCommand(t, src, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "initial")

	binDir := filepath.Join(t.TempDir(), "with spaces", "bin")
	install := func(args ...string) (string, error) {
		cmd := exec.Command("sh", append([]string{"install-source.sh", "--source", src, "--bin-dir", binDir}, args...)...)
		cmd.Dir = "."
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if output, err := install(); err != nil {
		t.Fatalf("bootstrap failed: %v\n%s", err, output)
	}
	bin := filepath.Join(binDir, "depengine")
	previous, err := os.ReadFile(bin)
	if err != nil || len(previous) == 0 {
		t.Fatalf("installed executable unavailable: %v", err)
	}
	if got := bootstrapCommand(t, binDir, bin); got != "fixture-version" {
		t.Fatalf("installed executable output = %q", got)
	}
	initialCommit := bootstrapCommand(t, src, "git", "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\nfunc invalid(\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bootstrapCommand(t, src, "git", "add", "main.go")
	bootstrapCommand(t, src, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "broken")
	if output, err := install(); err == nil || !strings.Contains(output, "previous executable was preserved") {
		t.Fatalf("broken source build should fail without modifying old executable: %v\n%s", err, output)
	}
	unchanged, err := os.ReadFile(bin)
	if err != nil || !bytes.Equal(unchanged, previous) {
		t.Fatalf("old executable damaged after build failure: %v", err)
	}
	if staged, err := filepath.Glob(filepath.Join(binDir, ".depengine.*")); err != nil || len(staged) != 0 {
		t.Fatalf("staging artifacts remain: %v (err=%v)", staged, err)
	}
	if output, err := install("--ref", initialCommit); err != nil {
		t.Fatalf("pinned bootstrap failed: %v\n%s", err, output)
	}
	if got := bootstrapCommand(t, binDir, bin); got != "fixture-version" {
		t.Fatalf("pinned executable output = %q", got)
	}
}
