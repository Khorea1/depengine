// Package state manages the depengine state file — a JSON record of every
// tool that has been installed (or already was present) through the engine.
// It lives at ~/.local/state/depengine/state.json when the home directory
// is available, otherwise under a verified per-user private temp root.
// Access is guarded by file-level locking to prevent concurrent-install races.
package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/formatversion"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/privatepath"
)

// CurrentVersion is the on-disk state format emitted by this build. Callers
// that construct State values directly should use this constant rather than a
// literal; persistence rejects every other version while the format is
// pre-freeze.
const CurrentVersion = formatversion.CurrentStateVersion

const currentStateVersion = CurrentVersion

// State is the on-disk schema for the depengine state file.
type State struct {
	Version                 int                                `json:"version"`
	SchemaPath              string                             `json:"schema_path"`
	SchemaModifiedAt        string                             `json:"schema_modified_at"`
	Tools                   map[string]ToolState               `json:"tools"`
	OwnedResources          []plan.OwnedResourceState          `json:"owned_resources,omitempty"`
	PreparationPlans        map[string]plan.PreparationPlan    `json:"preparation_plans,omitempty"`
	PreparationJournals     map[string]plan.PreparationJournal `json:"preparation_journals,omitempty"`
	ReplacementTransactions map[string]ReplacementTransaction  `json:"replacement_transactions,omitempty"`
	// Checksum is the SHA256 hex digest of the canonical JSON of the state
	// with this field zeroed. It is written by Save and verified by LoadFrom
	// to detect corrupted or tampered state files. Current state formats require this field
	// on every persisted document; older checksum-less formats are rejected.
	Checksum string `json:"checksum,omitempty"`
}

// ToolState records one installed tool.
type ToolState struct {
	// Method is the method name used for installation (e.g. "native", "cargo").
	Method string `json:"method"`
	// MethodKind is the technical method kind (e.g. "http"), not the display label.
	MethodKind string `json:"method_kind,omitempty"`
	// Provider records the concrete runtime provider when it differs from the
	// logical method kind (for example, "yay" for method kind "aur").
	Provider string `json:"provider,omitempty"`
	// InstalledAt is the RFC3339 timestamp of when the tool was installed.
	InstalledAt string `json:"installed_at"`
	// PostinstallDone is true if a postinstall script was successfully run.
	PostinstallDone bool `json:"postinstall_done"`
	// DefinitionHash is the SHA256 of the complete tool schema definition at
	// install time, including one-shot lifecycle hooks.
	DefinitionHash string `json:"definition_hash"`
	// DesiredStateHash excludes one-shot lifecycle hooks. Status uses it for
	// definition drift so historical hook success cannot become health evidence.
	DesiredStateHash string `json:"desired_state_hash,omitempty"`
	// Version is the installed tool version when the adapter can determine it
	// (e.g. the resolved/pinned tag at install time). Empty when unknown.
	Version string `json:"version,omitempty"`
	// RootRequested is durable user/root intent, as opposed to a tool installed
	// only to satisfy a dependency edge. Remove uses it to avoid garbage-
	// collecting a zero-ref prerequisite that the user also requested directly.
	RootRequested bool           `json:"root_requested,omitempty"`
	Config        map[string]any `json:"config"`
}

const privateDirMode = 0o700

// DefaultPath returns the platform-appropriate state file path.
// Uses XDG_STATE_HOME when set, otherwise ~/.local/state when home is
// available and a private per-user temp root when it is not.
func ensurePrivateDir(path string) error {
	root := privatepath.TempRootPath()
	if pathWithin(root, path) {
		if _, err := privatepath.TempRoot(); err != nil {
			return err
		}
	}
	return privatepath.EnsurePrivateDir(path)
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func defaultPath() (string, error) {
	xdgState := os.Getenv("XDG_STATE_HOME")
	if xdgState == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			xdgState = filepath.Join(home, ".local", "state")
		} else {
			root, err := privatepath.TempRoot()
			if err != nil {
				return "", fmt.Errorf("resolve private state directory: %w", err)
			}
			xdgState = filepath.Join(root, "state")
		}
	}
	return filepath.Join(xdgState, "depengine", "state.json"), nil
}

func DefaultPath() string {
	path, err := defaultPath()
	if err != nil {
		return ""
	}
	return path
}

// Load reads the state file from DefaultPath. LoadFrom creates missing state
// directly at the current format version and rejects unknown on-disk versions.
func Load() (*State, error) {
	path, err := defaultPath()
	if err != nil {
		return nil, err
	}
	if pathWithin(privatepath.TempRootPath(), path) {
		if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
			return nil, fmt.Errorf("state private directory: %w", err)
		}
	}
	return LoadFrom(path)
}

// Save writes the state to DefaultPath atomically: write to a temp file,
// fsync, then rename. This prevents corruption if the process crashes mid-write.
func Save(s *State) error {
	path, err := defaultPath()
	if err != nil {
		return err
	}
	data, err := marshalStateForPersistence(s)
	if err != nil {
		return err
	}
	if err := atomicWritePrivateFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func marshalStateForPersistence(s *State) ([]byte, error) {
	if err := validateStateSemantics(s); err != nil {
		return nil, err
	}
	if err := ValidateNoSecrets(s); err != nil {
		return nil, err
	}
	s.Checksum = ""
	canonical, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal state for checksum: %w", err)
	}
	sum := sha256.Sum256(canonical)
	s.Checksum = hex.EncodeToString(sum[:])
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}
	return data, nil
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
func LoadLocked() (*LockedState, error) { return LoadLockedContext(context.Background()) }

