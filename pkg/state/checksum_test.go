package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveWritesIntegrityChecksum(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	st := &State{
		Version:          1,
		SchemaPath:       "/tmp/schema.toml",
		SchemaModifiedAt: "2024-01-01T00:00:00Z",
		Tools: map[string]ToolState{
			"nvim": {
				Method:          "native",
				InstalledAt:     "2024-01-01T00:00:00Z",
				PostinstallDone: true,
				DefinitionHash:  "abc123",
				Version:         "0.10.0",
				Config:          map[string]any{"pkg": "nvim-stable"},
			},
		},
	}

	if err := Save(st); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	if st.Checksum == "" {
		t.Fatal("Save() did not populate Checksum on the state")
	}
	if len(st.Checksum) != sha256.Size*2 {
		t.Fatalf("Checksum = %q, want %d hex chars", st.Checksum, sha256.Size*2)
	}
	if _, err := hex.DecodeString(st.Checksum); err != nil {
		t.Fatalf("Checksum is not valid hex: %v", err)
	}

	// The on-disk file must carry the checksum and recomputing it over the
	// parsed file (checksum zeroed) must reproduce the same digest.
	data, err := os.ReadFile(DefaultPath())
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if !strings.Contains(string(data), st.Checksum) {
		t.Fatal("state file does not contain the checksum written by Save")
	}
	var onDisk State
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal on-disk state: %v", err)
	}
	onDisk.Checksum = ""
	canonical, err := json.Marshal(&onDisk)
	if err != nil {
		t.Fatalf("marshal on-disk state: %v", err)
	}
	sum := sha256.Sum256(canonical)
	if got := hex.EncodeToString(sum[:]); got != st.Checksum {
		t.Fatalf("on-disk digest %q does not match stored checksum %q", got, st.Checksum)
	}
}

func TestSaveLoadRoundTripWithChecksum(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	original := &State{
		Version:          1,
		SchemaPath:       "/tmp/schema.toml",
		SchemaModifiedAt: "2024-01-01T00:00:00Z",
		Tools: map[string]ToolState{
			"fd": {
				Method:          "cargo",
				InstalledAt:     "2024-01-01T00:00:00Z",
				PostinstallDone: true,
				DefinitionHash:  "def456",
				Version:         "10.1.0",
				Config:          map[string]any{"pkg": "fd-find"},
			},
		},
	}

	if err := Save(original); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if loaded.Checksum == "" {
		t.Fatal("Load() did not retain the checksum written by Save")
	}
	if loaded.Checksum != original.Checksum {
		t.Fatalf("Checksum: got %q, want %q", loaded.Checksum, original.Checksum)
	}
	tool, ok := loaded.Tools["fd"]
	if !ok {
		t.Fatal("tool 'fd' missing after round-trip")
	}
	if tool.Method != "cargo" || tool.Version != "10.1.0" {
		t.Fatalf("tool mismatch after round-trip: %+v", tool)
	}

	// Saving the loaded state again must still verify cleanly (checksum is
	// recomputed, not double-hashed).
	if err := Save(loaded); err != nil {
		t.Fatalf("second Save(): %v", err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load() after second Save(): %v", err)
	}
	if len(reloaded.Tools) != 1 {
		t.Fatalf("expected 1 tool after re-save round-trip, got %d", len(reloaded.Tools))
	}
}

func TestLoadFromDetectsCorruptedState(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	st := &State{
		Version: 1,
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
	if err := Save(st); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	path := DefaultPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	// Corrupt one byte inside a string value ("nvim-stable" → "ovim-stable")
	// so the file stays valid JSON but the data no longer matches the checksum.
	idx := strings.Index(string(data), "nvim-stable")
	if idx < 0 {
		t.Fatal("corruption target not found in state file")
	}
	data[idx] ^= 0x01
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("rewrite corrupted state: %v", err)
	}

	_, err = LoadFrom(path)
	if err == nil {
		t.Fatal("LoadFrom() accepted a corrupted state file")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("LoadFrom() error does not mention checksum: %v", err)
	}

	// Load() (default path) must surface the same corruption error.
	_, err = Load()
	if err == nil {
		t.Fatal("Load() accepted a corrupted state file")
	}
}

func TestLoadFromLegacyFileWithoutChecksum(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "legacy-state.json")

	// A state file written before the checksum field existed: no "checksum"
	// key, must load normally without any integrity error.
	data := `{"version":1,"schema_path":"/tmp/schema.toml","schema_modified_at":"2024-01-01T00:00:00Z","tools":{"fd":{"method":"cargo","installed_at":"2024-01-01T00:00:00Z","postinstall_done":false,"definition_hash":"abc","config":{"pkg":"fd-find"}}}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() on legacy file without checksum: %v", err)
	}
	if s.Checksum != "" {
		t.Fatalf("legacy file: expected empty Checksum, got %q", s.Checksum)
	}
	if s.Version != 1 {
		t.Fatalf("Version: got %d, want 1", s.Version)
	}
	tool, ok := s.Tools["fd"]
	if !ok {
		t.Fatal("tool 'fd' not loaded from legacy file")
	}
	if tool.Method != "cargo" {
		t.Fatalf("Method: got %q, want %q", tool.Method, "cargo")
	}
	if tool.Config == nil {
		t.Fatal("Config should not be nil after loading legacy file")
	}
}
