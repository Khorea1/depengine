package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func recipeDigestSchema(digest string) *config.Schema {
	return &config.Schema{Tools: map[string]*config.Tool{
		"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{{
				Kind:   "http",
				Config: map[string]any{"url": "https://example.test/tool.tar.gz"},
			}},
			Recipe: &config.RecipeOrigin{Digest: digest},
		},
	}}
}

func TestValidateFrozenV2ChecksRecipeDigest(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)

	t.Run("unchanged digest passes", func(t *testing.T) {
		schema := recipeDigestSchema(digestA)
		frozen := frozenV2TestLock(t, schema, "tool")
		if err := frozen.SetRecipeDigests(map[string]string{"tool": digestA}); err != nil {
			t.Fatal(err)
		}
		if err := ValidateFrozen(schema, frozen); err != nil {
			t.Fatalf("ValidateFrozen() rejected matching recipe digest: %v", err)
		}
	})

	t.Run("changed recipe content fails closed", func(t *testing.T) {
		schema := recipeDigestSchema(digestA)
		frozen := frozenV2TestLock(t, schema, "tool")
		if err := frozen.SetRecipeDigests(map[string]string{"tool": digestA}); err != nil {
			t.Fatal(err)
		}
		// A URL, checksum or plan-invisible field change all produce a new
		// digest because the digest covers the whole recipe document.
		schema.Tools["tool"].Recipe = &config.RecipeOrigin{Digest: digestB}
		err := ValidateFrozen(schema, frozen)
		if err == nil || !strings.Contains(err.Error(), "recipe content changed for tool \"tool\"") {
			t.Fatalf("ValidateFrozen() error = %v, want recipe content mismatch", err)
		}
	})

	t.Run("missing stored digest fails closed", func(t *testing.T) {
		schema := recipeDigestSchema(digestA)
		frozen := frozenV2TestLock(t, schema, "tool")
		err := ValidateFrozen(schema, frozen)
		if err == nil || !strings.Contains(err.Error(), "missing recipe digest for tool \"tool\"") {
			t.Fatalf("ValidateFrozen() error = %v, want missing digest", err)
		}
	})

	t.Run("tool no longer sourced from a recipe fails closed", func(t *testing.T) {
		schema := recipeDigestSchema(digestA)
		frozen := frozenV2TestLock(t, schema, "tool")
		if err := frozen.SetRecipeDigests(map[string]string{"tool": digestA}); err != nil {
			t.Fatal(err)
		}
		schema.Tools["tool"].Recipe = nil
		err := ValidateFrozen(schema, frozen)
		if err == nil || !strings.Contains(err.Error(), "no longer sourced from a catalog recipe") {
			t.Fatalf("ValidateFrozen() error = %v, want provenance change", err)
		}
	})
}

