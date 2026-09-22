package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exectest"
	"github.com/Khorea1/depengine/pkg/run"
)

func appImageTool(name string) *config.Tool { return &config.Tool{Name: name} }

func TestAppImageAdapterKind(t *testing.T) {
	if NewAppImageAdapter().Kind() != "appimage" {
		t.Fatalf("Kind() = %q, want %q", NewAppImageAdapter().Kind(), "appimage")
	}
}

func TestAppImageAdapterAvailable(t *testing.T) {
	if !NewAppImageAdapter().Available(context.Background(), &run.FakeRunner{}) {
		t.Fatal("Available should always be true (net/http is always available)")
	}
}

// --- binaryTarget / httpDelegate (pure helpers) ---

func TestBinaryTargetDefaults(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{}}
	dir, name := binaryTarget(appImageTool("obsidian"), mc)
	wantDir := config.ExpandHomeDir("~/.local/bin")
	if dir != wantDir {
		t.Errorf("install dir = %q, want %q", dir, wantDir)
	}
	if name != "obsidian" {
		t.Errorf("name = %q, want tool name %q", name, "obsidian")
	}
}

func TestBinaryTargetExplicitOverrides(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{
		"install_dir": "/opt/bin",
		"binary":      "obsidian-app",
	}}
	dir, name := binaryTarget(appImageTool("obsidian"), mc)
	if dir != "/opt/bin" {
		t.Errorf("install dir = %q, want %q", dir, "/opt/bin")
	}
	if name != "obsidian-app" {
		t.Errorf("name = %q, want %q", name, "obsidian-app")
	}
}

func TestHTTPDelegatePreservesOtherConfigKeys(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":      "https://example.com/App.AppImage",
		"checksum": "sha256:deadbeef",
	}}
	d := httpDelegate(mc, "/opt/bin", "obsidian")
	if d.Kind != "http" {
		t.Errorf("delegate Kind = %q, want %q", d.Kind, "http")
	}
	if d.Config["extract_to"] != "/opt/bin" {
		t.Errorf("delegate extract_to = %v, want /opt/bin", d.Config["extract_to"])
	}
	if d.Config["binary"] != "obsidian" {
		t.Errorf("delegate binary = %v, want obsidian", d.Config["binary"])
	}
	if d.Config["checksum"] != "sha256:deadbeef" {
		t.Errorf("delegate lost unrelated config key checksum: %v", d.Config["checksum"])
	}
	// Original mc must be untouched — httpDelegate must copy, not mutate.
	if _, ok := mc.Config["extract_to"]; ok {
		t.Error("httpDelegate mutated the original mc.Config")
	}
}

// --- Check ---

func TestAppImageAdapterCheckInstalled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "obsidian"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{"install_dir": dir}}
	fr := &run.FakeRunner{}
	if !NewAppImageAdapter().Check(context.Background(), fr, appImageTool("obsidian"), mc) {
		t.Fatal("Check should be true when the resolved binary exists in install_dir")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("filesystem check executed commands: %+v", fr.Calls)
	}
}

func TestAppImageAdapterCheckNotInstalled(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{"install_dir": t.TempDir()}}
	fr := &run.FakeRunner{}
	if NewAppImageAdapter().Check(context.Background(), fr, appImageTool("obsidian"), mc) {
		t.Fatal("Check should be false when the binary doesn't exist yet")
	}
}

// --- Install: error paths ---

func TestAppImageAdapterInstallNoURL(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{}}
	err := NewAppImageAdapter().Install(context.Background(), &run.FakeRunner{}, appImageTool("obsidian"), mc)
	if err == nil {
		t.Fatal("expected error when url is missing")
	}
}

// --- Install: full happy path against a local test server ---

// HTTPAdapter must install the versioned release asset directly under the
// stable AppImage binary name. FakeRunner forces the Go downloader.
func TestAppImageAdapterInstallsStableName(t *testing.T) {
	const body = "fake-appimage-bytes"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer ts.Close()

	installDir := t.TempDir()
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":           ts.URL + "/Obsidian-1.5.3.AppImage",
		"install_dir":   installDir,
		"sudo_required": false,
	}}
	tool := appImageTool("obsidian")
	fr := &run.FakeRunner{ExitCode: 1} // no curl/wget "available" -> GoDownloader

	a := NewAppImageAdapter()
	if err := a.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	stablePath := filepath.Join(installDir, "obsidian")
	data, err := os.ReadFile(stablePath)
	if err != nil {
		t.Fatalf("expected stable-named binary at %s: %v", stablePath, err)
	}
	if string(data) != body {
		t.Fatalf("installed content = %q, want %q", data, body)
	}

	versionedPath := filepath.Join(installDir, "Obsidian-1.5.3.AppImage")
	if _, err := os.Stat(versionedPath); !os.IsNotExist(err) {
		t.Fatalf("expected versioned filename to be renamed away, but it still exists at %s", versionedPath)
	}

	if !a.Check(context.Background(), &run.FakeRunner{ExitCode: 0}, tool, mc) {
		t.Error("Check should report installed after a successful Install")
	}
}

