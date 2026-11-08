// Command recipe-corpus verifies provenance, regenerates canonical variants, and
// compares enumerated and catalog-backed projections of the same real tools.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
)

func main() {
	root := flag.String("root", "testdata/recipe-corpus/dep-ixd-13", "corpus root")
	write := flag.Bool("write", false, "write canonical generated TOML")
	flag.Parse()
	if err := generateCorpusRecipes(*root, *write); err != nil {
		fail(err)
	}
	recipeFiles, recipeLines, licenseFiles, licenseLines, err := verifyCorpusSources(*root, true)
	if err != nil {
		fail(err)
	}
	fmt.Printf("corpus recipe_files=%d recipe_lines=%d separate_license_files=%d license_lines=%d\n", recipeFiles, recipeLines, licenseFiles, licenseLines)
	for _, name := range []string{"enumerated.toml", "intent.toml"} {
		data, err := os.ReadFile(filepath.Join(*root, name))
		if err != nil {
			fail(err)
		}
		schema, err := config.ParseProjectSchema(filepath.Join(*root, name), nil)
		if err != nil {
			fail(err)
		}
		var catalog *config.Catalog
		if name == "intent.toml" {
			catalogRoot, err := filepath.Abs(filepath.Join(*root, "catalog"))
			if err != nil {
				fail(err)
			}
			catalog = config.NewCatalog([]string{catalogRoot})
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
		fmt.Printf("projection %-20s lines=%d tools=%d candidates=%d when=%d\n", name, strings.Count(string(data), "\n"), len(resolved.Schema.Tools), methods, conditions)
	}
	for _, name := range []string{"intent.toml", "enumerated.toml", "catalog/fd.toml", "catalog/ripgrep.toml"} {
		data, err := os.ReadFile(filepath.Join(*root, name))
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
