package localartifact

import (
	"archive/tar"
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveExtractionRootRejectsSymlinkEscape(t *testing.T) {
	tests := []struct {
		name    string
		archive string
		write   func(*testing.T, *os.File)
		extract func(*os.File, int64, *os.Root) error
	}{
		{
			name:    "zip",
			archive: "payload-*.zip",
			write: func(t *testing.T, f *os.File) {
				t.Helper()
				zw := zip.NewWriter(f)
				entry, err := zw.Create("pivot/escaped")
				if err != nil {
					t.Fatalf("create zip entry: %v", err)
				}
				if _, err := io.WriteString(entry, "payload"); err != nil {
					t.Fatalf("write zip entry: %v", err)
				}
				if err := zw.Close(); err != nil {
					t.Fatalf("close zip: %v", err)
				}
			},
			extract: func(source *os.File, size int64, root *os.Root) error {
				_, err := extractZip(source, size, root)
				return err
			},
		},
		{
			name:    "tar",
			archive: "payload-*.tar",
			write: func(t *testing.T, f *os.File) {
				t.Helper()
				tw := tar.NewWriter(f)
				body := []byte("payload")
				if err := tw.WriteHeader(&tar.Header{Name: "pivot/escaped", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
					t.Fatalf("write tar header: %v", err)
				}
				if _, err := tw.Write(body); err != nil {
					t.Fatalf("write tar entry: %v", err)
				}
				if err := tw.Close(); err != nil {
					t.Fatalf("close tar: %v", err)
				}
			},
			extract: func(source *os.File, _ int64, root *os.Root) error {
				_, err := extractTar(source, root, false)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outside := t.TempDir()
			stage := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(stage, "pivot")); err != nil {
				t.Skipf("create symlink fixture: %v", err)
			}

			source, err := os.CreateTemp(t.TempDir(), tt.archive)
			if err != nil {
				t.Fatalf("create archive: %v", err)
			}
			t.Cleanup(func() { _ = source.Close() })
			tt.write(t, source)
			if _, err := source.Seek(0, io.SeekStart); err != nil {
				t.Fatalf("rewind archive: %v", err)
			}
			info, err := source.Stat()
			if err != nil {
				t.Fatalf("stat archive: %v", err)
			}
			root, err := os.OpenRoot(stage)
			if err != nil {
				t.Fatalf("open staging root: %v", err)
			}
			t.Cleanup(func() { _ = root.Close() })

			if err := tt.extract(source, info.Size(), root); err == nil {
				t.Fatal("archive extraction followed a staging symlink outside the root")
			}
			if _, err := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(err) {
				t.Fatalf("outside path was materialized or became unreadable: %v", err)
			}
		})
	}
}
