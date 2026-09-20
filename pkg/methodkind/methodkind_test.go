package methodkind_test

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/native"
	"github.com/Khorea1/depengine/pkg/plan"
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
		{kind: "container", cap: methodkind.CapabilityMutableTag, want: true},
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

func TestMissingCapabilitiesFailsClosedForProgrammaticConfig(t *testing.T) {
	contract, ok := methodkind.Lookup("native")
	if !ok {
		t.Fatal("native contract missing")
	}

	config := map[string]any{
		"source":       "internal",
		"architecture": "x64",
		"scope":        "user",
		"environment":  "tools",
		"rev":          "deadbeef",
	}
	want := methodkind.CapabilitySourceSelection |
		methodkind.CapabilityArchitecture |
		methodkind.CapabilityScope |
		methodkind.CapabilityEnvironmentTarget |
		methodkind.CapabilityRevision
	if got := contract.MissingCapabilities(config); got != want {
		t.Fatalf("MissingCapabilities() = %v, want %v", methodkind.CapabilityNames(got), methodkind.CapabilityNames(want))
	}

	snap, ok := methodkind.Lookup("snap")
	if !ok {
		t.Fatal("snap contract missing")
	}
	if got := snap.RequestedCapabilities(map[string]any{"branch": "edge/fix"}); got != methodkind.CapabilityChannel {
		t.Fatalf("snap branch RequestedCapabilities() = %v, want [channel]", methodkind.CapabilityNames(got))
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

func TestVersionIntentCapabilities(t *testing.T) {
	tests := []struct {
		name string
		in   plan.VersionIntent
		want methodkind.Capability
	}{
		{name: "latest", in: plan.VersionIntent{Mode: plan.VersionLatest}, want: 0},
		{name: "exact", in: plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}, want: methodkind.CapabilityExactVersion},
		{name: "constraint", in: plan.VersionIntent{Mode: plan.VersionConstraint, Value: ">=1,<2"}, want: methodkind.CapabilityVersionConstraint},
		{name: "channel", in: plan.VersionIntent{Mode: plan.VersionChannel, Channel: &plan.ChannelSelector{Name: "stable"}}, want: methodkind.CapabilityChannel},
		{name: "git-tag", in: plan.VersionIntent{Mode: plan.VersionGitTag, Value: "v1"}, want: methodkind.CapabilityRevision},
		{name: "git-branch", in: plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "main"}, want: methodkind.CapabilityRevision},
		{name: "git-revision", in: plan.VersionIntent{Mode: plan.VersionGitRevision, Value: "abc123"}, want: methodkind.CapabilityRevision},
		{name: "container-tag", in: plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "1.2"}, want: methodkind.CapabilityMutableTag},
		{name: "digest", in: plan.VersionIntent{Mode: plan.VersionDigest, Value: "sha256:abc"}, want: methodkind.CapabilityImmutableIdentity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := methodkind.VersionIntentCapabilities(tt.in)
			if err != nil {
				t.Fatalf("VersionIntentCapabilities() error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("VersionIntentCapabilities() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionIntentCapabilityFiltering(t *testing.T) {
	contract := func(kind string) methodkind.Contract {
		for _, c := range methodkind.Contracts {
			if c.Kind == kind {
				return c
			}
		}
		t.Fatalf("contract %q not found", kind)
		return methodkind.Contract{}
	}

	tests := []struct {
		name    string
		kind    string
		intent  plan.VersionIntent
		missing methodkind.Capability
	}{
		{name: "latest-is-universal", kind: "native", intent: plan.VersionIntent{Mode: plan.VersionLatest}},
		{name: "go-exact", kind: "go", intent: plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}},
		{name: "native-exact-rejected", kind: "native", intent: plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}, missing: methodkind.CapabilityExactVersion},
		{name: "snap-channel", kind: "snap", intent: plan.VersionIntent{Mode: plan.VersionChannel, Channel: &plan.ChannelSelector{Name: "stable"}}},
		{name: "git-revision", kind: "git", intent: plan.VersionIntent{Mode: plan.VersionGitRevision, Value: "abc123"}},
		{name: "container-tag", kind: "container", intent: plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "latest"}},
		{name: "container-digest", kind: "container", intent: plan.VersionIntent{Mode: plan.VersionDigest, Value: "sha256:abc"}},
		{name: "constraint-fails-closed-until-declared", kind: "npm", intent: plan.VersionIntent{Mode: plan.VersionConstraint, Value: "^1.2.0"}, missing: methodkind.CapabilityVersionConstraint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := contract(tt.kind).MissingVersionCapabilities(tt.intent)
			if err != nil {
				t.Fatalf("MissingVersionCapabilities() error: %v", err)
			}
			if got != tt.missing {
				t.Fatalf("MissingVersionCapabilities() = %v, want %v", got, tt.missing)
			}
		})
	}
}

