package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
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

	runpkg "github.com/Khorea1/depengine/internal/run"
)

type Target struct {
	Package string
	Name    string
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fuzz target validation failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	return runWithRunner(ctx, args, runpkg.OSExecRunner{})
}

func runWithRunner(ctx context.Context, args []string, rn runpkg.Runner) error {
	flags := flag.NewFlagSet("fuzzall", flag.ContinueOnError)
	fuzzTime := flags.String("fuzztime", "10s", "bounded fuzzing time or iteration count passed to go test")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	root, err := moduleRoot(ctx, rn)
	if err != nil {
		return err
	}
	targets, err := readManifest(filepath.Join(root, "scripts", "fuzz-targets.txt"))
	if err != nil {
		return err
	}
	declared, err := discoverDeclaredTargets(root)
	if err != nil {
		return err
	}
	runnable, err := discoverTargets(ctx, root, rn)
	if err != nil {
		return err
	}
	if err := validateTargets(targets, declared, runnable); err != nil {
		return err
	}
	return runTargets(ctx, root, targets, runnable, *fuzzTime, rn)
}

func moduleRoot(ctx context.Context, rn runpkg.Runner) (string, error) {
	res := rn.Run(ctx, "go", "env", "GOMOD")
	if err := runpkg.CheckResult(res, "resolve module root"); err != nil {
		return "", err
	}
	gomod := strings.TrimSpace(string(res.Stdout))
	if gomod == "" || gomod == os.DevNull {
		return "", errors.New("resolve module root: current directory is not inside a Go module")
	}
	return filepath.Dir(gomod), nil
}

