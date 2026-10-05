//go:build aix || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package httpdownload

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const unprivilegedTestChildEnv = "DEPENGINE_TEST_UNPRIVILEGED_CHILD"

// runAsUnprivilegedTest reruns a root-started test as UID/GID 65534. Permission
// failure tests need real unprivileged filesystem behavior, while BSD CI
// executes the test suite as root. Returning true means the child ran the test.
func runAsUnprivilegedTest(t *testing.T) bool {
	t.Helper()
	if os.Geteuid() != 0 {
		return false
	}
	if os.Getenv(unprivilegedTestChildEnv) == "1" {
		t.Fatal("test subprocess retained root identity")
	}

	dir, err := os.MkdirTemp("/tmp", "depengine-unprivileged-test-*")
	if err != nil {
		t.Fatalf("create unprivileged test directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove unprivileged test directory: %v", err)
		}
	})
	sourcePath, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	source, err := os.Open(sourcePath) // #nosec G304 -- sourcePath comes from os.Executable, not user input.
	if err != nil {
		t.Fatalf("open test executable: %v", err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			t.Errorf("close test executable: %v", err)
		}
	}()

	childPath := filepath.Join(dir, "httpdownload.test")
	child, err := os.OpenFile(childPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- childPath is inside the newly created private temporary directory.
	if err != nil {
		t.Fatalf("copy test executable: %v", err)
	}
	_, copyErr := io.Copy(child, source)
	closeErr := child.Close()
	if copyErr != nil {
		t.Fatalf("copy test executable: %v", copyErr)
	}
	if closeErr != nil {
		t.Fatalf("close test executable: %v", closeErr)
	}
	if err := os.Chmod(childPath, 0o755); err != nil { // #nosec G302 -- child test binary must be executable by its target uid.
		t.Fatalf("make test executable runnable: %v", err)
	}
	if err := os.Chown(dir, 65534, 65534); err != nil {
		t.Fatalf("transfer test directory: %v", err)
	}

	cmd := exec.Command(childPath, "-test.run", "^"+t.Name()+"$") // #nosec G204 -- childPath is the copied current test binary.
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	cmd.Env = make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "TMPDIR=") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+dir, unprivilegedTestChildEnv+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged child failed: %v\n%s", err, output)
	}
	return true
}