func TestVersionIntentCapabilitiesRejectInvalidIntent(t *testing.T) {
	if _, err := methodkind.VersionIntentCapabilities(plan.VersionIntent{Mode: plan.VersionExact}); err == nil {
		t.Fatal("VersionIntentCapabilities() accepted exact intent without a value")
	}
}

func TestPortableScopeMappings(t *testing.T) {
	tests := []struct {
		kind       string
		portable   plan.Scope
		adapter    string
		normalized string
	}{
		{kind: "winget", portable: plan.ScopeUser, adapter: "user", normalized: "user"},
		{kind: "winget", portable: plan.ScopeSystem, adapter: "machine", normalized: "system"},
		{kind: "scoop", portable: plan.ScopeSystem, adapter: "global", normalized: "system"},
		{kind: "pipx", portable: plan.ScopeSystem, adapter: "global", normalized: "system"},
		{kind: "flatpak", portable: plan.ScopeSystem, adapter: "system", normalized: "system"},
		{kind: "gem", portable: plan.ScopeUser, adapter: "user", normalized: "user"},
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+string(tt.portable), func(t *testing.T) {
			contract, ok := methodkind.Lookup(tt.kind)
			if !ok {
				t.Fatalf("missing contract %q", tt.kind)
			}
			gotAdapter, err := contract.AdapterScope(tt.portable)
			if err != nil {
				t.Fatalf("AdapterScope(%q): %v", tt.portable, err)
			}
			if gotAdapter != tt.adapter {
				t.Fatalf("AdapterScope(%q) = %q, want %q", tt.portable, gotAdapter, tt.adapter)
			}
			gotScope, err := contract.NormalizeScope(tt.adapter)
			if err != nil {
				t.Fatalf("NormalizeScope(%q): %v", tt.adapter, err)
			}
			if string(gotScope) != tt.normalized {
				t.Fatalf("NormalizeScope(%q) = %q, want %q", tt.adapter, gotScope, tt.normalized)
			}
		})
	}
}

func TestPortableScopeSupportFailsClosed(t *testing.T) {
	gem, ok := methodkind.Lookup("gem")
	if !ok {
		t.Fatal("gem contract missing")
	}
	if gem.SupportsScope(plan.ScopeSystem) {
		t.Fatal("gem system scope must not be inferred from manager-specific default")
	}
	if _, err := gem.NormalizeScope("default"); err == nil {
		t.Fatal("gem default unexpectedly normalized to a portable scope")
	}
	if _, err := gem.AdapterScope(plan.ScopeSystem); err == nil {
		t.Fatal("gem system scope unexpectedly supported")
	}

	native, ok := methodkind.Lookup("native")
	if !ok {
		t.Fatal("native contract missing")
	}
	if _, err := native.AdapterScope(plan.ScopeUser); err == nil {
		t.Fatal("native portable scope unexpectedly supported")
	}
}

