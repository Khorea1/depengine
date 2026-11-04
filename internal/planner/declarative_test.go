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
