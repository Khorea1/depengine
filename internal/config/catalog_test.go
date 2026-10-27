package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeRecipe(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, name+".toml")
	if !strings.Contains(body, "recipe_version") {
		body = "recipe_version = 1\n" + body
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write recipe %s: %v", path, err)
	}
	return path
}

func TestValidateCatalogName(t *testing.T) {
	valid := []string{
		"a", "foo", "Foo", "foo-bar", "foo_bar", "foo.bar", "foo+bar",
		strings.Repeat("a", 128),
	}
	for _, name := range valid {
		if err := ValidateCatalogName(name); err != nil {
			t.Errorf("ValidateCatalogName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"", ".foo", "-foo", "foo/bar", "foo\\bar", "foo:bar", "foo bar",
		"foo.", "CON", "con.txt", "COM1", "LPT9", "foo..", "fóo", "foo@bar",
		strings.Repeat("a", 129),
	}
	for _, name := range invalid {
		if err := ValidateCatalogName(name); err == nil {
			t.Errorf("ValidateCatalogName(%q) = nil, want error", name)
		}
	}
}

func TestCatalogBaseDir(t *testing.T) {
	cases := []struct {
		name    string
		goos    string
		home    string
		xdg     string
		appData string
		want    string
	}{
		{"linux xdg", "linux", "/home/u", "/srv/config", "", "/srv/config"},
		{"linux relative xdg falls back", "linux", "/home/u", "relative", "", "/home/u/.config"},
		{"linux home only", "linux", "/home/u", "", "", "/home/u/.config"},
		{"linux no roots", "linux", "", "", "", ""},
		{"darwin", "darwin", "/Users/u", "", "", "/Users/u/Library/Application Support"},
		{"darwin no home", "darwin", "", "", "", ""},
		{"windows", "windows", "", "", "/appdata", "/appdata"},
		{"windows relative", "windows", "", "", "appdata", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := catalogBaseDir(tc.goos, tc.home, tc.xdg, tc.appData); got != tc.want {
				t.Fatalf("catalogBaseDir() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveCatalogRoots(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if roots, err := ResolveCatalogRoots(nil, true); err != nil || roots != nil {
		t.Fatalf("no-catalog = %v, %v; want nil, nil", roots, err)
	}
	if _, err := ResolveCatalogRoots([]string{dir}, true); err == nil {
		t.Fatal("--catalog with --no-catalog must fail")
	}
	if _, err := ResolveCatalogRoots([]string{"relative"}, false); err == nil {
		t.Fatal("relative root must fail")
	}
	if _, err := ResolveCatalogRoots([]string{filepath.Join(dir, "missing")}, false); err == nil {
		t.Fatal("missing explicit root must fail")
	}
	if _, err := ResolveCatalogRoots([]string{file}, false); err == nil {
		t.Fatal("explicit root that is a file must fail")
	}
	roots, err := ResolveCatalogRoots([]string{dir}, false)
	if err != nil || len(roots) != 1 || roots[0] != dir {
		t.Fatalf("explicit root = %v, %v; want [%s]", roots, err, dir)
	}

	other := t.TempDir()
	t.Setenv(CatalogEnv, dir+string(os.PathListSeparator)+other)
	roots, err = ResolveCatalogRoots(nil, false)
	if err != nil || len(roots) != 2 || roots[0] != dir || roots[1] != other {
		t.Fatalf("env roots = %v, %v; want [%s %s]", roots, err, dir, other)
	}
}

func TestResolveCatalogRootsDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(CatalogEnv, "")
	roots, err := ResolveCatalogRoots(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return
	}
	want := filepath.Join(home, ".config", "depengine", "catalog")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("default roots = %v, want [%s]", roots, want)
	}
}

func TestCatalogLookup(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "foo", "[packages.foo]\nnative = \"foo-pkg\"\n")
	cat := NewCatalog([]string{root})

	rec, err := cat.Lookup("foo")
	if err != nil {
		t.Fatalf("Lookup(foo): %v", err)
	}
	if rec.Name != "foo" || rec.RootIndex != 0 || rec.Path != filepath.Join(root, "foo.toml") {
		t.Fatalf("recipe = %+v", rec)
	}
	if rec.Schema.Tools["foo"] == nil {
		t.Fatal("recipe schema is missing foo")
	}

	if _, err := cat.Lookup("bar"); !errors.Is(err, ErrRecipeNotFound) {
		t.Fatalf("Lookup(bar) = %v, want ErrRecipeNotFound", err)
	}
	if _, err := cat.Lookup("foo/bar"); !errors.Is(err, ErrRecipeNotAddressable) {
		t.Fatalf("Lookup(foo/bar) = %v, want ErrRecipeNotAddressable", err)
	}
	if _, err := NewCatalog(nil).Lookup("foo"); !errors.Is(err, ErrRecipeNotFound) {
		t.Fatalf("empty catalog Lookup = %v, want ErrRecipeNotFound", err)
	}
}

func TestCatalogLookupFailures(t *testing.T) {
	t.Run("name mismatch", func(t *testing.T) {
		root := t.TempDir()
		writeRecipe(t, root, "foo", "[packages.bar]\nnative = \"bar\"\n")
		_, err := NewCatalog([]string{root}).Lookup("foo")
		if err == nil || !strings.Contains(err.Error(), "declares") {
			t.Fatalf("error = %v, want name mismatch", err)
		}
	})
	t.Run("invalid toml", func(t *testing.T) {
		root := t.TempDir()
		writeRecipe(t, root, "foo", "[packages.foo\n")
		_, err := NewCatalog([]string{root}).Lookup("foo")
		if err == nil || !strings.Contains(err.Error(), "parse TOML") {
			t.Fatalf("error = %v, want TOML parse error", err)
		}
	})
	t.Run("missing version", func(t *testing.T) {
		root := t.TempDir()
		writeRecipe(t, root, "foo", "recipe_version = 0\n[packages.foo]\nnative = \"foo\"\n")
		_, err := NewCatalog([]string{root}).Lookup("foo")
		if err == nil || !strings.Contains(err.Error(), "unsupported version") {
			t.Fatalf("error = %v, want version error", err)
		}
	})
	t.Run("schema_version rejected", func(t *testing.T) {
		root := t.TempDir()
		writeRecipe(t, root, "foo", "schema_version = 1\n[packages.foo]\nnative = \"foo\"\n")
		_, err := NewCatalog([]string{root}).Lookup("foo")
		if err == nil || !strings.Contains(err.Error(), "unknown root field") {
			t.Fatalf("error = %v, want unknown root field", err)
		}
	})
	t.Run("manifest table rejected", func(t *testing.T) {
		root := t.TempDir()
		writeRecipe(t, root, "foo", "[manifest]\nallow_new_tools = true\n[packages.foo]\nnative = \"foo\"\n")
		_, err := NewCatalog([]string{root}).Lookup("foo")
		if err == nil || !strings.Contains(err.Error(), "unknown root field") {
			t.Fatalf("error = %v, want unknown root field", err)
		}
	})
	t.Run("directory at recipe path", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "foo.toml"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := NewCatalog([]string{root}).Lookup("foo")
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("error = %v, want non-regular file error", err)
		}
	})
	t.Run("symlink recipe", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		root := t.TempDir()
		real := writeRecipe(t, root, "real", "[packages.foo]\nnative = \"foo\"\n")
		if err := os.Symlink(real, filepath.Join(root, "foo.toml")); err != nil {
			t.Fatal(err)
		}
		_, err := NewCatalog([]string{root}).Lookup("foo")
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v, want symlink error", err)
		}
	})
}

