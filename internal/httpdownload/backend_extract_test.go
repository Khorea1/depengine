package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestSelectDownloaderPrefersCurl(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"curl": true, "wget": true}}

	dl := SelectDownloader(context.Background(), fr)
	if _, ok := dl.(*CurlDownloader); !ok {
		t.Fatalf("expected CurlDownloader, got %T", dl)
	}
}

func TestSelectDownloaderFallsBackToWget(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"curl": false, "wget": true}}

	dl := SelectDownloader(context.Background(), fr)
	if _, ok := dl.(*WgetDownloader); !ok {
		t.Fatalf("expected WgetDownloader, got %T", dl)
	}
}

func TestSelectDownloaderFallsBackToGo(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1, LookPaths: map[string]bool{"curl": false, "wget": false}}

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

func TestExtractExternalTarViaStreamingDecoder(t *testing.T) {
	t.Parallel()
	payload := plainTarBytes(t, []tarEntry{{
		header: tar.Header{Name: "bin/tool", Typeflag: tar.TypeReg, Mode: 0o755},
		body:   []byte("binary"),
	}})
	tests := []struct {
		ext     string
		decoder string
	}{
		{ext: ".tar.xz", decoder: "xz"},
		{ext: ".tar.zst", decoder: "zstd"},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src"+tt.ext)
			dest := filepath.Join(dir, "dest")
			fr := &run.FakeRunner{Stdout: string(payload)}

			if err := Extract(context.Background(), src, dest, tt.ext, fr, false, ""); err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			if len(fr.Calls) != 1 {
				t.Fatalf("calls = %+v, want one decoder call", fr.Calls)
			}
			got := fr.Calls[0]
			wantArgs := []string{"-d", "-c", "--", src}
			if got.Name != tt.decoder || !slices.Equal(got.Args, wantArgs) {
				t.Fatalf("decoder call = %s %v, want %s %v", got.Name, got.Args, tt.decoder, wantArgs)
			}
			data, err := os.ReadFile(filepath.Join(dest, "bin", "tool")) // #nosec G304 -- dest is test-controlled under t.TempDir.
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "binary" {
				t.Fatalf("extracted content = %q, want binary", data)
			}
		})
	}
}

func TestExtractZipNative(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.zip")
	writeZipWithEntry(t, src, "bin/tool")

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), src, dest, ".zip", fr, false, ""); err != nil {
		t.Fatalf("unexpected Extract error: %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native ZIP extraction invoked subprocesses: %+v", fr.Calls)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool")) // #nosec G304 -- dest is test-controlled under t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Fatalf("extracted content = %q, want content", string(data))
	}
}

