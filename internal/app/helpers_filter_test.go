package app

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestFilterToolsPreservesRequiredDependencies(t *testing.T) {
	tools := map[string]*config.Tool{
		"app":    {Name: "app", Requires: []string{"global"}, Methods: []*config.MethodCandidate{{Kind: "native", Requires: []string{"lazy"}}}},
		"global": {Name: "global", DependencyOnly: true},
		"lazy":   {Name: "lazy", DependencyOnly: true},
		"other":  {Name: "other"},
	}
	got := filterTools(tools, "app", "global,lazy", "")
	for _, name := range []string{"app", "global", "lazy"} {
		if got[name] == nil {
			t.Errorf("required tool %q was filtered out", name)
		}
	}
	if got["other"] != nil {
		t.Fatal("unrelated root was retained")
	}
}

func TestFilterToolsAllowsOnlyDependencyOnly(t *testing.T) {
	tools := map[string]*config.Tool{"helper": {Name: "helper", DependencyOnly: true}}
	got := filterTools(tools, "helper", "", "")
	if got["helper"] == nil || got["helper"].DependencyOnly {
		t.Fatal("--only did not promote dependency_only tool to a root")
	}
}

func TestHasLockableMutableSelectorsIncludesContainerTags(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		want   bool
	}{
		{name: "explicit tag", config: map[string]any{"source": "example/tool", "tag": "stable"}, want: true},
		{name: "implicit latest", config: map[string]any{"source": "example/tool"}, want: true},
		{name: "explicit digest", config: map[string]any{"source": "example/tool", "digest": "sha256:" + strings.Repeat("a", 64)}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{{Kind: "container", Config: tc.config}}}}}
			if got := hasLockableMutableSelectors(schema); got != tc.want {
				t.Fatalf("hasLockableMutableSelectors() = %t, want %t", got, tc.want)
			}
		})
	}
}