func TestCatalogFirstRootWins(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeRecipe(t, first, "foo", "[packages.foo]\nnative = \"first\"\n")
	writeRecipe(t, second, "foo", "[packages.foo]\nnative = \"second\"\n")
	rec, err := NewCatalog([]string{first, second}).Lookup("foo")
	if err != nil {
		t.Fatal(err)
	}
	pkg, _ := rec.Schema.Tools["foo"].Methods[0].Config["pkg"].(string)
	if pkg != "first" {
		t.Fatalf("pkg = %q, want first", pkg)
	}
}

func TestRecipeSpecializeExpandsHostPlaceholders(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "foo", "[packages.foo]\nhttp = { url = \"https://example.test/{arch}/foo\", checksum = \"sha256:abc\" }\n")
	rec, err := NewCatalog([]string{root}).Lookup("foo")
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := rec.Schema.Tools["foo"].Methods[0].Config["url"].(string)
	if !strings.Contains(canonical, "{arch}") {
		t.Fatalf("canonical url = %q, want literal {arch}", canonical)
	}
	specialized, err := rec.Specialize(map[string]string{"arch": "x86_64", "os": "linux"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := specialized.Tools["foo"].Methods[0].Config["url"].(string)
	if !strings.Contains(got, "x86_64") || strings.Contains(got, "{arch}") {
		t.Fatalf("specialized url = %q, want expanded arch", got)
	}
}

func TestResolveLayersCatalogPrecedence(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "foo", "[packages.foo]\nnative = { pkg = \"catalog-pkg\" }\ntags = [\"catalog\"]\n")
	schema := mustParse(t, writeSchemaInline(t, "[tools]\nfoo = { native = { pkg = \"schema-pkg\" } }\n"), ParseProjectSchema)
	manifest := mustParse(t, writeSchemaInline(t, "[packages]\nfoo = { native = { pkg = \"manifest-pkg\" }, tags = [\"manifest\"] }\n"), ParseManifest)

	res, err := ResolveLayers(schema, manifest, NewCatalog([]string{root}), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.CatalogCount != 1 || res.ManifestCount != 1 {
		t.Fatalf("counts = catalog %d manifest %d, want 1/1", res.CatalogCount, res.ManifestCount)
	}
	pkg, _ := res.Schema.Tools["foo"].Methods[0].Config["pkg"].(string)
	if pkg != "schema-pkg" {
		t.Fatalf("pkg = %q, want schema-pkg", pkg)
	}
	if !containsString(res.Schema.Tools["foo"].Tags, "catalog") || !containsString(res.Schema.Tools["foo"].Tags, "manifest") {
		t.Fatalf("tags = %v, want catalog and manifest", res.Schema.Tools["foo"].Tags)
	}
}

func TestResolveLayersManifestOnlyNameIsNotCatalogResolved(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "bar", "[packages.bar]\nnative = { pkg = \"catalog-bar\" }\n")
	schema := mustParse(t, writeSchemaInline(t, "[tools]\nfoo = { native = \"foo\" }\n"), ParseProjectSchema)
	manifest := mustParse(t, writeSchemaInline(t, "[manifest]\nallow_new_tools = true\n[packages]\nbar = { native = { pkg = \"manifest-bar\" } }\n"), ParseManifest)

	res, err := ResolveLayers(schema, manifest, NewCatalog([]string{root}), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.CatalogCount != 0 {
		t.Fatalf("catalog count = %d, want 0", res.CatalogCount)
	}
	pkg, _ := res.Schema.Tools["bar"].Methods[0].Config["pkg"].(string)
	if pkg != "manifest-bar" {
		t.Fatalf("bar pkg = %q, want manifest-bar", pkg)
	}
}

func TestResolveLayersTransitiveCatalogRequires(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "app", "[packages.app]\nrequires = [\"lib\"]\nnative = \"app-pkg\"\n")
	writeRecipe(t, root, "lib", "[packages.lib]\nnative = \"lib-pkg\"\n")
	schema := mustParse(t, writeSchemaInline(t, "[tools]\napp = {}\n"), ParseProjectSchema)

	res, err := ResolveLayers(schema, nil, NewCatalog([]string{root}), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.CatalogCount != 2 {
		t.Fatalf("catalog count = %d, want 2", res.CatalogCount)
	}
	if res.Schema.Tools["lib"] == nil {
		t.Fatal("transitive lib missing")
	}
	if !res.Schema.Tools["lib"].DependencyOnly {
		t.Fatal("transitive lib must be dependency-only")
	}
	if res.Schema.Tools["app"].DependencyOnly {
		t.Fatal("top-level app must not be dependency-only")
	}
}

func TestResolveLayersMissingTransitiveRequiresFails(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "app", "[packages.app]\nrequires = [\"missing\"]\nnative = \"app-pkg\"\n")
	schema := mustParse(t, writeSchemaInline(t, "[tools]\napp = {}\n"), ParseProjectSchema)

	if _, err := ResolveLayers(schema, nil, NewCatalog([]string{root}), nil, false); err == nil {
		t.Fatal("missing transitive dependency must fail")
	}
}

