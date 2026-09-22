package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// tmpFile writes content into a temp file and returns its path. The file
// is cleaned up when the test ends. Used so locateDetectScript's env-
// override branch has something real to stat.
func tmpFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "detect_os.sh")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("tmpFile write: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}
