package sbom

import (
	"encoding/json"
	"testing"

	"depengine/pkg/state"
)

func TestExportCycloneDX(t *testing.T) {
	s := &state.State{
		Version: 1,
		Tools: map[string]state.ToolState{
			"bat": {
				Method: "cargo",
				Config: map[string]any{"version": "0.24.0"},
			},
			"ripgrep": {
				Method: "cargo",
				Config: map[string]any{"version": "14.1.0"},
			},
			"neovim": {
				Method: "native",
				Config: map[string]any{},
			},
		},
	}

	data, err := ExportCycloneDX(s)
	if err != nil {
		t.Fatalf("ExportCycloneDX: %v", err)
	}

	var bom map[string]any
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Validate structure.
	if bom["bomFormat"] != "CycloneDX" {
		t.Fatalf("expected CycloneDX, got %v", bom["bomFormat"])
	}
	if bom["specVersion"] != "1.5" {
		t.Fatalf("expected 1.5, got %v", bom["specVersion"])
	}

	components, ok := bom["components"].([]any)
	if !ok {
		t.Fatal("components must be an array")
	}
	if len(components) != 3 {
		t.Fatalf("expected 3 components, got %d", len(components))
	}
}

func TestExportCycloneDXEmpty(t *testing.T) {
	s := &state.State{
		Version: 1,
		Tools:   map[string]state.ToolState{},
	}

	data, err := ExportCycloneDX(s)
	if err != nil {
		t.Fatalf("ExportCycloneDX empty: %v", err)
	}

	var bom map[string]any
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	components, _ := bom["components"].([]any)
	if len(components) != 0 {
		t.Fatalf("expected 0 components, got %d", len(components))
	}
}

func TestExportSPDX(t *testing.T) {
	s := &state.State{
		Version: 1,
		Tools: map[string]state.ToolState{
			"bat": {Method: "cargo", Config: map[string]any{"version": "0.24.0"}},
		},
	}

	data, err := ExportSPDX(s)
	if err != nil {
		t.Fatalf("ExportSPDX: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if doc["spdxVersion"] != "SPDX-2.3" {
		t.Fatalf("expected SPDX-2.3, got %v", doc["spdxVersion"])
	}
}

func TestExportSPDXEmpty(t *testing.T) {
	s := &state.State{
		Version: 1,
		Tools:   map[string]state.ToolState{},
	}

	data, err := ExportSPDX(s)
	if err != nil {
		t.Fatalf("ExportSPDX empty: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	packages, _ := doc["packages"].([]any)
	if len(packages) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(packages))
	}
}

func TestSafeSPDXID(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"bat", "bat"},
		{"my-tool", "my-tool"},
		{"tool_v2", "tool-v2"},
		{"foo/bar", "foo-bar"},
		{"@types/node", "-types-node"},
	}
	for _, c := range cases {
		got := safeSPDXID(c.input)
		if got != c.want {
			t.Errorf("safeSPDXID(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestComponentType(t *testing.T) {
	cases := []struct {
		method string
		want   string
	}{
		{"cargo", "library"},
		{"go", "library"},
		{"pip", "library"},
		{"native", "application"},
		{"flatpak", "application"},
		{"http", "application"},
	}
	for _, c := range cases {
		got := componentType(c.method)
		if got != c.want {
			t.Errorf("componentType(%q) = %q, want %q", c.method, got, c.want)
		}
	}
}

func TestExtractVersion(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]any
		want   string
	}{
		{"nil config", nil, "0.0.0"},
		{"empty config", map[string]any{}, "0.0.0"},
		{"has version", map[string]any{"version": "1.2.3"}, "1.2.3"},
		{"has tag", map[string]any{"tag": "v2.0"}, "v2.0"},
		{"has ver", map[string]any{"ver": "3"}, "3"},
		{"non-string version", map[string]any{"version": 42}, "0.0.0"},
	}
	for _, c := range cases {
		got := extractVersion(c.config)
		if got != c.want {
			t.Errorf("extractVersion(%v) = %q, want %q", c.config, got, c.want)
		}
	}
}

func TestCycloneDXDeterministic(t *testing.T) {
	s := &state.State{
		Version: 1,
		Tools: map[string]state.ToolState{
			"z": {Method: "cargo", Config: map[string]any{}},
			"a": {Method: "native", Config: map[string]any{}},
			"m": {Method: "go", Config: map[string]any{}},
		},
	}

	data1, err := ExportCycloneDX(s)
	if err != nil {
		t.Fatalf("ExportCycloneDX: %v", err)
	}
	data2, err := ExportCycloneDX(s)
	if err != nil {
		t.Fatalf("ExportCycloneDX: %v", err)
	}

	if string(data1) != string(data2) {
		t.Fatal("CycloneDX output is not deterministic")
	}
}

func TestSPDXDeterministic(t *testing.T) {
	s := &state.State{
		Version: 1,
		Tools: map[string]state.ToolState{
			"z": {Method: "cargo", Config: map[string]any{}},
			"a": {Method: "native", Config: map[string]any{}},
			"m": {Method: "go", Config: map[string]any{}},
		},
	}

	data1, err := ExportSPDX(s)
	if err != nil {
		t.Fatalf("ExportSPDX: %v", err)
	}
	data2, err := ExportSPDX(s)
	if err != nil {
		t.Fatalf("ExportSPDX: %v", err)
	}

	if string(data1) != string(data2) {
		t.Fatal("SPDX output is not deterministic")
	}
}
