// LoadFrom reads a state file from an arbitrary path (not DefaultPath).
// If the file does not exist, it returns an empty-but-valid State ready for first use.
// Package state manages the depengine state file — a JSON record of every
// tool that has been installed (or already was present) through the engine.
// It lives at ~/.local/state/depengine/state.json and is accessed with
// file-level locking to prevent concurrent-install races.
package state

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// State is the on-disk schema for the depengine state file.
type State struct {
	Version          int                  `json:"version"`
	SchemaPath       string               `json:"schema_path"`
	SchemaModifiedAt string               `json:"schema_modified_at"`
	Tools            map[string]ToolState `json:"tools"`
}

// ToolState records one installed tool.
type ToolState struct {
	// Method is the method name used for installation (e.g. "native", "cargo").
	Method string `json:"method"`
	// InstalledAt is the RFC3339 timestamp of when the tool was installed.
	InstalledAt string `json:"installed_at"`
	// PostinstallDone is true if a postinstall script was successfully run.
	PostinstallDone bool          `json:"postinstall_done"`
	// DefinitionHash is the SHA256 of the tool's schema definition at install time.
	DefinitionHash string         `json:"definition_hash"`
	Config         map[string]any `json:"config"`
}

// DefaultPath returns the platform-appropriate state file path.
// Uses XDG_STATE_HOME when set, falling back to ~/.local/state/depengine/state.json.
func DefaultPath() string {
	xdgState := os.Getenv("XDG_STATE_HOME")
	if xdgState == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "~"
		}
		xdgState = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(xdgState, "depengine", "state.json")
}

// Load reads the state file from DefaultPath. If the file does not exist,
// it returns an empty-but-valid State ready for first use.
func Load() (*State, error) {
	path := DefaultPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version: 1,
				Tools:   make(map[string]ToolState),
			}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Tools == nil {
		s.Tools = make(map[string]ToolState)
	}
	return &s, nil
}

// Save writes the state to DefaultPath, creating parent directories as needed.
func Save(s *State) error {
	path := DefaultPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// LockedState is a State handle that proves the file lock was acquired.
// Callers receive this from LoadLocked and must call Close when done.
type LockedState struct {
	state *State
	lock  io.Closer
}

// State exposes the underlying State for read access.
func (ls *LockedState) State() *State {
	return ls.state
}

// Save persists the state to disk. Must only be called while the lock is held.
func (ls *LockedState) Save() error {
	return Save(ls.state)
}

// Close releases the lock. Must be called (typically via defer).
func (ls *LockedState) Close() error {
	return ls.lock.Close()
}

// LoadLocked acquires the state lock and loads the state file.
// The caller must call Close on the returned LockedState to release the lock.
func LoadLocked() (*LockedState, error) {
	lk, err := lock()
	if err != nil {
		return nil, err
	}
	st, err := Load()
	if err != nil {
		lk.Close()
		return nil, err
	}
	return &LockedState{state: st, lock: lk}, nil
}

// LoadShared acquires a shared (read) lock and loads the state file.
// Use this for read-only operations (status, check) to avoid blocking
// concurrent install/remove. The caller must call Close on the returned
// LockedState to release the lock.
func LoadShared() (*LockedState, error) {
	lk, err := lockShared()
	if err != nil {
		return nil, err
	}
	st, err := Load()
	if err != nil {
		lk.Close()
		return nil, err
	}
	return &LockedState{state: st, lock: lk}, nil
}

// SaveLocked acquires the lock, saves the state, and releases the lock.
// Use this when you have a state to save without loading existing state
// (e.g., after a fresh install run). For read-modify-write, use LoadLocked instead.
func SaveLocked(st *State) error {
	lk, err := lock()
	if err != nil {
		return err
	}
	defer lk.Close()
	return Save(st)
}

// LoadFrom reads a state file from an arbitrary path (not DefaultPath).
// If the file does not exist, it returns an empty-but-valid State ready for first use.
func LoadFrom(path string) (*State, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return &State{
                Version: 1,
                Tools:   make(map[string]ToolState),
            }, nil
        }
        return nil, fmt.Errorf("read state: %w", err)
    }
    var s State
    if err := json.Unmarshal(data, &s); err != nil {
        return nil, fmt.Errorf("parse state: %w", err)
    }
    if s.Tools == nil {
        s.Tools = make(map[string]ToolState)
    }
    return &s, nil
}
