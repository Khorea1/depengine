package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func TestResolveUniversalLockDocumentUsesEachToolSourceRevision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a hermetic POSIX brew executable")
	}

	root := t.TempDir()
	brewRoot := filepath.Join(root, "taps")
	for _, name := range []string{"alpha", "beta"} {
		tapPath := filepath.Join(brewRoot, name)
		if err := os.MkdirAll(tapPath, 0o700); err != nil {
			t.Fatal(err)
		}
		initLocalGitRepository(t, tapPath)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	brew := filepath.Join(binDir, "brew")
	const fakeBrew = `#!/bin/sh
case "$1" in
  tap)
    printf 'alpha\nbeta\n'
    ;;
  tap-info)
    printf '[{"name":"%s","remote":"file://%s/%s"}]\n' "$3" "$DEPENGINE_TEST_BREW_ROOT" "$3"
    ;;
  --repo)
    printf '%s/%s\n' "$DEPENGINE_TEST_BREW_ROOT" "$2"
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(brew, []byte(fakeBrew), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	cargo := filepath.Join(binDir, "cargo")
	if err := os.WriteFile(cargo, []byte("#!/bin/sh\nif [ \"$*\" = \"install --list\" ]; then exit 0; fi\nexit 1\n"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	t.Setenv("DEPENGINE_TEST_BREW_ROOT", brewRoot)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	schemaPath := filepath.Join(root, "schema.toml")
	var schemaText strings.Builder
	schemaText.WriteString("schema_version = 1\n\n")
	for _, name := range []string{"alpha", "beta"} {
		fmt.Fprintf(&schemaText, "[tools.%s]\nmethod_only = [\"cargo\"]\n[tools.%s.cargo]\npkg = %q\nversion = \"1.2.3\"\nsources = [{ kind = \"brew-tap\", name = \"%s\", url = %q }]\n\n", name, name, name, name, "file://"+filepath.Join(brewRoot, name))
	}
	if err := os.WriteFile(schemaPath, []byte(schemaText.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	schema, err := config.ParseProjectSchema(schemaPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	wantRevisions := make(map[string]string, 2)
	for _, name := range []string{"alpha", "beta"} {
		wantRevisions[name] = gitHead(t, filepath.Join(brewRoot, name))
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	document, err := resolveUniversalLockDocument(context.Background(), schema, "macos", nil, schemaPath, logger, nil, universalLockCoverageNames(schema.Tools))
	if err != nil {
		resolver := newInstallExecutor(installPlan{schema: schemaPath, dryRun: true}, schema, "macos", nil, time.Time{}, logger)
		t.Fatalf("%v; alpha attempts: %+v", err, resolver.ExplainTool(context.Background(), schema.Tools["alpha"], "macos"))
	}
	entries := make(map[string]struct {
		method, version, kind, name, revision string
	}, len(document.Entries))
	for _, entry := range document.Entries {
		if len(entry.Identity.Sources) != 1 {
			t.Fatalf("tool %q locked sources = %+v, want only its own Git-backed source", entry.Tool.Name, entry.Identity.Sources)
		}
		source := entry.Identity.Sources[0]
		entries[entry.Tool.Name] = struct {
			method, version, kind, name, revision string
		}{entry.Candidate.Method, entry.Identity.Version, source.Kind, source.Name, source.Revision}
	}
	for _, name := range []string{"alpha", "beta"} {
		got, ok := entries[name]
		if !ok {
			t.Fatalf("projection omitted tool %q", name)
		}
		if got.method != "cargo" || got.version != "1.2.3" || got.kind != "brew-tap" || got.name != name || got.revision != wantRevisions[name] {
			t.Fatalf("tool %q lock identity = %+v, want cargo/1.2.3 with brew-tap %s at %s", name, got, name, wantRevisions[name])
		}
	}
}

func initLocalGitRepository(t *testing.T, path string) {
	t.Helper()
	gitCommand(t, path, "init", "-q")
	gitCommand(t, path, "config", "user.email", "test@example.invalid")
	gitCommand(t, path, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(path, "README"), []byte("local fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, path, "add", "README")
	gitCommand(t, path, "commit", "-q", "-m", "fixture")
}

func gitHead(t *testing.T, path string) string {
	t.Helper()
	return gitCommand(t, path, "rev-parse", "HEAD")
}

func gitCommand(t *testing.T, path string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", path}, args...)
	result := (run.OSExecRunner{}).RunInDir(context.Background(), path, "git", args...)
	if err := run.CheckResult(result, "git "+strings.Join(commandArgs, " ")); err != nil {
		t.Fatalf("%v\n%s", err, result.Stderr)
	}
	return strings.TrimSpace(string(result.Stdout))
}
