package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/formatversion"
)

func TestLoadFromRejectsUnknownStateFormatVersions(t *testing.T) {
	for _, version := range []int{0, formatversion.CurrentStateVersion - 1, formatversion.CurrentStateVersion + 1} {
		t.Run(fmt.Sprintf("version-%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			data, err := json.Marshal(State{Version: version, Tools: map[string]ToolState{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}

			_, err = LoadFrom(path)
			if err == nil {
				t.Fatalf("LoadFrom() accepted unsupported state version %d", version)
			}
			if !strings.Contains(err.Error(), "unsupported state format version") {
				t.Fatalf("LoadFrom() error = %q, want unsupported state format version", err)
			}
		})
	}
}

func TestSaveRejectsUnknownStateFormatVersion(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := &State{Version: formatversion.CurrentStateVersion + 1, Tools: map[string]ToolState{}}

	err := Save(st)
	if err == nil {
		t.Fatal("Save() accepted unsupported state version")
	}
	if !strings.Contains(err.Error(), "unsupported state format version") {
		t.Fatalf("Save() error = %q, want unsupported state format version", err)
	}
}

func TestMissingStateUsesCurrentFormatVersion(t *testing.T) {
	st, err := LoadFrom(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("LoadFrom(missing): %v", err)
	}
	if st.Version != formatversion.CurrentStateVersion {
		t.Fatalf("Version = %d, want current %d", st.Version, formatversion.CurrentStateVersion)
	}
}
