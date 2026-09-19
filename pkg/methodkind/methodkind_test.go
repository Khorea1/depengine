package methodkind_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/native"
)

// TestKnownKindsIncludesNativeManagers verifies that every native manager
// name (Manager.Name) and binary alias (managerNameToClan key) appears in
// knownKinds, so that config.Validate can cross-check method kinds against
// the compile-time list.
func TestKnownKindsIncludesNativeManagers(t *testing.T) {
	known := methodkind.KnownKinds()
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}

	var missing []string

	check := func(name, source string) {
		if !knownSet[name] {
			missing = append(missing, fmt.Sprintf("%q (from %s)", name, source))
		}
	}

	// Check all Manager.Name values.
	for _, name := range native.ManagerNames() {
		check(name, "native.ManagerNames")
	}

	// Check all managerNameToClan alias keys.
	for _, name := range native.ManagerBinaryNames() {
		check(name, "native.ManagerBinaryNames")
	}

	// "native" is the ecosystem native method kind — it must always be present.
	if !knownSet["native"] {
		missing = append(missing, `"native" (ecosystem native kind)`)
	}

	if len(missing) > 0 {
		t.Fatalf(
			"KnownKinds() is missing native manager names that are registered in pkg/native:\n%s\n\n"+
				"Add each missing name to knownKinds in pkg/methodkind/methodkind.go, "+
				"keeping them alphabetically among the existing native manager entries.",
			strings.Join(missing, "\n"),
		)
	}
}

func TestContractsDefineUniqueKindsAndDefaultOrder(t *testing.T) {
	seenKinds := map[string]bool{}
	seenOrder := map[int]bool{}
	for _, contract := range methodkind.Contracts {
		if seenKinds[contract.Kind] {
			t.Errorf("duplicate contract kind %q", contract.Kind)
		}
		seenKinds[contract.Kind] = true
		if contract.DefaultOrder > 0 {
			if seenOrder[contract.DefaultOrder] {
				t.Errorf("duplicate default order %d", contract.DefaultOrder)
			}
			seenOrder[contract.DefaultOrder] = true
		}
	}
	if got, want := len(methodkind.DefaultMethodOrder), len(seenOrder); got != want {
		t.Fatalf("default order has %d entries, contracts define %d", got, want)
	}
	for _, kind := range methodkind.DefaultMethodOrder {
		contract, ok := methodkind.Lookup(kind)
		if !ok || contract.DefaultOrder == 0 {
			t.Errorf("default kind %q has no ordered contract", kind)
		}
	}
}

func TestContractAliasesResolveToCanonicalKind(t *testing.T) {
	for _, alias := range []string{"paru", "yay"} {
		contract, ok := methodkind.Lookup(alias)
		if !ok || contract.Kind != "aur" {
			t.Errorf("Lookup(%q) = %+v, %t; want aur", alias, contract, ok)
		}
	}
}

func TestGitHubPrecedesHTTPWithoutGHAlias(t *testing.T) {
	githubIndex, httpIndex := -1, -1
	for i, kind := range methodkind.DefaultMethodOrder {
		switch kind {
		case "github":
			githubIndex = i
		case "http":
			httpIndex = i
		}
	}
	if githubIndex < 0 || httpIndex != githubIndex+1 {
		t.Fatalf("default order must place github immediately before http: %v", methodkind.DefaultMethodOrder)
	}
	if contract, ok := methodkind.Lookup("gh"); ok {
		t.Fatalf("gh unexpectedly resolves to %q", contract.Kind)
	}
}

func TestArtifactContractsDeclareSourcesAndChecksums(t *testing.T) {
	tests := []struct {
		kind         string
		alternatives [][]string
		checksum     bool
	}{
		{kind: "http", alternatives: [][]string{{"url"}, {"repo", "asset"}}, checksum: true},
		{kind: "github", alternatives: [][]string{{"repo", "asset"}}, checksum: true},
		{kind: "appimage", alternatives: [][]string{{"url"}, {"repo", "asset"}}, checksum: true},
		{kind: "android", alternatives: [][]string{{"url"}, {"repo", "asset"}}, checksum: true},
		{kind: "msi", alternatives: [][]string{{"url"}, {"repo", "asset"}}, checksum: true},
		{kind: "native"},
		{kind: "cargo"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			contract, ok := methodkind.Lookup(tt.kind)
			if !ok {
				t.Fatalf("missing contract for %q", tt.kind)
			}
			if !reflect.DeepEqual(contract.SourceAlternatives, tt.alternatives) {
				t.Errorf("SourceAlternatives = %v, want %v", contract.SourceAlternatives, tt.alternatives)
			}
			_, checksum := contract.Fields["checksum"]
			if checksum != tt.checksum {
				t.Errorf("checksum support = %t, want %t", checksum, tt.checksum)
			}
		})
	}
}

