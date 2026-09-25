//go:build ignore

// Command fuzz_inventory lists every committed Go fuzz target regardless of
// the current GOOS/build tags. Runtime discovery remains responsible for
// deciding which of these targets can execute on the current CI runner.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type target struct {
	pkg  string
	name string
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	var targets []target
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".worktrees", "vendor", "dist":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		testingAliases := map[string]bool{}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || importPath != "testing" {
				continue
			}
			alias := "testing"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias != "." && alias != "_" {
				testingAliases[alias] = true
			}
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Fuzz") {
				continue
			}
			if !isFuzzSignature(fn.Type, testingAliases) {
				continue
			}
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			pkg := "."
			if rel != "." {
				pkg = "./" + filepath.ToSlash(rel)
			}
			targets = append(targets, target{pkg: pkg, name: fn.Name.Name})
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].pkg != targets[j].pkg {
			return targets[i].pkg < targets[j].pkg
		}
		return targets[i].name < targets[j].name
	})
	for _, item := range targets {
		fmt.Printf("%s %s\n", item.pkg, item.name)
	}
}

func isFuzzSignature(fn *ast.FuncType, testingAliases map[string]bool) bool {
	if fn == nil || fn.Params == nil || len(fn.Params.List) != 1 {
		return false
	}
	ptr, ok := fn.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := ptr.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "F" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && testingAliases[pkg.Name]
}
