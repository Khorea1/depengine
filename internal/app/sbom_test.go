package app

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/state"
)

func TestSBOMVersionFallback(t *testing.T) {
	cases := []struct {
		name             string
		stateVersion     string
		lockVersion      int
		projection       string
		projectionMethod string
		legacyVersion    string
		malformed        bool
		want             string
	}{
		{name: "state version wins", stateVersion: "1.2.3", lockVersion: lock.CurrentVersion, projection: "2.3.4", projectionMethod: "http", legacyVersion: "9.9.9", want: "1.2.3"},
		{name: "v2 projection fallback", lockVersion: lock.CurrentVersion, projection: "2.3.4", projectionMethod: "http", want: "2.3.4"},
		{name: "v1 legacy fallback", lockVersion: 1, legacyVersion: "3.4.5", want: "3.4.5"},
		{name: "unknown fallback", lockVersion: 1, want: "0.0.0"},
		{name: "v2 projection beats stale legacy payload", lockVersion: lock.CurrentVersion, projection: "4.5.6", projectionMethod: "http", legacyVersion: "8.8.8", want: "4.5.6"},
		{name: "v2 projection method mismatch", lockVersion: lock.CurrentVersion, projection: "5.6.7", projectionMethod: "npm", legacyVersion: "8.8.8", want: "0.0.0"},
		{name: "malformed v2 projection fails", lockVersion: lock.CurrentVersion, legacyVersion: "8.8.8", malformed: true, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runSBOMFixture(t, tc.stateVersion, tc.lockVersion, tc.projection, tc.projectionMethod, tc.legacyVersion, tc.malformed)
			if got != tc.want {
				t.Fatalf("SBOM component version = %q, want %q", got, tc.want)
			}
		})
	}
}

func runSBOMFixture(t *testing.T, stateVersion string, lockVersion int, projectionVersion, projectionMethod, legacyVersion string, malformed bool) string {
	t.Helper()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	projectDir := t.TempDir()
	schemaPath := filepath.Join(projectDir, "schema.toml")
	if err := os.WriteFile(schemaPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	st := &state.State{
		Version:    state.CurrentVersion,
		SchemaPath: schemaPath,
		Tools: map[string]state.ToolState{
			"demo": {Method: "http", MethodKind: "http", Version: stateVersion},
		},
	}
	if err := state.SaveLocked(st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	lk := &lock.Lock{
		Version:     lockVersion,
		Tools:       map[string]lock.ToolPin{"demo/http/0": {Latest: legacyVersion}},
		MethodsHash: map[string]string{},
		SourceHash:  map[string]string{},
	}
	if malformed {
		lk.UniversalProjection = "not a projection"
	} else if projectionVersion != "" {
		resolved := plan.New("demo", projectionMethod, true)
		resolved.Identity.Version = projectionVersion
		document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
		if err != nil {
			t.Fatalf("build lock projection: %v", err)
		}
		if err := lk.SetProjection(document); err != nil {
			t.Fatalf("set lock projection: %v", err)
		}
	}
	if err := lock.Save(lock.DefaultPath(schemaPath), lk); err != nil {
		t.Fatalf("save lock: %v", err)
	}

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = write
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = write.Close()
		_ = read.Close()
	})
	format := "cyclonedx"
	runErr := runSBOM(&format)
	_ = write.Close()
	os.Stdout = originalStdout
	output, readErr := io.ReadAll(read)
	_ = read.Close()
	if readErr != nil {
		t.Fatalf("read SBOM output: %v", readErr)
	}
	if malformed {
		if runErr == nil {
			t.Fatal("runSBOM succeeded with malformed lock projection")
		}
		return ""
	}
	if runErr != nil {
		t.Fatalf("runSBOM: %v (output %s)", runErr, output)
	}

	var document struct {
		Components []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatalf("decode SBOM output: %v", err)
	}
	for _, component := range document.Components {
		if component.Name == "demo" {
			return strings.TrimSpace(component.Version)
		}
	}
	t.Fatalf("SBOM output has no demo component: %s", output)
	return ""
}
