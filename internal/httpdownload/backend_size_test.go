package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestGoDownloaderRejectsOversizedContentLengthAndPreservesExistingDestination(t *testing.T) {
	t.Setenv("DEPENGINE_DOWNLOAD_MAX_BYTES", "8")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "32")
		_, _ = w.Write([]byte(strings.Repeat("x", 32)))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(dest, []byte("keep existing destination"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewGoDownloader(&run.FakeRunner{}).Download(context.Background(), server.URL, dest)
	if err == nil || !strings.Contains(err.Error(), "8-byte") {
		t.Fatalf("Download() error = %v, want configured size-limit error", err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "keep existing destination" {
		t.Fatalf("destination after rejection = %q, %v; want existing contents preserved", got, err)
	}
}

func TestGoDownloaderStopsChunkedResponseAtLimitAndRemovesPartialFile(t *testing.T) {
	t.Setenv("DEPENGINE_DOWNLOAD_MAX_BYTES", "8")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("12345678"))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("9-over-limit"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := NewGoDownloader(&run.FakeRunner{}).Download(context.Background(), server.URL, dest)
	if err == nil || !strings.Contains(err.Error(), "8-byte") {
		t.Fatalf("Download() error = %v, want configured size-limit error", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("partial destination exists after rejection; stat error = %v", err)
	}
}

func TestMaxArtifactDownloadBytesDefaultsAndHonorsPositiveOverride(t *testing.T) {
	t.Setenv("DEPENGINE_DOWNLOAD_MAX_BYTES", "")
	if got := maxArtifactDownloadBytes(); got != defaultMaxArtifactBytes {
		t.Fatalf("default download limit = %d, want %d", got, defaultMaxArtifactBytes)
	}
	t.Setenv("DEPENGINE_DOWNLOAD_MAX_BYTES", "1024")
	if got := maxArtifactDownloadBytes(); got != 1024 {
		t.Fatalf("configured download limit = %d, want 1024", got)
	}
}

func TestDownloadAndExtractSmallStandaloneBzip2(t *testing.T) {
	t.Setenv("DEPENGINE_DOWNLOAD_MAX_BYTES", "128")
	bzipPath := writeBzip2Fixture(t, smallBzip2Fixture)
	compressed, err := os.ReadFile(bzipPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	downloaded := filepath.Join(t.TempDir(), "tool.bz2")
	if err := NewGoDownloader(&run.FakeRunner{}).Download(context.Background(), server.URL, downloaded); err != nil {
		t.Fatalf("download small bzip2 artifact: %v", err)
	}
	dest := t.TempDir()
	if err := extractBzip2(context.Background(), downloaded, dest, "tool", nil, false, ""); err != nil {
		t.Fatalf("extract downloaded bzip2 artifact: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "standalone bzip2 executable payload\n" {
		t.Fatalf("extracted payload = %q", got)
	}
}
