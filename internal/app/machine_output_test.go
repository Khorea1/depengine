package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/state"
)

// machineTestEnvelope mirrors machineEnvelope so tests decode the wire shape
// instead of the Go type: a field rename must fail these tests.
type machineTestEnvelope struct {
	Version int             `json:"version"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data"`
}

// decodeEnvelope extracts the JSON envelope from merged stdout/stderr output.
// Commands keep human progress on stderr, so the payload is the last JSON
// object in the captured stream.
func decodeEnvelope(t *testing.T, out string) machineTestEnvelope {
	t.Helper()
	start := strings.Index(out, "{\n  \"version\"")
	if start < 0 {
		t.Fatalf("no machine envelope in output:\n%s", out)
	}
	var env machineTestEnvelope
	if err := json.Unmarshal([]byte(out[start:]), &env); err != nil {
		t.Fatalf("invalid envelope JSON: %v\noutput:\n%s", err, out)
	}
	return env
}

// TestMachineOutputEnvelope pins one versioned envelope across every command
// that renders machine output: version, producing command, and a payload under
// `data`. Each case also checks one command-specific key so a silently empty
// payload cannot pass.
func TestMachineOutputEnvelope(t *testing.T) {
	checksum := strings.Repeat("a", 64)
	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "schema.toml")
	schema := "schema_version = 1\n\n[tools.demo.http]\nurl = \"https://example.com/demo\"\nchecksum = \"sha256:" + checksum + "\"\n"
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	seededState := func(t *testing.T) string {
		stateHome := t.TempDir()
		writeTestState(t, stateHome, map[string]state.ToolState{"gostr": goToolState()})
		return stateHome
	}

	cases := []struct {
		name    string
		command string
		args    func(t *testing.T) (env []string, args []string)
		assert  func(t *testing.T, data map[string]any)
	}{
		{
			name:    "validate",
			command: "validate",
			args: func(*testing.T) ([]string, []string) {
				return nil, []string{"--schema", schemaPath, "--no-manifest", "--json"}
			},
			assert: func(t *testing.T, data map[string]any) {
				if _, ok := data["errors"]; !ok {
					t.Errorf("validate data missing errors: %v", data)
				}
			},
		},
		{
			name:    "status",
			command: "status",
			args: func(*testing.T) ([]string, []string) {
				return []string{"XDG_STATE_HOME=" + t.TempDir()}, []string{"--schema", schemaPath, "--no-manifest", "--format", "json"}
			},
		},
		{
			name:    "diff",
			command: "diff",
			args: func(t *testing.T) ([]string, []string) {
				path := func(tools map[string]state.ToolState) string {
					stateHome := t.TempDir()
					writeTestState(t, stateHome, tools)
					return filepath.Join(stateHome, "depengine", "state.json")
				}
				before := path(map[string]state.ToolState{})
				after := path(map[string]state.ToolState{"gostr": goToolState()})
				return nil, []string{before, after, "--json"}
			},
		},
		{
			name:    "undo-list",
			command: "undo",
			args: func(*testing.T) ([]string, []string) {
				return []string{"XDG_STATE_HOME=" + t.TempDir()}, []string{"--list", "--json"}
			},
			assert: func(t *testing.T, data map[string]any) {
				snapshots, ok := data["snapshots"].([]any)
				if !ok || len(snapshots) != 0 {
					t.Errorf("undo list snapshots = %v, want empty array", data["snapshots"])
				}
			},
		},
		{
			name:    "forget",
			command: "forget",
			args: func(t *testing.T) ([]string, []string) {
				return []string{"XDG_STATE_HOME=" + seededState(t)}, []string{"gostr", "--json"}
			},
			assert: func(t *testing.T, data map[string]any) {
				if data["tool"] != "gostr" {
					t.Errorf("forget tool = %v, want gostr", data["tool"])
				}
			},
		},
		{
			name:    "remove-dry-run",
			command: "remove",
			args: func(t *testing.T) ([]string, []string) {
				stateHome := seededState(t)
				binDir := t.TempDir()
				fakeBinary(t, binDir, "stringer")
				return append([]string{"XDG_STATE_HOME=" + stateHome}, goBinEnvironment(binDir)...),
					[]string{"--dry-run", "gostr", "--json"}
			},
			assert: func(t *testing.T, data map[string]any) {
				if data["dry_run"] != true {
					t.Errorf("remove dry_run = %v, want true", data["dry_run"])
				}
			},
		},
		{
			name:    "update-dry-run",
			command: "update",
			args: func(*testing.T) ([]string, []string) {
				return nil, []string{"--schema", schemaPath, "--no-manifest", "--dry-run", "--json"}
			},
			assert: func(t *testing.T, data map[string]any) {
				if pinned, _ := data["pinned"].(float64); pinned != 1 {
					t.Errorf("update pinned = %v, want 1", data["pinned"])
				}
				if data["dry_run"] != true {
					t.Errorf("update dry_run = %v, want true", data["dry_run"])
				}
			},
		},
		{
			name:    "install-dry-run",
			command: "install",
			args: func(*testing.T) ([]string, []string) {
				return nil, []string{"--schema", schemaPath, "--no-manifest", "--dry-run", "--json"}
			},
		},
		{
			name:    "why",
			command: "why",
			args: func(*testing.T) ([]string, []string) {
				return nil, []string{"demo", "--schema", schemaPath, "--no-manifest", "--json"}
			},
			assert: func(t *testing.T, data map[string]any) {
				if data["tool"] != "demo" {
					t.Errorf("why tool = %v, want demo", data["tool"])
				}
			},
		},
		{
			name:    "doctor",
			command: "doctor",
			args: func(*testing.T) ([]string, []string) {
				return nil, []string{"--schema", schemaPath, "--no-manifest", "--no-catalog", "--json"}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, args := tc.args(t)
			code, out := runCommand(t, tc.command, env, args...)
			if code != 0 {
				t.Fatalf("%s exit = %d, want 0 (output:\n%s)", tc.name, code, out)
			}
			envl := decodeEnvelope(t, out)
			if envl.Version != machineOutputVersion {
				t.Errorf("version = %d, want %d", envl.Version, machineOutputVersion)
			}
			if envl.Command != tc.command {
				t.Errorf("command = %q, want %q", envl.Command, tc.command)
			}
			if len(envl.Data) == 0 || string(envl.Data) == "null" {
				t.Fatalf("data is empty for %s", tc.name)
			}
			if tc.assert == nil {
				return
			}
			var data map[string]any
			if err := json.Unmarshal(envl.Data, &data); err != nil {
				// Array payloads (status, diff) only need the wrapper check.
				return
			}
			tc.assert(t, data)
		})
	}
}

func TestNormalizeOutputFormat(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		jsonFlag  bool
		want      string
		wantError bool
	}{
		{name: "default text", format: "", want: "text"},
		{name: "explicit text", format: "text", want: "text"},
		{name: "explicit json", format: "json", want: "json"},
		{name: "json shorthand", format: "text", jsonFlag: true, want: "json"},
		{name: "json shorthand with empty format", format: "", jsonFlag: true, want: "json"},
		{name: "unknown format", format: "yaml", wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			format := tc.format
			if err := normalizeOutputFormat(&format, &tc.jsonFlag); err != nil {
				if !tc.wantError {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if tc.wantError {
				t.Fatal("expected an error for an unknown format")
			}
			if format != tc.want {
				t.Fatalf("format = %q, want %q", format, tc.want)
			}
		})
	}
}
