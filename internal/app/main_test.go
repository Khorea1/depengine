package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestMain(m *testing.M) {
	InitAdapters()
	os.Exit(m.Run())
}

func TestCommandReturnsTypedExitErrorWithoutTerminatingProcess(t *testing.T) {
	cmd := newGraphCmd()
	cmd.SetArgs([]string{"--format", "invalid"})
	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("Execute() error = %v, want ExitError code 2", err)
	}
}

func TestGraphCmdRejectsNegativeWidth(t *testing.T) {
	cmd := newGraphCmd()
	cmd.SetArgs([]string{"--format", "graph", "--width=-1"})
	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("Execute() error = %v, want ExitError code 2", err)
	}
}

func TestGraphCmdAllowsUnusedWidthForOtherFormats(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(schemaPath, []byte("schema_version = 1\n\n[tools.bat]\nnative = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := runCommand(t, "graph", nil,
		"--schema", schemaPath, "--no-manifest", "--format", "text", "--width=-1")
	if code != 0 {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
}

func TestGraphFormatGraphRendersTerminalDiagram(t *testing.T) {
	dir := t.TempDir()
	schema := "schema_version = 1\n\n" +
		"[tools.bat]\nnative = true\nrequires = [\"ctpv\"]\n\n" +
		"[tools.ctpv]\nnative = true\n\n" +
		"[tools.unzip]\nnative = true\n"
	schemaPath := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := runCommand(t, "graph", nil,
		"--schema", schemaPath, "--no-manifest", "--format", "graph", "--width", "40")
	if code != 0 {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "ctpv ──▶ bat") {
		t.Errorf("expected the requires edge as a diagram arrow, output:\n%s", out)
	}
	if !strings.Contains(out, "isolated:\nunzip") {
		t.Errorf("expected unzip in the isolated list, output:\n%s", out)
	}
}

func TestConfirmationAccepted(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  bool
	}{
		{name: "yes", input: "yes\n", want: true},
		{name: "uppercase y", input: " Y \n", want: true},
		{name: "no", input: "no\n", want: false},
		{name: "empty", input: "\n", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmationAccepted(strings.NewReader(tt.input)); got != tt.want {
				t.Fatalf("confirmationAccepted(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasLatestPlaceholdersRecognizesRepositoryReleaseMethods(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		config map[string]any
		want   bool
	}{
		{name: "github implicit latest", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz"}, want: true},
		{name: "github explicit latest", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "release": "latest"}, want: true},
		{name: "github named release", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "release": "nightly"}},
		{name: "github named branch", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "branch": "edge"}},
		{name: "http repo asset implicit latest", kind: "http", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz"}, want: true},
		{name: "non repository method", kind: "native", config: map[string]any{"pkg": "tool"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &config.Schema{Tools: map[string]*config.Tool{
				"tool": {Methods: []*config.MethodCandidate{{Kind: tt.kind, Config: tt.config}}},
			}}
			if got := hasLatestPlaceholders(s); got != tt.want {
				t.Fatalf("hasLatestPlaceholders() = %v, want %v", got, tt.want)
			}
		})
	}
}
