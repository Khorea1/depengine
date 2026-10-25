package validate

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
)

func TestPortableWindowsExecutableValidationIsExplicitAndFormatLimited(t *testing.T) {
	windows := &config.Condition{OS: []string{"windows"}}
	for _, tc := range []struct {
		name          string
		kind          string
		config        map[string]any
		when          *config.Condition
		distributions []config.Distribution
		wantOK        bool
	}{
		{name: "HTTP exe", kind: "http", config: map[string]any{"url": "https://example.test/cli.exe", "binary": "cli.exe", "portable_executable": true}, when: windows, wantOK: true},
		{name: "shared GitHub Windows distribution", kind: "github", config: map[string]any{"repo": "owner/cli", "asset": "cli-linux.tar.gz", "binary": "cli", "portable_executable": true}, distributions: []config.Distribution{{When: windows, Asset: config.AssetSelector{Template: "cli-windows-amd64.exe"}, HasAsset: true, Binary: "cli.exe", HasBinary: true}}, wantOK: true},
		{name: "anchored regex .exe", kind: "github", config: map[string]any{"repo": "owner/cli", "asset": map[string]any{"regex": ".*\\.exe$"}, "binary": "cli.exe", "portable_executable": true}, when: windows, wantOK: true},
		{name: "regex mentioning .exe but matching msi", kind: "github", config: map[string]any{"repo": "owner/cli", "asset": map[string]any{"regex": "(?:\\.exe|\\.msi)$"}, "binary": "cli.exe", "portable_executable": true}, when: windows},
		{name: "msi raw URL", kind: "http", config: map[string]any{"url": "https://example.test/setup.msi", "binary": "setup.exe", "portable_executable": true}, when: windows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract, ok := methodkind.Lookup(tc.kind)
			if !ok || contract.Artifact == nil {
				t.Fatalf("artifact contract missing for %s", tc.kind)
			}
			method := &config.MethodCandidate{Kind: tc.kind, Config: tc.config, When: tc.when, Distributions: tc.distributions}
			result := &Result{}
			validateArtifactContract("cli", 0, method, contract.Artifact, result)
			if gotOK := !result.HasErrors(); gotOK != tc.wantOK {
				t.Fatalf("validation errors = %v, want accepted=%v", result.Errors, tc.wantOK)
			}
		})
	}
}
