//go:build windows

package privatepath

import "testing"

func TestTempRootPathRejectsRelativeCacheHome(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "relative-cache-home")
	if got := TempRootPath(); got != "" {
		t.Fatalf("TempRootPath() = %q for relative LOCALAPPDATA, want empty", got)
	}
}
