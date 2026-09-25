package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	flags := flag.NewFlagSet("fuzzall", flag.ContinueOnError)
	fuzzTime := flags.String("fuzztime", "10s", "bounded fuzzing time or iteration count passed to go test")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	root, err := moduleRoot(ctx)
	if err != nil {
		return err
	}

	targets, err := readManifest(filepath.Join(root, "scripts", "fuzz-targets.txt"))
	if err != nil {
		return err
	}
	discovered, err := discoverTargets(ctx, root)
	if err != nil {
		return err
	}
	if err := validateTargets(targets, discovered); err != nil {
		return err
	}
	return runTargets(ctx, root, targets, *fuzzTime)
}

func moduleRoot(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOMOD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve module root: %w", err)
	}

	gomod := strings.TrimSpace(string(output))
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

func discoverTargets(ctx context.Context, root string) ([]Target, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, commandError("go list ./...", err)
	}

	seen := make(map[Target]struct{})
	for _, directory := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		directory = strings.TrimSpace(directory)
		if directory == "" {
			continue
		}

		rel, err := filepath.Rel(root, directory)
		if err != nil {
			return nil, fmt.Errorf("resolve package path for %s: %w", directory, err)
		}
		pkg := "."
		if rel != "." {
			pkg = "./" + filepath.ToSlash(rel)
		}

		targets, err := discoverPackageTargets(ctx, root, pkg)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			seen[target] = struct{}{}
		}
	}

	targets := make([]Target, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	sortTargets(targets)
	return targets, nil
}

func discoverPackageTargets(ctx context.Context, root, pkg string) ([]Target, error) {
	// #nosec G204 -- pkg is derived from `go list ./...` and is passed directly to go, never through a shell.
	cmd := exec.CommandContext(ctx, "go", "test", "-list=^Fuzz", "-run=^$", pkg)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, commandError("go test -list fuzz targets for "+pkg, err)
	}

	var targets []Target
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if validTargetName(name) {
			targets = append(targets, Target{Package: pkg, Name: name})
		}
	}
	return targets, nil
}

func validateTargets(expected, discovered []Target) error {
	listed := make(map[Target]struct{}, len(expected))
	for _, target := range expected {
		if _, exists := listed[target]; exists {
			return errors.New("duplicate fuzz target entry")
		}
		listed[target] = struct{}{}
	}

	actual := make(map[Target]struct{}, len(discovered))
	for _, target := range discovered {
		actual[target] = struct{}{}
	}

	if len(listed) == 0 && len(actual) == 0 {
		return errors.New("no runnable fuzz targets discovered")
	}

	var missing, stale []Target
	for target := range actual {
		if _, ok := listed[target]; !ok {
			missing = append(missing, target)
		}
	}
	for target := range listed {
		if _, ok := actual[target]; !ok {
			stale = append(stale, target)
		}
	}
	sortTargets(missing)
	sortTargets(stale)

	var details []string
	if len(missing) > 0 {
		details = append(details, "unlisted fuzz targets: "+formatTargets(missing))
	}
	if len(stale) > 0 {
		details = append(details, "missing or renamed targets: "+formatTargets(stale))
	}
	if len(details) > 0 {
		return errors.New(strings.Join(details, "; "))
	}
	return nil
}

func runTargets(ctx context.Context, root string, targets []Target, fuzzTime string) error {
	for _, target := range targets {
		fmt.Printf("==> %s %s (%s)\n", target.Package, target.Name, fuzzTime)
		// #nosec G204 -- manifest fields are validated and arguments are passed directly to go, never through a shell.
		cmd := exec.CommandContext(
			ctx,
			"go",
			"test",
			target.Package,
			"-run=^$",
			"-fuzz=^"+target.Name+"$",
			"-fuzztime="+fuzzTime,
		)
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return commandError("run fuzz target "+target.Package+" "+target.Name, err)
		}
	}
	return nil
}

func commandError(action string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr := strings.TrimSpace(string(exitErr.Stderr))
		if stderr != "" {
			return fmt.Errorf("%s: %w: %s", action, err, stderr)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
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
