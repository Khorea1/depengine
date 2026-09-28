package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

// manifestFixture returns a manifest-layer fixture exercising the merge,
// gating, and fail-closed paths of --manifest handling through the real CLI.
// This is the adversarial/invalid half of the P3 fixture matrix: the schema
// layer already has broad testdata coverage under internal/validate/testdata,
// while the personal-manifest layer previously had only unit coverage.
func manifestFixture(name string) string {
	return filepath.Join(findModuleRoot(), "tests", "integration", "testdata", "manifest", name)
}

func manifestSchema() string {
	return manifestFixture("schema.toml")
}

func TestValidate_ManifestBadTOML(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("adversarial-bad-toml.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "error loading manifest") {
		t.Errorf("expected manifest load failure, got:\n%s", output)
	}
}

func TestValidate_ManifestProjectSection(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("adversarial-project-section.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "unknown root field") {
		t.Errorf("expected unknown-root rejection, got:\n%s", output)
	}
}

func TestValidate_ManifestUnknownRoot(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("adversarial-unknown-root.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "unknown root field") {
		t.Errorf("expected unknown-root rejection, got:\n%s", output)
	}
}

func TestValidate_ManifestWrongVersion(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("adversarial-wrong-version.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "unsupported version") {
		t.Errorf("expected version-gate rejection, got:\n%s", output)
	}
}

func TestValidate_ManifestMissingVersion(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("adversarial-missing-version.toml"))
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; output:\n%s", code, output)
	}
	if !strings.Contains(output, "schema_version") {
		t.Errorf("expected version-gate rejection, got:\n%s", output)
	}
}

func TestValidate_ManifestNewToolBlockedByDefault(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("newtool-blocked.toml"))
	if code != 0 {
		t.Fatalf("expected exit 0 (tool silently ignored), got %d; output:\n%s", code, output)
	}
	whyOut, whyCode := runDepengine("why", "newtool", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("newtool-blocked.toml"))
	if whyCode == 0 {
		t.Fatalf("expected why to reject the unlisted tool, got exit 0; output:\n%s", whyOut)
	}
	if !strings.Contains(whyOut, "not found in schema") {
		t.Errorf("expected unknown-tool rejection, got:\n%s", whyOut)
	}
}

func TestValidate_ManifestNewToolAllowedWithOptIn(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("newtool-allowed.toml"))
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, output)
	}
	whyOut, whyCode := runDepengine("why", "newtool", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("newtool-allowed.toml"))
	if whyCode != 0 {
		t.Fatalf("expected why to resolve the opted-in tool, got exit %d; output:\n%s", whyCode, whyOut)
	}
	if !strings.Contains(whyOut, "Why newtool?") {
		t.Errorf("expected candidate inspection, got:\n%s", whyOut)
	}
}

func TestValidate_ManifestOverrideMerges(t *testing.T) {
	output, code := runDepengine("validate", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("override-merges.toml"))
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, output)
	}
	whyOut, whyCode := runDepengine("why", "curl", "--no-manifest", "--schema", manifestSchema(), "--manifest", manifestFixture("override-merges.toml"), "--fields")
	if whyCode != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", whyCode, whyOut)
	}
	if !strings.Contains(whyOut, "Methods: source=both") {
		t.Errorf("expected merged method provenance, got:\n%s", whyOut)
	}
}
