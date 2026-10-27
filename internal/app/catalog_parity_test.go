package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
)

func catalogParityFixture(tb testing.TB, n int) (string, *config.Catalog, map[string]string) {
	tb.Helper()
	root := tb.TempDir()
	catalogDir := filepath.Join(root, "catalog")
	if err := os.Mkdir(catalogDir, 0o700); err != nil {
		tb.Fatal(err)
	}
	var manifest strings.Builder
	manifest.WriteString("schema_version = 1\n[packages]\n")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%05d", i)
		body := fmt.Sprintf("native = { pkg = \"pkg-%d\", sources = [{ kind = \"brew-tap\", name = \"repo-%d\", url = \"https://example.test/%d\" }] }", i, i, i)
		recipe := fmt.Sprintf("recipe_version = 1\n[packages.%s]\n%s\n", name, body)
		if err := os.WriteFile(filepath.Join(catalogDir, name+".toml"), []byte(recipe), 0o600); err != nil {
			tb.Fatal(err)
		}
		fmt.Fprintf(&manifest, "%s = { %s }\n", name, body)
	}
	manifestPath := filepath.Join(root, "manifest.toml")
	if err := os.WriteFile(manifestPath, []byte(manifest.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
	return manifestPath, config.NewCatalog([]string{catalogDir}), map[string]string{"arch": "x86_64", "os": "linux"}
}

func TestCatalogLookupParityWithEager(t *testing.T) {
	assertCatalogParity(t, 1000, 20)
}

func TestCatalogLookupParityWithEager100K(t *testing.T) {
	if os.Getenv("DEPENGINE_CATALOG_PARITY_100K") != "1" {
		t.Skip("set DEPENGINE_CATALOG_PARITY_100K=1 to run the 100k eager-parity check")
	}
	assertCatalogParity(t, 100000, 5)
}

func assertCatalogParity(t *testing.T, n, samples int) {
	t.Helper()
	manifestPath, cat, facts := catalogParityFixture(t, n)
	eager, err := config.ParseManifest(manifestPath, facts)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < samples; i++ {
		name := fmt.Sprintf("tool%05d", (i*37)%n)
		rec, err := cat.Lookup(name)
		if err != nil {
			t.Fatalf("Lookup(%s): %v", name, err)
		}
		specialized, err := rec.Specialize(facts)
		if err != nil {
			t.Fatalf("Specialize(%s): %v", name, err)
		}
		want := eager.Tools[name]
		got := specialized.Tools[name]
		if want == nil || got == nil {
			t.Fatalf("%s missing (eager=%v catalog=%v)", name, want != nil, got != nil)
		}
		if len(want.Methods) != len(got.Methods) {
			t.Fatalf("%s method count = %d, want %d", name, len(got.Methods), len(want.Methods))
		}
		for j := range want.Methods {
			wm, gm := want.Methods[j], got.Methods[j]
			if wm.Kind != gm.Kind || wm.Label != gm.Label {
				t.Fatalf("%s method %d identity = %s/%s, want %s/%s", name, j, gm.Kind, gm.Label, wm.Kind, wm.Label)
			}
			if !reflect.DeepEqual(wm.Config, gm.Config) {
				t.Fatalf("%s method %d config differs:\neager=%#v\ncatalog=%#v", name, j, wm.Config, gm.Config)
			}
			if !reflect.DeepEqual(wm.Sources, gm.Sources) {
				t.Fatalf("%s method %d sources differ:\neager=%#v\ncatalog=%#v", name, j, wm.Sources, gm.Sources)
			}
		}

		eagerMethods, eagerSources, err := lock.SnapshotIntentMetadata(&config.Schema{Tools: map[string]*config.Tool{name: want}})
		if err != nil {
			t.Fatal(err)
		}
		catMethods, catSources, err := lock.SnapshotIntentMetadata(&config.Schema{Tools: map[string]*config.Tool{name: got}})
		if err != nil {
			t.Fatal(err)
		}
		if eagerMethods[name] != catMethods[name] {
			t.Fatalf("%s MethodsHash = %s, want %s", name, catMethods[name], eagerMethods[name])
		}
		if !reflect.DeepEqual(eagerSources, catSources) {
			t.Fatalf("%s SourceHash differs:\neager=%v\ncatalog=%v", name, eagerSources, catSources)
		}
	}
}

func BenchmarkCatalogLookup20(b *testing.B) {
	for _, n := range []int{1000, 100000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			if n >= 100000 && testing.Short() {
				b.Skip("100k recipe fixture is skipped in -short mode")
			}
			_, cat, facts := catalogParityFixture(b, n)
			names := make([]string, 20)
			for i := range names {
				names[i] = fmt.Sprintf("tool%05d", (i*37)%n)
			}
			b.Run("lookup", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for _, name := range names {
						if _, err := cat.Lookup(name); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
			b.Run("resolve", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for _, name := range names {
						rec, err := cat.Lookup(name)
						if err != nil {
							b.Fatal(err)
						}
						if _, err := rec.Specialize(facts); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		})
	}
}