func LoadLockedContext(ctx context.Context) (*LockedState, error) {
	if _, err := defaultPath(); err != nil {
		return nil, err
	}
	lk, err := lockContext(ctx)
	if err != nil {
		return nil, err
	}
	st, err := Load()
	if err != nil {
		_ = lk.Close() // best-effort release; the load error is what we report
		return nil, err
	}
	return &LockedState{state: st, lock: lk}, nil
}

// LoadShared acquires a shared (read) lock and loads the state file.
// Use this for read-only operations (status, check) to avoid blocking
// concurrent install/remove. The caller must call Close on the returned
// LockedState to release the lock.
func LoadShared() (*LockedState, error) { return LoadSharedContext(context.Background()) }

func LoadSharedContext(ctx context.Context) (*LockedState, error) {
	if _, err := defaultPath(); err != nil {
		return nil, err
	}
	lk, err := lockSharedContext(ctx)
	if err != nil {
		return nil, err
	}
	st, err := Load()
	if err != nil {
		_ = lk.Close() // best-effort release; the load error is what we report
		return nil, err
	}
	return &LockedState{state: st, lock: lk}, nil
}

// SaveLocked acquires the lock, saves the state, and releases the lock.
// Use this when you have a state to save without loading existing state
// (e.g., after a fresh install run). For read-modify-write, use LoadLocked instead.
func SaveLocked(st *State) error { return SaveLockedContext(context.Background(), st) }

func SaveLockedContext(ctx context.Context, st *State) error {
	if _, err := defaultPath(); err != nil {
		return err
	}
	lk, err := lockContext(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = lk.Close() }()
	return Save(st)
}

// LoadFrom reads a state file from an arbitrary path (not DefaultPath).
// If the file does not exist, it returns an empty-but-valid State ready for first use.
func LoadFrom(path string) (*State, error) {
	if path == "" {
		return nil, fmt.Errorf("state path is empty")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- LoadFrom intentionally reads an explicitly selected state file path.
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version:                 currentStateVersion,
				Tools:                   make(map[string]ToolState),
				PreparationPlans:        make(map[string]plan.PreparationPlan),
				PreparationJournals:     make(map[string]plan.PreparationJournal),
				ReplacementTransactions: make(map[string]ReplacementTransaction),
			}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if err := formatversion.ValidateReadVersion(formatversion.State, s.Version); err != nil {
		return nil, fmt.Errorf("invalid state file %s: %w", path, err)
	}

	// Current state formats require an integrity checksum on every persisted document.
	// Omitting the field must not provide a bypass around checksum validation.
	if s.Checksum == "" {
		return nil, fmt.Errorf("corrupted state file %s: missing integrity checksum", path)
	}
	want := s.Checksum
	s.Checksum = ""
	canonical, err := json.Marshal(&s)
	if err != nil {
		return nil, fmt.Errorf("marshal state for checksum: %w", err)
	}
	sum := sha256.Sum256(canonical)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("corrupted state file %s: checksum mismatch", path)
	}
	s.Checksum = want

	if s.Tools == nil {
		s.Tools = make(map[string]ToolState)
	}
	if s.PreparationPlans == nil {
		s.PreparationPlans = make(map[string]plan.PreparationPlan)
	}
	if s.PreparationJournals == nil {
		s.PreparationJournals = make(map[string]plan.PreparationJournal)
	}
	if err := validateStateSemantics(&s); err != nil {
		return nil, fmt.Errorf("invalid state file %s: %w", path, err)
	}
	if err := ValidateNoSecrets(&s); err != nil {
		return nil, fmt.Errorf("invalid state file %s: %w", path, err)
	}
	return &s, nil
}

func validateStateSemantics(s *State) error {
	if s == nil {
		return errors.New("state is nil")
	}
	if err := formatversion.ValidateReadVersion(formatversion.State, s.Version); err != nil {
		return err
	}
	if err := plan.ValidateOwnedResourceSnapshot(s.OwnedResources); err != nil {
		return fmt.Errorf("owned resources: %w", err)
	}
	if len(s.PreparationPlans) != len(s.PreparationJournals) {
		return fmt.Errorf("preparation transactions: %d plans for %d journals", len(s.PreparationPlans), len(s.PreparationJournals))
	}
	for key, journal := range s.PreparationJournals {
		if err := validatePreparationJournalKey(key); err != nil {
			return err
		}
		preparationPlan, exists := s.PreparationPlans[key]
		if !exists {
			return fmt.Errorf("preparation journal %q has no persisted plan", key)
		}
		if err := preparationPlan.Validate(); err != nil {
			return fmt.Errorf("preparation plan %q: %w", key, err)
		}
		if err := journal.Validate(preparationPlan); err != nil {
			return fmt.Errorf("preparation journal %q: %w", key, err)
		}
		if journal.Status == plan.PreparationCommitted || journal.Status == plan.PreparationRolledBack {
			return fmt.Errorf("preparation journal %q is terminal and must not be persisted as active", key)
		}
	}
	for key := range s.PreparationPlans {
		if err := validatePreparationJournalKey(key); err != nil {
			return err
		}
		if _, exists := s.PreparationJournals[key]; !exists {
			return fmt.Errorf("preparation plan %q has no active journal", key)
		}
	}
	for key, transaction := range s.ReplacementTransactions {
		if err := validateReplacementTransaction(key, transaction); err != nil {
			return err
		}
	}
	return nil
}
