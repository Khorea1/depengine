package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executeInitForTest(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	cmd := newInitCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		return requireExitCode(t, err), stdout.String(), stderr.String()
	}
	return 0, stdout.String(), stderr.String()
}

func TestInitDryRunNeverCreatesSchemaOrParent(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"default scaffold", nil, `simple = ["tool1", "tool2"]`},
		{"added tools", []string{"--add", "nvim,bat"}, `simple = ["nvim", "bat"]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "new-directory")
			path := filepath.Join(parent, "custom.toml")
			args := append([]string{"--schema", path, "--dry-run"}, tt.args...)
			code, stdout, stderr := executeInitForTest(t, "", args...)
			if code != 0 || !strings.Contains(stdout, tt.want) || !strings.Contains(stderr, "Preview: "+path+" (not written)") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if _, err := os.Stat(parent); !os.IsNotExist(err) {
				t.Fatalf("dry-run created parent directory: %v", err)
			}
		})
	}
}

func TestInitDryRunDoesNotOverwriteExistingSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(path, []byte("existing schema"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := executeInitForTest(t, "", "--schema", path, "--dry-run", "--add", "nvim")
	if code != 0 || !strings.Contains(stdout, `simple = ["nvim"]`) {
		t.Fatalf("code=%d preview=%q", code, stdout)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "existing schema" {
		t.Fatalf("dry-run modified existing file: %q (%v)", got, err)
	}
}

func TestInteractiveInitEOFNeverWrites(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
	}{
		{"before first tool", ""},
		{"after name", "demo\n"},
		{"during package override", "demo\n2\n"},
		{"after complete tool", "demo\n1\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "not-created")
			path := filepath.Join(parent, "schema.toml")
			code, stdout, stderr := executeInitForTest(t, tt.input, "--schema", path, "--interactive")
			if code != 2 || stdout != "" || !strings.Contains(stderr, "was not written") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if _, err := os.Stat(parent); !os.IsNotExist(err) {
				t.Fatalf("interrupted wizard created directory: %v", err)
			}
		})
	}
}

func TestInteractiveInitExplicitFinishAndPreview(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-created")
	path := filepath.Join(parent, "schema.toml")
	input := "demo\n1\n\n"
	code, stdout, stderr := executeInitForTest(t, input, "--schema", path, "--interactive", "--dry-run")
	if code != 0 || !strings.Contains(stdout, `simple = ["demo"]`) || !strings.Contains(stderr, "(not written)") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("interactive dry-run created directory: %v", err)
	}

	code, _, stderr = executeInitForTest(t, input, "--schema", path, "--interactive")
	if code != 0 {
		t.Fatalf("completed wizard code=%d stderr=%s", code, stderr)
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), `simple = ["demo"]`) {
		t.Fatalf("completed wizard schema=%q err=%v", body, err)
	}
}

func TestInitRejectsInteractiveAndAddTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")
	code, _, stderr := executeInitForTest(t, "", "--schema", path, "--interactive", "--add", "demo")
	if code != 2 || !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("conflicting flags wrote schema: %v", err)
	}
}
