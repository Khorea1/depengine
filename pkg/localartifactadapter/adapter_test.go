package localartifactadapter_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exectest"
	localartifactadapter "github.com/Khorea1/depengine/pkg/localartifactadapter"
	"github.com/Khorea1/depengine/pkg/run"
)

func TestAdapterConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, localartifactadapter.NewAdapter())
}

func TestAdapterInstallsRawArtifactRelativeToProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "demo"), []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	mc := &config.MethodCandidate{Kind: "local", ProjectRoot: root, Config: map[string]any{"local_path": "vendor/demo", "install_dir": dest}}
	tool := &config.Tool{Name: "demo"}
	fr := &run.FakeRunner{}
	adapter := localartifactadapter.NewAdapter()
	if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatal(err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("local install invoked subprocesses: %#v", fr.Calls)
	}
	resolvedChecksum, _ := mc.Config["checksum"].(string)
	if resolvedChecksum == "" {
		t.Fatal("local install did not freeze resolved content checksum for state persistence")
	}
	if strings.Contains(resolvedChecksum, root) {
		t.Fatalf("resolved checksum leaked project root: %q", resolvedChecksum)
	}
	got, err := os.ReadFile(filepath.Join(dest, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("installed payload = %q", got)
	}
	if !adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("installed local artifact not satisfied")
	}
	if err := os.WriteFile(filepath.Join(dest, "demo"), []byte("drift"), 0o755); err != nil {
		t.Fatal(err)
	}
	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("drifted raw artifact reported satisfied")
	}
}

func TestAdapterRemoveDoesNotNeedSourceFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo")
	if err := os.WriteFile(source, []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	mc := &config.MethodCandidate{Kind: "local", ProjectRoot: root, Config: map[string]any{"local_path": "demo", "install_dir": dest}}
	tool := &config.Tool{Name: "demo"}
	adapter := localartifactadapter.NewAdapter()
	if err := adapter.Install(context.Background(), &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Remove(context.Background(), &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "demo")); !os.IsNotExist(err) {
		t.Fatalf("installed file still exists: %v", err)
	}
}

func TestAdapterArchiveOwnsOnlyToolChild(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "demo.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entry, err := zw.Create("bin/demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	installDir := t.TempDir()
	sibling := filepath.Join(installDir, "keep")
	if err := os.WriteFile(sibling, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Kind: "local", ProjectRoot: root, Config: map[string]any{"local_path": "demo.zip", "install_dir": installDir}}
	tool := &config.Tool{Name: "demo"}
	fr := &run.FakeRunner{}
	adapter := localartifactadapter.NewAdapter()
	if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatal(err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("local archive install invoked subprocesses: %#v", fr.Calls)
	}
	if _, err := os.Stat(filepath.Join(installDir, "demo", "bin", "demo")); err != nil {
		t.Fatalf("archive payload missing: %v", err)
	}
	if !adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("installed archive not satisfied")
	}
	if err := os.WriteFile(archivePath, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("archive source changed after install but check remained satisfied")
	}
	if err := adapter.Remove(context.Background(), fr, tool, mc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "demo")); !os.IsNotExist(err) {
		t.Fatalf("owned tool directory still exists: %v", err)
	}
	if got, err := os.ReadFile(sibling); err != nil || string(got) != "keep" {
		t.Fatalf("sibling changed by remove: data=%q err=%v", got, err)
	}
}

func TestAdapterFreezesResolvedChecksumAcrossCheckAndInstall(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "demo")
	if err := os.WriteFile(source, []byte("first"), 0o755); err != nil {
		t.Fatal(err)
	}
	installDir := t.TempDir()
	mc := &config.MethodCandidate{
		Kind:        "local",
		ProjectRoot: root,
		Config: map[string]any{
			"local_path":  "demo",
			"install_dir": installDir,
		},
	}
	tool := &config.Tool{Name: "demo"}
	adapter := localartifactadapter.NewAdapter()
	fr := &run.FakeRunner{}
	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("missing destination unexpectedly satisfied")
	}
	checksum, _ := mc.Config["checksum"].(string)
	if checksum == "" {
		t.Fatal("Check() did not freeze resolved checksum")
	}
	if err := os.WriteFile(source, []byte("second"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("Install() accepted source changed after Check()")
	}
	if _, err := os.Stat(filepath.Join(installDir, "demo")); !os.IsNotExist(err) {
		t.Fatalf("destination exists after TOCTOU rejection: %v", err)
	}
}
