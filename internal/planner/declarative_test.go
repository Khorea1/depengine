package planner

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestValidateDeclarativeRecipeAcceptsNative(t *testing.T) {
	recipe := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{{
			Kind:   "native",
			Config: map[string]any{"pkg": "tool"},
		}}},
	}}
	if err := ValidateDeclarativeRecipe(recipe); err != nil {
		t.Fatalf("ValidateDeclarativeRecipe() = %v, want nil", err)
	}
}

func TestValidateDeclarativeRecipeRejectsLifecycleHook(t *testing.T) {
	recipe := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {
			Name:       "tool",
			PreInstall: []config.Hook{{Run: []string{"echo", "hi"}}},
			Methods: []*config.MethodCandidate{{
				Kind:   "native",
				Config: map[string]any{"pkg": "tool"},
			}},
		},
	}}
	err := ValidateDeclarativeRecipe(recipe)
	if err == nil || !strings.Contains(err.Error(), "pre_install lifecycle hook") {
		t.Fatalf("ValidateDeclarativeRecipe() = %v, want hook rejection", err)
	}
}

func TestValidateDeclarativeRecipeRejectsCommandField(t *testing.T) {
	recipe := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{{
			Kind:   "git",
			Config: map[string]any{"url": "https://example.test/tool.git", "build": "make"},
		}}},
	}}
	err := ValidateDeclarativeRecipe(recipe)
	if err == nil || !strings.Contains(err.Error(), "config \"build\" is a command field") {
		t.Fatalf("ValidateDeclarativeRecipe() = %v, want command-field rejection", err)
	}
}

func TestValidateDeclarativeRecipeRejectsArbitraryPlan(t *testing.T) {
	recipe := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{{
			Kind:   "deb",
			Config: map[string]any{"url": "https://example.test/tool.deb", "checksum": "sha256:" + strings.Repeat("0", 64)},
		}}},
	}}
	err := ValidateDeclarativeRecipe(recipe)
	if err == nil || !strings.Contains(err.Error(), "plan requires arbitrary code") {
		t.Fatalf("ValidateDeclarativeRecipe() = %v, want arbitrary-plan rejection", err)
	}
}

func TestValidateDeclarativeRecipeRejectsUnknownKind(t *testing.T) {
	recipe := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{{Kind: "not-a-method"}}},
	}}
	err := ValidateDeclarativeRecipe(recipe)
	if err == nil || !strings.Contains(err.Error(), "unknown method kind") {
		t.Fatalf("ValidateDeclarativeRecipe() = %v, want unknown-kind rejection", err)
	}
}

func TestValidateDeclarativeRecipeRejectsCredentialReferences(t *testing.T) {
	ref := &config.SecretReference{Provider: "env", Name: "SENSITIVE_TOKEN"}
	cases := []struct {
		name   string
		method *config.MethodCandidate
		want   string
	}{
		{name: "artifact", method: &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": "https://example.test/tool", "checksum": "sha256:" + strings.Repeat("0", 64)}, SecretRef: ref}, want: "secret_ref credential reference"},
		{name: "checksum", method: &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": "https://example.test/tool", "checksum": "sha256:auto", "checksum_url": "https://example.test/checksum"}, ChecksumSecretRef: ref}, want: "checksum_secret_ref credential reference"},
		{name: "signature", method: &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": "https://example.test/tool", "checksum": "sha256:auto", "signature_url": "https://example.test/signature", "signing_key": "key"}, SignatureSecretRef: ref}, want: "signature_secret_ref credential reference"},
		{name: "package-source", method: &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "tool"}, Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", SecretRef: ref}}}, want: "sources[0] secret_ref credential reference"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			recipe := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{tt.method}}}}
			err := ValidateDeclarativeRecipe(recipe)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateDeclarativeRecipe(%s) = %v, want rejection for %q", tt.name, err, tt.want)
			}
		})
	}
}
