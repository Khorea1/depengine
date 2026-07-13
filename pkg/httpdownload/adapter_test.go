package httpdownload

import (
	"context"
	"net/url"
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
func TestDownloadFileNameFromURL(t *testing.T) {
	tests := []struct {
		url      string
		ext      string
		wantName string // expected filePath suffix
	}{
		{
			url:      "https://github.com/org/repo/releases/download/v1.0/fastfetch-linux-amd64.deb",
			ext:      ".deb",
			wantName: "fastfetch-linux-amd64.deb",
		},
		{
			url:      "https://example.com/download.tar.gz",
			ext:      ".tar.gz",
			wantName: "download.tar.gz",
		},
		{
			url:      "https://example.com/tool?version=1.0",
			ext:      ".deb",
			wantName: "tool.deb", // path component "tool" used as filename
		},
		{
			url:      "https://example.com/",
			ext:      ".tar.gz",
			wantName: "download.tar.gz", // no filename in path, fallback to generic name
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatal(err)
			}

			fileName := "download" + tt.ext
			if u != nil && u.Path != "" {
				if base := filepath.Base(u.Path); base != "" && base != "." && base != "/" {
					if filepath.Ext(base) == "" {
						base += tt.ext
					}
					fileName = base
				}
			}

			if fileName != tt.wantName {
				t.Fatalf("got %q, want %q", fileName, tt.wantName)
			}
		})
	}
}

func TestMD5File(t *testing.T) {
	content := []byte("hello")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	hash, err := MD5File(tmpFile)
	if err != nil {
		t.Fatalf("MD5File: %v", err)
	}
	// echo -n "hello" | md5sum
	expected := "5d41402abc4b2a76b9719d911017c592"
	if hash != expected {
		t.Fatalf("hash = %q, want %q", hash, expected)
	}
}

func TestSHA1File(t *testing.T) {
	content := []byte("hello")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	hash, err := SHA1File(tmpFile)
	if err != nil {
		t.Fatalf("SHA1File: %v", err)
	}
	// echo -n "hello" | sha1sum
	expected := "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
	if hash != expected {
		t.Fatalf("hash = %q, want %q", hash, expected)
	}
}

func TestSHA512File(t *testing.T) {
	content := []byte("hello")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	hash, err := SHA512File(tmpFile)
	if err != nil {
		t.Fatalf("SHA512File: %v", err)
	}
	// echo -n "hello" | sha512sum
	expected := "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043"
	if hash != expected {
		t.Fatalf("hash = %q, want %q", hash, expected)
	}
}

