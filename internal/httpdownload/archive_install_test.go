package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func TestInstallArchiveStripEntrypointCheckRemove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher assertion")
	}
	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "nvim."+format)
			writeTestArchive(t, archive, format, "nvim-linux/bin/nvim", []byte("binary"))
			dest, links := filepath.Join(root, "opt", "nvim"), filepath.Join(root, "bin")
			mc := &config.MethodCandidate{Config: map[string]any{"extract_to": dest, "strip_components": int64(1), "entrypoints": map[string]any{"nvim": "bin/nvim"}, "link_dir": links}}
			mc.Config["sudo_required"] = false
			tool := &config.Tool{Name: "nvim"}
			ext := "." + format
			if err := installArchive(context.Background(), archive, ext, tool, mc, run.OSExecRunner{}); err != nil {
				t.Fatal(err)
			}
			adapter := NewHTTPAdapter()
			if !adapter.Check(context.Background(), run.OSExecRunner{}, tool, mc) {
				t.Fatal("check failed after install")
			}
			if err := adapter.Remove(context.Background(), run.OSExecRunner{}, tool, mc); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatalf("payload remains: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(links, "nvim")); !os.IsNotExist(err) {
				t.Fatalf("launcher remains: %v", err)
			}
		})
	}
}

func writeTestArchive(t *testing.T, path, format, name string, body []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if format == "zip" {
		w := zip.NewWriter(f)
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPRejectsInstallerArtifacts(t *testing.T) {
	tool := &config.Tool{Name: "nvim"}
	mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.invalid/nvim.msi"}}
	err := NewHTTPAdapter().Install(context.Background(), &run.FakeRunner{}, tool, mc)
	if err == nil {
		t.Fatal("expected installer rejection")
	}
}

func TestArchiveEntrypointRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requirePayloadFile(payload, "../outside"); err == nil {
		t.Fatal("expected traversal entrypoint to be rejected")
	}
	if err := requirePayloadFile(payload, outside); err == nil {
		t.Fatal("expected absolute entrypoint to be rejected")
	}
}

func TestArchiveRemoveRefusesRedirectedLauncher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink assertion")
	}
	root := t.TempDir()
	payload := filepath.Join(root, "opt", "nvim")
	links := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(payload, "bin", "nvim")
	if err := os.WriteFile(owned, []byte("owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(links, "nvim")
	if err := os.Symlink(foreign, launcher); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"extract_to":  payload,
		"entrypoints": map[string]any{"nvim": "bin/nvim"},
		"link_dir":    links,
	}}
	err := NewHTTPAdapter().Remove(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "nvim"}, mc)
	if err == nil {
		t.Fatal("expected redirected launcher removal to be refused")
	}
	if _, statErr := os.Stat(payload); statErr != nil {
		t.Fatalf("owned payload was removed after refusal: %v", statErr)
	}
	actual, readErr := os.Readlink(launcher)
	if readErr != nil || actual != foreign {
		t.Fatalf("foreign launcher changed: target=%q err=%v", actual, readErr)
	}
}

func TestHTTPRejectsAllPlatformInstallerArtifacts(t *testing.T) {
	for _, ext := range []string{".msi", ".exe", ".pkg", ".dmg", ".msix", ".appx"} {
		t.Run(ext, func(t *testing.T) {
			mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.invalid/tool" + ext}}
			err := NewHTTPAdapter().Install(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc)
			if err == nil {
				t.Fatalf("expected %s rejection", ext)
			}
		})
	}
}
