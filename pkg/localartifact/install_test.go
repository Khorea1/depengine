package localartifact_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/localartifact"
	"github.com/Khorea1/depengine/pkg/plan"
)

func TestInstallRawOfflineAndAtomicReplacement(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "vendor", "tool")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "vendor/tool", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("installed content = %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("installed mode = %o, want executable", info.Mode().Perm())
		}
	}
}

func TestInstallZipRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("bad"))
	_ = zw.Close()
	_ = f.Close()
	resolved, err := localartifact.Resolve(root, "bad.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	destRoot := t.TempDir()
	dest := filepath.Join(destRoot, "payload")
	if err := localartifact.Install(resolved, dest); err == nil {
		t.Fatal("archive traversal unexpectedly installed")
	}
	if _, err := os.Stat(filepath.Join(destRoot, "escape")); !os.IsNotExist(err) {
		t.Fatalf("escape path exists or stat failed unexpectedly: %v", err)
	}
}

func TestInstallTarGzExtractsRegularFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("binary")
	if err := tw.WriteHeader(&tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.tar.gz", "")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "payload")
	if err := localartifact.Install(resolved, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("content = %q", got)
	}
}

func TestInstallDefensivelyRejectsUnsupportedOfflineArchive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.tar.xz")
	body := []byte("not actually xz")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	resolved := localartifact.Resolved{
		Artifact: plan.Artifact{
			Kind:      plan.ArtifactArchive,
			LocalPath: "tool.tar.xz",
			Checksum:  "sha256:" + hex.EncodeToString(sum[:]),
		},
		Path: path,
		Size: int64(len(body)),
	}
	if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil {
		t.Fatal("tar.xz unexpectedly installed without extraction backend")
	}
}

func TestInstallRejectsArtifactChangedAfterResolve(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool")
	if err := os.WriteFile(path, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err == nil {
		t.Fatal("Install() accepted artifact changed after resolution")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists after checksum rejection: %v", err)
	}
}

func TestInstallPreservesUnrelatedLegacyBackupPath(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "tool")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyBackup := destination + ".depengine-backup"
	if err := os.WriteFile(legacyBackup, []byte("owned by user"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "owned by user" {
		t.Fatalf("legacy backup path changed: %q", got)
	}
}

func TestInstallArchiveWritesAndVerifiesContentIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("bin/tool")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	resolved, err := localartifact.Resolve(root, "tool.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyArchiveChecksum(destination, resolved.Artifact.Checksum); err != nil {
		t.Fatalf("VerifyArchiveChecksum() after install: %v", err)
	}
	wrong := "sha256:" + strings.Repeat("0", 64)
	if err := localartifact.VerifyArchiveChecksum(destination, wrong); err == nil {
		t.Fatal("VerifyArchiveChecksum() accepted wrong content identity")
	}
}

func TestInstallArchiveRejectsReservedMetadataEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create(".depengine-local-artifact.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("user payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "tool")); err == nil {
		t.Fatal("archive containing reserved metadata entry unexpectedly installed")
	}
}

func TestInstallZipRejectsWindowsDriveQualifiedEntriesOnEveryHost(t *testing.T) {
	for _, entryName := range []string{"C:/escape", "C:escape", `D:\\escape`} {
		t.Run(strings.ReplaceAll(entryName, "/", "_"), func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, "bad.zip")
			f, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			w, err := zw.Create(entryName)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte("bad")); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			resolved, err := localartifact.Resolve(root, "bad.zip", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil {
				t.Fatalf("drive-qualified archive entry %q unexpectedly installed", entryName)
			}
		})
	}
}

