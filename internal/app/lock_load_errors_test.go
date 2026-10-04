package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/state"
)

func TestLoadStatusSchemaLockLoadErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lockData   string
		lockDir    bool
		schemaData string
	}{
		{name: "malformed lock", lockData: "not = [toml"},
		{name: "unsupported version", lockData: `version = 99
`},
		{name: "unreadable lock", lockDir: true},
		{name: "schema degradation still surfaces lock error", lockData: "not = [toml", schemaData: "invalid = [toml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schemaPath := filepath.Join(t.TempDir(), "schema.toml")
			if err := os.WriteFile(schemaPath, []byte(tc.schemaData), 0o600); err != nil {
				t.Fatal(err)
			}
			lockPath := lock.DefaultPath(schemaPath)
			if tc.lockDir {
				if err := os.Mkdir(lockPath, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(lockPath, []byte(tc.lockData), 0o600); err != nil {
				t.Fatal(err)
			}

			manifest, noManifest := "", true
			_, _, err := loadStatusSchema(schemaPath, &manifest, &noManifest)
			if err == nil || !strings.Contains(err.Error(), "load lock for status") {
				t.Fatalf("loadStatusSchema error = %v, want lock-load error", err)
			}
		})
	}
}

func TestStatusCommandSurfacesLockLoadError(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(schemaPath, []byte("invalid = [toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock.DefaultPath(schemaPath), []byte("not = [toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveLocked(&state.State{Version: state.CurrentVersion}); err != nil {
		t.Fatal(err)
	}

	manifest, noManifest, format := "", true, "text"
	jsonFlag, orphans := false, false
	err := runStatus(context.Background(), &schemaPath, &manifest, &noManifest, &format, &jsonFlag, &orphans)
	if err == nil {
		t.Fatal("runStatus succeeded with an invalid lock; want runtime error")
	}
}

func TestLoadStatusSchemaValidLockLoads(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(schemaPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock.DefaultPath(schemaPath), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, noManifest := "", true
	_, lk, err := loadStatusSchema(schemaPath, &manifest, &noManifest)
	if err != nil || lk == nil || lk.Version != 1 {
		t.Fatalf("loadStatusSchema lock = (%v, %v), want valid v1 lock without error", lk, err)
	}
}

func TestLoadStatusSchemaMissingLockRemainsOptional(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(schemaPath, []byte("invalid = [toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, noManifest := "", true
	project, lk, err := loadStatusSchema(schemaPath, &manifest, &noManifest)
	if err != nil || project != nil || lk != nil {
		t.Fatalf("loadStatusSchema = (%v, %v, %v), want nil project and lock without error", project, lk, err)
	}
}

func TestSBOMLockLoadErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lockData string
		lockDir  bool
	}{
		{name: "malformed lock", lockData: "not = [toml"},
		{name: "unsupported version", lockData: `version = 99
`},
		{name: "unreadable lock", lockDir: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schemaPath := prepareSBOMLockFixture(t)
			lockPath := lock.DefaultPath(schemaPath)
			if tc.lockDir {
				if err := os.Mkdir(lockPath, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(lockPath, []byte(tc.lockData), 0o600); err != nil {
				t.Fatal(err)
			}
			format := "cyclonedx"
			if err := runSBOM(&format); err == nil {
				t.Fatal("runSBOM succeeded with an invalid lock; want lock-load error")
			}
		})
	}
}

func TestSBOMMissingLockRemainsOptional(t *testing.T) {
	prepareSBOMLockFixture(t)
	format := "cyclonedx"
	if err := runSBOM(&format); err != nil {
		t.Fatalf("runSBOM without lock: %v", err)
	}
}

func prepareSBOMLockFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(schemaPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveLocked(&state.State{Version: state.CurrentVersion, SchemaPath: schemaPath, Tools: map[string]state.ToolState{}}); err != nil {
		t.Fatal(err)
	}
	return schemaPath
}
