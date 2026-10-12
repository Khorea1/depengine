package validate

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
)

func TestPortableWindowsExecutableValidationIsExplicitAndFormatLimited(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   string
		config map[string]any
		when   *config.Condition
		wantOK bool
	}{
		{
			name: "HTTP portable executable",
			kind: "http",
			config: map[string]any{
				"url": "https://example.test/cli.exe", "binary": "cli.exe", "portable_executable": true,
			},
			when: &config.Condition{OS: []string{"windows"}}, wantOK: true,
		},
		{
			name: "GitHub portable executable",
			kind: "github",
			config: map[string]any{
				"repo": "owner/cli", "asset": "cli-windows-amd64.exe", "binary": "cli.exe", "portable_executable": true,
			},
			when: &config.Condition{OS: []string{"windows"}}, wantOK: true,
		},
		{
			name:   "no opt-in",
			kind:   "http",
			config: map[string]any{"url": "https://example.test/cli.exe", "binary": "cli.exe"},
			when:   &config.Condition{OS: []string{"windows"}},
		},
		{
			name:   "not restricted to Windows",
			kind:   "http",
			config: map[string]any{"url": "https://example.test/cli.exe", "binary": "cli.exe", "portable_executable": true},
			when:   &config.Condition{OS: []string{"windows", "linux"}},
		},
		{
			name:   "installer extension remains forbidden",
			kind:   "http",
			config: map[string]any{"url": "https://example.test/setup.msi", "binary": "setup.exe", "portable_executable": true},
			when:   &config.Condition{OS: []string{"windows"}},
		},
		{
			name:   "portable target requires exe name",
			kind:   "http",
			config: map[string]any{"url": "https://example.test/cli.exe", "binary": "cli", "portable_executable": true},
			when:   &config.Condition{OS: []string{"windows"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract, ok := methodkind.Lookup(tc.kind)
			if !ok || contract.Artifact == nil {
				t.Fatalf("artifact contract missing for %s", tc.kind)
			}
			method := &config.MethodCandidate{Kind: tc.kind, Config: tc.config, When: tc.when}
			result := &Result{}
			validateArtifactContract("cli", 0, method, contract.Artifact, result)
			if gotOK := !result.HasErrors(); gotOK != tc.wantOK {
				t.Fatalf("validation errors = %v, want accepted=%v", result.Errors, tc.wantOK)
			}
		})
	}
}
