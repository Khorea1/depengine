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
