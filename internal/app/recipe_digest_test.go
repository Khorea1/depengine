package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
)

func writeCatalogRecipe(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name+".toml")
	if !strings.HasPrefix(body, "recipe_version") {
		body = "recipe_version = 1\n" + body
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func catalogRecipeBody(url, checksum string) string {
	return "[packages.%s]\nmethod_only = [\"http\"]\n[packages.%s.http]\nurl = \"" + url + "\"\nchecksum = \"" + checksum + "\"\n"
}

// TestRunUpdateProfileRefreshesRetainedEntryOnRecipeChange is the regression
// test for a retained --profile entry whose catalog recipe changed. Method and
// source hashes stay identical (only the URL/checksum changed), so recipe
// content digest is the only signal that the retained entry is stale.
func TestRunUpdateProfileRefreshesRetainedEntryOnRecipeChange(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	catalogRoot := filepath.Join(dir, "catalog")
	if err := os.Mkdir(catalogRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	oldChecksum := "sha256:" + strings.Repeat("a", 64)
	newChecksum := "sha256:" + strings.Repeat("b", 64)
	profiledChecksum := "sha256:" + strings.Repeat("c", 64)

	writeCatalogRecipe(t, catalogRoot, "profiled", strings.ReplaceAll(catalogRecipeBody("https://example.com/profiled", profiledChecksum), "%s", "profiled"))
	writeCatalogRecipe(t, catalogRoot, "outside", strings.ReplaceAll(catalogRecipeBody("https://example.com/outside-v1", oldChecksum), "%s", "outside"))

	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n[tools.profiled]\ntags = [\"dev\"]\n\n[tools.outside]\ntags = [\"other\"]\n")

	catalog := config.NewCatalog([]string{catalogRoot})
	schema, err := config.ParseProjectSchema(schemaPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := config.ResolveLayers(schema, nil, catalog, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	oldRecipe, err := catalog.Lookup("outside")
	if err != nil {
		t.Fatal(err)
	}
	profiledRecipe, err := catalog.Lookup("profiled")
	if err != nil {
		t.Fatal(err)
	}
	methodsHash, sourceHash, err := lock.SnapshotIntentMetadata(resolved.Schema)
	if err != nil {
		t.Fatal(err)
	}

	seeded := func(name, url, checksum string) plan.ResolvedInstallPlan {
		p := plan.New(name, "http", true)
		p.Identity.Version = "9.9.9"
		p.Artifacts = []plan.Artifact{{Kind: plan.ArtifactArchive, URL: url, Checksum: checksum}}
		return p
	}
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{
		seeded("profiled", "https://example.com/profiled", profiledChecksum),
		seeded("outside", "https://example.com/outside-v1", oldChecksum),
	})
	if err != nil {
		t.Fatal(err)
	}
	previous := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{}}
	if err := previous.SetProjection(document); err != nil {
		t.Fatal(err)
	}
	previous.MethodsHash = methodsHash
	previous.SourceHash = sourceHash
	if err := previous.SetRecipeDigests(map[string]string{
		"profiled": profiledRecipe.Digest,
		"outside":  oldRecipe.Digest,
	}); err != nil {
		t.Fatal(err)
	}
	lockPath := lock.DefaultPath(schemaPath)
	if err := lock.Save(lockPath, previous); err != nil {
		t.Fatal(err)
	}

	// Change only the retained tool's recipe content: same method kind, same
	// source identity, new URL and checksum.
	writeCatalogRecipe(t, catalogRoot, "outside", strings.ReplaceAll(catalogRecipeBody("https://example.com/outside-v2", newChecksum), "%s", "outside"))

	profile := "dev"
	noManifest, frozen, dryRun, verbose := true, false, false, false
	manifest, lockFlag := "", ""
	if err := runUpdate(context.Background(), &schemaPath, &manifest, &noManifest, &lockFlag, &profile, &frozen, &dryRun, &verbose, catalog); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Version != lock.CurrentVersion {
		t.Fatalf("lock = %#v, want v%d", got, lock.CurrentVersion)
	}
	newRecipe, err := catalog.Lookup("outside")
	if err != nil {
		t.Fatal(err)
	}
	if newRecipe.Digest == oldRecipe.Digest {
		t.Fatal("test fixture did not change the recipe digest")
	}
	if got.RecipeDigest["outside"] != newRecipe.Digest {
		t.Fatalf("outside recipe digest = %q, want refreshed %q", got.RecipeDigest["outside"], newRecipe.Digest)
	}
	if got.RecipeDigest["profiled"] != profiledRecipe.Digest {
		t.Fatalf("profiled recipe digest = %q, want unchanged %q", got.RecipeDigest["profiled"], profiledRecipe.Digest)
	}
	projection, err := got.ProjectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := projection.EntryForTool("outside")
	if !ok {
		t.Fatal("outside entry missing from refreshed projection")
	}
	if len(entry.Identity.Artifacts) != 1 || entry.Identity.Artifacts[0].URL != "https://example.com/outside-v2" {
		t.Fatalf("outside artifacts = %#v, want the new recipe URL", entry.Identity.Artifacts)
	}
}