func mustParse(t *testing.T, path string, parse func(string, map[string]string) (*Schema, error)) *Schema {
	t.Helper()
	s, err := parse(path, nil)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return s
}

func TestResolveLayersCatalogRequiresOverriddenBySchema(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "app", "[packages.app]\nrequires = [\"missing\"]\nnative = \"app-pkg\"\n")
	schema := mustParse(t, writeSchemaInline(t, "[tools]\napp = { requires = [], native = \"app-pkg\" }\n"), ParseProjectSchema)

	res, err := ResolveLayers(schema, nil, NewCatalog([]string{root}), nil, false)
	if err != nil {
		t.Fatalf("schema-overridden requires must not require the catalog dependency: %v", err)
	}
	if got := res.Schema.Tools["app"].Requires; len(got) != 0 {
		t.Fatalf("requires = %v, want empty", got)
	}
}
func TestRecipeDigestIsCanonicalAndHostIndependent(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "foo", "[packages.foo]\nhttp = { url = \"https://example.test/{arch}/foo-{os}.tar.gz\", checksum = \"sha256:abc\" }\n")
	cat := NewCatalog([]string{root})
	first, err := cat.Lookup("foo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Digest, "sha256:") || len(first.Digest) != len("sha256:")+64 {
		t.Fatalf("digest = %q, want sha256:<64 hex>", first.Digest)
	}

	// Comments, whitespace and key order do not change the digest.
	writeRecipe(t, root, "foo", "# comment\n\n[packages.foo]\nhttp.checksum = \"sha256:abc\" # trailing\nhttp.url = \"https://example.test/{arch}/foo-{os}.tar.gz\"\n")
	second, err := cat.Lookup("foo")
	if err != nil {
		t.Fatal(err)
	}
	if second.Digest != first.Digest {
		t.Fatalf("digest changed across formatting: %q != %q", second.Digest, first.Digest)
	}

	// Host specialization expands placeholders in the parsed schema but must
	// not change the digest computed from the raw recipe document.
	specialized, err := first.Specialize(map[string]string{"arch": "aarch64", "os": "darwin"})
	if err != nil {
		t.Fatal(err)
	}
	url, _ := specialized.Tools["foo"].Methods[0].Config["url"].(string)
	if strings.ContainsAny(url, "{}") {
		t.Fatalf("specialized url = %q, want host placeholders expanded", url)
	}
	if first.Digest != second.Digest {
		t.Fatal("Specialize mutated the recipe digest")
	}
}