func TestExternalTarDecoderDoesNotReceiveDestination(t *testing.T) {
	t.Parallel()
	payload := plainTarBytes(t, []tarEntry{{
		header: tar.Header{Name: "tool", Typeflag: tar.TypeReg, Mode: 0o644},
		body:   []byte("x"),
	}})
	dir := t.TempDir()
	src := filepath.Join(dir, "src.tar.xz")
	dest := filepath.Join(dir, "sensitive-destination")
	fr := &run.FakeRunner{Stdout: string(payload)}
	if err := Extract(context.Background(), src, dest, ".tar.xz", fr, false, ""); err != nil {
		t.Fatal(err)
	}
	for _, arg := range fr.Calls[0].Args {
		if arg == dest {
			t.Fatalf("decoder received extraction destination in argv: %+v", fr.Calls[0])
		}
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
	// sudoRequired=false here: destDir is a plain user-writable tempdir, so
	// the direct os.WriteFile path is exercised (see
	// TestExtractCopyBinaryElevated for the sudoRequired=true path).
	if err := Extract(context.Background(), src, destDir, ".exe", nil, false, ""); err != nil {
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

func TestExtractBzip2Binary(t *testing.T) {
	src := filepath.Join(t.TempDir(), "tool.bz2")
	compressed, err := base64.StdEncoding.DecodeString("QlpoOTFBWSZTWQDaM14AAAERgAACOiGUICAAMQDTTQQAYi5VGAEkMvF3JFOFCQANozXg")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, compressed, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := extract(context.Background(), src, dest, ".bz2", "tool", nil, false, "tool"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary-content" {
		t.Fatalf("decompressed content = %q", got)
	}
}

func TestExtractCopyBinaryUsesConfiguredName(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "release-name-amd64")
	if err := os.WriteFile(src, []byte("binary-content"), 0o755); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()

	if err := extract(context.Background(), src, destDir, "", "tool", nil, false, "tool"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "tool")); err != nil {
		t.Fatalf("configured binary name was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, filepath.Base(src))); !os.IsNotExist(err) {
		t.Fatalf("download basename should not be installed, got err %v", err)
	}
}

func TestExtractArchiveIgnoresBinaryName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tool.tar.gz")
	writeTarGzWithEntry(t, src, &tar.Header{Name: "bin/tool", Mode: 0o755}, []byte("binary"))
	dest := filepath.Join(dir, "dest")
	fr := &run.FakeRunner{ExitCode: 0}

	if err := extract(context.Background(), src, dest, ".tar.gz", "renamed", fr, false, "tool"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native archive extraction invoked subprocesses: %+v", fr.Calls)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "tool")); err != nil {
		t.Fatalf("archive payload missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "renamed")); !os.IsNotExist(err) {
		t.Fatalf("binary name should not rename archive entries, got err %v", err)
	}
}

// TestExtractCopyBinaryElevated covers the system-scope single-file
// install path: copyBinary must shell out through the elevation prefix to
// stage the binary and atomically commit it instead of touching a
// root-owned directory directly (unlike a plain os.WriteFile). This test
// forces the non-root branch the same way TestElevationGuardNoMethod does.
func TestExtractCopyBinaryElevated(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The sudo/install/mv staging flow targets Unix paths and
		// Unix elevation; Windows elevation takes a different path.
		t.Skip("sudo staging flow is Unix-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("test requires non-root: the sudoRequired branch only triggers when Geteuid() != 0")
	}
	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "mybin")
	if err := os.WriteFile(src, []byte("binary-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	fr := &run.FakeRunner{ExitCode: 0}
	if err := Extract(context.Background(), src, "/usr/local/bin", ".exe", fr, true, "mytool"); err != nil {
		t.Fatalf("unexpected Extract error: %v", err)
	}

	if len(fr.Calls) != 2 {
		t.Fatalf("expected 2 elevated calls, got %d: %+v", len(fr.Calls), fr.Calls)
	}
	want := []run.FakeCall{
		{Name: "sudo", Args: []string{"install", "-m", "0755", src, "/usr/local/bin/mybin.depengine-new"}},
		{Name: "sudo", Args: []string{"mv", "-f", "--", "/usr/local/bin/mybin.depengine-new", "/usr/local/bin/mybin"}},
	}
	for i := range want {
		if got := fr.Calls[i]; got.Name != want[i].Name || !slices.Equal(got.Args, want[i].Args) {
			t.Errorf("call[%d] = %s %v, want %s %v", i, got.Name, got.Args, want[i].Name, want[i].Args)
		}
	}
}

func TestExtractCopyBinaryElevatedUsesConfiguredName(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The sudo/install/mv staging flow targets Unix paths and
		// Unix elevation; Windows elevation takes a different path.
		t.Skip("sudo staging flow is Unix-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("test requires non-root")
	}
	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")

	src := filepath.Join(t.TempDir(), "release-name-amd64")
	if err := os.WriteFile(src, []byte("binary-content"), 0o755); err != nil {
		t.Fatal(err)
	}
	fr := &run.FakeRunner{ExitCode: 0}
	if err := extract(context.Background(), src, "/usr/local/bin", "", "tool", fr, true, "tool"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := [][]string{
		{"install", "-m", "0755", src, "/usr/local/bin/tool.depengine-new"},
		{"mv", "-f", "--", "/usr/local/bin/tool.depengine-new", "/usr/local/bin/tool"},
	}
	if len(fr.Calls) != len(want) {
		t.Fatalf("calls = %+v, want %d calls", fr.Calls, len(want))
	}
	for i := range want {
		if got := fr.Calls[i].Args; fr.Calls[i].Name != "sudo" || !slices.Equal(got, want[i]) {
			t.Errorf("call[%d] = %s %v, want sudo %v", i, fr.Calls[i].Name, got, want[i])
		}
	}
}

// TestExtractCopyBinaryElevationUnavailable proves copyBinary now actually
// consults elevationGuard: when sudo is required, the process isn't root,
// and no elevation method is available, Extract must fail loudly instead of
// falling back to an unprivileged write that would silently do the wrong
// thing (or fail with a confusing permission error deep inside os.WriteFile).
func TestExtractCopyBinaryElevationUnavailable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test requires non-root: the sudoRequired branch only triggers when Geteuid() != 0")
	}
	if run.ElevationPrefix() != nil {
		t.Skip("test requires no elevation method available on this machine")
	}

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "mybin")
	if err := os.WriteFile(src, []byte("binary-content"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Extract(context.Background(), src, "/usr/local/bin", ".exe", &run.FakeRunner{}, true, "mytool")
	if err == nil {
		t.Fatal("expected error when sudo required but no elevation method is available")
	}
	if !strings.Contains(err.Error(), "mytool") {
		t.Errorf("error should name the tool, got: %v", err)
	}
}

func TestExtractExternalTarDecoderFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.tar.xz")
	payload := plainTarBytes(t, []tarEntry{{
		header: tar.Header{Name: "tool", Typeflag: tar.TypeReg, Mode: 0o644},
		body:   []byte("binary"),
	}})
	fr := &run.FakeRunner{Stdout: string(payload), ExitCode: 1, Stderr: "corrupt xz stream"}

	err := Extract(context.Background(), src, filepath.Join(dir, "dest"), ".tar.xz", fr, false, "")
	if err == nil {
		t.Fatal("expected decoder failure")
	}
	if !strings.Contains(err.Error(), "xz decoder") || !strings.Contains(err.Error(), "corrupt xz stream") {
		t.Fatalf("decoder failure = %v", err)
	}
}

func TestExtractZipFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "broken.zip")
	if err := os.WriteFile(src, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	fr := &run.FakeRunner{ExitCode: 0}
	if err := Extract(context.Background(), src, filepath.Join(dir, "dest"), ".zip", fr, false, ""); err == nil {
		t.Fatal("expected Extract error, got nil")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("corrupt native ZIP should not invoke subprocesses: %+v", fr.Calls)
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

func writeZipSymlink(t *testing.T, archivePath, name, target string) {
	t.Helper()
	f, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- archivePath is test-controlled under t.TempDir.
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	hdr := &zip.FileHeader{Name: name, Method: zip.Store}
	hdr.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("zip create symlink: %v", err)
	}
	if _, err := w.Write([]byte(target)); err != nil {
		t.Fatalf("zip write symlink: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestExtractRejectsZipSlipRelative(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	writeZipWithEntry(t, zipPath, "../../etc/cron.d/evil")

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	err := Extract(context.Background(), zipPath, dest, ".zip", fr, false, "")
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
	err := Extract(context.Background(), zipPath, dest, ".zip", fr, false, "")
	if err == nil {
		t.Fatal("expected Extract to reject an absolute zip entry path")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("expected extraction to be blocked before any subprocess call, got %d calls", len(fr.Calls))
	}
}

func TestExtractRejectsZipSymlinkBeforeUnzip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil-link.zip")
	writeZipSymlink(t, zipPath, "nested/link", "../../outside")

	fr := &run.FakeRunner{ExitCode: 0}
	err := Extract(context.Background(), zipPath, filepath.Join(dir, "dest"), ".zip", fr, false, "")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected ZIP symlink rejection, got %v", err)
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
	if err := Extract(context.Background(), zipPath, dest, ".zip", fr, false, ""); err != nil {
		t.Fatalf("unexpected error for safe zip: %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native ZIP extraction invoked subprocesses: %+v", fr.Calls)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool")) // #nosec G304 -- dest is test-controlled under t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Fatalf("extracted content = %q, want content", string(data))
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
	err := Extract(context.Background(), tarPath, dest, ".tar.gz", fr, false, "")
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
	err := Extract(context.Background(), tarPath, dest, ".tar.gz", fr, false, "")
	if err == nil {
		t.Fatal("expected Extract to reject a tar entry with an absolute symlink target")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("expected extraction to be blocked before any subprocess call, got %d calls", len(fr.Calls))
	}
}

func TestExtractRejectsEscapingTarHardlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "evil-hardlink.tar.gz")
	writeTarGzWithEntry(t, tarPath, &tar.Header{
		Name:     "nested/link",
		Typeflag: tar.TypeLink,
		Linkname: "../outside",
		Mode:     0o644,
	}, nil)

	fr := &run.FakeRunner{ExitCode: 0}
	err := Extract(context.Background(), tarPath, filepath.Join(dir, "dest"), ".tar.gz", fr, false, "")
	if err == nil {
		t.Fatal("expected Extract to reject a tar hardlink target outside the destination")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("expected extraction to be blocked before any subprocess call, got %d calls", len(fr.Calls))
	}
}

func TestExtractAllowsRootRelativeTarHardlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "safe-hardlink.tar.gz")
	writeTarGzEntries(t, tarPath, []compressedTarFixtureEntry{
		{header: tar.Header{Name: "bin/tool", Mode: 0o755, Typeflag: tar.TypeReg}, body: []byte("binary")},
		{header: tar.Header{Name: "nested/link", Mode: 0o644, Typeflag: tar.TypeLink, Linkname: "bin/tool"}},
	})

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), tarPath, dest, ".tar.gz", fr, false, ""); err != nil {
		t.Fatalf("unexpected error for root-relative hardlink target: %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native TAR extraction invoked subprocesses: %+v", fr.Calls)
	}
	got, err := os.ReadFile(filepath.Join(dest, "nested", "link")) // #nosec G304 -- dest is test-controlled under t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("hardlink content = %q, want binary", got)
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
	if err := Extract(context.Background(), tarPath, dest, ".tar.gz", fr, false, ""); err != nil {
		t.Fatalf("unexpected error for safe tar.gz: %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native TAR extraction invoked subprocesses: %+v", fr.Calls)
	}
	got, err := os.ReadFile(filepath.Join(dest, "bin", "tool")) // #nosec G304 -- dest is test-controlled under t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("extracted content = %q, want binary", got)
	}
}