func TestArtifactSpecializedContractsDoNotExposeOverriddenPlacementFields(t *testing.T) {
	tests := []struct {
		kind   string
		absent []string
	}{
		{kind: "appimage", absent: []string{"extract_to"}},
		{kind: "android", absent: []string{"extract_to", "binary"}},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			contract, ok := methodkind.Lookup(tt.kind)
			if !ok {
				t.Fatalf("missing contract for %q", tt.kind)
			}
			for _, field := range tt.absent {
				if _, ok := contract.Fields[field]; ok {
					t.Errorf("%s contract exposes %q even though the adapter overrides it", tt.kind, field)
				}
			}
		})
	}
}

func TestEveryContractFieldDeclaresSemanticEffects(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		for name, field := range contract.Fields {
			if field.Effects == 0 {
				t.Errorf("%s.%s has no semantic effect declaration", contract.Kind, name)
			}
		}
	}
}

func TestContractCapabilitiesMatchSemanticFields(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		if _, ok := contract.Fields["version"]; ok && !contract.Supports(methodkind.CapabilityExactVersion) {
			t.Errorf("%s declares version but not CapabilityExactVersion", contract.Kind)
		}
		if _, ok := contract.Fields["channel"]; ok && !contract.Supports(methodkind.CapabilityChannel) {
			t.Errorf("%s declares channel but not CapabilityChannel", contract.Kind)
		}
		if _, ok := contract.Fields["digest"]; ok && !contract.Supports(methodkind.CapabilityImmutableIdentity) {
			t.Errorf("%s declares digest but not CapabilityImmutableIdentity", contract.Kind)
		}
		for _, field := range []string{"source", "registry", "remote", "channels", "bucket", "index", "index_url"} {
			if _, ok := contract.Fields[field]; ok && !contract.Supports(methodkind.CapabilitySourceSelection) {
				t.Errorf("%s declares %s but not CapabilitySourceSelection", contract.Kind, field)
			}
		}
		for _, field := range []string{"rev"} {
			if _, ok := contract.Fields[field]; ok && !contract.Supports(methodkind.CapabilityRevision) {
				t.Errorf("%s declares %s but not CapabilityRevision", contract.Kind, field)
			}
		}
		if _, ok := contract.Fields["scope"]; ok && !contract.Supports(methodkind.CapabilityScope) {
			t.Errorf("%s declares scope but not CapabilityScope", contract.Kind)
		}
		for _, field := range []string{"architecture", "target", "platform"} {
			if _, ok := contract.Fields[field]; ok && !contract.Supports(methodkind.CapabilityArchitecture) {
				t.Errorf("%s declares %s but not CapabilityArchitecture", contract.Kind, field)
			}
		}
		for _, field := range []string{"environment", "prefix", "root"} {
			if _, ok := contract.Fields[field]; ok && !contract.Supports(methodkind.CapabilityEnvironmentTarget) {
				t.Errorf("%s declares %s but not CapabilityEnvironmentTarget", contract.Kind, field)
			}
		}
		for name, field := range contract.Fields {
			if field.Type == methodkind.Command && !contract.Supports(methodkind.CapabilityArbitraryCode) {
				t.Errorf("%s.%s is executable but the method lacks CapabilityArbitraryCode", contract.Kind, name)
			}
		}
	}
}

func TestKnownCapabilityDeclarations(t *testing.T) {
	tests := []struct {
		kind string
		cap  methodkind.Capability
		want bool
	}{
		{kind: "go", cap: methodkind.CapabilityExactVersion, want: true},
		{kind: "cargo", cap: methodkind.CapabilityExactVersion, want: true},
		{kind: "cargo", cap: methodkind.CapabilitySourceSelection, want: true},
		{kind: "cargo", cap: methodkind.CapabilityRevision, want: true},
		{kind: "cargo", cap: methodkind.CapabilityArchitecture, want: true},
		{kind: "cargo", cap: methodkind.CapabilityEnvironmentTarget, want: true},
		{kind: "asdf", cap: methodkind.CapabilityExactVersion, want: true},
		{kind: "sdkman", cap: methodkind.CapabilityExactVersion, want: true},
		{kind: "conda", cap: methodkind.CapabilityExactVersion, want: true},
		{kind: "conda", cap: methodkind.CapabilitySourceSelection, want: true},
		{kind: "conda", cap: methodkind.CapabilityEnvironmentTarget, want: true},
		{kind: "snap", cap: methodkind.CapabilityChannel, want: true},
		{kind: "container", cap: methodkind.CapabilityImmutableIdentity, want: true},
		{kind: "container", cap: methodkind.CapabilitySourceSelection, want: true},
		{kind: "git", cap: methodkind.CapabilityArbitraryCode, want: true},
		{kind: "native", cap: methodkind.CapabilityExactVersion, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			contract, ok := methodkind.Lookup(tt.kind)
			if !ok {
				t.Fatalf("missing contract %q", tt.kind)
			}
			if got := contract.Supports(tt.cap); got != tt.want {
				t.Fatalf("Supports(%d) = %t, want %t", tt.cap, got, tt.want)
			}
		})
	}
}