func TestRecipeDigestCoversPlanInvisibleFields(t *testing.T) {
	root := t.TempDir()
	base := "[packages.foo]\nhttp = { url = \"https://example.test/foo.tar.gz\", checksum = \"sha256:abc\""
	writeRecipe(t, root, "foo", base+" }\n")
	cat := NewCatalog([]string{root})
	baseRecipe, err := cat.Lookup("foo")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"url", "[packages.foo]\nhttp = { url = \"https://example.test/other.tar.gz\", checksum = \"sha256:abc\" }\n"},
		{"checksum", "[packages.foo]\nhttp = { url = \"https://example.test/foo.tar.gz\", checksum = \"sha256:def\" }\n"},
		{"extract_to", base + ", extract_to = \"lib\" }\n"},
		{"link_dir", base + ", link_dir = \"~/.local/bin\" }\n"},
		{"entrypoints", base + ", entrypoints = { foo = \"bin/foo\" } }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeRecipe(t, root, "foo", tc.body)
			recipe, err := cat.Lookup("foo")
			if err != nil {
				t.Fatal(err)
			}
			if recipe.Digest == baseRecipe.Digest {
				t.Fatalf("%s change did not change the recipe digest", tc.name)
			}
		})
	}
}

func TestResolveLayersAttachesRecipeDigest(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "foo", "[packages.foo]\nnative = \"foo\"\n")
	schema := mustParse(t, writeSchemaInline(t, "[tools]\nfoo = {}\n"), ParseProjectSchema)
	catalog := NewCatalog([]string{root})

	recipe, err := catalog.Lookup("foo")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLayers(schema, nil, catalog, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Schema.Tools["foo"].Recipe; got == nil || got.Digest != recipe.Digest {
		t.Fatalf("merged recipe origin = %+v, want digest %q", got, recipe.Digest)
	}
}
