package state

import (
	"os"
	"path/filepath"
	"testing"

	"depengine/pkg/schema"
)

func TestDefaultPath(t *testing.T) {
	path := DefaultPath()
	if path == "" {
		t.Fatal("DefaultPath() should not be empty")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("DefaultPath() = %q, expected absolute path", path)
	}
}

func TestLoadReturnsEmptyOnMissingFile(t *testing.T) {
	// Set XDG_STATE_HOME to a temp dir so DefaultPath points there.
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	s, err := Load()
	if err != nil {
		t.Fatalf("Load() on missing file: %v", err)
	}
	if s.Version != 1 {
		t.Fatalf("expected Version=1, got %d", s.Version)
	}
	if s.Tools == nil {
		t.Fatal("Load() returned nil Tools map")
	}
	if len(s.Tools) != 0 {
		t.Fatalf("expected empty Tools, got %d entries", len(s.Tools))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	original := &State{
		Version:          1,
		SchemaPath:       "/tmp/test.toml",
		SchemaModifiedAt: "2024-01-01T00:00:00Z",
		Tools: map[string]ToolState{
			"nvim": {
				Method:          "native",
				AdapterKind:     "native",
				InstalledAt:     "2024-01-01T00:00:00Z",
				PostinstallDone: true,
				DefinitionHash:  "abc123",
				Config:          map[string]any{"pkg": "nvim-stable"},
			},
		},
	}

	if err := Save(original); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	// Verify file exists
	path := DefaultPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("state file not created at %s", path)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}

	if loaded.Version != original.Version {
		t.Fatalf("Version: got %d, want %d", loaded.Version, original.Version)
	}
	if loaded.SchemaPath != original.SchemaPath {
		t.Fatalf("SchemaPath: got %q, want %q", loaded.SchemaPath, original.SchemaPath)
	}
	if loaded.SchemaModifiedAt != original.SchemaModifiedAt {
		t.Fatalf("SchemaModifiedAt: got %q, want %q", loaded.SchemaModifiedAt, original.SchemaModifiedAt)
	}

	tool, ok := loaded.Tools["nvim"]
	if !ok {
		t.Fatal("tool 'nvim' not found after round-trip")
	}
	if tool.Method != "native" {
		t.Fatalf("Method: got %q, want %q", tool.Method, "native")
	}
	if tool.DefinitionHash != "abc123" {
		t.Fatalf("DefinitionHash: got %q, want %q", tool.DefinitionHash, "abc123")
	}
	if !tool.PostinstallDone {
		t.Fatal("PostinstallDone should be true")
	}
	if tool.Config == nil {
		t.Fatal("Config should not be nil after round-trip")
	}
	if pkg, ok := tool.Config["pkg"].(string); !ok || pkg != "nvim-stable" {
		t.Fatalf("Config['pkg']: got %v, want 'nvim-stable'", tool.Config["pkg"])
	}
}

func TestLoadPrexistingFile(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	// Write a valid state file manually.
	path := filepath.Join(td, "depengine", "state.json")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(`{"version":1,"tools":{"fd":{"method":"cargo","adapter_kind":"cargo"}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	fd, ok := s.Tools["fd"]
	if !ok {
		t.Fatal("tool 'fd' not found")
	}
	if fd.Method != "cargo" {
		t.Fatalf("Method: got %q, want %q", fd.Method, "cargo")
	}
}

func TestLoadCorruptedFile(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	path := filepath.Join(td, "depengine", "state.json")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, []byte(`{invalid json`), 0644)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for corrupted state file")
	}
}

func TestDefinitionHashStability(t *testing.T) {
	tool := &schema.Tool{
		Name: "fd",
		Methods: []*schema.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "fd-find"}},
			{Kind: "cargo", Config: map[string]any{"pkg": "fd-find"}},
		},
	}

	h1 := DefinitionHash(tool)
	h2 := DefinitionHash(tool)

	if h1 != h2 {
		t.Fatalf("DefinitionHash not stable: %q != %q", h1, h2)
	}
}

func TestDefinitionHashDiffersForDifferentTools(t *testing.T) {
	tool1 := &schema.Tool{
		Name: "fd",
		Methods: []*schema.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "fd-find"}},
		},
	}
	tool2 := &schema.Tool{
		Name: "ripgrep",
		Methods: []*schema.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "ripgrep"}},
		},
	}

	h1 := DefinitionHash(tool1)
	h2 := DefinitionHash(tool2)

	if h1 == h2 {
		t.Fatal("DefinitionHash should differ for different tools")
	}
}

func TestDefinitionHashSortOrderIndependent(t *testing.T) {
	tool1 := &schema.Tool{
		Name: "fd",
		Methods: []*schema.MethodCandidate{
			{Kind: "cargo", Config: map[string]any{"pkg": "fd-find"}},
			{Kind: "native", Config: map[string]any{"pkg": "fd-find"}},
		},
	}
	tool2 := &schema.Tool{
		Name: "fd",
		Methods: []*schema.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "fd-find"}},
			{Kind: "cargo", Config: map[string]any{"pkg": "fd-find"}},
		},
	}

	h1 := DefinitionHash(tool1)
	h2 := DefinitionHash(tool2)

	if h1 != h2 {
		t.Fatal("DefinitionHash should be order-independent")
	}
}

func TestLockAcquireAndRelease(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	closer, err := lock()
	if err != nil {
		t.Fatalf("lock(): %v", err)
	}

	// Lock should be held — try to acquire again from another goroutine.
	got := make(chan error, 1)
	go func() {
		_, err := lock()
		got <- err
	}()

	// Unlock and let the goroutine proceed.
	if err := closer.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	if err := <-got; err != nil {
		t.Fatalf("lock() after release: %v", err)
	}
}
