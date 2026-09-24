package sbom

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/state"
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

func TestCycloneDXMethodPackageMetadata(t *testing.T) {
	cases := []struct {
		method        string
		componentType string
		purl          string
	}{
		{"cargo", "library", "pkg:cargo/tool@1.2.3"},
		{"go", "library", "pkg:golang/tool@1.2.3"},
		{"pip", "library", "pkg:pypi/tool@1.2.3"},
		{"pipx", "library", "pkg:pypi/tool@1.2.3"},
		{"uv", "library", "pkg:pypi/tool@1.2.3"},
		{"npm", "library", "pkg:npm/tool@1.2.3"},
		{"pnpm", "library", "pkg:npm/tool@1.2.3"},
		{"bun", "library", "pkg:npm/tool@1.2.3"},
		{"gem", "library", "pkg:gem/tool@1.2.3"},
		{"yarn", "library", "pkg:yarn/tool@1.2.3"},
		{"yarn-berry", "library", "pkg:yarn-berry/tool@1.2.3"},
		{"composer", "library", "pkg:composer/tool@1.2.3"},
		{"conda", "library", "pkg:conda/tool@1.2.3"},
		{"flatpak", "application", "pkg:flatpak/tool@1.2.3"},
		{"snap", "application", "pkg:snap/tool@1.2.3"},
		{"mas", "application", "pkg:mas/tool@1.2.3"},
		{"container", "application", "pkg:oci/tool@1.2.3"},
		{"native", "application", "pkg:native/tool@1.2.3"},
		{"custom", "application", "pkg:custom/tool@1.2.3"},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			s := &state.State{
				Version: 1,
				Tools: map[string]state.ToolState{
					"tool": {MethodKind: tc.method, Version: "1.2.3"},
				},
			}

			data, err := ExportCycloneDX(s)
			if err != nil {
				t.Fatalf("ExportCycloneDX: %v", err)
			}

			var bom struct {
				Components []CycloneDXComponent `json:"components"`
			}
			if err := json.Unmarshal(data, &bom); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if len(bom.Components) != 1 {
				t.Fatalf("components = %d, want 1", len(bom.Components))
			}
			got := bom.Components[0]
			if got.Type != tc.componentType {
				t.Errorf("component type = %q, want %q", got.Type, tc.componentType)
			}
			if got.PURL != tc.purl {
				t.Errorf("purl = %q, want %q", got.PURL, tc.purl)
			}
		})
	}
}

func TestExportCycloneDXVersionFallback(t *testing.T) {
	s := &state.State{
		Version: 1,
		Tools: map[string]state.ToolState{
			// Recorded ToolState.Version wins over config-derived values.
			"recorded": {
				Method:  "http",
				Version: "v4.30.0",
				Config:  map[string]any{"version": "0.0.0"},
			},
			// No recorded version: fall back to config-derived values.
			"configOnly": {
				Method: "cargo",
				Config: map[string]any{"tag": "v0.24.0"},
			},
			// Nothing knowable: 0.0.0 is the last resort.
			"unknown": {
				Method: "native",
				Config: map[string]any{},
			},
		},
	}

	data, err := ExportCycloneDX(s)
	if err != nil {
		t.Fatalf("ExportCycloneDX: %v", err)
	}

	var bom struct {
		Components []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	got := map[string]string{}
	for _, c := range bom.Components {
		got[c.Name] = c.Version
	}

	if got["recorded"] != "v4.30.0" {
		t.Errorf("recorded version: got %q, want %q", got["recorded"], "v4.30.0")
	}
	if got["configOnly"] != "v0.24.0" {
		t.Errorf("config fallback: got %q, want %q", got["configOnly"], "v0.24.0")
	}
	if got["unknown"] != "0.0.0" {
		t.Errorf("unknown version: got %q, want %q", got["unknown"], "0.0.0")
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

	// Compare only the components (schema-versioned), not the timestamp.
	var bom1, bom2 map[string]any
	json.Unmarshal(data1, &bom1)
	json.Unmarshal(data2, &bom2)
	delete(bom1, "metadata")
	delete(bom2, "metadata")
	if !reflect.DeepEqual(bom1, bom2) {
		t.Fatal("CycloneDX output (sans metadata) is not deterministic")
	}
	// Verify both timestamps are valid RFC3339.
	for _, d := range [][]byte{data1, data2} {
		var b map[string]any
		json.Unmarshal(d, &b)
		meta, ok := b["metadata"].(map[string]any)
		if !ok {
			t.Fatal("missing metadata")
		}
		ts, ok := meta["timestamp"].(string)
		if !ok {
			t.Fatal("missing timestamp")
		}
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Errorf("invalid timestamp %q: %v", ts, err)
		}
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

	// Compare only packages, not creationInfo (which has timestamp).
	var doc1, doc2 map[string]any
	json.Unmarshal(data1, &doc1)
	json.Unmarshal(data2, &doc2)
	delete(doc1, "creationInfo")
	delete(doc2, "creationInfo")
	if !reflect.DeepEqual(doc1, doc2) {
		t.Fatal("SPDX output (sans creationInfo) is not deterministic")
	}
	// Verify creationInfo has valid timestamps.
	for _, d := range [][]byte{data1, data2} {
		var doc map[string]any
		json.Unmarshal(d, &doc)
		ci, ok := doc["creationInfo"].(map[string]any)
		if !ok {
			t.Fatal("missing creationInfo")
		}
		created, ok := ci["created"].(string)
		if !ok {
			t.Fatal("missing created timestamp")
		}
		if _, err := time.Parse(time.RFC3339, created); err != nil {
			t.Errorf("invalid created timestamp %q: %v", created, err)
		}
	}
}
