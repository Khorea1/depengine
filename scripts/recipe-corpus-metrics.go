// Command recipe-corpus-metrics verifies pinned original-source hashes and
// compares authored projection size with resolved tool/candidate/when counts.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
)

func main() {
	root := "testdata/recipe-corpus/dep-ixd-13"
	files := []struct{ name, digest string }{
		{"brew/formula.rb", "2e493e50a49a37f7ed090f44019d1c58dd30c90a5c6fe1ecbcd69334dccaa60b"},
		{"scoop/manifest.json", "f28790bf5e965c8df9a9318a773f47c0b2a4d6d2866f11a4b4b54ef904e5062c"},
		{"winget/manifest.yaml", "3235a2308587f17e5f014bb5799ab718206af2eaa034b9a64a65a0ec73c081b6"},
		{"winget/version.yaml", "21e27fde586df90626a7a9dec1d68dcb0bfd3ae0744545f2abdd6d7e09910499"},
		{"winget/locale.yaml", "d1b904d3b8e3f19646bf56ff62e7cceb4f83c4bf46643ed3901b70b816bcd8c9"},
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(root, file.name))
		if err != nil {
			fail(err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != file.digest {
			fail(fmt.Errorf("%s SHA-256=%s, want %s", file.name, got, file.digest))
		}
		fmt.Printf("source %-22s lines=%d sha256=%s\n", file.name, strings.Count(string(data), "\n"), file.digest)
	}
	for _, name := range []string{"enumerated.toml", "intent.toml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			fail(err)
		}
		schema, err := config.ParseProjectSchema(filepath.Join(root, name), nil)
		if err != nil {
			fail(err)
		}
		var catalog *config.Catalog
		if name == "intent.toml" {
			recipeRoot, err := filepath.Abs(filepath.Join(root, "catalog"))
			if err != nil {
				fail(err)
			}
			catalog = config.NewCatalog([]string{recipeRoot})
		}
		resolved, err := config.ResolveLayers(schema, nil, catalog, nil, false)
		if err != nil {
			fail(err)
		}
		methods, conditions := 0, 0
		for _, tool := range resolved.Schema.Tools {
			methods += len(tool.Methods)
			for _, candidate := range tool.Methods {
				if candidate.When != nil {
					conditions++
				}
			}
		}
		fmt.Printf("projection %-20s lines=%d tools=%d candidates=%d when=%d\n",
			name, strings.Count(string(data), "\n"), len(resolved.Schema.Tools), methods, conditions)
	}
	for _, name := range []string{"intent.toml", "catalog/ripgrep.toml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			fail(err)
		}
		fmt.Printf("authored %-22s lines=%d\n", name, strings.Count(string(data), "\n"))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
