package validate

import (
	"strings"
	"testing"

	cfg "github.com/Khorea1/depengine/internal/config"
)

func TestValidateAutoChecksumDescribesVerificationSource(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   map[string]any
		wantText string
	}{
		{name: "inferred sidecar", config: map[string]any{"checksum": "sha256:auto"}, wantText: "inferred sidecar"},
		{name: "configured sidecar", config: map[string]any{"checksum": "sha256:auto", "checksum_url": "https://example.test/SHA256SUMS"}, wantText: "configured sidecar"},
		{name: "literal checksum", config: map[string]any{"checksum": "sha256:6ca13d52ca70c883e0f0bb101e425a89e8624de51db2d2392593af6a84118090"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := &cfg.Schema{Tools: map[string]*cfg.Tool{"app": tool("app", []*cfg.MethodCandidate{mc("http", nil, tc.config)}, nil)}}
			result := validateRequiredFields(schema)
			if tc.wantText == "" {
				if len(result.Warnings) != 0 {
					t.Fatalf("warnings = %v, want none", result.Warnings)
				}
				return
			}
			if len(result.Warnings) != 1 || result.Warnings[0].Code != WarnAutoChecksum || !strings.Contains(result.Warnings[0].Message, tc.wantText) || strings.Contains(result.Warnings[0].Message, "NOT verified") {
				t.Fatalf("warnings = %v, want one accurate W_AUTO_CHECKSUM", result.Warnings)
			}
		})
	}
}
