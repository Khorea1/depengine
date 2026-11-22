package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
)

func TestValidateToolSelectorsRejectNoMatches(t *testing.T) {
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"zsh":  {Name: "zsh", Tags: []string{"minimal", "shell"}},
		"nvim": {Name: "nvim", Tags: []string{"desktop"}},
	}}
	for _, tc := range []struct {
		name    string
		only    string
		skip    string
		profile string
	}{
		{name: "only", only: "zshh"},
		{name: "skip", skip: "zsh,nope"},
		{name: "profile", profile: "server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateToolSelectors(schema, tc.only, tc.skip, tc.profile)
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != 2 {
				t.Fatalf("validateToolSelectors(%q, %q, %q) = %v, want ExitError 2", tc.only, tc.skip, tc.profile, err)
			}
		})
	}
}

func TestValidateToolSelectorsAcceptMatches(t *testing.T) {
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"zsh":  {Name: "zsh", Tags: []string{"minimal", "shell"}},
		"nvim": {Name: "nvim", Tags: []string{"desktop"}},
	}}
	for _, tc := range []struct{ name, only, skip, profile string }{
		{name: "empty"},
		{name: "only", only: "zsh"},
		{name: "skip", skip: "nvim"},
		{name: "profile case-insensitive", profile: "Shell"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateToolSelectors(schema, tc.only, tc.skip, tc.profile); err != nil {
				t.Fatalf("validateToolSelectors(%q, %q, %q) = %v, want nil", tc.only, tc.skip, tc.profile, err)
			}
		})
	}
}

func TestPresentSchemaLoadErrorIsUniform(t *testing.T) {
	var err error
	stderr := captureStderr(t, func() {
		err = presentSchemaLoadError(schemaNotFound("schema.toml"), false)
	})
	if !strings.Contains(stderr, "error: schema schema.toml not found") {
		t.Errorf("stderr = %q, want canonical missing-schema message", stderr)
	}
	if !strings.Contains(stderr, schemaNotFoundGuidance) {
		t.Errorf("stderr = %q, want guidance %q", stderr, schemaNotFoundGuidance)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("presentSchemaLoadError() = %v, want ExitError 2", err)
	}
}

func TestPresentSchemaLoadErrorJSONUsesValidationEnvelope(t *testing.T) {
	var err error
	stdout := captureStdout(t, func() {
		err = presentSchemaLoadError(schemaNotFound("schema.toml"), true)
	})
	for _, want := range []string{`"code": "E_SCHEMA_NOT_FOUND"`, `"field": "schema"`, `"schema schema.toml not found"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("presentSchemaLoadError() = %v, want ExitError 2", err)
	}
}

func TestPrintNextStepHonorsQuietAndJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		quiet   bool
		json    bool
		wantOut bool
	}{
		{name: "text", wantOut: true},
		{name: "quiet", quiet: true},
		{name: "json", json: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stderr := captureStderr(t, func() {
				printNextStep(newCLIStyle(os.Stderr), tc.quiet, tc.json, "run 'depengine update'")
			})
			got := strings.Contains(stderr, "Next: run 'depengine update'")
			if got != tc.wantOut {
				t.Fatalf("hint printed = %t, want %t (stderr %q)", got, tc.wantOut, stderr)
			}
		})
	}
}

func TestInstallNextStepHint(t *testing.T) {
	report := &exec.ExecReport{Success: 2}
	for _, tc := range []struct {
		name   string
		dryRun bool
		lk     *lock.Lock
		report *exec.ExecReport
		want   string
	}{
		{name: "legacy lock suggests update", report: report, want: "update"},
		{name: "v2 lock needs no hint", lk: &lock.Lock{Version: lock.CurrentVersion}, report: report},
		{name: "dry run", dryRun: true, report: report},
		{name: "failed run", report: &exec.ExecReport{Failed: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hint := installNextStepHint(installPlan{dryRun: tc.dryRun}, tc.lk, tc.report)
			if tc.want == "" && hint != "" {
				t.Fatalf("hint = %q, want none", hint)
			}
			if tc.want != "" && !strings.Contains(hint, tc.want) {
				t.Fatalf("hint = %q, want it to mention %q", hint, tc.want)
			}
		})
	}
}

func TestUpdateNextStepHint(t *testing.T) {
	if hint := updateNextStepHint(false, &lock.Lock{Version: lock.CurrentVersion}); !strings.Contains(hint, "--frozen-lockfile") {
		t.Fatalf("v2 hint = %q, want --frozen-lockfile", hint)
	}
	if hint := updateNextStepHint(false, &lock.Lock{Version: 1}); strings.Contains(hint, "--frozen-lockfile") {
		t.Fatalf("v1 hint = %q, must not suggest --frozen-lockfile", hint)
	}
	if hint := updateNextStepHint(true, &lock.Lock{Version: lock.CurrentVersion}); hint != "" {
		t.Fatalf("dry-run hint = %q, want none", hint)
	}
}

func TestSchemaConsumingCommandsReportMissingSchemaTheSameWay(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "schema.toml")
	cases := [][]string{
		{"validate", "--schema", missing, "--no-manifest"},
		{"install", "--schema", missing, "--no-manifest", "--dry-run"},
		{"update", "--schema", missing, "--no-manifest"},
		{"upgrade", "--schema", missing, "--no-manifest"},
		{"why", "zsh", "--schema", missing, "--no-manifest"},
		{"graph", "--schema", missing, "--no-manifest"},
		{"check", "zsh", "--schema", missing, "--no-manifest"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			code, out := runCommand(t, args[0], nil, args[1:]...)
			if code != 2 {
				t.Fatalf("exit code %d, want 2; output:\n%s", code, out)
			}
			if !strings.Contains(out, "schema "+missing+" not found") {
				t.Errorf("output missing canonical message:\n%s", out)
			}
			if !strings.Contains(out, schemaNotFoundGuidance) {
				t.Errorf("output missing guidance %q:\n%s", schemaNotFoundGuidance, out)
			}
		})
	}
}

func TestValidateMissingSchemaJSONUsesSharedEnvelope(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "schema.toml")
	code, out := runCommand(t, "validate", nil, "--schema", missing, "--no-manifest", "--format", "json")
	if code != 2 {
		t.Fatalf("exit code %d, want 2; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"E_SCHEMA_NOT_FOUND"`) {
		t.Errorf("output missing shared error code:\n%s", out)
	}
}

func TestInstallRejectsSelectorsThatMatchNoTool(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.toml")
	body := "schema_version = 1\n\n[tools.zsh]\nnative = true\n\n[tools.nvim]\nnative = true\ntags = [\"desktop\"]\n"
	if err := os.WriteFile(schemaPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		args      []string
		want      string
		available string
	}{
		{name: "only", args: []string{"--only", "zshh"}, want: "--only matched no tool", available: "zsh"},
		{name: "skip", args: []string{"--skip", "zshh"}, want: "--skip matched no tool", available: "zsh"},
		{name: "profile", args: []string{"--profile", "server"}, want: "--profile matched no tool", available: "desktop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--schema", schemaPath, "--no-manifest", "--dry-run"}, tc.args...)
			code, out := runCommand(t, "install", nil, args...)
			if code != 2 {
				t.Fatalf("exit code %d, want 2; output:\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output missing %q:\n%s", tc.want, out)
			}
			if !strings.Contains(out, tc.available) {
				t.Errorf("output should list available values (%q):\n%s", tc.available, out)
			}
		})
	}
}
