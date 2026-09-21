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
// boundary. Production code under pkg/ must never spawn subprocesses directly
// via os/exec outside pkg/run; doing so would bypass BlockedRunner,
// logging/redaction, timeouts and fake runners. Executable lookup belongs behind
// run.LookPath for the same reason: tests and remote runners must observe the
// same capability probe. Walking all packages makes this fail closed for future
// adapters instead of relying on a hand-maintained allow-list.
func TestExecutionPackagesDoNotBypassRunner(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	pkgRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), ".."))
	runRoot := filepath.Clean(filepath.Dir(thisFile))

	err := filepath.WalkDir(pkgRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == runRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, "\"") == "os/exec" {
				rel, relErr := filepath.Rel(pkgRoot, path)
				if relErr != nil {
					rel = path
				}
				t.Errorf("pkg/%s imports os/exec; route subprocesses and executable lookup through pkg/run", filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk pkg tree: %v", err)
	}
}
