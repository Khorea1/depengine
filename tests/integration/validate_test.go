// Package integration tests the depengine CLI end-to-end by building
// the binary and running it against real schema files with various flags.
package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binary is the path to the compiled depengine binary (set in TestMain).
var binary string

// TestMain builds the depengine binary once for all tests.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "depengine-integration-*")
	if err != nil {
		os.Stderr.WriteString("integration: MkdirTemp: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binary = filepath.Join(tmp, "depengine")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = findModuleRoot()
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Stderr.WriteString("integration: build failed:\n" + string(out) + "\n")
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// findModuleRoot walks up to find go.mod.
func findModuleRoot() string {
	dir, _ := os.Getwd()
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	cwd, _ := os.Getwd()
	return cwd
}

// validatePath returns the path to a testdata file.
func validatePath(name string) string {
	return filepath.Join(findModuleRoot(), "pkg", "validate", "testdata", name)
}

// schemaPath returns the project's schema.example.toml.
func schemaPath() string {
	return filepath.Join(findModuleRoot(), "schema.example.toml")
}

// runDepengine executes depengine with args, returns output + exit code.
func runDepengine(args ...string) (output string, exitCode int) {
	cmd := exec.Command(binary, args...)
	out, err := cmd.CombinedOutput()
	output = string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	} else {
		exitCode = 0
	}
	return
}

// ============ Valid schemas ============

func TestValidate_ValidSchema(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", schemaPath())
	if code != 0 {
		t.Errorf("expected exit 0 for valid schema, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "✓ schema is valid") {
		t.Errorf("expected success message, got:\n%s", output)
	}
}

func TestValidate_AllValidEdgeCases(t *testing.T) {
	files := []string{
		"valid_minimal.toml",
		"valid_full.toml",
		"valid_with_http.toml",
		"valid_edge_cases.toml",
		"edge_all_methods.toml",
		"edge_long_chain.toml",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath(f))
			if code != 0 {
				t.Errorf("exit %d for %s; output:\n%s", code, f, output)
			}
		})
	}
}

// ============ Invalid schemas ============

func TestValidate_InvalidDanglingRef(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_dangling_ref.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "E_DANGLING_REF") {
		t.Error("expected E_DANGLING_REF")
	}
}

func TestValidate_Cycle(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_cycle.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "E_CYCLE") {
		t.Error("expected E_CYCLE")
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_multiple_errors.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	for _, code := range []string{"E_CYCLE", "E_DANGLING_REF", "E_MALFORMED_URL", "E_REQUIRED_FIELD"} {
		if !strings.Contains(output, code) {
			t.Errorf("expected %q in output:\n%s", code, output)
		}
	}
}

func TestValidate_PlaceholderWarning(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_unknown_placeholder.toml"))
	if code != 0 {
		t.Fatalf("expected exit 0 (warnings only), got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "W_UNKNOWN_PLACEHOLDER") {
		t.Error("expected W_UNKNOWN_PLACEHOLDER")
	}
}

func TestValidate_AllInvalidExit2(t *testing.T) {
	names := []string{
		"invalid_missing_git_url.toml",
		"invalid_missing_http_url.toml",
		"invalid_dangling_ref.toml",
		"invalid_malformed_url.toml",
		"invalid_cycle.toml",
		"invalid_multiple_errors.toml",
		"invalid_empty_config.toml",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath(name))
			if code != 2 {
				t.Errorf("expected exit 2, got %d; output:\n%s", code, output)
			}
		})
	}
}

func TestValidate_DupeTool(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_dupe_tool.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "E_DUPE_TOOL") {
		t.Error("expected E_DUPE_TOOL in text output")
	}
}

func TestValidate_DupeToolJSON(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_dupe_tool.toml"), "--format=json")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	var result struct {
		Errors   []json.RawMessage `json:"errors"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	// The first error's code should contain E_DUPE_TOOL.
	var firstErr struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(result.Errors[0], &firstErr); err != nil {
		t.Fatalf("invalid error JSON: %v", err)
	}
	if firstErr.Code != "E_DUPE_TOOL" {
		t.Errorf("expected code E_DUPE_TOOL, got %q", firstErr.Code)
	}
}

func TestValidate_DupeToolSimpleList(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_dupe_tool_simple.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "E_DUPE_TOOL") {
		t.Error("expected E_DUPE_TOOL in text output")
	}
	if !strings.Contains(output, "simple list") {
		t.Error("expected 'simple list' in text output (to distinguish from 'simple + inline table' branch)")
	}
}

func TestValidate_DupeToolSimpleListJSON(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_dupe_tool_simple.toml"), "--format=json")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	var result struct {
		Errors   []json.RawMessage `json:"errors"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	var firstErr struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(result.Errors[0], &firstErr); err != nil {
		t.Fatalf("invalid error JSON: %v", err)
	}
	if firstErr.Code != "E_DUPE_TOOL" {
		t.Errorf("expected code E_DUPE_TOOL, got %q", firstErr.Code)
	}
}

