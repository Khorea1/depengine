package methodkind_test

import (
	"fmt"
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
