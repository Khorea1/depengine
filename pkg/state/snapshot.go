package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SnapshotInfo holds metadata about a saved snapshot.
type SnapshotInfo struct {
	Path      string    `json:"path"`
	Timestamp time.Time `json:"timestamp"`
	ToolCount int       `json:"tool_count"`
}

// snapshotDir returns the path to the snapshots directory (next to state.json).
func snapshotDir() string {
	return filepath.Join(filepath.Dir(DefaultPath()), "snapshots")
}

// SaveSnapshot copies the current state.json to a timestamped snapshot file
// in the snapshots subdirectory. If state.json does not exist, saves an
// empty snapshot. Returns info about the saved snapshot.
func SaveSnapshot() (*SnapshotInfo, error) {
	now := time.Now()
	name := fmt.Sprintf("state-%s.json", now.Format("20060102T150405.000000000"))
	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}

	src := DefaultPath()
	dst := filepath.Join(dir, name)

	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte("{}")
		} else {
			return nil, fmt.Errorf("read state for snapshot: %w", err)
		}
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return nil, fmt.Errorf("write snapshot: %w", err)
	}

	// Count tools for the info.
	var s State
	_ = json.Unmarshal(data, &s)
	toolCount := len(s.Tools)

	return &SnapshotInfo{
		Path:      dst,
		Timestamp: now,
		ToolCount: toolCount,
	}, nil
}

// ListSnapshots returns all snapshots sorted by timestamp (newest first).
func ListSnapshots() ([]SnapshotInfo, error) {
	dir := snapshotDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list snapshots: %w", err)
	}

	var snapshots []SnapshotInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "state-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var s State
		_ = json.Unmarshal(data, &s)

		// Parse timestamp from filename:
		//   state-20060102T150405.000000000.json (nano, 9 sub-second digits)
		//   state-20060102T150405.000.json       (milli, 3 sub-second digits)
		//   state-20060102T150405.json            (old, seconds only)
		ts := time.Time{}
		name := e.Name()
		// "state-" is 6 chars.
		if len(name) >= 35 {
			tsStr := name[6:31] // "20060102T150405.000000000" (25 chars)
			ts, _ = time.Parse("20060102T150405.000000000", tsStr)
		}
		if ts.IsZero() && len(name) >= 30 {
			tsStr := name[6:25] // "20060102T150405.000" (19 chars)
			ts, _ = time.Parse("20060102T150405.000", tsStr)
		}
		if ts.IsZero() && len(name) >= 25 {
			tsStr := name[6:21] // "20060102T150405" (15 chars)
			ts, _ = time.Parse("20060102T150405", tsStr)
		}
		snapshots = append(snapshots, SnapshotInfo{
			Path:      path,
			Timestamp: ts,
			ToolCount: len(s.Tools),
		})
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp.After(snapshots[j].Timestamp)
	})

	return snapshots, nil
}

// LoadSnapshot reads a snapshot file into a State.
func LoadSnapshot(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	if s.Tools == nil {
		s.Tools = make(map[string]ToolState)
	}
	return &s, nil
}