func TestInstallArchivePreservesExplicitDirectoryModeAfterChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits are not stable on Windows")
	}
	tests := []struct {
		name  string
		file  string
		write func(*testing.T, string)
	}{
		{
			name: "zip",
			file: "tool.zip",
			write: func(t *testing.T, path string) {
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				zw := zip.NewWriter(f)
				w, err := zw.Create("bin/tool")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
				h := &zip.FileHeader{Name: "bin/", Method: zip.Store}
				h.SetMode(os.ModeDir | 0o700)
				if _, err := zw.CreateHeader(h); err != nil {
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
			file: "tool.tar",
			write: func(t *testing.T, path string) {
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				tw := tar.NewWriter(f)
				body := []byte("x")
				if err := tw.WriteHeader(&tar.Header{Name: "bin/tool", Mode: 0o644, Size: int64(len(body))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(body); err != nil {
					t.Fatal(err)
				}
				if err := tw.WriteHeader(&tar.Header{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o700}); err != nil {
					t.Fatal(err)
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tt.file)
			tt.write(t, path)
			resolved, err := localartifact.Resolve(root, tt.file, "")
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "payload")
			if err := localartifact.Install(resolved, dest); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(filepath.Join(dest, "bin"))
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o700 {
				t.Fatalf("directory mode = %o, want 700", got)
			}
		})
	}
}

func TestInstallZipRejectsWindowsSpecialEntriesOnEveryHost(t *testing.T) {
	for _, entryName := range []string{"NUL", "con.txt", "dir/COM1.exe", "dir/name:stream", "dir/file.", "dir/file ", `dir\file`, "dir/file?name", "dir/file*name", "dir/file|name", "dir/file<name", "dir/file>name", `dir/file"name`, "dir/file\nname"} {
		t.Run(strings.NewReplacer("/", "_", ":", "_").Replace(entryName), func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, "bad.zip")
			f, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			w, err := zw.Create(entryName)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte("bad")); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			resolved, err := localartifact.Resolve(root, "bad.zip", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil {
				t.Fatalf("Windows-special archive entry %q unexpectedly installed", entryName)
			}
		})
	}
}

func TestInstallArchiveRejectsPortablePathAliases(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		write func(*testing.T, string)
	}{
		{
			name: "zip case collision",
			file: "bad.zip",
			write: func(t *testing.T, archivePath string) {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				zw := zip.NewWriter(f)
				for _, name := range []string{"Bin/tool", "bin/other"} {
					w, err := zw.Create(name)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := w.Write([]byte(name)); err != nil {
						t.Fatal(err)
					}
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
			name: "zip empty component alias",
			file: "bad.zip",
			write: func(t *testing.T, archivePath string) {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				zw := zip.NewWriter(f)
				w, err := zw.Create("bin//tool")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte("x")); err != nil {
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
			name: "zip dot component alias",
			file: "bad.zip",
			write: func(t *testing.T, archivePath string) {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				zw := zip.NewWriter(f)
				w, err := zw.Create("bin/./tool")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte("x")); err != nil {
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
			name: "tar file parent collision",
			file: "bad.tar",
			write: func(t *testing.T, archivePath string) {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				tw := tar.NewWriter(f)
				for _, entry := range []struct {
					name string
					body string
				}{
					{name: "bin/tool", body: "tool"},
					{name: "BIN", body: "file"},
				} {
					if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body))}); err != nil {
						t.Fatal(err)
					}
					if _, err := tw.Write([]byte(entry.body)); err != nil {
						t.Fatal(err)
					}
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "zip parent component",
			file: "bad.zip",
			write: func(t *testing.T, archivePath string) {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				zw := zip.NewWriter(f)
				w, err := zw.Create("bin/../tool")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte("tool")); err != nil {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, tt.file)
			tt.write(t, archivePath)
			resolved, err := localartifact.Resolve(root, tt.file, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil {
				t.Fatal("portable archive path alias unexpectedly installed")
			}
		})
	}
}

func TestInstallArchiveRejectsReservedMetadataNamesCaseInsensitively(t *testing.T) {
	for _, name := range []string{
		".DEPENGINE-LOCAL-ARTIFACT.SHA256",
		".DepEngine-Local-Artifact-Tree.SHA256",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, "bad.zip")
			f, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte("forged")); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}

			resolved, err := localartifact.Resolve(root, "bad.zip", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil {
				t.Fatalf("reserved metadata alias %q unexpectedly installed", name)
			}
		})
	}
}

func TestVerifyArchiveChecksumDetectsInstalledPayloadDrift(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "tool.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("bin/tool")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	resolved, err := localartifact.Resolve(root, "tool.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyArchiveChecksum(destination, resolved.Artifact.Checksum); err != nil {
		t.Fatalf("initial VerifyArchiveChecksum() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "bin", "tool"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyArchiveChecksum(destination, resolved.Artifact.Checksum); err == nil {
		t.Fatal("VerifyArchiveChecksum() accepted modified installed payload")
	}
}

func TestInstallArchiveRejectsReservedTreeMetadataEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create(".depengine-local-artifact-tree.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("user payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "tool")); err == nil {
		t.Fatal("archive containing reserved tree metadata entry unexpectedly installed")
	}
}

func TestInstallRawAlreadySatisfiedDoesNotReplaceDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "tool")
	if err := os.WriteFile(source, []byte("stable"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("second install replaced an already satisfied raw artifact")
	}
}