func TestScopeCapabilitiesHavePortableMappings(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		if !contract.Supports(methodkind.CapabilityScope) {
			if contract.Scopes != nil {
				t.Errorf("%s has scope mappings without CapabilityScope", contract.Kind)
			}
			continue
		}
		if contract.Scopes == nil || len(contract.Scopes.AdapterValues) == 0 {
			t.Errorf("%s declares CapabilityScope without portable mappings", contract.Kind)
			continue
		}
		field, ok := contract.Fields["scope"]
		if !ok {
			t.Errorf("%s declares CapabilityScope without scope field", contract.Kind)
			continue
		}
		for portable, adapterValue := range contract.Scopes.AdapterValues {
			if err := portable.Validate(); err != nil {
				t.Errorf("%s maps invalid portable scope %q: %v", contract.Kind, portable, err)
			}
			if !slices.Contains(field.Enum, adapterValue) {
				t.Errorf("%s maps %q to adapter value %q absent from scope enum %v", contract.Kind, portable, adapterValue, field.Enum)
			}
		}
	}
}

func TestSourceCapabilitiesAreIndependentAndFailClosed(t *testing.T) {
	sources := []plan.SourceReference{{
		Role:  plan.SourceHostConfiguration,
		Name:  "corp",
		URL:   "https://packages.example.test/index",
		Owned: true,
		Trust: &plan.SourceTrust{Fingerprint: "SHA256:abc"},
		SecretRef: &plan.SecretReference{
			Provider: "env",
			Name:     "CORP_TOKEN",
		},
	}}
	got, err := methodkind.SourceCapabilities(sources)
	if err != nil {
		t.Fatalf("SourceCapabilities() error: %v", err)
	}
	want := methodkind.CapabilitySourceSelection |
		methodkind.CapabilitySourceMutation |
		methodkind.CapabilitySourceTrust |
		methodkind.CapabilityAuth
	if got != want {
		t.Fatalf("SourceCapabilities() = %v (%v), want %v (%v)", got, methodkind.CapabilityNames(got), want, methodkind.CapabilityNames(want))
	}
}

func TestMissingPlanCapabilitiesUsesResolvedSemantics(t *testing.T) {
	p := plan.New("tool", "fixture", true)
	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}
	p.Identity.Scope = string(plan.ScopeUser)
	p.Sources = []plan.SourceReference{{
		Role: plan.SourceRegistry,
		Name: "corp",
		SecretRef: &plan.SecretReference{
			Provider: "env",
			Name:     "CORP_TOKEN",
		},
	}}

	contract := methodkind.Contract{
		Kind:         "fixture",
		Capabilities: methodkind.CapabilityExactVersion | methodkind.CapabilitySourceSelection,
	}
	missing, err := contract.MissingPlanCapabilities(p)
	if err != nil {
		t.Fatalf("MissingPlanCapabilities() error: %v", err)
	}
	want := methodkind.CapabilityScope | methodkind.CapabilityAuth
	if missing != want {
		t.Fatalf("missing = %v (%v), want %v (%v)", missing, methodkind.CapabilityNames(missing), want, methodkind.CapabilityNames(want))
	}
}

func TestCapabilityNamesIncludesSourceSecurityCapabilities(t *testing.T) {
	caps := methodkind.CapabilitySourceMutation | methodkind.CapabilitySourceTrust | methodkind.CapabilityAuth
	got := methodkind.CapabilityNames(caps)
	want := []string{"source-mutation", "source-trust", "auth"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CapabilityNames() = %v, want %v", got, want)
	}
}

func TestPlanCapabilitiesIncludesLifecycleArbitraryCode(t *testing.T) {
	cases := []struct {
		name string
		edit func(*plan.ResolvedInstallPlan)
	}{
		{
			name: "hook",
			edit: func(p *plan.ResolvedInstallPlan) {
				p.Hooks = []plan.LifecycleHook{{
					ID: "pre", Transition: plan.TransitionInstall, Timing: plan.HookBefore,
					Operation:     plan.Operation{Kind: "hook", Effect: plan.EffectMutation, Command: []string{"tool"}, ArbitraryCode: true},
					FailurePolicy: plan.HookFailAbort,
				}}
			},
		},
		{
			name: "ensure",
			edit: func(p *plan.ResolvedInstallPlan) {
				p.Ensures = []plan.EnsureAction{{
					ID: "state", Resource: "state:tool",
					Check: plan.Operation{Kind: "check", Effect: plan.EffectReadOnly},
					Apply: plan.Operation{Kind: "apply", Effect: plan.EffectMutation, ArbitraryCode: true},
				}}
			},
		},
		{
			name: "preparation",
			edit: func(p *plan.ResolvedInstallPlan) {
				p.Preparation = &plan.PreparationPlan{Commit: []plan.Operation{{Kind: "commit", Effect: plan.EffectMutation, ArbitraryCode: true}}}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := plan.New("tool", "git", true)
			tc.edit(&p)
			got, err := methodkind.PlanCapabilities(p)
			if err != nil {
				t.Fatal(err)
			}
			if got&methodkind.CapabilityArbitraryCode == 0 {
				t.Fatalf("PlanCapabilities() = %v, want arbitrary-code", got)
			}
		})
	}
}

