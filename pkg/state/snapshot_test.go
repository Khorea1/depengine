package state

import (
	"os"
	"testing"
)

func TestSaveSnapshotCreatesFile(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	// Save a state first.
	st := &State{
		Version: 1,
		Tools: map[string]ToolState{
			"test": {Method: "native"},
		},
	}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}

	info, err := SaveSnapshot()
	if err != nil {
		t.Fatalf("SaveSnapshot(): %v", err)
	}
	if info == nil {
		t.Fatal("SaveSnapshot() returned nil")
	}
	if info.ToolCount != 1 {
		t.Fatalf("expected 1 tool, got %d", info.ToolCount)
	}

	// Verify file exists.
	if _, err := os.Stat(info.Path); os.IsNotExist(err) {
		t.Fatalf("snapshot file not created at %s", info.Path)
	}
}

func TestSaveSnapshotNoExistingState(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	// No state file exists yet.
	info, err := SaveSnapshot()
	if err != nil {
		t.Fatalf("SaveSnapshot() without state: %v", err)
	}
	if info == nil {
		t.Fatal("SaveSnapshot() returned nil")
	}
	if info.ToolCount != 0 {
		t.Fatalf("expected 0 tools, got %d", info.ToolCount)
	}

	// Verify file exists.
	if _, err := os.Stat(info.Path); os.IsNotExist(err) {
		t.Fatalf("snapshot file not created at %s", info.Path)
	}
}

func TestListSnapshotsOrder(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	// Save initial state.
	st := &State{
		Version: 1,
		Tools: map[string]ToolState{
			"a": {Method: "native"},
		},
	}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}

	// Create two snapshots.
	info1, err := SaveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	// Add another tool and save another snapshot.
	st.Tools["b"] = ToolState{Method: "cargo"}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}

	info2, err := SaveSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	// List should return newest first.
	snaps, err := ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots(): %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	if snaps[0].Path != info2.Path {
		t.Fatal("expected newest snapshot first")
	}
	if snaps[1].Path != info1.Path {
		t.Fatal("expected oldest snapshot second")
	}
}

func TestLoadSnapshot(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	st := &State{
		Version: 1,
		Tools: map[string]ToolState{
			"test": {Method: "native"},
		},
	}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}

	info, err := SaveSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSnapshot(info.Path)
	if err != nil {
		t.Fatalf("LoadSnapshot(): %v", err)
	}
	if _, ok := loaded.Tools["test"]; !ok {
		t.Fatal("tool 'test' not found in loaded snapshot")
	}
}

func TestListSnapshotsEmpty(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)

	snaps, err := ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots() on empty dir: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(snaps))
	}
}
