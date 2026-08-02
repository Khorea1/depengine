package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
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
	tool := &config.Tool{
		Name: "fd",
		Methods: []*config.MethodCandidate{
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
	tool1 := &config.Tool{
		Name: "fd",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "fd-find"}},
		},
	}
	tool2 := &config.Tool{
		Name: "ripgrep",
		Methods: []*config.MethodCandidate{
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
	tool1 := &config.Tool{
		Name: "fd",
		Methods: []*config.MethodCandidate{
			{Kind: "cargo", Config: map[string]any{"pkg": "fd-find"}},
			{Kind: "native", Config: map[string]any{"pkg": "fd-find"}},
		},
	}
	tool2 := &config.Tool{
		Name: "fd",
		Methods: []*config.MethodCandidate{
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

func TestDefinitionHashIncludesRequires(t *testing.T) {
	base := &config.Tool{
		Name: "test",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}},
		},
	}
	withRequires := &config.Tool{
		Name:     "test",
		Requires: []string{"other-tool"},
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}},
		},
	}

	h1 := DefinitionHash(base)
	h2 := DefinitionHash(withRequires)
	if h1 == h2 {
		t.Fatal("DefinitionHash should differ when Requires is added")
	}
}

func TestDefinitionHashIncludesPostInstall(t *testing.T) {
	base := &config.Tool{
		Name: "test",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}},
		},
	}
	withPost := &config.Tool{
		Name:        "test",
		PostInstall: "fc-cache -fv",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}},
		},
	}

	h1 := DefinitionHash(base)
	h2 := DefinitionHash(withPost)
	if h1 == h2 {
		t.Fatal("DefinitionHash should differ when PostInstall is added")
	}
}

func TestDefinitionHashIncludesPreInstall(t *testing.T) {
	base := &config.Tool{
		Name: "test",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}},
		},
	}
	withPre := &config.Tool{
		Name:       "test",
		PreInstall: "curl -fsSL https://example.com/setup.sh | sh",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}},
		},
	}

	h1 := DefinitionHash(base)
	h2 := DefinitionHash(withPre)
	if h1 == h2 {
		t.Fatal("DefinitionHash should differ when PreInstall is added")
	}
}

func TestDefinitionHashIncludesWhenCondition(t *testing.T) {
	base := &config.Tool{
		Name: "test",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}},
		},
	}
	withWhen := &config.Tool{
		Name: "test",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "test"}, When: &config.Condition{DistroFamily: []string{"arch"}}},
		},
	}

	h1 := DefinitionHash(base)
	h2 := DefinitionHash(withWhen)
	if h1 == h2 {
		t.Fatal("DefinitionHash should differ when When condition is added")
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

func TestLockSharedAcquireAndRelease(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	closer, err := lockShared()
	if err != nil {
		t.Fatalf("lockShared(): %v", err)
	}

	// While shared lock is held, another shared lock should succeed.
	got := make(chan error, 1)
	go func() {
		c2, err := lockShared()
		if err == nil {
			c2.Close() // release immediately so the exclusive test below isn't blocked
		}
		got <- err
	}()

	if err := <-got; err != nil {
		t.Fatalf("lockShared() while shared held: %v", err)
	}

	// Exclusive lock should block while shared is held.
	got2 := make(chan error, 1)
	go func() {
		_, err := lock()
		got2 <- err
	}()

	// Release shared lock.
	if err := closer.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	if err := <-got2; err != nil {
		t.Fatalf("lock() after shared release: %v", err)
	}
}

func TestLoadSharedReadOnly(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	// First, save something with exclusive lock.
	initial := &State{Version: 1, Tools: map[string]ToolState{"test": {Method: "native"}}}
	if err := SaveLocked(initial); err != nil {
		t.Fatalf("SaveLocked(): %v", err)
	}

	// Read with shared lock.
	ls, err := LoadShared()
	if err != nil {
		t.Fatalf("LoadShared(): %v", err)
	}
	defer ls.Close()

	st := ls.State()
	if _, ok := st.Tools["test"]; !ok {
		t.Fatal("LoadShared() did not load existing state")
	}
}

func TestLoadFromCustomPath(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "custom-state.json")

	// Load from non-existent file returns empty state.
	s, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom on missing file: %v", err)
	}
	if s.Version != 1 {
		t.Fatalf("expected Version=1, got %d", s.Version)
	}
	if len(s.Tools) != 0 {
		t.Fatalf("expected empty Tools, got %d entries", len(s.Tools))
	}

	// Write state via regular Save and load from custom path.
	s.Tools["foo"] = ToolState{Method: "cargo", Config: map[string]any{"pkg": "foo"}}
	if err := Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// LoadFrom should still read the default path (not the custom one).
	s2, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom after save: %v", err)
	}
	// The state saved to DefaultPath, not LoadFrom's path.
	if len(s2.Tools) != 0 {
		t.Fatalf("LoadFrom should not see DefaultPath state: got %d entries", len(s2.Tools))
	}

	// Write to the custom path directly.
	data := `{"version":1,"tools":{"bar":{"method":"native","config":{"pkg":"bar"}}}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	s3, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom after write: %v", err)
	}
	if s3.Version != 1 {
		t.Fatalf("expected Version=1, got %d", s3.Version)
	}
	ts, ok := s3.Tools["bar"]
	if !ok {
		t.Fatal("expected tool 'bar' in loaded state")
	}
	if ts.Method != "native" {
		t.Fatalf("expected method 'native', got %q", ts.Method)
	}
}

func TestLoadLockedExclusive(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	ls, err := LoadLocked()
	if err != nil {
		t.Fatalf("LoadLocked(): %v", err)
	}

	st := ls.State()
	if st.Version != 1 {
		t.Fatalf("expected Version=1, got %d", st.Version)
	}

	// Add a tool and save while locked.
	st.Tools["test"] = ToolState{Method: "native"}
	if err := ls.Save(); err != nil {
		t.Fatalf("LockedState.Save(): %v", err)
	}

	if err := ls.Close(); err != nil {
		t.Fatalf("LockedState.Close(): %v", err)
	}

	// Verify it was persisted.
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if _, ok := loaded.Tools["test"]; !ok {
		t.Fatal("tool 'test' not found after LoadLocked round-trip")
	}
}

func TestSaveLocked(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	st := &State{
		Version: 1,
		Tools: map[string]ToolState{
			"foo": {Method: "native"},
		},
	}

	if err := SaveLocked(st); err != nil {
		t.Fatalf("SaveLocked(): %v", err)
	}

	// Verify file exists at DefaultPath.
	path := DefaultPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("state file not created at %s", path)
	}

	// Load it back via Load().
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if loaded.Version != 1 {
		t.Fatalf("Version: got %d, want 1", loaded.Version)
	}
	ts, ok := loaded.Tools["foo"]
	if !ok {
		t.Fatal("tool 'foo' not found after SaveLocked")
	}
	if ts.Method != "native" {
		t.Fatalf("Method: got %q, want %q", ts.Method, "native")
	}
}