func TestLifecycleCapabilities(t *testing.T) {
	tests := []struct {
		transition plan.TransitionKind
		want       methodkind.Capability
	}{
		{transition: plan.TransitionInstall, want: 0},
		{transition: plan.TransitionRepair, want: methodkind.CapabilityCheck},
		{transition: plan.TransitionRemove, want: methodkind.CapabilityRemove},
		{transition: plan.TransitionUpgrade, want: methodkind.CapabilityUpgrade},
	}
	for _, tt := range tests {
		t.Run(string(tt.transition), func(t *testing.T) {
			got, err := methodkind.LifecycleCapabilities(tt.transition)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("LifecycleCapabilities(%q) = %v, want %v", tt.transition, got, tt.want)
			}
		})
	}
	if _, err := methodkind.LifecycleCapabilities(plan.TransitionKind("future")); err == nil {
		t.Fatal("unknown lifecycle transition should fail closed")
	}
}

func TestLifecycleCapabilityMetadataMatchesAdapterContract(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		if !contract.Supports(methodkind.CapabilityCheck) {
			t.Errorf("%s lacks CapabilityCheck although Check is mandatory on Adapter", contract.Kind)
		}
		if got := contract.Supports(methodkind.CapabilityRemove); got != contract.CanRemove {
			t.Errorf("%s CapabilityRemove=%t, CanRemove=%t", contract.Kind, got, contract.CanRemove)
		}
		if got := contract.Supports(methodkind.CapabilityUpgrade); got != contract.CanRemove {
			t.Errorf("%s CapabilityUpgrade=%t, CanRemove=%t; current upgrade is remove+install", contract.Kind, got, contract.CanRemove)
		}
	}
}

func TestImmutableLockCapabilityIsConservativeAndDerivedFromPinMechanisms(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		hasPinMechanism := contract.Supports(methodkind.CapabilityExactVersion) ||
			contract.Supports(methodkind.CapabilityRevision) ||
			contract.Supports(methodkind.CapabilityImmutableIdentity)
		if _, ok := contract.Fields["checksum"]; ok {
			hasPinMechanism = true
		}
		if got := contract.Supports(methodkind.CapabilityImmutableLock); got != hasPinMechanism {
			t.Errorf("%s CapabilityImmutableLock=%t, pin mechanism=%t", contract.Kind, got, hasPinMechanism)
		}
	}
}

func TestLocalArtifactCapabilityIsNotAdvertisedBeforeP217(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		if contract.Supports(methodkind.CapabilityLocalArtifact) {
			t.Errorf("%s advertises local-artifact before local path sources are implemented", contract.Kind)
		}
	}
}

func TestLockCapabilities(t *testing.T) {
	if got := methodkind.LockCapabilities(false); got != 0 {
		t.Fatalf("LockCapabilities(false) = %v, want 0", got)
	}
	if got := methodkind.LockCapabilities(true); got != methodkind.CapabilityImmutableLock {
		t.Fatalf("LockCapabilities(true) = %v, want immutable-lock", got)
	}
}

func TestCapabilityNamesIncludeLifecycleAndReproducibility(t *testing.T) {
	mask := methodkind.CapabilityCheck |
		methodkind.CapabilityRemove |
		methodkind.CapabilityUpgrade |
		methodkind.CapabilityImmutableLock |
		methodkind.CapabilityLocalArtifact
	got := methodkind.CapabilityNames(mask)
	want := []string{"check", "remove", "upgrade", "immutable-lock", "local-artifact"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CapabilityNames = %v, want %v", got, want)
	}
}

