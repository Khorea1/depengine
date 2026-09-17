package main

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
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