func TestInstallArchiveAlreadySatisfiedDoesNotReplaceDestination(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "tool.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("bin/tool")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("stable")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "payload")
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("second install replaced an already satisfied archive artifact")
	}
}

func TestInstallRawRepairsPermissionDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not stable desired-state identity on Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "tool")
	if err := os.WriteFile(source, []byte("stable"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(destination, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != resolved.Mode.Perm() {
		t.Fatalf("mode after repair = %o, want %o", info.Mode().Perm(), resolved.Mode.Perm())
	}
}

func TestInstallArchiveNormalizesAndVerifiesRootMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits are not stable on Windows")
	}
	root := t.TempDir()
	archivePath := filepath.Join(root, "tool.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("bin/tool")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	resolved, err := localartifact.Resolve(root, "tool.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "payload")
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("archive root mode = %o, want 755", got)
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyArchiveChecksum(destination, resolved.Artifact.Checksum); err == nil {
		t.Fatal("VerifyArchiveChecksum() accepted archive root permission drift")
	}
}

func TestInstallRejectsRawSourcePermissionDriftAfterResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not stable desired-state identity on Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "tool")
	if err := os.WriteFile(source, []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err == nil || !strings.Contains(err.Error(), "source mode changed") {
		t.Fatalf("Install() error = %v, want source permission drift rejection", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination created after permission drift rejection: %v", err)
	}
}

func TestInstallTarAllowsConventionalRootDirectoryEntry(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "tool.tar")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	if err := tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	body := []byte("ok")
	if err := tw.WriteHeader(&tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	resolved, err := localartifact.Resolve(root, "tool.tar", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "payload")
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "bin", "tool")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallArchiveDefersRestrictiveDirectoryModesUntilAfterExtraction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits are not stable on Windows")
	}
	tests := []struct {
		name  string
		file  string
		write func(*testing.T, string)
	}{
		{
			name: "zip",
			file: "tool.zip",
			write: func(t *testing.T, archivePath string) {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				zw := zip.NewWriter(f)
				h := &zip.FileHeader{Name: "bin/", Method: zip.Store}
				h.SetMode(os.ModeDir | 0o555)
				if _, err := zw.CreateHeader(h); err != nil {
					t.Fatal(err)
				}
				w, err := zw.Create("bin/tool")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte("payload")); err != nil {
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
			file: "tool.tar",
			write: func(t *testing.T, archivePath string) {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				tw := tar.NewWriter(f)
				if err := tw.WriteHeader(&tar.Header{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o555}); err != nil {
					t.Fatal(err)
				}
				body := []byte("payload")
				if err := tw.WriteHeader(&tar.Header{Name: "bin/tool", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(body); err != nil {
					t.Fatal(err)
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, tt.file)
			tt.write(t, archivePath)
			resolved, err := localartifact.Resolve(root, tt.file, "")
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "payload")
			// Ensure writable so t.TempDir cleanup can remove tree.
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(dest, "bin"), 0o755) })
			if err := localartifact.Install(resolved, dest); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "payload" {
				t.Fatalf("payload = %q", data)
			}
			info, err := os.Stat(filepath.Join(dest, "bin"))
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o555 {
				t.Fatalf("directory mode = %o, want 555", got)
			}
			if err := localartifact.VerifyArchiveChecksum(dest, resolved.Artifact.Checksum); err != nil {
				t.Fatalf("verify installed archive: %v", err)
			}
		})
	}
}

func TestInstallArchiveRejectsUnverifiableDirectoryMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission bits are not stable on Windows")
	}
	root := t.TempDir()
	archivePath := filepath.Join(root, "tool.tar")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	if err := tw.WriteHeader(&tar.Header{Name: "private/", Typeflag: tar.TypeDir, Mode: 0o300}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.tar", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil || !strings.Contains(err.Error(), "owner read+execute") {
		t.Fatalf("Install() error = %v, want unverifiable directory mode rejection", err)
	}
}

func TestInstallArchiveRejectsUnverifiableFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not stable on Windows")
	}
	root := t.TempDir()
	archivePath := filepath.Join(root, "tool.tar")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	body := []byte("payload")
	if err := tw.WriteHeader(&tar.Header{Name: "tool", Typeflag: tar.TypeReg, Mode: 0o200, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.tar", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil || !strings.Contains(err.Error(), "owner read permission") {
		t.Fatalf("Install() error = %v, want unverifiable file mode rejection", err)
	}
}