func TestPlanCapabilitiesRequireLocalArtifactExplicitly(t *testing.T) {
	p := minimalResolvedPlan(t)
	p.Artifacts = []plan.Artifact{{LocalPath: "vendor/tool.tar.gz", Checksum: "sha256:abc"}}
	got, err := methodkind.PlanCapabilities(p)
	if err != nil {
		t.Fatal(err)
	}
	if got&methodkind.CapabilityLocalArtifact == 0 {
		t.Fatalf("PlanCapabilities = %v; want local-artifact", methodkind.CapabilityNames(got))
	}
	contract, _ := methodkind.Lookup("http")
	missing, err := contract.MissingPlanCapabilities(p)
	if err != nil {
		t.Fatal(err)
	}
	if missing&methodkind.CapabilityLocalArtifact == 0 {
		t.Fatalf("http missing = %v; local artifact must fail closed until P2.17", methodkind.CapabilityNames(missing))
	}
}

func TestRequiredCapabilitiesCombinePlanLifecycleAndLockPolicy(t *testing.T) {
	p := minimalResolvedPlan(t)
	got, err := methodkind.RequiredCapabilities(p, methodkind.CandidateRequirements{
		Transition:           plan.TransitionUpgrade,
		RequireImmutableLock: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := methodkind.CapabilityUpgrade | methodkind.CapabilityImmutableLock
	if got&want != want {
		t.Fatalf("RequiredCapabilities = %v, want at least %v", methodkind.CapabilityNames(got), methodkind.CapabilityNames(want))
	}
}

func TestMissingRequirementsEliminatesLifecycleMismatch(t *testing.T) {
	p := minimalResolvedPlan(t)
	contract, _ := methodkind.Lookup("android") // check/install only; no remover today
	missing, err := contract.MissingRequirements(p, methodkind.CandidateRequirements{Transition: plan.TransitionUpgrade})
	if err != nil {
		t.Fatal(err)
	}
	if missing&methodkind.CapabilityUpgrade == 0 {
		t.Fatalf("missing = %v, want upgrade", methodkind.CapabilityNames(missing))
	}
}

func TestSchemaReferenceMethodTableMatchesContracts(t *testing.T) {
	data, err := os.ReadFile("../../docs/schema-reference.md")
	if err != nil {
		t.Fatalf("read schema reference: %v", err)
	}

	const tableHeader = "| Method | What it installs | Example |"
	const nextHeading = "### Ecosystem desired-state coverage"
	doc := string(data)
	start := strings.Index(doc, tableHeader)
	if start < 0 {
		t.Fatalf("schema reference is missing method table header %q", tableHeader)
	}
	section := doc[start+len(tableHeader):]
	end := strings.Index(section, nextHeading)
	if end < 0 {
		t.Fatalf("schema reference is missing heading %q after method table", nextHeading)
	}
	section = section[:end]

	documented := map[string]int{}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		rest := strings.TrimPrefix(line, "| `")
		cut := strings.Index(rest, "`")
		if cut <= 0 {
			continue
		}
		documented[rest[:cut]]++
	}

	want := make(map[string]bool, len(methodkind.Contracts))
	for _, contract := range methodkind.Contracts {
		want[contract.Kind] = true
	}

	var missing, extra, duplicate []string
	for kind := range want {
		if documented[kind] == 0 {
			missing = append(missing, kind)
		}
	}
	for kind, count := range documented {
		if !want[kind] {
			extra = append(extra, kind)
		}
		if count > 1 {
			duplicate = append(duplicate, kind)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	slices.Sort(duplicate)
	if len(missing) > 0 || len(extra) > 0 || len(duplicate) > 0 {
		t.Fatalf("schema-reference method table drift: missing=%v extra=%v duplicate=%v", missing, extra, duplicate)
	}
}

func minimalResolvedPlan(t *testing.T) plan.ResolvedInstallPlan {
	t.Helper()
	p := plan.New("demo", "http", true)
	if err := p.Validate(); err != nil {
		t.Fatalf("minimal plan invalid: %v", err)
	}
	return p
}
