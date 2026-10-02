package config

import (
	"reflect"
	"testing"
)

func parseLayerForMerge(t *testing.T, manifest bool, body string, facts map[string]string) *Schema {
	t.Helper()
	path := writeSchemaInline(t, body)
	var (
		s   *Schema
		err error
	)
	if manifest {
		s, err = ParseManifest(path, facts)
	} else {
		s, err = ParseProjectSchema(path, facts)
	}
	if err != nil {
		t.Fatalf("parse layer: %v", err)
	}
	return s
}

func TestMergeLayersPresenceExplicitZeroOverridesLower(t *testing.T) {
	manifest := parseLayerForMerge(t, true, `
[packages]
helper = { dependency_only = true, requires = ["lower"], native = true }
`, nil)
	project := parseLayerForMerge(t, false, `
[tools]
helper = { dependency_only = false, requires = [], native = true }
`, nil)

	merged := MergeLayers(manifest, project)
	tool := merged.Tools["helper"]
	if tool.DependencyOnly {
		t.Fatal("explicit dependency_only=false did not override lower true")
	}
	if len(tool.Requires) != 0 {
		t.Fatalf("explicit requires=[] did not clear lower requires: %v", tool.Requires)
	}
}

func TestMergeLayersPresenceDefaultsInheritPerField(t *testing.T) {
	manifest := parseLayerForMerge(t, true, `
[defaults]
aur_helper = "yay"
arch_map = { x86_64 = "amd64" }
method_prefer = ["cargo"]
[packages]
app = { cargo = true }
`, nil)
	project := parseLayerForMerge(t, false, `
[defaults]
manager = "apt"
[tools]
app = { cargo = true }
`, nil)

	merged := MergeLayers(manifest, project)
	if merged.Defaults.Manager != "apt" {
		t.Fatalf("manager = %q, want apt", merged.Defaults.Manager)
	}
	if merged.Defaults.AurHelper != "yay" {
		t.Fatalf("aur_helper = %q, want inherited yay", merged.Defaults.AurHelper)
	}
	if got := merged.Defaults.ArchMap["x86_64"]; got != "amd64" {
		t.Fatalf("arch_map[x86_64] = %q, want amd64", got)
	}
	if len(merged.Defaults.MethodOrder) == 0 || merged.Defaults.MethodOrder[0] != "cargo" {
		t.Fatalf("method order = %v, want inherited cargo prefix", merged.Defaults.MethodOrder)
	}
}

func TestMergeLayersPresenceEmptyDefaultPreferenceResetsLower(t *testing.T) {
	manifest := parseLayerForMerge(t, true, `
[defaults]
method_prefer = ["cargo"]
[packages]
app = { cargo = true }
`, nil)
	project := parseLayerForMerge(t, false, `
[defaults]
method_prefer = []
[tools]
app = { cargo = true }
`, nil)

	merged := MergeLayers(manifest, project)
	if !reflect.DeepEqual(merged.Defaults.MethodOrder, DefaultMethodOrder) {
		t.Fatalf("method order = %v, want engine default %v", merged.Defaults.MethodOrder, DefaultMethodOrder)
	}
}

func TestMergeLayersRequiresWhenMapMergeProjectWinsCollision(t *testing.T) {
	manifest := parseLayerForMerge(t, true, `
[packages]
app = { requires = ["a", "b"], requires_when = { a = { os = ["linux"] }, b = { arch = ["amd64"] } }, native = true }
`, nil)
	project := parseLayerForMerge(t, false, `
[tools]
app = { requires = ["a", "b"], requires_when = { a = { os = ["darwin"] } }, native = true }
`, nil)

	merged := MergeLayers(manifest, project)
	rw := merged.Tools["app"].RequiresWhen
	if rw["a"] == nil || !reflect.DeepEqual(rw["a"].OS, []string{"darwin"}) {
		t.Fatalf("collision did not use project condition: %+v", rw["a"])
	}
	if rw["b"] == nil || !reflect.DeepEqual(rw["b"].Arch, []string{"amd64"}) {
		t.Fatalf("lower-only condition was not inherited: %+v", rw["b"])
	}
}

func TestMergeLayersMethodMetadataPresenceAndHookIndependence(t *testing.T) {
	manifest := parseLayerForMerge(t, true, `
[packages]
app = { http = { url = "https://example.invalid/app", when = { os = ["linux"] }, pre_install = "echo lower-pre", post_install = "echo lower-post", arch_map = { x86_64 = "amd64" }, sources = [{ kind = "apt-ppa", name = "example/ppa" }] } }
`, nil)
	project := parseLayerForMerge(t, false, `
[tools]
app = { http = { url = "https://example.invalid/project" } }
`, nil)

	merged := MergeLayers(manifest, project)
	var method *MethodCandidate
	for _, candidate := range merged.Tools["app"].Methods {
		if candidate.Kind == "http" {
			method = candidate
			break
		}
	}
	if method == nil {
		t.Fatal("missing merged http candidate")
	}
	if method.When == nil || !reflect.DeepEqual(method.When.OS, []string{"linux"}) {
		t.Fatalf("when was not inherited: %+v", method.When)
	}
	if got := method.ArchMap["x86_64"]; got != "amd64" {
		t.Fatalf("arch_map not inherited: %q", got)
	}
	if len(method.PreInstall) != 1 || len(method.PostInstall) != 1 {
		t.Fatalf("hooks should inherit independently of sources: pre=%v post=%v", method.PreInstall, method.PostInstall)
	}
	if len(method.Sources) != 1 {
		t.Fatalf("sources not inherited: %v", method.Sources)
	}
}

func TestMergeLayersDoesNotMutateInputs(t *testing.T) {
	manifest := parseLayerForMerge(t, true, `
[packages]
app = { requires = ["a"], requires_when = { a = { os = ["linux"] } }, native = { pkg_overrides = { apt = "app-lower" } } }
`, nil)
	project := parseLayerForMerge(t, false, `
[tools]
app = { native = { pkg_overrides = { pacman = "app-upper" } } }
`, nil)
	beforeManifest := cloneTool(manifest.Tools["app"])
	beforeProject := cloneTool(project.Tools["app"])

	merged := MergeLayers(manifest, project)
	merged.Tools["app"].RequiresWhen["a"].OS[0] = "mutated"
	for _, method := range merged.Tools["app"].Methods {
		if overrides, ok := method.Config["pkg_overrides"].(map[string]any); ok {
			overrides["apt"] = "mutated"
		}
	}

	if !reflect.DeepEqual(manifest.Tools["app"], beforeManifest) {
		t.Fatal("manifest layer mutated during/after merge")
	}
	if !reflect.DeepEqual(project.Tools["app"], beforeProject) {
		t.Fatal("project layer mutated during/after merge")
	}
}

func TestParseManifestExpandsSameHostFacts(t *testing.T) {
	manifest := parseLayerForMerge(t, true, `
[packages]
app = { http = { url = "https://example.invalid/{distro_family}/{kernel}/app" } }
`, map[string]string{"distro_family": "debian", "kernel": "linux"})

	var got string
	for _, method := range manifest.Tools["app"].Methods {
		if method.Kind == "http" {
			got, _ = method.Config["url"].(string)
		}
	}
	if got != "https://example.invalid/debian/linux/app" {
		t.Fatalf("expanded url = %q", got)
	}
}
