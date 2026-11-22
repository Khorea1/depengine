package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/state"
)

func writeAdoptFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInitAdoptFromManifestCreatesValidSchema(t *testing.T) {
	dir := t.TempDir()
	manifest := writeAdoptFixture(t, dir, "manifest.toml", `schema_version = 1

[packages.nvim]
native = true
tags = ["editor"]

[packages.bat]
cargo = { pkg = "bat", version = "0.24.0" }
`)
	out := filepath.Join(dir, "schema.toml")

	captureStdout(t, func() {
		if err := runInitAdopt(initAdoptOptions{Output: out, FromManifest: manifest}); err != nil {
			t.Fatalf("runInitAdopt: %v", err)
		}
	})

	schema, err := config.ParseProjectSchema(out, map[string]string{})
	if err != nil {
		t.Fatalf("adopted schema does not parse: %v", err)
	}
	nvim, ok := schema.Tools["nvim"]
	if !ok {
		t.Fatalf("adopted schema missing nvim: %+v", schema.Tools)
	}
	if len(nvim.Tags) != 1 || nvim.Tags[0] != "editor" {
		t.Errorf("nvim tags = %v, want [editor]", nvim.Tags)
	}
	if len(nvim.Methods) != 1 || nvim.Methods[0].Kind != "native" {
		t.Fatalf("nvim methods = %+v, want one native method", nvim.Methods)
	}
	bat, ok := schema.Tools["bat"]
	if !ok || len(bat.Methods) == 0 || bat.Methods[0].Kind != "cargo" {
		t.Fatalf("bat methods = %+v, want cargo", bat)
	}
}

func TestInitAdoptFromStateRecordsMethodAndTODO(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"demo": {
			Method:     "yay",
			MethodKind: "aur",
			Provider:   "yay",
			Config:     map[string]any{"pkg": "demo-bin"},
			Version:    "1.2.3",
		},
	})
	out := filepath.Join(t.TempDir(), "schema.toml")

	captureStdout(t, func() {
		if err := runInitAdopt(initAdoptOptions{Output: out, FromState: "auto"}); err != nil {
			t.Fatalf("runInitAdopt: %v", err)
		}
	})

	raw, err := os.ReadFile(out) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		`[tools.demo.aur]`,
		`pkg = "demo-bin"`,
		"installed version was 1.2.3",
		"provider was yay",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("adopted state schema missing %q:\n%s", want, body)
		}
	}

	schema, err := config.ParseProjectSchema(out, map[string]string{})
	if err != nil {
		t.Fatalf("adopted state schema does not parse: %v", err)
	}
	if got := schema.Tools["demo"]; got == nil || len(got.Methods) != 1 || got.Methods[0].Kind != "aur" {
		t.Fatalf("demo methods = %+v, want one aur method", got)
	}
}

func TestInitAdoptMergesExistingSchemaWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	out := writeAdoptFixture(t, dir, "schema.toml", "# hand-written header\nschema_version = 1\n\n[tools.keep]\nnative = true\n")
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"keep":  {Method: "native", MethodKind: "native", Config: map[string]any{"pkg": "keep"}},
		"extra": {Method: "go", MethodKind: "go", Config: map[string]any{"pkg": "example.com/extra"}},
	})

	captureStdout(t, func() {
		if err := runInitAdopt(initAdoptOptions{Output: out, FromState: "auto"}); err != nil {
			t.Fatalf("runInitAdopt: %v", err)
		}
	})

	raw, err := os.ReadFile(out) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.HasPrefix(body, "# hand-written header\n") {
		t.Errorf("merge rewrote the existing file header:\n%s", body)
	}
	if got := strings.Count(body, "[tools.keep"); got != 1 {
		t.Errorf("merge declared the keep tool %d times, want 1:\n%s", got, body)
	}
	if !strings.Contains(body, `[tools.extra.go]`) {
		t.Errorf("merge did not append the extra tool:\n%s", body)
	}
	schema, err := config.ParseProjectSchema(out, map[string]string{})
	if err != nil {
		t.Fatalf("merged schema does not parse: %v", err)
	}
	if len(schema.Tools) != 2 {
		t.Fatalf("merged tools = %v, want keep and extra", schema.Tools)
	}
}

func TestInitAdoptDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	manifest := writeAdoptFixture(t, dir, "manifest.toml", "schema_version = 1\n\n[packages.demo]\ngo = \"example.com/demo\"\n")
	out := filepath.Join(dir, "schema.toml")

	printed := captureStdout(t, func() {
		if err := runInitAdopt(initAdoptOptions{Output: out, FromManifest: manifest, DryRun: true}); err != nil {
			t.Fatalf("runInitAdopt: %v", err)
		}
	})
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote %s (err=%v)", out, err)
	}
	if !strings.Contains(printed, "schema_version = 1") {
		t.Errorf("dry-run did not print the schema:\n%s", printed)
	}
}

func TestInitAdoptMissingSourceIsAnError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "schema.toml")
	err := runInitAdopt(initAdoptOptions{Output: out, FromManifest: filepath.Join(t.TempDir(), "missing.toml")})
	if code := requireExitCode(t, err); code != 2 {
		t.Fatalf("missing manifest exit = %d, want 2", code)
	}
}

func TestInitRejectsConflictingAdoptionFlags(t *testing.T) {
	cmd := newInitCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--from-state=auto", "--from-manifest=auto"})
	if code := requireExitCode(t, cmd.Execute()); code != 2 {
		t.Fatalf("conflicting adoption flags exit = %d, want 2", code)
	}
}