func TestValidateFrozenV2ChecksVirtualRecipeDigest(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	schema := recipeDigestSchema(digestA)
	schema.Tools["group"] = &config.Tool{
		Name:     "group",
		Requires: []string{"tool"},
		Recipe:   &config.RecipeOrigin{Digest: digestA},
	}
	frozen := frozenV2TestLock(t, schema, "tool")
	if err := frozen.SetRecipeDigests(map[string]string{"tool": digestA, "group": digestA}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFrozen(schema, frozen); err != nil {
		t.Fatalf("ValidateFrozen() rejected matching virtual recipe digest: %v", err)
	}
	schema.Tools["group"].Recipe = &config.RecipeOrigin{Digest: digestB}
	err := ValidateFrozen(schema, frozen)
	if err == nil || !strings.Contains(err.Error(), "recipe content changed for tool \"group\"") {
		t.Fatalf("ValidateFrozen() error = %v, want virtual recipe mismatch", err)
	}
}

func TestValidateCatalogLockVersion(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	schema := recipeDigestSchema(digest)

	if err := ValidateCatalogLockVersion(schema, nil); err == nil || !strings.Contains(err.Error(), "found missing") {
		t.Fatalf("ValidateCatalogLockVersion(nil) error = %v, want missing-lock failure", err)
	}
	if err := ValidateCatalogLockVersion(schema, &Lock{Version: 1}); err == nil || !strings.Contains(err.Error(), "found v1") {
		t.Fatalf("ValidateCatalogLockVersion(v1) error = %v, want v1 failure", err)
	}
	if err := ValidateCatalogLockVersion(schema, &Lock{Version: CurrentVersion}); err != nil {
		t.Fatalf("ValidateCatalogLockVersion(v2) error = %v", err)
	}
	if err := ValidateCatalogLockVersion(&config.Schema{Tools: map[string]*config.Tool{}}, &Lock{Version: 1}); err != nil {
		t.Fatalf("ValidateCatalogLockVersion without catalog tools error = %v", err)
	}
}

func TestValidateFrozenV1RejectsCatalogTools(t *testing.T) {
	schema := recipeDigestSchema("sha256:" + strings.Repeat("a", 64))
	err := ValidateFrozen(schema, &Lock{Version: 1})
	if err == nil || !strings.Contains(err.Error(), "catalog-sourced tools require lock v2") {
		t.Fatalf("ValidateFrozen(v1 + catalog) error = %v, want v2 requirement", err)
	}
}

func TestLoadRejectsRecipeDigestInV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "depengine.lock")
	body := "version = 1\n\n[recipe_digest]\ntool = \"sha256:" + strings.Repeat("a", 64) + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "version 1 cannot contain recipe digests") {
		t.Fatalf("Load(v1 with digest) error = %v, want rejection", err)
	}
}

func TestSetRecipeDigestsValidatesSyntax(t *testing.T) {
	schema := recipeDigestSchema("sha256:" + strings.Repeat("a", 64))
	frozen := frozenV2TestLock(t, schema, "tool")

	if err := frozen.SetRecipeDigests(map[string]string{"tool": "sha256:not-hex"}); err == nil || !strings.Contains(err.Error(), "hexadecimal") {
		t.Fatalf("SetRecipeDigests(malformed) error = %v, want syntax failure", err)
	}
	if err := (&Lock{Version: 1}).SetRecipeDigests(map[string]string{"tool": "sha256:" + strings.Repeat("a", 64)}); err == nil {
		t.Fatal("SetRecipeDigests accepted a v1 lock")
	}
	// A virtual dependency-group recipe has no projection entry but still
	// carries content that must be pinned.
	if err := frozen.SetRecipeDigests(map[string]string{"group": "sha256:" + strings.Repeat("a", 64)}); err != nil {
		t.Fatalf("SetRecipeDigests(virtual tool) error = %v", err)
	}
}

func TestSnapshotRecipeDigests(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	schema := recipeDigestSchema(digest)
	schema.Tools["local"] = &config.Tool{Name: "local", Methods: []*config.MethodCandidate{{Kind: "native"}}}

	got, err := SnapshotRecipeDigests(schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["tool"] != digest {
		t.Fatalf("SnapshotRecipeDigests() = %v, want only the catalog tool", got)
	}
	if names := CatalogToolNames(schema); len(names) != 1 || names[0] != "tool" {
		t.Fatalf("CatalogToolNames() = %v, want [tool]", names)
	}
	schema.Tools["tool"].Recipe = &config.RecipeOrigin{Digest: "sha256:xyz"}
	if _, err := SnapshotRecipeDigests(schema); err == nil {
		t.Fatal("SnapshotRecipeDigests accepted a malformed digest")
	}
}

func TestMergePreservesRecipeDigestForV2(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	schema := recipeDigestSchema(digest)
	existing := frozenV2TestLock(t, schema, "tool")
	if err := existing.SetRecipeDigests(map[string]string{"tool": digest}); err != nil {
		t.Fatal(err)
	}
	fresh := &Lock{Version: 1, Tools: map[string]ToolPin{}}

	merged := Merge(existing, fresh)
	if merged.Version != CurrentVersion {
		t.Fatalf("merged version = %d, want %d", merged.Version, CurrentVersion)
	}
	if merged.RecipeDigest["tool"] != digest {
		t.Fatalf("merged recipe digest = %q, want %q", merged.RecipeDigest["tool"], digest)
	}
}
