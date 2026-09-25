package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/run"
)

type recordingElevationRunner struct {
	calls []run.FakeCall
}

func (r *recordingElevationRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: append([]string(nil), args...)})
	if name == "sudo" {
		if len(args) == 0 {
			return run.Result{ExitCode: 1}
		}
		name, args = args[0], args[1:]
	}
	return (run.OSExecRunner{}).Run(ctx, name, args...)
}

func TestInstallArchiveElevatesPayloadAndLinkIndependently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX archive and launcher assertion")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can create the system launcher without elevation")
	}
	for _, tc := range []struct {
		name           string
		payloadInHome  bool
		linkInHome     bool
		wantElevatedLn bool
	}{
		{name: "system link with user payload", payloadInHome: true, linkInHome: false, wantElevatedLn: true},
		{name: "user link with system payload", payloadInHome: false, linkInHome: true, wantElevatedLn: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			exectest.SetHome(t, home)
			payloadBase, linkBase := filepath.Join(root, "system"), filepath.Join(root, "system")
			if tc.payloadInHome {
				payloadBase = filepath.Join(home, "tools")
			}
			if tc.linkInHome {
				linkBase = filepath.Join(home, "bin")
			}
			dest, links := filepath.Join(payloadBase, "demo"), linkBase
			archive := filepath.Join(root, "demo.tar.gz")
			writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("binary"))
			mc := &config.MethodCandidate{Config: map[string]any{
				"extract_to":    dest,
				"link_dir":      links,
				"entrypoints":   map[string]any{"demo": "bin/demo"},
				"sudo_required": false,
			}}
			// In the second case, payload elevation is explicitly requested by
			// its system path; the launcher still belongs in the user path.
			if !tc.payloadInHome {
				delete(mc.Config, "sudo_required")
			}
			run.OverrideElevation("sudo")
			defer run.OverrideElevation("")
			rn := &recordingElevationRunner{}
			if err := installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, rn); err != nil {
				t.Fatalf("installArchive() error = %v", err)
			}

			gotElevatedLn := false
			for _, call := range rn.calls {
				if call.Name == "sudo" && len(call.Args) >= 1 && call.Args[0] == "ln" {
					gotElevatedLn = true
				}
			}
			if gotElevatedLn != tc.wantElevatedLn {
				t.Fatalf("elevated ln = %v, want %v; calls: %+v", gotElevatedLn, tc.wantElevatedLn, rn.calls)
			}
		})
	}
}

func TestArchiveRemoveElevatesLinkIndependently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission and symlink assertion")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can remove the protected launcher without elevation")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	lockedParent := filepath.Join(root, "system")
	payload := filepath.Join(home, "tools", "demo")
	links := filepath.Join(lockedParent, "bin")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	exectest.SetHome(t, home)
	owned := filepath.Join(payload, "bin", "demo")
	if err := os.WriteFile(owned, []byte("owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(links, "demo")
	if err := os.Symlink(owned, launcher); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(links, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(links, 0o755)

	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"extract_to":  payload,
		"link_dir":    links,
		"entrypoints": map[string]any{"demo": "bin/demo"},
	}}
	if err := NewHTTPAdapter().Remove(context.Background(), fr, &config.Tool{Name: "demo"}, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(fr.Calls) != 1 || fr.Calls[0].Name != "sudo" || len(fr.Calls[0].Args) < 4 || fr.Calls[0].Args[0] != "rm" || fr.Calls[0].Args[1] != "-f" || fr.Calls[0].Args[3] != launcher {
		t.Fatalf("elevated launcher removal calls = %+v, want sudo rm -f %s", fr.Calls, launcher)
	}
	if _, err := os.Stat(payload); !os.IsNotExist(err) {
		t.Fatalf("user payload remains: %v", err)
	}
}

func TestInstallArchiveStripEntrypointCheckRemove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher assertion")
	}
	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			exectest.SetHome(t, home)
			archive := filepath.Join(root, "nvim."+format)
			writeTestArchive(t, archive, format, "nvim-linux/bin/nvim", []byte("binary"))
			dest, links := filepath.Join(home, "opt", "nvim"), filepath.Join(home, "bin")
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

func TestCopyTreeStrippedRejectsSourceSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix semantics")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "top"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../outside", filepath.Join(src, "top", "link")); err != nil {
		t.Fatal(err)
	}

	err := copyTreeStripped(src, dest, 0)
	if err == nil || !strings.Contains(err.Error(), "escapes staging") {
		t.Fatalf("copyTreeStripped() error = %v, want staging escape rejection", err)
	}
}

func TestCopyTreeStrippedRejectsSymlinkEscapeIntroducedByStrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix semantics")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "top", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "outside"), []byte("inside staging"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Safe before stripping: top/a/../../outside resolves inside src.
	// After stripping top/, a/../../outside would escape the payload root.
	if err := os.Symlink("../../outside", filepath.Join(src, "top", "a", "link")); err != nil {
		t.Fatal(err)
	}

	err := copyTreeStripped(src, dest, 1)
	if err == nil || !strings.Contains(err.Error(), "escapes stripped payload") {
		t.Fatalf("copyTreeStripped() error = %v, want stripped-payload escape rejection", err)
	}
}

func TestCopyTreeStrippedPreservesContainedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix semantics")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "top", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "top", "bin", "demo"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("demo", filepath.Join(src, "top", "bin", "current")); err != nil {
		t.Fatal(err)
	}

	if err := copyTreeStripped(src, dest, 1); err != nil {
		t.Fatalf("copyTreeStripped() error = %v", err)
	}
	link, err := os.Readlink(filepath.Join(dest, "bin", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "demo" {
		t.Fatalf("copied symlink target = %q, want demo", link)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary" {
		t.Fatalf("copied file = %q, want binary", string(data))
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
