package httpdownload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func TestDebLifecycleIsRejectedBeforeFilesystemOrRunnerMutation(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url":        "https://example.test/releases/tool_1.0_amd64.deb?download=1",
		"extract_to": root,
	}}
	runner := &run.FakeRunner{}
	adapter := NewHTTPAdapter()
	tool := &config.Tool{Name: "tool"}

	err := adapter.Remove(context.Background(), runner, tool, mc)
	if err == nil || !strings.Contains(err.Error(), "exact dpkg package identity is not persisted") {
		t.Fatalf("Remove error = %v, want explicit unsupported lifecycle error", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Remove changed filesystem marker: %v", err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("Remove runner calls = %v, want none", runner.Calls)
	}
	if err := adapter.Install(context.Background(), runner, tool, mc); err == nil || !strings.Contains(err.Error(), "exact dpkg package identity is not persisted") {
		t.Fatalf("Install error = %v, want explicit unsupported lifecycle error", err)
	}
	if adapter.Check(context.Background(), runner, tool, mc) {
		t.Fatal("Check reported unsupported .deb lifecycle as installed")
	}
	if _, err := adapter.Observe(context.Background(), runner, tool, mc); err == nil || !strings.Contains(err.Error(), "exact dpkg package identity is not persisted") {
		t.Fatalf("Observe error = %v, want explicit unsupported lifecycle error", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unsupported lifecycle changed filesystem marker: %v", err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("lifecycle runner calls = %v, want none", runner.Calls)
	}
}

func TestExtractDebRejectedBeforeCreatingDestinationOrRunningDpkg(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	runner := &run.FakeRunner{}
	err := Extract(context.Background(), "package.deb", root, ".deb", runner, false, "tool")
	if err == nil || !strings.Contains(err.Error(), "exact dpkg package identity is not persisted") {
		t.Fatalf("Extract error = %v, want explicit unsupported lifecycle error", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("destination stat error = %v, want destination absent", err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("Extract runner calls = %v, want none", runner.Calls)
	}
}