// ============ CLI flags ============

func TestValidate_StrictFlag(t *testing.T) {
	_, codeNoStrict := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_unknown_placeholder.toml"))
	if codeNoStrict != 0 {
		t.Error("without --strict expected exit 0")
	}

	output, codeStrict := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_unknown_placeholder.toml"), "--strict")
	if codeStrict != 1 {
		t.Errorf("with --strict expected exit 1, got %d; output:\n%s", codeStrict, output)
	}
}

func TestValidate_JSONFormat(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("invalid_multiple_errors.toml"), "--format=json")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	var result struct {
		Errors   []json.RawMessage `json:"errors"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one error in JSON")
	}
}

func TestValidate_JSONFormatSuccess(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("valid_minimal.toml"), "--format=json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, output)
	}
	var result struct {
		Errors   []json.RawMessage `json:"errors"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestValidate_CheckEnv(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", schemaPath(), "--check-env")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "W_ENV_MISSING") {
		t.Error("expected W_ENV_MISSING")
	}
}

func TestValidate_CheckEnvJSON(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", schemaPath(), "--check-env", "--format=json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, output)
	}
	var result struct {
		Errors   []json.RawMessage `json:"errors"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	t.Logf("env warnings: %d", len(result.Warnings))
}

func TestValidate_StrictCheckEnv(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", schemaPath(), "--strict", "--check-env")
	if code != 0 && code != 1 {
		t.Errorf("expected exit 0 or 1, got %d; output:\n%s", code, output)
	}
}

// ============ Corner cases ============

func TestValidate_NonExistentSchema(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", "/nonexistent/path/schema.toml")
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if !strings.Contains(output, "error:") {
		t.Errorf("expected error, got:\n%s", output)
	}
}

func TestValidate_EmptySchemaFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.toml")
	if err := os.WriteFile(emptyPath, []byte("[tools]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output, code := runDepengine("validate", "--no-manifest", "--schema", emptyPath)
	if code != 0 {
		t.Errorf("expected exit 0, got %d; output:\n%s", code, output)
	}
}

func TestValidate_HelpContainsValidate(t *testing.T) {
	output, code := runDepengine("help")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(output, "validate") {
		t.Error("expected 'validate' subcommand in help")
	}
}

// ============ Signature-no-key warning ============

func TestValidate_SignatureNoKeyWarning(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("warn_signature_no_key.toml"))
	if code != 0 {
		t.Errorf("expected exit 0 (warning), got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "W_SIGNATURE_NO_KEY") {
		t.Error("expected W_SIGNATURE_NO_KEY in output")
	}
	if !strings.Contains(output, "signature_url is set without signing_key") {
		t.Error("expected warning message about missing signing_key")
	}
}

func TestValidate_SignatureNoKeyJSON(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", validatePath("warn_signature_no_key.toml"), "--format=json")
	if code != 0 {
		t.Errorf("expected exit 0, got %d; output:\n%s", code, output)
	}
	var result struct {
		Errors   []json.RawMessage `json:"errors"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
	var warn struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(result.Warnings[0], &warn); err != nil {
		t.Fatalf("invalid warning JSON: %v", err)
	}
	if warn.Code != "W_SIGNATURE_NO_KEY" {
		t.Errorf("expected code W_SIGNATURE_NO_KEY, got %q", warn.Code)
	}
}

func TestValidate_SignatureNoKeyStrict(t *testing.T) {
	// Without --strict: exit 0
	_, codeNoStrict := runDepengine("validate", "--no-manifest", "--schema", validatePath("warn_signature_no_key.toml"))
	if codeNoStrict != 0 {
		t.Error("without --strict expected exit 0")
	}

	// With --strict: exit 1 (warning treated as error)
	output, codeStrict := runDepengine("validate", "--no-manifest", "--schema", validatePath("warn_signature_no_key.toml"), "--strict")
	if codeStrict != 1 {
		t.Errorf("with --strict expected exit 1, got %d; output:\n%s", codeStrict, output)
	}
}
