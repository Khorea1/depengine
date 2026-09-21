package config

import (
	"strings"
	"testing"
)

func TestDefaultsMethodPreferIsCanonicalPreferenceField(t *testing.T) {
	raw := map[string]any{
		"method_prefer": []any{"cargo", "github"},
	}
	defaults := extractDefaults(raw)
	if len(defaults.MethodOrder) < 2 || defaults.MethodOrder[0] != "cargo" || defaults.MethodOrder[1] != "github" {
		t.Fatalf("MethodOrder = %v, want cargo/github preference prefix", defaults.MethodOrder)
	}
}

func TestDefaultsMethodOrderCompatibilityAlias(t *testing.T) {
	raw := map[string]any{
		"method_order": []any{"cargo", "github"},
	}
	defaults := extractDefaults(raw)
	if len(defaults.MethodOrder) < 2 || defaults.MethodOrder[0] != "cargo" || defaults.MethodOrder[1] != "github" {
		t.Fatalf("MethodOrder = %v, want cargo/github preference prefix", defaults.MethodOrder)
	}
}

func TestDefaultsRejectsMethodPreferAndMethodOrderTogether(t *testing.T) {
	raw := map[string]any{
		"schema_version": int64(1),
		"defaults": map[string]any{
			"method_prefer": []any{"cargo"},
			"method_order":  []any{"native"},
		},
		"tools": map[string]any{},
	}
	err := validateRawSchema(raw, "tools")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("validateRawSchema error = %v, want mutually exclusive diagnostic", err)
	}
}
