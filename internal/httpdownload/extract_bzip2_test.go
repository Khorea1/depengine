package httpdownload

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	smallBzip2Fixture = "QlpoOTFBWSZTWeG8K4EAAA7ZgAAQQAAQAD4lznAgACIgaekMg9QpkxMgyMuLTfcLSrNekJHVMEI4To4Sd418XckU4UJDhvCuBA=="
	bombBzip2Fixture  = "QlpoOTFBWSZTWYmNA+gDbuNEAAAQIAAgADCATUYVCETRUIROYoKyTKazGW+XugEiOYgAACBAAEAAYQBSNFQhGSoQji7kinChIT9bmBg="
)

func TestExtractBzip2(t *testing.T) {
	want := []byte("standalone bzip2 executable payload\n")
	src := writeBzip2Fixture(t, smallBzip2Fixture)
	dest := t.TempDir()
	tmpDir := usePrivateBzip2TempDir(t)

	if err := extractBzip2(context.Background(), src, dest, "tool", nil, false, ""); err != nil {
		t.Fatalf("extractBzip2() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "tool")) // #nosec G304 -- dest is a fresh test-owned temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted bytes = %q, want %q", got, want)
	}
	info, err := os.Stat(filepath.Join(dest, "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o755 {
			t.Errorf("extracted mode = %04o, want 0755", got)
		}
	}
	assertBzip2TempDirEmpty(t, tmpDir)
}

func TestExtractBzip2ExpansionLimit(t *testing.T) {
	src := writeBzip2Fixture(t, bombBzip2Fixture)
	dest := t.TempDir()
	tmpDir := usePrivateBzip2TempDir(t)
	budget := &archiveExpansionBudget{used: archiveExpansionLimit - 64}

	err := extractBzip2WithBudget(context.Background(), src, dest, "tool", nil, false, "", budget)
	if err == nil || !strings.Contains(err.Error(), "expansion exceeds") {
		t.Fatalf("extractBzip2WithBudget() error = %v, want expansion-limit error", err)
	}
	if budget.used != archiveExpansionLimit {
		t.Errorf("expanded bytes accounted = %d, want limit %d", budget.used, archiveExpansionLimit)
	}
	assertBzip2TempDirEmpty(t, tmpDir)
	if _, err := os.Stat(filepath.Join(dest, "tool")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("binary exists after expansion rejection; stat error = %v", err)
	}
}

func TestExtractBzip2CancellationCleansTemporaryFile(t *testing.T) {
	src := writeBzip2Fixture(t, bombBzip2Fixture)
	dest := t.TempDir()
	tmpDir := usePrivateBzip2TempDir(t)

	err := extractBzip2WithBudget(&cancelDuringBzip2ReadContext{}, src, dest, "tool", nil, false, "", &archiveExpansionBudget{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("extractBzip2WithBudget() error = %v, want context.Canceled", err)
	}
	assertBzip2TempDirEmpty(t, tmpDir)
	if _, err := os.Stat(filepath.Join(dest, "tool")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("binary exists after cancellation; stat error = %v", err)
	}
}

type cancelDuringBzip2ReadContext struct {
	reads int
}

func (c cancelDuringBzip2ReadContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c cancelDuringBzip2ReadContext) Done() <-chan struct{}       { return nil }
func (c cancelDuringBzip2ReadContext) Value(any) any               { return nil }
func (c *cancelDuringBzip2ReadContext) Err() error {
	c.reads++
	if c.reads > 1 {
		return context.Canceled
	}
	return nil
}

func writeBzip2Fixture(t *testing.T, encoded string) string {
	t.Helper()
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.bz2")
	if err := os.WriteFile(path, compressed, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func usePrivateBzip2TempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	return dir
}

func assertBzip2TempDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("bzip2 temporary files remain in isolated temp directory %s: %v", dir, entries)
	}
}
