package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestSafeArchiveRelative(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "simple", input: "bin/tool", want: filepath.Join("bin", "tool")},
		{name: "dot prefix", input: "./bin/tool", want: filepath.Join("bin", "tool")},
		{name: "lexical parent inside root", input: "share/../bin/tool", want: filepath.Join("bin", "tool")},
		{name: "absolute", input: "/etc/passwd", wantErr: true},
		{name: "parent escape", input: "../../outside", wantErr: true},
		{name: "windows drive", input: "C:/Windows/System32", wantErr: true},
		{name: "backslash", input: `bin\tool`, wantErr: true},
		{name: "nul", input: "bin/\x00tool", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeArchiveRelative(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("safeArchiveRelative(%q) = %q, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeArchiveRelative(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("safeArchiveRelative(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func writePlainTar(t *testing.T, archivePath string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, entry := range entries {
		hdr := entry.header
		hdr.Size = int64(len(entry.body))
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if len(entry.body) > 0 {
			if _, err := tw.Write(entry.body); err != nil {
				_ = f.Close()
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

type tarEntry struct {
	header tar.Header
	body   []byte
}

func TestExtractTarNative(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.tar")
	writePlainTar(t, src, []tarEntry{
		{header: tar.Header{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o755}},
		{header: tar.Header{Name: "bin/tool", Typeflag: tar.TypeReg, Mode: 0o755}, body: []byte("binary")},
	})

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), src, dest, ".tar", fr, false, ""); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native TAR extraction invoked subprocesses: %+v", fr.Calls)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary" {
		t.Fatalf("extracted content = %q, want binary", string(data))
	}
	info, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("extracted mode = %o, want 755", info.Mode().Perm())
	}
}

func TestNativeArchiveExtractionRejectsPreexistingSymlinkPivot(t *testing.T) {
	tests := []struct {
		name  string
		ext   string
		write func(*testing.T, string)
	}{
		{
			name: "zip",
			ext:  ".zip",
			write: func(t *testing.T, archivePath string) {
				t.Helper()
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				zw := zip.NewWriter(f)
				w, err := zw.Create("pivot/escaped")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(w, "payload"); err != nil {
					t.Fatal(err)
				}
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "tar",
			ext:  ".tar",
			write: func(t *testing.T, archivePath string) {
				t.Helper()
				writePlainTar(t, archivePath, []tarEntry{{
					header: tar.Header{Name: "pivot/escaped", Typeflag: tar.TypeReg, Mode: 0o644},
					body:   []byte("payload"),
				}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			outside := filepath.Join(root, "outside")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dest, "pivot")); err != nil {
				t.Skipf("create symlink fixture: %v", err)
			}
			src := filepath.Join(root, "payload"+tt.ext)
			tt.write(t, src)

			fr := &run.FakeRunner{ExitCode: 0}
			err := Extract(context.Background(), src, dest, tt.ext, fr, false, "")
			if err == nil {
				t.Fatal("expected rooted extraction to reject staging symlink pivot")
			}
			if len(fr.Calls) != 0 {
				t.Fatalf("native extraction invoked subprocesses: %+v", fr.Calls)
			}
			if _, statErr := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(statErr) {
				t.Fatalf("outside path was materialized or became unreadable: %v", statErr)
			}
		})
	}
}

func TestExtractTarNativeRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "evil.tar")
	writePlainTar(t, src, []tarEntry{{
		header: tar.Header{
			Name:     "nested/link",
			Typeflag: tar.TypeSymlink,
			Linkname: "../../outside",
			Mode:     0o777,
		},
	}})

	fr := &run.FakeRunner{ExitCode: 0}
	err := Extract(context.Background(), src, filepath.Join(dir, "dest"), ".tar", fr, false, "")
	if err == nil || !strings.Contains(err.Error(), "link target") {
		t.Fatalf("expected escaping symlink rejection, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native TAR extraction invoked subprocesses: %+v", fr.Calls)
	}
}

func TestExtractTarNativePreservesContainedLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires platform symlink privileges")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "links.tar")
	writePlainTar(t, src, []tarEntry{
		{
			header: tar.Header{Name: "bin/tool", Typeflag: tar.TypeReg, Mode: 0o755},
			body:   []byte("binary"),
		},
		{
			header: tar.Header{Name: "bin/current", Typeflag: tar.TypeSymlink, Linkname: "tool", Mode: 0o777},
		},
		{
			header: tar.Header{Name: "bin/copy", Typeflag: tar.TypeLink, Linkname: "bin/tool", Mode: 0o755},
		},
	})

	fr := &run.FakeRunner{ExitCode: 0}
	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), src, dest, ".tar", fr, false, ""); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("native TAR extraction invoked subprocesses: %+v", fr.Calls)
	}
	target, err := os.Readlink(filepath.Join(dest, "bin", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "tool" {
		t.Fatalf("symlink target = %q, want tool", target)
	}
	original, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	copyInfo, err := os.Stat(filepath.Join(dest, "bin", "copy"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(original, copyInfo) {
		t.Fatal("hardlink does not reference the extracted regular file")
	}
}

func TestNativeArchiveDirectoryModeAppliedAfterChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permission assertion")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "mode.tar")
	writePlainTar(t, src, []tarEntry{
		{header: tar.Header{Name: "locked/", Typeflag: tar.TypeDir, Mode: 0o555}},
		{header: tar.Header{Name: "locked/tool", Typeflag: tar.TypeReg, Mode: 0o555}, body: []byte("binary")},
	})

	dest := filepath.Join(dir, "dest")
	if err := Extract(context.Background(), src, dest, ".tar", &run.FakeRunner{}, false, ""); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "locked"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o555 {
		t.Fatalf("directory mode = %o, want 555", info.Mode().Perm())
	}
	if _, err := os.ReadFile(filepath.Join(dest, "locked", "tool")); err != nil {
		t.Fatalf("child file was not materialized before final directory chmod: %v", err)
	}
}