func TestAppImageAdapterInstallSameName(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content"))
	}))
	defer ts.Close()

	installDir := t.TempDir()
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":           ts.URL + "/obsidian.AppImage",
		"install_dir":   installDir,
		"sudo_required": false,
	}}
	fr := &run.FakeRunner{ExitCode: 1}

	if err := NewAppImageAdapter().Install(context.Background(), fr, appImageTool("obsidian"), mc); err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "obsidian")); err != nil {
		t.Fatalf("expected binary at install_dir/obsidian: %v", err)
	}
}

// TestAppImageAdapterInstallWithDesktopEntry covers the optional .desktop
// launcher. $HOME is redirected to a tempdir so this never touches the
// real machine's XDG applications directory.
func TestAppImageAdapterInstallWithDesktopEntry(t *testing.T) {
	fakeHome := t.TempDir()
	exectest.SetHome(t, fakeHome)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content"))
	}))
	defer ts.Close()

	installDir := t.TempDir()
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":           ts.URL + "/Obsidian-2.0.0.AppImage",
		"install_dir":   installDir,
		"sudo_required": false,
		"desktop":       true,
	}}
	fr := &run.FakeRunner{ExitCode: 1}

	if err := NewAppImageAdapter().Install(context.Background(), fr, appImageTool("obsidian"), mc); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	desktopPath := filepath.Join(fakeHome, ".local", "share", "applications", "obsidian.desktop")
	data, err := os.ReadFile(desktopPath)
	if err != nil {
		t.Fatalf("expected .desktop entry at %s: %v", desktopPath, err)
	}
	content := string(data)
	execLine := "Exec=" + filepath.Join(installDir, "obsidian")
	if !strings.Contains(content, execLine) {
		t.Errorf(".desktop content missing %q:\n%s", execLine, content)
	}
	if !strings.Contains(content, "Name=obsidian") {
		t.Errorf(".desktop content missing Name= line:\n%s", content)
	}
}

func TestAppImageAdapterInstallWithoutDesktopSkipsEntry(t *testing.T) {
	fakeHome := t.TempDir()
	exectest.SetHome(t, fakeHome)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content"))
	}))
	defer ts.Close()

	mc := &config.MethodCandidate{Config: map[string]any{
		"url":           ts.URL + "/App.AppImage",
		"install_dir":   t.TempDir(),
		"sudo_required": false,
	}}
	fr := &run.FakeRunner{ExitCode: 1}

	if err := NewAppImageAdapter().Install(context.Background(), fr, appImageTool("app"), mc); err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	desktopPath := filepath.Join(fakeHome, ".local", "share", "applications", "app.desktop")
	if _, err := os.Stat(desktopPath); !os.IsNotExist(err) {
		t.Fatal("expected no .desktop entry when desktop is not set")
	}
}

// --- Remove ---

func TestAppImageAdapterCanRemove(t *testing.T) {
	if !NewAppImageAdapter().CanRemove() {
		t.Fatal("CanRemove should be true")
	}
}

func TestAppImageAdapterRemoveBinaryAndDesktopEntry(t *testing.T) {
	fakeHome := t.TempDir()
	exectest.SetHome(t, fakeHome)

	installDir := t.TempDir()
	binPath := filepath.Join(installDir, "obsidian")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	desktopDir := filepath.Join(fakeHome, ".local", "share", "applications")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	desktopPath := filepath.Join(desktopDir, "obsidian.desktop")
	if err := os.WriteFile(desktopPath, []byte("[Desktop Entry]"), 0o644); err != nil {
		t.Fatal(err)
	}

	mc := &config.MethodCandidate{Config: map[string]any{
		"install_dir": installDir,
		"desktop":     true,
	}}
	if err := NewAppImageAdapter().Remove(context.Background(), &run.FakeRunner{}, appImageTool("obsidian"), mc); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Error("expected binary to be removed")
	}
	if _, err := os.Stat(desktopPath); !os.IsNotExist(err) {
		t.Error("expected .desktop entry to be removed")
	}
}

func TestAppImageAdapterRemoveWithoutDesktopLeavesNothingToDelete(t *testing.T) {
	installDir := t.TempDir()
	binPath := filepath.Join(installDir, "obsidian")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{"install_dir": installDir}}
	if err := NewAppImageAdapter().Remove(context.Background(), &run.FakeRunner{}, appImageTool("obsidian"), mc); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Error("expected binary to be removed")
	}
}

func TestAppImageInstallRejectsNonAppImageBeforeDownload(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.com/tool.tar.gz"}}
	err := NewAppImageAdapter().Install(context.Background(), fr, appImageTool("tool"), mc)
	if err == nil || !strings.Contains(err.Error(), ".AppImage") {
		t.Fatalf("Install() error = %v, want .AppImage requirement", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("invalid artifact triggered subprocesses: %#v", fr.Calls)
	}
}