// XZ/Zstd decoders are byte producers only; the rooted TAR materializer must
// still reject a destination symlink pivot before any outside write occurs.
func TestExternalTarExtractionRejectsPreexistingSymlinkPivot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires platform symlink privileges")
	}
	payload := plainTarBytes(t, []tarEntry{{
		header: tar.Header{Name: "pivot/escaped", Typeflag: tar.TypeReg, Mode: 0o644},
		body:   []byte("payload"),
	}})
	for _, tt := range []struct {
		ext     string
		decoder string
	}{
		{ext: ".tar.xz", decoder: "xz"},
		{ext: ".tar.zst", decoder: "zstd"},
	} {
		t.Run(tt.ext, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			outside := filepath.Join(root, "outside")
			if err := os.Mkdir(dest, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dest, "pivot")); err != nil {
				t.Skipf("create symlink fixture: %v", err)
			}

			src := filepath.Join(root, "payload"+tt.ext)
			fr := &run.FakeRunner{Stdout: string(payload)}
			err := Extract(context.Background(), src, dest, tt.ext, fr, false, "")
			if err == nil {
				t.Fatal("expected rooted extraction to reject staging symlink pivot")
			}
			if len(fr.Calls) != 1 || fr.Calls[0].Name != tt.decoder {
				t.Fatalf("calls = %+v, want only %s decoder", fr.Calls, tt.decoder)
			}
			if _, statErr := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(statErr) {
				t.Fatalf("outside path was materialized or became unreadable: %v", statErr)
			}
		})
	}
}

func TestElevationGuardNoMethod(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("test requires non-root")
	}

	err := elevationGuard(true, "")
	if run.ElevationPrefix() == nil {
		// No elevation available — guard must report an error.
		if err == nil {
			t.Fatal("expected error when sudo required but no elevation method")
		}
		if !strings.Contains(err.Error(), "elevation method") {
			t.Errorf("error should mention 'elevation method', got: %v", err)
		}
	} else if err != nil {
		// Elevation is available — guard must pass.
		t.Fatalf("expected no error when elevation available, got: %v", err)
	}
}

func TestElevationGuardNotNeeded(t *testing.T) {
	t.Parallel()
	if err := elevationGuard(false, ""); err != nil {
		t.Errorf("expected no error when sudo not required, got: %v", err)
	}
}
