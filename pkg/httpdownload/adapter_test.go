package httpdownload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func TestHTTPAdapterKind(t *testing.T) {
	if NewHTTPAdapter().Kind() != "http" {
		t.Fatalf("Kind() = %q, want 'http'", NewHTTPAdapter().Kind())
	}
}

func TestHTTPAdapterAvailable(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewHTTPAdapter()

	if !adapter.Available(context.Background(), fr) {
		t.Fatal("Available should always return true (Go net/http fallback)")
	}
}

func TestHTTPAdapterCheckViaExtractTo(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0, Stdout: "file1"}
	adapter := NewHTTPAdapter()
	mc := &schema.MethodCandidate{Config: map[string]any{"extract_to": "/opt/tool"}}

	if !adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be true when extract_to exists and has files")
	}
}

func TestHTTPAdapterCheckViaBinary(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewHTTPAdapter()
	mc := &schema.MethodCandidate{Config: map[string]any{"binary": "somebin"}}

	if !adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be true when binary is in PATH")
	}
}

func TestHTTPAdapterCheckNotFound(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewHTTPAdapter()
	mc := &schema.MethodCandidate{Config: map[string]any{}}

	if adapter.Check(context.Background(), fr, nil, mc) {
		t.Fatal("Check should be false when nothing found")
	}
}

func TestHTTPAdapterInstallNoURL(t *testing.T) {
	adapter := NewHTTPAdapter()
	tool := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{Config: map[string]any{}}

	err := adapter.Install(context.Background(), nil, tool, mc)
	if err == nil {
		t.Fatal("expected error when no url, got nil")
	}
	if !strings.Contains(err.Error(), "no url") {
		t.Fatalf("error should mention 'no url', got: %v", err)
	}
}

func TestFileExtension(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://example.com/file.tar.gz", ".tar.gz"},
		{"https://example.com/file.tgz", ".tgz"},
		{"https://example.com/file.tar.bz2", ".tar.bz2"},
		{"https://example.com/file.tar.xz", ".tar.xz"},
		{"https://example.com/file.zip", ".zip"},
		{"https://example.com/file.deb", ".deb"},
		{"https://example.com/file", ""},
		{"https://example.com/file?version=1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			if got := fileExtension(tc.url); got != tc.want {
				t.Fatalf("fileExtension(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestSHA256File(t *testing.T) {
	content := []byte("hello world")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	hash, err := SHA256File(tmpFile)
	if err != nil {
		t.Fatalf("SHA256File: %v", err)
	}
	// echo -n "hello world" | sha256sum
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Fatalf("hash = %q, want %q", hash, expected)
	}
}

func TestVerifyChecksumMatch(t *testing.T) {
	content := []byte("hello world")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := VerifyChecksum(tmpFile, "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")
	if err != nil {
		t.Fatalf("VerifyChecksum should pass: %v", err)
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	content := []byte("hello world")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := VerifyChecksum(tmpFile, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

func TestVerifyChecksumEmpty(t *testing.T) {
	if err := VerifyChecksum("", ""); err != nil {
		t.Fatalf("empty checksum should pass: %v", err)
	}
}

func TestIsGitHubURL(t *testing.T) {
	if !IsGitHubURL("https://github.com/user/repo") {
		t.Fatal("should be GitHub URL")
	}
	if !IsGitHubURL("https://github.com/user/repo/releases/latest") {
		t.Fatal("should be GitHub URL")
	}
	if IsGitHubURL("https://gitlab.com/user/repo") {
		t.Fatal("should not be GitHub URL")
	}
}

func TestParseChecksumFile(t *testing.T) {
	input := strings.NewReader(`# comment
abc123  file1.txt
def456 *file2.bin
`)
	result, err := ParseChecksumFile(input)
	if err != nil {
		t.Fatalf("ParseChecksumFile: %v", err)
	}
	if result["file1.txt"] != "abc123" {
		t.Fatalf("file1.txt hash = %q, want 'abc123'", result["file1.txt"])
	}
	if result["file2.bin"] != "def456" {
		t.Fatalf("file2.bin hash = %q, want 'def456'", result["file2.bin"])
	}
}