func readManifest(path string) ([]Target, error) {
	// #nosec G304 -- production passes the repository-owned manifest path; tests use temp fixtures.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var targets []Target
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = line[:comment]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || !validPackage(fields[0]) || !validTargetName(fields[1]) {
			return nil, fmt.Errorf("%s:%d: expected '<package> <FuzzTarget>'", path, lineNumber)
		}
		targets = append(targets, Target{Package: fields[0], Name: fields[1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return targets, nil
}

func validPackage(pkg string) bool {
	return pkg == "." || strings.HasPrefix(pkg, "./")
}

func validTargetName(name string) bool {
	return strings.HasPrefix(name, "Fuzz") && token.IsIdentifier(name)
}

func discoverDeclaredTargets(root string) ([]Target, error) {
	seen := make(map[Target]struct{})
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		testingAliases := make(map[string]bool)
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
			if !ok || fn.Recv != nil || !validTargetName(fn.Name.Name) || !isFuzzSignature(fn.Type, testingAliases) {
				continue
			}
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("fuzz target %s is outside module root", path)
			}
			pkg := "."
			if rel != "." {
				pkg = "./" + filepath.ToSlash(rel)
			}
			seen[Target{Package: pkg, Name: fn.Name.Name}] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sortedTargets(seen), nil
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

func discoverTargets(ctx context.Context, root string, rn runpkg.Runner) ([]Target, error) {
	res := runpkg.RunInDir(ctx, rn, root, "go", "list", "-f", "{{.Dir}}", "./...")
	if err := runpkg.CheckResult(res, "go list ./..."); err != nil {
		return nil, err
	}
	seen := make(map[Target]struct{})
	for _, directory := range strings.Split(strings.TrimSpace(string(res.Stdout)), "\n") {
		directory = strings.TrimSpace(directory)
		if directory == "" {
			continue
		}
		rel, err := filepath.Rel(root, directory)
		if err != nil {
			return nil, fmt.Errorf("resolve package path for %s: %w", directory, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("package directory %s is outside module root", directory)
		}
		pkg := "."
		if rel != "." {
			pkg = "./" + filepath.ToSlash(rel)
		}
		targets, err := discoverPackageTargets(ctx, root, pkg, rn)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			seen[target] = struct{}{}
		}
	}
	return sortedTargets(seen), nil
}

func discoverPackageTargets(ctx context.Context, root, pkg string, rn runpkg.Runner) ([]Target, error) {
	res := runpkg.RunInDir(ctx, rn, root, "go", "test", "-list=^Fuzz", "-run=^$", pkg)
	if err := runpkg.CheckResult(res, "go test -list fuzz targets for "+pkg); err != nil {
		return nil, err
	}
	var targets []Target
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		name := strings.TrimSpace(line)
		if validTargetName(name) {
			targets = append(targets, Target{Package: pkg, Name: name})
		}
	}
	return targets, nil
}

func validateTargets(expected, declared, runnable []Target) error {
	listed := make(map[Target]struct{}, len(expected))
	for _, target := range expected {
		if _, exists := listed[target]; exists {
			return fmt.Errorf("duplicate fuzz target entry: %s %s", target.Package, target.Name)
		}
		listed[target] = struct{}{}
	}
	declaredSet := targetSet(declared)
	runnableSet := targetSet(runnable)
	if len(declaredSet) == 0 && len(listed) == 0 {
		return errors.New("no fuzz targets discovered")
	}
	var missing, stale, inventoryGap []Target
	for target := range declaredSet {
		if _, ok := listed[target]; !ok {
			missing = append(missing, target)
		}
	}
	for target := range listed {
		if _, ok := declaredSet[target]; !ok {
			stale = append(stale, target)
		}
	}
	for target := range runnableSet {
		if _, ok := declaredSet[target]; !ok {
			inventoryGap = append(inventoryGap, target)
		}
	}
	sortTargets(missing)
	sortTargets(stale)
	sortTargets(inventoryGap)
	var details []string
	if len(missing) > 0 {
		details = append(details, "unlisted fuzz targets: "+formatTargets(missing))
	}
	if len(stale) > 0 {
		details = append(details, "missing or renamed targets: "+formatTargets(stale))
	}
	if len(inventoryGap) > 0 {
		details = append(details, "runtime targets missing from static inventory: "+formatTargets(inventoryGap))
	}
	if len(details) > 0 {
		return errors.New(strings.Join(details, "; "))
	}
	return nil
}

func runTargets(ctx context.Context, root string, targets, runnable []Target, fuzzTime string, rn runpkg.Runner) error {
	runnableSet := targetSet(runnable)
	for _, target := range targets {
		if _, ok := runnableSet[target]; !ok {
			fmt.Printf("==> %s %s (not runnable on this host; inventory only)\n", target.Package, target.Name)
			continue
		}
		fmt.Printf("==> %s %s (%s)\n", target.Package, target.Name, fuzzTime)
		res := runpkg.RunInDir(ctx, rn, root, "go", "test", target.Package, "-run=^$", "-fuzz=^"+target.Name+"$", "-fuzztime="+fuzzTime)
		if len(res.Stdout) > 0 {
			_, _ = os.Stdout.Write(res.Stdout)
		}
		if len(res.Stderr) > 0 {
			_, _ = os.Stderr.Write(res.Stderr)
		}
		if err := runpkg.CheckResult(res, "run fuzz target "+target.Package+" "+target.Name); err != nil {
			return err
		}
	}
	return nil
}

func targetSet(targets []Target) map[Target]struct{} {
	set := make(map[Target]struct{}, len(targets))
	for _, target := range targets {
		set[target] = struct{}{}
	}
	return set
}

func sortedTargets(set map[Target]struct{}) []Target {
	targets := make([]Target, 0, len(set))
	for target := range set {
		targets = append(targets, target)
	}
	sortTargets(targets)
	return targets
}

func sortTargets(targets []Target) {
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Package == targets[j].Package {
			return targets[i].Name < targets[j].Name
		}
		return targets[i].Package < targets[j].Package
	})
}

func formatTargets(targets []Target) string {
	formatted := make([]string, len(targets))
	for i, target := range targets {
		formatted[i] = target.Package + " " + target.Name
	}
	return strings.Join(formatted, ", ")
}
