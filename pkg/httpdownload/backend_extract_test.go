package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"depengine/pkg/run"
)

func TestSelectDownloaderPrefersCurl(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	dl := SelectDownloader(context.Background(), fr)
	if _, ok := dl.(*CurlDownloader); !ok {
		t.Fatalf("expected CurlDownloader, got %T", dl)
	}
}

func TestSelectDownloaderFallsBackToGo(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	dl := SelectDownloader(context.Background(), fr)
	if _, ok := dl.(*GoDownloader); !ok {
		t.Fatalf("expected GoDownloader, got %T", dl)
	}
}

func TestCurlDownloaderDownload(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	dl := NewCurlDownloader(fr)
	if err := dl.Download(context.Background(), "https://example.com/file.tar.gz", "/tmp/dest"); err != nil {
		t.Fatalf("unexpected Download error: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	if fr.Calls[0].Name != "curl" {
		t.Errorf("expected curl command, got %s", fr.Calls[0].Name)
	}
}

func TestWgetDownloaderDownload(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	dl := NewWgetDownloader(fr)
	if err := dl.Download(context.Background(), "https://example.com/file.tar.gz", "/tmp/dest"); err != nil {
		t.Fatalf("unexpected Download error: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	if fr.Calls[0].Name != "wget" {
		t.Errorf("expected wget command, got %s", fr.Calls[0].Name)
	}
}

func TestCurlDownloaderFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	dl := NewCurlDownloader(fr)
	if err := dl.Download(context.Background(), "https://example.com/file", "/tmp/dest"); err == nil {
		t.Fatal("expected Download error, got nil")
	}
}

func TestExtractTarViaRunner(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	if err := Extract(context.Background(), "/tmp/src.tar.gz", "/tmp/dest", ".tar.gz", fr, false); err != nil {
		t.Fatalf("unexpected Extract error: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	got := fr.Calls[0]
	if got.Name != "tar" || got.Args[0] != "xzf" {
		t.Errorf("unexpected tar call for .tar.gz: %s %v", got.Name, got.Args)
	}
}

func TestExtractZipViaRunner(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	if err := Extract(context.Background(), "/tmp/src.zip", "/tmp/dest", ".zip", fr, false); err != nil {
		t.Fatalf("unexpected Extract error: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	if fr.Calls[0].Name != "unzip" {
		t.Errorf("expected unzip command, got %s", fr.Calls[0].Name)
	}
}

func TestExtractArchiveTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ext      string
		wantCmd  string
		wantFlag string
	}{
		{".tar.gz", "tar", "xzf"},
		{".tgz", "tar", "xzf"},
		{".tar.bz2", "tar", "xjf"},
		{".tar.xz", "tar", "xJf"},
		{".tar", "tar", "xf"},
		{".zip", "unzip", "-o"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			fr := &run.FakeRunner{ExitCode: 0}

			src := "/tmp/src" + tt.ext
			if err := Extract(context.Background(), src, "/tmp/dest", tt.ext, fr, false); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(fr.Calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(fr.Calls))
			}
			got := fr.Calls[0]
			if got.Name != tt.wantCmd {
				t.Errorf("expected cmd=%s, got %s", tt.wantCmd, got.Name)
			}
			if got.Args[0] != tt.wantFlag {
				t.Errorf("expected flag=%s, got %s", tt.wantFlag, got.Args[0])
			}
		})
	}
}

func TestExtractCopyBinary(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	src := filepath.Join(srcDir, "mybin")
	if err := os.WriteFile(src, []byte("binary-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	// copyBinary is called when ext doesn't match any archive format.
	if err := Extract(context.Background(), src, destDir, ".exe", nil, true); err != nil {
		t.Fatalf("unexpected Extract error: %v", err)
	}

	dest := filepath.Join(destDir, "mybin")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Fatal("expected destination file to exist")
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "binary-content" {
		t.Fatalf("unexpected content: %s", data)
	}
}

func TestExtractTarFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "tar: command not found"}

	if err := Extract(context.Background(), "/tmp/src.tar.gz", "/tmp/dest", ".tar.gz", fr, true); err == nil {
		t.Fatal("expected Extract error, got nil")
	}
}

func TestExtractZipFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "unzip: not found"}

	if err := Extract(context.Background(), "/tmp/src.zip", "/tmp/dest", ".zip", fr, true); err == nil {
		t.Fatal("expected Extract error, got nil")
	}
}

// --- archive safety (zip-slip / path-traversal) ---

func writeZipWithEntry(t *testing.T, path, entryName string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatalf("zip create entry: %v", err)
	}
	if _, err := w.Write([]byte("content")); err != nil {
		t.Fatalf("zip write entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

func TestExtractRejectsZipSlipRelative(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	writeZipWithEntry(t, zipPath, "../../etc/cron.d/evil")

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	err := Extract(context.Background(), zipPath, dest, ".zip", fr, false)
	if err == nil {
		t.Fatal("expected Extract to reject a path-traversal zip entry")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("expected extraction to be blocked before any subprocess call, got %d calls", len(fr.Calls))
	}
}

func TestExtractRejectsZipSlipAbsolute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil-abs.zip")
	writeZipWithEntry(t, zipPath, "/etc/passwd")

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	err := Extract(context.Background(), zipPath, dest, ".zip", fr, false)
	if err == nil {
		t.Fatal("expected Extract to reject an absolute zip entry path")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("expected extraction to be blocked before any subprocess call, got %d calls", len(fr.Calls))
	}
}

func TestExtractAllowsSafeZip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ok.zip")
	writeZipWithEntry(t, zipPath, "bin/tool")

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), zipPath, dest, ".zip", fr, false); err != nil {
		t.Fatalf("unexpected error for safe zip: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected extraction to proceed via unzip, got %d calls", len(fr.Calls))
	}
}

func writeTarGzWithEntry(t *testing.T, path string, hdr *tar.Header, body []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr.Size = int64(len(body))
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar write header: %v", err)
	}
	if len(body) > 0 {
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar write body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
}

func TestExtractRejectsTarSlip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "evil.tar.gz")
	writeTarGzWithEntry(t, tarPath, &tar.Header{
		Name: "../../etc/cron.d/evil",
		Mode: 0o644,
	}, []byte("pwned"))

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	err := Extract(context.Background(), tarPath, dest, ".tar.gz", fr, false)
	if err == nil {
		t.Fatal("expected Extract to reject a path-traversal tar entry")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("expected extraction to be blocked before any subprocess call, got %d calls", len(fr.Calls))
	}
}

func TestExtractRejectsTarAbsoluteSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "evil-link.tar.gz")
	writeTarGzWithEntry(t, tarPath, &tar.Header{
		Name:     "innocuous-name",
		Typeflag: tar.TypeSymlink,
		Linkname: "/etc/shadow",
		Mode:     0o777,
	}, nil)

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	err := Extract(context.Background(), tarPath, dest, ".tar.gz", fr, false)
	if err == nil {
		t.Fatal("expected Extract to reject a tar entry with an absolute symlink target")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("expected extraction to be blocked before any subprocess call, got %d calls", len(fr.Calls))
	}
}

func TestExtractAllowsSafeTarGz(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "ok.tar.gz")
	writeTarGzWithEntry(t, tarPath, &tar.Header{
		Name: "bin/tool",
		Mode: 0o755,
	}, []byte("binary"))

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), tarPath, dest, ".tar.gz", fr, false); err != nil {
		t.Fatalf("unexpected error for safe tar.gz: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected extraction to proceed via tar, got %d calls", len(fr.Calls))
	}
}

// .tar.xz has no decompressor in the standard library, so validateArchiveSafety
// is a no-op for it — Extract must still reach the system `tar` binary
// exactly as before this change (see TestExtractArchiveTypes).
func TestExtractSkipsSafetyCheckForUnsupportedCompression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Not a real xz file — proves the (skipped) check doesn't block on it.
	xzPath := filepath.Join(dir, "src.tar.xz")
	if err := os.WriteFile(xzPath, []byte("not really xz"), 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), xzPath, dest, ".tar.xz", fr, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected extraction to proceed via tar (no stdlib xz check), got %d calls", len(fr.Calls))
	}
}
