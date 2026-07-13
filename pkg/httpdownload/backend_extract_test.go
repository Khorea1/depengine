package httpdownload

import (
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
