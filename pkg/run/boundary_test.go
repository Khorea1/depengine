package run_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestExecutionPackagesDoNotBypassRunner protects the dry-run execution
// boundary. Adapter/executor code must never spawn subprocesses directly via
// os/exec; doing so would bypass BlockedRunner, logging/redaction, timeouts and
// fake runners. Executable lookup belongs behind run.LookPath for the same
// reason: tests and remote runners must observe the same capability probe.
func TestExecutionPackagesDoNotBypassRunner(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	packages := []string{
		"pkg/exec",
		"pkg/ecosystem",
		"pkg/git",
		"pkg/httpdownload",
		"pkg/container",
		"pkg/source",
		"pkg/native",
		"pkg/msi",
	}

	for _, rel := range packages {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Errorf("parse %s: %v", path, err)
				return nil
			}
			for _, imp := range file.Imports {
				if strings.Trim(imp.Path.Value, "\"") == "os/exec" {
					t.Errorf("%s imports os/exec; route subprocesses and executable lookup through pkg/run", filepath.ToSlash(path))
				}
			}
			// Parsing imports as Go instead of grepping avoids false positives in
			// comments and string literals.
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