func TestRequestedCapabilitiesAreDerivedFromConfiguredFields(t *testing.T) {
	tests := []struct {
		kind   string
		config map[string]any
		want   methodkind.Capability
	}{
		{kind: "go", config: map[string]any{"pkg": "example/tool", "version": "1.2.3"}, want: methodkind.CapabilityExactVersion},
		{kind: "cargo", config: map[string]any{"pkg": "crate", "registry": "corp"}, want: methodkind.CapabilitySourceSelection},
		{kind: "pipx", config: map[string]any{"pkg": "black", "index_url": "https://packages.example/simple"}, want: methodkind.CapabilitySourceSelection},
		{kind: "uv", config: map[string]any{"pkg": "ruff", "index": "https://packages.example/simple"}, want: methodkind.CapabilitySourceSelection},
		{kind: "gem", config: map[string]any{"pkg": "rake", "source": "https://gems.example"}, want: methodkind.CapabilitySourceSelection},
		{kind: "cargo", config: map[string]any{"git": "https://example.test/repo", "rev": "deadbeef"}, want: methodkind.CapabilityRevision},
		{kind: "cargo", config: map[string]any{"pkg": "crate", "target": "x86_64-unknown-linux-musl"}, want: methodkind.CapabilityArchitecture},
		{kind: "cargo", config: map[string]any{"pkg": "crate", "root": "~/.local/cargo-tools"}, want: methodkind.CapabilityEnvironmentTarget},
		{kind: "conda", config: map[string]any{"pkg": "numpy", "channels": []string{"conda-forge"}}, want: methodkind.CapabilitySourceSelection},
		{kind: "conda", config: map[string]any{"pkg": "numpy", "environment": "data"}, want: methodkind.CapabilityEnvironmentTarget},
		{kind: "snap", config: map[string]any{"pkg": "foo", "channel": "edge"}, want: methodkind.CapabilityChannel},
		{kind: "container", config: map[string]any{"source": "foo", "digest": "sha256:abc"}, want: methodkind.CapabilityImmutableIdentity | methodkind.CapabilitySourceSelection},
		{kind: "git", config: map[string]any{"url": "https://example.test/repo", "build": map[string]any{"run": []any{"make"}}}, want: methodkind.CapabilityArbitraryCode},
		{kind: "git", config: map[string]any{"url": "https://example.test/repo"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			contract, ok := methodkind.Lookup(tt.kind)
			if !ok {
				t.Fatalf("missing contract %q", tt.kind)
			}
			if got := contract.RequestedCapabilities(tt.config); got != tt.want {
				t.Fatalf("RequestedCapabilities() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExactVersionManagerContractsRejectEmptyVersionIntent(t *testing.T) {
	for _, kind := range []string{"asdf", "sdkman", "pip", "npm", "pipx", "uv", "gem", "composer", "bun", "pnpm", "yarn"} {
		t.Run(kind, func(t *testing.T) {
			contract, ok := methodkind.Lookup(kind)
			if !ok {
				t.Fatalf("missing contract %q", kind)
			}
			field, ok := contract.Fields["version"]
			if !ok || !field.NonEmpty {
				t.Fatalf("%s version field must be non-empty: %+v", kind, field)
			}
			if !contract.Supports(methodkind.CapabilityExactVersion) {
				t.Fatalf("%s must declare exact-version capability", kind)
			}
		})
	}
}

func TestCapabilityNamesAndMismatch(t *testing.T) {
	contract, ok := methodkind.Lookup("native")
	if !ok {
		t.Fatal("native contract missing")
	}
	missing := contract.MissingCapabilities(map[string]any{"pkg": "foo", "version": "1.2.3"})
	if missing != methodkind.CapabilityExactVersion {
		t.Fatalf("MissingCapabilities=%d want exact-version", missing)
	}
	if got := methodkind.CapabilityNames(missing); fmt.Sprint(got) != "[exact-version]" {
		t.Fatalf("CapabilityNames=%v", got)
	}

	choco, ok := methodkind.Lookup("choco")
	if !ok {
		t.Fatal("choco contract missing")
	}
	requested := choco.RequestedCapabilities(map[string]any{
		"version": "1.2.3", "source": "internal", "architecture": "x86",
	})
	want := methodkind.CapabilityExactVersion | methodkind.CapabilitySourceSelection | methodkind.CapabilityArchitecture
	if requested != want {
		t.Fatalf("RequestedCapabilities=%d want %d", requested, want)
	}
	if missing := choco.MissingCapabilities(map[string]any{"version": "1.2.3", "source": "internal", "architecture": "x86"}); missing != 0 {
		t.Fatalf("unexpected choco capability mismatch: %v", methodkind.CapabilityNames(missing))
	}
}

func TestContractFieldDependenciesReferenceDeclaredFields(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		for field, required := range contract.Requires {
			if _, ok := contract.Fields[field]; !ok {
				t.Errorf("%s dependency key %q is not a declared field", contract.Kind, field)
			}
			for _, dependency := range required {
				if dependency == field {
					t.Errorf("%s.%s cannot require itself", contract.Kind, field)
				}
				if _, ok := contract.Fields[dependency]; !ok {
					t.Errorf("%s.%s requires undeclared field %q", contract.Kind, field, dependency)
				}
			}
		}
	}
}
