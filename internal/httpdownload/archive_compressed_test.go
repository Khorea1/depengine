package httpdownload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

const (
	compressedTarBzip2Fixture      = "QlpoOTFBWSZTWUTrsBQAAG77gMmAAAhAAOaAAEB0Jd4gCAggAFRCBMCMAE2gkkQA0A0xGlesRyEHNiEIhJrzbLC5AhgYjB4nsI4cgPNc3Hirwcb1XGVc3lDCWRcJPwiIB0XckU4UJBE67AUA"
	compressedTarBzip2PivotFixture = "QlpoOTFBWSZTWQpBCv8AAHT7gMmAAAhAAP2ACABuJN8gCAggAFQ0U0aDTR6gD1GnlBJIhpoaAAAfZRHIQOehCK+LYF9UYIEMDHUniewjFyCQ0wFWtiGF5l56qw7NqM8aHc85M8UIiA/F3JFOFCQCkEK/wA=="
)

type compressedTarFixtureEntry struct {
	header tar.Header
	body   []byte
}

func TestExtractCompressedTarNative(t *testing.T) {
	t.Parallel()

	plain := makeCompressedTarFixture(t, []compressedTarFixtureEntry{{
		header: tar.Header{Name: "bin/tool", Mode: 0o755, Typeflag: tar.TypeReg},
		body:   []byte("payload"),
	}})
	gzipPayload := gzipCompressedTarFixture(t, plain)
	bzip2Payload, err := base64.StdEncoding.DecodeString(compressedTarBzip2Fixture)
	if err != nil {
		t.Fatalf("decode bzip2 fixture: %v", err)
	}

	tests := []struct {
		name    string
		ext     string
		payload []byte
	}{
		{name: "tar gzip", ext: ".tar.gz", payload: gzipPayload},
		{name: "tgz", ext: ".tgz", payload: gzipPayload},
		{name: "tar bzip2", ext: ".tar.bz2", payload: bzip2Payload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "payload"+tt.ext)
			if err := os.WriteFile(src, tt.payload, 0o600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(dir, "dest")
			fr := &run.FakeRunner{ExitCode: 0}

			if err := Extract(context.Background(), src, dest, tt.ext, fr, false, ""); err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if len(fr.Calls) != 0 {
				t.Fatalf("native compressed TAR extraction invoked subprocesses: %+v", fr.Calls)
			}
			got, err := os.ReadFile(filepath.Join(dest, "bin", "tool")) // #nosec G304 -- dest is test-controlled under t.TempDir.
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "payload" {
				t.Fatalf("extracted content = %q, want payload", got)
			}
		})
	}
}

func TestExtractCompressedTarRejectsSymlinkPivot(t *testing.T) {
	t.Parallel()

	plain := makeCompressedTarFixture(t, []compressedTarFixtureEntry{{
		header: tar.Header{Name: "pivot/escaped", Mode: 0o644, Typeflag: tar.TypeReg},
		body:   []byte("payload"),
	}})
	gzipPivot := gzipCompressedTarFixture(t, plain)
	bzip2Pivot, err := base64.StdEncoding.DecodeString(compressedTarBzip2PivotFixture)
	if err != nil {
		t.Fatalf("decode bzip2 pivot fixture: %v", err)
	}

	for _, tt := range []struct {
		name    string
		ext     string
		payload []byte
	}{
		{name: "gzip", ext: ".tar.gz", payload: gzipPivot},
		{name: "bzip2", ext: ".tar.bz2", payload: bzip2Pivot},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "payload"+tt.ext)
			if err := os.WriteFile(src, tt.payload, 0o600); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			dest := filepath.Join(dir, "dest")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dest, "pivot")); err != nil {
				t.Skipf("create symlink fixture: %v", err)
			}
			fr := &run.FakeRunner{ExitCode: 0}

			if err := Extract(context.Background(), src, dest, tt.ext, fr, false, ""); err == nil {
				t.Fatal("expected rooted extraction to reject staging symlink escape")
			}
			if len(fr.Calls) != 0 {
				t.Fatalf("rejected compressed TAR invoked subprocesses: %+v", fr.Calls)
			}
			if _, err := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(err) {
				t.Fatalf("outside path was materialized or became unreadable: %v", err)
			}
		})
	}
}

func TestExtractCompressedTarRejectsCorruptGzip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "broken.tar.gz")
	if err := os.WriteFile(src, []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	fr := &run.FakeRunner{ExitCode: 0}

	if err := Extract(context.Background(), src, filepath.Join(dir, "dest"), ".tar.gz", fr, false, ""); err == nil {
		t.Fatal("expected corrupt gzip archive error")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("corrupt native gzip TAR should not invoke subprocesses: %+v", fr.Calls)
	}
}

func writeTarGzEntries(t *testing.T, archivePath string, entries []compressedTarFixtureEntry) {
	t.Helper()
	plain := makeCompressedTarFixture(t, entries)
	if err := os.WriteFile(archivePath, gzipCompressedTarFixture(t, plain), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeCompressedTarFixture(t *testing.T, entries []compressedTarFixtureEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, entry := range entries {
		hdr := entry.header
		hdr.Size = int64(len(entry.body))
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("write tar header %q: %v", hdr.Name, err)
		}
		if len(entry.body) > 0 {
			if _, err := tw.Write(entry.body); err != nil {
				t.Fatalf("write tar body %q: %v", hdr.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	return buf.Bytes()
}

func gzipCompressedTarFixture(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(plain); err != nil {
		t.Fatalf("write gzip payload: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}