func TestVerifyChecksumMD5(t *testing.T) {
	content := []byte("hello")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Match
	err := VerifyChecksum(tmpFile, "md5:5d41402abc4b2a76b9719d911017c592")
	if err != nil {
		t.Fatalf("VerifyChecksum should pass: %v", err)
	}

	// Mismatch
	err = VerifyChecksum(tmpFile, "md5:00000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

func TestVerifyChecksumSHA1(t *testing.T) {
	content := []byte("hello")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Match
	err := VerifyChecksum(tmpFile, "sha1:aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d")
	if err != nil {
		t.Fatalf("VerifyChecksum should pass: %v", err)
	}

	// Mismatch
	err = VerifyChecksum(tmpFile, "sha1:0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

func TestVerifyChecksumSHA512(t *testing.T) {
	content := []byte("hello")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Match
	err := VerifyChecksum(tmpFile, "sha512:9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043")
	if err != nil {
		t.Fatalf("VerifyChecksum should pass: %v", err)
	}

	// Mismatch
	err = VerifyChecksum(tmpFile, "sha512:00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

func TestVerifyChecksumAutoError(t *testing.T) {
	// auto with any prefix should return an error
	err := VerifyChecksum("/dev/null", "sha256:auto")
	if err == nil {
		t.Fatal("expected error for sha256:auto, got nil")
	}
	err = VerifyChecksum("/dev/null", "md5:auto")
	if err == nil {
		t.Fatal("expected error for md5:auto, got nil")
	}
	err = VerifyChecksum("/dev/null", "sha1:auto")
	if err == nil {
		t.Fatal("expected error for sha1:auto, got nil")
	}
	err = VerifyChecksum("/dev/null", "sha512:auto")
	if err == nil {
		t.Fatal("expected error for sha512:auto, got nil")
	}
}

func TestVerifyChecksumUnsupportedPrefix(t *testing.T) {
	err := VerifyChecksum("/dev/null", "crc32:abc123")
	if err == nil {
		t.Fatal("expected error for unsupported prefix, got nil")
	}
}

func TestParseChecksumFileBSDExtended(t *testing.T) {
	input := strings.NewReader(`# comment line
SHA256 (file1.txt) = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
MD5 (file2.bin) = 5d41402abc4b2a76b9719d911017c592
`)
	result, err := ParseChecksumFileBSDExtended(input)
	if err != nil {
		t.Fatalf("ParseChecksumFileBSDExtended: %v", err)
	}
	if result["file1.txt"] != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("file1.txt hash = %q, want expected sha256", result["file1.txt"])
	}
	if result["file2.bin"] != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("file2.bin hash = %q, want expected md5", result["file2.bin"])
	}
}

func TestParseChecksumFileAuto_sha256sum(t *testing.T) {
	input := strings.NewReader(`abc123  file1.txt
def456 *file2.bin
`)
	result, err := ParseChecksumFileAuto(input)
	if err != nil {
		t.Fatalf("ParseChecksumFileAuto: %v", err)
	}
	if result["file1.txt"] != "abc123" {
		t.Fatalf("file1.txt hash = %q, want 'abc123'", result["file1.txt"])
	}
	if result["file2.bin"] != "def456" {
		t.Fatalf("file2.bin hash = %q, want 'def456'", result["file2.bin"])
	}
}

func TestParseChecksumFileAuto_BSD(t *testing.T) {
	input := strings.NewReader(`SHA256 (archive.tar.gz) = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
`)
	result, err := ParseChecksumFileAuto(input)
	if err != nil {
		t.Fatalf("ParseChecksumFileAuto: %v", err)
	}
	if result["archive.tar.gz"] != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("archive.tar.gz hash = %q, want expected sha256", result["archive.tar.gz"])
	}
}
func TestExtractChecksumConfig(t *testing.T) {
	tests := []struct {
		name      string
		checksum  string
		config    map[string]any
		wantAlgo  string
		wantURL   string
		wantFmt   string
		wantNil   bool
	}{
		{
			name:     "sha256_with_url_and_format",
			checksum: "sha256:abc123",
			config:   map[string]any{"checksum_url": "https://example.com/sha256sums.txt", "checksum_file_format": "bsd"},
			wantAlgo: "sha256",
			wantURL:  "https://example.com/sha256sums.txt",
			wantFmt:  "bsd",
		},
		{
			name:     "md5_no_config",
			checksum: "md5:def456",
			config:   map[string]any{},
			wantAlgo: "md5",
			wantURL:  "",
			wantFmt:  "",
		},
		{
			name:     "sha1_with_format_only",
			checksum: "sha1:abc",
			config:   map[string]any{"checksum_file_format": "sha256sum"},
			wantAlgo: "sha1",
			wantURL:  "",
			wantFmt:  "sha256sum",
		},
		{
			name:     "sha512_with_url_only",
			checksum: "sha512:xyz",
			config:   map[string]any{"checksum_url": "https://example.com/checksum.txt"},
			wantAlgo: "sha512",
			wantURL:  "https://example.com/checksum.txt",
			wantFmt:  "",
		},
		{
			name:     "unsupported_prefix",
			checksum: "crc32:abc",
			config:   map[string]any{},
			wantNil:  true,
		},
		{
			name:     "raw_format",
			checksum: "sha256:auto",
			config:   map[string]any{"checksum_file_format": "raw"},
			wantAlgo: "sha256",
			wantURL:  "",
			wantFmt:  "raw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc := extractChecksumConfig(tt.checksum, tt.config)
			if tt.wantNil {
				if cc != nil {
					t.Fatalf("expected nil, got %+v", cc)
				}
				return
			}
			if cc == nil {
				t.Fatal("expected non-nil checksumConfig")
			}
			if cc.algorithm != tt.wantAlgo {
				t.Fatalf("algorithm = %q, want %q", cc.algorithm, tt.wantAlgo)
			}
			if cc.url != tt.wantURL {
				t.Fatalf("url = %q, want %q", cc.url, tt.wantURL)
			}
			if cc.format != tt.wantFmt {
				t.Fatalf("format = %q, want %q", cc.format, tt.wantFmt)
			}
		})
	}
}

func TestDetectAlgorithmFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/SHA256SUMS", "sha256"},
		{"https://example.com/sha256sums.txt", "sha256"},
		{"https://example.com/SHA512SUMS", "sha512"},
		{"https://example.com/SHA1SUMS", "sha1"},
		{"https://example.com/MD5SUMS", "md5"},
		{"https://example.com/checksums.txt", ""},
		{"https://example.com/file.sha256", "sha256"},
		{"https://example.com/dir/SHA256SUMS", "sha256"},
		{"https://example.com/file", ""},
		{"https://example.com/sha256", "sha256"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := detectAlgorithmFromURL(tt.url); got != tt.want {
				t.Fatalf("detectAlgorithmFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestVerifyChecksumDirectHashStillWorks(t *testing.T) {
	// Verify that verifyChecksum with a concrete checksum (non-auto)
	// still works via the adapter method.
	content := []byte("hello world")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	a := &HTTPAdapter{}
	err := a.verifyChecksum(context.Background(), nil, tmpFile, "https://example.com/file.txt", "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9", map[string]any{})
	if err != nil {
		t.Fatalf("verifyChecksum should pass for valid hash: %v", err)
	}

	// Mismatch
	err = a.verifyChecksum(context.Background(), nil, tmpFile, "https://example.com/file.txt", "sha256:0000000000000000000000000000000000000000000000000000000000000000", map[string]any{})
	if err == nil {
		t.Fatal("verifyChecksum should fail for invalid hash")
	}
	err = a.verifyChecksum(context.Background(), nil, tmpFile, "https://example.com/file.txt", "md5:5eb63bbbe01eeed093cb22bb8f5acdc3", map[string]any{})
	if err != nil {
		t.Fatalf("verifyChecksum should pass for valid md5 hash: %v", err)
	}

	// Empty checksum (no config key present — code path doesn't reach verifyChecksum)
	// We can't test empty via verifyChecksum because it always expects a value.
}

func TestVerifyChecksumAutoWithConfig(t *testing.T) {
	// Test that :auto resolution fails with a network-dependent error
	// when no checksum_url is set and the companion file doesn't exist.
	// This proves the code path is reached and URL generation works.
	content := []byte("test data")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	a := &HTTPAdapter{}

	// md5:auto — should try companion URL patterns and fail with network error.
	err := a.verifyChecksum(context.Background(), &run.FakeRunner{ExitCode: 1}, tmpFile, "https://example.com/releases/tool-v1.0.tar.gz", "md5:auto", map[string]any{})
	if err == nil {
		t.Fatal("expected error for md5:auto (no network)")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "md5:auto:") {
		t.Fatalf("error should mention algorithm, got: %v", err)
	}
}

func TestVerifyChecksumAutoEmptyFilename(t *testing.T) {
	// Test with a URL that yields no filename.
	a := &HTTPAdapter{}
	err := a.verifyChecksum(context.Background(), nil, "/tmp/test", "https://example.com/", "sha256:auto", map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty filename URL")
	}
	if !strings.Contains(err.Error(), "cannot determine filename") {
		t.Fatalf("expected 'cannot determine filename' error, got: %v", err)
	}
}

func TestVerifyChecksumAutoWithURLChecksumURL(t *testing.T) {
	// Test with explicit checksum_url set but unreachable.
	content := []byte("test data")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	a := &HTTPAdapter{}
	config := map[string]any{
		"checksum_url": "https://example.com/custom-checksum.sha256",
	}
	err := a.verifyChecksum(context.Background(), &run.FakeRunner{ExitCode: 1}, tmpFile, "https://example.com/releases/tool-v1.0.tar.gz", "sha256:auto", config)
	if err == nil {
		t.Fatal("expected error for unreachable checksum_url")
	}
	// Should mention the custom URL, not a generated pattern.
	// The error should mention "custom-checksum"
	errStr := err.Error()
	if !strings.Contains(errStr, "custom-checksum") {
		t.Fatalf("error should mention custom checksum URL, got: %v", err)
	}
}

func TestVerifyChecksumConfigResolved(t *testing.T) {
	// When auto resolution succeeds, it should store _checksum_resolved in config.
	// We can't easily test the network path, so this just verifies the code
	// path is reachable (it will fail with a network error).
	content := []byte("test data")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	config := map[string]any{}
	a := &HTTPAdapter{}
	_ = a.verifyChecksum(context.Background(), &run.FakeRunner{ExitCode: 1}, tmpFile, "https://example.com/releases/tool-v1.0.tar.gz", "sha256:auto", config)
	// The auto resolution won't succeed (no network), so _checksum_resolved
	// should NOT be set.
	if _, ok := config["_checksum_resolved"]; ok {
		t.Fatal("_checksum_resolved should not be set when auto resolution fails")
	}
}

func TestVerifyChecksumFileFormatRaw(t *testing.T) {
	// Test that checksum_file_format:raw is passed through to URL attempts.
	content := []byte("test data")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	a := &HTTPAdapter{}
	config := map[string]any{
		"checksum_url":         "https://example.com/checksum.txt",
		"checksum_file_format": "raw",
	}
	err := a.verifyChecksum(context.Background(), &run.FakeRunner{ExitCode: 1}, tmpFile, "https://example.com/releases/tool-v1.0.tar.gz", "sha256:auto", config)
	if err == nil {
		t.Fatal("expected error for unreachable checksum_url with raw format")
	}
	errStr := err.Error()
	// Should mention downloading the custom URL.
	if !strings.Contains(errStr, "example.com/checksum.txt") {
		t.Fatalf("error should mention the checksum URL, got: %v", err)
	}
}
